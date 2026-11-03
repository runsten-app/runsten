package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"runsten/internal/core"
	"runsten/internal/teslamate"
)

// SourceTeslaMate is a TeslaMate instance, whose backup the user gives.
const SourceTeslaMate = "teslamate"

// maxTrackPoints bounds the points kept of a drive's track while reading: TeslaMate
// records one every few seconds, thousands a drive. Between half and all of it are
// kept, evenly.
const maxTrackPoints = 500

// TeslaMateCar is a car of a TeslaMate backup.
type TeslaMateCar struct {
	VIN  string
	Name string // empty when TeslaMate has none
}

// NoCarError is a backup without the vehicle's VIN: Cars are those it has.
type NoCarError struct{ Cars []TeslaMateCar }

func (e *NoCarError) Error() string {
	return fmt.Sprintf("no car of the backup has the vehicle's VIN (it has %d)", len(e.Cars))
}

// ErrTeslaMateCurrency is a currency Runsten does not know.
var ErrTeslaMateCurrency = errors.New("unknown currency")

// The tables and columns read: those of TeslaMate since 1.20 (2020), which a later
// release only adds to.
var teslaMateTables = []teslamate.Table{
	{Name: "cars", Columns: []string{"id", "vin", "name", "efficiency"}},
	{Name: "charges", Columns: []string{
		"charging_process_id", "date", "usable_battery_level", "charger_actual_current", "charger_phases",
		"fast_charger_present",
	}},
	{Name: "charging_processes", Columns: []string{
		"id", "car_id", "start_date", "end_date", "charge_energy_added", "start_battery_level", "end_battery_level",
		"start_rated_range_km", "end_rated_range_km", "outside_temp_avg", "position_id", "cost",
	}},
	{Name: "drives", Columns: []string{
		"id", "car_id", "start_date", "end_date", "distance", "start_km", "end_km", "start_rated_range_km",
		"end_rated_range_km", "outside_temp_avg", "start_position_id", "end_position_id",
	}},
	{Name: "positions", Columns: []string{
		"id", "car_id", "drive_id", "date", "latitude", "longitude", "battery_level", "odometer",
	}},
}

// TeslaMate imports the drives and charging processes of the car of a TeslaMate backup
// (internal/teslamate) whose VIN is vin, each drive with its track. A charge's cost is
// imported in currency, TeslaMate's being unknown to it; none when currency is empty.
// The backup is read once, as it comes, and nothing is written before its end: a
// backup cut short or not TeslaMate's returns a *teslamate.FormatError, one without the
// VIN a *NoCarError.
func (f *Files) TeslaMate(ctx context.Context, accountID, vehicleID, vin, currency string, backup io.Reader) (Report, error) {
	var cur core.Currency
	if currency != "" {
		var ok bool
		if cur, ok = core.CurrencyOf(currency); !ok {
			return Report{}, fmt.Errorf("%w %q", ErrTeslaMateCurrency, currency)
		}
	}
	r := newTeslaMateReading(vin)
	if err := teslamate.Read(backup, teslaMateTables, r.row); err != nil {
		return Report{}, fmt.Errorf("reading the backup: %w", err)
	}
	if r.car < 0 {
		return Report{}, &NoCarError{Cars: r.cars}
	}
	at := f.clk.Now()
	trips, charges, invalid := r.convert(cur, at)
	res, err := f.store.SaveImport(ctx, accountID, vehicleID, trips, charges)
	if err != nil {
		return Report{}, fmt.Errorf("saving: %w", err)
	}
	return Report{Result: res, Invalid: invalid + r.invalid}, nil
}

// teslaMateReading gathers, as the backup is read, what the import needs of one car. pg_dump
// writes the tables by name: the cars first, a charging process's readings before it, the
// positions after the drives.
type teslaMateReading struct {
	vin        string
	cars       []TeslaMateCar
	car        int64    // TeslaMate's ID of the car, -1 until found
	efficiency *float64 // kWh per km of rated range, TeslaMate's estimate
	invalid    int      // rows whose values are unreadable

	readings  map[int64]*chargeReadings // by charging process, of every car
	processes []teslaMateProcess
	drives    []teslaMateDrive
	tracks    map[int64]*driveTrack // by drive
	positions map[int64]*teslaMatePosition
}

