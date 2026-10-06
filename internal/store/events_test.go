package store

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/derive"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

func some(v float64) core.Value[float64] { return core.Value[float64]{V: v, OK: true} }

func sampleResult(at time.Time) core.Result {
	return core.Result{
		Trips: []core.Trip{{
			DetectedAt: at, Start: core.Bounds{After: at.Add(-10 * time.Minute), Before: at},
			End:             core.Bounds{After: at.Add(40 * time.Minute), Before: at.Add(41 * time.Minute)},
			StartOdometerKm: some(12400), EndOdometerKm: some(12432), DistanceKm: some(32),
			StartSoC: some(62), EndSoC: some(54), EnergyKWh: some(6.4),
			Capacity:    core.Value[core.Capacity]{V: core.Capacity{KWh: 80, Source: core.CapacityCatalogNet}, OK: true},
			From:        core.Value[core.Position]{V: core.Position{Lat: 45.764, Lon: 4.8357}, OK: true},
			TripMeterKm: some(32),
		}},
		Charges: []core.Charge{{
			DetectedAt: at.Add(time.Hour), Reconstructed: true,
			Start:    core.Bounds{After: at.Add(50 * time.Minute), Before: at.Add(70 * time.Minute)},
			End:      core.Bounds{After: at.Add(50 * time.Minute), Before: at.Add(70 * time.Minute)},
			Type:     core.Value[core.ChargeType]{V: core.DC, OK: true},
			StartSoC: some(54), EndSoC: some(80), EnergySoCKWh: some(20.8),
			Capacity: core.Value[core.Capacity]{V: core.Capacity{KWh: 80, Source: core.CapacityAPI}, OK: true},
			Position: core.Value[core.Position]{V: core.Position{Lat: 45.7797, Lon: 4.927}, OK: true},
		}},
		Cursor: core.Cursor{Settled: at.Add(2 * time.Hour), From: at},
	}
}

func TestSaveDerivation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")

	if c, err := s.DerivationCursor(ctx, a, v); err != nil || c != (core.Cursor{}) {
		t.Fatalf("initial cursor %+v, %v", c, err)
	}
	res := sampleResult(t0)
	for range 2 { // saving the same derivation again replaces it
		if err := s.SaveDerivation(ctx, a, v, time.Time{}, res); err != nil {
			t.Fatal(err)
		}
	}
	trips, err := s.Trips(ctx, a, v)
	if err != nil || !reflect.DeepEqual(trips, res.Trips) {
		t.Errorf("trips = %+v, %v\nwant %+v", trips, err, res.Trips)
	}
	charges, err := s.Charges(ctx, a, v)
	if err != nil || !reflect.DeepEqual(charges, res.Charges) {
		t.Errorf("charges = %+v, %v\nwant %+v", charges, err, res.Charges)
	}
	if c, err := s.DerivationCursor(ctx, a, v); err != nil || c != res.Cursor {
		t.Errorf("cursor %+v, %v", c, err)
	}

	// Incremental: the events detected after the cursor are replaced, the others kept.
	later := sampleResult(t0.Add(24 * time.Hour))
	if err := s.SaveDerivation(ctx, a, v, t0.Add(30*time.Minute), later); err != nil {
		t.Fatal(err)
	}
	trips, _ = s.Trips(ctx, a, v)
	charges, _ = s.Charges(ctx, a, v)
	if len(trips) != 2 || len(charges) != 1 || !charges[0].DetectedAt.Equal(later.Charges[0].DetectedAt) {
		t.Errorf("after incremental save: %d trips, charges %+v", len(trips), charges)
	}

	// A duplicate event is refused, and the transaction leaves nothing behind.
	dup := later
	dup.Trips = append(dup.Trips, dup.Trips[0])
	if err := s.SaveDerivation(ctx, a, v, t0.Add(30*time.Minute), dup); err == nil {
		t.Error("duplicate trip accepted")
	}
	if trips, _ := s.Trips(ctx, a, v); len(trips) != 2 {
		t.Errorf("failed save changed the trips: %d", len(trips))
	}

	// Bounds are checked by the database.
	bad := sampleResult(t0.Add(48 * time.Hour))
	bad.Trips[0].End.Before = bad.Trips[0].End.After.Add(-time.Minute)
	if err := s.SaveDerivation(ctx, a, v, t0.Add(47*time.Hour), bad); err == nil {
		t.Error("inverted bounds accepted")
	}
}

