package core

import (
	"math"
	"reflect"
	"slices"
	"sort"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

// m returns t0 plus n minutes.
func m(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }

// rec is a record fetched at minute f and checked again at minute c (c < f: never).
func rec(f, c int, s Snapshot) Record {
	if c < f {
		c = f
	}
	return Record{FetchedAt: m(f), CheckedAt: m(c), Snapshot: s}
}

var (
	home = Position{Lat: 45.764, Lon: 4.8357}
	work = Position{Lat: 45.7797, Lon: 4.927}
)

func engine(e EngineState) Snapshot {
	return Snapshot{Covers: FieldEngine, Engine: Some(e, time.Time{})}
}

func odo(km float64) Snapshot {
	return Snapshot{Covers: FieldOdometer, OdometerKm: Some(km, time.Time{})}
}

func loc(p Position, ts time.Time) Snapshot {
	return Snapshot{Covers: FieldPosition, Position: Some(p, ts)}
}

func capacity(kwh float64) Snapshot {
	return Snapshot{Covers: FieldCapacity, CapacityKWh: Some(kwh, time.Time{})}
}

const energyFields = FieldSoC | FieldRange | FieldCharging | FieldConnection | FieldChargeType | FieldPower | FieldTargetSoC

// nrg is an energy reading: SoC, its range and usable level, status, and the cable
// connected unless idle.
func nrg(soc float64, st ChargingStatus) Snapshot {
	s := Snapshot{
		Covers: energyFields, SoC: Some(soc, time.Time{}), RangeKm: Some(soc*4, time.Time{}),
		UsableSoC: Some(soc-1, time.Time{}),
		Charging:  Some(st, time.Time{}), TargetSoC: Some(80.0, time.Time{}),
		Connection: Some(Connected, time.Time{}),
	}
	if st == ChargingIdle {
		s.Connection = Some(Disconnected, time.Time{})
	}
	return s
}

func charging(soc, powerW float64, typ ChargeType) Snapshot {
	s := nrg(soc, ChargingActive)
	s.PowerW, s.ChargeType = Some(powerW, time.Time{}), Some(typ, time.Time{})
	return s
}

func val(v float64) Value[float64] { return Value[float64]{V: v, OK: true} }

// keptSpan is the retained interval of the power integration of a charge.
func keptSpan(start, end, kwh float64, gap, dur time.Duration) Value[PowerSpan] {
	return Value[PowerSpan]{V: PowerSpan{StartSoC: start, EndSoC: end, EnergyKWh: kwh, MaxGap: gap, Duration: dur}, OK: true}
}

// trip is a complete observed trip: parked at home, driving from minute 30 to 60, at
// work from minute 61.
func trip() []Record {
	return []Record{
		rec(0, 0, capacity(80)),
		rec(0, 20, engine(EngineStopped)),
		rec(0, 20, nrg(62, ChargingIdle)),
		rec(0, 20, odo(12400)),
		rec(0, 20, loc(home, m(0))),
		rec(30, 59, engine(EngineRunning)),
		rec(30, 30, nrg(61, ChargingIdle)),
		rec(45, 45, nrg(58, ChargingIdle)),
		rec(59, 59, nrg(55, ChargingIdle)),
		rec(61, 61, engine(EngineStopped)),
		rec(61, 61, odo(12432)),
		rec(61, 61, loc(work, m(60))),
	}
}

func TestDeriveTrips(t *testing.T) {
	tests := []struct {
		name    string
		records []Record
		params  func(*Params)
		want    []Trip
	}{
		{
			name:    "observed trip",
			records: trip(),
			want: []Trip{{
				DetectedAt: m(30), Start: Bounds{m(20), m(30)}, End: Bounds{m(59), m(60)},
				StartOdometerKm: val(12400), EndOdometerKm: val(12432), DistanceKm: val(32),
				StartSoC: val(62), EndSoC: val(55), StartRangeKm: val(248), EndRangeKm: val(220),
				EnergyKWh: val(5.6), From: Some(home, m(0)), To: Some(work, m(60)),
			}},
		},
		{
			name:    "vehicle timestamps ignored",
			records: trip(),
			params:  func(p *Params) { p.VehicleTimestamps = false },
			want: []Trip{{
				DetectedAt: m(30), Start: Bounds{m(20), m(30)}, End: Bounds{m(59), m(61)},
				StartOdometerKm: val(12400), EndOdometerKm: val(12432), DistanceKm: val(32),
				StartSoC: val(62), EndSoC: val(55), StartRangeKm: val(248), EndRangeKm: val(220),
				EnergyKWh: val(5.6), From: Some(home, m(0)), To: Some(work, m(60)),
			}},
		},
		{
			// No position after the stop (a round trip deduplicated, or the location API
			// down): the trip ends without arrival once the settle window is over.
			name: "trip without final position",
			records: func() []Record {
				rs := trip()[:11]
				rs[4].CheckedAt = m(200) // unchanged position, read again after the stop
				return append(rs, rec(100, 100, nrg(55, ChargingIdle)))
			}(),
			want: []Trip{{
				DetectedAt: m(30), Start: Bounds{m(20), m(30)}, End: Bounds{m(59), m(61)},
				StartOdometerKm: val(12400), EndOdometerKm: val(12432), DistanceKm: val(32),
				StartSoC: val(62), EndSoC: val(55), StartRangeKm: val(248), EndRangeKm: val(220),
				EnergyKWh: val(5.6), From: Some(home, m(0)),
			}},
		},
		{
			// Read at the stop, the odometer and the position the vehicle sent on the way;
			// read again 3 minutes later, those it sent at the arrival: they end the trip.
			name: "end readings sent on the way, then at the arrival",
			records: func() []Record {
				mid := Position{Lat: 45.77, Lon: 4.88}
				rs := trip()[:10]
				return append(rs,
					rec(61, 61, Snapshot{Covers: FieldOdometer, OdometerKm: Some(12416.0, m(45))}),
					rec(61, 61, loc(mid, m(45))),
					rec(64, 64, Snapshot{Covers: FieldOdometer, OdometerKm: Some(12432.0, m(63))}),
					rec(64, 64, loc(work, m(63))),
				)
			}(),
			want: []Trip{{
				DetectedAt: m(30), Start: Bounds{m(20), m(30)}, End: Bounds{m(59), m(61)},
				StartOdometerKm: val(12400), EndOdometerKm: val(12432), DistanceKm: val(32),
				StartSoC: val(62), EndSoC: val(55), StartRangeKm: val(248), EndRangeKm: val(220),
				EnergyKWh: val(5.6), From: Some(home, m(0)), To: Some(work, m(63)),
			}},
		},
		{
			name: "absent values are absent, never zero",
			records: []Record{
				rec(0, 20, engine(EngineStopped)),
				rec(0, 20, Snapshot{Covers: energyFields}), // every energy value in error
				rec(0, 20, Snapshot{Covers: FieldOdometer}),
				rec(30, 59, engine(EngineRunning)),
				rec(61, 61, engine(EngineStopped)),
				rec(61, 61, loc(work, m(60))),
			},
			want: []Trip{{
				DetectedAt: m(30), Start: Bounds{m(20), m(30)}, End: Bounds{m(59), m(60)},
				To: Some(work, m(60)),
			}},
		},
		{
			// The odometer rose between two parked readings: a trip that was never seen.
			name: "reconstructed trip",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 120, engine(EngineStopped)),
				rec(0, 20, nrg(62, ChargingIdle)),
				rec(60, 60, nrg(61, ChargingIdle)), // after the trip start
				rec(110, 120, nrg(55, ChargingIdle)),
				rec(0, 60, odo(12400)),
				rec(0, 60, loc(home, m(0))),
				rec(120, 120, odo(12432)),
				rec(120, 120, loc(work, m(100))),
			},
			want: []Trip{{
				DetectedAt: m(120), Reconstructed: true, Start: Bounds{m(60), m(100)}, End: Bounds{m(60), m(100)},
				StartOdometerKm: val(12400), EndOdometerKm: val(12432), DistanceKm: val(32),
				StartSoC: val(61), EndSoC: val(55), StartRangeKm: val(244), EndRangeKm: val(220),
				EnergyKWh: val(4.8), From: Some(home, m(0)), To: Some(work, m(100)),
			}},
		},
		{
			// The odometer rose before the engine was seen running: the trip was only
			// detected early, it is an observed one.
			name: "reconstructed, then seen driving",
			records: []Record{
				rec(0, 20, engine(EngineStopped)),
				rec(0, 20, odo(12400)),
				rec(0, 0, loc(home, m(0))),
				rec(25, 25, odo(12402)),
				rec(30, 50, engine(EngineRunning)),
				rec(51, 51, engine(EngineStopped)),
				rec(51, 51, odo(12430)),
				rec(51, 51, loc(work, m(50))),
			},
			want: []Trip{{
				DetectedAt: m(25), Start: Bounds{m(20), m(25)}, End: Bounds{m(50), m(50)},
				StartOdometerKm: val(12400), EndOdometerKm: val(12430), DistanceKm: val(30),
				From: Some(home, m(0)), To: Some(work, m(50)),
			}},
		},
		{
			name: "odometer rise below the threshold",
			records: []Record{
				rec(0, 60, odo(12400)),
				rec(120, 120, odo(12400.5)),
			},
		},
		{
			// The quota runs out while driving: no engine or odometer reading for hours, but
			// the Location API still reads the arrival. The trip ends when the engine
			// status is read again, bounded by the arrival timestamp.
			name: "quota cut in the middle of a trip",
			records: []Record{
				rec(0, 20, engine(EngineStopped)),
				rec(0, 20, odo(12400)),
				rec(0, 0, loc(home, m(0))),
				rec(30, 35, engine(EngineRunning)),
				rec(35, 35, odo(12405)),
				rec(90, 90, loc(work, m(70))),
				rec(900, 900, engine(EngineStopped)),
				rec(900, 900, odo(12432)),
			},
			want: []Trip{{
				DetectedAt: m(30), Start: Bounds{m(20), m(30)}, End: Bounds{m(35), m(70)},
				StartOdometerKm: val(12400), EndOdometerKm: val(12432), DistanceKm: val(32),
				From: Some(home, m(0)), To: Some(work, m(70)),
			}},
		},
		{
			// Running again before any new position: the same trip goes on.
			name: "short stop",
			records: []Record{
				rec(0, 20, engine(EngineStopped)),
				rec(0, 20, odo(100)),
				rec(0, 0, loc(home, m(0))),
				rec(30, 40, engine(EngineRunning)),
				rec(41, 41, engine(EngineStopped)),
				rec(45, 60, engine(EngineRunning)),
				rec(61, 61, engine(EngineStopped)),
				rec(61, 61, odo(150)),
				rec(61, 61, loc(work, time.Time{})),
			},
			want: []Trip{{
				DetectedAt: m(30), Start: Bounds{m(20), m(30)}, End: Bounds{m(60), m(61)},
				StartOdometerKm: val(100), EndOdometerKm: val(150), DistanceKm: val(50),
				From: Some(home, m(0)), To: Some(work, time.Time{}),
			}},
		},
		{
			name: "two trips",
			records: append(trip(),
				rec(200, 210, engine(EngineRunning)),
				rec(211, 211, engine(EngineStopped)),
				rec(211, 211, odo(12465)),
				rec(211, 211, loc(home, m(211))),
			),
			params: func(p *Params) { p.ResumeBeforeArrival = false },
			want: []Trip{
				{
					DetectedAt: m(30), Start: Bounds{m(20), m(30)}, End: Bounds{m(59), m(60)},
					StartOdometerKm: val(12400), EndOdometerKm: val(12432), DistanceKm: val(32),
					StartSoC: val(62), EndSoC: val(55), StartRangeKm: val(248), EndRangeKm: val(220),
					EnergyKWh: val(5.6), From: Some(home, m(0)), To: Some(work, m(60)),
				},
				{
					DetectedAt: m(200), Start: Bounds{m(61), m(200)}, End: Bounds{m(210), m(211)},
					StartOdometerKm: val(12432), EndOdometerKm: val(12465), DistanceKm: val(33),
					StartSoC: val(55), EndSoC: val(55), StartRangeKm: val(220), EndRangeKm: val(220),
					EnergyKWh: val(0), From: Some(work, m(60)), To: Some(home, m(211)),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := DefaultParams()
			if tt.params != nil {
				tt.params(&p)
			}
			got := Derive(tt.records, Cursor{}, p, nil)
			if len(got.Charges) != 0 {
				t.Errorf("charges: %+v", got.Charges)
			}
			checkTrips(t, got.Trips, tt.want)
		})
	}
}

