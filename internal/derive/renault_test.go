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
	"runsten/internal/collector/renaultsource"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/renault"
	"runsten/internal/simulator/renaultapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

// renaultCalls counts the Kamereon calls the simulator answers, per simulated hour.
type renaultCalls struct {
	mu      sync.Mutex
	clk     clock.Clock
	perHour map[time.Time]int
	next    http.Handler
}

func (c *renaultCalls) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.next.ServeHTTP(w, r)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(r.URL.Path) > len(renaultapi.KamereonPrefix) && r.URL.Path[:len(renaultapi.KamereonPrefix)] == renaultapi.KamereonPrefix {
		c.perHour[c.clk.Now().Truncate(time.Hour)]++
	}
}

// playRenault runs the collector against a scenario of the repository on the simulated
// My Renault API, under one account signed in with the simulator's account. The events
// are derived incrementally after each pass, until the given time past the scenario's
// start day.
func playRenault(t *testing.T, until time.Duration, file string) (*memStore, *Deriver, time.Time, *renaultCalls) {
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
		AccountID: "a", VehicleID: "r", VIN: sim.VIN(), ConnectionID: "rc", Provider: renault.Source, CredentialSetAt: t0,
	}
	clk := clock.NewManual(t0)
	calls := &renaultCalls{
		clk: clk, perHour: map[time.Time]int{},
		next: renaultapi.NewHandler([]renaultapi.Source{sim}, clk, renaultapi.DevKeys(), renaultapi.DefaultBehavior()),
	}
	mux := http.NewServeMux()
	mux.Handle(renaultapi.Prefix, calls)
	mux.Handle("/", volvoapi.NewHandler(nil, clk, volvoapi.DefaultLimits(), volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := renault.NewClient(renault.Config{
		GigyaURL: srv.URL + renaultapi.GigyaPrefix, GigyaKey: renaultapi.DevGigyaKey,
		KamereonURL: srv.URL + renaultapi.KamereonPrefix, KamereonKey: renaultapi.DevKamereonKey,
	}, srv.Client())
	ctx := context.Background()
	dev, err := client.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := client.Login(ctx, dev, renaultapi.DevEmail, renaultapi.DevPassword)
	if err != nil {
		t.Fatal(err)
	}
	person, err := client.PersonID(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}

	st := newMemStore(target)
	d := New(st, Sources(volvo.Normalizer{}, renault.Normalizer{}), core.DefaultParams(), renaultCatalog{}, quiet)
	c := collector.New(volvo.NewClient(srv.URL, "key", srv.Client()), st, fixedToken{}, clk, collector.DefaultIntervals(),
		collector.DefaultQuota(), quiet, nil, d)
	c.AddSource(renaultsource.New(client, func(context.Context, string, string) (renaultsource.Credential, error) {
		return renaultsource.Credential{Session: sess, PersonID: person, Country: "FR"}, nil
	}, clk), st)
	day := t0.Truncate(24 * time.Hour)
	for clk.Now().Before(day.Add(until)) {
		if err := c.PollOnce(ctx); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
	return st, d, day, calls
}

// renaultCatalog knows the demonstration Renault 5, as the catalog knows the variant its
// user chose: 52 kWh.
type renaultCatalog struct{}

func (renaultCatalog) NetCapacity(context.Context, string, string) (core.NetCapacity, error) {
	return func(n core.Snapshot) (float64, bool) {
		return 52, n.Family.V == "Renault 5 E-Tech"
	}, nil
}

// TestRenaultDays: the demonstration Renault 5's two days, read through the My Renault
// app's API: home → work and back, an AC charge at home, then a trip on the motorway with
// a DC charge on the way. Kamereon tells no ignition: a drive is told by the odometer,
// read every 10 minutes; the charges, by the battery's status. The calls stay within
// Renault's 60 an hour, and the incremental derivation matches a rebuild.
func TestRenaultDays(t *testing.T) {
	st, d, _, calls := playRenault(t, 36*time.Hour, "renault5.yaml")
	v := st.vehicle("r")
	for hour, n := range calls.perHour {
		if n > 60 {
			t.Errorf("%d calls in the hour of %v, over Renault's 60", n, hour)
		}
	}
	if len(v.trips) != 4 {
		t.Fatalf("%d trips derived, want the four drives:\n%+v", len(v.trips), v.trips)
	}
	for i, km := range []float64{8, 8, 150, 160} {
		if trip := v.trips[i]; !near(trip.DistanceKm, km, 1) {
			t.Errorf("the drive %d: %+v, want %v km", i+1, trip.DistanceKm, km)
		}
	}
	// The position, read at once when a drive ends, places each one.
	places := []core.Position{
		{Lat: 45.7640, Lon: 4.8357},
		{Lat: 45.7980, Lon: 4.8300},
		{Lat: 45.7640, Lon: 4.8357},
		{Lat: 44.5580, Lon: 4.7509},
		{Lat: 43.2965, Lon: 5.3698},
	}
	for i, trip := range v.trips {
		if !at(trip.From, places[i].Lat, places[i].Lon) || !at(trip.To, places[i+1].Lat, places[i+1].Lon) {
			t.Errorf("the drive %d: from %+v to %+v", i+1, trip.From, trip.To)
		}
	}
	if len(v.charges) != 2 {
		t.Fatalf("%d charges derived, want the AC charge at home and the DC one on the way:\n%+v", len(v.charges), v.charges)
	}
	ac, dc := v.charges[0], v.charges[1]
	if !near(ac.EndSoC, 90, 1) {
		t.Errorf("the AC charge: %+v", ac)
	}
	// Read every 5 minutes, a fast charge's end may be seen a few percent short.
	if !near(dc.EndSoC, 85, 3) {
		t.Errorf("the DC charge: %+v", dc)
	}
	checkRebuildOf(t, st, d, "r")
}
