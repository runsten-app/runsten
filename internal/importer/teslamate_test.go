package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/teslamate"
)

// allSaver keeps what an import of files writes.
type allSaver struct {
	trips   []ImportedTrip
	charges []core.Charge
	err     error
}

func (s *allSaver) SaveImport(_ context.Context, _, _ string, trips []ImportedTrip, charges []core.Charge) (Result, error) {
	if s.err != nil {
		return Result{}, s.err
	}
	s.trips, s.charges = trips, charges
	return Result{Saved: len(trips) + len(charges), Cutoff: cutoff}, nil
}

// The fixtures hold the same synthetic rows in the schemas of TeslaMate 4.4.0 and of
// 1.20.0 (2020): what the import reads did not change between them.
var teslaMateFixtures = []string{"v4.4.0", "v1.20.0"}

func importFixture(t *testing.T, s *allSaver, vin, currency string) (Report, error) {
	t.Helper()
	return importVersion(t, "v4.4.0", s, vin, currency)
}

func importVersion(t *testing.T, version string, s *allSaver, vin, currency string) (Report, error) {
	t.Helper()
	f, err := os.Open("../teslamate/testdata/teslamate-" + version + ".bck") //nolint:gosec // a fixture's name
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return NewFiles(s, clock.NewManual(t0)).TeslaMate(context.Background(), "acc", "veh", vin, currency, f)
}

func TestFilesTeslaMate(t *testing.T) {
	for _, version := range teslaMateFixtures {
		t.Run(version, func(t *testing.T) { testFilesTeslaMate(t, version) })
	}
}

func testFilesTeslaMate(t *testing.T, version string) {
	s := &allSaver{}
	// The VIN as a user may type it.
	rep, err := importVersion(t, version, s, " synthetic0000vin1 ", "EUR")
	if err != nil || rep.Saved != 2 || rep.Invalid != 1 || len(s.trips) != 1 || len(s.charges) != 1 {
		t.Fatalf("%+v, %v: %d trips, %d charges", rep, err, len(s.trips), len(s.charges))
	}
	at := t0
	some := func(v float64) core.Value[float64] { return core.Some(v, at) }

	trip := s.trips[0].Trip
	want := core.Trip{
		DetectedAt: at,
		Start:      exact(time.Date(2024, 5, 1, 7, 0, 0, 0, time.UTC)),
		End:        exact(time.Date(2024, 5, 1, 7, 20, 0, 0, time.UTC)),
		Sources:    []string{SourceTeslaMate},
		Import:     core.Some(core.Import{Source: SourceTeslaMate, SourceID: "1", ImportedAt: at}, at),
		DistanceKm: some(10), StartOdometerKm: some(10000), EndOdometerKm: some(10010),
		StartSoC: some(80), EndSoC: some(76), StartRangeKm: some(400), EndRangeKm: some(380),
		EnergyKWh: some(20 * 0.153), OutsideTempC: some(14.5),
		From: core.Value[core.Position]{V: core.Position{Lat: 48.8566, Lon: 2.3522}, OK: true},
		To:   core.Value[core.Position]{V: core.Position{Lat: 48.87, Lon: 2.37}, OK: true},
	}
	if !equalTrips(trip, want) {
		t.Errorf("trip\n got %+v\nwant %+v", trip, want)
	}
	if got := string(s.trips[0].Track); got != `{"type":"LineString","coordinates":[[2.3522,48.8566],[2.353,48.857],[2.36,48.86],[2.37,48.87]]}` {
		t.Errorf("track %s", got)
	}

	c := s.charges[0]
	wantCharge := core.Charge{
		DetectedAt: at,
		Start:      exact(time.Date(2024, 5, 2, 18, 0, 0, 0, time.UTC)),
		End:        exact(time.Date(2024, 5, 2, 23, 30, 0, 0, time.UTC)),
		Sources:    []string{SourceTeslaMate},
		Import:     core.Some(core.Import{Source: SourceTeslaMate, SourceID: "1", ImportedAt: at}, at),
		Type:       core.Some(core.AC, at),
		StartSoC:   some(40), EndSoC: some(76), StartRangeKm: some(200), EndRangeKm: some(380), EndUsableSoC: some(75),
		EnergyPowerKWh: some(27.5), EnergyMeasuredKWh: some(27.5), AveragePowerKW: some(5),
		OdometerKm: some(10010), MaxCurrentA: some(16), Phases: core.Some(3, at), OutsideTempC: some(12),
		Position: core.Value[core.Position]{V: core.Position{Lat: 48.87, Lon: 2.37}, OK: true},
		PaidCost: core.Some(core.Amount{Currency: "EUR", Minor: 602}, at),
	}
	if !equalCharges(c, wantCharge) {
		t.Errorf("charge\n got %+v\nwant %+v", c, wantCharge)
	}

	// Without a currency, no cost.
	if _, err := importVersion(t, version, s, "SYNTHETIC0000VIN1", ""); err != nil || s.charges[0].PaidCost.OK {
		t.Errorf("without a currency: %v, %+v", err, s.charges[0].PaidCost)
	}
}

