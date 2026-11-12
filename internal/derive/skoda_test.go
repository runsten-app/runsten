package derive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/collector/skodasource"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/skodaapi"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/skoda"
	"runsten/internal/volvo"
)

// skodaCalls counts the calls the simulated MyŠkoda API answers, per simulated hour, and
// its 429s.
type skodaCalls struct {
	mu      sync.Mutex
	clk     clock.Clock
	perHour map[time.Time]int
	limited int
	next    http.Handler
}

func (c *skodaCalls) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w}
	c.next.ServeHTTP(rec, r)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.perHour[c.clk.Now().Truncate(time.Hour)]++
	if rec.status == http.StatusTooManyRequests {
		c.limited++
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// noExpiry is the store's expiry of the key, which the derivation does not read.
type noExpiry struct{}

func (noExpiry) SetCredentialExpiry(context.Context, string, string, time.Time, time.Time) error {
	return nil
}

// playSkoda runs the collector against a scenario of the repository on the simulated
// MyŠkoda Public API, under one account, read with the simulator's key. The events are
// derived incrementally after each pass, until the given time past the scenario's start
// day.
func playSkoda(t *testing.T, until time.Duration, file string) (*memStore, *Deriver, time.Time, *skodaCalls) {
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
		AccountID: "a", VehicleID: "s", VIN: sim.VIN(), ConnectionID: "sc", Provider: skoda.Source, CredentialSetAt: t0,
	}
	clk := clock.NewManual(t0)
	calls := &skodaCalls{
		clk: clk, perHour: map[time.Time]int{},
		next: skodaapi.NewHandler([]skodaapi.Source{sim}, clk, skodaapi.DevAPIKey, skodaapi.DefaultBehavior()),
	}
	mux := http.NewServeMux()
	mux.Handle(skodaapi.Prefix, calls)
	mux.Handle("/", volvoapi.NewHandler(nil, clk, volvoapi.DefaultLimits(), volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	st := newMemStore(target)
	d := New(st, Sources(volvo.Normalizer{}, skoda.Normalizer{}), core.DefaultParams(), skodaCatalog{}, quiet)
	c := collector.New(volvo.NewClient(srv.URL, "key", srv.Client()), st, fixedToken{}, clk, collector.DefaultIntervals(),
		collector.DefaultQuota(), quiet, nil, d)
	c.AddSource(skodasource.New(skoda.NewClient(srv.URL+skodaapi.Prefix, srv.Client()),
		func(context.Context, string, string) (skodasource.Credential, error) {
			return skodasource.Credential{APIKey: skodaapi.DevAPIKey, SetAt: t0}, nil
		}, noExpiry{}, clk), st)
	day := t0.Truncate(24 * time.Hour)
	for clk.Now().Before(day.Add(until)) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
	return st, d, day, calls
}

// skodaCatalog knows the demonstration Enyaq, as the catalog knows the variant its user
// chose: 77 kWh, the usable capacity of enyaq.yaml.
type skodaCatalog struct{}

func (skodaCatalog) NetCapacity(context.Context, string, string) (core.NetCapacity, error) {
	return func(n core.Snapshot) (float64, bool) {
		return 77, n.Family.V == "Enyaq"
	}, nil
}

// TestSkodaDays: the demonstration Enyaq's two days, read through the MyŠkoda Public API:
// home → work and back, an AC charge at home, then a trip to the coast with a DC charge
// on the way. The API has no ignition: a drive is told by the parking position's
// IN_MOTION and the odometer, read every 20 minutes parked and every 5 driving or
// charging; the charges have their power. The calls never exceed Škoda's 20 an hour, and
// the incremental derivation matches a rebuild.
func TestSkodaDays(t *testing.T) {
	st, d, day, calls := playSkoda(t, 36*time.Hour, "enyaq.yaml")
	v := st.vehicle("s")
	for hour, n := range calls.perHour {
		if n > 20 {
			t.Errorf("%d calls in the hour of %v, over Škoda's 20", n, hour)
		}
	}
	if calls.limited > 0 {
		t.Errorf("%d calls answered 429", calls.limited)
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
		if !near(trip.DistanceKm, 8, 1) || !at(trip.From, want.from.Lat, want.from.Lon) || !at(trip.To, want.to.Lat, want.to.Lon) {
			t.Errorf("the drive %d: %+v", i+1, trip)
		}
		if !within(trip.Start.After, day.Add(want.start-30*time.Minute), day.Add(want.start+30*time.Minute)) {
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
	// Read every 5 minutes, a fast charge's end may be seen a few percent short: the car
	// leaves before the next read.
	if !dc.Type.OK || dc.Type.V != core.DC || !near(dc.EndSoC, 85, 3) || !near(dc.StartSoC, 57, 2) {
		t.Errorf("the DC charge: %+v", dc)
	}
	// The API gives the charging power: the charges have its energy too.
	if !ac.EnergyPowerKWh.OK || !dc.EnergyPowerKWh.OK {
		t.Errorf("the charges' power energies: %+v, %+v", ac.EnergyPowerKWh, dc.EnergyPowerKWh)
	}
	if trip := v.trips[2]; !near(trip.DistanceKm, 150, 1) {
		t.Errorf("the drive to the charger: %+v", trip)
	}
	if trip := v.trips[3]; !near(trip.DistanceKm, 160, 1) {
		t.Errorf("the drive to the coast: %+v", trip)
	}
	checkRebuildOf(t, st, d, "s")
}
