// Package mqtt publishes the vehicles' state to each account's MQTT broker, on paho
// (MQTT 3.1.1, QoS 1, retained messages). It implements the collector's Publisher.
//
// One connection per account that set a broker, opened at the first pass that finds
// its configuration, and closed when the configuration is removed or changed, or when
// the account may no longer publish. Its testament (last will) is the availability
// offline, retained; at each connection, the availability online, Home Assistant's
// discovery and the whole state known of the account's vehicles, then only what
// changes. A vehicle gone from the account has its entities and its values removed. A connection lost
// is opened again by paho, with a growing delay; one that could not be opened, by the
// next passes, with a growing delay too.
//
// Every publication runs in the collector's goroutine: paho's only tell what happens
// to the connection, which the next call of the collector acts upon.
//
// Where the instance accepts public brokers only, the dialer refuses an address that
// is not of the Internet, as resolved when connecting (netguard.Public), and the
// broker must speak TLS. TLS verifies the broker with the system's authorities.
package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"sync"
	"syscall"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/platform/netguard"
	"runsten/internal/publish"
)

// Store lists the accounts' brokers, reads their configuration and writes what the
// publisher knows of its connections.
type Store interface {
	// MQTTBrokers returns the accounts that have a broker, across the accounts.
	MQTTBrokers(ctx context.Context) ([]publish.BrokerRef, error)
	// PublishBroker returns the account's broker, its password opened; false: none.
	PublishBroker(ctx context.Context, accountID string) (publish.Broker, bool, error)
	// SaveMQTTStatus writes the status of the connection to the broker configured at
	// updatedAt; nothing if its configuration changed since.
	SaveMQTTStatus(ctx context.Context, accountID string, updatedAt time.Time, s publish.Status) error
}

// States reads a vehicle's current state, as GET /vehicles/{id}/state does.
type States interface {
	Current(ctx context.Context, accountID, vehicleID string, at time.Time) (core.Current, error)
}

// Models tells what a vehicle is, from its current state, for its device in Home
// Assistant: the binary knows the catalog, the publisher does not.
type Models interface {
	Model(ctx context.Context, accountID, vehicleID string, c core.Current) (publish.Model, error)
}

const (
	qos = 1
	// connectTimeout bounds a connection: the TCP connection, TLS and the broker's answer.
	connectTimeout = 10 * time.Second
	// publishTimeout bounds the wait for the broker's acknowledgment of a message.
	publishTimeout = 5 * time.Second
	// keepAlive is how often the client pings an idle broker, which finds it gone after
	// one and a half times as long, and publishes its testament.
	keepAlive = time.Minute
	// minRetry and maxRetry bound the delay before the next attempt to connect, doubled
	// after each failure.
	minRetry = 30 * time.Second
	maxRetry = 10 * time.Minute
	// quiesce is how long a closed connection waits for the work under way.
	quiesce = 250 // ms
)

// The ports of MQTT, plain and over TLS, when the URL names none.
const (
	plainPort = "1883"
	tlsPort   = "8883"
)

var (
	errRefusedAddress = errors.New("the broker's address is not public")
	errTLS            = errors.New("TLS")
)

// Publisher publishes to the accounts' brokers. Its methods are called from the
// collector's goroutine.
type Publisher struct {
	st         Store
	states     States
	models     Models
	clk        clock.Clock
	publicOnly bool
	log        *slog.Logger
	// rootCAs verify the brokers; nil: the system's.
	rootCAs *x509.CertPool

	conns map[string]*conn // by account
	// unreadable are the configurations that could not be read, by account: read again
	// once changed.
	unreadable map[string]time.Time
}

// New creates a publisher. publicOnly refuses the brokers of a private network and
// those without TLS (RUNSTEN_MQTT_PRIVATE_BROKERS false).
func New(st Store, states States, models Models, clk clock.Clock, publicOnly bool, log *slog.Logger) *Publisher {
	return &Publisher{
		st: st, states: states, models: models, clk: clk, publicOnly: publicOnly, log: log,
		conns: map[string]*conn{}, unreadable: map[string]time.Time{},
	}
}