type chargeReadings struct {
	last         time.Time
	usable       *float64 // at the latest reading
	maxCurrent   *float64
	phases       *int64 // of the latest reading with a current
	phasesAt     time.Time
	fast, phased bool // a reading with a fast charger, with AC phases
}

type teslaMateProcess struct {
	id                   int64
	start                time.Time
	end                  *time.Time
	added, cost, outside *float64
	startSoC, endSoC     *float64
	startRange, endRange *float64
	position             int64
}

type teslaMateDrive struct {
	id                            int64
	start                         time.Time
	end                           *time.Time
	distance, startKm, endKm      *float64
	startRange, endRange, outside *float64
	startPosition, endPosition    *int64
}

type teslaMatePosition struct {
	lat, lon        float64
	soc, odometerKm *float64
}

func newTeslaMateReading(vin string) *teslaMateReading {
	return &teslaMateReading{
		vin: strings.ToUpper(strings.TrimSpace(vin)), car: -1,
		readings: map[int64]*chargeReadings{}, tracks: map[int64]*driveTrack{}, positions: map[int64]*teslaMatePosition{},
	}
}

func (r *teslaMateReading) row(row teslamate.Row) error {
	var ok bool
	switch row.Table() {
	case "cars":
		ok = r.readCar(row)
	case "charges":
		ok = r.readCharge(row)
	case "charging_processes":
		ok = r.readProcess(row)
	case "drives":
		ok = r.readDrive(row)
	case "positions":
		ok = r.readPosition(row)
	}
	if !ok {
		r.invalid++
	}
	return nil
}

func (r *teslaMateReading) readCar(row teslamate.Row) bool {
	id, okID := row.Int("id")
	eff, okEff := row.Float("efficiency")
	vin := row.Text("vin")
	if !okID || id == nil || !okEff || vin == nil {
		return false
	}
	car := TeslaMateCar{VIN: *vin}
	if name := row.Text("name"); name != nil {
		car.Name = *name
	}
	r.cars = append(r.cars, car)
	if strings.ToUpper(strings.TrimSpace(*vin)) == r.vin {
		r.car, r.efficiency = *id, eff
	}
	return true
}

// readCharge keeps what a charging process's readings tell beyond the process: its usable
// level at the end, its highest current and its phases, and whether a fast charger or AC
// phases were seen. Before the process, and so before its car is known: every car's.
func (r *teslaMateReading) readCharge(row teslamate.Row) bool {
	pid, okPID := row.Int("charging_process_id")
	at, okAt := row.Time("date")
	usable, okUsable := row.Float("usable_battery_level")
	current, okCurrent := row.Float("charger_actual_current")
	phases, okPhases := row.Int("charger_phases")
	fast, okFast := row.Bool("fast_charger_present")
	if !okPID || pid == nil || !okAt || at == nil || !okUsable || !okCurrent || !okPhases || !okFast {
		return false
	}
	c := r.readings[*pid]
	if c == nil {
		c = &chargeReadings{}
		r.readings[*pid] = c
	}
	if !at.Before(c.last) {
		c.last, c.usable = *at, usable
	}
	// A DC charge reads 0 A on the car's AC charger: no current told.
	if current != nil && *current > 0 && (c.maxCurrent == nil || *current > *c.maxCurrent) {
		c.maxCurrent = current
	}
	if current != nil && *current > 0 && phases != nil && *phases > 0 && !at.Before(c.phasesAt) {
		c.phases, c.phasesAt = phases, *at
	}
	c.fast = c.fast || fast != nil && *fast
	c.phased = c.phased || phases != nil && *phases > 0
	return true
}

