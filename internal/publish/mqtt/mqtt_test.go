package mqtt

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang/packets"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	mpackets "github.com/mochi-mqtt/server/v2/packets"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/publish"
)

const (
	account = "5f0c9c1e-8a2b-4d3c-b1e0-7a6f5e4d3c2b"
	vehicle = "0b7e6a52-3f1d-4c8e-9a51-1f2d3c4b5a69"
	second  = "1c8f7b63-4a2e-4d9f-8b62-2a3e4d5c6b7a"
	state   = "runsten/vehicles/" + vehicle + "/"
)

var (
	quiet = slog.New(slog.NewTextHandler(io.Discard, nil))
	t0    = time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
)

// testBroker is an embedded broker that records the messages it receives.
type testBroker struct {
	*mochi.Server
	addr string

	mu       sync.Mutex
	received []string // topic=payload, in order
}

// startBroker starts a broker on the loopback, over TLS with cfg, and with the users of
// ledger (nil: anyone).
func startBroker(t *testing.T, cfg *tls.Config, ledger *auth.Ledger) *testBroker {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if cfg != nil {
		ln = tls.NewListener(ln, cfg)
	}
	srv := mochi.New(&mochi.Options{InlineClient: true, Logger: quiet})
	if ledger == nil {
		err = srv.AddHook(new(auth.AllowHook), nil)
	} else {
		err = srv.AddHook(new(auth.Hook), &auth.Options{Ledger: ledger})
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.AddListener(listeners.NewNet("test", ln)); err != nil {
		t.Fatal(err)
	}
	if err := srv.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	b := &testBroker{Server: srv, addr: addr}
	if err := srv.Subscribe("runsten/#", 1, func(_ *mochi.Client, _ mpackets.Subscription, pk mpackets.Packet) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.received = append(b.received, pk.TopicName+"="+string(pk.Payload))
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

// retained returns the messages the broker keeps under runsten/: an empty one removed
// its topic's.
func (b *testBroker) retained() map[string]string { return b.retainedUnder("runsten/#") }

// retainedUnder returns the messages the broker keeps under filter.
func (b *testBroker) retainedUnder(filter string) map[string]string {
	m := map[string]string{}
	for _, pk := range b.Topics.Messages(filter) {
		m[pk.TopicName] = string(pk.Payload)
	}
	return m
}

// take returns the messages received since the previous take.
func (b *testBroker) take() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	got := b.received
	b.received = nil
	return got
}

// takeAtLeast waits for n messages since the previous take, and returns them all: the
// broker acknowledges a message before its subscribers get it, so a publication done is
// not yet a message received.
func (b *testBroker) takeAtLeast(t *testing.T, n int) []string {
	t.Helper()
	var got []string
	eventually(t, fmt.Sprintf("%d messages", n), func() bool {
		got = append(got, b.take()...)
		return len(got) >= n
	})
	return got
}

// memStore holds the accounts' brokers and the statuses written.
type memStore struct {
	mu       sync.Mutex
	brokers  map[string]publish.Broker
	status   map[string]publish.Status
	readErr  error
	reads    int
	writeErr error
}

func newStore(brokers ...publish.Broker) *memStore {
	s := &memStore{brokers: map[string]publish.Broker{}, status: map[string]publish.Status{}}
	for _, b := range brokers {
		s.brokers[b.AccountID] = b
	}
	return s
}

func (s *memStore) MQTTBrokers(context.Context) ([]publish.BrokerRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var refs []publish.BrokerRef
	for _, b := range s.brokers {
		refs = append(refs, publish.BrokerRef{AccountID: b.AccountID, UpdatedAt: b.UpdatedAt})
	}
	return refs, nil
}

func (s *memStore) PublishBroker(_ context.Context, accountID string) (publish.Broker, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.readErr != nil {
		return publish.Broker{}, false, s.readErr
	}
	b, ok := s.brokers[accountID]
	return b, ok, nil
}

// SaveMQTTStatus keeps the stored times a zero one leaves, as the store does.
func (s *memStore) SaveMQTTStatus(_ context.Context, accountID string, updatedAt time.Time, st publish.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	if b, ok := s.brokers[accountID]; !ok || !b.UpdatedAt.Equal(updatedAt) {
		return nil
	}
	old := s.status[accountID]
	if st.ConnectedAt.IsZero() {
		st.ConnectedAt = old.ConnectedAt
	}
	if st.FailedAt.IsZero() {
		st.FailedAt, st.Failure = old.FailedAt, old.Failure
	}
	s.status[accountID] = st
	return nil
}

func (s *memStore) statusOf() publish.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status[account]
}

func (s *memStore) set(b publish.Broker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.brokers[b.AccountID] = b
	delete(s.status, b.AccountID)
}

// states gives each vehicle's current state.
type states struct {
	current map[string]core.Current
	err     error
}

func (s *states) Current(_ context.Context, _, vehicleID string, _ time.Time) (core.Current, error) {
	return s.current[vehicleID], s.err
}

// models gives each vehicle's model; unknown by default.
type models map[string]publish.Model

func (m models) Model(_ context.Context, _, vehicleID string, _ core.Current) (publish.Model, error) {
	return m[vehicleID], nil
}

func currentWith(soc float64) core.Current {
	return core.Latest([]core.Record{{FetchedAt: t0, CheckedAt: t0, Snapshot: core.Snapshot{
		Covers: core.FieldSoC | core.FieldEngine | core.FieldPosition,
		SoC:    core.Some(soc, t0), Engine: core.Some(core.EngineStopped, t0),
		Position: core.Some(core.Position{Lat: 59.3293, Lon: 18.0686}, t0),
	}}})
}

func brokerAt(url string) publish.Broker {
	return publish.Broker{
		AccountID: account, URL: url, TopicPrefix: "runsten", Discovery: true, DiscoveryPrefix: "homeassistant",
		PublishLocation: true, UpdatedAt: t0,
	}
}

// eventually calls f until it holds, for a few seconds: paho connects in its goroutines.
func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !f() {
		select {
		case <-deadline:
			t.Fatalf("%s: not after 10 s", what)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// inAccount are the account's vehicles, unless a test removes one.
var inAccount = map[string][]string{account: {vehicle, second}}

// synced runs a Sync with the account's vehicles, which must not fail.
func synced(t *testing.T, p *Publisher, withheld map[string]bool) {
	t.Helper()
	syncedWith(t, p, inAccount, withheld)
}

func syncedWith(t *testing.T, p *Publisher, vehicles map[string][]string, withheld map[string]bool) {
	t.Helper()
	if err := p.Sync(context.Background(), vehicles, withheld); err != nil {
		t.Fatal(err)
	}
}

// connected syncs p until its connection to account's broker is told connected.
func connected(t *testing.T, p *Publisher, st *memStore) {
	t.Helper()
	eventually(t, "connected", func() bool {
		synced(t, p, nil)
		s := st.statusOf()
		return !s.ConnectedAt.IsZero() && !s.FailedAt.After(s.ConnectedAt)
	})
}

// TestPublish: connected, the publisher says it is online, with its testament, then the
// vehicle's state, retained, an unknown value removed; then only what changes; a
// connection lost and opened again gets everything again; closed, it says offline.
func TestPublish(t *testing.T) {
	b := startBroker(t, nil, nil)
	st := newStore(brokerAt("mqtt://" + b.addr))
	sts := &states{current: map[string]core.Current{vehicle: currentWith(80)}}
	p := New(st, sts, models{}, clock.NewManual(t0), false, quiet)
	defer p.Close()

	// Nothing sent before the connection: the state waits for it.
	if err := p.Publish(context.Background(), account, vehicle); err != nil {
		t.Fatal(err)
	}
	connected(t, p, st)
	if err := p.Publish(context.Background(), account, vehicle); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"runsten/status": "online", state + "battery_level": "80", state + "engine": "stopped",
		state + "location": `{"latitude":59.3293,"longitude":18.0686}`, state + "read_at": "2026-10-04T08:00:00Z",
	}
	eventually(t, "state retained", func() bool { return equal(b.retained(), want) })
	cl, ok := b.Clients.Get("runsten-5f0c9c1e8a2b4d3")
	if !ok {
		t.Fatal("no client with the account's client ID")
	}
	if w := cl.Properties.Will; w.TopicName != "runsten/status" || string(w.Payload) != "offline" || !w.Retain || w.Qos != 1 {
		t.Errorf("testament = %+v", w)
	}
	if n := len(b.takeAtLeast(t, 12)); n != 12 { // online, and the 11 values, the unknown ones empty
		t.Errorf("%d messages on connection, want 12", n)
	}

	// Only the value that changed.
	sts.current[vehicle] = currentWith(81)
	if err := p.Publish(context.Background(), account, vehicle); err != nil {
		t.Fatal(err)
	}
	if got := b.takeAtLeast(t, 1); len(got) != 1 || got[0] != state+"battery_level=81" {
		t.Errorf("after a change: %v, want battery_level alone", got)
	}
	if err := p.Publish(context.Background(), account, vehicle); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := b.take(); len(got) != 0 {
		t.Errorf("unchanged state published again: %v", got)
	}

	// The connection lost: paho opens it again, and the next call sends everything.
	connectedAt := st.statusOf().ConnectedAt
	cl.Stop(errors.New("network down"))
	var again []string
	eventually(t, "reconnected", func() bool {
		synced(t, p, nil)
		again = append(again, b.take()...)
		return len(again) >= 12
	})
	if s := st.statusOf(); s.Failure != publish.FailUnreachable || s.ConnectedAt.Before(connectedAt) {
		t.Errorf("status after a loss = %+v", s)
	}

	p.Close()
	eventually(t, "offline", func() bool { return b.retained()["runsten/status"] == "offline" })
}

const batteryConfig = "homeassistant/sensor/runsten_" + vehicle + "/battery_level/config"

var ex30 = publish.Model{Variant: "EX30 Extended Range", Family: "EX30", Brand: "Volvo", Year: 2024}

// deviceName reads the device's name from an entity's configuration.
func deviceName(t *testing.T, payload string) string {
	t.Helper()
	var cfg struct {
		Device struct {
			Name string `json:"name"`
		} `json:"device"`
	}
	if err := json.Unmarshal([]byte(payload), &cfg); err != nil {
		t.Fatalf("configuration %q: %v", payload, err)
	}
	return cfg.Device.Name
}

// TestPublishDiscovery: each vehicle's entities are configured, under the discovery
// prefix, two alike telling themselves apart; a vehicle gone from the account has its
// entities and its values removed, and the other's name comes back.
func TestPublishDiscovery(t *testing.T) {
	b := startBroker(t, nil, nil)
	st := newStore(brokerAt("mqtt://" + b.addr))
	sts := &states{current: map[string]core.Current{vehicle: currentWith(80), second: currentWith(55)}}
	p := New(st, sts, models{vehicle: ex30, second: ex30}, clock.NewManual(t0), false, quiet)
	defer p.Close()
	connected(t, p, st)
	if err := p.Publish(context.Background(), account, vehicle); err != nil {
		t.Fatal(err)
	}
	eventually(t, "configured", func() bool { return b.retainedUnder("homeassistant/#")[batteryConfig] != "" })
	if got := deviceName(t, b.retainedUnder("homeassistant/#")[batteryConfig]); got != "EX30 Extended Range · 2024" {
		t.Errorf("device = %q", got)
	}
	if n := len(b.retainedUnder("homeassistant/#")); n != 12 {
		t.Errorf("%d configurations, want 12", n)
	}

	if err := p.Publish(context.Background(), account, second); err != nil {
		t.Fatal(err)
	}
	eventually(t, "two alike", func() bool {
		return deviceName(t, b.retainedUnder("homeassistant/#")[batteryConfig]) == "EX30 Extended Range · 2024 · 0b7e6a"
	})

	syncedWith(t, p, map[string][]string{account: {second}}, nil)
	eventually(t, "vehicle removed", func() bool {
		for topic := range b.retainedUnder("#") {
			if strings.Contains(topic, vehicle) {
				return false
			}
		}
		return true
	})
	secondConfig := strings.ReplaceAll(batteryConfig, vehicle, second)
	if err := p.Publish(context.Background(), account, second); err != nil {
		t.Fatal(err)
	}
	eventually(t, "its own name again", func() bool {
		return deviceName(t, b.retainedUnder("homeassistant/#")[secondConfig]) == "EX30 Extended Range · 2024"
	})
	if len(p.conns[account].removed) != 0 {
		t.Errorf("removals left: %v", p.conns[account].removed)
	}
}

// TestPublishConfiguration: as a connection closes, it removes from its broker what
// the next configuration will not replace; withheld, it only says offline.
func TestPublishConfiguration(t *testing.T) {
	other := startBroker(t, nil, nil)
	for _, tt := range []struct {
		name string
		// change changes the configuration (nil: removed), or withholds the account.
		change   func(b publish.Broker) *publish.Broker
		withheld bool
		// what stays on the first broker: the configuration, the vehicle's battery level,
		// the availability.
		config, battery, status string
	}{
		{name: "withheld", withheld: true, config: "kept", battery: "80", status: "offline"},
		{name: "removed", config: "", battery: "", status: ""},
		{
			name:   "another broker",
			change: func(b publish.Broker) *publish.Broker { b.URL = "mqtt://" + other.addr; return &b },
			config: "", battery: "", status: "",
		},
		{
			name:   "another topic prefix",
			change: func(b publish.Broker) *publish.Broker { b.TopicPrefix = "cars"; return &b },
			config: "replaced", battery: "", status: "",
		},
		{
			name:   "discovery off",
			change: func(b publish.Broker) *publish.Broker { b.Discovery = false; return &b },
			config: "", battery: "80", status: "online",
		},
		{
			name:   "password changed",
			change: func(b publish.Broker) *publish.Broker { b.Password = "new"; return &b },
			config: "kept", battery: "80", status: "online",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := startBroker(t, nil, nil)
			first := brokerAt("mqtt://" + b.addr)
			st := newStore(first)
			p := New(st, &states{current: map[string]core.Current{vehicle: currentWith(80)}}, models{vehicle: ex30},
				clock.NewManual(t0), false, quiet)
			defer p.Close()
			connected(t, p, st)
			if err := p.Publish(context.Background(), account, vehicle); err != nil {
				t.Fatal(err)
			}
			eventually(t, "published", func() bool { return b.retainedUnder("homeassistant/#")[batteryConfig] != "" })
			before := b.retainedUnder("homeassistant/#")[batteryConfig]

			switch {
			case tt.withheld:
			case tt.change == nil:
				st.mu.Lock()
				delete(st.brokers, account)
				st.mu.Unlock()
			default:
				next := tt.change(first)
				next.UpdatedAt = t0.Add(time.Hour)
				st.set(*next)
			}
			synced(t, p, map[string]bool{account: tt.withheld})
			if tt.change != nil {
				connected(t, p, st)
				if err := p.Publish(context.Background(), account, vehicle); err != nil {
					t.Fatal(err)
				}
			}
			eventually(t, "the first broker as it should be", func() bool {
				r, cfg := b.retained(), b.retainedUnder("homeassistant/#")[batteryConfig]
				switch tt.config {
				case "kept":
					if cfg != before {
						return false
					}
				case "replaced":
					if cfg == "" || cfg == before {
						return false
					}
				default:
					if cfg != "" {
						return false
					}
				}
				return r[state+"battery_level"] == tt.battery && r["runsten/status"] == tt.status
			})
		})
	}
}

// TestPublishFailures: each failure to connect is told by its kind, and the next attempt
// waits a growing delay.
func TestPublishFailures(t *testing.T) {
	cert, cfg := selfSigned(t)
	ledger := &auth.Ledger{Auth: auth.AuthRules{{Username: "runsten", Password: "right", Allow: true}}}
	plain := startBroker(t, nil, ledger)
	secure := startBroker(t, cfg, nil)
	closed, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := closed.Addr().String()
	_ = closed.Close()
	trusted := x509.NewCertPool()
	trusted.AddCert(cert)

	tests := []struct {
		name       string
		url        string
		username   string
		password   string
		publicOnly bool
		roots      *x509.CertPool
		want       string // empty: connected
	}{
		{name: "credentials", url: "mqtt://" + plain.addr, username: "runsten", password: "right"},
		{name: "wrong password", url: "mqtt://" + plain.addr, username: "runsten", password: "wrong", want: publish.FailNotAuthorized},
		{name: "unreachable", url: "mqtt://" + closedAddr, want: publish.FailUnreachable},
		{name: "unknown host", url: "mqtt://broker.invalid", want: publish.FailUnreachable},
		{name: "TLS trusted", url: "mqtts://" + secure.addr, roots: trusted},
		{name: "TLS untrusted", url: "mqtts://" + secure.addr, want: publish.FailTLS},
		{name: "TLS to a plain broker", url: "mqtts://" + plain.addr, roots: trusted, want: publish.FailTLS},
		{name: "public only: loopback", url: "mqtts://" + secure.addr, roots: trusted, publicOnly: true, want: publish.FailRefusedAddress},
		{name: "public only: localhost", url: "mqtts://localhost:" + port(secure.addr), roots: trusted, publicOnly: true, want: publish.FailRefusedAddress},
		{name: "public only: no TLS", url: "mqtt://" + plain.addr, publicOnly: true, want: publish.FailRefusedAddress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := brokerAt(tt.url)
			b.Username, b.Password = tt.username, tt.password
			st := newStore(b)
			clk := clock.NewManual(t0)
			p := New(st, &states{}, models{}, clk, tt.publicOnly, quiet)
			p.rootCAs = tt.roots
			defer p.Close()
			if tt.want == "" {
				connected(t, p, st)
				return
			}
			eventually(t, "failure "+tt.want, func() bool {
				synced(t, p, nil)
				s := st.statusOf()
				return s.Failure == tt.want && s.ConnectedAt.IsZero()
			})
			c := p.conns[account]
			eventually(t, "attempt over", func() bool { synced(t, p, nil); return c.connecting == nil })
			if c.delay != minRetry || !c.retryAt.Equal(t0.Add(minRetry)) {
				t.Errorf("delay %v, retry at %v", c.delay, c.retryAt)
			}
			clk.Advance(minRetry)
			synced(t, p, nil) // a second attempt
			eventually(t, "second attempt over", func() bool { synced(t, p, nil); return c.connecting == nil })
			if c.delay != 2*minRetry {
				t.Errorf("delay after two failures: %v", c.delay)
			}
		})
	}
}

