package teslasource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/teslaapi"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/tesla"
)

const vin = "LRW3E7EK1RC000001"

var setAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// tokens is an in-memory Store of one connection's refresh token.
type tokens struct {
	token     string
	setAt     time.Time
	rotateErr error
	rotations int
}

func (s *tokens) TeslaToken(context.Context, string, string) (string, time.Time, error) {
	return s.token, s.setAt, nil
}

func (s *tokens) RotateTeslaToken(_ context.Context, _, _ string, at time.Time, token string) error {
	if s.rotateErr != nil {
		return s.rotateErr
	}
	if at.Equal(s.setAt) {
		s.token = token
		s.rotations++
	}
	return nil
}

// sim is the Owner API of the simulator on the model3 scenario, at its start.
type sim struct {
	src    *Source
	tokens *tokens
	clk    *clock.Manual
	srv    *httptest.Server
}

func newSim(t *testing.T) *sim { return newSimWith(t, nil) }

// newSimWith serves the simulator through wrap, unless nil.
func newSimWith(t *testing.T, wrap func(http.Handler) http.Handler) *sim {
	t.Helper()
	data, err := os.ReadFile("../../../scenarios/model3.yaml")
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
	var h http.Handler = teslaapi.NewHandler([]teslaapi.Source{s}, clk, teslaapi.DefaultBehavior())
	if wrap != nil {
		h = wrap(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	tok := &tokens{token: teslaapi.DevRefreshToken(), setAt: setAt}
	client := tesla.NewClient(tesla.Hosts{API: srv.URL, Auth: srv.URL + teslaapi.AuthPrefix}, srv.Client())
	return &sim{src: New(client, tok), tokens: tok, clk: clk, srv: srv}
}

func (s *sim) control(t *testing.T, path string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.srv.URL+teslaapi.ControlPrefix+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func kindOf(err error) collector.ErrorKind {
	var se *collector.SourceError
	if !errors.As(err, &se) {
		return -1
	}
	return se.Kind
}

func TestLoginAndFetch(t *testing.T) {
	ctx := context.Background()
	s := newSim(t)
	sess, ttl, err := s.src.Login(ctx, "a", "c")
	if err != nil || ttl != 8*time.Hour {
		t.Fatalf("login: %v, %v", ttl, err)
	}
	// The refresh token given in exchange is kept: the simulator's are single use.
	if s.tokens.rotations != 1 || s.tokens.token == teslaapi.DevRefreshToken() {
		t.Errorf("rotations %d", s.tokens.rotations)
	}
	if _, _, err := s.src.Login(ctx, "a", "c"); err != nil || s.tokens.rotations != 2 {
		t.Fatalf("login again: %v, rotations %d", err, s.tokens.rotations)
	}
	var a collector.Activity
	for _, ep := range s.src.Spec().Endpoints {
		raw, err := sess.Fetch(ctx, vin, ep.Endpoint)
		if err != nil {
			t.Fatalf("%s: %v", ep.Endpoint, err)
		}
		if err := s.src.Observe(ep.Endpoint, raw, &a); err != nil {
			t.Fatalf("%s: %v", ep.Endpoint, err)
		}
		if _, err := s.src.Fingerprint(raw); err != nil {
			t.Fatal(err)
		}
		if u, err := s.src.Upload(ep.Endpoint, raw); err != nil || len(u.Fields) == 0 || slices.Contains(u.Fields, "vin") {
			t.Errorf("%s: upload %+v, %v", ep.Endpoint, u, err)
		}
	}
	// Parked at home, awake as the scenario starts.
	if a.Asleep || a.Driving || a.Charges || a.Engine == "" || a.Charging == "" {
		t.Errorf("activity %+v", a)
	}
	if _, err := sess.Fetch(ctx, "LRW3E7EK1RC000009", collector.Endpoint(tesla.VehicleState)); kindOf(err) != collector.KindUnshared {
		t.Errorf("another VIN: %v", err)
	}

	// Left alone, it falls asleep: its state says so, and vehicle_data is no failure.
	s.clk.Advance(15 * time.Minute)
	raw, err := sess.Fetch(ctx, vin, collector.Endpoint(tesla.VehicleState))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.src.Observe(collector.Endpoint(tesla.VehicleState), raw, &a); err != nil || !a.Asleep {
		t.Errorf("asleep: %+v, %v", a, err)
	}
	if _, err := sess.Fetch(ctx, vin, collector.Endpoint(tesla.VehicleData)); kindOf(err) != collector.KindAsleep {
		t.Errorf("vehicle_data asleep: %v", err)
	}
}

// TestRotationNotKept: a refresh token given that could not be kept is refreshed with
// next, the one stored no longer refreshing; unless the owner set another meanwhile.
func TestRotationNotKept(t *testing.T) {
	ctx := context.Background()
	s := newSim(t)
	s.tokens.rotateErr = errors.New("database down")
	if _, _, err := s.src.Login(ctx, "a", "c"); err == nil {
		t.Fatal("login with the token given not kept: no error")
	}
	s.tokens.rotateErr = nil
	if _, _, err := s.src.Login(ctx, "a", "c"); err != nil || s.tokens.rotations != 1 {
		t.Fatalf("login once kept: %v, rotations %d", err, s.tokens.rotations)
	}

	s.tokens.rotateErr = errors.New("database down")
	if _, _, err := s.src.Login(ctx, "a", "c"); err == nil {
		t.Fatal("no error")
	}
	// The owner gives a new token: the one given for the previous is forgotten.
	s.tokens.rotateErr = nil
	s.tokens.token, s.tokens.setAt = issued(t, s), setAt.Add(time.Hour)
	if _, _, err := s.src.Login(ctx, "a", "c"); err != nil {
		t.Fatalf("login with the owner's new token: %v", err)
	}
}

// issued is a new refresh token of the simulator, as Tesla Auth gives the owner.
func issued(t *testing.T, s *sim) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.srv.URL+teslaapi.ControlPrefix+"refresh-token", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &v); err != nil || v.RefreshToken == "" {
		t.Fatalf("issued: %s, %v", body, err)
	}
	return v.RefreshToken
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	s := newSim(t)
	sess, _, err := s.src.Login(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	s.control(t, "rate-limit?for=2m")
	_, err = sess.Fetch(ctx, vin, collector.Endpoint(tesla.VehicleState))
	var se *collector.SourceError
	if !errors.As(err, &se) || se.Kind != collector.KindRateLimited || se.RetryIn != 2*time.Minute || se.Status != 429 {
		t.Errorf("rate limited: %v", err)
	}
	s.control(t, "rate-limit?for=0s")

	s.control(t, "owner-api?closed=true")
	if _, _, err := s.src.Login(ctx, "a", "c"); kindOf(err) != collector.KindAPIClosed {
		t.Errorf("closed: %v", err)
	}
	s.control(t, "owner-api?closed=false")

	s.clk.Advance(8 * time.Hour)
	if _, err := sess.Fetch(ctx, vin, collector.Endpoint(tesla.VehicleState)); kindOf(err) != collector.KindUnauthorized {
		t.Errorf("expired: %v", err)
	}

	s.control(t, "revoke")
	if _, _, err := s.src.Login(ctx, "a", "c"); kindOf(err) != collector.KindCredentialRefused {
		t.Errorf("revoked: %v", err)
	}
}

// store is the collector's Store and SourceStore, in memory: the snapshots stored, the
// statuses saved.
type store struct {
	targets   []collector.Target
	snapshots []collector.Snapshot
	last      map[collector.Endpoint][]byte
	status    collector.Status
	refused   bool
}

func (s *store) Targets(context.Context) ([]collector.Target, error) { return s.targets, nil }

func (s *store) SaveSnapshot(_ context.Context, sn collector.Snapshot) (bool, error) {
	if string(s.last[sn.Endpoint]) == string(sn.Hash) {
		return false, nil
	}
	s.last[sn.Endpoint] = sn.Hash
	s.snapshots = append(s.snapshots, sn)
	return true, nil
}

func (s *store) SaveStatus(_ context.Context, st collector.Status) error {
	s.status = st
	return nil
}

func (*store) SaveCalls(context.Context, []collector.Calls) error { return nil }

func (*store) Calls(context.Context, time.Time) ([]collector.Calls, error) { return nil, nil }

func (*store) ConnectionKey(context.Context, string, string) (string, error) { return "", nil }

func (*store) SetKeyRefused(context.Context, string, string, time.Time, bool) error { return nil }

func (*store) SetMarketRefused(context.Context, string, string) error { return nil }

func (s *store) SetCredentialRefused(context.Context, string, string, time.Time) error {
	s.refused = true
	return nil
}

func (*store) SetUnshared(context.Context, string, string) error { return nil }

// calls counts the calls of a vehicle the simulator answered, by endpoint and time.
type calls struct {
	clk clock.Clock
	at  map[tesla.Endpoint][]time.Time
}

func (c *calls) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/vehicle_data"):
			c.at[tesla.VehicleData] = append(c.at[tesla.VehicleData], c.clk.Now())
		case strings.HasPrefix(r.URL.Path, teslaapi.APIPrefix+"vehicles/"):
			c.at[tesla.VehicleState] = append(c.at[tesla.VehicleState], c.clk.Now())
		}
		next.ServeHTTP(w, r)
	})
}