func (r *teslaMateReading) readProcess(row teslamate.Row) bool {
	if car, ok := row.Int("car_id"); !ok || car == nil || *car != r.car {
		return ok
	}
	p := teslaMateProcess{}
	id, okID := row.Int("id")
	start, okStart := row.Time("start_date")
	var okEnd bool
	p.end, okEnd = row.Time("end_date")
	pos, okPos := row.Int("position_id")
	if !okID || id == nil || !okStart || start == nil || !okEnd || !okPos || pos == nil {
		return false
	}
	p.id, p.start, p.position = *id, *start, *pos
	for _, f := range []struct {
		col string
		v   **float64
	}{
		{"charge_energy_added", &p.added},
		{"cost", &p.cost},
		{"outside_temp_avg", &p.outside},
		{"start_battery_level", &p.startSoC},
		{"end_battery_level", &p.endSoC},
		{"start_rated_range_km", &p.startRange},
		{"end_rated_range_km", &p.endRange},
	} {
		var ok bool
		if *f.v, ok = row.Float(f.col); !ok {
			return false
		}
	}
	r.processes = append(r.processes, p)
	r.positions[p.position] = nil
	return true
}

func (r *teslaMateReading) readDrive(row teslamate.Row) bool {
	if car, ok := row.Int("car_id"); !ok || car == nil || *car != r.car {
		return ok
	}
	d := teslaMateDrive{}
	id, okID := row.Int("id")
	start, okStart := row.Time("start_date")
	var okEnd, okFrom, okTo bool
	d.end, okEnd = row.Time("end_date")
	d.startPosition, okFrom = row.Int("start_position_id")
	d.endPosition, okTo = row.Int("end_position_id")
	if !okID || id == nil || !okStart || start == nil || !okEnd || !okFrom || !okTo {
		return false
	}
	d.id, d.start = *id, *start
	for _, f := range []struct {
		col string
		v   **float64
	}{
		{"distance", &d.distance},
		{"start_km", &d.startKm},
		{"end_km", &d.endKm},
		{"start_rated_range_km", &d.startRange},
		{"end_rated_range_km", &d.endRange},
		{"outside_temp_avg", &d.outside},
	} {
		var ok bool
		if *f.v, ok = row.Float(f.col); !ok {
			return false
		}
	}
	r.drives = append(r.drives, d)
	r.tracks[d.id] = &driveTrack{step: 1}
	for _, p := range []*int64{d.startPosition, d.endPosition} {
		if p != nil {
			r.positions[*p] = nil
		}
	}
	return true
}

// readPosition adds the position to its drive's track, and keeps it when a drive or a
// charge starts or ends there.
func (r *teslaMateReading) readPosition(row teslamate.Row) bool {
	if car, ok := row.Int("car_id"); !ok || car == nil || *car != r.car {
		return ok
	}
	id, okID := row.Int("id")
	drive, okDrive := row.Int("drive_id")
	at, okAt := row.Time("date")
	lat, okLat := row.Float("latitude")
	lon, okLon := row.Float("longitude")
	if !okID || id == nil || !okDrive || !okAt || at == nil || !okLat || lat == nil || !okLon || lon == nil {
		return false
	}
	if drive != nil {
		if t := r.tracks[*drive]; t != nil {
			t.add(trackPoint{at: *at, lat: *lat, lon: *lon})
		}
	}
	if _, wanted := r.positions[*id]; !wanted {
		return true
	}
	soc, okSoC := row.Float("battery_level")
	odo, okOdo := row.Float("odometer")
	if !okSoC || !okOdo {
		return false
	}
	r.positions[*id] = &teslaMatePosition{lat: *lat, lon: *lon, soc: soc, odometerKm: odo}
	return true
}

// convert converts the car's drives and charging processes: an unfinished one, or one
// with a value out of bounds, is left out and counted.
func (r *teslaMateReading) convert(cur core.Currency, at time.Time) ([]ImportedTrip, []core.Charge, int) {
	invalid := 0
	var trips []ImportedTrip
	for _, d := range r.drives {
		trip, ok := r.convertDrive(d, at)
		if !ok {
			invalid++
			continue
		}
		trips = append(trips, ImportedTrip{Trip: trip, Track: r.tracks[d.id].lineString()})
	}
	var charges []core.Charge
	for _, p := range r.processes {
		c, ok := r.convertProcess(p, cur, at)
		if !ok {
			invalid++
			continue
		}
		charges = append(charges, c)
	}
	return trips, charges, invalid
}