// conn is the connection to an account's broker.
type conn struct {
	b      publish.Broker
	client paho.Client
	// connecting is the attempt to connect under way, if any.
	connecting paho.Token
	retryAt    time.Time
	delay      time.Duration
	// models, values and discovery are the latest of each vehicle, by ID: what it is,
	// its state's messages and its entities' configurations (none without discovery).
	models    map[string]publish.Model
	values    map[string][]publish.Message
	discovery map[string][]publish.Message
	// sent is what the broker acknowledged on this connection, by vehicle and topic.
	sent map[string]map[string]string
	// removed are the messages that remove the vehicles gone from the account, until the
	// broker acknowledged them all.
	removed map[string][]publish.Message
	// logged is what was last logged of the connection: connected, or a failure.
	logged string

	mu sync.Mutex // guards what follows, written by paho's goroutines
	// fresh: connected since the whole state was last sent.
	fresh  bool
	status publish.Status
	dirty  bool // status not written yet
}

// Sync implements collector.Publisher: a connection for each account that has a broker
// and is not withheld, the one of its current configuration. The connection of a
// configuration removed or changed first removes what the next one will not replace;
// a withheld account's leaves its entities, unavailable. It opens the connections due,
// removes the vehicles gone from vehicles (by account), and sends what a connection
// opened since the previous call has not received yet.
func (p *Publisher) Sync(ctx context.Context, vehicles map[string][]string, withheld map[string]bool) error {
	refs, err := p.st.MQTTBrokers(ctx)
	if err != nil {
		return fmt.Errorf("brokers: %w", err)
	}
	listed := make(map[string]time.Time, len(refs))
	for _, r := range refs {
		listed[r.AccountID] = r.UpdatedAt
	}
	for account := range p.unreadable {
		if at, ok := listed[account]; !ok || !at.Equal(p.unreadable[account]) {
			delete(p.unreadable, account)
		}
	}
	for _, account := range sortedKeys(p.conns) {
		c := p.conns[account]
		at, ok := listed[account]
		switch {
		case withheld[account]:
			p.close(c, cleanup{})
		case !ok:
			p.close(c, cleanupFor(c.b, nil))
		case !at.Equal(c.b.UpdatedAt):
			next, found := p.read(ctx, account, at)
			cl := cleanup{} // a configuration not read: nothing is known of the next one
			switch {
			case found:
				cl = cleanupFor(c.b, next)
			case p.unreadable[account].IsZero(): // removed since the list
				cl = cleanupFor(c.b, nil)
			}
			p.close(c, cl)
			delete(p.conns, account)
			if found {
				p.open(*next)
			}
			continue
		default:
			continue
		}
		delete(p.conns, account)
	}
	for _, account := range sortedKeys(listed) {
		if _, ok := p.conns[account]; !ok && !withheld[account] && p.unreadable[account].IsZero() {
			if b, found := p.read(ctx, account, listed[account]); found {
				p.open(*b)
			}
		}
	}
	now := p.clk.Now()
	for _, account := range sortedKeys(p.conns) {
		c := p.conns[account]
		c.forget(vehicles[account])
		c.connect(now)
		if err := p.flush(c); err != nil {
			p.log.Warn("MQTT state not published", "account", account, "err", err)
		}
		p.saveStatus(ctx, c)
	}
	return nil
}

// Publish implements collector.Publisher: the vehicle's values that changed since the
// broker acknowledged them, or all of them on a new connection. Without a connection
// open, its state waits for one.
func (p *Publisher) Publish(ctx context.Context, accountID, vehicleID string) error {
	c, ok := p.conns[accountID]
	if !ok {
		return nil
	}
	cur, err := p.states.Current(ctx, accountID, vehicleID, p.clk.Now())
	if err != nil {
		return fmt.Errorf("state of vehicle %s: %w", vehicleID, err)
	}
	m, err := p.models.Model(ctx, accountID, vehicleID, cur)
	if err != nil {
		return fmt.Errorf("model of vehicle %s: %w", vehicleID, err)
	}
	delete(c.removed, vehicleID) // back in the account
	c.models[vehicleID] = m
	c.values[vehicleID] = publish.State(c.b, vehicleID, cur)
	c.describe()
	err = p.flush(c)
	p.saveStatus(ctx, c)
	if err != nil {
		return fmt.Errorf("account %s: %w", accountID, err)
	}
	return nil
}

