package derive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// memStore is an in-memory collector.Store and Store, with the same deduplication and
// the same uniqueness of events as PostgreSQL.
type memStore struct {
	targets  []collector.Target
	vehicles map[string]*memVehicle
}

type memVehicle struct {
	rows    map[volvo.Endpoint][]Snapshot
	hashes  map[volvo.Endpoint][]byte
	trips   []core.Trip
	charges []core.Charge
	cursor  core.Cursor
}

func newMemStore(targets ...collector.Target) *memStore {
	return &memStore{targets: targets, vehicles: map[string]*memVehicle{}}
}

func (m *memStore) vehicle(id string) *memVehicle {
	v, ok := m.vehicles[id]
	if !ok {
		v = &memVehicle{rows: map[volvo.Endpoint][]Snapshot{}, hashes: map[volvo.Endpoint][]byte{}}
		m.vehicles[id] = v
	}
	return v
}

func (m *memStore) Targets(context.Context) ([]collector.Target, error) { return m.targets, nil }

func (m *memStore) SaveStatus(context.Context, collector.Status) error { return nil }

func (m *memStore) SaveCalls(context.Context, []collector.Calls) error { return nil }

func (m *memStore) Calls(context.Context, time.Time) ([]collector.Calls, error) { return nil, nil }

func (m *memStore) ConnectionKey(context.Context, string, string) (string, error) { return "", nil }

func (m *memStore) SetKeyRefused(context.Context, string, string, time.Time, bool) error { return nil }

func (m *memStore) SaveSnapshot(_ context.Context, s collector.Snapshot) (bool, error) {
	v := m.vehicle(s.VehicleID)
	rows := v.rows[s.Endpoint]
	if len(rows) > 0 && bytes.Equal(v.hashes[s.Endpoint], s.Hash) {
		rows[len(rows)-1].CheckedAt = s.FetchedAt
		return false, nil
	}
	v.hashes[s.Endpoint] = s.Hash
	v.rows[s.Endpoint] = append(rows, Snapshot{Endpoint: s.Endpoint, FetchedAt: s.FetchedAt, CheckedAt: s.FetchedAt, Payload: s.Payload})
	return true, nil
}

func (m *memStore) DerivationCursor(_ context.Context, _, vehicleID string) (core.Cursor, error) {
	return m.vehicle(vehicleID).cursor, nil
}

