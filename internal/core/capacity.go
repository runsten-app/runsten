package core

import (
	"cmp"
	"math"
	"slices"
	"sort"
	"time"
)

// PowerSide says which side of the charger the reported charging power is measured on.
type PowerSide string

// Sides of the charging power.
const (
	// PowerBattery is the power that enters the battery: the energy of the integral is
	// what the SoC change stands for. Assumption: this is what the vehicle reports, its
	// charger's output rather than the meter's, until a real reading says otherwise.
	PowerBattery PowerSide = "battery"
	// PowerGrid is the power metered before the charger: the efficiency of the charge's
	// type bridges the energy to the battery.
	PowerGrid PowerSide = "grid"
)

// CapacityParams are the assumptions of the battery capacity estimates. The filters are
// applied in the order of the reasons of CapacityExcluded: a candidate the filters set
// aside is counted once, under the first that takes it.
type CapacityParams struct {
	// CostParams holds the charging efficiencies, shared with the cost of a charge.
	CostParams

	// PowerSide is where the charging power is measured from. It moves the level of
	// every estimate, never their trend: the two sources of one charge differ by it,
	// and a real reading with a receipt tells it apart.
	PowerSide PowerSide
	// MinSpanSoC is the smallest SoC change, in points, an estimate divides by. The
	// SoC is an integer: each end is rounded by half a point, 5 % of the estimate on a
	// 20-point span.
	MinSpanSoC float64
	// MaxPowerGap is the widest interval two of a span's readings may leave between
	// them: past it a trapezoid invents the power curve (a DC charge above all).
	MaxPowerGap time.Duration
	// MinMeanPowerKW is the lowest mean power an estimate rests on. On a domestic
	// socket the heating of the battery and the 12 V system draw energy that raises
	// the SoC less than the estimate would say.
	MinMeanPowerKW float64
	// PlausibleMin and PlausibleMax bound an estimate around the reference capacity:
	// outside lies an outlier (a SoC recalibrated by the BMS, a false reading). No
	// bound when no reference is known.
	PlausibleMin, PlausibleMax float64
	// RangeMinSoC is the lowest trip SoC the displayed range at a full charge is kept
	// from: below it, the integer SoC rounds the ratio too much.
	RangeMinSoC float64
	// RecentCount is how many of the latest estimates the current capacity is the
	// median of, and how many of the first ones the evolution starts from.
	RecentCount int
	// MinEstimates is the fewest retained estimates the current capacity, and its
	// deviation, are shown from.
	MinEstimates int
	// MinTrendSpan is how many calendar months must separate the first and the last
	// estimate before the evolution is shown: a shorter span says more about the noise
	// than about the battery.
	MinTrendSpan int
	// RatedMinDuration is the shortest charge the rated efficiency is measured on: a
	// short one adds too little range for the counters' rounding. TeslaMate's.
	RatedMinDuration time.Duration
	// RatedMinSoC is the lowest usable level, in %, a rated estimate brings the range at
	// the end of a charge back to 100 % from: the level is an integer, rounded by half a
	// point, 1 % of the estimate at 50 %.
	RatedMinSoC float64
}

// DefaultCapacityParams returns the assumptions used without explicit configuration.
func DefaultCapacityParams() CapacityParams {
	return CapacityParams{
		CostParams:     DefaultCostParams(),
		PowerSide:      PowerBattery,
		MinSpanSoC:     20,
		MaxPowerGap:    3 * time.Minute,
		MinMeanPowerKW: 2,
		PlausibleMin:   0.6,
		PlausibleMax:   1.15,
		RangeMinSoC:    30,
		RecentCount:    20,
		MinEstimates:   5,
		MinTrendSpan:   12,

		RatedMinDuration: 10 * time.Minute,
		RatedMinSoC:      50,
	}
}

// typeEfficiency is the charging efficiency of the charge's type: what stands between
// the metered energy and the battery when the power is reported on the grid's side. The
// place's efficiency says what its own charger loses, not what the vehicle reports:
// the default of the type is the honest guess here.
func (cp CapacityParams) typeEfficiency(c Charge) float64 {
	if isDC(c) {
		return cp.EfficiencyDC
	}
	return cp.EfficiencyAC
}

// EstimateSource says what a capacity estimate was measured on. The two estimates of
// one charge are two points, never averaged.
type EstimateSource string

