package core

import (
	"sort"
	"time"
)

// Params tunes the detection. Each parameter isolates a choice that the vendor
// documentation does not settle.
type Params struct {
	// MinTripKm is the odometer increase, between two readings not seen driving, that
	// reveals a reconstructed trip.
	MinTripKm float64
	// MinChargeSoC is the SoC rise, in points, that reveals a reconstructed charge and
	// ends a trip's SoC reading. A smaller rise is taken as noise (rounding,
	// regenerative braking).
	MinChargeSoC float64
	// MaxSpanSoC is the highest SoC a reading of a charge may have to extend the span
	// kept for a battery capacity estimate. Assumption: above it, the power goes into
	// balancing the cells without raising the SoC, so the energy and the SoC change no
	// longer match. Applied where the readings are, at derivation: changing it takes a
	// rebuild to change the stored spans.
	MaxSpanSoC float64
	// SettleWindow is how long after a trip ends the readings that complete it (odometer,
	// arrival position, trip statistics) are still attached to it. The collector takes
	// them right after the transition.
	SettleWindow time.Duration
	// ResumeBeforeArrival: the engine running again after a stop, before any new position
	// was read, continues the same trip (a short stop) instead of starting another.
	ResumeBeforeArrival bool
	// VehicleTimestamps narrows the bounds with the vehicle-side timestamps when they lie
	// within the polling bounds. Assumption: the vehicle stamps a value when it uploads
	// it, so the timestamp of a new value is an upper bound of the change.
	VehicleTimestamps bool
	// Capacity is the battery capacity that turns a change of SoC into energy.
	Capacity CapacitySource
}

// DefaultParams returns the parameters used without explicit configuration.
func DefaultParams() Params {
	return Params{
		MinTripKm:           1,
		MinChargeSoC:        2,
		MaxSpanSoC:          95,
		SettleWindow:        15 * time.Minute,
		ResumeBeforeArrival: true,
		VehicleTimestamps:   true,
		Capacity:            CapacityCatalogNet,
	}
}

// CapacitySource says which battery capacity an energy estimate rests on.
type CapacitySource string

// Capacity sources.
const (
	// CapacityCatalogNet is the net capacity of the vehicle's variant when the catalog
	// knows it, and else the API's. Assumption: the SoC runs from 0 to 100 % over the net
	// capacity; the vendor API reports the gross one (the published values), which would
	// overstate every energy by 4 to 8 %.
	CapacityCatalogNet CapacitySource = "catalog_net"
	// CapacityAPI is the capacity the vendor API reports with the vehicle's details.
	CapacityAPI CapacitySource = "api"
)

// Capacity is the battery capacity an energy estimate rests on.
type Capacity struct {
	KWh    float64
	Source CapacitySource // CapacityCatalogNet or CapacityAPI, where it came from
}

// NetCapacity returns the net capacity, in kWh, of the vehicle whose details a snapshot
// holds, when it is known. It may depend on the snapshot's readings and on what never
// changes for the vehicle (its VIN), nothing else: an incremental derivation and a
// rebuild must give the same energies.
type NetCapacity func(Snapshot) (float64, bool)

// Cursor is where an incremental derivation resumes. After Settled, no event was in
// progress; replaying the records from From (the state is rebuilt, nothing is detected
// up to Settled) gives the same result as a derivation of the whole history.
type Cursor struct {
	Settled time.Time
	From    time.Time
}

// Result is the outcome of a derivation: the events detected after the cursor it
// started from, and the cursor to resume from.
//
// A trip that has ended but may still be completed by upcoming readings is included:
// the next derivation, which starts before it, replaces it. Trips and charges still in
// progress are not.
type Result struct {
	Trips   []Trip
	Charges []Charge
	Cursor  Cursor
}

// Derive detects trips and charges in records, sorted or not.
//
// records must hold every record fetched after from.From, and the latest record of each
// kind fetched at or before it. Only events detected after from.Settled are
// returned; a zero cursor derives the whole history. net gives the vehicle's net
// capacity for CapacityCatalogNet; nil when it is unknown.
func Derive(records []Record, from Cursor, p Params, net NetCapacity) Result {
	d := &detector{p: p, net: net, res: Result{Cursor: from}}
	ps := points(records)
	for i, pt := range ps {
		prev := d.st
		d.st.apply(pt)
		live := pt.at.After(from.Settled)
		if live {
			d.stepTrip(pt, prev)
			d.stepCharge(pt, prev)
		}
		d.track(pt)
		// Never settle on the last readings: a reading stored later may bear their time.
		groupEnd := i < len(ps)-1 && !ps[i+1].at.Equal(pt.at)
		if live && groupEnd && d.trip == nil && d.charge == nil {
			d.res.Cursor = Cursor{Settled: pt.at, From: d.st.rowAt(FieldOdometer)}
		}
	}
	if d.trip != nil && d.trip.phase == tripEnding {
		d.res.Trips = append(d.res.Trips, d.buildTrip(d.trip))
	}
	if d.charge != nil && d.charge.c.Reconstructed {
		d.res.Charges = append(d.res.Charges, d.buildCharge(d.charge))
	}
	return d.res
}

// point is a reading: a record at its FetchedAt, or again, unchanged, at its CheckedAt.
type point struct {
	at      time.Time
	row     time.Time // FetchedAt of the record
	confirm bool
	snap    Snapshot
}

