package api

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/platform/netguard"
)

// The account's MQTT broker: the collector connects to it and publishes the vehicles'
// state, for Home Assistant and the like. One per account, given here, never by the
// environment. Its password is never returned: whether it has one only. Managed with
// the session alone: an access token neither reads nor changes it.

// Brokers keeps the account's MQTT broker. Every method is restricted to the account.
type Brokers interface {
	// MQTTBroker returns the account's broker, with what the collector wrote of its
	// connection; false: none.
	MQTTBroker(ctx context.Context, accountID string) (MQTTBroker, bool, error)
	// SetMQTTBroker stores the account's broker as of at, its Password when
	// SetPassword, else the stored one; what the collector wrote of the previous
	// configuration goes.
	SetMQTTBroker(ctx context.Context, accountID string, b MQTTBroker, at time.Time) error
	// DeleteMQTTBroker removes it: the collector no longer publishes.
	DeleteMQTTBroker(ctx context.Context, accountID string) error
}

// MQTTBroker is an account's broker.
type MQTTBroker struct {
	URL      string // mqtt://host:port or mqtts://host:port
	Username string // empty: none
	// Password is written only when SetPassword (empty: none), and never read back:
	// PasswordSet tells whether there is one.
	Password    string
	SetPassword bool
	PasswordSet bool
	ClientID    string // empty: the collector's own
	TopicPrefix string
	// Discovery publishes Home Assistant's discovery messages, under DiscoveryPrefix.
	Discovery       bool
	DiscoveryPrefix string
	PublishLocation bool
	UpdatedAt       time.Time
	Status          MQTTStatus
}

// MQTTStatus is what the collector wrote of its connection to the broker.
type MQTTStatus struct {
	ConnectedAt time.Time // the latest connection; zero: none
	FailedAt    time.Time // the latest failure; zero: none
	Failure     string
}

// The kinds of failure of the connection to the broker, as the collector writes them.
type mqttFailureJSON string

const (
	mqttUnreachable    mqttFailureJSON = "unreachable"
	mqttRefusedAddress mqttFailureJSON = "refused_address"
	mqttTLS            mqttFailureJSON = "tls"
	mqttNotAuthorized  mqttFailureJSON = "not_authorized"
	mqttRejected       mqttFailureJSON = "rejected"
)

// Schema lists the kinds.
func (mqttFailureJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(mqttUnreachable, mqttRefusedAddress, mqttTLS, mqttNotAuthorized, mqttRejected)
}

type mqttBrokerJSON struct {
	URL             string    `json:"url" maxLength:"255" doc:"mqtt://host:port, or mqtts://host:port over TLS; the ports default to 1883 and 8883." example:"mqtt://homeassistant.local:1883"`
	Username        string    `json:"username" maxLength:"256" doc:"Empty: none."`
	PasswordSet     bool      `json:"password_set" doc:"The broker has a password: it is never returned."`
	ClientID        string    `json:"client_id" pattern:"^[0-9A-Za-z_-]{0,64}$" doc:"The MQTT client identifier; empty: one the collector derives from the account."`
	TopicPrefix     string    `json:"topic_prefix" minLength:"1" maxLength:"64" pattern:"^[^/+#]+(/[^/+#]+)*$" doc:"The topics are under it: <topic_prefix>/vehicles/<vehicle id>/…, and the availability <topic_prefix>/status."`
	Discovery       bool      `json:"discovery" doc:"Home Assistant's discovery messages are published, which create its entities."`
	DiscoveryPrefix string    `json:"discovery_prefix" minLength:"1" maxLength:"64" pattern:"^[^/+#]+(/[^/+#]+)*$" doc:"Where Home Assistant reads them: homeassistant unless changed in its MQTT integration."`
	PublishLocation bool      `json:"publish_location" doc:"The vehicles' position is published."`
	UpdatedAt       time.Time `json:"updated_at" pattern:"Z$"`
}