func checkTrips(t *testing.T, got, want []Trip) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d trips, want %d: %+v", len(got), len(want), got)
	}
	for i := range got {
		g := got[i]
		roundTrip(&g)
		if !want[i].Capacity.OK { // not what the test is about: TestCapacity checks it
			g.Capacity = Value[Capacity]{}
		}
		if !reflect.DeepEqual(g, want[i]) {
			t.Errorf("trip %d:\n got %+v\nwant %+v", i, g, want[i])
		}
	}
}

// roundTrip drops the vehicle timestamps of the start and end values and rounds the
// estimates, so that expectations stay readable.
func roundTrip(t *Trip) {
	for _, v := range []*Value[float64]{
		&t.StartOdometerKm, &t.EndOdometerKm, &t.DistanceKm, &t.StartSoC, &t.EndSoC,
		&t.StartRangeKm, &t.EndRangeKm, &t.EnergyKWh, &t.TripMeterKm, &t.ConsumptionKWhPer100km,
	} {
		v.At = time.Time{}
		v.V = float64(int(v.V*1000+0.5)) / 1000
	}
}

func TestDeriveCharges(t *testing.T) {
	tests := []struct {
		name    string
		records []Record
		want    []Charge
	}{
		{
			name: "AC charge",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 0, loc(home, m(0))),
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 30, charging(51, 7000, AC)),
				rec(31, 80, charging(55, 7000, AC)),
				rec(81, 81, nrg(80, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(80), m(81)},
				Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(80), TargetSoC: val(80),
				EnergySoCKWh: val(24), EnergyPowerKWh: val(7), Position: Some(home, m(0)),
				// The whole charge stays below MaxSpanSoC: the span is the integral.
				Span: keptSpan(51, 55, 7, 49*time.Minute, 60*time.Minute),
			}},
		},
		{
			// A vehicle that says none as the charge starts tells no type: the next does.
			name: "type none, then AC",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 0, loc(home, m(0))),
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 30, charging(51, 7000, NoCharge)),
				rec(31, 80, charging(55, 7000, AC)),
				rec(81, 81, nrg(80, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(80), m(81)},
				Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(80), TargetSoC: val(80),
				EnergySoCKWh: val(24), EnergyPowerKWh: val(7), Position: Some(home, m(0)),
				Span: keptSpan(51, 55, 7, 49*time.Minute, 60*time.Minute),
			}},
		},
		{
			// Unplugged before the target: the charge ends at the disconnection.
			name: "interrupted charge",
			records: []Record{
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 40, charging(51, 11000, AC)),
				rec(41, 41, func() Snapshot {
					s := nrg(60, ChargingScheduled) // not an end status by itself
					s.Connection = Some(Disconnected, time.Time{})
					return s
				}()),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(40), m(41)},
				Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(60), TargetSoC: val(80),
				EnergyPowerKWh: val(11000 * 20 / 60.0 / 1000),
				Span:           keptSpan(51, 51, 11000*20/60.0/1000, 20*time.Minute, 20*time.Minute),
			}},
		},
		{
			// Past MaxSpanSoC the readings no longer extend the span: the power goes into
			// balancing the cells without raising the SoC. The integral of the whole
			// charge stays what it is.
			name: "span stops at the highest SoC",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 10, nrg(90, ChargingIdle)),
				rec(20, 20, charging(91, 12000, AC)),
				rec(30, 30, charging(94, 12000, AC)),
				rec(40, 40, charging(97, 12000, AC)),
				rec(50, 50, charging(100, 12000, AC)),
				rec(60, 60, nrg(100, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(50), m(60)},
				Type: Some(AC, time.Time{}), StartSoC: val(90), EndSoC: val(100), TargetSoC: val(80),
				EnergySoCKWh: val(8), EnergyPowerKWh: val(6),
				Span: keptSpan(91, 94, 2, 10*time.Minute, 10*time.Minute),
			}},
		},
		{
			// A pause cuts the integration in segments; the span keeps the one with the
			// largest SoC change.
			name: "paused charge, segment with the largest SoC change",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 10, nrg(40, ChargingIdle)),
				rec(20, 20, charging(41, 6000, AC)),
				rec(30, 30, charging(44, 6000, AC)),
				rec(40, 40, nrg(44, ChargingScheduled)), // the pause
				rec(50, 50, nrg(44, ChargingScheduled)),
				rec(60, 60, charging(45, 6000, AC)),
				rec(70, 70, charging(52, 6000, AC)),
				rec(80, 80, nrg(52, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(70), m(80)},
				Type: Some(AC, time.Time{}), StartSoC: val(40), EndSoC: val(52), TargetSoC: val(80),
				EnergySoCKWh: val(9.6), EnergyPowerKWh: val(2),
				Span: keptSpan(45, 52, 1, 10*time.Minute, 10*time.Minute),
			}},
		},
		{
			name: "paused charge, equal segments keep the first",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 10, nrg(40, ChargingIdle)),
				rec(20, 20, charging(41, 6000, AC)),
				rec(30, 30, charging(45, 6000, AC)),
				rec(40, 40, nrg(45, ChargingScheduled)),
				rec(50, 50, nrg(45, ChargingScheduled)),
				rec(60, 60, charging(46, 6000, AC)),
				rec(70, 70, charging(50, 6000, AC)),
				rec(80, 80, nrg(50, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(70), m(80)},
				Type: Some(AC, time.Time{}), StartSoC: val(40), EndSoC: val(50), TargetSoC: val(80),
				EnergySoCKWh: val(8), EnergyPowerKWh: val(2),
				Span: keptSpan(41, 45, 1, 10*time.Minute, 10*time.Minute),
			}},
		},
		{
			// A reading missing in the middle of the charge: the gap it leaves is the
			// widest of the span.
			name: "missing reading",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 20, charging(51, 6000, AC)),
				rec(40, 40, charging(55, 6000, AC)), // nothing read at 30
				rec(50, 50, charging(58, 6000, AC)),
				rec(60, 60, nrg(60, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(50), m(60)},
				Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(60), TargetSoC: val(80),
				EnergySoCKWh: val(8), EnergyPowerKWh: val(3),
				Span: keptSpan(51, 58, 3, 20*time.Minute, 30*time.Minute),
			}},
		},
		{
			// A reading without a SoC stops the span: the ones after it would add their
			// SoC change without the energy of its trapezoid.
			name: "span stops at a reading without a SoC",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 20, charging(51, 6000, AC)),
				rec(30, 30, charging(53, 6000, AC)),
				rec(40, 40, func() Snapshot { s := charging(0, 6000, AC); s.SoC = Value[float64]{}; return s }()),
				rec(50, 50, charging(57, 6000, AC)),
				rec(60, 60, nrg(58, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(50), m(60)},
				Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(58), TargetSoC: val(80),
				EnergySoCKWh: val(6.4), EnergyPowerKWh: val(3),
				Span: keptSpan(51, 53, 1, 10*time.Minute, 10*time.Minute),
			}},
		},
		{
			// A single reading with a power: no trapezoid, so no span and no integral.
			name: "single power reading",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 20, charging(51, 6000, AC)),
				rec(30, 30, nrg(52, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(20), m(30)},
				Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(52), TargetSoC: val(80),
				EnergySoCKWh: val(1.6),
			}},
		},
		{
			// The odometer is the one known at the reading that ends the charge, never
			// one read later.
			name: "odometer of the ending reading",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 0, odo(12400)),
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 30, charging(51, 7000, AC)),
				rec(31, 80, charging(55, 7000, AC)),
				rec(80, 80, odo(12432)),
				rec(81, 81, nrg(80, ChargingDone)),
				rec(90, 90, odo(12432.5)), // after the end: not taken
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(80), m(81)},
				Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(80), TargetSoC: val(80),
				EnergySoCKWh: val(24), EnergyPowerKWh: val(7),
				Span: keptSpan(51, 55, 7, 49*time.Minute, 60*time.Minute), OdometerKm: val(12432),
			}},
		},
		{
			// A pause (scheduled) neither ends the charge nor counts in the power
			// integral: no span either, the vehicle reports no charging power.
			name: "paused charge, no charging power",
			records: []Record{
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 30, func() Snapshot { s := nrg(51, ChargingActive); s.ChargeType = Some(DC, time.Time{}); return s }()),
				rec(31, 60, nrg(55, ChargingScheduled)),
				rec(61, 70, nrg(56, ChargingActive)),
				rec(71, 71, nrg(70, ChargingDone)),
			},
			want: []Charge{{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(70), m(71)},
				Type: Some(DC, time.Time{}), StartSoC: val(50), EndSoC: val(70), TargetSoC: val(80),
			}},
		},
		{
			// Charging is never seen (status not reported), but the SoC rose while parked.
			// The charge lasts as long as the SoC keeps rising.
			name: "reconstructed charge",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 60, Snapshot{Covers: energyFields, SoC: Some(40.0, time.Time{})}),
				rec(120, 120, Snapshot{Covers: energyFields, SoC: Some(60.0, m(115))}),
				rec(180, 240, Snapshot{Covers: energyFields, SoC: Some(80.0, m(170))}),
			},
			want: []Charge{{
				DetectedAt: m(120), Reconstructed: true, Start: Bounds{m(60), m(170)}, End: Bounds{m(60), m(170)},
				StartSoC: val(40), EndSoC: val(80), EnergySoCKWh: val(32),
			}},
		},
		{
			// A reconstructed charge takes the odometer known at its last rise, never one
			// read while it stayed open, and has no span: no power was read.
			name: "reconstructed charge, odometer of its last rise",
			records: []Record{
				rec(0, 0, capacity(80)),
				rec(0, 60, Snapshot{Covers: energyFields, SoC: Some(40.0, time.Time{})}),
				rec(120, 120, odo(12400)),
				rec(120, 120, Snapshot{Covers: energyFields, SoC: Some(60.0, m(115))}),
				rec(180, 180, Snapshot{Covers: energyFields, SoC: Some(80.0, m(170))}),
				rec(190, 190, odo(12400.5)), // read after the last rise: not taken
				rec(240, 240, Snapshot{Covers: energyFields, SoC: Some(80.0, m(170))}),
			},
			want: []Charge{{
				DetectedAt: m(120), Reconstructed: true, Start: Bounds{m(60), m(170)}, End: Bounds{m(60), m(170)},
				StartSoC: val(40), EndSoC: val(80), EnergySoCKWh: val(32),
				OdometerKm: val(12400),
			}},
		},
		{
			name: "SoC rise below the threshold",
			records: []Record{
				rec(0, 60, nrg(40, ChargingIdle)),
				rec(120, 120, nrg(41, ChargingIdle)),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Derive(tt.records, Cursor{}, DefaultParams(), nil)
			checkCharges(t, got.Charges, tt.want)
		})
	}
}

