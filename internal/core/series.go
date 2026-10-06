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
	energy := make([]Record, 0, len(records))
	for _, r := range records {
		if r.Snapshot.Covers&FieldSoC != 0 {
			energy = append(energy, r)
		}
	}
	sort.SliceStable(energy, func(i, j int) bool { return energy[i].FetchedAt.Before(energy[j].FetchedAt) })
	var runs [][]Reading
	var last time.Time // the previous record's last reading
	for _, r := range energy {
		f, c := r.FetchedAt.UTC(), r.CheckedAt.UTC()
		if c.Before(from) || f.After(to) {
			continue
		}
		if runs == nil || f.Sub(last) > gap {
			runs = append(runs, nil)
		}
		s := r.Snapshot
		reading := func(at time.Time) Reading {
			return Reading{At: at, SoC: s.SoC, RangeKm: s.RangeKm, PowerW: s.PowerW}
		}
		first, end := later(f, from), earlier(c, to)
		runs[len(runs)-1] = append(runs[len(runs)-1], reading(first))
		if end.After(first) {
			runs[len(runs)-1] = append(runs[len(runs)-1], reading(end))
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