type mqttStatusJSON struct {
	ConnectedAt *time.Time            `json:"connected_at" pattern:"Z$" doc:"The collector's latest connection to the broker; null: none since the configuration was saved."`
	FailedAt    *time.Time            `json:"failed_at" pattern:"Z$" doc:"Its latest failure, to connect or to publish: the connection is down when it is after connected_at. null: none."`
	Failure     null[mqttFailureJSON] `json:"failure" doc:"The kind of that failure, never the broker's message. unreachable: no connection to the host. refused_address: the instance does not publish to the host's address. tls: the TLS handshake failed (a certificate not trusted, or not for the host). not_authorized: the broker refused the username or the password. rejected: the broker refused the connection otherwise (the client identifier, the protocol). null: none."`
}

type mqttSettingsJSON struct {
	Broker null[mqttBrokerJSON] `json:"broker" doc:"null: none, nothing is published."`
	Status null[mqttStatusJSON] `json:"status" doc:"The collector's connection to the broker; null: no broker."`
	// PublicOnly is the instance's rule, which the interface tells before a refusal.
	PublicOnly bool `json:"public_only" doc:"The instance publishes to brokers on the Internet only, over TLS (mqtts://): a broker on a local network must be exposed to it."`
}

type mqttBrokerUpdateJSON struct {
	URL      string  `json:"url" minLength:"1" maxLength:"255" doc:"mqtt://host:port, or mqtts://host:port over TLS; the ports default to 1883 and 8883. Neither a path, nor a query, nor credentials."`
	Username string  `json:"username" maxLength:"256" doc:"Empty: none."`
	Password *string `json:"password" maxLength:"256" doc:"Never returned. null: the stored one is kept, unless the host changes; empty: none."`
	ClientID string  `json:"client_id" pattern:"^[0-9A-Za-z_-]{0,64}$" doc:"The MQTT client identifier; empty: one the collector derives from the account."`
	// A prefix is topic levels, without the wildcards nor an empty one.
	TopicPrefix     string `json:"topic_prefix" minLength:"1" maxLength:"64" pattern:"^[^/+#]+(/[^/+#]+)*$" doc:"runsten, unless several instances publish to one broker."`
	Discovery       bool   `json:"discovery"`
	DiscoveryPrefix string `json:"discovery_prefix" minLength:"1" maxLength:"64" pattern:"^[^/+#]+(/[^/+#]+)*$" example:"homeassistant"`
	PublishLocation bool   `json:"publish_location"`
}

type mqttBrokerInput struct {
	Body mqttBrokerUpdateJSON
}

func (s *Server) mqttSettings(ctx context.Context, account string) (mqttSettingsJSON, error) {
	out := mqttSettingsJSON{PublicOnly: s.PublicBrokersOnly}
	b, ok, err := s.Brokers.MQTTBroker(ctx, account)
	if err != nil {
		return out, s.internal("MQTT broker not read", err)
	}
	if !ok {
		return out, nil
	}
	out.Broker = known(mqttBrokerJSON{
		URL: b.URL, Username: b.Username, PasswordSet: b.PasswordSet, ClientID: b.ClientID, TopicPrefix: b.TopicPrefix,
		Discovery: b.Discovery, DiscoveryPrefix: b.DiscoveryPrefix, PublishLocation: b.PublishLocation, UpdatedAt: b.UpdatedAt,
	})
	st := mqttStatusJSON{ConnectedAt: timeOrNil(b.Status.ConnectedAt), FailedAt: timeOrNil(b.Status.FailedAt)}
	if b.Status.Failure != "" {
		st.Failure = known(mqttFailureJSON(b.Status.Failure))
	}
	out.Status = known(st)
	return out, nil
}