func equalTrips(a, b core.Trip) bool {
	return a.DetectedAt.Equal(b.DetectedAt) && a.Start == b.Start && a.End == b.End &&
		strings.Join(a.Sources, ",") == strings.Join(b.Sources, ",") && a.Import == b.Import &&
		a.DistanceKm == b.DistanceKm && a.StartOdometerKm == b.StartOdometerKm && a.EndOdometerKm == b.EndOdometerKm &&
		a.StartSoC == b.StartSoC && a.EndSoC == b.EndSoC && a.StartRangeKm == b.StartRangeKm &&
		a.EndRangeKm == b.EndRangeKm && a.EnergyKWh == b.EnergyKWh && a.OutsideTempC == b.OutsideTempC &&
		a.From == b.From && a.To == b.To && !a.Capacity.OK
}

func equalCharges(a, b core.Charge) bool {
	return a.DetectedAt.Equal(b.DetectedAt) && a.Start == b.Start && a.End == b.End &&
		strings.Join(a.Sources, ",") == strings.Join(b.Sources, ",") && a.Import == b.Import && a.Type == b.Type &&
		a.StartSoC == b.StartSoC && a.EndSoC == b.EndSoC && a.StartRangeKm == b.StartRangeKm &&
		a.EndRangeKm == b.EndRangeKm && a.EndUsableSoC == b.EndUsableSoC && a.EnergyPowerKWh == b.EnergyPowerKWh &&
		a.EnergyMeasuredKWh == b.EnergyMeasuredKWh && a.AveragePowerKW == b.AveragePowerKW &&
		a.OdometerKm == b.OdometerKm && a.MaxCurrentA == b.MaxCurrentA && a.Phases == b.Phases &&
		a.OutsideTempC == b.OutsideTempC && a.Position == b.Position && a.PaidCost == b.PaidCost &&
		!a.EnergySoCKWh.OK && !a.Capacity.OK && !a.Span.OK
}

func TestFilesTeslaMateFailures(t *testing.T) {
	s := &allSaver{}
	var noCar *NoCarError
	if _, err := importFixture(t, s, "OTHER", ""); !errors.As(err, &noCar) || len(noCar.Cars) != 2 ||
		noCar.Cars[0].VIN != "SYNTHETIC0000VIN1" || noCar.Cars[0].Name != "Blue\tback\\slash \"été\"" ||
		noCar.Cars[1].Name != "" {
		t.Errorf("another VIN: %v %+v", err, noCar)
	}
	if _, err := importFixture(t, s, "SYNTHETIC0000VIN1", "XYZ"); !errors.Is(err, ErrTeslaMateCurrency) {
		t.Errorf("unknown currency: %v", err)
	}
	var fe *teslamate.FormatError
	_, err := NewFiles(s, clock.NewManual(t0)).TeslaMate(context.Background(), "acc", "veh", "V", "", strings.NewReader("PGDMP"))
	if !errors.As(err, &fe) {
		t.Errorf("custom format: %v", err)
	}
	boom := errors.New("boom")
	s.err = boom
	if _, err := importFixture(t, s, "SYNTHETIC0000VIN1", ""); !errors.Is(err, boom) {
		t.Errorf("the store failing: %v", err)
	}
}

