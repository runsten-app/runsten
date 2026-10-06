package core

import "time"

type tripPhase int

const (
	tripDriving tripPhase = iota // the engine was seen running
	tripEnding                   // over; later readings may still complete it
)

type tripBuilder struct {
	t     Trip
	phase tripPhase

	lastRunning  time.Time   // last reading of a running engine
	stopAt       time.Time   // first reading showing the trip over
	endLimit     time.Time   // End.Before from polling alone
	hints        []time.Time // vehicle timestamps that may narrow End.Before
	departureRow time.Time   // FetchedAt of the departure position
	arrived      bool
	run          energyRun
}

// stepTrip runs the trip machine.
//
// Start: the engine is seen running, or the odometer rose between two readings (a
// reconstructed trip). End: the engine is seen stopped; the trip then waits
// SettleWindow for the readings that complete it, the arrival position first.
func (d *detector) stepTrip(p point, prev state) {
	s := d.st.snap
	engine := p.snap.Covers&FieldEngine != 0 && s.Engine.OK
	running := engine && s.Engine.V == EngineRunning

	tb := d.trip
	if tb != nil && tb.phase == tripEnding && !p.at.Before(tb.stopAt.Add(d.p.SettleWindow)) {
		d.finishTrip()
		tb = nil
	}
	switch {
	case tb == nil:
		if running {
			d.startTrip(p, prev)
			return
		}
		if p.snap.Covers&FieldOdometer != 0 && prev.snap.OdometerKm.OK && s.OdometerKm.OK &&
			s.OdometerKm.V-prev.snap.OdometerKm.V >= d.p.MinTripKm {
			d.reconstructTrip(p, prev)
		}
	case tb.phase == tripDriving:
		if running {
			tb.lastRunning = p.at
		} else if engine {
			tb.phase, tb.stopAt = tripEnding, p.at
			tb.t.Capacity = d.capacity()
			tb.t.End.After, tb.endLimit = tb.lastRunning, p.at
			tb.hints = []time.Time{s.Engine.At}
		}
	case running:
		if tb.arrived || !d.p.ResumeBeforeArrival {
			d.finishTrip()
			d.startTrip(p, prev)
			return
		}
		d.resumeTrip(tb, p)
	}
	if tb := d.trip; tb != nil && tb.phase == tripEnding {
		tb.checkArrival(&d.st)
	}
}

func (d *detector) startTrip(p point, prev state) {
	d.interruptCharge(p, prev)
	after := later(d.lastNotRunning, prev.seenAt(FieldOdometer))
	if after.IsZero() {
		after = p.at
	}
	tb := &tripBuilder{
		t: Trip{
			DetectedAt:      p.at,
			Start:           Bounds{After: after, Before: d.narrow(p.at, after, d.st.snap.Engine.At)},
			StartOdometerKm: prev.snap.OdometerKm,
			StartSoC:        prev.snap.SoC,
			StartRangeKm:    prev.snap.RangeKm,
			From:            prev.snap.Position,
		},
		phase:        tripDriving,
		lastRunning:  p.at,
		departureRow: prev.rowAt(FieldPosition),
	}
	tb.run.reset(prev.snap)
	d.trip = tb
}

// reconstructTrip opens a trip that was not seen: the odometer rose since its last
// reading. It started after that reading; its start values are those of that time.
func (d *detector) reconstructTrip(p point, prev state) {
	m := d.odoMark
	after := prev.seenAt(FieldOdometer)
	d.trip = &tripBuilder{
		t: Trip{
			DetectedAt:      p.at,
			Reconstructed:   true,
			Start:           Bounds{After: after},
			End:             Bounds{After: after},
			StartOdometerKm: prev.snap.OdometerKm,
			StartSoC:        m.snap.SoC,
			StartRangeKm:    m.snap.RangeKm,
			From:            m.snap.Position,
			Capacity:        d.capacity(),
		},
		phase:        tripEnding,
		stopAt:       p.at,
		endLimit:     p.at,
		hints:        []time.Time{d.st.snap.OdometerKm.At},
		departureRow: m.rowAt(FieldPosition),
		run:          d.idleRun,
	}
}

// resumeTrip continues a trip seen running again before it reached a new position. A
// reconstructed trip becomes an observed one: it started at the latest when it was
// detected.
func (d *detector) resumeTrip(tb *tripBuilder, p point) {
	if tb.t.Reconstructed {
		tb.t.Reconstructed = false
		tb.t.Start.Before = d.narrow(tb.t.DetectedAt, tb.t.Start.After, tb.hints...)
	}
	tb.phase, tb.lastRunning = tripDriving, p.at
	tb.stopAt, tb.endLimit, tb.hints = time.Time{}, time.Time{}, nil
	tb.t.End, tb.t.Capacity = Bounds{}, Value[Capacity]{}
}

// checkArrival takes a position read after the departure one as the arrival.
// Assumption: the vehicle uploads its position at the end of a trip only.
func (tb *tripBuilder) checkArrival(st *state) {
	if pos := st.snap.Position; pos.OK && st.rowAt(FieldPosition).After(tb.departureRow) {
		tb.t.To, tb.arrived = pos, true
	}
}

func (d *detector) finishTrip() {
	d.res.Trips = append(d.res.Trips, d.buildTrip(d.trip))
	d.trip = nil
}

func (d *detector) buildTrip(tb *tripBuilder) Trip {
	t, s := tb.t, d.st.snap
	t.End.Before = d.narrow(tb.endLimit, t.End.After, append(tb.hints, t.To.At)...)
	if t.Reconstructed {
		t.Start.Before = t.End.Before
	}
	t.EndOdometerKm = s.OdometerKm
	t.DistanceKm = diff(t.EndOdometerKm, t.StartOdometerKm)
	t.EndSoC, t.EndRangeKm = tb.run.soc, tb.run.rng
	t.EnergyKWh = energy(t.StartSoC, t.EndSoC, t.Capacity)
	// The trip statistics of a reconstructed trip may describe another one.
	if !t.Reconstructed && !d.st.rowAt(FieldTripMeter).Before(tb.stopAt) {
		t.TripMeterKm, t.ConsumptionKWhPer100km = s.TripMeterKm, s.ConsumptionKWhPer100km
	}
	return t
}