// Estimate sources.
const (
	// EstimatePower is the integral of the charging power over the charge's retained
	// span, over the SoC change of the same readings.
	EstimatePower EstimateSource = "power"
	// EstimateBilled is the energy billed for the charge, entered from its receipt,
	// over the SoC change of the whole charge: the receipt covers it entirely.
	EstimateBilled EstimateSource = "billed"
	// EstimateRated is the rated range at the end of the charge brought back to 100 % of
	// the usable level, times the vehicle's rated efficiency (a Tesla's, as TeslaMate
	// reads it): the rated range is a fixed conversion of the energy the vehicle counts
	// in its battery, so the estimate follows the battery, not the season.
	EstimateRated EstimateSource = "rated"
)

// rank orders the sources within one charge: power, billed, then rated.
func (s EstimateSource) rank() int {
	switch s {
	case EstimateBilled:
		return 1
	case EstimateRated:
		return 2
	default:
		return 0
	}
}

// RatedEfficiency is the energy a vehicle counts per km of its rated range: the median,
// over the charges it is measured on, of the energy the vehicle measured into its
// battery over the rated range the charge added.
type RatedEfficiency struct {
	KWhPerKm float64
	Charges  int
}

// ratedEfficiency measures the rated efficiency on the observed charges of at least
// RatedMinDuration that ended at MaxSpanSoC at most (above, the energy goes into
// balancing the cells), with a measured energy and a rated range that rose. None
// without such a charge.
func (cp CapacityParams) ratedEfficiency(charges []Charge, p Params) Value[RatedEfficiency] {
	var per []float64
	for _, c := range charges {
		added := diff(c.EndRangeKm, c.StartRangeKm)
		if c.Reconstructed || !c.EnergyMeasuredKWh.OK || !added.OK || added.V <= 0 ||
			!c.EndSoC.OK || c.EndSoC.V > p.MaxSpanSoC || c.End.After.Sub(c.Start.Before) < cp.RatedMinDuration {
			continue
		}
		per = append(per, c.EnergyMeasuredKWh.V/added.V)
	}
	if len(per) == 0 {
		return Value[RatedEfficiency]{}
	}
	slices.Sort(per)
	return known(RatedEfficiency{KWhPerKm: quantile(per, 0.5), Charges: len(per)})
}

// BilledEnergy is what the billed estimate of a charge rests on: the energy billed for
// it, entered from its receipt, and the efficiency that turns it into what reached the
// battery — the one a tariff's cost would apply to that charge, the place's or the
// default of its type. The caller attaches the entered costs to the charges and hands
// each charge its value, as it hands Summarize its costs: the entered costs and the
// places are not derived, and the trend stays a pure function of what it is given.
type BilledEnergy struct {
	EnergyKWh  float64
	Efficiency float64
}

// CapacityEstimate is one estimate of the battery capacity, from one charge: the
// energy it took over the SoC change it raised.
type CapacityEstimate struct {
	// Charge identifies the charge the estimate comes from: its detection time.
	Charge time.Time
	// At is when the charge ended at the latest: the date the estimate is plotted at.
	At time.Time
	// OdometerKm is the charge's: the estimate is plotted against it too. Absent when
	// no reading of it ended the charge.
	OdometerKm Value[float64]
	Source     EstimateSource
	Type       Value[ChargeType]
	// SpanSoC is the SoC the estimate divides by, in points: the change over the span
	// for the power source, over the whole charge for the billed one; the usable level
	// at the end of the charge for the rated one.
	SpanSoC float64
	// CapacityKWh is the estimate itself.
	CapacityKWh float64
}

// CapacityQuartiles are the median and the first and third quartiles of a set of
// capacity estimates, in kWh, with how many estimates they summarize: the middle half
// is the uncertainty of the median.
type CapacityQuartiles struct {
	Median, Q1, Q3 float64
	Count          int
}

// RangeMedian is the median of a set of displayed ranges at a full charge, in km, with
// how many trips it summarizes.
type RangeMedian struct {
	MedianKm float64
	Readings int
}

// CapacityChange is the evolution of the estimated capacity over the estimates the
// trend holds: a ratio of two medians of the same method, in which a constant bias (a
// charging efficiency, the side of the power) cancels out.
type CapacityChange struct {
	// Since is the date of the first estimate: the evolution is measured from the data
	// at hand, not from when the vehicle was new.
	Since time.Time
	// InitialKWh is the median of the first RecentCount estimates.
	InitialKWh float64
	// ChangePct is the current capacity over the initial one, minus one, in whole
	// percent.
	ChangePct int
}