func checkCharges(t *testing.T, got, want []Charge) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d charges, want %d: %+v", len(got), len(want), got)
	}
	for i := range got {
		g := got[i]
		for _, v := range []*Value[float64]{&g.StartSoC, &g.EndSoC, &g.TargetSoC, &g.EnergySoCKWh, &g.EnergyPowerKWh, &g.OdometerKm, &g.StartRangeKm, &g.EndRangeKm, &g.EndUsableSoC} {
			v.At = time.Time{}
			v.V = float64(int(v.V*1000+0.5)) / 1000
		}
		if g.Span.OK {
			g.Span.V.EnergyKWh = float64(int(g.Span.V.EnergyKWh*1000+0.5)) / 1000
		}
		w := want[i]
		if !w.Capacity.OK { // not what the test is about: TestCapacity checks it
			g.Capacity = Value[Capacity]{}
		}
		// The fixtures' range is four times their SoC and their usable level one point
		// below it (nrg), or none: unless the test says otherwise, each end's are the ones
		// read with its SoC.
		if !w.StartRangeKm.OK && !w.EndRangeKm.OK && !w.EndUsableSoC.OK {
			read := func(r, soc Value[float64], f func(float64) float64) bool {
				return !r.OK || soc.OK && r == val(f(soc.V))
			}
			range4 := func(soc float64) float64 { return soc * 4 }
			usable := func(soc float64) float64 { return soc - 1 }
			if !read(g.StartRangeKm, g.StartSoC, range4) || !read(g.EndRangeKm, g.EndSoC, range4) || !read(g.EndUsableSoC, g.EndSoC, usable) {
				t.Errorf("charge %d: ranges %+v, %+v and usable level %+v not read with the SoC %+v, %+v",
					i, g.StartRangeKm, g.EndRangeKm, g.EndUsableSoC, g.StartSoC, g.EndSoC)
			}
			g.StartRangeKm, g.EndRangeKm, g.EndUsableSoC = Value[float64]{}, Value[float64]{}, Value[float64]{}
		}
		w.EnergyPowerKWh.V = float64(int(w.EnergyPowerKWh.V*1000+0.5)) / 1000
		if w.Span.OK {
			w.Span.V.EnergyKWh = float64(int(w.Span.V.EnergyKWh*1000+0.5)) / 1000
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("charge %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
}

// A charge takes the first position read after its end, within the settle window and
// before a trip starts: unplugged at home and driven straight to a charger, unseen, the
// vehicle uploads its new position during the second charge, read only after its end.
func TestChargePosition(t *testing.T) {
	twoCharges := func(more ...Record) []Record {
		return append([]Record{
			rec(0, 0, capacity(80)),
			rec(0, 0, engine(EngineStopped)),
			rec(0, 62, loc(home, m(0))), // read again as the first charge ends
			rec(0, 10, nrg(50, ChargingIdle)),
			rec(20, 59, charging(51, 7000, AC)),
			rec(60, 74, nrg(60, ChargingIdle)),
			rec(75, 94, charging(55, 50000, DC)),
			rec(95, 95, nrg(80, ChargingDone)),
		}, more...)
	}
	tests := []struct {
		name    string
		records []Record
		want    Value[Position]
	}{
		{"read after the end", twoCharges(rec(96, 96, loc(work, m(77)))), Some(work, m(77))},
		{"read after the window", twoCharges(rec(111, 111, loc(work, m(77)))), Some(home, m(0))},
		{"trip first", twoCharges(rec(100, 100, engine(EngineRunning)), rec(101, 101, loc(work, m(77)))), Some(home, m(0))},
		// One reading tells both: the position may be the one read as the trip starts.
		{"read as a trip starts", twoCharges(rec(100, 100, func() Snapshot {
			s := loc(work, m(77))
			s.Covers |= FieldEngine
			s.Engine = Some(EngineRunning, time.Time{})
			return s
		}())), Some(home, m(0))},
		{"none yet", twoCharges(), Some(home, m(0))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Derive(tt.records, Cursor{}, DefaultParams(), nil)
			if len(res.Charges) != 2 {
				t.Fatalf("charges %+v", res.Charges)
			}
			if got := res.Charges[0].Position; got != Some(home, m(0)) {
				t.Errorf("first charge at %+v", got)
			}
			if got := res.Charges[1].Position; got != tt.want {
				t.Errorf("second charge at %+v, want %+v", got, tt.want)
			}
		})
	}
}

// The report's day: unplugged at home, driven unseen to work, charged there. The
// readings the end of that charge triggers come in order, the odometer first, stamped
// when uploaded: it reveals the drive to work, which may as well have followed the
// charge, so the charge waits only for a position recorded before it ended. The trip a
// later odometer reading reveals starts at the position recorded before that reading.
func TestChargeAfterUnseenTrip(t *testing.T) {
	odoAt := func(km float64, at time.Time) Snapshot {
		return Snapshot{Covers: FieldOdometer, OdometerKm: Some(km, at)}
	}
	far := Position{Lat: 45.8, Lon: 4.9}
	day := func(more ...Record) []Record {
		return append([]Record{
			rec(0, 0, capacity(80)),
			rec(0, 200, engine(EngineStopped)),
			rec(0, 61, odoAt(1000, m(0))),
			rec(0, 61, loc(home, m(0))),
			rec(0, 10, nrg(50, ChargingIdle)),
			rec(20, 59, charging(51, 7000, AC)),
			rec(60, 74, nrg(60, ChargingIdle)), // unplugged at home, then driven to work
			rec(75, 94, charging(55, 50000, DC)),
			rec(95, 95, nrg(80, ChargingDone)),
		}, more...)
	}
	// The drive to work, revealed as the charge ends, stamped when uploaded.
	toWork := rec(96, 96, odoAt(1006, m(96)))
	// A drive away after the charge, the position and the odometer in one reading.
	farAway := odoAt(1012, m(99))
	farAway.Covers |= FieldPosition
	farAway.Position = Some(far, m(99))
	type ends struct{ from, to Value[Position] } // an arrival unset is not checked
	tests := []struct {
		name   string
		more   []Record
		charge Value[Position] // the second charge's
		trips  []ends
	}{
		{
			name: "the report's",
			more: []Record{
				toWork,
				rec(97, 120, loc(work, m(73))), // recorded during the charge, read just after
				rec(121, 121, odoAt(1008, m(115))),
				rec(122, 122, loc(far, m(115))),
			},
			charge: Some(work, m(73)),
			trips:  []ends{{Some(home, m(0)), Some(work, m(73))}, {Some(work, m(73)), Some(far, m(115))}},
		},
		{
			// The position recorded after the charge's end is not its own.
			name:   "driven away after the charge",
			more:   []Record{toWork, rec(100, 100, odoAt(1012, m(99))), rec(101, 120, loc(far, m(99)))},
			charge: Some(home, m(0)),
			trips:  []ends{{Some(home, m(0)), Some(far, m(99))}},
		},
		{
			// The same, the position and the odometer in one reading.
			name:   "driven away, one reading",
			more:   []Record{rec(100, 120, farAway)},
			charge: Some(home, m(0)),
			trips:  []ends{{from: Some(home, m(0))}},
		},
		{
			// The same, then at that very time the position recorded during the charge.
			name: "one reading, then the charge's position",
			more: []Record{
				rec(100, 120, farAway),
				rec(100, 120, loc(work, m(73))),
			},
			charge: Some(work, m(73)),
			trips:  []ends{{from: Some(home, m(0))}},
		},
		{
			// Read before the arrival, recorded after the trip had ended: a drive further
			// does not keep the trip from its arrival, nor the charge from its position.
			name: "drive further read first",
			more: []Record{
				toWork,
				rec(97, 97, loc(far, m(99))),
				rec(98, 120, loc(work, m(73))),
			},
			charge: Some(work, m(73)),
			trips:  []ends{{Some(home, m(0)), Some(work, m(73))}},
		},
		{
			// The position the charge ended with was recorded after its end bound (the
			// vehicle's clock ahead): it does not bound the next one from below.
			name: "position at the end past its bound",
			more: []Record{
				{FetchedAt: m(94).Add(30 * time.Second), CheckedAt: m(94).Add(30 * time.Second), Snapshot: loc(far, m(95).Add(30*time.Second))},
				toWork,
				rec(97, 120, loc(work, m(73))),
			},
			charge: Some(work, m(73)),
			trips:  []ends{{from: Some(home, m(0))}},
		},
		{
			// Older than the position the charge has: it would only stand in for it.
			name: "stale position under the bound",
			more: []Record{
				toWork,
				rec(97, 97, loc(far, m(-10))),
				rec(98, 120, loc(work, m(73))),
			},
			charge: Some(work, m(73)),
			trips:  []ends{{Some(home, m(0)), Some(work, m(73))}},
		},
		{
			// Read after the odometer, recorded before the position it had: no newer.
			name: "stale position",
			more: []Record{
				toWork,
				rec(97, 97, loc(work, m(73))),
				rec(98, 120, loc(far, m(50))),
				rec(121, 121, odoAt(1008, m(115))),
			},
			charge: Some(work, m(73)),
			trips:  []ends{{Some(home, m(0)), Some(work, m(73))}, {from: Some(work, m(73))}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Derive(day(tt.more...), Cursor{}, DefaultParams(), nil)
			if len(res.Charges) != 2 {
				t.Fatalf("charges %+v", res.Charges)
			}
			if got := res.Charges[0].Position; got != Some(home, m(0)) {
				t.Errorf("first charge at %+v", got)
			}
			if got := res.Charges[1].Position; got != tt.charge {
				t.Errorf("second charge at %+v, want %+v", got, tt.charge)
			}
			if len(res.Trips) != len(tt.trips) {
				t.Fatalf("trips %+v", res.Trips)
			}
			for i, w := range tt.trips {
				if tr := res.Trips[i]; tr.From != w.from || w.to.OK && tr.To != w.to {
					t.Errorf("trip %d from %+v to %+v, want %+v", i, tr.From, tr.To, w)
				}
			}
			trips, charges := deriveIncrementally(day(tt.more...), DefaultParams())
			if !reflect.DeepEqual(trips, res.Trips) || !reflect.DeepEqual(charges, res.Charges) {
				t.Errorf("incremental:\n%+v\n%+v\nrebuild:\n%+v\n%+v", trips, charges, res.Trips, res.Charges)
			}
		})
	}

	// Without the vehicle's timestamps, the drive revealed may have followed the charge.
	p := DefaultParams()
	p.VehicleTimestamps = false
	res := Derive(day(tests[0].more...), Cursor{}, p, nil)
	if len(res.Charges) != 2 || res.Charges[1].Position != Some(home, m(0)) {
		t.Errorf("without vehicle timestamps %+v", res.Charges)
	}
	// Nor does a trip's arrival tell an older position: the latest read is.
	res = Derive(day(tests[len(tests)-1].more...), Cursor{}, p, nil)
	if len(res.Trips) == 0 || res.Trips[0].To != Some(far, m(50)) {
		t.Errorf("arrival without vehicle timestamps %+v", res.Trips)
	}
}

// deriveIncrementally derives records as the collector would have them stored over
// time, from the cursor each derivation returns, as TestIncrementalMatchesRebuild does
// from readings.
func deriveIncrementally(records []Record, p Params) ([]Trip, []Charge) {
	r := incremental(records, p)
	return r.Trips, r.Charges
}

// incremental derives records as the collector does, one reading time after another,
// and returns every event it kept.
func incremental(records []Record, p Params) Result {
	var times []time.Time
	for _, r := range records {
		times = append(times, r.FetchedAt, r.CheckedAt)
	}
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	times = slices.Compact(times)
	var trips []Trip
	var charges []Charge
	var versions []SoftwareVersion
	var cursor Cursor
	for _, now := range times {
		var since []Record
		latest := map[Field]Record{}
		for _, r := range records {
			if r.FetchedAt.After(now) {
				continue
			}
			if r.CheckedAt.After(now) {
				r.CheckedAt = now
			}
			if r.FetchedAt.After(cursor.From) {
				since = append(since, r)
			} else if l, ok := latest[r.Snapshot.Covers]; !ok || r.FetchedAt.After(l.FetchedAt) {
				latest[r.Snapshot.Covers] = r
			}
		}
		for _, r := range latest {
			since = append(since, r)
		}
		res := Derive(since, cursor, p, nil)
		trips = append(keepTrips(trips, cursor.Settled), res.Trips...)
		charges = append(keepCharges(charges, cursor.Settled), res.Charges...)
		versions = append(slices.DeleteFunc(versions, func(v SoftwareVersion) bool { return v.DetectedAt.After(cursor.Settled) }), res.Versions...)
		cursor = res.Cursor
	}
	return Result{Trips: trips, Charges: charges, Versions: versions, Cursor: cursor}
}

// A trip starting while charging ends the charge; the trip's SoC ends before the next
// charge starts.
func TestTripAndCharge(t *testing.T) {
	records := []Record{
		rec(0, 0, capacity(100)),
		rec(0, 20, engine(EngineStopped)),
		rec(0, 20, odo(1000)),
		rec(0, 0, loc(home, m(0))),
		rec(0, 10, nrg(50, ChargingIdle)),
		rec(15, 25, charging(55, 50000, DC)),
		rec(30, 60, engine(EngineRunning)),
		rec(40, 40, nrg(50, ChargingIdle)),
		rec(59, 59, nrg(40, ChargingIdle)),
		rec(61, 61, engine(EngineStopped)),
		rec(61, 61, odo(1050)),
		rec(61, 61, loc(work, m(61))),
		rec(62, 62, charging(41, 50000, DC)),
		rec(63, 63, charging(50, 50000, DC)),
	}
	res := Derive(records, Cursor{}, DefaultParams(), nil)
	if len(res.Trips) != 1 || len(res.Charges) != 1 {
		t.Fatalf("trips %+v, charges %+v", res.Trips, res.Charges)
	}
	if c := res.Charges[0]; c.End != (Bounds{m(25), m(30)}) || c.EndSoC.V != 55 {
		t.Errorf("interrupted charge %+v", c)
	}
	if tr := res.Trips[0]; tr.EndSoC.V != 40 || tr.EnergyKWh.V != 15 {
		t.Errorf("trip SoC %+v → %+v, energy %+v", tr.StartSoC, tr.EndSoC, tr.EnergyKWh)
	}
}

// An unfinished trip (still waiting for its readings) is returned; a trip or a charge
// still in progress is not, and the cursor stays before them.
func TestDeriveInProgress(t *testing.T) {
	driving := trip()[:6]
	res := Derive(driving, Cursor{}, DefaultParams(), nil)
	if len(res.Trips) != 0 || !res.Cursor.Settled.Equal(m(20)) || !res.Cursor.From.Equal(m(0)) {
		t.Errorf("driving: %+v", res)
	}
	ending := trip()[:10]
	res = Derive(ending, Cursor{}, DefaultParams(), nil)
	if len(res.Trips) != 1 || res.Trips[0].To.OK || !res.Cursor.Settled.Equal(m(20)) {
		t.Errorf("ending: %+v", res)
	}
	res = Derive(trip(), Cursor{}, DefaultParams(), nil)
	if !res.Cursor.Settled.Equal(m(20)) {
		t.Errorf("within the settle window: cursor %+v", res.Cursor)
	}
	res = Derive(append(trip(), rec(100, 110, nrg(55, ChargingIdle))), Cursor{}, DefaultParams(), nil)
	if !res.Cursor.Settled.Equal(m(100)) || !res.Cursor.From.Equal(m(61)) {
		t.Errorf("settled: cursor %+v", res.Cursor)
	}
	charge := []Record{rec(0, 10, nrg(50, ChargingIdle)), rec(20, 30, charging(51, 7000, AC))}
	if res := Derive(charge, Cursor{}, DefaultParams(), nil); len(res.Charges) != 0 || !res.Cursor.Settled.Equal(m(10)) {
		t.Errorf("charging: %+v", res)
	}
	// Over, it is returned, but still waits for its position.
	charge = append(charge, rec(31, 40, nrg(60, ChargingDone)))
	if res := Derive(charge, Cursor{}, DefaultParams(), nil); len(res.Charges) != 1 || !res.Cursor.Settled.Equal(m(10)) {
		t.Errorf("charge over: %+v", res)
	}
	// Done with the cable connected, it may still resume: the cursor stays before it.
	charge = append(charge, rec(50, 60, nrg(61, ChargingDone)))
	if res := Derive(charge, Cursor{}, DefaultParams(), nil); len(res.Charges) != 1 || !res.Cursor.Settled.Equal(m(10)) {
		t.Errorf("charge resumable: %+v", res)
	}
	// Past MaxChargePause, it no longer may, though no energy reading tells.
	later := append(slices.Clone(charge), rec(31+24*60, 41+24*60, odo(12400)))
	if res := Derive(later, Cursor{}, DefaultParams(), nil); len(res.Charges) != 1 || !res.Cursor.Settled.Equal(m(31+24*60)) {
		t.Errorf("charge past its pause: %+v", res)
	}
	// Unplugged, it no longer may.
	charge = append(charge, rec(70, 80, nrg(61, ChargingIdle)))
	if res := Derive(charge, Cursor{}, DefaultParams(), nil); len(res.Charges) != 1 || !res.Cursor.Settled.Equal(m(70)) {
		t.Errorf("charge settled: %+v", res)
	}
}

// pausedAt is an energy reading of a charge stopped, the cable still connected: done,
// or idle as a Polestar says between two runs of a solar charge.
func pausedAt(soc float64, st ChargingStatus) Snapshot {
	s := nrg(soc, st)
	s.Connection = Some(Connected, time.Time{})
	return s
}

// A charge stopped with the cable connected goes on when the vehicle charges again
// (a solar charge), until the cable is disconnected, a trip, a reconstructed
// charge or MaxChargePause.
func TestChargeResumes(t *testing.T) {
	// solar charges at 1.5 kW from minute 20 to 30, stops (st), and again from resume
	// to resume + 29; between, more readings.
	solar := func(st ChargingStatus, resume int, between ...Record) []Record {
		rs := []Record{
			rec(0, 0, capacity(80)),
			rec(0, 0, odo(12400)),
			rec(0, 0, loc(home, m(0))),
			rec(0, 10, nrg(50, ChargingIdle)),
			rec(20, 30, charging(51, 1500, AC)),
			rec(31, 31, pausedAt(52, st)),
		}
		rs = append(rs, between...)
		return append(rs,
			rec(resume, resume+29, charging(53, 1500, AC)),
			rec(resume+30, resume+30, pausedAt(54, ChargingDone)),
		)
	}
	first := 1500 * 10 / 60.0 / 1000
	second := 1500 * 29 / 60.0 / 1000
	one := func(resume int) Charge {
		return Charge{
			DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(resume + 29), m(resume + 30)},
			Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(54), TargetSoC: val(80),
			EnergySoCKWh: val(3.2), EnergyPowerKWh: val(first + second), OdometerKm: val(12400),
			Position: Some(home, m(0)),
			// The runs on either side of the pause, the same SoC change: the first.
			Span: keptSpan(51, 51, first, 10*time.Minute, 10*time.Minute),
		}
	}
	two := func(after, resume int, odometer float64) []Charge {
		return []Charge{
			{
				DetectedAt: m(20), Start: Bounds{m(10), m(20)}, End: Bounds{m(30), m(31)},
				Type: Some(AC, time.Time{}), StartSoC: val(50), EndSoC: val(52), TargetSoC: val(80),
				EnergySoCKWh: val(1.6), EnergyPowerKWh: val(first), OdometerKm: val(12400),
				Position: Some(home, m(0)), Span: keptSpan(51, 51, first, 10*time.Minute, 10*time.Minute),
			},
			{
				DetectedAt: m(resume), Start: Bounds{m(after), m(resume)}, End: Bounds{m(resume + 29), m(resume + 30)},
				Type: Some(AC, time.Time{}), StartSoC: val(52), EndSoC: val(54), TargetSoC: val(80),
				EnergySoCKWh: val(1.6), EnergyPowerKWh: val(second), OdometerKm: val(odometer),
				Position: Some(home, m(0)), Span: keptSpan(53, 53, second, 29*time.Minute, 29*time.Minute),
			},
		}
	}
	night := 16 * 60
	noResume := DefaultParams()
	noResume.MaxChargePause = 0
	noStamps := DefaultParams()
	noStamps.VehicleTimestamps = false
	// stamped gives the charge's last reading the vehicle's timestamp of its status, and
	// the reading that paused it another: a timestamp before its floor narrows nothing.
	stamped := func(rs []Record, pause, end time.Time) []Record {
		rs = slices.Clone(rs)
		for i := range rs {
			if s := &rs[i].Snapshot; s.Covers&FieldCharging != 0 && s.Charging.V != ChargingActive && rs[i].FetchedAt.After(m(30)) {
				s.Charging.At = end
				if rs[i].FetchedAt.Equal(m(31)) {
					s.Charging.At = pause
				}
			}
		}
		return rs
	}
	narrowed := func(c Charge, before time.Time) Charge { c.End.Before = before; return c }
	late := m(night + 29).Add(30 * time.Second)
	tests := []struct {
		name    string
		records []Record
		params  Params
		want    []Charge
	}{
		{name: "idle overnight, connected", records: solar(ChargingIdle, night), want: []Charge{one(night)}},
		{name: "done, connected", records: solar(ChargingDone, 60), want: []Charge{one(60)}},
		{
			name:    "the pause read again",
			records: solar(ChargingIdle, night, rec(300, 600, pausedAt(52, ChargingIdle))),
			want:    []Charge{one(night)},
		},
		{
			name:    "unplugged in between",
			records: solar(ChargingIdle, night, rec(300, 300, nrg(52, ChargingIdle))),
			want:    two(300, night, 12400),
		},
		{
			name:    "the odometer read with the resume",
			records: solar(ChargingIdle, night, rec(night, night, odo(12400))),
			want:    []Charge{one(night)},
		},
		{
			name:    "longer than MaxChargePause",
			records: solar(ChargingIdle, 31+24*60),
			want:    two(31, 31+24*60, 12400),
		},
		{
			// The end narrowed by the reading that ended it, never by the one that paused it.
			name:    "vehicle timestamps",
			records: stamped(solar(ChargingIdle, night), m(30).Add(30*time.Second), late),
			want:    []Charge{narrowed(one(night), late)},
		},
		{
			// The pause stamped before the charge's last reading: out of order, ignored.
			name:    "vehicle timestamps out of order",
			records: stamped(solar(ChargingIdle, night), m(25), late),
			want:    []Charge{narrowed(one(night), late)},
		},
		{
			name:    "vehicle timestamps off",
			records: stamped(solar(ChargingIdle, night), m(30).Add(30*time.Second), late),
			params:  noStamps,
			want:    []Charge{one(night)},
		},
		{name: "MaxChargePause 0", records: solar(ChargingDone, 60), params: noResume, want: two(31, 60, 12400)},
		{
			// Unplugged and driven unseen: the odometer reveals the trip.
			name:    "a trip in between",
			records: solar(ChargingIdle, night, rec(300, 300, odo(12405))),
			want:    two(31, night, 12405),
		},
		{
			name: "a trip seen in between",
			records: solar(ChargingIdle, night,
				rec(0, 0, engine(EngineStopped)),
				rec(300, 300, engine(EngineRunning)),
				rec(310, 310, engine(EngineStopped)),
			),
			want: two(31, night, 12400),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.params
			if p == (Params{}) {
				p = DefaultParams()
			}
			res := Derive(tt.records, Cursor{}, p, nil)
			checkCharges(t, res.Charges, tt.want)
			trips, charges := deriveIncrementally(tt.records, p)
			if !reflect.DeepEqual(trips, res.Trips) || !reflect.DeepEqual(charges, res.Charges) {
				t.Errorf("incremental:\n%+v\n%+v\nrebuild:\n%+v\n%+v", trips, charges, res.Trips, res.Charges)
			}
		})
	}

	// The SoC rising unseen while it may resume is a reconstructed charge, as without
	// the pause: it ends the first, which no longer resumes.
	records := solar(ChargingIdle, night, rec(300, 300, pausedAt(55, ChargingIdle)))
	res := Derive(records, Cursor{}, DefaultParams(), nil)
	if len(res.Charges) != 2 || res.Charges[0].End != (Bounds{m(30), m(31)}) || res.Charges[1].DetectedAt != m(300) {
		t.Errorf("reconstructed in the pause: %+v", res.Charges)
	}
	if trips, charges := deriveIncrementally(records, DefaultParams()); !reflect.DeepEqual(trips, res.Trips) || !reflect.DeepEqual(charges, res.Charges) {
		t.Errorf("reconstructed in the pause, incremental:\n%+v\nrebuild:\n%+v", charges, res.Charges)
	}
}

