package core

import "time"

// Current is the latest known value of every field, with when it was read.
type Current struct {
	Snapshot Snapshot
	st       state
}

// Latest returns the current state of a vehicle from its records, sorted or not: the
// latest record of each kind is enough.
func Latest(records []Record) Current {
	var st state
	for _, p := range points(records) {
		st.apply(p)
	}
	st.forgetBelatedSleep()
	return Current{Snapshot: st.snap, st: st}
}

// forgetBelatedSleep forgets a vehicle told asleep or offline when a record fetched
// after that state's last reading says it answered since: it was awake then. A source
// that streams never tells the sleep state, so the last one a polled source told would
// otherwise stand for good.
func (s *state) forgetBelatedSleep() {
	if s.snap.Covers&FieldSleep == 0 || !s.snap.Sleep.OK || s.snap.Sleep.V == Awake {
		return
	}
	told := s.seenAt(FieldSleep)
	for c := s.snap.Covers &^ FieldSleep; c != 0; c &= c - 1 {
		if s.row[c.index()].After(told) {
			s.snap.Covers &^= FieldSleep
			s.snap.Sleep = Value[SleepState]{}
			s.seen[FieldSleep.index()], s.row[FieldSleep.index()] = time.Time{}, time.Time{}
			return
		}
	}
}

// Read returns when the current value of f was first read (FetchedAt of its record)
// and last read unchanged (its CheckedAt). Both are zero if f was never read.
func (c Current) Read(f Field) (fetchedAt, checkedAt time.Time) {
	return c.st.rowAt(f), c.st.seenAt(f)
}

// CheckedAt returns the latest reading of any field; zero if there is none.
func (c Current) CheckedAt() time.Time {
	var last time.Time
	for _, t := range c.st.seen {
		last = later(last, t)
	}
	return last
}
