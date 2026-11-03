package core

import (
	"slices"
	"sort"
	"time"
)

// SleepSpan is a stretch of time a response told the vehicle's sleep state over: from
// its fetch to its latest confirmation. Nothing is known between two responses.
type SleepSpan struct {
	From, To time.Time
	// State is the one the response told.
	State SleepState
	// Asleep is the vehicle asleep or out of reach (Offline): either way it is not read.
	// Assumption: an Offline car drains as little as one asleep; Tesla does not always
	// tell one asleep from one awake without a network.
	Asleep bool
}

// SleepSpans returns the sleep states the records tell, oldest first: a record that
// covers FieldSleep without a known state tells nothing.
func SleepSpans(records []Record) []SleepSpan {
	var out []SleepSpan
	for _, r := range records {
		if r.Snapshot.Covers&FieldSleep == 0 || !r.Snapshot.Sleep.OK {
			continue
		}
		st := r.Snapshot.Sleep.V
		out = append(out, SleepSpan{From: r.FetchedAt, To: r.CheckedAt, State: st, Asleep: st != Awake})
	}
	return sortedSpans(out)
}

// SleepStretches lays the sorted spans out over [from, to]: each cut at its edges, and
// joined to the one before it when they tell the same state and no more than gap
// separates them, as the readings of a run are. Between two stretches nothing was read.
func SleepStretches(spans []SleepSpan, from, to time.Time, gap time.Duration) []SleepSpan {
	var out []SleepSpan
	for _, s := range spans {
		s.From, s.To = later(s.From, from), earlier(s.To, to)
		if !s.To.After(s.From) {
			continue
		}
		if n := len(out); n > 0 && out[n-1].State == s.State && s.From.Sub(out[n-1].To) <= gap {
			out[n-1].To = s.To
			continue
		}
		out = append(out, s)
	}
	return out
}

// sortedSpans sorts spans by their start and cuts each one where the previous ends: two
// sources telling the same time count it once.
func sortedSpans(spans []SleepSpan) []SleepSpan {
	spans = slices.Clone(spans)
	slices.SortStableFunc(spans, func(a, b SleepSpan) int { return a.From.Compare(b.From) })
	out := spans[:0]
	var end time.Time
	for _, s := range spans {
		if s.From.Before(end) {
			s.From = end
		}
		if !s.To.After(s.From) {
			continue
		}
		out = append(out, s)
		end = s.To
	}
	return out
}

// sleepWithin returns how much of [from, to] the sorted spans tell the sleep state of,
// and how much of it they tell the vehicle asleep.
func sleepWithin(spans []SleepSpan, from, to time.Time) (read, asleep time.Duration) {
	for i := sort.Search(len(spans), func(i int) bool { return spans[i].To.After(from) }); i < len(spans) && spans[i].From.Before(to); i++ {
		d := earlier(spans[i].To, to).Sub(later(spans[i].From, from))
		read += d
		if spans[i].Asleep {
			asleep += d
		}
	}
	return read, asleep
}
