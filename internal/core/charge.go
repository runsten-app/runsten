package core

import (
	"slices"
	"time"
)

type chargeBuilder struct {
	c Charge

	lastCharging time.Time   // last reading of an active charge
	endLimit     time.Time   // End.Before from polling alone
	hints        []time.Time // vehicle timestamps that may narrow End.Before

	// Power integration, by trapezoids between consecutive readings of an active charge.
	power     Value[float64]
	powerAt   time.Time
	wh        float64
	intervals int

	// The span kept for a battery capacity estimate: the one of the runs of integrated
	// readings with the largest SoC change so far, and the run in progress.
	span Value[PowerSpan]
	run  spanRun

	// The vehicle's energy counter (EnergyAddedKWh): its latest value, and what it counted
	// since the charge started.
	counter  Value[float64]
	measured float64
	counted  bool
}

// sessionBase is the vehicle's energy counter as a charge starts: zero after a reading
// of the cable disconnected, plugging in starting a new session; else the latest count,
// a session that may go on.
func sessionBase(s Snapshot) Value[float64] {
	if s.Connection.OK && s.Connection.V == Disconnected {
		return Value[float64]{V: 0, OK: true}
	}
	return s.EnergyAddedKWh
}

// count follows the vehicle's energy counter to a reading of the charge. A counter
// lower than the previous one started a new session from zero: all of it was counted
// since. Assumption: the counter starts again when the cable is plugged in; where the
// plug-in was not seen, the collector reads the charge within a minute of its start,
// before the new count has overtaken the previous session's. Without a value before the
// charge, its first is the base: the energy counted before it is lost, not guessed.
func (cb *chargeBuilder) count(e Value[float64]) {
	if !e.OK {
		return
	}
	if cb.counter.OK {
		if e.V >= cb.counter.V {
			cb.measured += e.V - cb.counter.V
		} else {
			cb.measured += e.V
		}
		cb.counted = true
	}
	cb.counter = e
}

// spanRun follows one run of the power integration: consecutive readings that all
// report a power. A pause, or a reading without one, ends the run. The first reading
// whose SoC is unknown or exceeds MaxSpanSoC stops extending it, for good: past that
// point the power no longer raises the SoC, and a later reading would add a SoC change
// without the energy of the trapezoids left out.
type spanRun struct {
	open     bool
	stopped  bool
	startSoC Value[float64] // SoC of the run's first reading
	startAt  time.Time      // when that reading was taken
	endSoC   Value[float64] // SoC of the last reading that extended it
	endAt    time.Time      // when that reading was taken
	wh       float64        // energy of the trapezoids those readings closed
	maxGap   time.Duration  // widest of those trapezoids
}

// start opens a run at a reading that reports a power.
func (r *spanRun) start(s Snapshot, at time.Time) {
	*r = spanRun{open: true, startSoC: s.SoC, startAt: at}
}

// keep adds to the run the trapezoid a reading just closed, until a reading's SoC is
// unknown or exceeds maxSoC.
func (r *spanRun) keep(s Snapshot, at time.Time, wh float64, gap time.Duration, maxSoC float64) {
	if r.stopped || !s.SoC.OK || s.SoC.V > maxSoC {
		r.stopped = true
		return
	}
	r.wh += wh
	r.maxGap = max(r.maxGap, gap)
	r.endSoC, r.endAt = s.SoC, at
}

// span returns the run's span, none until a reading has extended it: a span holds only
// between two integrated readings.
func (r *spanRun) span() Value[PowerSpan] {
	if !r.open || !r.startSoC.OK || !r.endSoC.OK {
		return Value[PowerSpan]{}
	}
	return Value[PowerSpan]{V: PowerSpan{
		StartSoC: r.startSoC.V, EndSoC: r.endSoC.V, EnergyKWh: r.wh / 1000, MaxGap: r.maxGap,
		Duration: r.endAt.Sub(r.startAt),
	}, OK: true}
}

// closeSpan ends the run in progress and keeps its span when its SoC change is the
// largest: the estimate rests on the widest change. Ties keep the first.
func (cb *chargeBuilder) closeSpan() {
	if s := cb.run.span(); s.OK && (!cb.span.OK || s.V.EndSoC-s.V.StartSoC > cb.span.V.EndSoC-cb.span.V.StartSoC) {
		cb.span = s
	}
	cb.run = spanRun{}
}