// TestPublishErrors: a configuration that cannot be read is logged once, not read again
// until it changes; a state that cannot be read is an error; the status that could not
// be written is written again.
func TestPublishErrors(t *testing.T) {
	b := startBroker(t, nil, nil)
	st := newStore(brokerAt("mqtt://" + b.addr))
	st.readErr = errors.New("message authentication failed")
	sts := &states{err: errors.New("database down")}
	p := New(st, sts, models{}, clock.NewManual(t0), false, quiet)
	defer p.Close()
	synced(t, p, nil)
	synced(t, p, nil)
	if st.reads != 1 || len(p.conns) != 0 {
		t.Errorf("%d reads, %d connections", st.reads, len(p.conns))
	}
	st.readErr = nil
	changed := brokerAt("mqtt://" + b.addr)
	changed.UpdatedAt = t0.Add(time.Minute)
	st.writeErr = errors.New("database down")
	st.set(changed)
	eventually(t, "connected", func() bool { synced(t, p, nil); return p.conns[account].client.IsConnectionOpen() })
	st.mu.Lock()
	st.writeErr = nil
	st.mu.Unlock()
	connected(t, p, st)
	if err := p.Publish(context.Background(), account, vehicle); err == nil || !strings.Contains(err.Error(), vehicle) {
		t.Errorf("publish with a state not read: %v", err)
	}
	if err := p.Publish(context.Background(), "another", vehicle); err != nil {
		t.Errorf("publish for an account without a broker: %v", err)
	}
}

