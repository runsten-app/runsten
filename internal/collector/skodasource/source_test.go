package skodasource

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/skodaapi"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/skoda"
)

const simVIN = "TMBJC9NY0RF000001"

var setAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// expiries records the expiries the source writes.
type expiries struct {
	mu  sync.Mutex
	got []time.Time
	err error
}

func (e *expiries) SetCredentialExpiry(_ context.Context, _, _ string, at, expiresAt time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !at.Equal(setAt) {
		return errors.New("another key's set time")
	}
	if e.err != nil {
		return e.err
	}
	e.got = append(e.got, expiresAt)
	return nil
}

// sim answers the MyŠkoda Public API on the Enyaq of demonstration: this is the contract
// test between the source and the fake API.
type sim struct {
	src *Source
	h   *skodaapi.Handler
	clk *clock.Manual
	st  *expiries
}

func newSim(t *testing.T) *sim {
	t.Helper()
	data, err := os.ReadFile("../../../scenarios/enyaq.yaml") //nolint:gosec // the test's own scenario
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	s, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	h := skodaapi.NewHandler([]skodaapi.Source{s}, clk, skodaapi.DevAPIKey, skodaapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	credential := func(context.Context, string, string) (Credential, error) {
		return Credential{APIKey: skodaapi.DevAPIKey, SetAt: setAt}, nil
	}
	st := &expiries{}
	return &sim{src: New(skoda.NewClient(srv.URL+skodaapi.Prefix, srv.Client()), credential, st, clk), h: h, clk: clk, st: st}
}

func (s *sim) fetch(t *testing.T) ([]byte, error) {
	t.Helper()
	sess, ttl, err := s.src.Login(context.Background(), "account", "connection")
	if err != nil || ttl != keyLife {
		t.Fatalf("login: %v, %v", ttl, err)
	}
	return sess.Fetch(context.Background(), simVIN, collector.Endpoint(skoda.Vehicle))
}

func kindOf(t *testing.T, err error) collector.ErrorKind {
	t.Helper()
	var se *collector.SourceError
	if !errors.As(err, &se) {
		t.Fatalf("not a SourceError: %v", err)
	}
	return se.Kind
}

func TestSpec(t *testing.T) {
	spec := (&Source{}).Spec()
	if spec.Name != skoda.Source || len(spec.Endpoints) != 1 || spec.Endpoints[0].API != skoda.Source || spec.Daily != Daily {
		t.Errorf("spec %+v", spec)
	}
	// Within Škoda's 20 an hour, the margin left: active 12 an hour.
	if per := int(time.Hour/Intervals().Active) + reserve; per > 20 {
		t.Errorf("active pace %v leaves no margin", Intervals().Active)
	}
}

// TestFetch: the read, its activity, and the key's expiry written once while it holds.
func TestFetch(t *testing.T) {
	s := newSim(t)
	raw, err := s.fetch(t)
	if err != nil {
		t.Fatal(err)
	}
	var a collector.Activity
	if err := s.src.Observe(collector.Endpoint(skoda.Vehicle), raw, &a); err != nil || a.Driving || a.Charges {
		t.Errorf("parked at home: %+v, %v", a, err)
	}
	if _, err := s.fetch(t); err != nil {
		t.Fatal(err)
	}
	if len(s.st.got) != 1 || !s.st.got[0].Equal(s.h.ExpiresAt()) {
		t.Errorf("expiries written: %v", s.st.got)
	}
	s.h.ExpireIn(48 * time.Hour) // renewed in the app, or another expiry told
	if _, err := s.fetch(t); err != nil {
		t.Fatal(err)
	}
	if len(s.st.got) != 2 {
		t.Errorf("a new expiry not written: %v", s.st.got)
	}
	if fp, err := s.src.Fingerprint(raw); err != nil || len(fp) == 0 {
		t.Errorf("fingerprint: %v", err)
	}
	if u, err := s.src.Upload(collector.Endpoint(skoda.Vehicle), raw); err != nil || u.At.IsZero() || u.ModelYear != 2024 {
		t.Errorf("upload %+v, %v", u, err)
	}
	if err := s.src.Observe("", []byte("{"), &a); err == nil {
		t.Error("invalid JSON observed")
	}
}

// TestDrivingAndCharging: the parking position tells the drive, the state the charge.
func TestDrivingAndCharging(t *testing.T) {
	s := newSim(t)
	s.clk.Advance(2*time.Hour + 10*time.Minute) // on the way to work
	raw, err := s.fetch(t)
	if err != nil {
		t.Fatal(err)
	}
	var a collector.Activity
	if err := s.src.Observe("", raw, &a); err != nil || !a.Driving || a.Engine != "running" {
		t.Errorf("driving: %+v, %v", a, err)
	}
	s.clk.Advance(11 * time.Hour) // home, plugged in at 17:50
	raw, err = s.fetch(t)
	if err != nil {
		t.Fatal(err)
	}
	a = collector.Activity{}
	if err := s.src.Observe("", raw, &a); err != nil || a.Driving || !a.Charges {
		t.Errorf("charging: %+v, %v", a, err)
	}
}

// TestQuota: the VIN is held back once its quota is down to the margin, until the
// window's end; a 429 holds it as long as it says.
func TestQuota(t *testing.T) {
	s := newSim(t)
	for i := range 20 - reserve {
		if _, err := s.fetch(t); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		if held := s.src.HeldUntil(simVIN); held.IsZero() != (i < 20-reserve-1) {
			t.Fatalf("call %d: held until %v", i+1, held)
		}
	}
	if held := s.src.HeldUntil(simVIN); !held.Equal(s.clk.Now().Add(time.Hour)) {
		t.Errorf("held until %v, want the window's end", held)
	}
	// Another client spends the rest: the next read meets a 429.
	for range reserve {
		if _, err := s.fetch(t); err != nil {
			t.Fatal(err)
		}
	}
	s.clk.Advance(10 * time.Minute)
	_, err := s.fetch(t)
	if k := kindOf(t, err); k != collector.KindVehicleLimited {
		t.Fatalf("kind %d", k)
	}
	if held := s.src.HeldUntil(simVIN); !held.Equal(s.clk.Now().Add(50 * time.Minute)) {
		t.Errorf("held until %v after the 429", held)
	}
	s.clk.Advance(50 * time.Minute)
	if _, err := s.fetch(t); err != nil {
		t.Fatal(err)
	}
	if held := s.src.HeldUntil(simVIN); !held.IsZero() {
		t.Errorf("a new window still held: %v", held)
	}
}

func TestRefusals(t *testing.T) {
	tests := []struct {
		name  string
		fault func(*skodaapi.Handler)
		kind  collector.ErrorKind
	}{
		{"expired", func(h *skodaapi.Handler) { h.ExpireIn(0) }, collector.KindCredentialRefused},
		{"unticked", func(h *skodaapi.Handler) { h.Uncover(simVIN) }, collector.KindUnshared},
		{"outage", func(h *skodaapi.Handler) { h.Outage(time.Hour) }, collector.KindOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSim(t)
			tt.fault(s.h)
			_, err := s.fetch(t)
			if k := kindOf(t, err); k != tt.kind {
				t.Errorf("kind %d, want %d", k, tt.kind)
			}
		})
	}
}

func TestFailures(t *testing.T) {
	s := newSim(t)
	sess, _, _ := s.src.Login(context.Background(), "account", "connection")
	if _, err := sess.Fetch(context.Background(), simVIN, "status"); err == nil {
		t.Error("another endpoint read")
	}
	s.st.err = errors.New("database down")
	if _, err := s.fetch(t); err != nil {
		t.Errorf("the read stands when the expiry is not written: %v", err)
	}
	failing := New(skoda.NewClient("http://127.0.0.1:1", nil), func(context.Context, string, string) (Credential, error) {
		return Credential{}, errors.New("no key")
	}, s.st, s.clk)
	if _, _, err := failing.Login(context.Background(), "a", "c"); err == nil {
		t.Error("a login without a key")
	}
	if err := failing.fetchError(simVIN, errors.New("network")); err == nil || errors.As(err, new(*collector.SourceError)) {
		t.Errorf("a transport error: %v", err)
	}
}
