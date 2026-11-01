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
func OpenSince(records []Record) time.Time {
	var (
		st    state
		since time.Time
	)
	for _, p := range points(records) {
		st.apply(p)
		s := st.snap
		switch {
		case !s.Engine.OK || s.Engine.V != EngineStopped || !s.Open():
			since = time.Time{}
		case since.IsZero():
			since = p.at
		}
	}
	return since
}
