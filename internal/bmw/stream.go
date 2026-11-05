package bmw

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// The stream's MQTT settings, as the free clients use them: QoS 1, a clean session, a
// keepalive of 30 s (BMW's streaming guide, Home Assistant's integration).
const (
	streamQoS       = 1
	streamKeepAlive = 30 * time.Second
	streamTimeout   = 10 * time.Second
	// takeoverWait is how long a connection refused as a second one waits before trying
	// again, right after closing its own: the broker may not have released its slot yet.
	takeoverWait = 2 * time.Second
)

// The CONNACK codes of MQTT 3.1.1 BMW's broker refuses with.
const (
	// RefusedBadPassword: the ID token is refused, or expired.
	RefusedBadPassword byte = 4
	// RefusedNotAuthorized: another client holds the account's only connection (Home
	// Assistant, evcc), or the token lacks the stream's scope.
	RefusedNotAuthorized byte = 5
)

// StreamError is a connection the broker refused.
type StreamError struct {
	Code byte // the CONNACK's: RefusedBadPassword, RefusedNotAuthorized
	Err  error
}

func (e *StreamError) Error() string { return fmt.Sprintf("stream: refused (%d): %v", e.Code, e.Err) }

func (e *StreamError) Unwrap() error { return e.Err }

// Subscription is a subscription to the stream of an account, <GCID>/+: every vehicle of it. It
// keeps, by VIN, the last value the car pushed of each descriptor. It connects only when
// asked (Connect): the ID token it connects with lasts an hour, after which BMW closes the
// connection, and the caller refreshes it.
type Subscription struct {
	url  string
	gcid string
	tls  *tls.Config

	mu       sync.Mutex
	client   paho.Client
	password string // the ID token of the connection, when up
	up       bool
	lost     error // why the last connection ended; nil while up
	values   map[string]map[string]datum
}

// datum is a descriptor's last value, as the car pushed it.
type datum struct {
	Value     json.RawMessage `json:"value"`
	Unit      string          `json:"unit,omitempty"`
	Timestamp string          `json:"timestamp"`
}

// NewSubscription returns the subscription of the account gcid to the stream at url
// (DefaultStreamURL; tcp:// for the simulator). A tls:// URL is held to TLS 1.3, which
// BMW's broker asks for.
func NewSubscription(url, gcid string) *Subscription {
	return &Subscription{
		url: url, gcid: gcid, values: map[string]map[string]datum{},
		tls: &tls.Config{MinVersion: tls.VersionTLS13},
	}
}

// GCID is the account the stream reads.
func (s *Subscription) GCID() string { return s.gcid }

// Up reports whether the connection is up, and else why it ended (nil before any).
func (s *Subscription) Up() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.up, s.lost
}

// Connect connects with the ID token idToken, and subscribes to the account's topics,
// closing the connection of an older token first: BMW allows one at a time. Connected
// with the same token already, it does nothing. A refusal is a *StreamError.
func (s *Subscription) Connect(ctx context.Context, idToken string) error {
	s.mu.Lock()
	if s.up && s.password == idToken {
		s.mu.Unlock()
		return nil
	}
	old := s.client
	s.client, s.up, s.password = nil, false, ""
	s.mu.Unlock()
	if old != nil {
		old.Disconnect(250)
	}
	err := s.connect(ctx, idToken)
	var se *StreamError
	if old != nil && errors.As(err, &se) && se.Code == RefusedNotAuthorized {
		// Our own connection may still hold the slot.
		t := time.NewTimer(takeoverWait)
		select {
		case <-ctx.Done():
			t.Stop()
			return fmt.Errorf("stream: %w", ctx.Err())
		case <-t.C:
		}
		err = s.connect(ctx, idToken)
	}
	return err
}