func count(at []time.Time, from, to time.Time) int {
	n := 0
	for _, t := range at {
		if !t.Before(from) && t.Before(to) {
			n++
		}
	}
	return n
}

// TestCollectorDay: the collector reads the simulated Model 3 over its scenario's day.
// The car falls asleep in its first sleep window, and is left asleep until it drives;
// both trips and the charge are read; the access token, expired after 8 h, is asked for
// again with the refresh token the first refresh gave.
func TestCollectorDay(t *testing.T) {
	cc := &calls{at: map[tesla.Endpoint][]time.Time{}}
	s := newSimWith(t, cc.wrap)
	cc.clk = s.clk
	st := &store{
		targets: []collector.Target{{AccountID: "a", VehicleID: "v", VIN: vin, ConnectionID: "c", Provider: tesla.Source, CredentialSetAt: setAt}},
		last:    map[collector.Endpoint][]byte{},
	}
	c := collector.New(nil, st, nil, s.clk, collector.DefaultIntervals(), collector.DefaultQuota(), slog.New(slog.DiscardHandler), nil, nil)
	c.WithoutVolvo()
	c.AddSource(s.src, st)

	t0 := s.clk.Now()
	for s.clk.Now().Before(t0.Add(15 * time.Hour)) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		s.clk.Advance(10 * time.Second)
	}

	data := cc.at[tesla.VehicleData]
	if n := count(data, t0.Add(30*time.Minute), t0.Add(2*time.Hour)); n != 0 {
		t.Errorf("vehicle_data read %d times while the car sleeps before its first trip", n)
	}
	if n := count(cc.at[tesla.VehicleState], t0, t0.Add(2*time.Hour)); n < 115 {
		t.Errorf("state read %d times in 2 h, want one a minute", n)
	}
	// 05:00 parked; 07:00 to work (30 min); 16:30 home (35 min); 18:05 charge.
	var engines, charges int
	for _, sn := range st.snapshots {
		if sn.Endpoint != collector.Endpoint(tesla.VehicleData) {
			continue
		}
		snap, err := tesla.Normalize(tesla.VehicleData, sn.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Engine.V == core.EngineRunning {
			engines++
		}
		if snap.Charging.V == core.ChargingActive {
			charges++
		}
	}
	if engines < 100 || charges == 0 {
		t.Errorf("read driving %d times, charging %d times", engines, charges)
	}
	if st.refused || st.status.Failure != (collector.Failure{}) {
		t.Errorf("refused %v, failure %+v", st.refused, st.status.Failure)
	}
	if s.tokens.rotations < 2 {
		t.Errorf("refresh token kept %d times in 15 h, want at least 2", s.tokens.rotations)
	}
	t.Logf("calls: vehicle %d, vehicle_data %d", len(cc.at[tesla.VehicleState]), len(data))
}
