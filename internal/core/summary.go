package core

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"sort"
	"time"
)

// Bucket is how a period is split into intervals.
type Bucket string

// Buckets. BucketNone gives the totals only.
const (
	BucketNone  Bucket = ""
	BucketDay   Bucket = "day"
	BucketWeek  Bucket = "week" // ISO week, from Monday
	BucketMonth Bucket = "month"
)

// MaxBuckets is the largest number of intervals a summary may have: a year by day.
const MaxBuckets = 400

// Errors of Summarize.
var (
	ErrEmptyPeriod    = errors.New("the period must end after it starts")
	ErrTooManyBuckets = errors.New("too many intervals")
	ErrUnknownBucket  = errors.New("unknown bucket")
)

// Span is a sum of durations known only by bounds: every total the events allow lies in
// [Min, Max].
type Span struct {
	Min, Max time.Duration
}

// TripStats sums trips. A sum covers the known values only, and says how many trips
// lacked one.
type TripStats struct {
	Count, Reconstructed int

	DistanceKm      float64
	DistanceUnknown int

	// DrivingTime covers observed trips: a reconstructed one has no duration, and counts
	// in DrivingTimeUnknown.
	DrivingTime        Span
	DrivingTimeUnknown int

	EnergyKWh     float64
	EnergyUnknown int

	// ConsumptionKWhPer100km is an average: unknown without a trip whose energy and
	// positive distance are both known.
	ConsumptionKWhPer100km Value[float64]
}

// CostSum sums the costs of charges, in minor units: every total their bounds allow lies
// in [Min, Max].
type CostSum struct {
	Min, Max int64
	Unknown  int // charges without a cost, left out of Min and Max
	Entered  int // charges whose cost was entered
}

// OrphanedCosts sums entered costs that no charge takes. They stay out of every CostSum:
// the charge they were entered for, detected again, most often has a cost already.
type OrphanedCosts struct {
	Count       int
	AmountMinor int64
}

// ChargeTypeStats sums the charges of one type.
type ChargeTypeStats struct {
	Count            int
	EnergySoCKWh     float64
	EnergySoCUnknown int
	Cost             CostSum
}

// ChargeStats sums charges. The two energy estimates stay apart, as on each charge.
type ChargeStats struct {
	Count, Reconstructed int

	EnergySoCKWh     float64
	EnergySoCUnknown int

	EnergyPowerKWh     float64
	EnergyPowerUnknown int

	// ChargingTime covers observed charges; reconstructed ones count in
	// ChargingTimeUnknown.
	ChargingTime        Span
	ChargingTimeUnknown int

	Cost CostSum
	// Orphaned counts in the interval that holds its WindowAfter.
	Orphaned OrphanedCosts

	AC, DC, UnknownType ChargeTypeStats
}

// ParkedStats sums the intervals between two consecutive events (phantom drain).
type ParkedStats struct {
	Intervals int
	// Time is the sure part of each interval, from the latest end of an event to the
	// earliest start of the next.
	Time time.Duration

	SoCLossPct     float64
	SoCLossUnknown int // intervals without a SoC at both ends, or with a charge between
	// SoCLossPctPerDay is SoCLossPct over the time of the intervals with a known loss:
	// unknown without such time.
	SoCLossPctPerDay Value[float64]
}

// Stats are the aggregates of a period or of one of its intervals.
type Stats struct {
	Trips   TripStats
	Charges ChargeStats
	Parked  ParkedStats
}

// BucketStats are the aggregates of an interval [Start, End).
type BucketStats struct {
	Start, End time.Time
	Stats
}

// DistanceBand counts the trips whose distance lies in [MinKm, MaxKm); the last band has
// no upper limit (MaxKm unknown).
type DistanceBand struct {
	MinKm float64
	MaxKm Value[float64]
	Count int
	// ConsumptionKWhPer100km is an average, as TripStats': unknown without a trip whose
	// energy and positive distance are both known.
	ConsumptionKWhPer100km Value[float64]
}

// distanceLimits split the trips by distance, in km: across town, a commute, around a
// region, a day out, a journey.
var distanceLimits = []float64{5, 20, 50, 100}

// TripsByDistance spreads the observed trips of a period over bands of distance. A
// reconstructed trip may hold several, and one without a distance belongs nowhere: both
// are left out, and counted.
type TripsByDistance struct {
	Bands   []DistanceBand
	LeftOut int
}