// TestIncrementalMatchesRebuild feeds readings one at a time, as the collector stores
// them (identical readings only advance CheckedAt), derives incrementally after each
// one, and compares with a derivation of the whole history.
func TestIncrementalMatchesRebuild(t *testing.T) {
	type reading struct {
		at   int
		snap Snapshot
	}
	var day []reading
	add := func(at int, s Snapshot) { day = append(day, reading{at, s}) }
	add(0, details(80, "EX30", 2024, true))
	add(0, loc(home, m(0)))
	for i := 0; i <= 600; i += 10 {
		if i == 350 { // the backend reports another capacity, as the EX30's did for months
			add(i, details(66, "EX30", 2024, true))
		}
		soc, e, km := 62.0, EngineStopped, 12400.0
		switch {
		case i >= 60 && i < 100: // driving
			soc, e, km = 62-float64(i-60)/5, EngineRunning, 12400+float64(i-60)
		case i >= 100 && i < 300:
			soc, km = 54, 12440
		case i >= 300 && i < 400: // an unseen trip: only the odometer reveals it
			soc, km = 50, 12470
		case i >= 400: // charging
			soc, km = min(50+float64(i-400)/5, 80), 12470
		}
		add(i, engine(e))
		st := ChargingIdle
		if i >= 400 && soc < 80 {
			st = ChargingActive
		} else if i >= 400 {
			st = ChargingDone
		}
		add(i, charging(soc, 7000, AC))
		day[len(day)-1].snap.Charging = Some(st, time.Time{})
		if i%60 == 0 || i == 100 {
			add(i, odo(km))
		}
		if i == 100 {
			add(i, loc(work, m(100)))
		}
		if i == 560 { // read after the charge, which takes it
			add(i, loc(home, m(555)))
		}
	}

	// The catalog knows the EX30 at 80 kWh only.
	net := func(s Snapshot) (float64, bool) { return 64, s.Family.V == "EX30" && s.CapacityKWh.V == 80 }

	var records []Record
	last := map[Field]int{} // index in records of the latest record, by Covers
	var trips []Trip
	var charges []Charge
	var cursor Cursor
	for _, r := range day {
		if i, ok := last[r.snap.Covers]; ok && reflect.DeepEqual(records[i].Snapshot, r.snap) {
			records[i].CheckedAt = m(r.at)
		} else {
			last[r.snap.Covers] = len(records)
			// Two sources, so that the events' sources are compared too.
			source := "volvo"
			if r.snap.Covers == FieldEngine {
				source = "car"
			}
			records = append(records, Record{Source: source, FetchedAt: m(r.at), CheckedAt: m(r.at), Snapshot: r.snap})
		}
		// What the store returns: the records fetched after the cursor, and the latest
		// one of each kind fetched at or before it.
		var since []Record
		latest := map[Field]Record{}
		for _, rc := range records {
			if rc.FetchedAt.After(cursor.From) {
				since = append(since, rc)
			} else {
				latest[rc.Snapshot.Covers] = rc
			}
		}
		for _, rc := range latest {
			since = append(since, rc)
		}
		res := Derive(since, cursor, DefaultParams(), net)
		trips = append(keepTrips(trips, cursor.Settled), res.Trips...)
		charges = append(keepCharges(charges, cursor.Settled), res.Charges...)
		cursor = res.Cursor
	}

	full := Derive(records, Cursor{}, DefaultParams(), net)
	if len(full.Trips) != 2 || len(full.Charges) != 1 || !full.Trips[1].Reconstructed {
		t.Fatalf("full derivation: %+v", full)
	}
	// Each event takes the capacity of the readings that end it: the catalog's before the
	// change, even for the reconstructed trip, which only settles after it.
	netCap := Value[Capacity]{V: Capacity{KWh: 64, Source: CapacityCatalogNet}, OK: true}
	apiCap := Value[Capacity]{V: Capacity{KWh: 66, Source: CapacityAPI}, OK: true}
	if full.Trips[0].Capacity != netCap || full.Trips[1].Capacity != netCap || full.Charges[0].Capacity != apiCap {
		t.Errorf("capacities %+v, %+v, %+v", full.Trips[0].Capacity, full.Trips[1].Capacity, full.Charges[0].Capacity)
	}
	if got := full.Charges[0].Position; got != Some(home, m(555)) {
		t.Errorf("charge at %+v", got)
	}
	if got := full.Trips[0].Sources; !slices.Equal(got, []string{"car", "volvo"}) {
		t.Errorf("sources %q", got)
	}
	if !reflect.DeepEqual(trips, full.Trips) || !reflect.DeepEqual(charges, full.Charges) {
		t.Errorf("incremental:\n%+v\n%+v\nfull:\n%+v\n%+v", trips, charges, full.Trips, full.Charges)
	}
	if cursor.Settled.IsZero() || cursor.From.After(cursor.Settled) {
		t.Errorf("cursor %+v", cursor)
	}
}