// Close publishes the availability offline on every connection, and closes them: the
// collector stops.
func (p *Publisher) Close() {
	for account, c := range p.conns {
		p.close(c, cleanup{})
		delete(p.conns, account)
	}
}

// read reads the account's broker configured at updatedAt; false when it has none, or
// it could not be read.
func (p *Publisher) read(ctx context.Context, account string, updatedAt time.Time) (*publish.Broker, bool) {
	b, ok, err := p.st.PublishBroker(ctx, account)
	if err != nil {
		// Logged once per configuration: a password sealed with another key does not
		// open on the next pass either.
		p.unreadable[account] = updatedAt
		p.log.Error("MQTT broker configuration not read", "account", account, "err", err)
		return nil, false
	}
	return &b, ok // not ok: removed since the list
}

// open creates the connection to the broker; it connects at the next connect.
func (p *Publisher) open(b publish.Broker) {
	c := &conn{
		b: b, models: map[string]publish.Model{}, values: map[string][]publish.Message{},
		discovery: map[string][]publish.Message{}, sent: map[string]map[string]string{},
		removed: map[string][]publish.Message{},
	}
	opts := paho.NewClientOptions().
		AddBroker(b.URL).
		SetClientID(b.ClientIdentifier()).
		SetUsername(b.Username).
		SetPassword(b.Password).
		SetProtocolVersion(4). // 3.1.1, explicit: paho would try 3.1 after a refusal
		SetCleanSession(true).
		SetOrderMatters(false).
		SetWill(publish.AvailabilityTopic(b.TopicPrefix), publish.Offline, qos, true).
		SetKeepAlive(keepAlive).
		SetConnectTimeout(connectTimeout).
		SetWriteTimeout(publishTimeout).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(maxRetry).
		SetCustomOpenConnectionFn(p.dial).
		SetConnectionNotificationHandler(c.notified(p.clk))
	c.client = paho.NewClient(opts)
	p.conns[b.AccountID] = c
}

// cleanup is what a connection removes from its broker as it closes.
type cleanup struct {
	discovery bool // the entities' configurations: Home Assistant removes them
	state     bool // the values and the availability
}

// cleanupFor is what the connection to old removes when next replaces it (nil: the
// broker removed): everything when the broker goes or moves; the entities when the
// discovery stops or moves to another prefix; the values when they move to another
// prefix. What the next connection publishes in the same place replaces the rest.
func cleanupFor(old publish.Broker, next *publish.Broker) cleanup {
	if next == nil || next.URL != old.URL {
		return cleanup{discovery: old.Discovery, state: true}
	}
	return cleanup{
		discovery: old.Discovery && (!next.Discovery || next.DiscoveryPrefix != old.DiscoveryPrefix),
		state:     next.TopicPrefix != old.TopicPrefix,
	}
}

// close removes what cl says, if the connection is up, then publishes the availability
// offline, which the broker does not on a clean disconnection (or empties it, when the
// values go), and disconnects. A connection down removes nothing: what it would have
// removed stays on the broker.
func (p *Publisher) close(c *conn, cl cleanup) {
	if c.client.IsConnectionOpen() {
		var msgs []publish.Message
		for _, vehicle := range sortedKeys(c.removed) {
			msgs = append(msgs, c.removed[vehicle]...)
		}
		for _, vehicle := range sortedKeys(c.models) {
			if cl.discovery {
				msgs = append(msgs, publish.RemoveDiscovery(c.b, vehicle)...)
			}
			if cl.state {
				msgs = append(msgs, publish.RemoveState(c.b, vehicle)...)
			}
		}
		status := publish.Availability(c.b.TopicPrefix, false)
		if cl.state {
			status.Payload = ""
		}
		msgs = append(msgs, status)
		tokens := make([]paho.Token, len(msgs))
		for i, m := range msgs {
			tokens[i] = c.client.Publish(m.Topic, qos, m.Retained, m.Payload)
		}
		for _, tok := range tokens {
			if !tok.WaitTimeout(publishTimeout) {
				break
			}
		}
	}
	c.client.Disconnect(quiesce)
}