// stepCharge runs the charge machine on the energy readings.
//
// Start: the status becomes charging, or the SoC rose by MinChargeSoC while parked (a
// reconstructed charge, open while the SoC keeps rising). End: the status becomes done
// or idle, the cable is disconnected, or a trip starts. Other statuses (scheduled,
// error…) pause the charge without ending it: assumption, their meaning during a
// charge is not documented.
//
// A charge ended by done or idle with the cable connected may resume: charging again
// before the cable is disconnected, a trip, a reconstructed charge or MaxChargePause
// goes on with it rather than starting another. A solar charge stops and starts again
// with the sun, the cable connected all along.
func (d *detector) stepCharge(p point, prev state) {
	if p.snap.Covers&(FieldSoC|FieldCharging|FieldConnection) == 0 {
		return
	}
	s := d.st.snap
	active := s.Charging.OK && s.Charging.V == ChargingActive
	cb := d.charge
	switch {
	case cb == nil:
		if active && d.resumable != nil {
			d.resumeCharge(p)
		} else if active {
			d.startCharge(p, prev)
		} else if rise := diff(s.SoC, prev.snap.SoC); d.trip == nil && !p.confirm && rise.OK && rise.V >= d.p.MinChargeSoC && d.onPlug(p) {
			d.reconstructCharge(p, prev)
		}
	case active:
		d.charging(cb, p)
	case cb.c.Reconstructed:
		if !p.confirm && s.SoC.OK && s.SoC.V > cb.c.EndSoC.V {
			cb.c.endAt(s)
			cb.endLimit, cb.hints = p.at, []time.Time{s.SoC.At}
			cb.c.Capacity = d.capacity()
			cb.c.OdometerKm = s.OdometerKm
			return
		}
		d.finishCharge()
	case s.Charging.OK && (s.Charging.V == ChargingDone || s.Charging.V == ChargingIdle):
		d.endCharge(cb, p, prev, s.Charging.At)
		if s.Connection.OK && s.Connection.V == Connected && d.p.MaxChargePause > 0 {
			d.resumable = cb
		}
	case s.Connection.OK && s.Connection.V == Disconnected:
		d.endCharge(cb, p, prev, s.Connection.At)
	default:
		cb.power = Value[float64]{} // paused: no integration across the pause
	}
}

func (d *detector) startCharge(p point, prev state) {
	d.releaseCharge()
	d.resumable = nil
	s := d.st.snap
	after := d.lastNotCharging
	if after.IsZero() {
		after = p.at
	}
	cb := &chargeBuilder{c: Charge{
		DetectedAt: p.at,
		Start:      Bounds{After: after, Before: d.narrow(p.at, after, s.Charging.At)},
	}, counter: sessionBase(prev.snap)}
	cb.c.startAt(prev.snap)
	if !cb.c.StartSoC.OK {
		cb.c.startAt(s)
	}
	d.charge = cb
	d.charging(cb, p)
}

// onPlug is whether a rise of the SoC read at p may be a charge. A plug-in hybrid's
// engine raises it too, while driving (a charge mode, or charge sustain), on a trip
// perhaps never seen between two readings: its rise is a charge only when the reading
// that shows it says the cable is connected. The odometer would tell whether it drove,
// but parked it is read once an hour. A charge made while the vehicle was out of reach
// and unplugged before it came back is lost: better than a charge made up, which costs.
func (d *detector) onPlug(p point) bool {
	if !d.st.snap.PlugInHybrid() {
		return true
	}
	c := p.snap.Connection
	return p.snap.Covers&FieldConnection != 0 && c.OK && c.V == Connected
}

// reconstructCharge opens a charge that was not seen: the SoC rose since its previous
// reading.
func (d *detector) reconstructCharge(p point, prev state) {
	d.releaseCharge()
	d.resumable = nil
	s := d.st.snap
	after := prev.seenAt(FieldSoC)
	d.charge = &chargeBuilder{
		c: Charge{
			DetectedAt:    p.at,
			Reconstructed: true,
			Start:         Bounds{After: after},
			End:           Bounds{After: after},
			TargetSoC:     s.TargetSoC,
			Capacity:      d.capacity(),
			OdometerKm:    s.OdometerKm,
		},
		endLimit: p.at,
		hints:    []time.Time{s.SoC.At},
	}
	d.charge.c.startAt(prev.snap)
	d.charge.c.endAt(s)
}

// startAt takes the charge's start values from the reading of its StartSoC.
func (c *Charge) startAt(s Snapshot) {
	c.StartSoC, c.StartRangeKm = s.SoC, s.RangeKm
}