// An event's sources are those of the readings from its start to its end, never of
// the readings before it.
func TestSources(t *testing.T) {
	records := trip()
	for i := range records {
		records[i].Source = "volvo"
	}
	records[0].Source = "phone" // before the trip
	records[7].Source = "car"   // while driving
	res := Derive(records, Cursor{}, DefaultParams(), nil)
	if len(res.Trips) != 1 {
		t.Fatalf("trips %+v", res.Trips)
	}
	if got := res.Trips[0].Sources; !slices.Equal(got, []string{"car", "volvo"}) {
		t.Errorf("sources %q, want car and volvo", got)
	}

	charge := []Record{
		{Source: "volvo", FetchedAt: m(0), CheckedAt: m(10), Snapshot: nrg(50, ChargingIdle)},
		{Source: "volvo", FetchedAt: m(20), CheckedAt: m(30), Snapshot: charging(51, 7000, AC)},
		{Source: "volvo", FetchedAt: m(40), CheckedAt: m(100), Snapshot: nrg(60, ChargingDone)},
	}
	res = Derive(charge, Cursor{}, DefaultParams(), nil)
	if len(res.Charges) != 1 || !slices.Equal(res.Charges[0].Sources, []string{"volvo"}) {
		t.Errorf("charges %+v", res.Charges)
	}

	// A reconstructed trip whose bounds the arrival timestamp narrows to before the
	// reading that revealed it: no reading lies within them, that one still counts.
	missed := []Record{
		rec(0, 120, engine(EngineStopped)),
		rec(0, 20, nrg(62, ChargingIdle)),
		rec(60, 60, nrg(61, ChargingIdle)),
		rec(0, 60, odo(12400)),
		rec(0, 60, loc(home, m(0))),
		rec(120, 120, odo(12432)),
		rec(120, 120, loc(work, m(100))),
	}
	for i := range missed {
		missed[i].Source = "volvo"
	}
	res = Derive(missed, Cursor{}, DefaultParams(), nil)
	if len(res.Trips) != 1 || res.Trips[0].End.Before != m(100) || !slices.Equal(res.Trips[0].Sources, []string{"volvo"}) {
		t.Errorf("reconstructed trips %+v", res.Trips)
	}
}