// CapacityExcluded counts the candidates each filter set aside, by reason, in the order
// the filters are applied. A charge is a power candidate, and a billed one on top when
// an entered cost of its has a billed energy; each counts once, under the first filter
// that takes it.
type CapacityExcluded struct {
	Reconstructed int // the charge was not seen: neither its power nor its receipt is trustworthy
	NoPower       int // the vehicle reports no charging power
	HighSoC       int // above MaxSpanSoC the power goes into balancing the cells without raising the SoC
	SpanSoC       int // the SoC change is too small: the integer SoC rounds the estimate too much
	PowerGap      int // two of the span's readings are too far apart
	LowPower      int // a slow charge: its heating and 12 V system weigh in the energy
	Implausible   int // far from the reference capacity: a false reading or a recalibration
	// A rated candidate, a charge whose end tells the rated range and the usable level,
	// set aside before any of the above but Reconstructed:
	NoEfficiency int // no charge measures the rated efficiency yet
	LowSoC       int // the usable level is below RatedMinSoC: its rounding weighs too much
}

// CapacityMonth is one calendar month of the trend: the capacity estimates that ended
// in it, the rated ones alone when the trend is Rated, and the displayed range at a full charge of the trips that started in it. A
// month without a value is present with none — a series shows a gap, never a zero —
// and an absent Capacity reads as a count of 0.
type CapacityMonth struct {
	Start, End time.Time
	Capacity   Value[CapacityQuartiles]
	Range      Value[RangeMedian]
}

// Trend is what the battery page shows of a vehicle, computed from its whole history
// on each read: nothing of it is stored, and no parameter of CapacityParams takes a
// rebuild to change what it shows.
type Trend struct {
	// Rated tells that Current, Change and the months' capacities rest on the rated
	// estimates alone: once there are MinEstimates of them, the vehicle's own count is
	// more precise than the others, which stay in Estimates. Else every source together.
	Rated bool
	// Efficiency is the vehicle's rated efficiency, the rated estimates rest on. Absent
	// for a vehicle without a rated range or a measured energy, and until a charge
	// measures it.
	Efficiency Value[RatedEfficiency]
	// Reference is the capacity the estimates are compared with and bounded by: the
	// net capacity of the vehicle's variant, else the one the vendor API reports.
	// Absent when neither is known.
	Reference Value[Capacity]
	// Current is the median of the latest RecentCount estimates, all sources together or
	// the rated ones (Rated), with their quartiles. Absent under MinEstimates estimates: not enough points to
	// say anything yet, the series still shows them.
	Current Value[CapacityQuartiles]
	// DeviationPct is the current capacity over the reference, minus one, in whole
	// percent: signed and never clamped, so that the bias of the method shows as what
	// it is instead of passing for a new battery. Absent when either capacity is.
	DeviationPct Value[int]
	// Change is the evolution of the estimate. Absent while the history is too short
	// to hold it.
	Change Value[CapacityChange]
	// Cycles is the energy the charges put through the battery over the reference
	// capacity: an estimate too. Absent without a reference.
	Cycles Value[float64]
	// Estimates are the retained estimates, sorted by date, then charge, then source
	// (power before billed): a total, deterministic order.
	Estimates []CapacityEstimate
	// Excluded counts the candidates the filters set aside, by reason, all reasons
	// present: the reader sees why there are few points.
	Excluded CapacityExcluded
	// Months are the calendar months of the history, in the trend's time zone, empty
	// ones included.
	Months []CapacityMonth
}