func points(records []Record) []point {
	ps := make([]point, 0, 2*len(records))
	for _, r := range records {
		if r.Snapshot.Covers == 0 {
			continue
		}
		f, c := r.FetchedAt.UTC(), r.CheckedAt.UTC()
		ps = append(ps, point{at: f, row: f, snap: r.Snapshot})
		if c.After(f) {
			ps = append(ps, point{at: c, row: f, confirm: true, snap: r.Snapshot})
		}
	}
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if !a.at.Equal(b.at) {
			return a.at.Before(b.at)
		}
		if ia, ib := a.snap.Covers.index(), b.snap.Covers.index(); ia != ib {
			return ia < ib
		}
		return a.row.Before(b.row)
	})
	return ps
}

// state is the latest known value of every field.
type state struct {
	snap Snapshot
	seen [fieldCount]time.Time // last reading of each field
	row  [fieldCount]time.Time // FetchedAt of the record holding each field
}

func (s *state) apply(p point) {
	s.snap.merge(p.snap)
	for c := p.snap.Covers; c != 0; c &= c - 1 {
		i := c.index()
		s.seen[i], s.row[i] = p.at, p.row
	}
}

func (s *state) seenAt(f Field) time.Time { return s.seen[f.index()] }
func (s *state) rowAt(f Field) time.Time  { return s.row[f.index()] }

// energyRun follows the SoC down to the end of a trip: it keeps the lowest reading
// until the vehicle charges or the SoC clearly rises.
type energyRun struct {
	soc, rng Value[float64]
	frozen   bool
}

func (r *energyRun) reset(s Snapshot) { r.soc, r.rng, r.frozen = s.SoC, s.RangeKm, false }

func (r *energyRun) observe(s Snapshot, minRise float64) {
	if r.frozen || !s.SoC.OK {
		return
	}
	switch {
	case s.Charging.OK && s.Charging.V == ChargingActive:
		r.frozen = true
	case !r.soc.OK || s.SoC.V <= r.soc.V:
		r.soc, r.rng = s.SoC, s.RangeKm
	case s.SoC.V >= r.soc.V+minRise:
		r.frozen = true
	}
}

// detector runs the trip and charge state machines over the readings, in time order.
//
// The trackers (st, lastNotRunning, lastNotCharging, odoMark, idleRun) depend only on
// the readings, never on the machines: this is what lets a derivation resume from a
// cursor by replaying the recent records.
type detector struct {
	p   Params
	net NetCapacity
	st  state
	res Result

	lastNotRunning  time.Time // last reading of a stopped engine
	lastNotCharging time.Time // last reading of a charging status other than charging
	odoMark         state     // state at the last odometer reading
	idleRun         energyRun // SoC since the last odometer reading

	trip   *tripBuilder
	charge *chargeBuilder
}

func (d *detector) track(p point) {
	s, c := d.st.snap, p.snap.Covers
	if c&FieldEngine != 0 && s.Engine.OK && s.Engine.V != EngineRunning {
		d.lastNotRunning = p.at
	}
	if c&FieldCharging != 0 && s.Charging.OK && s.Charging.V != ChargingActive {
		d.lastNotCharging = p.at
	}
	if c&FieldOdometer != 0 {
		d.odoMark = d.st
		d.idleRun.reset(s)
	}
	if c&FieldSoC != 0 {
		d.idleRun.observe(s, d.p.MinChargeSoC)
		if d.trip != nil {
			d.trip.run.observe(s, d.p.MinChargeSoC)
		}
	}
}

// narrow returns the earliest of limit and the vehicle timestamps that lie in
// [floor, limit].
func (d *detector) narrow(limit, floor time.Time, hints ...time.Time) time.Time {
	if !d.p.VehicleTimestamps {
		return limit
	}
	for _, h := range hints {
		if !h.IsZero() && !h.Before(floor) && h.Before(limit) {
			limit = h
		}
	}
	return limit
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func diff(a, b Value[float64]) Value[float64] {
	if !a.OK || !b.OK {
		return Value[float64]{}
	}
	return Value[float64]{V: a.V - b.V, OK: true}
}

// capacity is the battery capacity of the latest readings. The machines take it at the
// reading that ends an event, never when they build it: an event is built at the first
// reading after its settle window, which may be a confirmation that an incremental
// derivation saw earlier than a rebuild (a run of identical responses grows), and a
// change of the details in between would give them different energies.
func (d *detector) capacity() Value[Capacity] {
	s := d.st.snap
	if d.p.Capacity == CapacityCatalogNet && d.net != nil {
		if kwh, ok := d.net(s); ok {
			return Value[Capacity]{V: Capacity{KWh: kwh, Source: CapacityCatalogNet}, OK: true}
		}
	}
	if !s.CapacityKWh.OK {
		return Value[Capacity]{}
	}
	return Value[Capacity]{V: Capacity{KWh: s.CapacityKWh.V, Source: CapacityAPI}, OK: true}
}

// energy estimates the energy of a SoC change, in kWh.
func energy(from, to Value[float64], c Value[Capacity]) Value[float64] {
	d := diff(from, to)
	if !d.OK || !c.OK {
		return Value[float64]{}
	}
	return Value[float64]{V: d.V / 100 * c.V.KWh, OK: true}
}
