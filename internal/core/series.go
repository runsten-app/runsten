package core

import (
	"sort"
	"time"
)

// Reading is the energy state of a vehicle as one response gave it: at the record's
// FetchedAt, and again, unchanged, at its CheckedAt.
type Reading struct {
	At      time.Time
	SoC     Value[float64] // %
	RangeKm Value[float64]
	PowerW  Value[float64] // charging power
}

// Runs lays the readings of the records within [from, to], both included, out in runs,
// oldest first: a new run starts where more than gap separates a record from the one
// before it, nothing being read in between. A record's two readings stay in one run
// whatever their distance: identical responses were read between them, so its value
// held from one to the other, and a record across from or to gives it at that edge
// instead. Only the records reporting the state of charge count: those of the energy
// state. Nothing is interpolated: every value is one the vehicle gave.
func Runs(records []Record, from, to time.Time, gap time.Duration) [][]Reading {
	var spans []readSpan
	for i, r := range records {
		if r.Snapshot.Covers&FieldSoC != 0 {
			spans = append(spans, readSpan{fetched: r.FetchedAt, checked: r.CheckedAt, i: i})
		}
	}
	return layOut(spans, from, to, gap, func(i int, at time.Time) Reading {
		s := records[i].Snapshot
		return Reading{At: at, SoC: s.SoC, RangeKm: s.RangeKm, PowerW: s.PowerW}
	})
}

// readSpan is a record's two readings, at its FetchedAt and its CheckedAt; i identifies it.
type readSpan struct {
	fetched, checked time.Time
	i                int
}

// layOut lays the readings of the spans within [from, to], both included, out in runs,
// oldest first: a new run starts where more than gap separates a span from the one
// before it, nothing being read in between. A span's two readings stay in one run
// whatever their distance: identical responses were read between them, so its value
// held from one to the other, and a span across from or to gives it at that edge
// instead. reading gives the span's value at an instant.
func layOut[T any](spans []readSpan, from, to time.Time, gap time.Duration, reading func(i int, at time.Time) T) [][]T {
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].fetched.Before(spans[j].fetched) })
	var runs [][]T
	var last time.Time // the previous span's last reading
	for _, s := range spans {
		f, c := s.fetched.UTC(), s.checked.UTC()
		if c.Before(from) || f.After(to) {
			continue
		}
		if runs == nil || f.Sub(last) > gap {
			runs = append(runs, nil)
		}
		first, end := later(f, from), earlier(c, to)
		runs[len(runs)-1] = append(runs[len(runs)-1], reading(s.i, first))
		if end.After(first) {
			runs[len(runs)-1] = append(runs[len(runs)-1], reading(s.i, end))
		}
		last = c
	}
	return runs
}

func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