func TestCapacity(t *testing.T) {
	// As the catalog would: a battery electric EX30, of 64 kWh net.
	ex30 := func(s Snapshot) (float64, bool) { return 64, s.Family.V == "EX30" && s.BatteryElectric() }
	net := Value[Capacity]{V: Capacity{KWh: 64, Source: CapacityCatalogNet}, OK: true}
	api := Value[Capacity]{V: Capacity{KWh: 69, Source: CapacityAPI}, OK: true}
	tests := []struct {
		name     string
		details  Snapshot
		source   CapacitySource
		net      NetCapacity
		want     Value[Capacity]
		trip, ch float64 // energies of the trip (62 → 55 %) and of the charge (55 → 75 %)
	}{
		{"the catalog's net capacity", details(69, "EX30", 2024, true), CapacityCatalogNet, ex30, net, 4.48, 12.8},
		{"a variant the catalog does not know", details(69, "EX-SIM", 2026, true), CapacityCatalogNet, ex30, api, 4.83, 13.8},
		{"no catalog", details(69, "EX30", 2024, true), CapacityCatalogNet, nil, api, 4.83, 13.8},
		{"a hybrid", details(69, "EX30", 2024, false), CapacityCatalogNet, ex30, api, 4.83, 13.8},
		{"the API's, by choice", details(69, "EX30", 2024, true), CapacityAPI, ex30, api, 4.83, 13.8},
		// The API's capacity is unknown, not the catalog's.
		{
			"the net capacity without the API's",
			Snapshot{Covers: FieldModel, Family: Some("EX30", time.Time{}), Powertrain: Some(Electric, time.Time{})},
			CapacityCatalogNet, ex30, net, 4.48, 12.8,
		},
		{"no capacity at all", Snapshot{Covers: FieldModel}, CapacityCatalogNet, ex30, Value[Capacity]{}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := append(trip(), rec(100, 100, charging(56, 7000, AC)), rec(110, 110, charging(75, 7000, AC)),
				rec(120, 120, nrg(75, ChargingDone)))
			records[0] = rec(0, 0, tt.details)
			p := DefaultParams()
			p.Capacity = tt.source
			res := Derive(records, Cursor{}, p, tt.net)
			if len(res.Trips) != 1 || len(res.Charges) != 1 {
				t.Fatalf("trips %+v, charges %+v", res.Trips, res.Charges)
			}
			tr, c := res.Trips[0], res.Charges[0]
			if tr.Capacity != tt.want || c.Capacity != tt.want {
				t.Errorf("capacity of the trip %+v, of the charge %+v; want %+v", tr.Capacity, c.Capacity, tt.want)
			}
			roundTrip(&tr)
			if tr.EnergyKWh.V != tt.trip || tr.EnergyKWh.OK != tt.want.OK {
				t.Errorf("energy of the trip %+v, want %v", tr.EnergyKWh, tt.trip)
			}
			if e := c.EnergySoCKWh; float64(int(e.V*1000+0.5))/1000 != tt.ch || e.OK != tt.want.OK {
				t.Errorf("energy of the charge %+v, want %v", e, tt.ch)
			}
		})
	}
}

