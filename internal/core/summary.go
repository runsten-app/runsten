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

	// Cost sums the trips' known costs (TripCosts); its Unknown counts the others, its
	// Entered stays 0.
	Cost CostSum
	// CostPer100km is the cost of the trips whose cost and positive distance are both
	// known, over that distance, in minor units: unknown without such a trip.
	CostPer100km Value[MinorRange]
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
	// unknown when that time is under Params.MinParkedForRate.
	SoCLossPctPerDay Value[float64]
	// PowerW is the mean power lost, in W: the energy of the SoC lost (the loss × the
	// battery capacity of the event before, else after) over the time of the intervals
	// whose loss and capacity are both known. Unknown when that time is under
	// Params.MinParkedForRate.
	PowerW Value[float64]
	// SleepRead is the part of Time the readings told the vehicle's sleep state over
	// (SleepSpans), and Asleep the part of it they told it asleep or out of reach: none
	// for a source that does not tell it.
	SleepRead, Asleep time.Duration
	// AsleepShare is Asleep over SleepRead: unknown without SleepRead.
	AsleepShare Value[float64]
}

// OdometerStats is the odometer at both ends of a period or an interval, as the events
// read it: their trips' odometers at start and end, their charges' at end. It moves only
// while driving, and every trip is an event, a reconstructed one included, so the
// latest reading before an instant holds until the next trip.
type OdometerStats struct {
	// StartKm is the highest reading of the events before the interval, else the lowest
	// of its own; EndKm the highest of those that started before it ends. Unknown
	// without one.
	StartKm, EndKm Value[float64]
	// DistanceKm is EndKm − StartKm: unknown without both.
	DistanceKm Value[float64]
}

