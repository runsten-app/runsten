package bmwsource

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"runsten/internal/bmw"
	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/bmwapi"
	"runsten/internal/simulator/vehicle"
)

const vin = "WBY31AW02RFR00001"

// waitTries bounds a wait for what the stream delivers, 10 ms apart: 5 s.
const waitTries = 500

var setAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

type car struct {
	mu sync.Mutex
	st vehicle.State
}

func (*car) VIN() string   { return vin }
func (*car) Brand() string { return "bmw" }
func (*car) ReportAt(time.Time) vehicle.Report {
	return vehicle.Report{Spec: vehicle.Spec{Model: "i4 eDrive40", ModelYear: 2024, BatteryKWh: 83.9, UsableKWh: 80.7, ConsumptionKWhPer100km: 16}}
}

func (c *car) StateAt(time.Time) vehicle.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

func (c *car) set(f func(*vehicle.State)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f(&c.st)
}

// store keeps one connection's credential.
type store struct {
	mu      sync.Mutex
	cred    Credential
	rotated int
}

func (s *store) BMWCredential(context.Context, string, string) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cred, nil
}

func (s *store) RotateBMWToken(_ context.Context, _, _ string, at time.Time, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at.Equal(s.cred.SetAt) {
		s.cred.Refresh = token
		s.rotated++
	}
	return nil
}

func (s *store) SetBMWContainer(_ context.Context, _, _ string, at time.Time, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at.Equal(s.cred.SetAt) {
		s.cred.ContainerID = id
	}
	return nil
}

type fixture struct {
	src    *Source
	st     *store
	clk    *clock.Manual
	car    *car
	srv    *httptest.Server
	stream *bmwapi.Stream
	addr   string
	client *bmw.Client
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	c := &car{st: vehicle.State{SoC: 64.2, OdometerKm: 21034.6, TargetSoC: 80, Position: vehicle.Position{Lat: 50.827, Lon: 4.372}}}
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	h := bmwapi.NewHandler([]bmwapi.Source{c}, clk, bmwapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := bmwapi.NewStream(h, ln, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = stream.Run(ctx, time.Hour) // stepped by the test
	}()
	t.Cleanup(func() { cancel(); <-done })

	client := bmw.NewClient(bmw.Hosts{Auth: srv.URL + "/gcdm/oauth", API: srv.URL}, srv.Client())
	f := &fixture{clk: clk, car: c, srv: srv, stream: stream, addr: "tcp://" + ln.Addr().String(), client: client}
	tok := f.deviceFlow(t)
	f.st = &store{cred: Credential{ClientID: bmwapi.DevClientID, GCID: tok.GCID, Refresh: tok.Refresh, SetAt: setAt}}
	f.src = New(client, f.addr, f.st, clk)
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f.src.Run(ctx) // closes the subscriptions
	})
	return f
}

// deviceFlow runs the owner's device flow, as runsten-api will.
func (f *fixture) deviceFlow(t *testing.T) bmw.Tokens {
	t.Helper()
	verifier, challenge := bmw.NewVerifier()
	d, err := f.client.DeviceCode(t.Context(), bmwapi.DevClientID, challenge)
	if err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(15 * time.Second)
	tok, err := f.client.PollToken(t.Context(), bmwapi.DevClientID, d.DeviceCode, verifier)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (f *fixture) control(t *testing.T, path string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+bmwapi.ControlPrefix+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := f.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func (f *fixture) login(t *testing.T) collector.Session {
	t.Helper()
	sess, ttl, err := f.src.Login(t.Context(), "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if ttl != time.Hour-accessMargin {
		t.Errorf("access for %v", ttl)
	}
	return sess
}

// streamed reads the stream until the snapshot satisfies ok.
func streamed(t *testing.T, sess collector.Session, ok func(core.Snapshot) bool) core.Snapshot {
	t.Helper()
	for i := 0; ; i++ {
		raw, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.Stream))
		if err != nil {
			t.Fatal(err)
		}
		s, err := bmw.Normalize(bmw.Stream, raw)
		if err != nil {
			t.Fatal(err)
		}
		if ok(s) {
			return s
		}
		if i == waitTries {
			t.Fatalf("stream: %s", raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func kindOf(err error) collector.ErrorKind {
	var se *collector.SourceError
	if !errors.As(err, &se) {
		return -1
	}
	return se.Kind
}

func TestStreamAndAPI(t *testing.T) {
	f := newFixture(t)
	first := f.st.cred.Refresh
	sess := f.login(t)
	if f.st.cred.Refresh == first || f.st.rotated != 1 {
		t.Errorf("refresh token not rotated: %d", f.st.rotated)
	}

	// The stream: its current values on subscribing, then what changes.
	s := streamed(t, sess, func(s core.Snapshot) bool { return s.SoC.OK })
	if s.SoC.V != 64.2 || s.Engine.V != core.EngineStopped {
		t.Errorf("first values: %+v", s)
	}
	f.clk.Advance(time.Minute)
	f.car.set(func(st *vehicle.State) {
		st.PluggedIn, st.Charging, st.ChargeType, st.ChargingPowerW = true, vehicle.Charging, vehicle.AC, 7400
	})
	f.stream.Step()
	s = streamed(t, sess, func(s core.Snapshot) bool { return s.Charging.V == core.ChargingActive })
	var a collector.Activity
	raw, _ := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.Stream))
	if err := f.src.Observe(collector.Endpoint(bmw.Stream), raw, &a); err != nil || !a.Charges || a.Driving {
		t.Errorf("activity %+v %v", a, err)
	}
	if st := f.src.StreamState("c"); !st.Up || st.Held {
		t.Errorf("stream state %+v", st)
	}

	// The container, created at the first read and kept.
	raw, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.TelematicData))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := bmw.Normalize(bmw.TelematicData, raw); s.PowerW.V != 7400 {
		t.Errorf("container's values: %s", raw)
	}
	if f.st.cred.ContainerID == "" {
		t.Error("container not kept")
	}
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.BasicData)); err != nil {
		t.Error(err)
	}
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.ChargingHistory)); err != nil {
		t.Error(err)
	}
	if _, err := sess.Fetch(t.Context(), "WBY31AW02RFR99999", collector.Endpoint(bmw.BasicData)); kindOf(err) != collector.KindUnshared {
		t.Errorf("another VIN: %v", err)
	}

	// The day's calls spent: the API is left alone until midnight, the stream reads on.
	f.control(t, "quota?used=50")
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.TelematicData)); kindOf(err) != collector.KindNotAvailable {
		t.Errorf("quota spent: %v", err)
	}
	f.control(t, "quota?used=0")
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.TelematicData)); !errors.Is(err, errSpent) {
		t.Errorf("called again before midnight: %v", err)
	}
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.Stream)); err != nil {
		t.Errorf("stream with the quota spent: %v", err)
	}
	f.clk.Advance(18 * time.Hour) // past 00:00 UTC
	sess = f.login(t)
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.TelematicData)); err != nil {
		t.Errorf("the next day: %v", err)
	}
}