// ChargesBySoC counts the observed charges of a period by the SoC they started and ended
// at, in points rounded to the nearest: Start[20] is the charges that started at 20 %. A
// reconstructed charge's SoC are readings before and after it, not its own; it is left
// out and counted, as one whose SoC is unknown.
type ChargesBySoC struct {
	Start, End [101]int
	LeftOut    int
}

// PlaceStats sums the charges of a place, or of a group outside every place.
type PlaceStats struct {
	Count, Reconstructed int
	EnergySoCKWh         float64
	EnergySoCUnknown     int
	Cost                 CostSum
}

// PlaceCharges are the charges of one place.
type PlaceCharges struct {
	Place Place
	PlaceStats
}

// ChargesByPlace groups the charges of a period by the place their cost takes
// (PlaceOf). Reconstructed charges are counted: their energy adds up whether they hold
// one charge or several, and their place is the one their cost takes, so the groups
// add up to the period's totals.
type ChargesByPlace struct {
	// Places are those holding a charge, in the order given.
	Places []PlaceCharges
	// Outside every place, with a position, by type.
	AC, DC, UnknownType PlaceStats
	// NoPosition are the charges without a position that no place takes: DC ones, or
	// every one without a place marked WithoutPosition.
	NoPosition PlaceStats
}

// Summary holds the totals of a period and, when split, its intervals, empty ones
// included, and how its trips and charges spread.
type Summary struct {
	Totals          Stats
	Buckets         []BucketStats
	TripsByDistance TripsByDistance
	ChargesBySoC    ChargesBySoC
	ChargesByPlace  ChargesByPlace
}

// Summarize aggregates the trips and charges of a vehicle over [from, to), split by
// bucket in loc (UTC if nil).
//
// An event counts in the interval that holds its Start.After: it counts once, whatever
// the split, since its bounds do not tell when it crossed a boundary. trips and charges
// are those starting in [from, to), plus the latest event before from, which opens the
// first parked interval; any other event outside the period is ignored.
//
// costs holds the costs of charges, in the same order, and the orphaned entered costs:
// those whose WindowAfter lies in the period count apart. places are the account's, in
// a stable order, which group the charges.
func Summarize(trips []Trip, charges []Charge, costs Costs, places []Place, from, to time.Time, loc *time.Location, bucket Bucket, p Params) (Summary, error) {
	if !from.Before(to) {
		return Summary{}, ErrEmptyPeriod
	}
	if loc == nil {
		loc = time.UTC
	}
	bounds, err := intervals(from, to, loc, bucket)
	if err != nil {
		return Summary{}, err
	}

	var total acc
	accs := make([]acc, len(bounds))
	dist := newDistanceAcc()
	var bySoC ChargesBySoC
	byPlace := newPlaceAcc(places)
	// add counts an event starting at t in the totals and in its interval.
	add := func(t time.Time, f func(*acc)) {
		if !in(t, from, to) {
			return
		}
		f(&total)
		if len(bounds) > 0 {
			f(&accs[sort.Search(len(bounds), func(i int) bool { return bounds[i].End.After(t) })])
		}
	}

	evs := make([]event, 0, len(trips)+len(charges))
	for _, t := range trips {
		add(t.Start.After, func(a *acc) { a.trip(t) })
		if in(t.Start.After, from, to) {
			dist.add(t)
		}
		evs = append(evs, event{t.DetectedAt, t.Start, t.End, t.StartSoC, t.EndSoC})
	}
	for i, c := range charges {
		var cost Value[Cost]
		if i < len(costs.Charges) {
			cost = costs.Charges[i]
		}
		add(c.Start.After, func(a *acc) { a.charge(c, cost) })
		if in(c.Start.After, from, to) {
			bySoC.add(c)
			byPlace.add(c, cost)
		}
		evs = append(evs, event{c.DetectedAt, c.Start, c.End, c.StartSoC, c.EndSoC})
	}
	for _, o := range costs.Orphans {
		add(o.WindowAfter, func(a *acc) {
			a.s.Charges.Orphaned.Count++
			a.s.Charges.Orphaned.AmountMinor += o.AmountMinor
		})
	}
	slices.SortStableFunc(evs, func(a, b event) int {
		return cmp.Or(a.start.After.Compare(b.start.After), a.detectedAt.Compare(b.detectedAt))
	})
	for i := 1; i < len(evs); i++ {
		prev, next := evs[i-1], evs[i]
		add(next.start.After, func(a *acc) { a.parked(prev, next, p.MinChargeSoC) })
	}

	s := Summary{
		Totals: total.stats(), TripsByDistance: dist.result(), ChargesBySoC: bySoC, ChargesByPlace: byPlace.result(),
	}
	if len(bounds) > 0 {
		s.Buckets = make([]BucketStats, len(bounds))
		for i, b := range bounds {
			s.Buckets[i] = BucketStats{Start: b.Start, End: b.End, Stats: accs[i].stats()}
		}
	}
	return s, nil
}