// forget removes the vehicles no longer in the account (current, their IDs): their
// entities, if discovered, and their values, sent at the next flush.
func (c *conn) forget(current []string) {
	gone := false
	for vehicle := range c.models {
		if slices.Contains(current, vehicle) {
			continue
		}
		msgs := publish.RemoveState(c.b, vehicle)
		if c.b.Discovery {
			msgs = append(publish.RemoveDiscovery(c.b, vehicle), msgs...)
		}
		c.removed[vehicle] = msgs
		delete(c.models, vehicle)
		delete(c.values, vehicle)
		delete(c.discovery, vehicle)
		delete(c.sent, vehicle)
		gone = true
	}
	if gone {
		c.describe() // the labels of those left may change
	}
}

// describe builds the configurations of every vehicle's entities, whose labels depend
// on one another: two alike tell themselves apart.
func (c *conn) describe() {
	if !c.b.Discovery {
		return
	}
	labels := publish.Labels(c.models)
	for vehicle, m := range c.models {
		c.discovery[vehicle] = publish.Discovery(c.b, vehicle, labels[vehicle], m)
	}
}

// connect starts an attempt to connect, unless one is under way, the connection is up
// (or paho opens it again), or the next attempt is not due.
func (c *conn) connect(now time.Time) {
	if c.connecting != nil {
		select {
		case <-c.connecting.Done():
		default:
			return
		}
		if c.connecting.Error() == nil {
			c.delay = 0
		} else {
			c.delay = min(max(2*c.delay, minRetry), maxRetry)
			c.retryAt = now.Add(c.delay)
		}
		c.connecting = nil
	}
	if c.client.IsConnected() || now.Before(c.retryAt) {
		return
	}
	c.connecting = c.client.Connect()
}

// notified records what paho tells of the connection, from its goroutines.
func (c *conn) notified(clk clock.Clock) paho.ConnectionNotificationHandler {
	return func(_ paho.Client, n paho.ConnectionNotification) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch n := n.(type) {
		case paho.ConnectionNotificationConnected:
			c.fresh = true
			c.status.ConnectedAt = clk.Now()
		case paho.ConnectionNotificationFailed:
			c.status.FailedAt, c.status.Failure = clk.Now(), failure(n.Reason)
		case paho.ConnectionNotificationLost:
			c.status.FailedAt, c.status.Failure = clk.Now(), publish.FailUnreachable
		default:
			return
		}
		c.dirty = true
	}
}

// failed records a failure to publish.
func (c *conn) failed(at time.Time, kind string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.FailedAt, c.status.Failure, c.dirty = at, kind, true
}

// flush sends what the broker lacks: on a new connection, the availability online, the
// entities' configurations and the whole state, else what changed; then the removals of
// the vehicles gone. It stops at the first message the broker does not acknowledge,
// which the next flush sends again.
func (p *Publisher) flush(c *conn) error {
	if !c.client.IsConnectionOpen() {
		return nil
	}
	c.mu.Lock()
	fresh := c.fresh
	c.fresh = false
	c.mu.Unlock()
	type pending struct {
		vehicle string // empty: the availability
		msg     publish.Message
		removal bool
	}
	var out []pending
	if fresh {
		clear(c.sent)
		out = append(out, pending{msg: publish.Availability(c.b.TopicPrefix, true)})
	}
	for _, vehicle := range sortedKeys(c.values) {
		// The configurations first: an entity created then reads the retained value.
		for _, m := range slices.Concat(c.discovery[vehicle], c.values[vehicle]) {
			if sent, ok := c.sent[vehicle][m.Topic]; !ok || sent != m.Payload {
				out = append(out, pending{vehicle: vehicle, msg: m})
			}
		}
	}
	for _, vehicle := range sortedKeys(c.removed) {
		for _, m := range c.removed[vehicle] {
			out = append(out, pending{vehicle: vehicle, msg: m, removal: true})
		}
	}
	tokens := make([]paho.Token, len(out))
	for i, o := range out {
		tokens[i] = c.client.Publish(o.msg.Topic, qos, o.msg.Retained, o.msg.Payload)
	}
	for i, tok := range tokens {
		kind := ""
		switch {
		case !tok.WaitTimeout(publishTimeout):
			kind = publish.FailUnreachable
		case tok.Error() != nil:
			kind = failure(tok.Error())
		}
		if kind != "" {
			if fresh && i == 0 {
				c.mu.Lock()
				c.fresh = true // the availability, then all again
				c.mu.Unlock()
			}
			c.failed(p.clk.Now(), kind)
			return fmt.Errorf("publish: %s", kind)
		}
		switch o := out[i]; {
		case o.removal:
			if i == len(out)-1 || out[i+1].vehicle != o.vehicle || !out[i+1].removal {
				delete(c.removed, o.vehicle) // its last removal acknowledged
			}
		case o.vehicle != "":
			if c.sent[o.vehicle] == nil {
				c.sent[o.vehicle] = map[string]string{}
			}
			c.sent[o.vehicle][o.msg.Topic] = o.msg.Payload
		}
	}
	return nil
}

