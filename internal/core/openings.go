package core

import "time"

// Open tells whether the vehicle stands open, by what it told: unlocked, or a door, the
// hood, the tailgate, a window or the sunroof open or ajar. The tank lid is left out: an
// electric car's may be its charge port's, open while it charges. What it does not tell
// is not open.
func (s Snapshot) Open() bool {
	d, w := s.Doors.V, s.Windows.V
	if s.Doors.OK && d.Locked.OK && !d.Locked.V {
		return true
	}
	var parts []Value[Opening]
	if s.Doors.OK {
		parts = append(parts, d.FrontLeft, d.FrontRight, d.RearLeft, d.RearRight, d.Hood, d.Tailgate)
	}
	if s.Windows.OK {
		parts = append(parts, w.FrontLeft, w.FrontRight, w.RearLeft, w.RearRight, w.Sunroof)
	}
	for _, p := range parts {
		if p.OK && p.V != Closed {
			return true
		}
	}
	return false
}

// OpenSince returns since when the records see the vehicle parked (its engine read
// stopped) and open (Snapshot.Open): the first reading of their latest run that saw it
// so, every reading since seeing it so; zero when the latest does not. A lower bound: it
// may have been left so between two readings, or before the first record given.
func OpenSince(records []Record) time.Time { return OpenSinceFresh(records, 0) }

// OpenSinceFresh is OpenSince, but the doors and the windows read more than maxAge before
// a reading count as unknown at it: a run starts only from fresh readings, never from a
// stale window that showed open. Zero: no limit.
func OpenSinceFresh(records []Record, maxAge time.Duration) time.Time {
	var (
		st    state
		since time.Time
	)
	standing := func(s Snapshot) bool { return s.Engine.OK && s.Engine.V == EngineStopped && s.Open() }
	for _, p := range points(records) {
		// What the run rested on may have gone stale before this reading: it ended then, and
		// a run that this reading starts again starts here.
		if maxAge > 0 && !since.IsZero() && !standing(withoutStale(st.snap, p.at.Add(-maxAge), st.seenAt)) {
			since = time.Time{}
		}
		st.apply(p)
		s := st.snap
		if maxAge > 0 {
			s = withoutStale(s, p.at.Add(-maxAge), st.seenAt)
		}
		switch {
		case !standing(s):
			since = time.Time{}
		case since.IsZero():
			since = p.at
		}
	}
	return since
}

// withoutStale is s with the doors and the windows read before from taken as unknown;
// seen gives when a field was last read.
func withoutStale(s Snapshot, from time.Time, seen func(Field) time.Time) Snapshot {
	if s.Doors.OK && seen(FieldDoors).Before(from) {
		s.Doors = Value[Doors]{}
	}
	if s.Windows.OK && seen(FieldWindows).Before(from) {
		s.Windows = Value[Windows]{}
	}
	return s
}

// OpenReadSince tells whether the vehicle stands open by the doors and windows read at or after
// from alone: a part read before is unknown, and shows nothing.
func (c Current) OpenReadSince(from time.Time) bool {
	return withoutStale(c.Snapshot, from, c.st.seenAt).Open()
}
