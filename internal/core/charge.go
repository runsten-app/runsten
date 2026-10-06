package core

import "time"

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
func (d *detector) stepCharge(p point, prev state) {
	if p.snap.Covers&(FieldSoC|FieldCharging|FieldConnection) == 0 {
		return
	}
	s := d.st.snap
	active := s.Charging.OK && s.Charging.V == ChargingActive
	cb := d.charge
	switch {
	case cb == nil:
		if active {
			d.startCharge(p, prev)
		} else if rise := diff(s.SoC, prev.snap.SoC); d.trip == nil && !p.confirm && rise.OK && rise.V >= d.p.MinChargeSoC {
			d.reconstructCharge(p, prev)
		}
	case active:
		d.charging(cb, p)
	case cb.c.Reconstructed:
		if !p.confirm && s.SoC.OK && s.SoC.V > cb.c.EndSoC.V {
			cb.c.EndSoC, cb.endLimit, cb.hints = s.SoC, p.at, []time.Time{s.SoC.At}
			cb.c.Capacity = d.capacity()
			cb.c.OdometerKm = s.OdometerKm
			return
		}
		d.finishCharge()
	case s.Charging.OK && (s.Charging.V == ChargingDone || s.Charging.V == ChargingIdle):
		d.endCharge(cb, p, prev, s.Charging.At)
	case s.Connection.OK && s.Connection.V == Disconnected:
		d.endCharge(cb, p, prev, s.Connection.At)
	default:
		cb.power = Value[float64]{} // paused: no integration across the pause
	}
}

func (d *detector) startCharge(p point, prev state) {
	s := d.st.snap
	after := d.lastNotCharging
	if after.IsZero() {
		after = p.at
	}
	cb := &chargeBuilder{c: Charge{
		DetectedAt: p.at,
		Start:      Bounds{After: after, Before: d.narrow(p.at, after, s.Charging.At)},
		StartSoC:   prev.snap.SoC,
	}}
	if !cb.c.StartSoC.OK {
		cb.c.StartSoC = s.SoC
	}
	d.charge = cb
	d.charging(cb, p)
}

// reconstructCharge opens a charge that was not seen: the SoC rose since its previous
// reading.
func (d *detector) reconstructCharge(p point, prev state) {
	s := d.st.snap
	after := prev.seenAt(FieldSoC)
	d.charge = &chargeBuilder{
		c: Charge{
			DetectedAt:    p.at,
			Reconstructed: true,
			Start:         Bounds{After: after},
			End:           Bounds{After: after},
			StartSoC:      prev.snap.SoC,
			EndSoC:        s.SoC,
			TargetSoC:     s.TargetSoC,
			Capacity:      d.capacity(),
			OdometerKm:    s.OdometerKm,
		},
		endLimit: p.at,
		hints:    []time.Time{s.SoC.At},
	}
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
	if !cb.c.Type.OK && s.ChargeType.OK {
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
	cb.c.Capacity = d.capacity()
	cb.c.OdometerKm = s.OdometerKm
	cb.c.EndSoC = s.SoC
	if !s.SoC.OK {
		cb.c.EndSoC = prev.snap.SoC
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
		return
	case !cb.c.Reconstructed:
		cb.c.End.After, cb.endLimit, cb.hints = cb.lastCharging, p.at, nil
		cb.c.Capacity = d.capacity()
		cb.c.OdometerKm = d.st.snap.OdometerKm
		cb.c.EndSoC = prev.snap.SoC
	}
	d.finishCharge()
}

func (d *detector) finishCharge() {
	d.res.Charges = append(d.res.Charges, d.buildCharge(d.charge))
	d.charge = nil
}

func (d *detector) buildCharge(cb *chargeBuilder) Charge {
	c, s := cb.c, d.st.snap
	cb.closeSpan()
	c.Span = cb.span
	c.End.Before = d.narrow(cb.endLimit, c.End.After, cb.hints...)
	if c.Reconstructed {
		c.Start.Before = c.End.Before
	}
	c.EnergySoCKWh = energy(c.EndSoC, c.StartSoC, c.Capacity)
	if cb.intervals > 0 {
		c.EnergyPowerKWh = Value[float64]{V: cb.wh / 1000, OK: true}
	}
	c.Position = s.Position
	return c
}