// convertDrive converts a drive. Its energy is TeslaMate's: the rated range it used
// times the car's efficiency, which TeslaMate measures on its charges; no capacity
// stands behind it.
func (r *teslaMateReading) convertDrive(d teslaMateDrive, at time.Time) (core.Trip, bool) {
	if d.end == nil || d.end.Before(d.start) {
		return core.Trip{}, false
	}
	trip := core.Trip{
		DetectedAt: at.UTC(), Start: exact(d.start), End: exact(*d.end), Sources: []string{SourceTeslaMate},
		Import: core.Some(core.Import{Source: SourceTeslaMate, SourceID: strconv.FormatInt(d.id, 10), ImportedAt: at}, at),
	}
	from, to := r.position(d.startPosition), r.position(d.endPosition)
	var ok bool
	if trip.From, ok = positionOf(from); !ok {
		return core.Trip{}, false
	}
	if trip.To, ok = positionOf(to); !ok {
		return core.Trip{}, false
	}
	if from != nil {
		if trip.StartSoC, ok = soc(from.soc, at); !ok {
			return core.Trip{}, false
		}
	}
	if to != nil {
		if trip.EndSoC, ok = soc(to.soc, at); !ok {
			return core.Trip{}, false
		}
	}
	for _, f := range []struct {
		v   *float64
		out *core.Value[float64]
	}{
		{d.distance, &trip.DistanceKm},
		{d.startKm, &trip.StartOdometerKm},
		{d.endKm, &trip.EndOdometerKm},
		{d.startRange, &trip.StartRangeKm},
		{d.endRange, &trip.EndRangeKm},
	} {
		if *f.out, ok = nonNegative(f.v, at); !ok {
			return core.Trip{}, false
		}
	}
	if trip.OutsideTempC, ok = finiteValue(d.outside, at); !ok {
		return core.Trip{}, false
	}
	if d.startRange != nil && d.endRange != nil && r.efficiency != nil && finite(*r.efficiency) && *r.efficiency > 0 {
		trip.EnergyKWh = core.Some((*d.startRange-*d.endRange)**r.efficiency, at)
	}
	return trip, true
}

