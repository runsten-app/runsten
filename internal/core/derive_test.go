package core

import (
	"reflect"
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

// nrg is an energy reading: SoC, status, and the cable connected unless idle.
func nrg(soc float64, st ChargingStatus) Snapshot {
	s := Snapshot{
		Covers: energyFields, SoC: Some(soc, time.Time{}), RangeKm: Some(soc*4, time.Time{}),
		Charging: Some(st, time.Time{}), TargetSoC: Some(80.0, time.Time{}),
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
		for _, v := range []*Value[float64]{&g.StartSoC, &g.EndSoC, &g.TargetSoC, &g.EnergySoCKWh, &g.EnergyPowerKWh, &g.OdometerKm} {
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
		w.EnergyPowerKWh.V = float64(int(w.EnergyPowerKWh.V*1000+0.5)) / 1000
		if w.Span.OK {
			w.Span.V.EnergyKWh = float64(int(w.Span.V.EnergyKWh*1000+0.5)) / 1000
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("charge %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
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
			records = append(records, Record{FetchedAt: m(r.at), CheckedAt: m(r.at), Snapshot: r.snap})
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
	if !reflect.DeepEqual(trips, full.Trips) || !reflect.DeepEqual(charges, full.Charges) {
		t.Errorf("incremental:\n%+v\n%+v\nfull:\n%+v\n%+v", trips, charges, full.Trips, full.Charges)
	}
	if cursor.Settled.IsZero() || cursor.From.After(cursor.Settled) {
		t.Errorf("cursor %+v", cursor)
	}
}

func TestCapacity(t *testing.T) {
	// As the catalog would: a battery electric EX30, of 64 kWh net.
	ex30 := func(s Snapshot) (float64, bool) { return 64, s.Family.V == "EX30" && s.BatteryElectric.V }
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
			Snapshot{Covers: FieldModel, Family: Some("EX30", time.Time{}), BatteryElectric: Some(true, time.Time{})},
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
