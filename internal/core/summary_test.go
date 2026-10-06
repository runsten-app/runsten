package core

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
	_ "time/tzdata" // Europe/Paris whatever the host
)

var paris = mustLoad("Europe/Paris")

func mustLoad(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// pt is a wall time in Paris, "2026-03-29 08:00".
func pt(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, paris)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// utc is an RFC 3339 time.
func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// within returns the bounds [t, t+n min].
func within(t time.Time, n int) Bounds { return Bounds{t, t.Add(time.Duration(n) * time.Minute)} }

// drive is an observed trip starting within 5 minutes after start and ending within 5
// minutes after end, with a known distance, energy and SoC.
func drive(start, end time.Time, km, kwh, fromSoC, toSoC float64) Trip {
	return Trip{
		DetectedAt: start.Add(5 * time.Minute), Start: within(start, 5), End: within(end, 5),
		DistanceKm: val(km), EnergyKWh: val(kwh), StartSoC: val(fromSoC), EndSoC: val(toSoC),
	}
}

// plug is an observed charge, bounded like drive.
func plug(start, end time.Time, typ ChargeType, socKWh, powerKWh, fromSoC, toSoC float64) Charge {
	return Charge{
		DetectedAt: start.Add(5 * time.Minute), Start: within(start, 5), End: within(end, 5),
		Type: known(typ), EnergySoCKWh: val(socKWh), EnergyPowerKWh: val(powerKWh),
		StartSoC: val(fromSoC), EndSoC: val(toSoC),
	}
}

func unknown[T any]() Value[T] { return Value[T]{} }

func TestSummarizeIntervals(t *testing.T) {
	for _, tt := range []struct {
		name     string
		from, to time.Time
		loc      *time.Location
		bucket   Bucket
		want     [][2]string // limits, RFC 3339 in UTC
		wantErr  error
	}{
		{name: "totals only", from: pt("2026-03-01 00:00"), to: pt("2026-04-01 00:00"), loc: paris},
		{
			name: "day of 23 hours", from: pt("2026-03-29 00:00"), to: pt("2026-03-30 00:00"), loc: paris, bucket: BucketDay,
			want: [][2]string{{"2026-03-28T23:00:00Z", "2026-03-29T22:00:00Z"}},
		},
		{
			name: "day of 25 hours", from: pt("2026-10-25 00:00"), to: pt("2026-10-26 00:00"), loc: paris, bucket: BucketDay,
			want: [][2]string{{"2026-10-24T22:00:00Z", "2026-10-25T23:00:00Z"}},
		},
		{
			// From the start of the day holding from, to the end of the day holding the
			// instant before to.
			name: "partial days", from: pt("2026-06-01 10:00"), to: pt("2026-06-03 00:00"), loc: paris, bucket: BucketDay,
			want: [][2]string{
				{"2026-05-31T22:00:00Z", "2026-06-01T22:00:00Z"},
				{"2026-06-01T22:00:00Z", "2026-06-02T22:00:00Z"},
			},
		},
		{
			name: "one second into the next day", from: pt("2026-06-01 10:00"), to: pt("2026-06-03 00:00").Add(time.Second),
			loc: paris, bucket: BucketDay,
			want: [][2]string{
				{"2026-05-31T22:00:00Z", "2026-06-01T22:00:00Z"},
				{"2026-06-01T22:00:00Z", "2026-06-02T22:00:00Z"},
				{"2026-06-02T22:00:00Z", "2026-06-03T22:00:00Z"},
			},
		},
		{
			name: "weeks across both changes of time", from: pt("2026-03-29 12:00"), to: pt("2026-10-26 00:00"), loc: paris,
			bucket: BucketWeek,
			want: func() [][2]string {
				// Monday 23 March, winter time; 31 weeks up to Monday 26 October, winter
				// time again.
				var w [][2]string
				for i := range 31 {
					s := time.Date(2026, 3, 23+7*i, 0, 0, 0, 0, paris).UTC()
					e := time.Date(2026, 3, 30+7*i, 0, 0, 0, 0, paris).UTC()
					w = append(w, [2]string{s.Format(time.RFC3339), e.Format(time.RFC3339)})
				}
				return w
			}(),
		},
		{
			name: "week of 167 hours", from: pt("2026-03-29 00:00"), to: pt("2026-03-29 01:00"), loc: paris, bucket: BucketWeek,
			want: [][2]string{{"2026-03-22T23:00:00Z", "2026-03-29T22:00:00Z"}},
		},
		{
			name: "week of 169 hours", from: pt("2026-10-19 00:00"), to: pt("2026-10-20 00:00"), loc: paris, bucket: BucketWeek,
			want: [][2]string{{"2026-10-18T22:00:00Z", "2026-10-25T23:00:00Z"}},
		},
		{
			// 2026-12-31 is a Thursday: ISO week 53 of 2026 runs from Monday 28 December
			// to Sunday 3 January.
			name: "ISO weeks across a new year", from: pt("2027-01-01 00:00"), to: pt("2027-01-05 00:00"), loc: paris,
			bucket: BucketWeek,
			want: [][2]string{
				{"2026-12-27T23:00:00Z", "2027-01-03T23:00:00Z"},
				{"2027-01-03T23:00:00Z", "2027-01-10T23:00:00Z"},
			},
		},
		{
			name: "week from a Monday", from: pt("2026-06-01 00:00"), to: pt("2026-06-08 00:00"), loc: paris, bucket: BucketWeek,
			want: [][2]string{{"2026-05-31T22:00:00Z", "2026-06-07T22:00:00Z"}},
		},
		{
			name: "months of 31, 28, 31 less an hour, and 30 days", from: pt("2026-01-15 12:00"), to: pt("2026-04-30 00:00"),
			loc: paris, bucket: BucketMonth,
			want: [][2]string{
				{"2025-12-31T23:00:00Z", "2026-01-31T23:00:00Z"},
				{"2026-01-31T23:00:00Z", "2026-02-28T23:00:00Z"},
				{"2026-02-28T23:00:00Z", "2026-03-31T22:00:00Z"},
				{"2026-03-31T22:00:00Z", "2026-04-30T22:00:00Z"},
			},
		},
		{
			name: "month of 29 days", from: pt("2028-02-10 00:00"), to: pt("2028-03-01 00:00"), loc: paris, bucket: BucketMonth,
			want: [][2]string{{"2028-01-31T23:00:00Z", "2028-02-29T23:00:00Z"}},
		},
		{
			name: "UTC when no location", from: utc("2026-06-01T10:00:00Z"), to: utc("2026-06-01T11:00:00Z"), bucket: BucketDay,
			want: [][2]string{{"2026-06-01T00:00:00Z", "2026-06-02T00:00:00Z"}},
		},
		{
			name: "400 intervals", from: utc("2025-01-01T00:00:00Z"), to: utc("2025-01-01T00:00:00Z").AddDate(0, 0, 400),
			loc: time.UTC, bucket: BucketDay,
			want: func() [][2]string {
				var w [][2]string
				for i := range 400 {
					s := utc("2025-01-01T00:00:00Z").AddDate(0, 0, i)
					w = append(w, [2]string{s.Format(time.RFC3339), s.AddDate(0, 0, 1).Format(time.RFC3339)})
				}
				return w
			}(),
		},
		{
			name: "401 intervals", from: utc("2025-01-01T00:00:00Z"), to: utc("2025-01-01T00:00:01Z").AddDate(0, 0, 400),
			loc: time.UTC, bucket: BucketDay, wantErr: ErrTooManyBuckets,
		},
		{
			name: "too many intervals, however long", from: utc("2000-01-01T00:00:00Z"), to: utc("2100-01-01T00:00:00Z"),
			loc: time.UTC, bucket: BucketWeek, wantErr: ErrTooManyBuckets,
		},
		{
			name: "a century of totals", from: utc("2000-01-01T00:00:00Z"), to: utc("2100-01-01T00:00:00Z"), loc: time.UTC,
		},
		{name: "empty period", from: pt("2026-06-01 00:00"), to: pt("2026-06-01 00:00"), loc: paris, wantErr: ErrEmptyPeriod},
		{
			name: "reversed period", from: pt("2026-06-02 00:00"), to: pt("2026-06-01 00:00"), loc: paris, bucket: BucketDay,
			wantErr: ErrEmptyPeriod,
		},
		{
			name: "unknown bucket", from: pt("2026-06-01 00:00"), to: pt("2026-06-02 00:00"), loc: paris, bucket: "year",
			wantErr: ErrUnknownBucket,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Summarize(nil, nil, Costs{}, nil, tt.from, tt.to, tt.loc, tt.bucket, DefaultParams())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.bucket == BucketNone && s.Buckets != nil {
				t.Errorf("buckets = %v, want none", s.Buckets)
			}
			var got [][2]string
			for _, b := range s.Buckets {
				if b.Start.Location() != time.UTC || b.End.Location() != time.UTC {
					t.Errorf("bucket %v not in UTC", b)
				}
				if b.Stats != (Stats{}) {
					t.Errorf("bucket %s: stats = %+v, want zero", b.Start, b.Stats)
				}
				got = append(got, [2]string{b.Start.Format(time.RFC3339), b.End.Format(time.RFC3339)})
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buckets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSummarizeAttachment(t *testing.T) {
	from, to := pt("2026-06-01 00:00"), pt("2026-06-04 00:00")
	trips := []Trip{
		drive(pt("2026-05-31 23:00"), pt("2026-05-31 23:30"), 10, 2, 80, 78), // before the period: parked only
		drive(pt("2026-06-01 23:50"), pt("2026-06-02 00:10"), 20, 3, 78, 75), // across midnight: 1 June
		drive(pt("2026-06-03 00:00"), pt("2026-06-03 00:20"), 3, 1, 75, 75),  // at midnight: 3 June
		drive(pt("2026-06-03 23:59"), pt("2026-06-04 00:30"), 5, 1, 75, 74),  // last minute of 3 June
		drive(pt("2026-06-04 00:00"), pt("2026-06-04 00:30"), 7, 1, 74, 73),  // at to: out
	}
	s, err := Summarize(trips, nil, Costs{}, nil, from, to, paris, BucketDay, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct{ count, parked int }{{1, 1}, {0, 0}, {2, 2}} {
		b := s.Buckets[i].Stats
		if b.Trips.Count != want.count || b.Parked.Intervals != want.parked {
			t.Errorf("day %d: %d trips and %d parked intervals, want %d and %d",
				i+1, b.Trips.Count, b.Parked.Intervals, want.count, want.parked)
		}
	}
	if s.Totals.Trips.Count != 3 || s.Totals.Trips.DistanceKm != 28 {
		t.Errorf("totals = %+v", s.Totals.Trips)
	}
}

func TestSummarizeTrips(t *testing.T) {
	t1 := pt("2026-06-01 08:00")
	h := func(n int) time.Time { return t1.Add(time.Duration(n) * time.Hour) }
	for _, tt := range []struct {
		name  string
		trips []Trip
		want  TripStats
	}{
		{name: "no trip: zero sums, unknown consumption"},
		{
			// Bounds [0, 5] and [30, 35] minutes: 25 to 35 minutes, as the front shows it.
			name:  "one trip",
			trips: []Trip{drive(t1, t1.Add(30*time.Minute), 12, 3, 80, 77)},
			want: TripStats{
				Count: 1, DistanceKm: 12, EnergyKWh: 3, DrivingTime: Span{25 * time.Minute, 35 * time.Minute},
				ConsumptionKWhPer100km: val(25),
			},
		},
		{
			// Overlapping bounds: the minimum is 0, never negative (honestDuration).
			name: "short trip with wide bounds",
			trips: []Trip{{
				DetectedAt: t1, Start: within(t1, 10), End: within(t1.Add(4*time.Minute), 8),
				DistanceKm: val(1), EnergyKWh: val(0.5),
			}},
			want: TripStats{
				Count: 1, DistanceKm: 1, EnergyKWh: 0.5, DrivingTime: Span{0, 12 * time.Minute},
				ConsumptionKWhPer100km: val(50),
			},
		},
		{
			name: "consumption over the complete trips only",
			trips: []Trip{
				drive(h(0), h(1), 10, 2, 80, 78),
				drive(h(2), h(3), 30, 4, 78, 73),
				func() Trip { // unknown energy
					tr := drive(h(4), h(5), 5, 0, 73, 0)
					tr.EnergyKWh, tr.EndSoC = unknown[float64](), unknown[float64]()
					return tr
				}(),
				func() Trip { // unknown distance
					tr := drive(h(6), h(7), 0, 3, 72, 68)
					tr.DistanceKm = unknown[float64]()
					return tr
				}(),
				drive(h(8), h(9), 0, 1, 68, 67), // no distance: no consumption
			},
			want: TripStats{
				Count: 5, DistanceKm: 45, DistanceUnknown: 1, EnergyKWh: 10, EnergyUnknown: 1,
				DrivingTime:            Span{5 * 55 * time.Minute, 5 * 65 * time.Minute},
				ConsumptionKWhPer100km: val(15), // 6 kWh over 40 km
			},
		},
		{
			name: "no complete trip",
			trips: []Trip{func() Trip {
				tr := drive(h(0), h(1), 10, 0, 80, 78)
				tr.EnergyKWh = unknown[float64]()
				return tr
			}()},
			want: TripStats{
				Count: 1, DistanceKm: 10, EnergyUnknown: 1, DrivingTime: Span{55 * time.Minute, 65 * time.Minute},
			},
		},
		{
			// A reconstructed trip has a distance and an energy, but no duration.
			name: "reconstructed trip",
			trips: []Trip{
				drive(h(0), h(1), 10, 2, 80, 78),
				{
					DetectedAt: h(9), Reconstructed: true, Start: Bounds{h(2), h(9)}, End: Bounds{h(2), h(9)},
					DistanceKm: val(30), EnergyKWh: val(6), StartSoC: val(78), EndSoC: val(71),
				},
			},
			want: TripStats{
				Count: 2, Reconstructed: 1, DistanceKm: 40, EnergyKWh: 8,
				DrivingTime: Span{55 * time.Minute, 65 * time.Minute}, DrivingTimeUnknown: 1,
				ConsumptionKWhPer100km: val(20),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Summarize(tt.trips, nil, Costs{}, nil, pt("2026-06-01 00:00"), pt("2026-06-02 00:00"), paris, BucketNone, DefaultParams())
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Totals.Trips; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("trips = %+v\nwant    %+v", got, tt.want)
			}
		})
	}
}

// The observed trips of the period spread over bands of distance, each with its
// consumption; a reconstructed trip, one without a distance, and one before the period
// are left out of every band.
func TestSummarizeTripsByDistance(t *testing.T) {
	t1 := pt("2026-06-01 08:00")
	h := func(n int) time.Time { return t1.Add(time.Duration(n) * time.Hour) }
	noDistance := drive(h(6), h(7), 0, 1, 60, 59)
	noDistance.DistanceKm = unknown[float64]()
	trips := []Trip{
		drive(h(-24), h(-23), 3, 1, 90, 89), // the day before: it opens the parked time only
		drive(h(0), h(1), 4.9, 1, 89, 88),
		drive(h(1), h(2), 5, 1, 88, 87), // a limit belongs to the band above
		drive(h(2), h(3), 15, 3, 87, 83),
		drive(h(3), h(4), 140, 21, 83, 50),
		{
			DetectedAt: h(5), Reconstructed: true, Start: Bounds{h(4), h(5)}, End: Bounds{h(4), h(5)},
			DistanceKm: val(30), EnergyKWh: val(6), StartSoC: val(50), EndSoC: val(40),
		},
		noDistance,
	}
	s, err := Summarize(trips, nil, Costs{}, nil, pt("2026-06-01 00:00"), pt("2026-06-02 00:00"), paris, BucketDay, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	want := TripsByDistance{
		Bands: []DistanceBand{
			{MinKm: 0, MaxKm: val(5), Count: 1, ConsumptionKWhPer100km: val(1 * 100 / 4.9)},
			{MinKm: 5, MaxKm: val(20), Count: 2, ConsumptionKWhPer100km: val(20)}, // 4 kWh over 20 km
			{MinKm: 20, MaxKm: val(50)},
			{MinKm: 50, MaxKm: val(100)},
			{MinKm: 100, Count: 1, ConsumptionKWhPer100km: val(15)},
		},
		LeftOut: 2,
	}
	if got := s.TripsByDistance; !reflect.DeepEqual(got, want) {
		t.Errorf("by distance = %+v\nwant          %+v", got, want)
	}
}

// The observed charges of the period count by the SoC they started and ended at, rounded
// to the nearest point; a reconstructed charge and one without a SoC are left out.
func TestSummarizeChargesBySoC(t *testing.T) {
	t1 := pt("2026-06-01 08:00")
	h := func(n int) time.Time { return t1.Add(time.Duration(n) * time.Hour) }
	noEnd := plug(h(4), h(5), AC, 0, 0, 30, 0)
	noEnd.EndSoC = unknown[float64]()
	charges := []Charge{
		plug(h(-24), h(-23), AC, 10, 10, 10, 90), // the day before
		plug(h(0), h(2), AC, 16, 15, 19.6, 80),
		plug(h(2), h(3), DC, 40, 38, 20, 100.2),
		plug(h(3), h(4), AC, 8, 8, 20.4, 80),
		{
			DetectedAt: h(9), Reconstructed: true, Start: Bounds{h(5), h(9)}, End: Bounds{h(5), h(9)},
			EnergySoCKWh: val(20), StartSoC: val(55), EndSoC: val(80),
		},
		noEnd,
	}
	s, err := Summarize(nil, charges, Costs{}, nil, pt("2026-06-01 00:00"), pt("2026-06-02 00:00"), paris, BucketNone, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	var want ChargesBySoC
	want.Start[20] = 3
	want.End[80], want.End[100] = 2, 1
	want.LeftOut = 2
	if got := s.ChargesBySoC; got != want {
		t.Errorf("by SoC = %+v\nwant     %+v", got, want)
	}
}

// The charges of the period group by the place their cost takes, then outside every
// place by type, then without a position; reconstructed ones are counted, and a place
// without a charge is left out.
func TestSummarizeChargesByPlace(t *testing.T) {
	t1 := pt("2026-06-01 08:00")
	h := func(n int) time.Time { return t1.Add(time.Duration(n) * time.Hour) }
	home := Place{ID: "home", Name: "Home", Position: Position{Lat: 59.33, Lon: 18.06}, RadiusM: 100, WithoutPosition: true}
	work := Place{ID: "work", Name: "Work", Position: Position{Lat: 59.40, Lon: 17.95}, RadiusM: 100}
	empty := Place{ID: "empty", Name: "Cabin", Position: Position{Lat: 61, Lon: 14}, RadiusM: 100}
	away := Position{Lat: 55.6, Lon: 13}
	at := func(c Charge, p Value[Position]) Charge { c.Position = p; return c }
	noEnergy := at(plug(h(3), h(4), AC, 0, 0, 50, 60), known(work.Position))
	noEnergy.EnergySoCKWh = unknown[float64]()
	reconstructed := Charge{
		DetectedAt: h(9), Reconstructed: true, Start: Bounds{h(5), h(9)}, End: Bounds{h(5), h(9)},
		EnergySoCKWh: val(12), StartSoC: val(40), EndSoC: val(60),
	}
	lost := reconstructed // reconstructed outside every place: no type
	lost.DetectedAt, lost.Position = h(10), known(away)
	charges := []Charge{
		at(plug(h(-24), h(-23), AC, 10, 10, 10, 90), known(home.Position)), // the day before
		at(plug(h(0), h(2), AC, 16, 15, 20, 80), known(home.Position)),
		reconstructed, // no position: home takes it, as its cost
		at(plug(h(2), h(3), AC, 8, 8, 40, 50), known(work.Position)),
		noEnergy,
		at(plug(h(4), h(5), AC, 7, 7, 50, 60), known(away)),
		at(plug(h(5), h(6), DC, 40, 38, 20, 80), known(away)),
		lost,
		plug(h(11), h(12), DC, 30, 29, 20, 70), // DC without a position: never at home
	}
	tariff := func(lo, hi int64) Value[Cost] { return known(Cost{Min: lo, Max: hi, Source: CostTariff}) }
	costs := Costs{Charges: []Value[Cost]{
		tariff(1, 1), tariff(300, 400), tariff(200, 250), tariff(150, 150),
		{},
		{},
		known(Cost{Min: 2000, Max: 2000, Source: CostEntered}),
		{},
		{},
	}}
	s, err := Summarize(nil, charges, costs, []Place{home, empty, work}, pt("2026-06-01 00:00"), pt("2026-06-02 00:00"), paris, BucketNone, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	want := ChargesByPlace{
		Places: []PlaceCharges{
			{home, PlaceStats{Count: 2, Reconstructed: 1, EnergySoCKWh: 28, Cost: CostSum{Min: 500, Max: 650}}},
			{work, PlaceStats{Count: 2, EnergySoCKWh: 8, EnergySoCUnknown: 1, Cost: CostSum{Min: 150, Max: 150, Unknown: 1}}},
		},
		AC:          PlaceStats{Count: 1, EnergySoCKWh: 7, Cost: CostSum{Unknown: 1}},
		DC:          PlaceStats{Count: 1, EnergySoCKWh: 40, Cost: CostSum{Min: 2000, Max: 2000, Entered: 1}},
		UnknownType: PlaceStats{Count: 1, Reconstructed: 1, EnergySoCKWh: 12, Cost: CostSum{Unknown: 1}},
		NoPosition:  PlaceStats{Count: 1, EnergySoCKWh: 30, Cost: CostSum{Unknown: 1}},
	}
	if got := s.ChargesByPlace; !reflect.DeepEqual(got, want) {
		t.Errorf("by place = %+v\nwant        %+v", got, want)
	}

	// Without places, every charge is outside, or without a position.
	s, err = Summarize(nil, charges, Costs{}, nil, pt("2026-06-01 00:00"), pt("2026-06-02 00:00"), paris, BucketNone, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.ChargesByPlace; len(got.Places) != 0 || got.AC.Count != 4 || got.NoPosition.Count != 2 {
		t.Errorf("without places = %+v", got)
	}
	if got := NoChargesByPlace(); got.Places == nil || got.AC.Count != 0 {
		t.Errorf("no charges = %+v", got)
	}
}

func TestSummarizeCharges(t *testing.T) {
	t1 := pt("2026-06-01 08:00")
	h := func(n int) time.Time { return t1.Add(time.Duration(n) * time.Hour) }
	for _, tt := range []struct {
		name    string
		charges []Charge
		want    ChargeStats
	}{
		{name: "no charge"},
		{
			name: "both energies, by type",
			charges: []Charge{
				plug(h(0), h(2), AC, 16, 15, 40, 60),
				plug(h(3), h(4), DC, 40, 38, 20, 70),
				plug(h(5), h(7), AC, 8, 7.5, 60, 70),
			},
			want: ChargeStats{
				Count: 3, EnergySoCKWh: 64, EnergyPowerKWh: 60.5,
				ChargingTime: Span{(2*115 + 55) * time.Minute, (2*125 + 65) * time.Minute},
				Cost:         CostSum{Unknown: 3}, // no cost given
				AC:           ChargeTypeStats{Count: 2, EnergySoCKWh: 24, Cost: CostSum{Unknown: 2}},
				DC:           ChargeTypeStats{Count: 1, EnergySoCKWh: 40, Cost: CostSum{Unknown: 1}},
			},
		},
		{
			// Only the SoC tells a reconstructed charge: no power, no type, no duration.
			name: "reconstructed charge",
			charges: []Charge{
				plug(h(0), h(2), AC, 16, 15, 40, 60),
				{
					DetectedAt: h(9), Reconstructed: true, Start: Bounds{h(3), h(9)}, End: Bounds{h(3), h(9)},
					EnergySoCKWh: val(20), StartSoC: val(55), EndSoC: val(80),
				},
			},
			want: ChargeStats{
				Count: 2, Reconstructed: 1, EnergySoCKWh: 36, EnergyPowerKWh: 15, EnergyPowerUnknown: 1,
				ChargingTime: Span{115 * time.Minute, 125 * time.Minute}, ChargingTimeUnknown: 1, Cost: CostSum{Unknown: 2},
				AC:          ChargeTypeStats{Count: 1, EnergySoCKWh: 16, Cost: CostSum{Unknown: 1}},
				UnknownType: ChargeTypeStats{Count: 1, EnergySoCKWh: 20, Cost: CostSum{Unknown: 1}},
			},
		},
		{
			name: "unknown energies and type",
			charges: []Charge{
				func() Charge {
					c := plug(h(0), h(2), DC, 0, 0, 40, 60)
					c.EnergySoCKWh, c.EnergyPowerKWh = unknown[float64](), unknown[float64]()
					return c
				}(),
				func() Charge {
					c := plug(h(3), h(4), "", 5, 5, 60, 66)
					c.Type = unknown[ChargeType]()
					return c
				}(),
			},
			want: ChargeStats{
				Count: 2, EnergySoCKWh: 5, EnergySoCUnknown: 1, EnergyPowerKWh: 5, EnergyPowerUnknown: 1,
				ChargingTime: Span{(115 + 55) * time.Minute, (125 + 65) * time.Minute}, Cost: CostSum{Unknown: 2},
				DC:          ChargeTypeStats{Count: 1, EnergySoCUnknown: 1, Cost: CostSum{Unknown: 1}},
				UnknownType: ChargeTypeStats{Count: 1, EnergySoCKWh: 5, Cost: CostSum{Unknown: 1}},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Summarize(nil, tt.charges, Costs{}, nil, pt("2026-06-01 00:00"), pt("2026-06-02 00:00"), paris, BucketNone, DefaultParams())
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Totals.Charges; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("charges = %+v\nwant      %+v", got, tt.want)
			}
		})
	}
}

// Costs sum by charge, known ones only, in the interval of the charge's Start.After;
// orphaned entered costs count apart, in the interval of their WindowAfter, never in the
// cost.
func TestSummarizeCosts(t *testing.T) {
	from, to := pt("2026-06-01 00:00"), pt("2026-06-03 00:00")
	day1, day2 := pt("2026-06-01 20:00"), pt("2026-06-02 20:00")
	tariff := func(lo, hi int64) Value[Cost] { return known(Cost{Min: lo, Max: hi, Source: CostTariff}) }
	entered := func(n int64) Value[Cost] { return known(Cost{Min: n, Max: n, Source: CostEntered}) }
	charges := []Charge{
		plug(pt("2026-05-31 20:00"), pt("2026-05-31 23:00"), AC, 10, 10, 50, 70), // before the period
		plug(day1, day1.Add(3*time.Hour), AC, 20, 19, 40, 70),
		plug(day1.Add(4*time.Hour), day1.Add(6*time.Hour), DC, 30, 29, 20, 80), // after midnight: day 2
		plug(day2, day2.Add(time.Hour), AC, 5, 5, 60, 66),
		func() Charge {
			c := plug(day2.Add(2*time.Hour), day2.Add(3*time.Hour), "", 5, 5, 66, 72)
			c.Type = unknown[ChargeType]()
			return c
		}(),
	}
	costs := Costs{
		Charges: []Value[Cost]{entered(9999), tariff(476, 643), entered(1240), {}, tariff(0, 0)},
		Orphans: []EnteredCost{
			{WindowAfter: day1.Add(-time.Hour), AmountMinor: 700}, // day 1
			{WindowAfter: day2, AmountMinor: 300},                 // day 2
			{WindowAfter: from.Add(-time.Minute), AmountMinor: 5}, // before the period
			{WindowAfter: to, AmountMinor: 6},                     // after it
		},
	}
	s, err := Summarize(nil, charges, costs, nil, from, to, paris, BucketDay, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	type got struct {
		cost               CostSum
		orphaned           OrphanedCosts
		ac, dc, unknownTyp CostSum
	}
	of := func(c ChargeStats) got { return got{c.Cost, c.Orphaned, c.AC.Cost, c.DC.Cost, c.UnknownType.Cost} }
	for _, tt := range []struct {
		name string
		got  ChargeStats
		want got
	}{
		{"totals", s.Totals.Charges, got{
			cost:     CostSum{Min: 476 + 1240, Max: 643 + 1240, Unknown: 1, Entered: 1},
			orphaned: OrphanedCosts{Count: 2, AmountMinor: 1000},
			ac:       CostSum{Min: 476, Max: 643, Unknown: 1}, dc: CostSum{Min: 1240, Max: 1240, Entered: 1},
		}},
		{"day 1", s.Buckets[0].Charges, got{
			cost: CostSum{Min: 476, Max: 643}, orphaned: OrphanedCosts{Count: 1, AmountMinor: 700},
			ac: CostSum{Min: 476, Max: 643},
		}},
		{"day 2", s.Buckets[1].Charges, got{
			cost:     CostSum{Min: 1240, Max: 1240, Unknown: 1, Entered: 1},
			orphaned: OrphanedCosts{Count: 1, AmountMinor: 300},
			ac:       CostSum{Unknown: 1}, dc: CostSum{Min: 1240, Max: 1240, Entered: 1},
		}},
	} {
		if g := of(tt.got); g != tt.want {
			t.Errorf("%s: %+v\nwant %+v", tt.name, g, tt.want)
		}
	}

	// Fewer costs than charges: the others are unknown.
	s, err = Summarize(nil, charges, Costs{Charges: costs.Charges[:2]}, nil, from, to, paris, BucketNone, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if c := s.Totals.Charges.Cost; c != (CostSum{Min: 476, Max: 643, Unknown: 3}) {
		t.Errorf("cost = %+v", c)
	}
}

func TestSummarizeParked(t *testing.T) {
	from, to := pt("2026-06-01 00:00"), pt("2026-06-03 00:00")
	evening, morning := pt("2026-06-01 18:00"), pt("2026-06-02 08:00") // trips end 18:05, start 08:00: 13 h 55
	const parked = 13*time.Hour + 55*time.Minute
	perDay := func(loss float64, d time.Duration) Value[float64] { return val(loss / d.Hours() * 24) }
	for _, tt := range []struct {
		name    string
		trips   []Trip
		charges []Charge
		params  func(*Params)
		want    ParkedStats
		perDay  []int // parked intervals by day
	}{
		{name: "no event: unknown rate", perDay: []int{0, 0}},
		{
			name:   "a single event",
			trips:  []Trip{drive(morning, morning.Add(time.Hour), 10, 2, 80, 78)},
			perDay: []int{0, 0},
		},
		{
			name: "drop overnight, counted on the day of the next event",
			trips: []Trip{
				drive(evening.Add(-time.Hour), evening, 10, 2, 80, 72),
				drive(morning, morning.Add(time.Hour), 10, 2, 69, 67),
			},
			want:   ParkedStats{Intervals: 1, Time: parked, SoCLossPct: 3, SoCLossPctPerDay: perDay(3, parked)},
			perDay: []int{0, 1},
		},
		{
			name: "rise below MinChargeSoC is noise",
			trips: []Trip{
				drive(evening.Add(-time.Hour), evening, 10, 2, 80, 72),
				drive(morning, morning.Add(time.Hour), 10, 2, 73.5, 71),
			},
			want:   ParkedStats{Intervals: 1, Time: parked, SoCLossPctPerDay: val(0)},
			perDay: []int{0, 1},
		},
		{
			name: "a larger threshold keeps a rise as noise",
			trips: []Trip{
				drive(evening.Add(-time.Hour), evening, 10, 2, 80, 72),
				drive(morning, morning.Add(time.Hour), 10, 2, 74, 71),
			},
			params: func(p *Params) { p.MinChargeSoC = 3 },
			want:   ParkedStats{Intervals: 1, Time: parked, SoCLossPctPerDay: val(0)},
			perDay: []int{0, 1},
		},
		{
			// A rise of MinChargeSoC or more is a charge no event holds: the loss is
			// unknown, and its time is left out of the rate.
			name: "rise of MinChargeSoC: unknown loss",
			trips: []Trip{
				drive(evening.Add(-time.Hour), evening, 10, 2, 80, 72),
				drive(morning, morning.Add(time.Hour), 10, 2, 74, 71),
				drive(morning.Add(3*time.Hour), morning.Add(4*time.Hour), 10, 2, 70, 68),
			},
			want: ParkedStats{
				Intervals: 2, Time: parked + 115*time.Minute, SoCLossPct: 1, SoCLossUnknown: 1,
				SoCLossPctPerDay: perDay(1, 115*time.Minute),
			},
			perDay: []int{0, 2},
		},
		{
			name: "unknown SoC at one end",
			trips: []Trip{
				func() Trip {
					tr := drive(evening.Add(-time.Hour), evening, 10, 2, 80, 0)
					tr.EndSoC = unknown[float64]()
					return tr
				}(),
				drive(morning, morning.Add(time.Hour), 10, 2, 69, 67),
			},
			want:   ParkedStats{Intervals: 1, Time: parked, SoCLossUnknown: 1},
			perDay: []int{0, 1},
		},
		{
			// The latest event before the period opens its first parked interval, and
			// counts nowhere else.
			name: "first interval from the event before the period",
			trips: []Trip{
				drive(pt("2026-05-31 17:00"), pt("2026-05-31 18:00"), 10, 2, 80, 72),
				drive(pt("2026-06-01 08:00"), pt("2026-06-01 09:00"), 10, 2, 70, 67),
			},
			want:   ParkedStats{Intervals: 1, Time: parked, SoCLossPct: 2, SoCLossPctPerDay: perDay(2, parked)},
			perDay: []int{1, 0},
		},
		{
			// Trips and charges together, given unsorted: trip, charge, trip.
			name: "trips and charges in order of start",
			trips: []Trip{
				drive(morning.Add(4*time.Hour), morning.Add(5*time.Hour), 10, 2, 88, 85),
				drive(evening.Add(-time.Hour), evening, 10, 2, 80, 72),
			},
			charges: []Charge{plug(morning, morning.Add(3*time.Hour), AC, 16, 15, 71, 90)},
			want: ParkedStats{
				Intervals: 2, Time: parked + 55*time.Minute, SoCLossPct: 3, SoCLossPctPerDay: perDay(3, parked+55*time.Minute),
			},
			perDay: []int{0, 2},
		},
		{
			// Same start: the first detected comes first.
			name: "ties broken by detection",
			trips: []Trip{
				func() Trip {
					tr := drive(morning, morning.Add(time.Hour), 10, 2, 60, 55)
					tr.DetectedAt = morning.Add(20 * time.Minute)
					return tr
				}(),
				func() Trip {
					tr := drive(morning, morning.Add(10*time.Minute), 1, 0.5, 70, 61)
					tr.DetectedAt = morning.Add(10 * time.Minute)
					return tr
				}(),
			},
			// 61 → 60 over no time: bounds overlap, the parked time is 0.
			want:   ParkedStats{Intervals: 1, SoCLossPct: 1},
			perDay: []int{0, 1},
		},
		{
			name: "several days",
			trips: []Trip{
				drive(pt("2026-05-31 17:00"), pt("2026-05-31 18:00"), 10, 2, 80, 72),
				drive(pt("2026-06-01 08:00"), pt("2026-06-01 09:00"), 10, 2, 70, 67),
				drive(evening.Add(-time.Hour), evening, 10, 2, 67, 60),
				drive(morning, morning.Add(time.Hour), 10, 2, 57, 50),
			},
			want: ParkedStats{
				Intervals: 3, Time: 2*parked + 7*time.Hour + 55*time.Minute, SoCLossPct: 5,
				SoCLossPctPerDay: perDay(5, 2*parked+7*time.Hour+55*time.Minute),
			},
			perDay: []int{2, 1},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := DefaultParams()
			if tt.params != nil {
				tt.params(&p)
			}
			s, err := Summarize(tt.trips, tt.charges, Costs{}, nil, from, to, paris, BucketDay, p)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Totals.Parked; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parked = %+v\nwant     %+v", got, tt.want)
			}
			var got []int
			for _, b := range s.Buckets {
				got = append(got, b.Parked.Intervals)
			}
			if !reflect.DeepEqual(got, tt.perDay) {
				t.Errorf("parked intervals by day = %v, want %v", got, tt.perDay)
			}
		})
	}
}

// history is a pseudo-random history of trips and charges across the changes of time
// of 2026, with unknown values and reconstructed events, and the costs of the charges:
// computed, entered or unknown, and orphaned entered costs.
func history(seed uint64) ([]Trip, []Charge, Costs) {
	r := rand.New(rand.NewPCG(seed, seed)) //nolint:gosec // reproducible test data
	maybe := func(v float64) Value[float64] {
		if r.IntN(8) == 0 {
			return Value[float64]{}
		}
		return val(v)
	}
	var trips []Trip
	var charges []Charge
	var costs Costs
	t := pt("2026-02-20 06:00")
	for t.Before(pt("2026-11-10 00:00")) {
		t = t.Add(time.Duration(1+r.IntN(20*60)) * time.Minute)
		end := t.Add(time.Duration(5+r.IntN(120)) * time.Minute)
		start, stop := within(t, r.IntN(6)), within(end, r.IntN(6))
		reconstructed := r.IntN(10) == 0
		if reconstructed {
			start, stop = Bounds{t, end}, Bounds{t, end}
		}
		if r.IntN(4) == 0 {
			var typ Value[ChargeType]
			switch r.IntN(3) {
			case 0:
				typ = known(AC)
			case 1:
				typ = known(DC)
			}
			charges = append(charges, Charge{
				DetectedAt: start.Before, Reconstructed: reconstructed, Start: start, End: stop, Type: typ,
				StartSoC: maybe(20 + float64(r.IntN(30))), EndSoC: maybe(60 + float64(r.IntN(30))),
				EnergySoCKWh: maybe(r.Float64() * 50), EnergyPowerKWh: maybe(r.Float64() * 50),
			})
			var cost Value[Cost]
			if r.IntN(5) > 0 {
				lo := int64(r.IntN(2000))
				cost = known(Cost{Min: lo, Max: lo + int64(r.IntN(300)), Source: []CostSource{CostTariff, CostEntered}[r.IntN(2)]})
			}
			costs.Charges = append(costs.Charges, cost)
			if r.IntN(6) == 0 {
				costs.Orphans = append(costs.Orphans, EnteredCost{WindowAfter: start.After, WindowBefore: stop.Before, AmountMinor: int64(r.IntN(3000))})
			}
		} else {
			trips = append(trips, Trip{
				DetectedAt: start.Before, Reconstructed: reconstructed, Start: start, End: stop,
				StartSoC: maybe(40 + float64(r.IntN(50))), EndSoC: maybe(30 + float64(r.IntN(40))),
				DistanceKm: maybe(float64(r.IntN(80))), EnergyKWh: maybe(r.Float64() * 15),
			})
		}
		t = end
	}
	return trips, charges, costs
}

// Every event counts once, whatever the split: the totals are the sum of the intervals,
// and do not depend on the split.
func TestSummarizeTotalsAreSums(t *testing.T) {
	from, to := pt("2026-03-01 00:00"), pt("2026-11-01 00:00")
	for seed := range uint64(4) {
		trips, charges, costs := history(seed)
		none, err := Summarize(trips, charges, costs, nil, from, to, paris, BucketNone, DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		if c := none.Totals.Charges; none.Totals.Trips.Count == 0 || c.Count == 0 || none.Totals.Parked.Intervals == 0 ||
			c.Cost.Entered == 0 || c.Cost.Unknown == 0 || c.Orphaned.Count == 0 {
			t.Fatalf("seed %d: empty history %+v", seed, none.Totals)
		}
		for _, bucket := range []Bucket{BucketDay, BucketWeek, BucketMonth} {
			s, err := Summarize(trips, charges, costs, nil, from, to, paris, bucket, DefaultParams())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.Totals, none.Totals) {
				t.Errorf("seed %d, %s: totals = %+v, want %+v", seed, bucket, s.Totals, none.Totals)
			}
			var sum Stats
			for i, b := range s.Buckets {
				if i > 0 && !b.Start.Equal(s.Buckets[i-1].End) {
					t.Errorf("seed %d, %s: gap before %s", seed, bucket, b.Start)
				}
				addNumbers(reflect.ValueOf(&sum).Elem(), reflect.ValueOf(b.Stats))
			}
			checkSums(t, fmt.Sprintf("seed %d, %s", seed, bucket), reflect.ValueOf(sum), reflect.ValueOf(s.Totals))
		}
	}
}

var valueKind = reflect.TypeFor[Value[float64]]()

// addNumbers adds the counts, sums and durations of b to a, leaving averages out.
func addNumbers(a, b reflect.Value) {
	switch {
	case a.Type() == valueKind:
	case a.Kind() == reflect.Struct:
		for i := range a.NumField() {
			addNumbers(a.Field(i), b.Field(i))
		}
	case a.CanInt():
		a.SetInt(a.Int() + b.Int())
	case a.CanFloat():
		a.SetFloat(a.Float() + b.Float())
	}
}

// checkSums compares the counts, sums and durations of got and want, floats within
// rounding.
func checkSums(t *testing.T, path string, got, want reflect.Value) {
	t.Helper()
	switch {
	case got.Type() == valueKind:
	case got.Kind() == reflect.Struct:
		for i := range got.NumField() {
			checkSums(t, path+"."+got.Type().Field(i).Name, got.Field(i), want.Field(i))
		}
	case got.CanInt():
		if got.Int() != want.Int() {
			t.Errorf("%s: sum of intervals %d, total %d", path, got.Int(), want.Int())
		}
	case got.CanFloat():
		if math.Abs(got.Float()-want.Float()) > 1e-6 {
			t.Errorf("%s: sum of intervals %g, total %g", path, got.Float(), want.Float())
		}
	}
}