func TestSnapshotsSince(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	save := func(ep volvo.Endpoint, at time.Time, raw string) {
		t.Helper()
		h, _ := volvo.Fingerprint([]byte(raw))
		if _, err := s.SaveSnapshot(ctx, collector.Snapshot{AccountID: a, VehicleID: v, Endpoint: ep, FetchedAt: at, Payload: []byte(raw), Hash: h}); err != nil {
			t.Fatal(err)
		}
	}
	save(volvo.Details, t0, `{"data":{"batteryCapacityKWH":80}}`)
	save(volvo.Odometer, t0, `{"v":1}`)
	save(volvo.Odometer, t0.Add(time.Hour), `{"v":2}`)
	save(volvo.Odometer, t0.Add(2*time.Hour), `{"v":2}`) // checked again
	save(volvo.Odometer, t0.Add(3*time.Hour), `{"v":3}`)
	save(volvo.Doors, t0.Add(3*time.Hour), `{}`)

	got, err := s.SnapshotsSince(ctx, a, v, t0.Add(time.Hour), []volvo.Endpoint{volvo.Odometer, volvo.Details})
	if err != nil {
		t.Fatal(err)
	}
	want := []derive.Snapshot{
		{Endpoint: volvo.Details, FetchedAt: t0, CheckedAt: t0, Payload: []byte(`{"data": {"batteryCapacityKWH": 80}}`)},
		{Endpoint: volvo.Odometer, FetchedAt: t0.Add(time.Hour), CheckedAt: t0.Add(2 * time.Hour), Payload: []byte(`{"v": 2}`)},
		{Endpoint: volvo.Odometer, FetchedAt: t0.Add(3 * time.Hour), CheckedAt: t0.Add(3 * time.Hour), Payload: []byte(`{"v": 3}`)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	all, _ := s.SnapshotsSince(ctx, a, v, time.Time{}, []volvo.Endpoint{volvo.Odometer})
	if len(all) != 3 {
		t.Errorf("whole history: %d snapshots", len(all))
	}
}

func TestSnapshotsBetween(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	save := func(acc, vehicle string, ep volvo.Endpoint, at time.Time, raw string) {
		t.Helper()
		h, _ := volvo.Fingerprint([]byte(raw))
		if _, err := s.SaveSnapshot(ctx, collector.Snapshot{AccountID: acc, VehicleID: vehicle, Endpoint: ep, FetchedAt: at, Payload: []byte(raw), Hash: h}); err != nil {
			t.Fatal(err)
		}
	}
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	save(a, v, volvo.EnergyState, at(0), `{"v":1}`) // over before the window
	save(a, v, volvo.EnergyState, at(10), `{"v":2}`)
	save(a, v, volvo.EnergyState, at(40), `{"v":2}`) // read again within it: straddles from
	save(a, v, volvo.EnergyState, at(60), `{"v":3}`)
	save(a, v, volvo.EnergyState, at(90), `{"v":4}`) // at to: kept
	save(a, v, volvo.EnergyState, at(91), `{"v":5}`)
	save(a, v, volvo.Odometer, at(50), `{"v":1}`)
	save(b, vb, volvo.EnergyState, at(50), `{"v":9}`)
	energy := []volvo.Endpoint{volvo.EnergyState}

	got, err := s.SnapshotsBetween(ctx, a, v, at(30), at(90), energy, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []derive.Snapshot{
		{Endpoint: volvo.EnergyState, FetchedAt: at(10), CheckedAt: at(40), Payload: []byte(`{"v": 2}`)},
		{Endpoint: volvo.EnergyState, FetchedAt: at(60), CheckedAt: at(60), Payload: []byte(`{"v": 3}`)},
		{Endpoint: volvo.EnergyState, FetchedAt: at(90), CheckedAt: at(90), Payload: []byte(`{"v": 4}`)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	// A response over before from is not read within the window.
	if got, _ := s.SnapshotsBetween(ctx, a, v, at(5), at(8), energy, 10); len(got) != 0 {
		t.Errorf("before the next response: %+v", got)
	}
	if got, _ := s.SnapshotsBetween(ctx, a, v, at(30), at(90), energy, 2); len(got) != 2 || !got[1].FetchedAt.Equal(at(60)) {
		t.Errorf("limit 2: %+v", got)
	}
	// Another account's vehicle reads nothing, even by its ID.
	if got, err := s.SnapshotsBetween(ctx, a, vb, at(0), at(100), energy, 10); err != nil || len(got) != 0 {
		t.Errorf("account A read B's snapshots: %+v, %v", got, err)
	}
}

// TestEventIsolation extends TestAccountIsolation to the derived tables.
func TestEventIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	for _, x := range [][2]string{{a, va}, {b, vb}} {
		if err := s.SaveDerivation(ctx, x[0], x[1], time.Time{}, sampleResult(t0)); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"trips", "charges", "derivation_cursors"}
	count := func(account, table string) int {
		t.Helper()
		var n int
		err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, table := range tables {
		if got := count(a, table); got != 1 {
			t.Errorf("account A, %s: %d rows visible, want 1", table, got)
		}
	}
	t.Run("with no account set, nothing is visible", func(t *testing.T) {
		for _, table := range tables {
			var n int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("%s: %d visible, %v", table, n, err)
			}
		}
	})
	t.Run("reading another account's vehicle returns nothing", func(t *testing.T) {
		trips, err := s.Trips(ctx, a, vb)
		charges, err2 := s.Charges(ctx, a, vb)
		cursor, err3 := s.DerivationCursor(ctx, a, vb)
		if len(trips) != 0 || len(charges) != 0 || cursor != (core.Cursor{}) || err != nil || err2 != nil || err3 != nil {
			t.Errorf("trips %v, charges %v, cursor %v", trips, charges, cursor)
		}
	})
	t.Run("write into another account refused", func(t *testing.T) {
		if err := s.SaveDerivation(ctx, a, vb, time.Time{}, sampleResult(t0.Add(time.Hour))); err == nil {
			t.Error("events written to another account's vehicle")
		}
		for _, table := range tables {
			err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, "UPDATE "+table+" SET account_id = $1", b)
				return err
			})
			if err == nil {
				t.Errorf("%s: rows moved to another account", table)
			}
		}
	})
	t.Run("deleting another account's events has no effect", func(t *testing.T) {
		if err := s.SaveDerivation(ctx, a, va, time.Time{}, core.Result{}); err != nil {
			t.Fatal(err)
		}
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			for _, table := range tables {
				if _, err := tx.Exec(ctx, "DELETE FROM "+table); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range tables {
			if count(b, table) != 1 {
				t.Errorf("%s of account B gone", table)
			}
		}
	})
}

// TestDeriveOnPostgres plays the commute day against the real store: incremental
// derivation after each pass, then a rebuild that must give the same events.
func TestDeriveOnPostgres(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	data, err := os.ReadFile("../../scenarios/commute.yaml")
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
	clk := clock.NewManual(sc.Start)
	srv := httptest.NewServer(volvoapi.NewHandler([]volvoapi.Source{sim}, clk, volvoapi.DefaultLimits(), volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	defer srv.Close()
	a, v := newVehicle(t, s, "token", sc.VIN)

	d := derive.New(s, core.DefaultParams(), nil, quietLog)
	// A token pasted by hand: no expiry, never refreshed.
	tokens := oauth.NewManager(s, nil, clk, oauth.DefaultParams(), quietLog)
	c := collector.New(volvo.NewClient(srv.URL, "key", srv.Client()), s, tokens, clk, collector.DefaultIntervals(), collector.DefaultQuota(), quietLog, nil, d)
	end := sc.Start.Truncate(24 * time.Hour).Add(23 * time.Hour)
	for clk.Now().Before(end) {
		if err := c.PollOnce(ctx); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
	trips, _ := s.Trips(ctx, a, v)
	charges, _ := s.Charges(ctx, a, v)
	if len(trips) != 2 || len(charges) != 1 {
		t.Fatalf("%d trips, %d charges; want 2 and 1", len(trips), len(charges))
	}
	for range 2 {
		if _, err := d.Rebuild(ctx, a, v); err != nil {
			t.Fatal(err)
		}
		rt, _ := s.Trips(ctx, a, v)
		rc, _ := s.Charges(ctx, a, v)
		if !reflect.DeepEqual(rt, trips) || !reflect.DeepEqual(rc, charges) {
			t.Errorf("rebuild differs:\n%+v\n%+v\nwant\n%+v\n%+v", rt, rc, trips, charges)
		}
	}
}

// TestChargeSpanRoundTrip checks the columns a battery capacity estimate will rest on:
// the span of the power integral and the odometer, in and out of the database, NULL
// without a span, and the constraints refusing the impossible.
func TestChargeSpanRoundTrip(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	charge := func(at time.Time, sp core.Value[core.PowerSpan], odo core.Value[float64]) core.Charge {
		return core.Charge{
			DetectedAt: at,
			Start:      core.Bounds{After: at, Before: at.Add(10 * time.Minute)},
			End:        core.Bounds{After: at.Add(50 * time.Minute), Before: at.Add(time.Hour)},
			StartSoC:   some(47), EndSoC: some(78), Span: sp, OdometerKm: odo,
		}
	}
	span := func(start, end, kwh float64, gap, dur time.Duration) core.Value[core.PowerSpan] {
		return core.Value[core.PowerSpan]{V: core.PowerSpan{StartSoC: start, EndSoC: end, EnergyKWh: kwh, MaxGap: gap, Duration: dur}, OK: true}
	}
	res := core.Result{
		Charges: []core.Charge{
			charge(t0, span(47, 78, 24.5, 10*time.Minute, 77*time.Minute), some(12400)),
			charge(t0.Add(time.Hour), core.Value[core.PowerSpan]{}, core.Value[float64]{}), // no span, no odometer
		},
		Cursor: core.Cursor{Settled: t0.Add(2 * time.Hour), From: t0},
	}
	if err := s.SaveDerivation(ctx, a, v, time.Time{}, res); err != nil {
		t.Fatal(err)
	}
	charges, err := s.Charges(ctx, a, v)
	if err != nil || !reflect.DeepEqual(charges, res.Charges) {
		t.Errorf("charges = %+v, %v\nwant %+v", charges, err, res.Charges)
	}

	for _, tt := range []struct {
		name   string
		charge core.Charge
	}{
		{"a SoC above 100", charge(t0.Add(3*time.Hour), span(47, 101, 24.5, 10*time.Minute, 77*time.Minute), some(12400))},
		{"a negative SoC", charge(t0.Add(3*time.Hour), span(-1, 78, 24.5, 10*time.Minute, 77*time.Minute), some(12400))},
		{"a negative energy", charge(t0.Add(3*time.Hour), span(47, 78, -0.5, 10*time.Minute, 77*time.Minute), some(12400))},
		{"a negative gap", charge(t0.Add(3*time.Hour), span(47, 78, 24.5, -time.Minute, 77*time.Minute), some(12400))},
		{"a negative duration", charge(t0.Add(3*time.Hour), span(47, 78, 24.5, 10*time.Minute, -time.Minute), some(12400))},
		{"a negative odometer", charge(t0.Add(3*time.Hour), span(47, 78, 24.5, 10*time.Minute, 77*time.Minute), some(-1))},
	} {
		bad := core.Result{Charges: []core.Charge{tt.charge}, Cursor: res.Cursor}
		if err := s.SaveDerivation(ctx, a, v, time.Time{}, bad); err == nil {
			t.Errorf("%s accepted", tt.name)
		}
	}
	// The refused derivations leave the saved charges in place.
	if charges, err := s.Charges(ctx, a, v); err != nil || !reflect.DeepEqual(charges, res.Charges) {
		t.Errorf("charges after the refused saves = %+v, %v\nwant %+v", charges, err, res.Charges)
	}

	// The five columns of the span are NULL together, a constraint of the table.
	err = s.inAccount(ctx, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO charges (account_id, vehicle_id, detected_at, reconstructed,
			started_after, started_before, ended_after, ended_before, span_start_soc)
			VALUES ($1, $2, $3, false, $4, $4, $4, $4, 47)`, a, v, t0.Add(3*time.Hour), t0)
		return err
	})
	if err == nil {
		t.Error("a span without all of its columns accepted")
	}
}

// TestPeriodEvents reads the events of a period for the statistics: those that started
// in it, and the latest one of each kind before it.
func TestPeriodEvents(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	event := func(h int) (time.Time, core.Bounds, core.Bounds) {
		start := at(h)
		return start.Add(5 * time.Minute), core.Bounds{After: start, Before: start.Add(5 * time.Minute)},
			core.Bounds{After: start.Add(30 * time.Minute), Before: start.Add(35 * time.Minute)}
	}
	var res core.Result
	for _, h := range []int{0, 2, 4} {
		d, start, end := event(h)
		res.Trips = append(res.Trips, core.Trip{DetectedAt: d, Start: start, End: end, DistanceKm: some(10)})
	}
	for _, h := range []int{1, 3} {
		d, start, end := event(h)
		res.Charges = append(res.Charges, core.Charge{DetectedAt: d, Start: start, End: end, EnergySoCKWh: some(20)})
	}
	for _, x := range [][2]string{{a, va}, {b, vb}} {
		if err := s.SaveDerivation(ctx, x[0], x[1], time.Time{}, res); err != nil {
			t.Fatal(err)
		}
	}
	hours := func(trips []core.Trip, charges []core.Charge) (th, ch []int) {
		for _, tr := range trips {
			th = append(th, int(tr.Start.After.Sub(t0).Hours()))
		}
		for _, c := range charges {
			ch = append(ch, int(c.Start.After.Sub(t0).Hours()))
		}
		return th, ch
	}
	for _, tt := range []struct {
		name           string
		from, to       time.Time
		trips, charges []int
	}{
		{"whole history", time.Time{}, at(5), []int{0, 2, 4}, []int{1, 3}},
		{"latest of each before from", at(2).Add(30 * time.Minute), at(5), []int{2, 4}, []int{1, 3}},
		{"from at a start: included", at(2), at(5), []int{0, 2, 4}, []int{1, 3}},
		{"to excluded", at(1), at(4), []int{0, 2}, []int{1, 3}},
		{"nothing before", time.Time{}, at(0), nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := s.PeriodEvents(ctx, a, va, tt.from, tt.to)
			if err != nil {
				t.Fatal(err)
			}
			th, ch := hours(p.Trips, p.Charges)
			if !reflect.DeepEqual(th, tt.trips) || !reflect.DeepEqual(ch, tt.charges) {
				t.Errorf("trips at %v, charges at %v; want %v and %v", th, ch, tt.trips, tt.charges)
			}
		})
	}
	if p, _ := s.PeriodEvents(ctx, a, va, at(4), at(5)); !reflect.DeepEqual(p.Trips[1], res.Trips[2]) ||
		!reflect.DeepEqual(p.Charges[0], res.Charges[1]) {
		t.Errorf("events read back differ: %+v, %+v", p.Trips, p.Charges)
	}
	// Another account's vehicle: nothing, whatever the period.
	if p, err := s.PeriodEvents(ctx, a, vb, time.Time{}, at(5)); len(p.Trips) != 0 || len(p.Charges) != 0 || err != nil {
		t.Errorf("another account's events: %+v, %v", p, err)
	}
}

// TestReadsSeeOneSnapshot checks that the reads of the API see the database as it was
// at their first statement: a derivation committed in the middle of a read shows in
// none of its later statements. Under READ COMMITTED, the second count would see it.
func TestReadsSeeOneSnapshot(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	res := sampleResult(t0)
	if err := s.SaveDerivation(ctx, a, v, time.Time{}, res); err != nil {
		t.Fatal(err)
	}
	var before, after int
	err := s.readInAccount(ctx, a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM charges").Scan(&before); err != nil {
			return err
		}
		// Another connection, committed at once.
		more := res
		more.Charges = append(slices.Clone(res.Charges), core.Charge{DetectedAt: t0.Add(48 * time.Hour)})
		if err := s.SaveDerivation(ctx, a, v, time.Time{}, more); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM charges").Scan(&after)
	})
	if err != nil || before != len(res.Charges) || after != before {
		t.Errorf("charges: %d then %d within one read (%v), want %d", before, after, err, len(res.Charges))
	}
	if n := count(ctx, t, s, a, "charges"); n != before+1 {
		t.Errorf("%d charges after the read, want %d", n, before+1)
	}
	err = s.readInAccount(ctx, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM charges")
		return err
	})
	if err == nil {
		t.Error("a read deleted the charges")
	}
}