// convertProcess converts a charging process. Its energy is the car's own count of what
// went into its battery (charge_energy_added): both the API's measured flow and the
// energy the rated estimates divide by. A fast charger makes it DC, AC phases AC.
func (r *teslaMateReading) convertProcess(p teslaMateProcess, cur core.Currency, at time.Time) (core.Charge, bool) {
	if p.end == nil || p.end.Before(p.start) {
		return core.Charge{}, false
	}
	c := core.Charge{
		DetectedAt: at.UTC(), Start: exact(p.start), End: exact(*p.end), Sources: []string{SourceTeslaMate},
		Import: core.Some(core.Import{Source: SourceTeslaMate, SourceID: strconv.FormatInt(p.id, 10), ImportedAt: at}, at),
	}
	pos := r.positions[p.position]
	var ok bool
	if c.Position, ok = positionOf(pos); !ok {
		return core.Charge{}, false
	}
	if pos != nil {
		if c.OdometerKm, ok = nonNegative(pos.odometerKm, at); !ok {
			return core.Charge{}, false
		}
	}
	if c.StartSoC, ok = soc(p.startSoC, at); !ok {
		return core.Charge{}, false
	}
	if c.EndSoC, ok = soc(p.endSoC, at); !ok {
		return core.Charge{}, false
	}
	for _, f := range []struct {
		v   *float64
		out *core.Value[float64]
	}{
		{p.added, &c.EnergyPowerKWh},
		{p.added, &c.EnergyMeasuredKWh},
		{p.startRange, &c.StartRangeKm},
		{p.endRange, &c.EndRangeKm},
	} {
		if *f.out, ok = nonNegative(f.v, at); !ok {
			return core.Charge{}, false
		}
	}
	if c.OutsideTempC, ok = finiteValue(p.outside, at); !ok {
		return core.Charge{}, false
	}
	if hours := p.end.Sub(p.start).Hours(); c.EnergyPowerKWh.OK && hours > 0 {
		c.AveragePowerKW = core.Some(c.EnergyPowerKWh.V/hours, at)
	}
	if rd := r.readings[p.id]; rd != nil {
		if c.EndUsableSoC, ok = soc(rd.usable, at); !ok {
			return core.Charge{}, false
		}
		if c.MaxCurrentA, ok = nonNegative(rd.maxCurrent, at); !ok {
			return core.Charge{}, false
		}
		if rd.phases != nil && *rd.phases <= math.MaxInt16 {
			c.Phases = core.Some(int(*rd.phases), at)
		}
		switch {
		case rd.fast:
			c.Type = core.Some(core.DC, at)
		case rd.phased:
			c.Type = core.Some(core.AC, at)
		}
	}
	if p.cost != nil && cur.Code != "" {
		if !finite(*p.cost) || *p.cost < 0 {
			return core.Charge{}, false
		}
		minor := math.Round(*p.cost * math.Pow10(cur.MinorDigits))
		c.PaidCost = core.Some(core.Amount{Currency: cur.Code, Minor: int64(minor)}, at)
	}
	return c, true
}

func (r *teslaMateReading) position(id *int64) *teslaMatePosition {
	if id == nil {
		return nil
	}
	return r.positions[*id]
}

func positionOf(p *teslaMatePosition) (core.Value[core.Position], bool) {
	if p == nil {
		return core.Value[core.Position]{}, true
	}
	return position(&p.lat, &p.lon)
}

// soc is a level in %, false out of [0, 100].
func soc(v *float64, at time.Time) (core.Value[float64], bool) {
	if v == nil {
		return core.Value[float64]{}, true
	}
	if !finite(*v) || *v < 0 || *v > 100 {
		return core.Value[float64]{}, false
	}
	return core.Some(*v, at), true
}

func finiteValue(v *float64, at time.Time) (core.Value[float64], bool) {
	if v == nil {
		return core.Value[float64]{}, true
	}
	if !finite(*v) {
		return core.Value[float64]{}, false
	}
	return core.Some(*v, at), true
}

type trackPoint struct {
	at       time.Time
	lat, lon float64
}

// driveTrack keeps a drive's points as they are read, at most maxTrackPoints: once full, every
// other one is dropped, and from then on one point in twice as many is taken.
type driveTrack struct {
	points []trackPoint
	step   int // a point is taken every step
	seen   int
	last   trackPoint // the latest read, kept whatever the step: the drive's end
}

func (t *driveTrack) add(p trackPoint) {
	if t.seen == 0 || !p.at.Before(t.last.at) {
		t.last = p
	}
	t.seen++
	if (t.seen-1)%t.step != 0 {
		return
	}
	t.points = append(t.points, p)
	if len(t.points) < maxTrackPoints {
		return
	}
	kept := t.points[:0]
	for i, q := range t.points {
		if i%2 == 0 {
			kept = append(kept, q)
		}
	}
	t.points, t.step = kept, t.step*2
}

// lineString is the track as a GeoJSON LineString, sorted by time, the points out of
// bounds left out; nil with fewer than two.
func (t *driveTrack) lineString() []byte {
	if t == nil {
		return nil
	}
	slices.SortStableFunc(t.points, func(a, b trackPoint) int { return a.at.Compare(b.at) })
	if n := len(t.points); n > 0 && t.points[n-1] != t.last {
		t.points = append(t.points, t.last)
	}
	coords := make([][2]float64, 0, len(t.points))
	for _, p := range t.points {
		coords = append(coords, [2]float64{p.lon, p.lat})
	}
	return lineString(coords)
}
