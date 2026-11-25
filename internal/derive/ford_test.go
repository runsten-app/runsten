package derive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/collector/fordsource"
	"runsten/internal/core"
	"runsten/internal/ford"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/fordapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

// fordCalls records when the simulated FordConnect Query was read, and counts its 429s.
type fordCalls struct {
	mu      sync.Mutex
	clk     clock.Clock
	reads   []time.Time
	garages []time.Time // those of the garage
	limited int
	next    http.Handler
}

func (c *fordCalls) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w}
	c.next.ServeHTTP(rec, r)
	if r.Method != http.MethodGet {
		return // the token endpoint: not a read
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads = append(c.reads, c.clk.Now())
	if strings.HasSuffix(r.URL.Path, "/garage") {
		c.garages = append(c.garages, c.clk.Now())
	}
	if rec.status == http.StatusTooManyRequests {
		c.limited++
	}
}

// fordGrant keeps the connection's credential, as the store does.
type fordGrant struct {
	mu   sync.Mutex
	cred fordsource.Credential
}

func (g *fordGrant) FordCredential(context.Context, string, string) (fordsource.Credential, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cred, nil
}

func (g *fordGrant) RotateFordToken(_ context.Context, _, _ string, _ time.Time, token string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cred.Refresh = token
	return nil
}

func (*fordGrant) SetCredentialExpiry(context.Context, string, string, time.Time, time.Time) error {
	return nil
}

// playFord runs the collector against a scenario of the repository on the simulated
// FordConnect Query, under one account, signed in with the simulator's application. The
// events are derived incrementally after each pass, until the given time past the
// scenario's start day.
func playFord(t *testing.T, until time.Duration, file string) (*memStore, *Deriver, time.Time, *fordCalls) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../scenarios", file)) //nolint:gosec // repository file
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	t0 := sim.StatusAt(sc.Start).Now
	target := collector.Target{
		AccountID: "a", VehicleID: "f", VIN: sim.VIN(), ConnectionID: "fc", Provider: ford.Source, CredentialSetAt: t0,
	}
	clk := clock.NewManual(t0)
	calls := &fordCalls{
		clk:  clk,
		next: fordapi.NewHandler([]fordapi.Source{sim}, clk, fordapi.DevApplication(), fordapi.DefaultBehavior()),
	}
	mux := http.NewServeMux()
	mux.Handle(fordapi.Prefix, calls)
	mux.Handle("/", volvoapi.NewHandler(nil, clk, volvoapi.DefaultLimits(), volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := ford.NewClient(ford.Hosts{
		Auth: srv.URL + fordapi.Prefix + "auth/init", Token: srv.URL + fordapi.Prefix + "token", API: srv.URL + "/ford",
	}, srv.Client())
	dev := fordapi.DevApplication()
	app := ford.Application{ClientID: dev.ClientID, ClientSecret: dev.ClientSecret, RedirectURI: "http://localhost/auth/ford/callback"}
	authURL, err := client.AuthorizationURL(app, "state")
	if err != nil {
		t.Fatal(err)
	}
	hc := *srv.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, authURL, http.NoBody)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	back, _ := url.Parse(resp.Header.Get("Location"))
	tok, err := client.Exchange(t.Context(), app, back.Query().Get("code"))
	if err != nil {
		t.Fatal(err)
	}
	calls.mu.Lock()
	calls.reads, calls.garages = nil, nil // the sign-in's own request
	calls.mu.Unlock()

	st := newMemStore(target)
	d := New(st, Sources(volvo.Normalizer{}, ford.Normalizer{}), core.DefaultParams(), fordCatalog{}, quiet)
	c := collector.New(volvo.NewClient(srv.URL, "key", srv.Client()), st, fixedToken{}, clk, collector.DefaultIntervals(),
		collector.DefaultQuota(), quiet, nil, d)
	c.AddSource(fordsource.New(client, &fordGrant{cred: fordsource.Credential{Application: app, Refresh: tok.Refresh, SetAt: t0}}, clk), st)
	day := t0.Truncate(24 * time.Hour)
	for clk.Now().Before(day.Add(until)) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
	return st, d, day, calls
}