func (s *Subscription) connect(ctx context.Context, idToken string) error {
	opts := paho.NewClientOptions().
		AddBroker(s.url).
		// Assumed: BMW names no client identifier; the GCID is one only this account
		// uses, and the broker holds one connection per account anyway.
		SetClientID(s.gcid).
		SetUsername(s.gcid).
		SetPassword(idToken).
		SetProtocolVersion(4). // 3.1.1, explicit: paho would try 3.1 after a refusal
		SetCleanSession(true).
		SetKeepAlive(streamKeepAlive).
		SetConnectTimeout(streamTimeout).
		SetAutoReconnect(false). // the caller reconnects, with a token that may be new
		SetConnectRetry(false).
		SetOrderMatters(false).
		SetTLSConfig(s.tls)
	var cl paho.Client
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.client == cl {
			s.up, s.lost = false, err
		}
	})
	cl = paho.NewClient(opts)
	tok := cl.Connect()
	if err := wait(ctx, tok); err != nil {
		// Given up on (a timeout, ctx done), the attempt may still succeed: closed once it
		// ends, or it would hold the account's only connection.
		go func() {
			<-tok.Done()
			cl.Disconnect(250)
		}()
		var code byte
		if ct, ok := tok.(*paho.ConnectToken); ok {
			code = ct.ReturnCode()
		}
		if code == RefusedBadPassword || code == RefusedNotAuthorized {
			return s.fail(&StreamError{Code: code, Err: err})
		}
		return s.fail(fmt.Errorf("stream: connect: %w", err))
	}
	s.mu.Lock()
	s.client, s.up, s.password, s.lost = cl, true, idToken, nil
	s.mu.Unlock()
	sub := cl.Subscribe(s.gcid+"/+", streamQoS, s.received)
	if err := wait(ctx, sub); err != nil {
		cl.Disconnect(0)
		return s.fail(fmt.Errorf("stream: subscribe: %w", err))
	}
	if st, ok := sub.(*paho.SubscribeToken); ok {
		for _, code := range st.Result() {
			if code >= 0x80 {
				cl.Disconnect(0)
				return s.fail(&StreamError{Code: RefusedNotAuthorized, Err: errors.New("subscription refused")})
			}
		}
	}
	return nil
}

// fail records a connection that did not come up, and returns err.
func (s *Subscription) fail(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client, s.up, s.password, s.lost = nil, false, "", err
	return err
}

// wait waits for tok, at most streamTimeout and until ctx is done.
func wait(ctx context.Context, tok paho.Token) error {
	t := time.NewTimer(streamTimeout)
	defer t.Stop()
	select {
	case <-tok.Done():
		return tok.Error() //nolint:wrapcheck // paho's, wrapped by the callers
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // wrapped by the callers
	case <-t.C:
		return errors.New("timed out")
	}
}

// Close closes the connection.
func (s *Subscription) Close() {
	s.mu.Lock()
	cl := s.client
	s.client, s.up, s.password = nil, false, ""
	s.mu.Unlock()
	if cl != nil {
		cl.Disconnect(250)
	}
}

// message is what the stream pushes: some descriptors of a vehicle, each with the time
// the car recorded it.
type message struct {
	VIN  string           `json:"vin"`
	Data map[string]datum `json:"data"`
}

// received merges a message into the values of its vehicle. A message that does not
// read is dropped: the next carries the values again when they change.
func (s *Subscription) received(_ paho.Client, m paho.Message) {
	var msg message
	if json.Unmarshal(m.Payload(), &msg) != nil {
		return
	}
	vin := msg.VIN
	if vin == "" {
		vin = strings.TrimPrefix(m.Topic(), s.gcid+"/")
	}
	if vin == "" || strings.Contains(vin, "/") {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vals := s.values[vin]
	if vals == nil {
		vals = map[string]datum{}
		s.values[vin] = vals
	}
	for k, d := range msg.Data {
		if len(d.Value) == 0 || d.Timestamp == "" {
			continue
		}
		if old, ok := vals[k]; ok && stampOf(old.Timestamp).After(stampOf(d.Timestamp)) {
			continue // an older value is not news
		}
		vals[k] = d
	}
}

// Values returns what the stream pushed of the vehicle, its last value of each
// descriptor, as a TelematicData response: {"telematicData": {descriptor: {value, unit,
// timestamp}}}. It is empty until the car pushed something.
func (s *Subscription) Values(vin string) []byte {
	s.mu.Lock()
	vals := make(map[string]datum, len(s.values[vin]))
	for k, d := range s.values[vin] {
		vals[k] = d
	}
	s.mu.Unlock()
	raw, err := json.Marshal(map[string]any{"telematicData": vals}) // sorted keys
	if err != nil {
		return []byte(`{"telematicData":{}}`) // only valid JSON values
	}
	return raw
}
