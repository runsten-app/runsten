// Package publish builds what Runsten publishes of the vehicles to an account's MQTT
// broker: one topic per value, in plain text, retained, so that a subscriber that comes
// later (Home Assistant restarted) gets the latest value. It is pure: neither network
// nor database. The adapter (publish/mqtt) holds the connections.
//
// The topics are a versioned contract, as the API: a topic or a value changes only
// with a new version, documented.
package publish

import (
	"strconv"
	"time"

	"runsten/internal/core"
)

// Broker is an account's broker, as the publisher connects to it.
type Broker struct {
	AccountID string
	URL       string // mqtt://host[:port] or mqtts://host[:port]
	Username  string
	Password  string // empty: none
	// ClientID is the MQTT client identifier; empty: derived from the account
	// (ClientIdentifier).
	ClientID        string
	TopicPrefix     string
	Discovery       bool
	DiscoveryPrefix string
	PublishLocation bool
	// UpdatedAt changes with any change of the configuration: the connection is opened
	// again.
	UpdatedAt time.Time
}

// ClientIdentifier is the client identifier to connect with: the one set, else one
// derived from the account, 23 characters at most (the length every MQTT 3.1.1 server
// accepts).
func (b Broker) ClientIdentifier() string {
	if b.ClientID != "" {
		return b.ClientID
	}
	id := make([]byte, 0, 15)
	for i := 0; i < len(b.AccountID) && len(id) < cap(id); i++ {
		if c := b.AccountID[i]; c != '-' {
			id = append(id, c)
		}
	}
	return "runsten-" + string(id)
}

// BrokerRef is an account that has a broker, and when its configuration changed.
type BrokerRef struct {
	AccountID string
	UpdatedAt time.Time
}

// Status is what the publisher tells the user of its connection to an account's
// broker: when it connected, and its latest failure as a kind, never the broker's
// message. A zero time keeps the one stored before, which a restarted collector does
// not know.
type Status struct {
	ConnectedAt time.Time
	FailedAt    time.Time
	Failure     string // one of the Fail constants; empty with a zero FailedAt
}

// The kinds of a Status's failure.
const (
	FailUnreachable    = "unreachable"     // no connection: name not resolved, refused, timed out, lost
	FailRefusedAddress = "refused_address" // the broker resolves to an address of a private network, which the instance refuses
	FailTLS            = "tls"             // the TLS handshake failed: the certificate, mostly
	FailNotAuthorized  = "not_authorized"  // the broker refused the username or the password
	FailRejected       = "rejected"        // the broker refused the connection otherwise (client ID, protocol, unavailable)
)

// Message is a message to publish.
type Message struct {
	Topic   string
	Payload string
	// Retained messages are kept by the broker for the subscribers to come; an empty
	// one removes the topic's.
	Retained bool
}

// The availability, the payloads of Availability: online while the publisher is
// connected, offline as its testament (last will) or when it stops.
const (
	Online  = "online"
	Offline = "offline"
)

// AvailabilityTopic is where the publisher tells whether it is connected.
func AvailabilityTopic(prefix string) string { return prefix + "/status" }

// Availability is the message that tells whether the publisher is connected.
func Availability(prefix string, online bool) Message {
	p := Offline
	if online {
		p = Online
	}
	return Message{Topic: AvailabilityTopic(prefix), Payload: p, Retained: true}
}

// State is the vehicle's current state, one retained message per value, in the order
// of the contract. An unknown value is an empty message, which removes the previous
// one: unknown is never a zero. The location is left empty unless the broker
// publishes it, so that turning it off removes the one published before.
func State(b Broker, vehicleID string, c core.Current) []Message {
	n := c.Snapshot
	base := b.TopicPrefix + "/vehicles/" + vehicleID + "/"
	location := ""
	if b.PublishLocation {
		location = text(n.Position, positionJSON)
	}
	readAt := ""
	if at := c.CheckedAt(); !at.IsZero() {
		readAt = at.UTC().Format(time.RFC3339)
	}
	values := []struct{ name, payload string }{
		{"battery_level", text(n.SoC, number)},
		{"range_km", text(n.RangeKm, number)},
		{"charging_status", text(n.Charging, str[core.ChargingStatus])},
		{"plug", text(n.Connection, str[core.Connection])},
		{"charge_type", text(n.ChargeType, str[core.ChargeType])},
		{"charging_power_kw", text(n.PowerW, func(w float64) string { return number(w / 1000) })},
		{"target_soc", text(n.TargetSoC, number)},
		{"odometer_km", text(n.OdometerKm, number)},
		{"engine", text(n.Engine, str[core.EngineState])},
		{"location", location},
		{"read_at", readAt},
	}
	msgs := make([]Message, len(values))
	for i, v := range values {
		msgs[i] = Message{Topic: base + v.name, Payload: v.payload, Retained: true}
	}
	return msgs
}

// text is the value's payload; empty when it is unknown.
func text[T any](v core.Value[T], f func(T) string) string {
	if !v.OK {
		return ""
	}
	return f(v.V)
}

func str[T ~string](v T) string { return string(v) }

// number writes a decimal number with a point, as short as it reads back the same.
func number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func positionJSON(p core.Position) string {
	return `{"latitude":` + number(p.Lat) + `,"longitude":` + number(p.Lon) + `}`
}
