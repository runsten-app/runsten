package core

import (
	"testing"
	"time"
)

// doors is a response of the doors: locked or not, the tailgate as given.
func doors(locked bool, tailgate Opening) Snapshot {
	return Snapshot{Covers: FieldDoors, Doors: Some(Doors{
		Locked: Some(locked, time.Time{}), Tailgate: Some(tailgate, time.Time{}),
	}, time.Time{})}
}

func windows(front Opening) Snapshot {
	return Snapshot{Covers: FieldWindows, Windows: Some(Windows{FrontLeft: Some(front, time.Time{})}, time.Time{})}
}

func TestOpen(t *testing.T) {
	for _, tt := range []struct {
		name string
		s    Snapshot
		want bool
	}{
		{"nothing told", Snapshot{}, false},
		{"locked, closed", doors(true, Closed), false},
		{"unlocked", doors(false, Closed), true},
		{"the tailgate ajar", doors(true, Ajar), true},
		{"a window open", windows(Open), true},
		{"the lock not told, closed", Snapshot{Covers: FieldDoors, Doors: Some(Doors{Hood: Some(Closed, time.Time{})}, time.Time{})}, false},
		{"the tank lid open alone", Snapshot{Covers: FieldDoors, Doors: Some(Doors{
			Locked: Some(true, time.Time{}), TankLid: Some(Open, time.Time{}),
		}, time.Time{})}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.Open(); got != tt.want {
				t.Errorf("Open() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenSince(t *testing.T) {
	for _, tt := range []struct {
		name    string
		records []Record
		want    time.Time
	}{
		{"no record", nil, time.Time{}},
		{
			"parked and unlocked since its first reading so",
			[]Record{rec(0, 60, engine(EngineStopped)), rec(10, 10, doors(true, Closed)), rec(20, 50, doors(false, Closed))},
			m(20),
		},
		{
			// Open all along, another part opened: the run goes on.
			"a window opened besides",
			[]Record{rec(0, 60, engine(EngineStopped)), rec(10, 30, doors(false, Closed)), rec(20, 60, windows(Open)), rec(40, 60, doors(false, Ajar))},
			m(10),
		},
		{"closed again", []Record{rec(0, 60, engine(EngineStopped)), rec(10, 20, doors(false, Closed)), rec(30, 40, doors(true, Closed))}, time.Time{}},
		{"running", []Record{rec(0, 60, engine(EngineRunning)), rec(10, 50, doors(false, Closed))}, time.Time{}},
		{"the engine not told", []Record{rec(10, 50, doors(false, Closed))}, time.Time{}},
		{
			// Driven between two readings of the doors: the run starts again once parked.
			"driven in between",
			[]Record{rec(0, 10, engine(EngineStopped)), rec(5, 60, doors(false, Closed)), rec(20, 30, engine(EngineRunning)), rec(40, 60, engine(EngineStopped))},
			m(40),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := OpenSince(tt.records); !got.Equal(tt.want) {
				t.Errorf("OpenSince = %s, want %s", got, tt.want)
			}
		})
	}
}