func TestFailure(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w : %w", packets.ErrorNetworkError, &net.OpError{Op: "dial", Err: errRefusedAddress}), publish.FailRefusedAddress},
		{fmt.Errorf("%w : %w", packets.ErrorNetworkError, fmt.Errorf("%w: x509", errTLS)), publish.FailTLS},
		{packets.ErrorRefusedBadUsernameOrPassword, publish.FailNotAuthorized},
		{packets.ErrorRefusedNotAuthorised, publish.FailNotAuthorized},
		{packets.ErrorRefusedIDRejected, publish.FailRejected},
		{packets.ErrorRefusedServerUnavailable, publish.FailRejected},
		{packets.ErrorRefusedBadProtocolVersion, publish.FailRejected},
		{fmt.Errorf("%w : %w", packets.ErrorNetworkError, errors.New("connection refused")), publish.FailUnreachable},
	} {
		if got := failure(tt.err); got != tt.want {
			t.Errorf("failure(%v) = %s, want %s", tt.err, got, tt.want)
		}
	}
}

func TestControl(t *testing.T) {
	open := &Publisher{}
	guarded := &Publisher{publicOnly: true}
	for _, tt := range []struct {
		addr       string
		guardedErr bool
	}{
		{"1.1.1.1:8883", false},
		{"[2606:4700:4700::1111]:8883", false},
		{"127.0.0.1:1883", true},
		{"10.0.0.5:1883", true},
		{"[::1]:1883", true},
		{"169.254.169.254:80", true},
		{"not an address", true},
	} {
		if err := open.control("tcp", tt.addr, nil); err != nil {
			t.Errorf("private brokers allowed: %s refused", tt.addr)
		}
		if err := guarded.control("tcp", tt.addr, nil); (err != nil) != tt.guardedErr {
			t.Errorf("public only: %s: %v", tt.addr, err)
		}
	}
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func port(addr string) string {
	_, p, _ := net.SplitHostPort(addr)
	return p
}

// selfSigned returns a certificate for 127.0.0.1 and localhost, and the TLS
// configuration of a server presenting it.
func selfSigned(t *testing.T) (*x509.Certificate, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "broker"},
		NotBefore:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		IPAddresses: []net.IP{netip.MustParseAddr("127.0.0.1").AsSlice(), net.IPv6loopback},
		DNSNames:    []string{"localhost"},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return parsed, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
}
