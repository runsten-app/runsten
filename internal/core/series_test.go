package core

import (
	"reflect"
	"testing"
	"time"
)

func TestRuns(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	energy := func(f, c int, soc float64, power Value[float64]) Record {
		return Record{FetchedAt: at(f), CheckedAt: at(c), Snapshot: Snapshot{
			Covers: FieldSoC | FieldRange | FieldPower, SoC: Some(soc, at(f)), RangeKm: Some(soc*4, at(f)), PowerW: power,
		}}
	}
	read := func(m int, soc float64, power Value[float64]) Reading {
		return Reading{At: at(m), SoC: Some(soc, at(m)), RangeKm: Some(soc*4, at(m)), PowerW: power}
	}
	// read gives the vehicle's timestamps of the record fetched at f.
	readOf := func(m, f int, soc float64, power Value[float64]) Reading {
		r := read(m, soc, power)
		r.SoC.At, r.RangeKm.At = at(f), at(f)
		return r
	}
	none := Value[float64]{}
	kw := func(w float64) Value[float64] { return Value[float64]{V: w, OK: true} }
	odometer := Record{FetchedAt: at(15), CheckedAt: at(15), Snapshot: Snapshot{Covers: FieldOdometer, OdometerKm: Some(12400.0, at(15))}}

	tests := []struct {
		name     string
		records  []Record
		from, to int
		want     [][]Reading
	}{
		{"nothing", nil, 0, 60, nil},
		{
			// Unsorted, with another endpoint's record among them; each response gives
			// its two readings, and one read once a single one.
			"both ends of each response",
			[]Record{energy(20, 20, 51, kw(7400)), odometer, energy(0, 10, 50, none)},
			0, 60,
			[][]Reading{{read(0, 50, none), readOf(10, 0, 50, none), read(20, 51, kw(7400))}},
		},
		{
			// More than the gap between two responses (92 minutes, not 90): nothing read
			// in between. Hours between the two readings of one response are no gap.
			"a gap between responses",
			[]Record{energy(0, 300, 50, none), energy(390, 390, 49, none), energy(482, 482, 48, none)},
			0, 600,
			[][]Reading{{read(0, 50, none), readOf(300, 0, 50, none), read(390, 49, none)}, {read(482, 48, none)}},
		},
		{
			// Across the edges, a response gives its value there: it held all along. One
			// over before from, or starting after to, gives none.
			"clipped to the window",
			[]Record{energy(0, 5, 49, none), energy(10, 40, 50, none), energy(45, 45, 51, none), energy(50, 80, 52, none), energy(90, 90, 53, none)},
			20, 60,
			[][]Reading{{readOf(20, 10, 50, none), readOf(40, 10, 50, none), read(45, 51, none), readOf(50, 50, 52, none), readOf(60, 50, 52, none)}},
		},
		{
			"one response over the whole window",
			[]Record{energy(0, 600, 50, none)},
			100, 200,
			[][]Reading{{readOf(100, 0, 50, none), readOf(200, 0, 50, none)}},
		},
		{"to included", []Record{energy(60, 60, 50, none)}, 0, 60, [][]Reading{{read(60, 50, none)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Runs(tt.records, at(tt.from), at(tt.to), 90*time.Minute)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