// in tells whether t lies in [from, to).
func in(t, from, to time.Time) bool { return !t.Before(from) && t.Before(to) }

// distanceAcc accumulates the trips by distance: a band each, as acc for consumption.
type distanceAcc struct {
	bands   []acc
	leftOut int
}

func newDistanceAcc() *distanceAcc {
	return &distanceAcc{bands: make([]acc, len(distanceLimits)+1)}
}

// NoTripsByDistance is how no trip spreads: every band, empty.
func NoTripsByDistance() TripsByDistance { return newDistanceAcc().result() }

func (d *distanceAcc) add(t Trip) {
	if t.Reconstructed || !t.DistanceKm.OK {
		d.leftOut++
		return
	}
	i := sort.Search(len(distanceLimits), func(i int) bool { return t.DistanceKm.V < distanceLimits[i] })
	d.bands[i].trip(t)
}

func (d *distanceAcc) result() TripsByDistance {
	out := TripsByDistance{Bands: make([]DistanceBand, len(d.bands)), LeftOut: d.leftOut}
	for i := range d.bands {
		s := d.bands[i].stats().Trips
		b := DistanceBand{Count: s.Count, ConsumptionKWhPer100km: s.ConsumptionKWhPer100km}
		if i > 0 {
			b.MinKm = distanceLimits[i-1]
		}
		if i < len(distanceLimits) {
			b.MaxKm = Value[float64]{V: distanceLimits[i], OK: true}
		}
		out.Bands[i] = b
	}
	return out
}

func (c *ChargesBySoC) add(ch Charge) {
	if ch.Reconstructed || !ch.StartSoC.OK || !ch.EndSoC.OK {
		c.LeftOut++
		return
	}
	c.Start[soCPoint(ch.StartSoC.V)]++
	c.End[soCPoint(ch.EndSoC.V)]++
}

// soCPoint rounds a SoC to the nearest point, within 0 to 100.
func soCPoint(v float64) int { return min(100, max(0, int(math.Round(v)))) }

// placeAcc accumulates the charges by place: one PlaceStats per place, in its order.
type placeAcc struct {
	places []Place
	stats  []PlaceStats
	out    ChargesByPlace
}

func newPlaceAcc(places []Place) *placeAcc {
	return &placeAcc{places: places, stats: make([]PlaceStats, len(places))}
}

func (a *placeAcc) add(c Charge, cost Value[Cost]) {
	var s *PlaceStats
	i := placeIndex(c, a.places)
	switch {
	case i >= 0:
		s = &a.stats[i]
	case !c.Position.OK:
		s = &a.out.NoPosition
	case c.Type.OK && c.Type.V == AC:
		s = &a.out.AC
	case c.Type.OK && c.Type.V == DC:
		s = &a.out.DC
	default:
		s = &a.out.UnknownType
	}
	s.Count++
	if c.Reconstructed {
		s.Reconstructed++
	}
	sum(&s.EnergySoCKWh, &s.EnergySoCUnknown, c.EnergySoCKWh)
	s.Cost.add(cost)
}

func (a *placeAcc) result() ChargesByPlace {
	out := a.out
	out.Places = []PlaceCharges{}
	for i, s := range a.stats {
		if s.Count > 0 {
			out.Places = append(out.Places, PlaceCharges{Place: a.places[i], PlaceStats: s})
		}
	}
	return out
}

// NoChargesByPlace is how no charge spreads: no place, every group empty.
func NoChargesByPlace() ChargesByPlace { return newPlaceAcc(nil).result() }

// interval is a bucket's limits, in UTC.
type interval struct {
	Start, End time.Time
}