// fordCatalog knows the demonstration Puma, as the catalog knows the variant its user
// chose: 43 kWh, the usable capacity of puma.yaml.
type fordCatalog struct{}

func (fordCatalog) NetCapacity(context.Context, string, string) (core.NetCapacity, error) {
	return func(n core.Snapshot) (float64, bool) {
		return 43, n.Family.V == "Puma"
	}, nil
}

// TestFordDays: the demonstration Puma's two days, read through FordConnect Query: home →
// work and back, an AC charge at home, then a trip to the mountain with a DC charge on
// the way. The ignition tells each drive, read every minute then; the charges have their
// power, from the charger's voltage and current. No call is answered 429, two reads are
// never within twenty seconds, and the incremental derivation matches a rebuild.
func TestFordDays(t *testing.T) {
	st, d, day, calls := playFord(t, 36*time.Hour, "puma.yaml")
	v := st.vehicle("f")
	if calls.limited > 0 {
		t.Errorf("%d calls answered 429", calls.limited)
	}
	for i := 1; i < len(calls.reads); i++ {
		if gap := calls.reads[i].Sub(calls.reads[i-1]); gap < 20*time.Second {
			t.Errorf("two reads %v apart, at %v", gap, calls.reads[i])
		}
	}
	// The garage, due with the telemetry at the first pass, is read once the gap is over,
	// not a day later, then daily.
	if len(calls.garages) != 2 || calls.garages[0].Sub(calls.reads[0]) > time.Minute {
		t.Errorf("the garage read at %v, the first telemetry at %v", calls.garages, calls.reads[0])
	}
	// A read every 5 minutes parked and every minute driving or charging, over 36 hours.
	if n := len(calls.reads); n > 1100 {
		t.Errorf("%d calls in 36 hours", n)
	}
	home, work := core.Position{Lat: 45.7640, Lon: 4.8357}, core.Position{Lat: 45.7980, Lon: 4.8300}
	if len(v.trips) != 4 {
		t.Fatalf("%d trips derived, want the four drives:\n%+v", len(v.trips), v.trips)
	}
	for i, want := range []struct {
		from, to core.Position
		start    time.Duration
	}{{home, work, 7 * time.Hour}, {work, home, 16*time.Hour + 25*time.Minute}} {
		trip := v.trips[i]
		// The odometer comes in whole kilometres.
		if !near(trip.DistanceKm, 8, 1.5) || !at(trip.From, want.from.Lat, want.from.Lon) || !at(trip.To, want.to.Lat, want.to.Lon) {
			t.Errorf("the drive %d: %+v", i+1, trip)
		}
		if !within(trip.Start.After, day.Add(want.start-10*time.Minute), day.Add(want.start+10*time.Minute)) {
			t.Errorf("the drive %d starts after %v, want about %v", i+1, trip.Start.After, day.Add(want.start))
		}
	}
	if len(v.charges) != 2 {
		t.Fatalf("%d charges derived, want the AC charge at home and the DC one on the way:\n%+v", len(v.charges), v.charges)
	}
	ac, dc := v.charges[0], v.charges[1]
	if !ac.Type.OK || ac.Type.V != core.AC || !near(ac.EndSoC, 90, 1) {
		t.Errorf("the AC charge: %+v", ac)
	}
	if !dc.Type.OK || dc.Type.V != core.DC || !near(dc.EndSoC, 80, 2) {
		t.Errorf("the DC charge: %+v", dc)
	}
	if !ac.EnergyPowerKWh.OK || !dc.EnergyPowerKWh.OK {
		t.Errorf("the charges' power energies: %+v, %+v", ac.EnergyPowerKWh, dc.EnergyPowerKWh)
	}
	if trip := v.trips[2]; !near(trip.DistanceKm, 110, 1.5) {
		t.Errorf("the drive to the charger: %+v", trip)
	}
	if trip := v.trips[3]; !near(trip.DistanceKm, 60, 1.5) {
		t.Errorf("the drive to the mountain: %+v", trip)
	}
	checkRebuildOf(t, st, d, "f")
}
