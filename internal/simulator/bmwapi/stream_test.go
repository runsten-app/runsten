package bmwapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// startStream serves the server's stream on the loopback, stepped by the test.
func (s *server) startStream(t *testing.T) string {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	st, err := NewStream(s.h, ln, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = st.Run(ctx, time.Hour) // stepped by the test
	}()
	t.Cleanup(func() { cancel(); <-done })
	s.stream = st
	return "tcp://" + ln.Addr().String()
}

// subscriber is a client of the stream, which collects the messages it receives.
type subscriber struct {
	paho.Client
	msgs chan map[string]any
	lost chan error
}

func connect(t *testing.T, addr, user, password string) (*subscriber, error) {
	t.Helper()
	sub := &subscriber{msgs: make(chan map[string]any, 16), lost: make(chan error, 1)}
	opts := paho.NewClientOptions().AddBroker(addr).SetClientID("test-" + nonce()[:6]).
		SetUsername(user).SetPassword(password).SetProtocolVersion(4).SetCleanSession(true).
		SetAutoReconnect(false).SetConnectRetry(false).
		SetConnectionLostHandler(func(_ paho.Client, err error) { sub.lost <- err })
	sub.Client = paho.NewClient(opts)
	tok := sub.Connect()
	if !tok.WaitTimeout(5 * time.Second) {
		t.Fatal("connect timed out")
	}
	if err := tok.Error(); err != nil {
		return nil, err
	}
	t.Cleanup(func() { sub.Disconnect(0) })
	return sub, nil
}

func (sub *subscriber) subscribe(t *testing.T, topic string) error {
	t.Helper()
	tok := sub.Subscribe(topic, 1, func(_ paho.Client, m paho.Message) {
		var body map[string]any
		if err := json.Unmarshal(m.Payload(), &body); err == nil {
			body["_topic"] = m.Topic()
			sub.msgs <- body
		}
	})
	tok.WaitTimeout(5 * time.Second)
	return tok.Error()
}

func (sub *subscriber) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case m := <-sub.msgs:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
		return nil
	}
}

func TestStream(t *testing.T) {
	s := newServer(t)
	addr := s.startStream(t)
	tokens := s.signIn(t, allScopes)
	id := tokens["id_token"].(string)

	if _, err := connect(t, addr, GCID, "not-a-token"); err == nil || !strings.Contains(err.Error(), "user name or password") {
		t.Errorf("a bad password: %v", err)
	}
	sub, err := connect(t, addr, GCID, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connect(t, addr, GCID, id); err == nil || !strings.Contains(err.Error(), "not Authorized") {
		t.Errorf("a second connection: %v", err)
	}
	if err := sub.subscribe(t, GCID+"/+"); err != nil {
		t.Fatal(err)
	}

	// On subscribing, the current values.
	first := sub.next(t)
	data := first["data"].(map[string]any)
	if first["_topic"] != GCID+"/"+vin || first["vin"] != vin || len(data) != len(Descriptors()) {
		t.Fatalf("first message: %v", first)
	}
	if soc := data[DescSoC].(map[string]any); soc["value"] != 64.2 || soc["unit"] != "%" || soc["timestamp"] != "2026-09-28T06:00:10.000Z" {
		t.Errorf("SoC: %v", soc)
	}

	// Then what changes.
	s.clk.Advance(time.Minute)
	s.src.st.PluggedIn = true
	s.stream.Step()
	change := sub.next(t)["data"].(map[string]any)
	if len(change) != 2 || change[DescPortStatus].(map[string]any)["value"] != "CONNECTED" {
		t.Errorf("plugged in: %v", change)
	}
	if hv := change[DescHVStatus].(map[string]any); hv["value"] != "WAITING_FOR_CHARGING" || hv["timestamp"] != "2026-09-28T06:01:10.000Z" {
		t.Errorf("hv status: %v", hv)
	}
	s.stream.Step()
	select {
	case m := <-sub.msgs:
		t.Errorf("nothing changed, sent %v", m)
	case <-time.After(100 * time.Millisecond):
	}

	// The ID token's expiry closes the connection, before what changed meanwhile is sent;
	// a fresh one connects again.
	s.clk.Advance(time.Hour)
	s.src.st.PluggedIn = false
	s.stream.Step()
	select {
	case <-sub.lost:
	case m := <-sub.msgs:
		t.Fatalf("sent past the ID token's expiry: %v", m)
	case <-time.After(5 * time.Second):
		t.Fatal("still connected after the ID token expired")
	}
	if _, err := connect(t, addr, GCID, id); err == nil {
		t.Error("connected with an expired ID token")
	}
	_, fresh := s.refresh(t, tokens["refresh_token"].(string))
	if _, err := connect(t, addr, GCID, fresh["id_token"].(string)); err != nil {
		t.Errorf("a fresh ID token: %v", err)
	}
}

func TestStreamRefusals(t *testing.T) {
	s := newServer(t)
	addr := s.startStream(t)
	apiOnly := s.signIn(t, "openid "+ScopeAPI)["id_token"].(string)
	if _, err := connect(t, addr, GCID, apiOnly); err == nil {
		t.Error("connected without the stream's scope")
	}
	id := s.signIn(t, allScopes)["id_token"].(string)
	if _, err := connect(t, addr, "another-gcid", id); err == nil {
		t.Error("connected as another account")
	}
	sub, err := connect(t, addr, GCID, id)
	if err != nil {
		t.Fatal(err)
	}
	// MQTT 3.1.1 refuses a subscription in its SUBACK: nothing comes.
	for _, topic := range []string{"another-gcid/+", GCID + "/#", GCID + "/WBY00000000000000"} {
		if err := sub.subscribe(t, topic); err == nil {
			select {
			case m := <-sub.msgs:
				t.Errorf("%s received %v", topic, m)
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	if err := sub.subscribe(t, GCID+"/"+vin); err != nil {
		t.Fatal(err)
	}
	if m := sub.next(t); m["vin"] != vin {
		t.Errorf("the vehicle's topic: %v", m)
	}
}

func TestStreamDrop(t *testing.T) {
	s := newServer(t)
	addr := s.startStream(t)
	id := s.signIn(t, allScopes)["id_token"].(string)
	sub, err := connect(t, addr, GCID, id)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := s.call(t, "POST", ControlPrefix+"stream/drop", "", nil, nil); status != 204 {
		t.Fatal(status)
	}
	select {
	case <-sub.lost:
	case <-time.After(5 * time.Second):
		t.Fatal("not dropped")
	}
	if _, err := connect(t, addr, GCID, id); err != nil {
		t.Errorf("reconnect after the drop: %v", err)
	}
}