// Stats are the aggregates of a period or of one of its intervals.
type Stats struct {
	Trips    TripStats
	Charges  ChargeStats
	Parked   ParkedStats
	Odometer OdometerStats
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

// TemperatureBandC is the width of a band of outside temperature, in °C.
const TemperatureBandC = 5

// MinTemperatureC and MaxTemperatureC bound the outside temperatures a trip is grouped
// by, [min, max): beyond, a reading is taken for an error and the trip is left out, so
// that one wrong value never spreads the bands over thousands of empty ones.
const (
	MinTemperatureC = -60
	MaxTemperatureC = 70
)

// Rate is an average known by bounds: a quantity over the most and the least time the
// events' bounds allow, a speed in km/h or a power in kW. Max is unknown when they allow
// no time at all.
type Rate struct {
	Min float64
	Max Value[float64]
}

// rate is q over the time of span, per hour: unknown when span allows no time, or q is
// negative. A known q of 0 is a rate of 0, not an unknown one.
func rate(q float64, span Span) Value[Rate] {
	if q < 0 || span.Max <= 0 {
		return Value[Rate]{}
	}
	r := Rate{Min: q / span.Max.Hours()}
	if span.Min > 0 {
		r.Max = Value[float64]{V: q / span.Min.Hours(), OK: true}
	}
	return Value[Rate]{V: r, OK: true}
}

// TemperatureBand sums the trips whose outside temperature lies in [MinC, MinC +
// TemperatureBandC).
type TemperatureBand struct {
	MinC            float64
	Count           int
	DistanceKm      float64
	DistanceUnknown int
	// ConsumptionKWhPer100km is an average, as TripStats'.
	ConsumptionKWhPer100km Value[float64]
	// Speed covers the trips whose distance is known, in km/h: unknown without one.
	Speed Value[Rate]
}

// TripsByTemperature spreads the observed trips of a period over bands of outside
// temperature, from the coldest band holding a trip to the warmest, empty ones between
// included. A trip without a temperature (a reconstructed or imported one, one of a
// vehicle that tells none) belongs nowhere: left out, and counted.
type TripsByTemperature struct {
	Bands   []TemperatureBand
	LeftOut int
}

// ParkedBand sums the parked intervals whose outside temperature lies in [MinC, MinC +
// TemperatureBandC), as ParkedStats.
type ParkedBand struct {
	MinC float64
	ParkedStats
}

// ParkedByTemperature spreads the parked intervals of a period over the bands of
// TripsByTemperature. An interval's temperature is the mean of those of the events
// around it (a trip's or a charge's, OutsideTempC): the vehicle is not read while it
// sleeps, and waking it to read one would itself drain it. Without either (a vehicle
// that tells none, reconstructed or imported events), it is left out, and counted.
type ParkedByTemperature struct {
	Bands   []ParkedBand
	LeftOut int
}

// ChargeBandType sums the charges of one type in a band of outside temperature.
type ChargeBandType struct {
	Count int
	// EnergyPowerKWh sums the known integrated power estimates.
	EnergyPowerKWh float64
	// PowerKW is EnergyPowerKWh over the charging time of the same charges, pauses of a
	// resumed charge included: unknown without one.
	PowerKW Value[Rate]
}

// ChargeBand sums the charges whose outside temperature lies in [MinC, MinC +
// TemperatureBandC). Count includes those of unknown type, in neither AC nor DC.
type ChargeBand struct {
	MinC   float64
	Count  int
	AC, DC ChargeBandType
}

// ChargesByTemperature spreads the observed charges of a period over the bands of
// TripsByTemperature, by their OutsideTempC. A charge without one (a reconstructed or
// imported one, one of a vehicle that tells none) is left out, and counted.
type ChargesByTemperature struct {
	Bands   []ChargeBand
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
	Totals             Stats
	Buckets            []BucketStats
	TripsByDistance    TripsByDistance
	TripsByTemperature TripsByTemperature
	// ParkedByTemperature and ChargesByTemperature are in bands of TemperatureBandC as
	// TripsByTemperature, each from its coldest band holding one to its warmest.
	ParkedByTemperature  ParkedByTemperature
	ChargesByTemperature ChargesByTemperature
	ChargesBySoC         ChargesBySoC
	ChargesByPlace       ChargesByPlace
}

// WithoutConsumption is s with every consumption unknown, and every cost per 100 km: a
// plug-in hybrid's trips measure the battery's energy only, and nothing tells which
// kilometres the engine drove, so an energy over the whole distance would pass for a low
// consumption. The trips' costs stay: the electricity they used.
func (s Summary) WithoutConsumption() Summary {
	s.Totals.Trips.ConsumptionKWhPer100km = Value[float64]{}
	s.Totals.Trips.CostPer100km = Value[MinorRange]{}
	s.Buckets = slices.Clone(s.Buckets)
	for i := range s.Buckets {
		s.Buckets[i].Trips.ConsumptionKWhPer100km = Value[float64]{}
		s.Buckets[i].Trips.CostPer100km = Value[MinorRange]{}
	}
	s.TripsByDistance.Bands = slices.Clone(s.TripsByDistance.Bands)
	for i := range s.TripsByDistance.Bands {
		s.TripsByDistance.Bands[i].ConsumptionKWhPer100km = Value[float64]{}
	}
	s.TripsByTemperature.Bands = slices.Clone(s.TripsByTemperature.Bands)
	for i := range s.TripsByTemperature.Bands {
		s.TripsByTemperature.Bands[i].ConsumptionKWhPer100km = Value[float64]{}
	}
	return s
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
// those whose WindowAfter lies in the period count apart; and the costs of trips, in
// their order. places are the account's, in
// a stable order, which group the charges.
//
// at keeps the charges of one place in the charges' sums, the orphaned costs, which are
// at no place, left out; the others still open and close the parked intervals and give
// their odometer, as the trips do.
//
// sleep are the vehicle's sleep states over the parked intervals (SleepSpans), nil for a
// source that does not tell them.
func Summarize(trips []Trip, charges []Charge, costs Costs, places []Place, sleep []SleepSpan, from, to time.Time, loc *time.Location, bucket Bucket, at ChargesAt, p Params) (Summary, error) {
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
	var temp temperatureAcc
	var parkedTemp parkedTempAcc
	var chargeTemp chargeTempAcc
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

	// before is the highest odometer of the events before the period: where it starts.
	var before Value[float64]
	reading := func(t time.Time, kms ...Value[float64]) {
		if t.Before(from) {
			for _, km := range kms {
				before = highest(before, km)
			}
			return
		}
		add(t, func(a *acc) { a.odometer(kms...) })
	}

	evs := make([]event, 0, len(trips)+len(charges))
	for i, t := range trips {
		var cost Value[TripCost]
		if i < len(costs.Trips) {
			cost = costs.Trips[i]
		}
		add(t.Start.After, func(a *acc) { a.trip(t, cost) })
		reading(t.Start.After, t.StartOdometerKm, t.EndOdometerKm)
		if in(t.Start.After, from, to) {
			dist.add(t)
			temp.add(t)
		}
		evs = append(evs, event{t.DetectedAt, t.Start, t.End, t.StartSoC, t.EndSoC, t.Capacity, eventTemp(t.Reconstructed, t.OutsideTempC)})
	}
	for i, c := range charges {
		var cost Value[Cost]
		if i < len(costs.Charges) {
			cost = costs.Charges[i]
		}
		reading(c.Start.After, c.OdometerKm)
		evs = append(evs, event{c.DetectedAt, c.Start, c.End, c.StartSoC, c.EndSoC, c.Capacity, eventTemp(c.Reconstructed, c.OutsideTempC)})
		if !at.Keeps(c, places) {
			continue
		}
		add(c.Start.After, func(a *acc) { a.charge(c, cost) })
		if in(c.Start.After, from, to) {
			bySoC.add(c)
			byPlace.add(c, cost)
			chargeTemp.add(c)
		}
	}
	if at.Group == PlaceGroupAny {
		for _, o := range costs.Orphans {
			add(o.WindowAfter, func(a *acc) {
				a.s.Charges.Orphaned.Count++
				a.s.Charges.Orphaned.AmountMinor += o.AmountMinor
			})
		}
	}
	slices.SortStableFunc(evs, func(a, b event) int {
		return cmp.Or(a.start.After.Compare(b.start.After), a.detectedAt.Compare(b.detectedAt))
	})
	sleep = sortedSpans(sleep)
	for i := 1; i < len(evs); i++ {
		iv := parkedOf(evs[i-1], evs[i], sleep, p.MinChargeSoC)
		add(evs[i].start.After, func(a *acc) { a.parked(iv) })
		if in(evs[i].start.After, from, to) {
			parkedTemp.add(iv)
		}
	}

	total.odometerFrom(before)
	for i, odo := 0, before; i < len(accs); i++ {
		odo = accs[i].odometerFrom(odo)
	}
	s := Summary{
		Totals: total.stats(p.MinParkedForRate), TripsByDistance: dist.result(), TripsByTemperature: temp.result(),
		ParkedByTemperature: parkedTemp.result(p.MinParkedForRate), ChargesByTemperature: chargeTemp.result(),
		ChargesBySoC: bySoC, ChargesByPlace: byPlace.result(),
	}
	if len(bounds) > 0 {
		s.Buckets = make([]BucketStats, len(bounds))
		for i, b := range bounds {
			s.Buckets[i] = BucketStats{Start: b.Start, End: b.End, Stats: accs[i].stats(p.MinParkedForRate)}
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
	d.bands[i].trip(t, Value[TripCost]{})
}

func (d *distanceAcc) result() TripsByDistance {
	out := TripsByDistance{Bands: make([]DistanceBand, len(d.bands)), LeftOut: d.leftOut}
	for i := range d.bands {
		s := d.bands[i].stats(0).Trips
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

// bandOf returns the index of the band of outside temperature holding c: MinC /
// TemperatureBandC. False without c, or out of [MinTemperatureC, MaxTemperatureC).
func bandOf(c Value[float64]) (int, bool) {
	// Written so that NaN, which every comparison fails, is left out too.
	if !c.OK || !(c.V >= MinTemperatureC && c.V < MaxTemperatureC) {
		return 0, false
	}
	return int(math.Floor(c.V / TemperatureBandC)), true
}

// bands accumulates by band of outside temperature, keyed by bandOf.
type bands[T any] map[int]*T

func (b *bands[T]) at(i int) *T {
	if *b == nil {
		*b = map[int]*T{}
	}
	t := (*b)[i]
	if t == nil {
		t = new(T)
		(*b)[i] = t
	}
	return t
}

// each calls f for each band from the coldest holding one to the warmest, with nil for
// an empty one between.
func (b bands[T]) each(f func(minC float64, t *T)) {
	if len(b) == 0 {
		return
	}
	lo, hi := math.MaxInt, math.MinInt
	for i := range b {
		lo, hi = min(lo, i), max(hi, i)
	}
	for i := lo; i <= hi; i++ {
		f(float64(i*TemperatureBandC), b[i])
	}
}

// temperatureAcc accumulates the trips by outside temperature.
type temperatureAcc struct {
	bands   bands[temperatureBandAcc]
	leftOut int
}

// temperatureBandAcc is a band's trips, as acc for consumption, and the distance and
// driving time of those whose distance is known, for their speed.
type temperatureBandAcc struct {
	acc
	speedKm   float64
	speedTime Span
}

func (a *temperatureAcc) add(t Trip) {
	i, ok := bandOf(t.OutsideTempC)
	if t.Reconstructed || !ok {
		a.leftOut++
		return
	}
	b := a.bands.at(i)
	b.trip(t, Value[TripCost]{})
	if t.DistanceKm.OK {
		b.speedKm += t.DistanceKm.V
		b.speedTime.add(t.Start, t.End)
	}
}

func (a *temperatureAcc) result() TripsByTemperature {
	out := TripsByTemperature{Bands: []TemperatureBand{}, LeftOut: a.leftOut}
	a.bands.each(func(minC float64, b *temperatureBandAcc) {
		band := TemperatureBand{MinC: minC}
		if b != nil {
			s := b.stats(0).Trips
			band.Count, band.DistanceKm, band.DistanceUnknown = s.Count, s.DistanceKm, s.DistanceUnknown
			band.ConsumptionKWhPer100km = s.ConsumptionKWhPer100km
			band.Speed = rate(b.speedKm, b.speedTime)
		}
		out.Bands = append(out.Bands, band)
	})
	return out
}

// parkedTempAcc accumulates the parked intervals by outside temperature, each band as acc.
type parkedTempAcc struct {
	bands   bands[acc]
	leftOut int
}

func (a *parkedTempAcc) add(iv parkedInterval) {
	i, ok := bandOf(iv.tempC)
	if !ok {
		a.leftOut++
		return
	}
	a.bands.at(i).parked(iv)
}

func (a *parkedTempAcc) result(minRate time.Duration) ParkedByTemperature {
	out := ParkedByTemperature{Bands: []ParkedBand{}, LeftOut: a.leftOut}
	a.bands.each(func(minC float64, b *acc) {
		band := ParkedBand{MinC: minC}
		if b != nil {
			band.ParkedStats = b.stats(minRate).Parked
		}
		out.Bands = append(out.Bands, band)
	})
	return out
}

// chargeTempAcc accumulates the observed charges by outside temperature.
type chargeTempAcc struct {
	bands   bands[chargeBandAcc]
	leftOut int
}

// chargeBandAcc is a band's charges, and by type the charging time of those whose
// integrated energy is known, for their power.
type chargeBandAcc struct {
	count  int
	ac, dc chargeTypeAcc
}

type chargeTypeAcc struct {
	count  int
	energy float64
	time   Span
}

func (a *chargeTempAcc) add(c Charge) {
	i, ok := bandOf(c.OutsideTempC)
	if c.Reconstructed || !ok {
		a.leftOut++
		return
	}
	b := a.bands.at(i)
	b.count++
	if !c.Type.OK {
		return
	}
	t := &b.ac
	if c.Type.V == DC {
		t = &b.dc
	}
	t.count++
	if c.EnergyPowerKWh.OK {
		t.energy += c.EnergyPowerKWh.V
		t.time.add(c.Start, c.End)
	}
}

func (a *chargeTempAcc) result() ChargesByTemperature {
	out := ChargesByTemperature{Bands: []ChargeBand{}, LeftOut: a.leftOut}
	a.bands.each(func(minC float64, b *chargeBandAcc) {
		band := ChargeBand{MinC: minC}
		if b != nil {
			band.Count, band.AC, band.DC = b.count, b.ac.result(), b.dc.result()
		}
		out.Bands = append(out.Bands, band)
	})
	return out
}

func (t chargeTypeAcc) result() ChargeBandType {
	return ChargeBandType{Count: t.count, EnergyPowerKWh: t.energy, PowerKW: rate(t.energy, t.time)}
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
	capacity         Value[Capacity]
	tempC            Value[float64]
}

// eventTemp is the outside temperature an event gives the parked intervals around it: a
// reconstructed one's would be a reading of some other time.
func eventTemp(reconstructed bool, c Value[float64]) Value[float64] {
	if reconstructed {
		return Value[float64]{}
	}
	return c
}

// parkedInterval is what the sums need of the interval between two consecutive events.
type parkedInterval struct {
	// time is its sure part, from the latest end of the first event to the earliest start
	// of the next.
	time time.Duration
	// lossPct is the SoC lost: unknown without a SoC at both ends, or a rise that only a
	// charge explains. lossKWh is its energy, unknown without a capacity either.
	lossPct, lossKWh Value[float64]
	// sleepRead and asleep are the parts of time the vehicle's sleep state was read, and
	// read asleep or out of reach.
	sleepRead, asleep time.Duration
	// tempC is the mean of the events' outside temperatures, those known.
	tempC Value[float64]
}

// parkedOf is the interval between prev and next. A rise of the SoC below minRise is
// noise, counted as no loss.
func parkedOf(prev, next event, sleep []SleepSpan, minRise float64) parkedInterval {
	from := prev.end.Before
	iv := parkedInterval{time: max(0, next.start.After.Sub(from))}
	if loss := prev.endSoC.V - next.startSoC.V; prev.endSoC.OK && next.startSoC.OK && loss > -minRise {
		iv.lossPct = Value[float64]{V: max(0, loss), OK: true}
		c := prev.capacity
		if !c.OK {
			c = next.capacity
		}
		if c.OK {
			iv.lossKWh = Value[float64]{V: iv.lossPct.V * c.V.KWh / 100, OK: true}
		}
	}
	iv.sleepRead, iv.asleep = sleepWithin(sleep, from, from.Add(iv.time))
	var sum float64
	n := 0
	for _, t := range []Value[float64]{prev.tempC, next.tempC} {
		if t.OK {
			sum += t.V
			n++
		}
	}
	if n > 0 {
		iv.tempC = Value[float64]{V: sum / float64(n), OK: true}
	}
	return iv
}

// acc accumulates the events of a period or an interval.
type acc struct {
	s Stats
	// Consumption: the energy and distance of the trips where both are known.
	consKWh, consKm float64
	// Cost per 100 km: the costs and distance of the trips where both are known.
	costMin, costMax, costKm float64
	// Parked time of the intervals with a known loss, and of those whose loss's energy is
	// known too, with that energy.
	lossTime, powerTime time.Duration
	lossKWh             float64
	// The lowest and highest odometers of the events.
	odoLow, odoHigh Value[float64]
}

func (a *acc) trip(t Trip, cost Value[TripCost]) {
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
	if !cost.OK {
		s.Cost.Unknown++
		return
	}
	s.Cost.Min += cost.V.Min
	s.Cost.Max += cost.V.Max
	if t.DistanceKm.OK && t.DistanceKm.V > 0 {
		a.costMin += float64(cost.V.Min)
		a.costMax += float64(cost.V.Max)
		a.costKm += t.DistanceKm.V
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

// parked adds the interval between two consecutive events.
func (a *acc) parked(iv parkedInterval) {
	s := &a.s.Parked
	s.Intervals++
	s.Time += iv.time
	s.SleepRead += iv.sleepRead
	s.Asleep += iv.asleep
	if !iv.lossPct.OK {
		s.SoCLossUnknown++
		return
	}
	s.SoCLossPct += iv.lossPct.V
	a.lossTime += iv.time
	if iv.lossKWh.OK {
		a.lossKWh += iv.lossKWh.V
		a.powerTime += iv.time
	}
}

func (a *acc) odometer(kms ...Value[float64]) {
	for _, km := range kms {
		if km.OK && (!a.odoLow.OK || km.V < a.odoLow.V) {
			a.odoLow = km
		}
		a.odoHigh = highest(a.odoHigh, km)
	}
}

// odometerFrom sets the odometer at both ends of the interval from start, the one when
// it starts as the events before it read it, and returns the one when it ends.
func (a *acc) odometerFrom(start Value[float64]) Value[float64] {
	o := OdometerStats{StartKm: start, EndKm: highest(start, a.odoHigh)}
	if !o.StartKm.OK {
		o.StartKm = a.odoLow
	}
	if o.StartKm.OK && o.EndKm.OK {
		o.DistanceKm = Value[float64]{V: o.EndKm.V - o.StartKm.V, OK: true}
	}
	a.s.Odometer = o
	return o.EndKm
}

// highest is the higher of two values, either one when the other is unknown.
func highest(a, b Value[float64]) Value[float64] {
	if !a.OK || (b.OK && b.V > a.V) {
		return b
	}
	return a
}

// stats are the sums of a; the rates of the loss while parked need minRate of parked
// time.
func (a *acc) stats(minRate time.Duration) Stats {
	s := a.s
	if a.consKm > 0 {
		s.Trips.ConsumptionKWhPer100km = Value[float64]{V: a.consKWh * 100 / a.consKm, OK: true}
	}
	if a.costKm > 0 {
		var r MinorRange
		r.Min, r.Max = round(a.costMin*100/a.costKm, a.costMax*100/a.costKm, 0)
		s.Trips.CostPer100km = known(r)
	}
	if a.lossTime > 0 && a.lossTime >= minRate {
		s.Parked.SoCLossPctPerDay = Value[float64]{V: s.Parked.SoCLossPct / a.lossTime.Hours() * 24, OK: true}
	}
	if a.powerTime > 0 && a.powerTime >= minRate {
		s.Parked.PowerW = Value[float64]{V: a.lossKWh * 1000 / a.powerTime.Hours(), OK: true}
	}
	if s.Parked.SleepRead > 0 {
		s.Parked.AsleepShare = Value[float64]{V: s.Parked.Asleep.Seconds() / s.Parked.SleepRead.Seconds(), OK: true}
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