// CapacityTrend computes what the battery page shows of a vehicle, from the whole
// history of its charges and trips: one capacity estimate per source and charge, the
// current capacity and its change, the monthly medians, the displayed range at a full
// charge, and the equivalent full cycles.
//
// Every charge is a power candidate, a billed one on top when billed holds the energy
// entered for it, in the same order as charges, and a rated one when its end tells the
// rated range and the usable level. trips feeds the displayed range
// at a full charge. reference bounds the estimates and anchors the deviation and the
// cycles, as the capacity of the derivation is handed to core.Derive: without it there
// is no bound, no deviation and no cycles. loc splits the months; UTC when nil.
func CapacityTrend(charges []Charge, billed []Value[BilledEnergy], trips []Trip, reference Value[Capacity], p Params, cp CapacityParams, loc *time.Location) (Trend, error) {
	if loc == nil {
		loc = time.UTC
	}
	var excluded CapacityExcluded
	est := make([]CapacityEstimate, 0, len(charges))
	// keep adds a plausible estimate to the series, and counts the implausible ones.
	keep := func(e CapacityEstimate, kwh float64) {
		if cp.plausible(kwh, reference) {
			e.CapacityKWh = kwh
			est = append(est, e)
		} else {
			excluded.Implausible++
		}
	}
	efficiency := cp.ratedEfficiency(charges, p)
	for i, c := range charges {
		head := CapacityEstimate{Charge: c.DetectedAt, At: c.End.Before, OdometerKm: c.OdometerKm, Type: c.Type}
		e := head
		e.Source = EstimatePower
		switch {
		case c.Reconstructed:
			excluded.Reconstructed++
		case !c.EnergyPowerKWh.OK:
			excluded.NoPower++
		case !c.Span.OK:
			// A power was read but no span was kept: the charge never had two
			// consecutive readings under MaxSpanSoC (it started above it, or only one
			// reading reported a power).
			excluded.HighSoC++
		case c.Span.V.EndSoC-c.Span.V.StartSoC < cp.MinSpanSoC:
			excluded.SpanSoC++
		case c.Span.V.MaxGap > cp.MaxPowerGap:
			excluded.PowerGap++
		case c.Span.V.Duration <= 0 || c.Span.V.EnergyKWh/c.Span.V.Duration.Hours() < cp.MinMeanPowerKW:
			excluded.LowPower++
		default:
			// The energy and the SoC change come from the same readings; the power is
			// the battery's, so nothing stands between them. On the grid's side, the
			// efficiency of the charge's type does.
			k := 1.0
			if cp.PowerSide == PowerGrid {
				k = cp.typeEfficiency(c)
			}
			e.SpanSoC = c.Span.V.EndSoC - c.Span.V.StartSoC
			keep(e, c.Span.V.EnergyKWh*k/e.SpanSoC*100)
		}
		if c.EndRangeKm.OK && c.EndUsableSoC.OK {
			e = head
			e.Source = EstimateRated
			switch u := c.EndUsableSoC.V; {
			case c.Reconstructed:
				excluded.Reconstructed++
			case !efficiency.OK:
				excluded.NoEfficiency++
			case u < cp.RatedMinSoC || u <= 0:
				excluded.LowSoC++
			default:
				e.SpanSoC = u
				keep(e, c.EndRangeKm.V/u*100*efficiency.V.KWhPerKm)
			}
		}
		var b Value[BilledEnergy]
		if i < len(billed) {
			b = billed[i]
		}
		if !b.OK {
			continue
		}
		e = head
		e.Source = EstimateBilled
		switch d := diff(c.EndSoC, c.StartSoC); {
		case c.Reconstructed:
			excluded.Reconstructed++
		case c.EndSoC.OK && c.EndSoC.V > p.MaxSpanSoC:
			// The billed energy includes the balancing above MaxSpanSoC, which no SoC
			// change accounts for.
			excluded.HighSoC++
		case !d.OK || d.V < cp.MinSpanSoC:
			excluded.SpanSoC++
		default:
			// The efficiency is the caller's: the one a tariff's cost would apply to
			// this charge.
			e.SpanSoC = d.V
			keep(e, b.V.EnergyKWh*b.V.Efficiency/d.V*100)
		}
	}

	slices.SortStableFunc(est, func(a, b CapacityEstimate) int {
		return cmp.Or(
			a.At.Compare(b.At),
			a.Charge.Compare(b.Charge),
			cmp.Compare(a.Source.rank(), b.Source.rank()),
		)
	})

	t := Trend{Efficiency: efficiency, Reference: reference, Estimates: est, Excluded: excluded}
	// basis is what the current capacity, its change and the months rest on.
	basis := est
	if rated := slices.DeleteFunc(slices.Clone(est), func(e CapacityEstimate) bool { return e.Source != EstimateRated }); len(rated) >= max(cp.MinEstimates, 1) {
		t.Rated, basis = true, rated
	}
	if cp.RecentCount > 0 && len(basis) >= cp.MinEstimates {
		t.Current = quartiles(capacities(basis[max(0, len(basis)-cp.RecentCount):]))
	}
	if t.Current.OK && reference.OK {
		t.DeviationPct = known(percentChange(t.Current.V.Median, reference.V.KWh))
	}
	// The evolution compares the median of the first RecentCount estimates with the
	// current one, the median of the last ones: both windows must lie in the history
	// without overlapping, hence twice RecentCount estimates, and the battery must have
	// had the time to change, hence MinTrendSpan months of it, counted as calendars do
	// (a February or a change of time never shortens the span).
	if t.Current.OK && len(basis) >= 2*cp.RecentCount &&
		!basis[len(basis)-1].At.Before(basis[0].At.AddDate(0, cp.MinTrendSpan, 0)) {
		initial := quartiles(capacities(basis[:cp.RecentCount]))
		t.Change = known(CapacityChange{
			Since: basis[0].At, InitialKWh: initial.V.Median,
			ChangePct: percentChange(t.Current.V.Median, initial.V.Median),
		})
	}
	if reference.OK {
		var kwh float64
		for _, c := range charges {
			if c.EnergySoCKWh.OK {
				kwh += c.EnergySoCKWh.V
			}
		}
		t.Cycles = known(kwh / reference.V.KWh)
	}

	// The months run from the first estimate, or the first trip kept for the range,
	// whichever is earlier, to the last of them: the range series shares the months of
	// the capacity one.
	var first, last time.Time
	if len(est) > 0 {
		first, last = est[0].At, est[len(est)-1].At
	}
	ranged := make([]struct {
		at time.Time
		km float64
	}, 0, len(trips))
	for _, tr := range trips {
		if !tr.StartSoC.OK || !tr.StartRangeKm.OK || tr.StartSoC.V < cp.RangeMinSoC {
			continue
		}
		// The vehicle's own forecast of its consumption, which follows the season and
		// the driving, not the battery: kept apart from the estimates.
		ranged = append(ranged, struct {
			at time.Time
			km float64
		}{tr.Start.After, tr.StartRangeKm.V / tr.StartSoC.V * 100})
		if first.IsZero() || tr.Start.After.Before(first) {
			first = tr.Start.After
		}
		if tr.Start.After.After(last) {
			last = tr.Start.After
		}
	}
	if !first.IsZero() {
		bounds, err := intervals(first, last.Add(time.Nanosecond), loc, BucketMonth)
		if err != nil {
			return Trend{}, err
		}
		caps, rngs := make([][]float64, len(bounds)), make([][]float64, len(bounds))
		monthOf := func(at time.Time) int {
			return sort.Search(len(bounds), func(i int) bool { return bounds[i].End.After(at) })
		}
		for _, e := range basis {
			i := monthOf(e.At)
			caps[i] = append(caps[i], e.CapacityKWh)
		}
		for _, r := range ranged {
			i := monthOf(r.at)
			rngs[i] = append(rngs[i], r.km)
		}
		months := make([]CapacityMonth, len(bounds))
		for i, b := range bounds {
			months[i] = CapacityMonth{Start: b.Start, End: b.End, Capacity: quartiles(caps[i])}
			if len(rngs[i]) > 0 {
				slices.Sort(rngs[i])
				months[i].Range = known(RangeMedian{MedianKm: quantile(rngs[i], 0.5), Readings: len(rngs[i])})
			}
		}
		t.Months = months
	}
	return t, nil
}