func (m *memStore) SnapshotsSince(_ context.Context, _, vehicleID string, from time.Time, endpoints []volvo.Endpoint) ([]Snapshot, error) {
	var out []Snapshot
	for _, ep := range endpoints {
		rows := m.vehicle(vehicleID).rows[ep]
		for i, r := range rows {
			lastBefore := !r.FetchedAt.After(from) && (i == len(rows)-1 || rows[i+1].FetchedAt.After(from))
			if lastBefore || r.FetchedAt.After(from) {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FetchedAt.Before(out[j].FetchedAt) })
	return out, nil
}

func (m *memStore) SnapshotsBetween(_ context.Context, _, vehicleID string, from, to time.Time, endpoints []volvo.Endpoint, limit int) ([]Snapshot, error) {
	var out []Snapshot
	for _, ep := range endpoints {
		for _, r := range m.vehicle(vehicleID).rows[ep] {
			if !r.FetchedAt.After(to) && !r.CheckedAt.Before(from) {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FetchedAt.Before(out[j].FetchedAt) })
	return out[:min(limit, len(out))], nil
}

func (m *memStore) SaveDerivation(_ context.Context, _, vehicleID string, after time.Time, res core.Result) error {
	v := m.vehicle(vehicleID)
	var trips []core.Trip
	for _, t := range v.trips {
		if !t.DetectedAt.After(after) {
			trips = append(trips, t)
		}
	}
	var charges []core.Charge
	for _, c := range v.charges {
		if !c.DetectedAt.After(after) {
			charges = append(charges, c)
		}
	}
	trips = append(trips, res.Trips...)
	charges = append(charges, res.Charges...)
	seen := map[time.Time]bool{}
	for _, t := range trips {
		if seen[t.DetectedAt] {
			return fmt.Errorf("duplicate trip detected at %v", t.DetectedAt)
		}
		seen[t.DetectedAt] = true
	}
	clear(seen)
	for _, c := range charges {
		if seen[c.DetectedAt] {
			return fmt.Errorf("duplicate charge detected at %v", c.DetectedAt)
		}
		seen[c.DetectedAt] = true
	}
	v.trips, v.charges, v.cursor = trips, charges, res.Cursor
	return nil
}

// play runs the collector against a scenario of the repository, with incremental
// derivation after each pass, until the given time past the scenario start day.
func play(t *testing.T, file string, until time.Duration) (*memStore, *Deriver, time.Time) {
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
	limits := volvoapi.DefaultLimits()
	if q := sc.API.DailyQuota; q != nil {
		limits.DailyQuota = *q
	}
	var faults volvoapi.Faults
	for _, o := range sc.API.Outages {
		faults.Outages = append(faults.Outages, volvoapi.Outage{From: sc.Start.Add(o.After), To: sc.Start.Add(o.After + o.Duration)})
	}
	clk := clock.NewManual(sc.Start)
	srv := httptest.NewServer(volvoapi.NewHandler([]volvoapi.Source{sim}, clk, limits, faults, volvoapi.DefaultOAuth()))
	t.Cleanup(srv.Close)

	st := newMemStore(collector.Target{AccountID: "a", VehicleID: "v", VIN: sc.VIN, ConnectionID: "c"})
	d := New(st, core.DefaultParams(), simCatalog{}, quiet)
	c := collector.New(volvo.NewClient(srv.URL, "key", srv.Client()), st, fixedToken{}, clk, collector.DefaultIntervals(), collector.DefaultQuota(), quiet, nil, d)
	day := sc.Start.Truncate(24 * time.Hour)
	for clk.Now().Before(day.Add(until)) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
	return st, d, day
}

// simCatalog knows the simulator's vehicle of 80 kWh, the capacity of most scenarios,
// with a net capacity equal to it: the energies stay those of the scenarios, and rest on
// the catalog. The 107 kWh of long-trip-dc is not in it.
type simCatalog struct{}

func (simCatalog) NetCapacity(context.Context, string, string) (core.NetCapacity, error) {
	return func(n core.Snapshot) (float64, bool) {
		return 80, n.Family.V == "EX-SIM" && n.CapacityKWh.V == 80
	}, nil
}

// fixedToken is a collector.TokenSource with a token that never expires.
type fixedToken struct{}

func (fixedToken) Token(context.Context, string, string) (string, error) { return "token", nil }

func (fixedToken) Refresh(context.Context, string, string, string) (string, error) {
	return "token", nil
}
func (fixedToken) KeepAlive(context.Context) error { return nil }

// checkRebuild checks that a rebuild from the whole history gives exactly the events
// derived incrementally, and that rebuilding again changes nothing.
func checkRebuild(t *testing.T, st *memStore, d *Deriver) {
	t.Helper()
	v := st.vehicle("v")
	trips, charges := v.trips, v.charges
	for range 2 {
		if _, err := d.Rebuild(context.Background(), "a", "v"); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(v.trips, trips) {
			t.Errorf("rebuilt trips differ:\nincremental %+v\nrebuilt     %+v", trips, v.trips)
		}
		if !reflect.DeepEqual(v.charges, charges) {
			t.Errorf("rebuilt charges differ:\nincremental %+v\nrebuilt     %+v", charges, v.charges)
		}
	}
}

func near(v core.Value[float64], want, tol float64) bool { return v.OK && math.Abs(v.V-want) <= tol }

func at(p core.Value[core.Position], lat, lon float64) bool {
	return p.OK && math.Abs(p.V.Lat-lat) < 1e-6 && math.Abs(p.V.Lon-lon) < 1e-6
}

func within(ts, from, to time.Time) bool { return !ts.Before(from) && !ts.After(to) }

var (
	home = core.Position{Lat: 45.7640, Lon: 4.8357}
	work = core.Position{Lat: 45.7797, Lon: 4.9270}
)

func logEvents(t *testing.T, st *memStore) {
	t.Helper()
	v := st.vehicle("v")
	for _, tr := range v.trips {
		t.Logf("trip: %+v", tr)
	}
	for _, c := range v.charges {
		t.Logf("charge: %+v", c)
	}
}

// TestCommute: home → work 07:00–07:40 (32 km), work → home 16:40–17:25 (33 km), AC
// charge from 18:25 up to 80 %.
func TestCommute(t *testing.T) {
	st, d, day := play(t, "commute.yaml", 23*time.Hour+30*time.Minute)
	logEvents(t, st)
	v := st.vehicle("v")
	h := func(hh, mm int) time.Time {
		return day.Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute)
	}

	if len(v.trips) != 2 {
		t.Fatalf("%d trips, want 2", len(v.trips))
	}
	for i, want := range []struct {
		km               float64
		from, to         core.Position
		start, end       time.Time
		startSoC, endSoC float64
	}{
		{32, home, work, h(7, 0), h(7, 40), 62, 54},
		{33, work, home, h(16, 40), h(17, 25), 54, 47},
	} {
		tr := v.trips[i]
		if tr.Reconstructed {
			t.Errorf("trip %d reconstructed", i)
		}
		if !near(tr.DistanceKm, want.km, 1) {
			t.Errorf("trip %d: distance %+v, want ≈ %v", i, tr.DistanceKm, want.km)
		}
		if !at(tr.From, want.from.Lat, want.from.Lon) || !at(tr.To, want.to.Lat, want.to.Lon) {
			t.Errorf("trip %d: from %+v to %+v", i, tr.From, tr.To)
		}
		if !within(want.start, tr.Start.After, tr.Start.Before) || !within(want.end, tr.End.After, tr.End.Before) {
			t.Errorf("trip %d: start %+v, end %+v; actual %v → %v", i, tr.Start, tr.End, want.start, want.end)
		}
		// Parked, the engine is read every 10 minutes; driving, every minute.
		if tr.Start.Before.Sub(tr.Start.After) > 10*time.Minute || tr.End.Before.Sub(tr.End.After) > 2*time.Minute {
			t.Errorf("trip %d: bounds too wide: %+v, %+v", i, tr.Start, tr.End)
		}
		if !near(tr.StartSoC, want.startSoC, 1) || !near(tr.EndSoC, want.endSoC, 1) {
			t.Errorf("trip %d: SoC %+v → %+v", i, tr.StartSoC, tr.EndSoC)
		}
		// 18 kWh/100 km: 5.8 and 5.9 kWh, within a SoC point (0.8 kWh) of rounding.
		if !near(tr.EnergyKWh, want.km*0.18, 1) {
			t.Errorf("trip %d: energy %+v", i, tr.EnergyKWh)
		}
		if !near(tr.TripMeterKm, want.km, 1) || !tr.ConsumptionKWhPer100km.OK {
			t.Errorf("trip %d: trip meter %+v, consumption %+v", i, tr.TripMeterKm, tr.ConsumptionKWhPer100km)
		}
		if tr.Capacity != fromCatalog {
			t.Errorf("trip %d: capacity %+v", i, tr.Capacity)
		}
	}

	if len(v.charges) != 1 {
		t.Fatalf("%d charges, want 1", len(v.charges))
	}
	c := v.charges[0]
	if c.Reconstructed || !c.Type.OK || c.Type.V != core.AC {
		t.Errorf("charge type %+v, reconstructed %v", c.Type, c.Reconstructed)
	}
	if !near(c.EndSoC, 80, 0) || !near(c.TargetSoC, 80, 0) || !near(c.StartSoC, 47, 1) {
		t.Errorf("charge SoC %+v → %+v, target %+v", c.StartSoC, c.EndSoC, c.TargetSoC)
	}
	if !within(h(18, 25), c.Start.After, c.Start.Before) || !at(c.Position, home.Lat, home.Lon) {
		t.Errorf("charge start %+v at %+v", c.Start, c.Position)
	}
	// 33 points of 80 kWh: 26.4 kWh. The power integral misses the time before the
	// charge was seen (up to one parked interval): it is lower, never higher.
	if !near(c.EnergySoCKWh, 26.4, 0.8) || c.Capacity != fromCatalog {
		t.Errorf("energy by SoC: %+v, capacity %+v", c.EnergySoCKWh, c.Capacity)
	}
	if !c.EnergyPowerKWh.OK || c.EnergyPowerKWh.V > c.EnergySoCKWh.V+0.8 || c.EnergyPowerKWh.V < c.EnergySoCKWh.V-7.4*10/60-0.8 {
		t.Errorf("energy by power: %+v, by SoC %+v", c.EnergyPowerKWh, c.EnergySoCKWh)
	}
	// The span covers the whole integral (the charge stays below MaxSpanSoC); the
	// odometer is the one the readings knew at its end.
	if !c.Span.OK || math.Abs(c.Span.V.EnergyKWh-c.EnergyPowerKWh.V) > 0.1 {
		t.Errorf("span %+v of the integral %+v", c.Span, c.EnergyPowerKWh)
	}
	if !near(c.OdometerKm, 12465, 1) {
		t.Errorf("odometer %+v", c.OdometerKm)
	}
	checkRebuild(t, st, d)
}

var fromCatalog = core.Value[core.Capacity]{V: core.Capacity{KWh: 80, Source: core.CapacityCatalogNet}, OK: true}

// TestMissedTrip: the API is down for the whole trip. The odometer and the position read
// afterwards reveal a reconstructed trip, home → work.
func TestMissedTrip(t *testing.T) {
	st, d, day := play(t, "missed-trip.yaml", 21*time.Hour)
	logEvents(t, st)
	v := st.vehicle("v")
	if len(v.trips) != 1 || len(v.charges) != 0 {
		t.Fatalf("%d trips, %d charges; want 1 trip", len(v.trips), len(v.charges))
	}
	tr := v.trips[0]
	if !tr.Reconstructed || !near(tr.DistanceKm, 32, 1) {
		t.Errorf("reconstructed %v, distance %+v", tr.Reconstructed, tr.DistanceKm)
	}
	if !at(tr.From, home.Lat, home.Lon) || !at(tr.To, work.Lat, work.Lon) {
		t.Errorf("from %+v to %+v", tr.From, tr.To)
	}
	// Not seen: somewhere between the last reading before the outage and the arrival
	// timestamp of the position.
	if tr.Start != tr.End || !within(day.Add(7*time.Hour), tr.Start.After, tr.Start.Before) ||
		!within(day.Add(7*time.Hour+40*time.Minute), tr.Start.After, tr.Start.Before) {
		t.Errorf("bounds %+v, %+v", tr.Start, tr.End)
	}
	if !near(tr.StartSoC, 62, 0) || !near(tr.EndSoC, 55, 1) || !tr.EnergyKWh.OK {
		t.Errorf("SoC %+v → %+v, energy %+v", tr.StartSoC, tr.EndSoC, tr.EnergyKWh)
	}
	if tr.TripMeterKm.OK {
		t.Error("trip meter attached to a reconstructed trip")
	}
	checkRebuild(t, st, d)
}

// TestLongTripDC: Lyon → service area (280 km), DC charge up to 80 %, service area →
// Marseille (180 km). The ex90-like profile uploads no charging power.
func TestLongTripDC(t *testing.T) {
	st, d, _ := play(t, "long-trip-dc.yaml", 21*time.Hour)
	logEvents(t, st)
	v := st.vehicle("v")
	if len(v.trips) != 2 || len(v.charges) != 1 {
		t.Fatalf("%d trips, %d charges; want 2 and 1", len(v.trips), len(v.charges))
	}
	for i, km := range []float64{280, 180} {
		if tr := v.trips[i]; tr.Reconstructed || !near(tr.DistanceKm, km, 1) {
			t.Errorf("trip %d: reconstructed %v, distance %+v", i, tr.Reconstructed, tr.DistanceKm)
		}
	}
	// The vehicle leaves the second the charge is done: the next reading already shows
	// 79 %, and nothing says 80 was reached but the target.
	c := v.charges[0]
	if c.Reconstructed || !c.Type.OK || c.Type.V != core.DC || !near(c.EndSoC, 80, 1) || !near(c.TargetSoC, 80, 0) {
		t.Errorf("charge %+v", c)
	}
	if !c.EnergySoCKWh.OK || c.EnergyPowerKWh.OK {
		t.Errorf("energy by SoC %+v, by power %+v: only the SoC estimate can exist", c.EnergySoCKWh, c.EnergyPowerKWh)
	}
	// A vehicle the catalog does not know: the API's capacity.
	if want := (core.Capacity{KWh: 107, Source: core.CapacityAPI}); c.Capacity.V != want || v.trips[0].Capacity.V != want {
		t.Errorf("capacities %+v, %+v", c.Capacity, v.trips[0].Capacity)
	}
	// The trip ends where the charge starts: its SoC is the arrival SoC, not the charged one.
	if !near(v.trips[0].EndSoC, c.StartSoC.V, 1) || !c.Start.After.Before(v.trips[1].Start.Before) {
		t.Errorf("trip 1 ends at %+v, charge starts at %+v", v.trips[0].EndSoC, c.StartSoC)
	}
	checkRebuild(t, st, d)
}

// TestReadings reads the energy states of the commute's AC charge: each read within its
// bounds, in order, the power at the charger's while it charges.
func TestReadings(t *testing.T) {
	st, d, _ := play(t, "commute.yaml", 23*time.Hour+30*time.Minute)
	c := st.vehicle("v").charges[0]
	got, err := d.Readings(context.Background(), "a", "v", c.Start.After, c.End.Before, 10_000)
	if err != nil || len(got) < 10 {
		t.Fatalf("%d readings, %v", len(got), err)
	}
	var peak float64
	for i, r := range got {
		if r.Snapshot.Covers&core.FieldSoC == 0 || r.CheckedAt.Before(c.Start.After) || r.FetchedAt.After(c.End.Before) {
			t.Errorf("reading %d: %+v", i, r)
		}
		if i > 0 && !got[i-1].FetchedAt.Before(r.FetchedAt) {
			t.Errorf("reading %d out of order", i)
		}
		if r.Snapshot.PowerW.OK {
			peak = max(peak, r.Snapshot.PowerW.V)
		}
	}
	if peak < 7000 || peak > 7400 {
		t.Errorf("peak power %v W: the charge's 7.4 kW", peak)
	}
	if few, _ := d.Readings(context.Background(), "a", "v", c.Start.After, c.End.Before, 3); len(few) != 3 {
		t.Errorf("limit 3: %d readings", len(few))
	}
}

// TestQuotaExhausted documents what survives a quota of 150 calls per day and per API.
//
// Connected Vehicle (engine, odometer) runs out late in the morning and Energy during
// the charge; Location never does. The morning trip is observed. Until the quotas reset at
// midnight UTC, the return trip is invisible (no odometer) and the charge stays open
// (its end is not seen). After midnight the odometer reveals the return trip,
// reconstructed from work to home, and the end of the charge is read.
func TestQuotaExhausted(t *testing.T) {
	st, _, day := play(t, "quota-exhausted.yaml", 21*time.Hour)
	logEvents(t, st)
	v := st.vehicle("v")
	if len(v.trips) != 1 || v.trips[0].Reconstructed || !near(v.trips[0].DistanceKm, 32, 1) {
		t.Fatalf("at 21:00: trips %+v, want the observed morning trip only", v.trips)
	}
	if len(v.charges) != 0 {
		t.Fatalf("at 21:00: charges %+v, want none (still open)", v.charges)
	}

	st, d, _ := play(t, "quota-exhausted.yaml", 25*time.Hour)
	logEvents(t, st)
	v = st.vehicle("v")
	if len(v.trips) != 2 || len(v.charges) != 1 {
		t.Fatalf("after midnight: %d trips, %d charges; want 2 and 1", len(v.trips), len(v.charges))
	}
	back := v.trips[1]
	if !back.Reconstructed || !near(back.DistanceKm, 33, 1) || !at(back.From, work.Lat, work.Lon) || !at(back.To, home.Lat, home.Lon) {
		t.Errorf("return trip %+v", back)
	}
	// Bounded by the last odometer reading of the morning and the arrival timestamp of
	// the position, read by the Location API all day long.
	if !within(day.Add(16*time.Hour+40*time.Minute), back.Start.After, back.End.Before) || back.End.Before.After(day.Add(17*time.Hour+30*time.Minute)) {
		t.Errorf("return trip bounds %+v", back.End)
	}
	// Its SoC ends before the charge, not after.
	if !near(back.EndSoC, 47, 1) {
		t.Errorf("return trip SoC %+v → %+v", back.StartSoC, back.EndSoC)
	}
	c := v.charges[0]
	if c.Reconstructed || !near(c.EndSoC, 80, 0) || !near(c.EnergySoCKWh, 26.4, 0.8) {
		t.Errorf("charge %+v", c)
	}
	// The end of the charge was not seen: its bounds span the blocked period, narrowed by
	// the vehicle timestamp of the "done" status.
	if c.End.Before.After(day.Add(22*time.Hour)) || c.End.After.After(day.Add(20*time.Hour)) {
		t.Errorf("charge end %+v", c.End)
	}
	if !c.EnergyPowerKWh.OK || c.EnergyPowerKWh.V >= c.EnergySoCKWh.V {
		t.Errorf("power estimate %+v covers only the observed part", c.EnergyPowerKWh)
	}
	checkRebuild(t, st, d)
}

// TestCurrent: at the end of the commute day, the vehicle is parked at home, charged
// to 80 %.
func TestCurrent(t *testing.T) {
	_, d, day := play(t, "commute.yaml", 23*time.Hour)
	now := day.Add(23 * time.Hour)
	c, err := d.Current(context.Background(), "a", "v", now)
	if err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot
	if !near(s.SoC, 80, 0) || !s.Engine.OK || s.Engine.V != core.EngineStopped || !at(s.Position, home.Lat, home.Lon) ||
		!near(s.OdometerKm, 12465, 1) {
		t.Errorf("current = %+v", s)
	}
	if fetched, checked := c.Read(core.FieldSoC); fetched.After(checked) || checked.After(now) || now.Sub(checked) > 10*time.Minute {
		t.Errorf("SoC read at %s, checked at %s", fetched, checked)
	}
	if !c.CheckedAt().After(now.Add(-10 * time.Minute)) {
		t.Errorf("checked at %s", c.CheckedAt())
	}
	// Before any snapshot: nothing known.
	if c, err := d.Current(context.Background(), "a", "v", day); err != nil || c.Snapshot.Covers != 0 {
		t.Errorf("before the first pass: %+v, %v", c.Snapshot, err)
	}
	if _, err := New(&scriptedStore{snapErr: errors.New("boom")}, core.DefaultParams(), nil, quiet).Current(context.Background(), "a", "v", now); err == nil {
		t.Error("store error ignored")
	}
}

// TestEX30CapacityEstimate: the EX30's SoC runs over its usable 64 kWh, and the power
// it reports is the one entering the battery: the power estimate of its AC charge
// comes back at the usable capacity, within the rounding of an integer SoC at both
// ends of the span.
// TestBatteryScenario: the battery scenario gives the battery page its current capacity,
// one estimate per charge, each near the usable 64 kWh.
func TestBatteryScenario(t *testing.T) {
	st, d, _ := play(t, "battery.yaml", 30*time.Hour)
	v := st.vehicle("v")
	checkRebuild(t, st, d)
	reference := core.Value[core.Capacity]{V: core.Capacity{KWh: 64, Source: core.CapacityCatalogNet}, OK: true}
	got, err := core.CapacityTrend(v.charges, nil, v.trips, reference, core.DefaultParams(), core.DefaultCapacityParams(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Estimates) != 6 || !got.Current.OK || got.Excluded != (core.CapacityExcluded{}) {
		t.Fatalf("%d estimates, current %+v, excluded %+v: want six, and a current capacity", len(got.Estimates), got.Current, got.Excluded)
	}
	for _, e := range got.Estimates {
		if math.Abs(e.CapacityKWh-64) > 64*0.03 {
			t.Errorf("estimate %.2f kWh, want 64 within 3 %%", e.CapacityKWh)
		}
	}
}

func TestEX30CapacityEstimate(t *testing.T) {
	st, d, _ := play(t, "ex30.yaml", 23*time.Hour+30*time.Minute)
	v := st.vehicle("v")
	if len(v.charges) != 1 || !v.charges[0].Span.OK {
		t.Fatalf("charges %+v, want one with a span", v.charges)
	}
	checkRebuild(t, st, d)
	reference := core.Value[core.Capacity]{V: core.Capacity{KWh: 64, Source: core.CapacityCatalogNet}, OK: true}
	got, err := core.CapacityTrend(v.charges, nil, v.trips, reference, core.DefaultParams(), core.DefaultCapacityParams(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Estimates) != 1 || got.Estimates[0].Source != core.EstimatePower || got.Excluded != (core.CapacityExcluded{}) {
		t.Fatalf("estimates %+v, excluded %+v, want the power one", got.Estimates, got.Excluded)
	}
	if kwh := got.Estimates[0].CapacityKWh; math.Abs(kwh-64) > 64*0.03 {
		t.Errorf("estimate %.2f kWh, want 64 within 3 %%", kwh)
	}
}
