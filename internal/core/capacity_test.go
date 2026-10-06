package core

import (
	"math"
	"testing"
	"time"
)

// net64 is the reference of a vehicle whose variant's net capacity is 64 kWh, as the
// catalog states it.
var net64 = known(Capacity{KWh: 64, Source: CapacityCatalogNet})

// api69 is the reference of a vehicle no variant is known for: the capacity the API
// reports, the gross one.
var api69 = known(Capacity{KWh: 69, Source: CapacityAPI})

// charged is an observed charge whose span covers it whole: an hour of readings a
// minute apart, at the latest ending at at. from→to is the SoC change of the span, kwh
// the energy of its integral.
func charged(at time.Time, typ ChargeType, from, to, kwh float64) Charge {
	return Charge{
		DetectedAt: at.Add(-time.Minute),
		Start:      Bounds{After: at.Add(-2 * time.Hour), Before: at.Add(-time.Hour)},
		End:        Bounds{After: at.Add(-10 * time.Minute), Before: at},
		Type:       known(typ), StartSoC: val(from), EndSoC: val(to),
		EnergySoCKWh: val((to - from) / 100 * 64), EnergyPowerKWh: val(kwh),
		Span:       known(PowerSpan{StartSoC: from, EndSoC: to, EnergyKWh: kwh, MaxGap: time.Minute, Duration: time.Hour}),
		OdometerKm: val(12345),
	}
}

// receipt is the energy billed for a charge, entered from its receipt, with the
// efficiency a tariff's cost would apply to it.
func receipt(kwh, eta float64) Value[BilledEnergy] {
	return known(BilledEnergy{EnergyKWh: kwh, Efficiency: eta})
}

// est is the estimate a case expects, off the charge it comes from.
func est(c Charge, source EstimateSource, spanSoC, kwh float64) CapacityEstimate {
	return CapacityEstimate{
		Charge: c.DetectedAt, At: c.End.Before, OdometerKm: c.OdometerKm, Type: c.Type,
		Source: source, SpanSoC: spanSoC, CapacityKWh: kwh,
	}
}

func checkEstimates(t *testing.T, got, want []CapacityEstimate) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d estimates, want %d:\n%+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Charge != w.Charge || g.At != w.At || g.Source != w.Source || g.SpanSoC != w.SpanSoC ||
			g.OdometerKm != w.OdometerKm || g.Type != w.Type || math.Abs(g.CapacityKWh-w.CapacityKWh) > 1e-9 {
			t.Errorf("estimate %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
}

func TestQuantile(t *testing.T) {
	// Hand-computed: the median of 1, 2, 3, 4 is 2.5, and each quartile falls between
	// the two ranks that hold it.
	for _, tt := range []struct {
		name           string
		sample         []float64
		median, q1, q3 float64
	}{
		{"one value", []float64{10}, 10, 10, 10},
		{"two values", []float64{10, 20}, 15, 12.5, 17.5},
		{"three values", []float64{10, 20, 30}, 20, 15, 25},
		{"four values", []float64{10, 20, 30, 40}, 25, 17.5, 32.5},
		{"twenty values", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, 10.5, 5.75, 15.25},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// quartiles sorts its input: the sample of a month or of a window never is.
			reverse := make([]float64, len(tt.sample))
			for i, v := range tt.sample {
				reverse[len(tt.sample)-1-i] = v
			}
			if q := quartiles(reverse); !q.OK || math.Abs(q.V.Median-tt.median) > 1e-9 ||
				math.Abs(q.V.Q1-tt.q1) > 1e-9 || math.Abs(q.V.Q3-tt.q3) > 1e-9 || q.V.Count != len(tt.sample) {
				t.Errorf("quartiles(%v) = %+v, want median %v, q1 %v, q3 %v", reverse, q, tt.median, tt.q1, tt.q3)
			}
			if q := quartiles(nil); q.OK {
				t.Errorf("quartiles of nothing = %+v, want none", q)
			}
		})
	}
}

