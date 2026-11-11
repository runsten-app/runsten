package derive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/collector/pleossource"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/pleos"
	"runsten/internal/simulator/pleosapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

// playPleos runs the collector against a scenario of the repository on the simulated
// Pleos Vehicle Data API, under one account: the car of brand kia, read with the
// simulator's key. The events are derived incrementally after each pass, until the
// given time past the scenario's start day.
func playPleos(t *testing.T, until time.Duration, file string) (*memStore, *Deriver, time.Time) {
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
		AccountID: "a", VehicleID: "k", VIN: sim.VIN(), ConnectionID: "kc",
		Provider: pleos.Kia.Provider(), CredentialSetAt: t0,
	}
	clk := clock.NewManual(t0)
	mux := http.NewServeMux()
	mux.Handle(pleosapi.Prefix, pleosapi.NewHandler([]pleosapi.Source{sim}, clk,
		pleosapi.DevCredential(), pleosapi.DefaultBehavior()))
	mux.Handle("/", volvoapi.NewHandler(nil, clk, volvoapi.DefaultLimits(), volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	st := newMemStore(target)
	d := New(st, Sources(volvo.Normalizer{}, pleos.Normalizer{}), core.DefaultParams(), pleosCatalog{}, quiet)
	c := collector.New(volvo.NewClient(srv.URL, "key", srv.Client()), st, fixedToken{}, clk, collector.DefaultIntervals(),
		collector.DefaultQuota(), quiet, nil, d)
	dev := pleosapi.DevCredential()
	c.AddSource(pleossource.New(pleos.NewClient(srv.URL+pleosapi.Prefix, srv.Client()),
		func(context.Context, string, string) (pleossource.Credential, error) {
			return pleossource.Credential{ClientID: dev.ClientID, ClientSecret: dev.ClientSecret, Brand: pleos.Kia}, nil
		}, clk), st)
	day := t0.Truncate(24 * time.Hour)
	for clk.Now().Before(day.Add(until)) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
	return st, d, day
}

// pleosCatalog knows the demonstration EV6, as the catalog knows the variant its user
// chose: 74 kWh, the usable capacity of ev6.yaml.
type pleosCatalog struct{}

func (pleosCatalog) NetCapacity(context.Context, string, string) (core.NetCapacity, error) {
	return func(n core.Snapshot) (float64, bool) {
		return 74, n.Family.V == "EV6"
	}, nil
}

// Where the demonstration EV6 goes, and the net capacity its derivation rests on.
var (
	pleosHome = core.Position{Lat: 45.7640, Lon: 4.8357}
	pleosWork = core.Position{Lat: 45.7980, Lon: 4.8300}
)

// TestPleosDay: the demonstration EV6's day, read through the Pleos Vehicle Data API:
// home → work in the morning, work → home in the evening, an AC charge at home after.
// The car sends at each change and every five minutes driving or charging, the API
// returns its last send and the collector reads it at its own cadence, dated from the
// car's own clock: the trips are observed, and the incremental derivation matches a
// rebuild (the rule of TestIncrementalMatchesRebuild).
func TestPleosDay(t *testing.T) {
	st, d, day := playPleos(t, 27*time.Hour, "ev6.yaml")
	v := st.vehicle("k")
	if len(v.trips) != 2 {
		t.Fatalf("%d trips derived, want the two drives of the day:\n%+v", len(v.trips), v.trips)
	}
	// The drives of the scenario: 07:00 to work, 16:20 back.
	for i, want := range []struct {
		from, to core.Position
		start    time.Duration
	}{{pleosHome, pleosWork, 7 * time.Hour}, {pleosWork, pleosHome, 16*time.Hour + 20*time.Minute}} {
		trip := v.trips[i]
		if !near(trip.DistanceKm, 8, 0.5) || !at(trip.From, want.from.Lat, want.from.Lon) ||
			!at(trip.To, want.to.Lat, want.to.Lon) {
			t.Errorf("the drive %d: %+v", i+1, trip)
		}
		if !within(trip.Start.After, day.Add(want.start-30*time.Minute), day.Add(want.start+30*time.Minute)) {
			t.Errorf("the drive %d starts after %v, want about %v", i+1, trip.Start.After, day.Add(want.start))
		}
	}
	if len(v.charges) != 1 {
		t.Fatalf("%d charges derived, want the AC charge of the evening:\n%+v", len(v.charges), v.charges)
	}
	charge := v.charges[0]
	if !near(charge.StartSoC, 60, 6) || !near(charge.EndSoC, 90, 1) {
		t.Errorf("the charge: %+v", charge)
	}
	if !charge.Type.OK || charge.Type.V != core.AC {
		t.Errorf("the charge's type: %+v", charge.Type)
	}
	// The API gives no charging power: the charge keeps the energy of its state of
	// charge alone, over the net capacity of the catalog (74 kWh).
	if !near(charge.EnergySoCKWh, 0.30*74, 3) {
		t.Errorf("the charge's energy: %+v", charge.EnergySoCKWh)
	}
	if charge.EnergyPowerKWh.OK {
		t.Errorf("the charge's power energy: %+v", charge.EnergyPowerKWh)
	}
	checkRebuildOf(t, st, d, "k")
}