// plausible tells whether an estimate lies in the bounds the reference capacity allows:
// an outlier (a SoC recalibrated by the BMS, a false reading) falls outside. Without a
// reference there is no bound.
func (cp CapacityParams) plausible(kwh float64, reference Value[Capacity]) bool {
	if !reference.OK {
		return true
	}
	return kwh >= reference.V.KWh*cp.PlausibleMin && kwh <= reference.V.KWh*cp.PlausibleMax
}

// capacities is the estimate of each of est.
func capacities(est []CapacityEstimate) []float64 {
	out := make([]float64, len(est))
	for i, e := range est {
		out[i] = e.CapacityKWh
	}
	return out
}

// quartiles summarizes estimates by their median and their first and third quartile.
// None without a value.
func quartiles(vals []float64) Value[CapacityQuartiles] {
	if len(vals) == 0 {
		return Value[CapacityQuartiles]{}
	}
	slices.Sort(vals)
	return known(CapacityQuartiles{
		Median: quantile(vals, 0.5), Q1: quantile(vals, 0.25), Q3: quantile(vals, 0.75),
		Count: len(vals),
	})
}

// quantile is the continuous quantile f of a sorted sample, interpolated linearly
// between the two ranks that hold it, as a database's percentile_cont does: the median
// of 1, 2, 3, 4 is 2.5. sorted must be sorted and not empty.
func quantile(sorted []float64, f float64) float64 {
	pos := f * float64(len(sorted)-1)
	lo, hi := int(math.Floor(pos)), int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (pos-float64(lo))*(sorted[hi]-sorted[lo])
}

// percentChange is v over base, minus one, in whole percent: signed and never clamped,
// so that a positive bias of the method shows as one instead of passing for health.
func percentChange(v, base float64) int { return int(math.Round((v/base - 1) * 100)) }