// intervals splits [from, to) in loc: from the start of the day, week or month holding
// from, to the end of the one holding the last instant before to. Limits come from
// calendar arithmetic, never from adding hours: a day lasts 23 or 25 hours across a
// change of daylight saving time.
func intervals(from, to time.Time, loc *time.Location, bucket Bucket) ([]interval, error) {
	local := from.In(loc)
	y, m, d := local.Date()
	var days, months int
	switch bucket {
	case BucketNone:
		return nil, nil
	case BucketDay:
		days = 1
	case BucketWeek:
		d -= (int(local.Weekday()) + 6) % 7 // back to Monday
		days = 7
	case BucketMonth:
		d, months = 1, 1
	default:
		return nil, ErrUnknownBucket
	}
	var out []interval
	start := time.Date(y, m, d, 0, 0, 0, 0, loc)
	for i := 1; start.Before(to); i++ {
		if len(out) == MaxBuckets {
			return nil, ErrTooManyBuckets
		}
		end := time.Date(y, m+time.Month(i*months), d+i*days, 0, 0, 0, 0, loc)
		out = append(out, interval{start.UTC(), end.UTC()})
		start = end
	}
	return out, nil
}

// event is what the parked intervals need of a trip or a charge.
type event struct {
	detectedAt       time.Time
	start, end       Bounds
	startSoC, endSoC Value[float64]
}

// acc accumulates the events of a period or an interval.
type acc struct {
	s Stats
	// Consumption: the energy and distance of the trips where both are known.
	consKWh, consKm float64
	// Parked time of the intervals with a known loss.
	lossTime time.Duration
}

func (a *acc) trip(t Trip) {
	s := &a.s.Trips
	s.Count++
	if t.Reconstructed {
		s.Reconstructed++
		s.DrivingTimeUnknown++
	} else {
		s.DrivingTime.add(t.Start, t.End)
	}
	sum(&s.DistanceKm, &s.DistanceUnknown, t.DistanceKm)
	sum(&s.EnergyKWh, &s.EnergyUnknown, t.EnergyKWh)
	if t.EnergyKWh.OK && t.DistanceKm.OK && t.DistanceKm.V > 0 {
		a.consKWh += t.EnergyKWh.V
		a.consKm += t.DistanceKm.V
	}
}

func (a *acc) charge(c Charge, cost Value[Cost]) {
	s := &a.s.Charges
	s.Count++
	if c.Reconstructed {
		s.Reconstructed++
		s.ChargingTimeUnknown++
	} else {
		s.ChargingTime.add(c.Start, c.End)
	}
	sum(&s.EnergySoCKWh, &s.EnergySoCUnknown, c.EnergySoCKWh)
	sum(&s.EnergyPowerKWh, &s.EnergyPowerUnknown, c.EnergyPowerKWh)
	byType := &s.UnknownType
	if c.Type.OK {
		switch c.Type.V {
		case AC:
			byType = &s.AC
		case DC:
			byType = &s.DC
		}
	}
	byType.Count++
	sum(&byType.EnergySoCKWh, &byType.EnergySoCUnknown, c.EnergySoCKWh)
	s.Cost.add(cost)
	byType.Cost.add(cost)
}

func (s *CostSum) add(c Value[Cost]) {
	switch {
	case !c.OK:
		s.Unknown++
		return
	case c.V.Source == CostEntered:
		s.Entered++
	}
	s.Min += c.V.Min
	s.Max += c.V.Max
}

// parked adds the interval between two consecutive events. A rise of the SoC below
// minRise is noise; a larger one means a charge that no event holds, so the loss is
// unknown.
func (a *acc) parked(prev, next event, minRise float64) {
	s := &a.s.Parked
	s.Intervals++
	d := max(0, next.start.After.Sub(prev.end.Before))
	s.Time += d
	loss := prev.endSoC.V - next.startSoC.V
	if !prev.endSoC.OK || !next.startSoC.OK || loss <= -minRise {
		s.SoCLossUnknown++
		return
	}
	s.SoCLossPct += max(0, loss)
	a.lossTime += d
}

func (a *acc) stats() Stats {
	s := a.s
	if a.consKm > 0 {
		s.Trips.ConsumptionKWhPer100km = Value[float64]{V: a.consKWh * 100 / a.consKm, OK: true}
	}
	if a.lossTime > 0 {
		s.Parked.SoCLossPctPerDay = Value[float64]{V: s.Parked.SoCLossPct / a.lossTime.Hours() * 24, OK: true}
	}
	return s
}

// add sums the duration of an observed event as the front shows it: at least from the
// latest start to the earliest end, at most from the earliest start to the latest end.
func (s *Span) add(start, end Bounds) {
	s.Min += max(0, end.After.Sub(start.Before))
	s.Max += max(0, end.Before.Sub(start.After))
}

// sum adds v to total when known, and counts it in unknown otherwise.
func sum(total *float64, unknown *int, v Value[float64]) {
	if v.OK {
		*total += v.V
	} else {
		*unknown++
	}
}