func TestCapacityEstimates(t *testing.T) {
	at := utc("2026-09-21T21:40:00Z")
	later := at.Add(48 * time.Hour)
	// The charge of the example: a span from 30 to 70 % that integrated 25.6 kWh, an
	// even 64 kWh over 40 points.
	ac := charged(at, AC, 30, 70, 25.6)
	dc := charged(at, DC, 30, 70, 25.6)
	untyped := with(charged(at, AC, 30, 70, 25.6), func(c *Charge) { c.Type = Value[ChargeType]{} })
	for _, tt := range []struct {
		name      string
		charges   []Charge
		billed    []Value[BilledEnergy]
		params    func(*CapacityParams)
		reference Value[Capacity]
		want      []CapacityEstimate
		excluded  CapacityExcluded
	}{
		{
			name: "power, on the battery's side", charges: []Charge{ac}, reference: net64,
			want: []CapacityEstimate{est(ac, EstimatePower, 40, 64)},
		},
		{
			// Both estimates of one charge are two points, never averaged: power first.
			name: "power and billed on one charge", charges: []Charge{ac},
			billed: []Value[BilledEnergy]{receipt(30, 0.88)}, reference: net64,
			want: []CapacityEstimate{est(ac, EstimatePower, 40, 64), est(ac, EstimateBilled, 40, 66)},
		},
		{
			name: "grid side, AC and unknown type", charges: []Charge{ac, untyped}, reference: net64,
			params: func(p *CapacityParams) { p.PowerSide = PowerGrid },
			want:   []CapacityEstimate{est(ac, EstimatePower, 40, 56.32), est(untyped, EstimatePower, 40, 56.32)},
		},
		{
			name: "grid side, DC", charges: []Charge{dc}, reference: net64,
			params: func(p *CapacityParams) { p.PowerSide = PowerGrid },
			want:   []CapacityEstimate{est(dc, EstimatePower, 40, 60.8)},
		},
		{
			// The efficiency of the billed estimate is the caller's: the one a tariff's
			// cost would apply to the charge, the place's here (0.9, not the 0.88 of AC).
			name: "billed, the efficiency of the charge's place", charges: []Charge{ac},
			billed: []Value[BilledEnergy]{receipt(30, 0.9)}, reference: net64,
			want: []CapacityEstimate{est(ac, EstimatePower, 40, 64), est(ac, EstimateBilled, 40, 67.5)},
		},
		{
			// A charge never seen has no span; its billed candidate is set aside for
			// the same reason: each candidate counts once, under the first filter.
			name: "reconstructed charge, both candidates",
			charges: []Charge{with(charged(at, AC, 30, 70, 0), func(c *Charge) {
				c.Reconstructed, c.EnergyPowerKWh, c.Span, c.OdometerKm = true, Value[float64]{}, Value[PowerSpan]{}, Value[float64]{}
			})},
			billed: []Value[BilledEnergy]{receipt(30, 0.88)}, reference: net64,
			excluded: CapacityExcluded{Reconstructed: 2},
		},
		{
			// The vehicle reports no charging power: the billed estimate still stands.
			name: "no power reported",
			charges: []Charge{with(charged(at, AC, 30, 70, 0), func(c *Charge) {
				c.EnergyPowerKWh, c.Span = Value[float64]{}, Value[PowerSpan]{}
			})},
			billed: []Value[BilledEnergy]{receipt(30, 0.88)}, reference: net64,
			want:     []CapacityEstimate{est(ac, EstimateBilled, 40, 66)},
			excluded: CapacityExcluded{NoPower: 1},
		},
		{
			// A power was read but no span was kept: the charge started above
			// MaxSpanSoC, or a single reading reported one.
			name: "power without a span",
			charges: []Charge{with(charged(at, AC, 90, 100, 8), func(c *Charge) {
				c.Span = Value[PowerSpan]{}
			})},
			reference: net64, excluded: CapacityExcluded{HighSoC: 1},
		},
		{
			// The receipt covers the balancing above MaxSpanSoC, which no SoC change
			// accounts for; the power estimate keeps the span, which stops below.
			name: "billed over the balancing, power under it",
			charges: []Charge{with(charged(at, AC, 30, 96, 40.96), func(c *Charge) {
				c.Span = known(PowerSpan{StartSoC: 30, EndSoC: 94, EnergyKWh: 40.96, MaxGap: time.Minute, Duration: time.Hour})
			})},
			billed: []Value[BilledEnergy]{receipt(45, 0.88)}, reference: net64,
			want:     []CapacityEstimate{est(ac, EstimatePower, 64, 64)},
			excluded: CapacityExcluded{HighSoC: 1},
		},
		{
			name: "span too small, power",
			charges: []Charge{with(charged(at, AC, 50, 64, 9), func(c *Charge) {
				c.Span.V.StartSoC, c.Span.V.EndSoC = 50, 64
			})},
			reference: net64, excluded: CapacityExcluded{SpanSoC: 1},
		},
		{
			name: "span too small, billed",
			charges: []Charge{with(charged(at, AC, 50, 64, 9), func(c *Charge) {
				c.Span.V.StartSoC, c.Span.V.EndSoC = 50, 64
			})},
			billed: []Value[BilledEnergy]{receipt(12, 0.88)}, reference: net64,
			excluded: CapacityExcluded{SpanSoC: 2},
		},
		{
			// The SoC at the ends of the whole charge may be unknown even when the
			// span's are not: the billed candidate has nothing to divide by.
			name: "billed without a SoC at both ends",
			charges: []Charge{with(charged(at, AC, 30, 70, 25.6), func(c *Charge) {
				c.StartSoC = Value[float64]{}
			})},
			billed: []Value[BilledEnergy]{receipt(30, 0.88)}, reference: net64,
			want:     []CapacityEstimate{est(ac, EstimatePower, 40, 64)},
			excluded: CapacityExcluded{SpanSoC: 1},
		},
		{
			name: "gap between the span's readings",
			charges: []Charge{with(charged(at, AC, 30, 70, 25.6), func(c *Charge) {
				c.Span.V.MaxGap = 4 * time.Minute
			})},
			reference: net64, excluded: CapacityExcluded{PowerGap: 1},
		},
		{
			name: "mean power too low",
			charges: []Charge{with(charged(at, AC, 30, 70, 25.6), func(c *Charge) {
				c.Span.V.EnergyKWh, c.EnergyPowerKWh.V = 1.5, 1.5
			})},
			reference: net64, excluded: CapacityExcluded{LowPower: 1},
		},
		{
			// No duration: the mean power is nothing, never infinite.
			name: "span of no duration",
			charges: []Charge{with(charged(at, AC, 30, 70, 25.6), func(c *Charge) {
				c.Span.V.Duration = 0
			})},
			reference: net64, excluded: CapacityExcluded{LowPower: 1},
		},
		{
			name:      "implausible, below the bounds",
			charges:   []Charge{charged(at, AC, 30, 70, 15)},
			reference: net64, excluded: CapacityExcluded{Implausible: 1},
		},
		{
			name:      "implausible, above the bounds",
			charges:   []Charge{charged(at, AC, 30, 70, 30.4)},
			reference: net64, excluded: CapacityExcluded{Implausible: 1},
		},
		{
			// Without a reference there is no bound: the outlier stays a point.
			name: "no reference, no bound", charges: []Charge{charged(at, AC, 30, 70, 15)},
			want: []CapacityEstimate{est(ac, EstimatePower, 40, 37.5)},
		},
		{
			// The capacity the API reports is the gross one: the bounds are wider, the
			// estimate of 64 kWh stays within.
			name: "bounds on the API's capacity", charges: []Charge{charged(at, AC, 30, 70, 25.6)},
			reference: api69, want: []CapacityEstimate{est(ac, EstimatePower, 40, 64)},
		},
		{
			// A billed candidate over the balancing with a SoC change too small: the
			// first filter takes it, not the second.
			name: "billed over the balancing and too small a span",
			charges: []Charge{with(charged(at, AC, 90, 96, 4), func(c *Charge) {
				c.Span = Value[PowerSpan]{}
			})},
			billed: []Value[BilledEnergy]{receipt(6, 0.88)}, reference: net64,
			excluded: CapacityExcluded{HighSoC: 2},
		},
		{
			// The charges need not come sorted: the series is, by date.
			name: "charges out of order", charges: []Charge{charged(later, AC, 30, 70, 25.6), ac},
			reference: net64,
			want:      []CapacityEstimate{est(ac, EstimatePower, 40, 64), est(charged(later, AC, 30, 70, 25.6), EstimatePower, 40, 64)},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cp := DefaultCapacityParams()
			if tt.params != nil {
				tt.params(&cp)
			}
			got, err := CapacityTrend(tt.charges, tt.billed, nil, tt.reference, DefaultParams(), cp, paris)
			if err != nil {
				t.Fatal(err)
			}
			checkEstimates(t, got.Estimates, tt.want)
			if got.Excluded != tt.excluded {
				t.Errorf("excluded = %+v, want %+v", got.Excluded, tt.excluded)
			}
		})
	}
}