// TestTeslaMateValues covers what the fixture does not: values out of bounds, a DC charge,
// a charge without readings, a car other than the VIN's.
func TestTeslaMateValues(t *testing.T) {
	dump := `COPY public.cars (id, vin, name, efficiency) FROM stdin;
1	VIN1	\N	0.15
2	VIN2	\N	\N
\.
COPY public.charges (charging_process_id, date, usable_battery_level, charger_actual_current, charger_phases, fast_charger_present) FROM stdin;
1	2024-01-01 10:00:00	50	0	\N	t
1	2024-01-01 10:30:00	80	0	\N	t
9	2024-01-01 10:30:00	80	0	\N	\N
x	2024-01-01 10:30:00	80	0	\N	\N
\.
COPY public.charging_processes (id, car_id, start_date, end_date, charge_energy_added, start_battery_level, end_battery_level, start_rated_range_km, end_rated_range_km, outside_temp_avg, position_id, cost) FROM stdin;
1	1	2024-01-01 10:00:00	2024-01-01 10:30:00	40.0	20	80	100	400	\N	1	0.00
2	1	2024-01-02 10:00:00	2024-01-02 10:30:00	-1	20	80	\N	\N	\N	1	\N
3	1	2024-01-03 10:00:00	2024-01-03 10:30:00	1	20	180	\N	\N	\N	1	\N
4	1	2024-01-04 10:00:00	2024-01-04 09:30:00	1	20	80	\N	\N	\N	1	\N
5	1	2024-01-05 10:00:00	2024-01-05 10:30:00	1	20	80	\N	\N	\N	1	-2
6	2	2024-01-05 10:00:00	2024-01-05 10:30:00	1	20	80	\N	\N	\N	1	\N
7	1	2024-01-07 10:00:00	2024-01-07 10:00:00	1	20	80	\N	\N	\N	2	\N
\.
COPY public.drives (id, car_id, start_date, end_date, distance, start_km, end_km, start_rated_range_km, end_rated_range_km, outside_temp_avg, start_position_id, end_position_id) FROM stdin;
1	1	2024-01-01 08:00:00	2024-01-01 09:00:00	5	\N	\N	\N	\N	\N	\N	\N
2	1	2024-01-01 08:00:00	2024-01-01 09:00:00	-5	\N	\N	\N	\N	\N	\N	\N
3	1	2024-01-01 08:00:00	2024-01-01 09:00:00	5	\N	\N	\N	\N	NaN	\N	\N
4	1	2024-01-01 08:00:00	2024-01-01 09:00:00	5	\N	\N	\N	\N	\N	3	\N
5	1	2024-01-01 08:00:00	2024-01-01 09:00:00	5	\N	\N	\N	\N	\N	\N	4
6	1	2024-01-01 08:00:00	2024-01-01 09:00:00	5	\N	\N	\N	\N	\N	\N	5
\.
COPY public.positions (id, car_id, drive_id, date, latitude, longitude, battery_level, odometer) FROM stdin;
1	1	\N	2024-01-01 10:00:00	45.0	5.0	20	100
2	1	\N	2024-01-01 10:00:00	45.0	5.0	20	-1
3	1	\N	2024-01-01 10:00:00	95.0	5.0	20	1
4	1	\N	2024-01-01 10:00:00	45.0	5.0	120	1
5	1	1	2024-01-01 10:00:00	45.0	5.0	20	1
6	2	1	2024-01-01 10:00:00	45.0	5.0	20	1
7	1	1	not a date	45.0	5.0	20	1
\.
`
	s := &allSaver{}
	rep, err := NewFiles(s, clock.NewManual(t0)).TeslaMate(context.Background(), "acc", "veh", "VIN1", "EUR", strings.NewReader(dump))
	if err != nil {
		t.Fatal(err)
	}
	// Readable rows refused by the conversion: processes 2 to 5 and 7, drives 2 to 5;
	// unreadable: a reading and a position.
	if len(s.charges) != 1 || len(s.trips) != 2 || rep.Invalid != 11 {
		t.Fatalf("%d charges, %d trips, %d invalid", len(s.charges), len(s.trips), rep.Invalid)
	}
	c := s.charges[0]
	if c.Type.V != core.DC || c.MaxCurrentA.OK || c.Phases.OK || c.EndUsableSoC.V != 80 ||
		c.PaidCost.V != (core.Amount{Currency: "EUR", Minor: 0}) || c.OutsideTempC.OK || c.OdometerKm.V != 100 {
		t.Errorf("DC charge %+v", c)
	}
	// The drive's only position of its car makes no track; its energy needs both ranges.
	if s.trips[0].Track != nil || s.trips[0].Trip.EnergyKWh.OK || s.trips[0].Trip.From.OK {
		t.Errorf("drive %+v", s.trips[0])
	}
	if to := s.trips[1].Trip; !to.To.OK || to.EndSoC.V != 20 || to.StartSoC.OK {
		t.Errorf("drive to a position %+v", to)
	}
}