func TestStreamReconnects(t *testing.T) {
	f := newFixture(t)
	sess := f.login(t)
	streamed(t, sess, func(s core.Snapshot) bool { return s.SoC.OK })

	// The ID token expires: the broker closes the stream, whose next read asks for a new
	// access.
	f.clk.Advance(61 * time.Minute)
	f.stream.Step()
	for i := 0; f.src.StreamState("c").Up; i++ {
		if i == waitTries {
			t.Fatal("still up")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.Stream)); kindOf(err) != collector.KindUnauthorized {
		t.Fatalf("expired ID token: %v", err)
	}
	sess = f.login(t)
	streamed(t, sess, func(s core.Snapshot) bool { return s.SoC.OK })
	if !f.src.StreamState("c").Up {
		t.Error("not up again")
	}

	// A new access before the old one ends: the stream moves to its ID token.
	f.clk.Advance(56 * time.Minute)
	sess = f.login(t)
	streamed(t, sess, func(s core.Snapshot) bool { return s.SoC.OK })
	f.clk.Advance(10 * time.Minute) // past the first ID token's end
	f.stream.Step()
	time.Sleep(100 * time.Millisecond)
	if !f.src.StreamState("c").Up {
		t.Error("closed with the old ID token")
	}
}

func TestStreamHeld(t *testing.T) {
	f := newFixture(t)
	// Another client of the account holds the stream (Home Assistant's integration).
	tok, err := f.client.Refresh(t.Context(), bmwapi.DevClientID, f.st.cred.Refresh)
	if err != nil {
		t.Fatal(err)
	}
	f.st.cred.Refresh = tok.Refresh
	other := bmw.NewSubscription(f.addr, tok.GCID)
	if err := other.Connect(t.Context(), tok.ID); err != nil {
		t.Fatal(err)
	}

	sess := f.login(t)
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.Stream)); kindOf(err) != collector.KindStreamHeld {
		t.Errorf("held by another: %v", err)
	}
	if st := f.src.StreamState("c"); st.Up || !st.Held {
		t.Errorf("stream state %+v", st)
	}
	// Not tried again for a while: still told held, rather than the values it left.
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.Stream)); kindOf(err) != collector.KindStreamHeld {
		t.Errorf("before retrying: %v", err)
	}
	other.Close()
	time.Sleep(100 * time.Millisecond)
	f.clk.Advance(heldRetry)
	streamed(t, sess, func(s core.Snapshot) bool { return s.SoC.OK })
}

func TestRefusals(t *testing.T) {
	f := newFixture(t)
	sess := f.login(t)
	streamed(t, sess, func(s core.Snapshot) bool { return s.SoC.OK })

	// Left unread: the subscription is closed.
	f.clk.Advance(idleClose)
	f.src.sweep()
	if st := f.src.StreamState("c"); st != (StreamState{}) {
		t.Errorf("unread subscription kept: %+v", st)
	}
	if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(bmw.Stream)); kindOf(err) != collector.KindUnauthorized {
		t.Errorf("read after the sweep: %v", err)
	}

	f.control(t, "revoke")
	if _, _, err := f.src.Login(t.Context(), "a", "c"); kindOf(err) != collector.KindCredentialRefused {
		t.Errorf("revoked: %v", err)
	}
}
