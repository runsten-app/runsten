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
	return Current{Snapshot: st.snap, st: st}
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