func TestDriveTrack(t *testing.T) {
	tr := &driveTrack{step: 1}
	start := time.Date(2024, 5, 1, 7, 0, 0, 0, time.UTC)
	const n = 2017
	for i := range n {
		tr.add(newTrackPoint(start.Add(time.Duration(i)*time.Second), 45, float64(i)/1000))
	}
	if len(tr.points) > maxTrackPoints || len(tr.points) < maxTrackPoints/2 {
		t.Errorf("%d points kept", len(tr.points))
	}
	ls := string(tr.lineString())
	if !strings.HasPrefix(ls, `{"type":"LineString","coordinates":[[0,45],`) || !strings.HasSuffix(ls, `[2.016,45]]}`) {
		t.Errorf("not from the first point to the last: %s…%s", ls[:60], ls[len(ls)-30:])
	}
	if (*driveTrack)(nil).lineString() != nil {
		t.Error("no track")
	}
}

// TestTeslaMateTooLarge: a backup with more than an import keeps in memory stops it.
func TestTeslaMateTooLarge(t *testing.T) {
	cars := "COPY public.cars (id, vin, name, efficiency) FROM stdin;\n1\tVIN1\t\\N\t0.15\n\\.\n"
	rows := func(header, row string, n int) string {
		var b strings.Builder
		b.WriteString(header)
		for i := range n {
			fmt.Fprintf(&b, row, i)
		}
		b.WriteString("\\.\n")
		return b.String()
	}
	for _, tc := range []struct {
		name, dump, what string
		max              int
	}{
		{
			"drives", cars + rows("COPY public.drives (id, car_id, start_date, end_date, distance, start_km, end_km, "+
				"start_rated_range_km, end_rated_range_km, outside_temp_avg, start_position_id, end_position_id) FROM stdin;\n",
				"%d\t1\t2024-01-01 08:00:00\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\n", maxTeslaMateEvents+1),
			"drives of the car", maxTeslaMateEvents,
		},
		{
			"processes", cars + rows("COPY public.charging_processes (id, car_id, start_date, end_date, charge_energy_added, "+
				"start_battery_level, end_battery_level, start_rated_range_km, end_rated_range_km, outside_temp_avg, position_id, "+
				"cost) FROM stdin;\n", "%d\t1\t2024-01-01 08:00:00\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t1\t\\N\n", maxTeslaMateEvents+1),
			"charges of the car", maxTeslaMateEvents,
		},
		{
			"readings", cars + rows("COPY public.charges (charging_process_id, date, usable_battery_level, "+
				"charger_actual_current, charger_phases, fast_charger_present) FROM stdin;\n",
				"%d\t2024-01-01 08:00:00\t\\N\t\\N\t\\N\t\\N\n", maxTeslaMateProcesses+1),
			"charges", maxTeslaMateProcesses,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &allSaver{}
			_, err := NewFiles(s, clock.NewManual(t0)).TeslaMate(context.Background(), "acc", "veh", "VIN1", "",
				strings.NewReader(tc.dump))
			var tl *TooLargeError
			if !errors.As(err, &tl) || tl.What != tc.what || tl.Max != tc.max || s.trips != nil || s.charges != nil {
				t.Errorf("%v", err)
			}
		})
	}
}
