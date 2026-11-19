package derive

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/collector/demosource"
	"runsten/internal/core"
	"runsten/internal/demo"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

// demoNet is the net capacity of the demo vehicle's variant, as the catalog gives it.
func demoNet(n core.Snapshot) (float64, bool) { return 64, n.Family.V == "EX30" }

type demoCatalog struct{}

func (demoCatalog) NetCapacity(context.Context, string, string) (core.NetCapacity, error) {
	return demoNet, nil
}

func demoTarget(t0 time.Time) collector.Target {
	return collector.Target{
		AccountID: "a", VehicleID: "d", VIN: demo.VIN, ConnectionID: "dc", Provider: demo.Source, CredentialSetAt: t0,
	}
}

// demoDeriver is a Deriver of the demo's responses over st.
func demoDeriver(st *memStore, caps Capacities) *Deriver {
	return New(st, Sources(volvo.Normalizer{}, demo.Normalizer{}), core.DefaultParams(), caps, quiet)
}

// writeDemoHistory saves the demo's readings from from to to as the history written at
// connection does, one after the other, as the collector would have.
func writeDemoHistory(t *testing.T, st *memStore, d *demo.Demo, from, to time.Time) {
	t.Helper()
	samples, err := d.History(from, to)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range samples {
		if _, err := st.SaveSnapshot(context.Background(), collector.Snapshot{
			AccountID: "a", VehicleID: "d", Source: demo.Source, Endpoint: collector.Endpoint(demo.Vehicle),
			FetchedAt: s.At, Payload: s.Payload, Hash: s.Hash,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// inWeek returns the trips and charges that start in the week that starts at monday.
func inWeek(trips []core.Trip, charges []core.Charge, monday time.Time) ([]core.Trip, []core.Charge) {
	var ts []core.Trip
	var cs []core.Charge
	for _, tr := range trips {
		if !tr.Start.After.Before(monday) && tr.Start.After.Before(monday.Add(demo.Week)) {
			ts = append(ts, tr)
		}
	}
	for _, c := range charges {
		if !c.Start.After.Before(monday) && c.Start.After.Before(monday.Add(demo.Week)) {
			cs = append(cs, c)
		}
	}
	return ts, cs
}

// TestDemoHistory: four weeks of the demo vehicle, written after the fact and derived from
// scratch, give each week's trips and charges: the commutes and errands, the Saturday's
// two long drives with its fast charge, and the home charges, with the energies the net
// capacity gives.
func TestDemoHistory(t *testing.T) {
	week, err := demo.New()
	if err != nil {
		t.Fatal(err)
	}
	to := demo.Origin().Add(30*demo.Week + 3*24*time.Hour + 14*time.Hour)
	st := newMemStore(demoTarget(to))
	writeDemoHistory(t, st, week, to.Add(-demo.HistoryDays*24*time.Hour), to)
	// The API derives it with the net capacity it gives (RebuildWith), the collector with its
	// catalog's (Rebuild): the same events.
	d := demoDeriver(st, demoCatalog{})
	if _, err := d.RebuildWith(context.Background(), "a", "d", demoNet); err != nil {
		t.Fatal(err)
	}
	v := st.vehicle("d")
	// A full week of the four, the second: 17 drives (ten commutes, one more on Wednesday,
	// two on Thursday, the weekend's four) and 8 charges: the home ones on Monday, Tuesday,
	// Wednesday, Thursday (twice), Saturday and Sunday, and the fast one on Saturday.
	monday := demo.Origin().Add(28 * demo.Week)
	trips, charges := inWeek(v.trips, v.charges, monday)
	if len(trips) != 17 {
		t.Errorf("%d trips in the week, want 17", len(trips))
	}
	var km float64
	long := 0
	for _, tr := range trips {
		km += tr.DistanceKm.V
		if tr.DistanceKm.V > 100 {
			long++
		}
	}
	if long != 2 || km < week.WeekKm()-5 || km > week.WeekKm()+5 {
		t.Errorf("%d long drives, %.1f km a week, want 2 and %.1f", long, km, week.WeekKm())
	}
	for _, tr := range trips {
		// An EX30 consumes 17.5 kWh per 100 km on the simulator's model; the SoC read to a
		// tenth and the net capacity give it to within a few percent, a short trip less.
		per100 := tr.EnergyKWh.V / tr.DistanceKm.V * 100
		t.Logf("trip   %s %6.1f km  %4.1f to %4.1f %%  %5.2f kWh  %5.1f kWh/100 km", tr.Start.After.Format("Mon 15:04"), tr.DistanceKm.V, tr.StartSoC.V, tr.EndSoC.V, tr.EnergyKWh.V, per100)
		if tr.DistanceKm.V >= 10 && (per100 < 15 || per100 > 20) {
			t.Errorf("trip of %v: %.1f kWh/100 km", tr.Start.After, per100)
		}
	}
	var ac, dc int
	for _, c := range charges {
		bySoC, byPower := c.EnergySoCKWh.V, c.EnergyPowerKWh.V
		t.Logf("charge %s %s %4.1f to %4.1f %%  by SoC %5.2f kWh, by power %5.2f kWh", c.Start.After.Format("Mon 15:04"), c.Type.V, c.StartSoC.V, c.EndSoC.V, bySoC, byPower)
		if c.EnergySoCKWh.OK && c.EnergyPowerKWh.OK && math.Abs(byPower-bySoC) > 0.1*bySoC {
			t.Errorf("charge of %v: %.2f kWh by power, %.2f by SoC", c.Start.After, byPower, bySoC)
		}
		switch {
		case c.Type.OK && c.Type.V == core.DC:
			dc++
			if !near(c.EndSoC, 80, 3) || c.StartSoC.V > 40 {
				t.Errorf("the fast charge: %.1f to %.1f %%", c.StartSoC.V, c.EndSoC.V)
			}
		case c.Type.OK && c.Type.V == core.AC:
			ac++
			if !near(c.EndSoC, 80, 1) {
				t.Errorf("a home charge: %.1f to %.1f %%", c.StartSoC.V, c.EndSoC.V)
			}
		default:
			t.Errorf("a charge of no type at %v", c.DetectedAt)
		}
		if !c.EnergyPowerKWh.OK {
			t.Errorf("a charge without its energy at %v", c.DetectedAt)
		}
	}
	if dc != 1 || ac != 7 {
		t.Errorf("%d AC and %d DC charges in the week, want 7 and 1", ac, dc)
	}
	checkRebuildOf(t, st, d, "d")
}

// TestDemoLiveAgreesWithHistory: the collector reading the demo vehicle live, a day at a
// time, derives the same trips and charges as the history written after the fact for the
// same days: the simulation depends on the instant alone.
func TestDemoLiveAgreesWithHistory(t *testing.T) {
	// Each window ends with the vehicle parked, charged or not charging: no event is open.
	for _, tt := range []struct {
		name  string
		week  time.Duration // from the origin
		until time.Duration // from the week's Monday
	}{
		{"to Saturday's dawn", 12 * demo.Week, 5*24*time.Hour + 6*time.Hour},
		{"to Friday evening, another week", 40 * demo.Week, 4*24*time.Hour + 20*time.Hour},
	} {
		t.Run(tt.name, func(t *testing.T) {
			liveAgreesWithHistory(t, demo.Origin().Add(tt.week), demo.Origin().Add(tt.week+tt.until))
		})
	}
}

func liveAgreesWithHistory(t *testing.T, from, to time.Time) {
	t.Helper()
	week, err := demo.New()
	if err != nil {
		t.Fatal(err)
	}

	past := newMemStore(demoTarget(from))
	writeDemoHistory(t, past, week, from, to)
	hd := demoDeriver(past, nil)
	if _, err := hd.RebuildWith(context.Background(), "a", "d", demoNet); err != nil {
		t.Fatal(err)
	}

	live := newMemStore(demoTarget(from))
	clk := clock.NewManual(from)
	ld := demoDeriver(live, demoCatalog{})
	api := httptest.NewServer(volvoapi.NewHandler(nil, clk, volvoapi.DefaultLimits(), volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	t.Cleanup(api.Close)
	c := collector.New(volvo.NewClient(api.URL, "key", http.DefaultClient), live, fixedToken{}, clk, collector.DefaultIntervals(),
		collector.DefaultQuota(), quiet, nil, ld)
	c.AddSource(demosource.New(week, clk), live)
	for clk.Now().Before(to) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}

	hv, lv := past.vehicle("d"), live.vehicle("d")
	if len(lv.trips) < 8 || len(lv.charges) < 4 {
		t.Fatalf("%d trips and %d charges derived live", len(lv.trips), len(lv.charges))
	}
	// The window ends at the Saturday's dawn, the vehicle parked, charged on Friday evening
	// or not at all: no event is open at its end, and each one the history has the live
	// reads have too.
	if len(hv.trips) != len(lv.trips) || len(hv.charges) != len(lv.charges) {
		t.Fatalf("history: %d trips and %d charges, live: %d and %d", len(hv.trips), len(hv.charges), len(lv.trips), len(lv.charges))
	}
	for i := range lv.trips {
		h, l := hv.trips[i], lv.trips[i]
		// The two read at their own instants, each at most a parked pace from the other's.
		if !near(l.DistanceKm, h.DistanceKm.V, 0.2) || l.Start.After.Sub(h.Start.After).Abs() > demo.ParkedEvery {
			t.Errorf("trip %d: live %.1f km after %v, history %.1f km after %v", i, l.DistanceKm.V, l.Start.After, h.DistanceKm.V, h.Start.After)
		}
	}
	for i := range lv.charges {
		h, l := hv.charges[i], lv.charges[i]
		if l.Type != h.Type || !near(l.EndSoC, h.EndSoC.V, 0.5) || !near(l.StartSoC, h.StartSoC.V, 1) {
			t.Errorf("charge %d: live %v %.1f to %.1f, history %v %.1f to %.1f", i, l.Type.V, l.StartSoC.V, l.EndSoC.V, h.Type.V, h.StartSoC.V, h.EndSoC.V)
		}
	}
	checkRebuildOf(t, live, ld, "d")
}
