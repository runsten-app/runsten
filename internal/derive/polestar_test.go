package derive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/collector/polestarsource"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/polestar"
	"runsten/internal/simulator/polestarapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

func (m *memStore) SetCredentialRefused(context.Context, string, string, time.Time) error { return nil }

func (m *memStore) SetUnshared(context.Context, string, string) error { return nil }

// playFleet runs the collector against scenarios of the repository, one vehicle each,
// under one account: those of brand polestar on the simulated Data Portal (vehicle IDs
// p, p2…, read with the simulator's credential), the others on the simulated Volvo API
// (v, v2…). The events are derived incrementally after each pass, until the given time
// past the first scenario's start day.
func playFleet(t *testing.T, until time.Duration, files ...string) (*memStore, *Deriver, time.Time) {
	t.Helper()
	var scs []*scenario.Scenario
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join("../../scenarios", f)) //nolint:gosec // repository file
		if err != nil {
			t.Fatal(err)
		}
		sc, err := scenario.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		scs = append(scs, sc)
	}
	fleet, err := scenario.NewFleet(scs, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var (
		volvos    []volvoapi.Source
		polestars []polestarapi.Source
		targets   []collector.Target
	)
	t0 := fleet.Start()
	for _, sim := range fleet.Simulations() {
		if sim.Brand() == scenario.BrandPolestar {
			polestars = append(polestars, sim)
			targets = append(targets, collector.Target{
				AccountID: "a", VehicleID: vehicleID("p", len(polestars)), VIN: sim.VIN(), ConnectionID: "pc",
				Provider: polestar.Source, CredentialSetAt: t0,
			})
			continue
		}
		volvos = append(volvos, sim)
		targets = append(targets, collector.Target{AccountID: "a", VehicleID: vehicleID("v", len(volvos)), VIN: sim.VIN(), ConnectionID: "c"})
	}
	clk := clock.NewManual(t0)
	cred := polestarapi.DevCredential()
	mux := http.NewServeMux()
	mux.Handle(polestarapi.Prefix, polestarapi.NewHandler(polestars, clk, cred, polestarapi.DefaultLimits(), polestarapi.DefaultBehavior()))
	mux.Handle("/", volvoapi.NewHandler(volvos, clk, volvoapi.DefaultLimits(), volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	st := newMemStore(targets...)
	d := New(st, Sources(volvo.Normalizer{}, polestar.Normalizer{}), core.DefaultParams(), fleetCatalog{}, quiet)
	c := collector.New(volvo.NewClient(srv.URL, "key", srv.Client()), st, fixedToken{}, clk, collector.DefaultIntervals(),
		collector.DefaultQuota(), quiet, nil, d)
	ps := polestarsource.New(polestar.NewClient(srv.URL+polestarapi.Prefix, srv.Client()),
		func(context.Context, string, string) (polestar.Credential, error) {
			return polestar.Credential{ClientID: cred.ClientID, ClientSecret: cred.ClientSecret, AccountID: cred.AccountID}, nil
		})
	c.AddSource(ps, st)
	day := t0.Truncate(24 * time.Hour)
	for clk.Now().Before(day.Add(until)) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
	return st, d, day
}

func vehicleID(prefix string, n int) string {
	if n == 1 {
		return prefix
	}
	return prefix + strconv.Itoa(n)
}

// fleetCatalog is simCatalog, and knows a Polestar by its brand alone, as the catalog
// knows the variant its user chose (the Data Portal tells no family): 79 kWh, the
// usable capacity of polestar2.
type fleetCatalog struct{}

func (fleetCatalog) NetCapacity(ctx context.Context, accountID, vehicleID string) (core.NetCapacity, error) {
	sim, _ := simCatalog{}.NetCapacity(ctx, accountID, vehicleID)
	return func(n core.Snapshot) (float64, bool) {
		if n.Brand.V == polestar.Brand && !n.Family.OK {
			return 79, true
		}
		return sim(n)
	}, nil
}

// Where the demonstration Polestar goes, and the net capacity its derivation rests on.
var (
	psHome    = core.Position{Lat: 57.6890, Lon: 11.9746}
	psWork    = core.Position{Lat: 57.7210, Lon: 12.0400}
	psCharger = core.Position{Lat: 57.7075, Lon: 11.9668}
	psNet     = core.Value[core.Capacity]{V: core.Capacity{KWh: 79, Source: core.CapacityCatalogNet}, OK: true}
)

// TestPolestarDay: the demonstration Polestar's day, read through the Data Portal: home
// → work 07:00–07:22 (9 km), work → a fast charger 16:22–16:37 (6 km), a DC charge to 80
// %, the charger → home (4 km), an AC charge to 90 % an hour later. The car uploads its
// position at the end of a trip only, and its battery and usage mode every 10 minutes
// while parked: the trips are observed, not reconstructed, and the incremental
// derivation matches a rebuild.
func TestPolestarDay(t *testing.T) {
	st, d, day := playFleet(t, 21*time.Hour, "polestar2.yaml")
	checkPolestarDay(t, st, day)
	checkRebuildOf(t, st, d, "p")
}

func checkPolestarDay(t *testing.T, st *memStore, day time.Time) {
	t.Helper()
	v := st.vehicle("p")
	h := func(hh, mm int) time.Time {
		return day.Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute)
	}
	if len(v.trips) != 3 {
		t.Fatalf("%d trips, want 3: %+v", len(v.trips), v.trips)
	}
	for i, want := range []struct {
		km         float64
		from, to   core.Position
		start, end time.Time
	}{
		{9, psHome, psWork, h(7, 0), h(7, 22)},
		{6, psWork, psCharger, h(16, 22), h(16, 37)},
		{4, psCharger, psHome, h(16, 44), h(16, 54)},
	} {
		tr := v.trips[i]
		if tr.Reconstructed || !slices.Equal(tr.Sources, []string{polestar.Source}) {
			t.Errorf("trip %d: reconstructed %v, sources %v", i, tr.Reconstructed, tr.Sources)
		}
		if !near(tr.DistanceKm, want.km, 0.5) || !at(tr.From, want.from.Lat, want.from.Lon) || !at(tr.To, want.to.Lat, want.to.Lon) {
			t.Errorf("trip %d: %+v km from %+v to %+v", i, tr.DistanceKm, tr.From, tr.To)
		}
		// Parked, the usage mode is read every 10 minutes; driving, every minute.
		if !within(want.start, tr.Start.After, tr.Start.Before) || !within(want.end, tr.End.After, tr.End.Before) ||
			tr.Start.Before.Sub(tr.Start.After) > 10*time.Minute || tr.End.Before.Sub(tr.End.After) > time.Minute {
			t.Errorf("trip %d: start %+v, end %+v; actual %v → %v", i, tr.Start, tr.End, want.start, want.end)
		}
		if !tr.EnergyKWh.OK || tr.Capacity != psNet {
			t.Errorf("trip %d: energy %+v, capacity %+v", i, tr.EnergyKWh, tr.Capacity)
		}
	}

	if len(v.charges) != 2 {
		t.Fatalf("%d charges, want 2: %+v", len(v.charges), v.charges)
	}
	for i, want := range []struct {
		typ      core.ChargeType
		at       core.Position
		start    time.Time
		from, to float64
	}{
		{core.DC, psCharger, h(16, 37), 60, 78},
		{core.AC, psHome, h(17, 54), 79, 90},
	} {
		c := v.charges[i]
		if c.Reconstructed || c.Type != core.Some(want.typ, c.Type.At) || !at(c.Position, want.at.Lat, want.at.Lon) {
			t.Errorf("charge %d: %+v at %+v, reconstructed %v", i, c.Type, c.Position, c.Reconstructed)
		}
		if !within(want.start, c.Start.After, c.Start.Before) || !near(c.StartSoC, want.from, 1) || !near(c.EndSoC, want.to, 1) {
			t.Errorf("charge %d: start %+v, SoC %+v → %+v", i, c.Start, c.StartSoC, c.EndSoC)
		}
		// The energy of the SoC on the variant's net capacity, and of the power read
		// every minute while charging.
		if !near(c.EnergySoCKWh, (want.to-want.from)*0.79, 1) || !c.EnergyPowerKWh.OK || c.Capacity != psNet {
			t.Errorf("charge %d: energy %+v (power %+v), capacity %+v", i, c.EnergySoCKWh, c.EnergyPowerKWh, c.Capacity)
		}
	}
}

// TestFleetOfBothBrands: a Volvo and a Polestar of one account, each read through its
// provider, derive the same events as alone.
func TestFleetOfBothBrands(t *testing.T) {
	st, d, day := playFleet(t, 23*time.Hour+30*time.Minute, "commute.yaml", "polestar2.yaml")
	checkPolestarDay(t, st, day)
	volvo := st.vehicle("v")
	if len(volvo.trips) != 2 || len(volvo.charges) != 1 {
		t.Fatalf("Volvo: %d trips, %d charges; want 2, 1", len(volvo.trips), len(volvo.charges))
	}
	for _, tr := range volvo.trips {
		if !slices.Equal(tr.Sources, []string{"volvo"}) || tr.Capacity != fromCatalog {
			t.Errorf("Volvo trip: sources %v, capacity %+v", tr.Sources, tr.Capacity)
		}
	}
	for _, id := range []string{"v", "p"} {
		for k := range st.vehicle(id).rows {
			if want := map[string]string{"v": "volvo", "p": polestar.Source}[id]; k.Source != want {
				t.Errorf("vehicle %s: a snapshot of %s", id, k.Source)
			}
		}
		checkRebuildOf(t, st, d, id)
	}
}

// TestFleetHealth: the health of both brands, read every rare interval: the Polestar's
// tyres with their pressures, the Volvo's with a warning each, the service and the checks
// of both; the Polestar's pressures over the day.
func TestFleetHealth(t *testing.T) {
	_, d, day := playFleet(t, 12*time.Hour, "commute.yaml", "polestar2.yaml")
	ctx, now := context.Background(), day.Add(12*time.Hour)

	p, err := d.Health(ctx, "a", "p", now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Covers != core.HealthTyres|core.HealthService || !p.Pressures() || p.RecommendedFrontKPa.V != 280 ||
		!p.DaysToService.OK || !p.Service.OK || p.Service.V != core.ServiceOK || len(p.Checks) != 24 ||
		now.Sub(p.CheckedAt) > time.Hour {
		t.Errorf("Polestar: %+v", p)
	}
	v, err := d.Health(ctx, "a", "v", now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Covers != core.HealthTyres|core.HealthService || v.Pressures() || len(v.Tyres) != 4 ||
		v.Tyres[core.RearLeft].Warning.V != core.TyreOK || !v.MonthsToService.OK || !v.DistanceToServiceKm.OK ||
		len(v.Checks) != 27 {
		t.Errorf("Volvo: %+v", v)
	}
	// Before the first pass, nothing is known.
	if h, err := d.Health(ctx, "a", "p", day); err != nil || h.Covers != 0 || !h.CheckedAt.IsZero() {
		t.Errorf("before: %+v, %v", h, err)
	}

	records, err := d.HealthReadings(ctx, "a", "p", day, now, 1000)
	if err != nil {
		t.Fatal(err)
	}
	runs := core.TyreRuns(records, day, now, 90*time.Minute)
	if len(runs) != 1 || len(runs[0]) < 2 {
		t.Fatalf("runs %+v", runs)
	}
	for _, r := range runs[0] {
		if !r.PressureKPa[core.FrontLeft].OK || !r.RecommendedRearKPa.OK {
			t.Errorf("reading %+v", r)
		}
	}
}