// saveStatus writes the connection's status when it changed, and logs a change of it:
// a connection, or a kind of failure, never the broker's message.
func (p *Publisher) saveStatus(ctx context.Context, c *conn) {
	c.mu.Lock()
	st, dirty := c.status, c.dirty
	c.dirty = false
	c.mu.Unlock()
	if !dirty {
		return
	}
	state := "connected"
	if st.FailedAt.After(st.ConnectedAt) {
		state = st.Failure
	}
	if state != c.logged {
		c.logged = state
		if state == "connected" {
			p.log.Info("MQTT broker connected", "account", c.b.AccountID)
		} else {
			p.log.Warn("MQTT broker connection failed", "account", c.b.AccountID, "failure", state)
		}
	}
	if err := p.st.SaveMQTTStatus(ctx, c.b.AccountID, c.b.UpdatedAt, st); err != nil {
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
		if ctx.Err() == nil {
			p.log.Warn("save MQTT status", "account", c.b.AccountID, "err", err)
		}
	}
}

// dial opens the connection to the broker, in place of paho's (which would follow a
// proxy from the environment, around the address check): TCP, then TLS for mqtts.
func (p *Publisher) dial(u *url.URL, _ paho.ClientOptions) (net.Conn, error) {
	port := u.Port()
	switch u.Scheme {
	case "mqtt":
		if p.publicOnly { // credentials and positions do not cross the Internet in clear
			return nil, fmt.Errorf("%w: mqtt:// without TLS", errRefusedAddress)
		}
		if port == "" {
			port = plainPort
		}
	case "mqtts":
		if port == "" {
			port = tlsPort
		}
	default:
		return nil, fmt.Errorf("scheme %q", u.Scheme)
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	d := net.Dialer{Control: p.control}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	if u.Scheme == "mqtt" {
		return conn, nil
	}
	tc := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), RootCAs: p.rootCAs, MinVersion: tls.VersionTLS12})
	if err := tc.HandshakeContext(ctx); err != nil {
		// The broker answered TCP: a handshake that fails, even closed or silent, is a
		// certificate refused or a port that does not speak TLS (a plain MQTT broker
		// closes the connection on a ClientHello).
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %w", errTLS, err)
	}
	return tc, nil
}

// control refuses, where the instance accepts public brokers only, an address of a
// private network: checked on each address the name resolved to, as it is dialed, so
// that a name that resolves otherwise later is checked again.
func (p *Publisher) control(_, address string, _ syscall.RawConn) error {
	if !p.publicOnly {
		return nil
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !netguard.Public(ap.Addr()) {
		return errRefusedAddress
	}
	return nil
}

// failure is the kind of a failure to connect or to publish.
func failure(err error) string {
	switch {
	case errors.Is(err, errRefusedAddress):
		return publish.FailRefusedAddress
	case errors.Is(err, errTLS):
		return publish.FailTLS
	case errors.Is(err, packets.ErrorRefusedBadUsernameOrPassword), errors.Is(err, packets.ErrorRefusedNotAuthorised):
		return publish.FailNotAuthorized
	case errors.Is(err, packets.ErrorRefusedIDRejected), errors.Is(err, packets.ErrorRefusedServerUnavailable),
		errors.Is(err, packets.ErrorRefusedBadProtocolVersion), errors.Is(err, packets.ErrorProtocolViolation):
		return publish.FailRejected
	}
	return publish.FailUnreachable
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