// endAt takes the charge's end values from the reading of its EndSoC.
func (c *Charge) endAt(s Snapshot) {
	c.EndSoC, c.EndRangeKm, c.EndUsableSoC = s.SoC, s.RangeKm, s.UsableSoC
}

// resumeCharge goes on with the resumable charge, charging again. The charge it was
// reported as is withdrawn, waiting for its position or already in the result: the
// cursor never settles while a charge may resume, so a derivation that resumes it
// also detected it.
func (d *detector) resumeCharge(p point) {
	cb := d.resumable
	d.resumable = nil
	if e := d.ended; e != nil && e.c.DetectedAt.Equal(cb.c.DetectedAt) {
		d.ended = nil
	} else {
		d.res.Charges = slices.DeleteFunc(d.res.Charges, func(c Charge) bool { return c.DetectedAt.Equal(cb.c.DetectedAt) })
	}
	cb.endLimit, cb.hints, cb.power = time.Time{}, nil, Value[float64]{}
	d.charge = cb
	d.charging(cb, p)
}

// charging records a reading of an active charge. A reconstructed charge seen charging
// becomes an observed one.
func (d *detector) charging(cb *chargeBuilder, p point) {
	s := d.st.snap
	if cb.c.Reconstructed {
		cb.c.Reconstructed = false
		cb.c.Start.Before = cb.c.DetectedAt
		cb.endLimit, cb.hints = time.Time{}, nil
	}
	cb.lastCharging = p.at
	cb.count(s.EnergyAddedKWh)
	if !cb.c.Type.OK && s.ChargeType.OK && s.ChargeType.V != NoCharge {
		cb.c.Type = s.ChargeType
	}
	if s.TargetSoC.OK {
		cb.c.TargetSoC = s.TargetSoC
	}
	if s.PowerW.OK && cb.power.OK {
		gap := p.at.Sub(cb.powerAt)
		wh := (cb.power.V + s.PowerW.V) / 2 * gap.Hours()
		cb.wh += wh
		cb.intervals++
		cb.run.keep(s, p.at, wh, gap, d.p.MaxSpanSoC)
	} else {
		// A pause, or a reading without a power, ends the run of the span in progress;
		// a reading with one starts the next.
		cb.closeSpan()
		if s.PowerW.OK {
			cb.run.start(s, p.at)
		}
	}
	cb.power, cb.powerAt = s.PowerW, p.at
}

func (d *detector) endCharge(cb *chargeBuilder, p point, prev state, hint time.Time) {
	s := d.st.snap
	cb.c.End.After, cb.endLimit, cb.hints = cb.lastCharging, p.at, []time.Time{hint}
	// The reading that ends the charge has the counter's last count.
	cb.count(s.EnergyAddedKWh)
	cb.c.Capacity = d.capacity()
	cb.c.OdometerKm = s.OdometerKm
	cb.c.endAt(s)
	if !s.SoC.OK {
		cb.c.endAt(prev.snap)
	}
	if s.TargetSoC.OK {
		cb.c.TargetSoC = s.TargetSoC
	}
	d.finishCharge()
}

// interruptCharge ends the charge in progress when a trip starts.
func (d *detector) interruptCharge(p point, prev state) {
	cb := d.charge
	switch {
	case cb == nil:
		d.releaseCharge()
		return
	case !cb.c.Reconstructed:
		cb.c.End.After, cb.endLimit, cb.hints = cb.lastCharging, p.at, nil
		cb.c.Capacity = d.capacity()
		cb.c.OdometerKm = d.st.snap.OdometerKm
		cb.c.endAt(prev.snap)
	}
	d.finishCharge()
	d.releaseCharge()
}

// finishCharge ends the charge in progress. It then waits SettleWindow for a position
// read after its end: the vehicle may upload its position late, after the reading that
// ended the charge, and the one it had before may be the previous charge's place
// (unplugged there, driven straight to another charger before the position changed).
func (d *detector) finishCharge() {
	d.releaseCharge()
	cb := d.charge
	d.charge = nil
	d.ended = &endedCharge{c: d.buildCharge(cb), since: cb.endLimit}
}

// endedCharge is a charge over, waiting for its position.
type endedCharge struct {
	c     Charge
	since time.Time       // the reading that ended it
	read  Value[Position] // a position read after it, taken once its readings are all in
	until time.Time       // if set, the latest a position it takes may have been recorded
}