// series is n charges, one every step days from at, whose span estimates kwh exactly.
func series(at time.Time, n, stepDays int, kwh float64) []Charge {
	out := make([]Charge, n)
	for i := range n {
		end := at.AddDate(0, 0, i*stepDays)
		out[i] = charged(end, AC, 30, 70, kwh*0.4)
	}
	return out
}

func TestCapacityCurrent(t *testing.T) {
	cp := DefaultCapacityParams()
	// 4 estimates: the series shows them, the current capacity is not told yet.
	t4, err := CapacityTrend(series(utc("2026-01-10T12:00:00Z"), 4, 30, 64), nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if len(t4.Estimates) != 4 || t4.Current.OK || t4.DeviationPct.OK || t4.Change.OK {
		t.Errorf("4 estimates: %+v, current %+v, deviation %+v, change %+v",
			t4.Estimates, t4.Current, t4.DeviationPct, t4.Change)
	}

	// 5: the current capacity and its deviation are told.
	t5, err := CapacityTrend(series(utc("2026-01-10T12:00:00Z"), 5, 30, 64), nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if !t5.Current.OK || t5.Current.V.Median != 64 || t5.Current.V.Count != 5 {
		t.Errorf("current = %+v, want the median of 5 estimates at 64 kWh", t5.Current)
	}
	if !t5.DeviationPct.OK || t5.DeviationPct.V != 0 {
		t.Errorf("deviation = %+v, want 0", t5.DeviationPct)
	}
	if t5.Change.OK {
		t.Errorf("change = %+v, want none: not twice RecentCount", t5.Change)
	}

	// 25: the window keeps RecentCount of them, and the evolution still waits for
	// twice that many.
	t25, err := CapacityTrend(series(utc("2024-01-10T12:00:00Z"), 25, 30, 64), nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if !t25.Current.OK || t25.Current.V.Count != cp.RecentCount {
		t.Errorf("current = %+v, want the last %d estimates only", t25.Current, cp.RecentCount)
	}
	if t25.Change.OK {
		t.Errorf("change = %+v, want none: %d estimates are fewer than twice RecentCount", t25.Change, 25)
	}
	// Without a reference, the current capacity stands but the deviation does not.
	t25, err = CapacityTrend(series(utc("2024-01-10T12:00:00Z"), 25, 30, 64), nil, nil, Value[Capacity]{}, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if !t25.Current.OK || t25.DeviationPct.OK {
		t.Errorf("current %+v, deviation %+v: no deviation without a reference", t25.Current, t25.DeviationPct)
	}

	// A positive bias is shown as one, never clamped to a healthy zero.
	up, err := CapacityTrend(series(utc("2026-01-10T12:00:00Z"), 5, 30, 65.92), nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if !up.Current.OK || up.Current.V.Median != 65.92 || !up.DeviationPct.OK || up.DeviationPct.V != 3 {
		t.Errorf("current %+v, deviation %+v: want 65.92 kWh, +3%%", up.Current, up.DeviationPct)
	}
}

func TestCapacityChange(t *testing.T) {
	cp := DefaultCapacityParams()
	first := utc("2025-01-10T12:00:00Z")
	// 40 estimates a month apart: the first and last windows do not overlap, and 13
	// months separate the ends.
	full, err := CapacityTrend(series(first, 40, 30, 64), nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if !full.Change.OK {
		t.Fatalf("change = %+v, want one over %d estimates", full.Change, 40)
	}
	if want := (CapacityChange{Since: first, InitialKWh: 64, ChangePct: 0}); full.Change.V != want {
		t.Errorf("change = %+v, want %+v", full.Change.V, want)
	}

	// One estimate short of twice RecentCount: no evolution.
	short, err := CapacityTrend(series(first, 2*cp.RecentCount-1, 30, 64), nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if short.Change.OK {
		t.Errorf("change = %+v, want none: %d estimates", short.Change, 2*cp.RecentCount-1)
	}

	// 40 estimates 8 days apart: 312 days from the first to the last, short of the
	// calendar year the evolution waits for.
	close := series(first, 40, 8, 64)
	tr, err := CapacityTrend(close, nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil || tr.Change.OK {
		t.Errorf("change = %+v (%v), want none within 11 months", tr.Change, err)
	}
}

func TestCapacityMonths(t *testing.T) {
	cp := DefaultCapacityParams()
	estimate := func(at time.Time, kwh float64) Charge { return charged(at, AC, 30, 70, kwh*0.4) }

	// A month without an estimate is present, its values absent: a gap, never a zero.
	got, err := CapacityTrend([]Charge{estimate(pt("2026-01-10 12:00"), 64), estimate(pt("2026-03-10 12:00"), 64)},
		nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 3 {
		t.Fatalf("%d months, want January to March", len(got.Months))
	}
	if got.Months[1].Capacity.OK || got.Months[1].Capacity.V.Count != 0 || got.Months[1].Range.OK {
		t.Errorf("February = %+v, want an empty month", got.Months[1])
	}
	for _, m := range []CapacityMonth{got.Months[0], got.Months[2]} {
		if !m.Capacity.OK || m.Capacity.V.Count != 1 {
			t.Errorf("month %v = %+v, want one estimate", m.Start, m)
		}
	}

	// The months are calendar months of the time zone, across both changes of time.
	got, err = CapacityTrend([]Charge{estimate(pt("2026-02-15 12:00"), 64), estimate(pt("2026-11-10 12:00"), 64)},
		nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 10 {
		t.Fatalf("%d months, want February to November", len(got.Months))
	}
	want := [][2]string{
		{"2026-02-28T23:00:00Z", "2026-03-31T22:00:00Z"}, // March, of 30 days less an hour
		{"2026-09-30T22:00:00Z", "2026-10-31T23:00:00Z"}, // October, of 31 days plus an hour
	}
	for _, w := range want {
		found := false
		for _, m := range got.Months {
			if m.Start.Equal(utc(w[0])) && m.End.Equal(utc(w[1])) {
				found = true
			}
		}
		if !found {
			t.Errorf("no month from %s to %s in %+v", w[0], w[1], got.Months)
		}
	}

	// 23:30 UTC is the next month in Paris, the same one in UTC.
	mar := utc("2026-03-31T23:30:00Z")
	got, err = CapacityTrend([]Charge{estimate(mar.Add(-48*time.Hour), 64), estimate(mar, 64)},
		nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 2 || !got.Months[1].Start.Equal(utc("2026-03-31T22:00:00Z")) ||
		got.Months[1].Capacity.V.Count != 1 {
		t.Errorf("months = %+v, want the estimate of 23:30 UTC in Paris's April", got.Months)
	}
	got, err = CapacityTrend([]Charge{estimate(mar.Add(-48*time.Hour), 64), estimate(mar, 64)},
		nil, nil, net64, DefaultParams(), cp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 1 || got.Months[0].Capacity.V.Count != 2 {
		t.Errorf("months = %+v, want both estimates in UTC's March", got.Months)
	}

	// More than MaxBuckets months: refused, as the statistics are.
	if _, err := CapacityTrend([]Charge{estimate(utc("1990-01-15T12:00:00Z"), 64), estimate(utc("2024-06-15T12:00:00Z"), 64)},
		nil, nil, net64, DefaultParams(), cp, paris); err != ErrTooManyBuckets {
		t.Errorf("error = %v, want ErrTooManyBuckets", err)
	}
}

// tripRange is a trip that starts at SoC with the displayed range km.
func tripRange(at time.Time, soc, km float64) Trip {
	return Trip{Start: within(at, 5), StartSoC: val(soc), StartRangeKm: val(km)}
}

func TestCapacityRangeMonths(t *testing.T) {
	// The displayed range at a full charge of the trips that start in a month, SoC and
	// range both known, above the SoC the integer rounding spoils: a median per month,
	// with how many trips it stands.
	trips := []Trip{
		tripRange(pt("2026-01-05 08:00"), 50, 190), // 380 km at a full charge
		tripRange(pt("2026-01-06 08:00"), 40, 168), // 420 km
		tripRange(pt("2026-01-07 08:00"), 10, 50),  // below RangeMinSoC: left out
		with(tripRange(pt("2026-01-08 08:00"), 60, 240), func(t *Trip) { t.StartRangeKm = Value[float64]{} }),
		// A reconstructed trip is kept: its start SoC and range are as good.
		with(tripRange(pt("2026-01-09 08:00"), 80, 320), func(t *Trip) { t.Reconstructed = true }), // 400 km
		tripRange(pt("2026-03-01 08:00"), 50, 200),                                                 // 400 km
	}
	got, err := CapacityTrend(nil, nil, trips, net64, DefaultParams(), DefaultCapacityParams(), paris)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 3 {
		t.Fatalf("%d months, want January to March: %+v", len(got.Months), got.Months)
	}
	jan, mar := got.Months[0], got.Months[2]
	if !jan.Range.OK || jan.Range.V.MedianKm != 400 || jan.Range.V.Readings != 3 {
		t.Errorf("January range = %+v, want the median of 3 readings at 400 km", jan.Range)
	}
	if !mar.Range.OK || mar.Range.V.MedianKm != 400 || mar.Range.V.Readings != 1 {
		t.Errorf("March range = %+v, want 400 km of one reading", mar.Range)
	}
	if got.Months[1].Range.OK {
		t.Errorf("February range = %+v, want none", got.Months[1].Range)
	}
	for _, m := range got.Months {
		if m.Capacity.OK {
			t.Errorf("month %v: capacity = %+v, want none without a charge", m.Start, m.Capacity)
		}
	}
}

func TestCapacityCycles(t *testing.T) {
	cp := DefaultCapacityParams()
	charges := []Charge{
		charged(utc("2026-09-21T21:40:00Z"), AC, 30, 70, 25.6), // 25.6 kWh by the SoC
		with(charged(utc("2026-10-21T21:40:00Z"), AC, 20, 70, 32), func(c *Charge) {
			c.Reconstructed, c.EnergyPowerKWh, c.Span = true, Value[float64]{}, Value[PowerSpan]{}
		}), // 32 kWh, reconstructed: it counts
		with(charged(utc("2026-11-21T21:40:00Z"), AC, 30, 70, 25.6), func(c *Charge) {
			c.EnergySoCKWh = Value[float64]{}
		}), // unknown: left out
	}
	got, err := CapacityTrend(charges, nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Cycles.OK || math.Abs(got.Cycles.V-(25.6+32)/64) > 1e-9 {
		t.Errorf("cycles = %+v, want (25.6 + 32) / 64", got.Cycles)
	}
	// The cycles are told from the reference alone, even at zero; never without it.
	got, err = CapacityTrend(nil, nil, nil, net64, DefaultParams(), cp, paris)
	if err != nil || !got.Cycles.OK || got.Cycles.V != 0 {
		t.Errorf("cycles = %+v (%v), want 0 with a reference", got.Cycles, err)
	}
	got, err = CapacityTrend(charges, nil, nil, Value[Capacity]{}, DefaultParams(), cp, paris)
	if err != nil || got.Cycles.OK {
		t.Errorf("cycles = %+v (%v), want none without a reference", got.Cycles, err)
	}
}

// TestSyntheticYear: a year of charges on a battery whose usable capacity fades
// linearly by 3 % over it, the SoC an integer at both ends of every span. The current
// capacity must come back within 2 % of the true one at the end of the year, the
// evolution at −3 % within a point, and the deviation against the reference given.
func TestSyntheticYear(t *testing.T) {
	start := utc("2025-09-01T12:00:00Z")
	fade := func(day int) float64 { return 64 * (1 - 0.03*float64(day)/365) }
	// 60 charges: 20 clustered at each end of the year, 20 spread in between, so that
	// the medians the evolution compares sit at the year's ends.
	var days []int
	for d := range 20 {
		days = append(days, d)
	}
	for i := range 20 {
		days = append(days, 40+15*i)
	}
	for d := range 20 {
		days = append(days, 360+d)
	}
	charges := make([]Charge, len(days))
	for i, d := range days {
		// The integer SoC rounds the ends of the span: the true change is a fraction
		// of a point off the 56 the readings report.
		delta := 56 + []float64{0.4, -0.4}[i%2]
		from := float64(20 + i%3)
		charges[i] = charged(start.AddDate(0, 0, d), AC, from, from+56, delta/100*fade(d))
	}
	got, err := CapacityTrend(charges, nil, nil, net64, DefaultParams(), DefaultCapacityParams(), paris)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Estimates) != len(days) || got.Excluded != (CapacityExcluded{}) {
		t.Fatalf("%d estimates, excluded %+v, want %d kept", len(got.Estimates), got.Excluded, len(days))
	}
	if !got.Current.OK || got.Current.V.Count != 20 {
		t.Fatalf("current = %+v, want the median of the last 20 estimates", got.Current)
	}
	// The true capacity now is the one at the last estimate.
	if ratio := got.Current.V.Median / fade(days[len(days)-1]); math.Abs(ratio-1) > 0.02 {
		t.Errorf("current %.2f kWh, the true one %.2f: %.1f %% off", got.Current.V.Median, fade(days[len(days)-1]), (ratio-1)*100)
	}
	if !got.Change.OK {
		t.Fatalf("change = %+v, want one over a year of estimates", got.Change)
	}
	if got.Change.V.ChangePct < -4 || got.Change.V.ChangePct > -2 {
		t.Errorf("change %d %%, want −3 within a point", got.Change.V.ChangePct)
	}
	if !got.Change.V.Since.Equal(charges[0].End.Before) {
		t.Errorf("change since %v, want %v", got.Change.V.Since, charges[0].End.Before)
	}
	if !got.DeviationPct.OK || got.DeviationPct.V != percentChange(got.Current.V.Median, 64) {
		t.Errorf("deviation %+v, want the current capacity over the reference", got.DeviationPct)
	}
	// September 2025 to September 2026.
	if len(got.Months) != 13 {
		t.Errorf("%d months, want 13", len(got.Months))
	}
	var monthsCount int
	for _, m := range got.Months {
		monthsCount += m.Capacity.V.Count
	}
	if monthsCount != len(days) {
		t.Errorf("%d estimates over the months, want %d", monthsCount, len(days))
	}
}

func TestPricingEfficiency(t *testing.T) {
	// The place takes the charges without a position too (WithoutPosition), as the
	// place of a charge never seen at its position.
	home := with(homePlace(), func(p *Place) {
		p.Efficiency, p.WithoutPosition = val(0.9), true
	})
	pricing := Pricing{Currency: eur, Places: []Place{home}, Params: DefaultCostParams()}
	away := Position{Lat: 45.9, Lon: 4.9}
	for _, tt := range []struct {
		name   string
		charge Charge
		want   float64
	}{
		{"the place's efficiency, AC", with(charged(utc("2026-09-21T21:40:00Z"), AC, 30, 70, 25.6), func(c *Charge) {
			c.Position = known(lyon)
		}), 0.9},
		{"the place's efficiency, DC", with(charged(utc("2026-09-21T21:40:00Z"), DC, 30, 70, 25.6), func(c *Charge) {
			c.Position = known(lyon)
		}), 0.9},
		{"the AC default away", with(charged(utc("2026-09-21T21:40:00Z"), AC, 30, 70, 25.6), func(c *Charge) {
			c.Position = known(away)
		}), 0.88},
		{"the DC default away", with(charged(utc("2026-09-21T21:40:00Z"), DC, 30, 70, 25.6), func(c *Charge) {
			c.Position = known(away)
		}), 0.95},
		{"the AC default for an unknown type", with(charged(utc("2026-09-21T21:40:00Z"), AC, 30, 70, 25.6), func(c *Charge) {
			c.Type, c.Position = Value[ChargeType]{}, known(away)
		}), 0.88},
		{"the place without a position", with(charged(utc("2026-09-21T21:40:00Z"), AC, 30, 70, 25.6), func(c *Charge) {
			c.Position = Value[Position]{}
		}), 0.9},
		{"a DC charge is never at home by default", with(charged(utc("2026-09-21T21:40:00Z"), DC, 30, 70, 25.6), func(c *Charge) {
			c.Position = Value[Position]{}
		}), 0.95},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if eta := pricing.Efficiency(tt.charge); math.Abs(eta-tt.want) > 1e-9 {
				t.Errorf("efficiency = %v, want %v", eta, tt.want)
			}
		})
	}
}