func keepTrips(ts []Trip, settled time.Time) []Trip {
	var out []Trip
	for _, t := range ts {
		if !t.DetectedAt.After(settled) {
			out = append(out, t)
		}
	}
	return out
}

func keepCharges(cs []Charge, settled time.Time) []Charge {
	var out []Charge
	for _, c := range cs {
		if !c.DetectedAt.After(settled) {
			out = append(out, c)
		}
	}
	return out
}

func TestPointsOrder(t *testing.T) {
	// Same time: the engine before the energy, before the odometer; a confirmation
	// before the next record of the same kind.
	ps := points([]Record{
		rec(10, 10, odo(1)),
		rec(10, 10, nrg(50, ChargingIdle)),
		rec(10, 10, engine(EngineStopped)),
		rec(0, 10, engine(EngineRunning)),
		{Snapshot: Snapshot{}}, // nothing covered: ignored
	})
	var got []Field
	for _, p := range ps {
		got = append(got, p.snap.Covers)
	}
	want := []Field{FieldEngine, FieldEngine, FieldEngine, energyFields, FieldOdometer}
	if !reflect.DeepEqual(got, want) || !ps[1].confirm || ps[2].confirm {
		t.Errorf("order %v, points %+v", got, ps)
	}
	if !sort.SliceIsSorted(ps, func(i, j int) bool { return ps[i].at.Before(ps[j].at) }) {
		t.Error("not sorted by time")
	}
}

// TestDeriveMeasuredEnergy: the energy the vehicle's counter measured over a charge, a
// new session's counted from zero, a session resumed from where it stood.
func TestDeriveMeasuredEnergy(t *testing.T) {
	// counted is a reading with the vehicle's counter.
	counted := func(s Snapshot, kwh float64) Snapshot {
		s.Covers |= FieldEnergyAdded
		s.EnergyAddedKWh = Some(kwh, time.Time{})
		return s
	}
	tests := []struct {
		name    string
		records []Record
		want    Value[float64]
	}{
		{
			// The previous session's count holds until the cable is plugged in again.
			name: "a new session",
			records: []Record{
				rec(0, 10, counted(nrg(50, ChargingIdle), 30)),
				rec(20, 20, counted(charging(51, 11000, AC), 0.2)),
				rec(40, 40, counted(charging(60, 11000, AC), 3.9)),
				rec(60, 60, counted(nrg(65, ChargingDone), 5.7)),
			},
			want: val(5.7),
		},
		{
			// Charging again in the same session: the count goes on from where it stood.
			name: "a session resumed",
			records: []Record{
				rec(0, 10, counted(nrg(50, ChargingScheduled), 4)),
				rec(20, 20, counted(charging(51, 11000, AC), 4.2)),
				rec(40, 40, counted(nrg(60, ChargingDone), 7.5)),
			},
			want: val(3.5),
		},
		{
			// Unplugged before: the charge starts a new session, counted whole, though the
			// first reading has already passed the previous session's count.
			name: "plugged in after a small session",
			records: []Record{
				rec(0, 10, counted(nrg(50, ChargingIdle), 0.2)),
				rec(20, 20, counted(charging(51, 11000, AC), 2)),
				rec(40, 40, counted(nrg(60, ChargingDone), 5)),
			},
			want: val(5),
		},
		{
			// Unplugged before, no count known: counted from zero.
			name: "plugged in, no count before",
			records: []Record{
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 20, counted(charging(51, 11000, AC), 0.5)),
				rec(40, 40, counted(nrg(60, ChargingDone), 4)),
			},
			want: val(4),
		},
		{
			// Plugged in all along, nothing known before the charge: its first reading is
			// the base.
			name: "no count before the charge",
			records: []Record{
				rec(0, 10, nrg(50, ChargingScheduled)),
				rec(20, 20, counted(charging(51, 11000, AC), 0.5)),
				rec(40, 40, counted(nrg(60, ChargingDone), 4)),
			},
			want: val(3.5),
		},
		{
			// Done, then charging again with the cable connected: the same charge, its
			// count going on.
			name: "a charge resumed",
			records: []Record{
				rec(0, 10, counted(nrg(50, ChargingIdle), 12)),
				rec(20, 20, counted(charging(51, 7000, AC), 0.1)),
				rec(40, 40, counted(nrg(55, ChargingDone), 2)),
				rec(100, 100, counted(charging(55, 7000, AC), 2)),
				rec(120, 120, counted(nrg(60, ChargingDone), 4.5)),
			},
			want: val(4.5),
		},
		{
			// A counter that never rose is no measure of 0 kWh: the estimates stand.
			name: "a counter that never rose",
			records: []Record{
				rec(0, 10, counted(nrg(50, ChargingIdle), 0)),
				rec(20, 20, counted(charging(51, 11000, AC), 0)),
				rec(40, 40, counted(nrg(60, ChargingDone), 0)),
			},
		},
		{
			name: "a vehicle without a counter",
			records: []Record{
				rec(0, 10, nrg(50, ChargingIdle)),
				rec(20, 20, charging(51, 11000, AC)),
				rec(40, 40, nrg(60, ChargingDone)),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Derive(tt.records, Cursor{}, DefaultParams(), nil)
			if len(res.Charges) != 1 {
				t.Fatalf("%d charges: %+v", len(res.Charges), res.Charges)
			}
			got := res.Charges[0].EnergyMeasuredKWh
			if got.OK != tt.want.OK || math.Abs(got.V-tt.want.V) > 1e-9 {
				t.Errorf("measured %+v, want %+v", got, tt.want)
			}
		})
	}
}