// settleCharge notes the first position read after the ended charge's end, or releases
// it with the one it had when the settle window is over.
func (d *detector) settleCharge(p point) {
	e := d.ended
	if e == nil || !p.at.After(e.since) {
		return
	}
	if !p.at.Before(e.since.Add(d.p.SettleWindow)) {
		d.releaseCharge()
		return
	}
	// A position noted earlier that the charge's bound now refuses (a trip revealed since,
	// at the same time) gives way to the next it takes.
	if pos := d.st.snap.Position; p.snap.Covers&FieldPosition != 0 && pos.OK &&
		(!e.read.OK || !e.takes(e.read)) && e.takes(pos) {
		e.read = pos
	}
}

// takes reports whether the ended charge may take a position: any, unless a trip that
// may have followed it bounds it to those recorded before it ended, and no older than
// the one it has (a stale one would only stand in for the position it already has).
func (e *endedCharge) takes(pos Value[Position]) bool {
	if e.until.IsZero() {
		return true
	}
	// The one it has bounds it from below only when it is itself within the bound.
	had := e.c.Position
	fresh := !had.OK || had.At.IsZero() || had.At.After(e.until) || !pos.At.Before(had.At)
	return !pos.At.IsZero() && !pos.At.After(e.until) && fresh
}

// placeCharge gives the ended charge the position read after its end, once every
// reading of that time is in: a trip or a charge starting at the same time releases it
// first, with the position it had, since the vehicle may have read that one elsewhere.
func (d *detector) placeCharge() {
	e := d.ended
	if e == nil || !e.read.OK {
		return
	}
	// Checked again: a trip of the same time may have bounded it after the position was
	// noted (one reading that tells both).
	if e.takes(e.read) {
		e.c.Position = e.read
		d.releaseCharge()
		return
	}
	e.read = Value[Position]{}
}

// releaseCharge adds the ended charge to the result as it stands: a trip or a charge
// starting, the vehicle may no longer be where it charged.
func (d *detector) releaseCharge() {
	if d.ended != nil {
		d.res.Charges = append(d.res.Charges, d.ended.c)
		d.ended = nil
	}
}

func (d *detector) buildCharge(cb *chargeBuilder) Charge {
	c, s := cb.c, d.st.snap
	cb.closeSpan()
	c.Span = cb.span
	c.End.Before = d.narrow(cb.endLimit, c.End.After, cb.hints...)
	if c.Reconstructed {
		c.Start.Before = c.End.Before
	}
	cb.energies(&c)
	c.Position = s.Position
	return c
}

// energies sets the charge's energies from what the builder followed so far.
func (cb *chargeBuilder) energies(c *Charge) {
	c.EnergySoCKWh = energy(c.EndSoC, c.StartSoC, c.Capacity)
	if cb.intervals > 0 {
		c.EnergyPowerKWh = Value[float64]{V: cb.wh / 1000, OK: true}
	}
	// A counter that never rose measured nothing: stuck, or not counting this charge.
	if cb.counted && cb.measured > 0 {
		c.EnergyMeasuredKWh = Value[float64]{V: cb.measured, OK: true}
	}
}

// ChargeInProgress is the charge still open at the latest reading: what is known of it
// so far, read as its end would be at that reading.
type ChargeInProgress struct {
	// Charge has the start, the type, the target and the sources the derived charge
	// will have, under the same DetectedAt. Its end is the latest reading: End.After the
	// latest one charging, End.Before the latest energy reading, later while the charge
	// is paused (a status neither charging nor over: scheduled, error); EndSoC, the
	// energies, the capacity and the position are those of that reading.
	Charge
	// PowerW is the charging power of the latest reading.
	PowerW Value[float64]
}

// inProgress returns the open charge as it stands, without ending it.
func (d *detector) inProgress(ps []point) ChargeInProgress {
	cb, s := d.charge, d.st.snap
	c := cb.c
	readAt := later(later(d.st.seenAt(FieldSoC), d.st.seenAt(FieldCharging)), d.st.seenAt(FieldConnection))
	c.End = Bounds{After: cb.lastCharging, Before: readAt}
	c.endAt(s)
	if s.TargetSoC.OK {
		c.TargetSoC = s.TargetSoC
	}
	c.Capacity = d.capacity()
	c.OdometerKm = s.OdometerKm
	cb.energies(&c)
	c.Position = s.Position
	c.Sources = sourcesIn(ps, c.Start.Before, readAt)
	return ChargeInProgress{Charge: c, PowerW: s.PowerW}
}