func (s *Server) getMQTT(ctx context.Context, _ *struct{}) (*body[mqttSettingsJSON], error) {
	out, err := s.mqttSettings(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) putMQTT(ctx context.Context, in *mqttBrokerInput) (*body[mqttSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	host, err := brokerHost(in.Body.URL)
	if err != nil {
		return nil, apiError(http.StatusBadRequest, codeInvalidBody, "body.url: "+err.Error())
	}
	if s.PublicBrokersOnly {
		if why := publicOnly(in.Body.URL, host); why != "" {
			return nil, apiError(http.StatusBadRequest, codeBrokerRefused, why)
		}
	}
	b := MQTTBroker{
		URL: in.Body.URL, Username: in.Body.Username, ClientID: in.Body.ClientID, TopicPrefix: in.Body.TopicPrefix,
		Discovery: in.Body.Discovery, DiscoveryPrefix: in.Body.DiscoveryPrefix, PublishLocation: in.Body.PublishLocation,
	}
	if in.Body.Password != nil {
		b.Password, b.SetPassword = *in.Body.Password, true
	} else {
		// A password kept goes to the host it was given for, never to another: whoever
		// holds the session would read it there.
		old, ok, err := s.Brokers.MQTTBroker(ctx, account)
		if err != nil {
			return nil, s.internal("MQTT broker not read", err)
		}
		if ok && old.PasswordSet {
			if oldHost, err := brokerHost(old.URL); err != nil || oldHost != host {
				return nil, apiError(http.StatusBadRequest, codeInvalidBody, "body.password: required for another host")
			}
		}
	}
	if err := s.Brokers.SetMQTTBroker(ctx, account, b, s.Clock.Now()); err != nil {
		return nil, s.internal("MQTT broker not stored", err)
	}
	s.Log.Info("MQTT broker set", "account", account)
	out, err := s.mqttSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) deleteMQTT(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	if err := s.Brokers.DeleteMQTTBroker(ctx, account); err != nil {
		return nil, s.internal("MQTT broker not deleted", err)
	}
	s.Log.Info("MQTT broker deleted", "account", account)
	return &struct{}{}, nil
}

// brokerHost checks a broker's URL and returns its host, lowercase, without brackets.
// The errors never repeat the URL: it may carry credentials pasted by mistake.
func brokerHost(raw string) (string, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return "", errors.New("not a URL")
	case u.Scheme != "mqtt" && u.Scheme != "mqtts":
		return "", errors.New("the scheme must be mqtt or mqtts")
	case u.User != nil:
		return "", errors.New("credentials go in username and password, not in the URL")
	case u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "":
		return "", errors.New("neither a path, nor a query, nor a fragment")
	case u.Hostname() == "":
		return "", errors.New("a host is required")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", errors.New("the port must be between 1 and 65535")
		}
	}
	return strings.ToLower(u.Hostname()), nil
}

// publicOnly returns why an instance that publishes to the Internet only refuses a
// broker, as far as its URL tells without resolving its name; empty: it does not. The
// collector checks every address again when it connects: a name may resolve to
// another later.
func publicOnly(raw, host string) string {
	if strings.HasPrefix(raw, "mqtt://") {
		return "this instance publishes over TLS only (mqtts://)"
	}
	const local = "this instance does not publish to a local or reserved address"
	if a, err := netip.ParseAddr(host); err == nil {
		if !netguard.Public(a) {
			return local
		}
		return ""
	}
	// A single label resolves through the host's search domains: a local name.
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".") {
		return local
	}
	return ""
}

func (s *Server) registerMQTT(api huma.API) {
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "getMQTT", Method: http.MethodGet, Path: "/mqtt", Tags: []string{"mqtt"},
		Summary:     "The account's MQTT broker",
		Description: "The broker the collector publishes the vehicles' state to, without its password, and the collector's connection to it.",
	}, "The broker.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errSessionOnly, http.StatusInternalServerError: errInternal,
	}), s.getMQTT)
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "putMQTT", Method: http.MethodPut, Path: "/mqtt", Tags: []string{"mqtt"},
		Summary: "Set the account's MQTT broker",
		Description: "The collector connects to it again with this configuration, at its next pass. What it wrote of the " +
			"previous connection goes.",
		Middlewares: huma.Middlewares{s.requireJSON, s.requireMQTT},
	}, "The broker.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " The URL is checked beyond its schema, and a password is required for another " +
			"host than the stored one's. `broker_refused`: the instance publishes to brokers on the Internet only, over " +
			"TLS (`public_only`), and the URL names another.",
		http.StatusForbidden: errCrossOrigin + " " + errSessionOnly + " " + errFeature,
	})), s.putMQTT)
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "deleteMQTT", Method: http.MethodDelete, Path: "/mqtt", Tags: []string{"mqtt"},
		Summary:       "Remove the account's MQTT broker",
		Description:   "The collector disconnects from it, at its next pass, and publishes nothing more. The retained messages stay on the broker.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin + " " + errSessionOnly,
		http.StatusInternalServerError: errInternal,
	}), s.deleteMQTT)
}