// software is a reading of the vehicle's software version.
func software(v string) Snapshot {
	return Snapshot{Covers: FieldSoftware, Software: Some(v, time.Time{})}
}

// Each version of the software is reported once, when first read: bounded by the last
// reading of the previous one, the first with none before it.
func TestSoftwareVersions(t *testing.T) {
	tests := []struct {
		name    string
		records []Record
		want    []SoftwareVersion
	}{
		{"none", []Record{rec(0, 10, nrg(50, ChargingIdle))}, nil},
		{"the first", []Record{rec(5, 30, software("2026.38.3.1"))}, []SoftwareVersion{
			{DetectedAt: m(5), Version: "2026.38.3.1", Installed: Bounds{Before: m(5)}},
		}},
		{
			// Read again unchanged until minute 30: the new one was installed after it.
			"an update",
			[]Record{
				rec(5, 30, software("2026.38.3.1")),
				rec(40, 50, software("2026.38.3.2")),
			},
			[]SoftwareVersion{
				{DetectedAt: m(5), Version: "2026.38.3.1", Installed: Bounds{Before: m(5)}},
				{DetectedAt: m(40), Version: "2026.38.3.2", Installed: Bounds{After: m(30), Before: m(40)}},
			},
		},
		{
			// Other readings in between change nothing; the same version read again is no
			// new one.
			"the same version again",
			[]Record{
				rec(5, 5, software("2026.38.3.1")),
				rec(10, 10, odo(12400)),
				rec(20, 20, nrg(50, ChargingIdle)),
				rec(60, 60, software("2026.38.3.1")),
				rec(70, 70, odo(12401)),
				rec(90, 90, software("2026.44.1")),
			},
			[]SoftwareVersion{
				{DetectedAt: m(5), Version: "2026.38.3.1", Installed: Bounds{Before: m(5)}},
				{DetectedAt: m(90), Version: "2026.44.1", Installed: Bounds{After: m(60), Before: m(90)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Derive(tt.records, Cursor{}, DefaultParams(), nil)
			if !reflect.DeepEqual(res.Versions, tt.want) {
				t.Errorf("versions:\n got %+v\nwant %+v", res.Versions, tt.want)
			}
			if inc := incremental(tt.records, DefaultParams()); !reflect.DeepEqual(inc.Versions, res.Versions) {
				t.Errorf("incremental:\n%+v\nrebuild:\n%+v", inc.Versions, res.Versions)
			}
		})
	}
}

// parked is an energy reading of a parked vehicle: not charging, the cable as given, the
// SoC recorded by the vehicle at its minute.
func parked(soc float64, c Connection, at int) Snapshot {
	return Snapshot{
		Covers: energyFields, SoC: Some(soc, m(at)), Charging: Some(ChargingIdle, m(at)),
		Connection: Some(c, m(at)),
	}
}

func powertrain(p Powertrain) Snapshot {
	return Snapshot{Covers: FieldModel, Powertrain: Some(p, time.Time{})}
}

// A plug-in hybrid's engine raises the SoC while driving (a charge mode, or charge
// sustain): a rise read parked is a charge only when the reading that shows it says the
// cable is connected. A battery electric vehicle, or one whose powertrain is not known
// yet, keeps the rule of the SoC alone.
func TestHybridReconstructedCharges(t *testing.T) {
	charge := func(at int) []Charge {
		return []Charge{{
			DetectedAt: m(at), Reconstructed: true, Start: Bounds{m(60), m(at - 5)}, End: Bounds{m(60), m(at - 5)},
			StartSoC: val(40), EndSoC: val(60), EnergySoCKWh: val(3.6),
		}}
	}
	tests := []struct {
		name    string
		records []Record
		want    []Charge
	}{
		{
			name: "plug-in hybrid, the cable connected",
			records: []Record{
				rec(0, 0, powertrain(PlugInHybrid)),
				rec(0, 60, parked(40, Disconnected, 0)),
				rec(120, 120, parked(60, Connected, 115)),
			},
			want: charge(120),
		},
		{
			name: "plug-in hybrid, the cable disconnected",
			records: []Record{
				rec(0, 0, powertrain(PlugInHybrid)),
				rec(0, 60, parked(40, Disconnected, 0)),
				rec(120, 120, parked(60, Disconnected, 115)),
			},
		},
		{
			name: "plug-in hybrid, the cable not reported",
			records: []Record{
				rec(0, 0, powertrain(PlugInHybrid)),
				rec(0, 60, Snapshot{Covers: energyFields, SoC: Some(40.0, m(0))}),
				rec(120, 120, Snapshot{Covers: energyFields, SoC: Some(60.0, m(115))}),
			},
		},
		{
			// The cable read connected earlier says nothing of the reading that shows the
			// rise.
			name: "plug-in hybrid, the cable connected before, not since",
			records: []Record{
				rec(0, 0, powertrain(PlugInHybrid)),
				rec(0, 60, parked(40, Connected, 0)),
				rec(120, 120, Snapshot{Covers: FieldSoC, SoC: Some(60.0, m(115))}),
			},
		},
		{
			name: "battery electric, the cable disconnected",
			records: []Record{
				rec(0, 0, powertrain(Electric)),
				rec(0, 60, parked(40, Disconnected, 0)),
				rec(120, 120, parked(60, Disconnected, 115)),
			},
			want: charge(120),
		},
		{
			// Until its details are read, a vehicle is taken for an electric one.
			name: "plug-in hybrid read after the rise",
			records: []Record{
				rec(0, 60, parked(40, Disconnected, 0)),
				rec(120, 120, parked(60, Disconnected, 115)),
				rec(130, 130, powertrain(PlugInHybrid)),
			},
			want: charge(120),
		},
		{
			// The engine charging the battery on a trip that was seen: never a charge, for
			// any vehicle.
			name: "plug-in hybrid, a charge mode while driving",
			records: []Record{
				rec(0, 0, powertrain(PlugInHybrid)),
				rec(0, 20, engine(EngineStopped)),
				rec(0, 20, parked(30, Disconnected, 0)),
				rec(0, 20, odo(1550)),
				rec(30, 30, engine(EngineRunning)),
				rec(30, 30, parked(30, Disconnected, 30)),
				rec(40, 40, parked(34, Disconnected, 40)),
				rec(50, 50, parked(38, Disconnected, 50)),
				rec(61, 61, engine(EngineStopped)),
				rec(61, 61, odo(1580)),
				rec(70, 120, parked(38, Disconnected, 70)),
			},
		},
		{
			// A V60's battery held empty by its engine on a 41 km trip: one point up and
			// down, never a charge.
			name: "plug-in hybrid, charge sustain",
			records: []Record{
				rec(0, 0, powertrain(PlugInHybrid)),
				rec(0, 0, engine(EngineRunning)),
				rec(0, 0, parked(3, Disconnected, 0)),
				rec(5, 5, parked(2, Disconnected, 5)),
				rec(10, 10, parked(3, Disconnected, 10)),
				rec(15, 15, parked(2, Disconnected, 15)),
				rec(20, 20, engine(EngineStopped)),
				rec(25, 25, parked(3, Disconnected, 25)),
				rec(35, 90, parked(2, Disconnected, 35)),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := append([]Record{rec(0, 0, capacity(18))}, tt.records...)
			for _, p := range []Params{DefaultParams(), func() Params { p := DefaultParams(); p.VehicleTimestamps = false; return p }()} {
				want := slices.Clone(tt.want)
				if !p.VehicleTimestamps {
					for i := range want { // the polling bounds alone
						want[i].Start.Before, want[i].End.Before = m(120), m(120)
					}
				}
				got := Derive(records, Cursor{}, p, nil)
				checkCharges(t, got.Charges, want)
				_, charges := deriveIncrementally(records, p)
				checkCharges(t, charges, want)
			}
		})
	}
}
