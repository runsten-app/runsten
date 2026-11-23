package core

import (
	"testing"
	"time"
)

func TestBatteryLow(t *testing.T) {
	parked := func(soc float64, st ChargingStatus) Snapshot {
		s := nrg(soc, st)
		s.Covers |= FieldEngine
		s.Engine = Some(EngineStopped, time.Time{})
		return s
	}
	driving := parked(15, ChargingIdle)
	driving.Engine = Some(EngineRunning, time.Time{})
	noEngine := nrg(15, ChargingIdle)
	noCharging := parked(15, ChargingIdle)
	noCharging.Charging = Value[ChargingStatus]{}
	noCharging.Connection = Value[Connection]{}
	noCharging.Covers &^= FieldCharging | FieldConnection
	unplugged := noCharging
	unplugged.Connection = Some(Disconnected, time.Time{})

	for _, tt := range []struct {
		name string
		s    Snapshot
		want Value[bool]
	}{
		{"no state of charge", Snapshot{}, Value[bool]{}},
		{"under, parked, idle", parked(19, ChargingIdle), known(true)},
		{"at the threshold", parked(20, ChargingIdle), known(false)},
		{"above, engine unknown", nrg(60, ChargingIdle), known(false)},
		{"under, charging", parked(15, ChargingActive), known(false)},
		{"under, plugged in and done", parked(15, ChargingDone), known(true)},
		{"under, driving", driving, known(false)},
		{"under, engine not told", noEngine, Value[bool]{}},
		{"under, neither charging nor cable told", noCharging, Value[bool]{}},
		{"under, cable told disconnected", unplugged, known(true)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := BatteryLow(tt.s, 20)
			if got.OK != tt.want.OK || got.V != tt.want.V {
				t.Errorf("BatteryLow = %+v, want %+v", got, tt.want)
			}
		})
	}
	if got := BatteryLow(parked(24, ChargingIdle), 25); !got.OK || !got.V {
		t.Errorf("24 under 25 = %+v, want low", got)
	}
}

func soc(v float64) Snapshot { return Snapshot{Covers: FieldSoC, SoC: Some(v, time.Time{})} }

func TestLowSince(t *testing.T) {
	for _, tt := range []struct {
		name    string
		records []Record
		want    time.Time
	}{
		{"no record", nil, time.Time{}},
		{"above all along", []Record{rec(0, 10, soc(40)), rec(20, 30, soc(25))}, time.Time{}},
		{"under since its first reading under", []Record{rec(0, 10, soc(40)), rec(20, 30, soc(19)), rec(40, 50, soc(18))}, m(20)},
		{"a charge under the threshold goes on the episode", []Record{rec(0, 10, soc(19)), rec(20, 30, soc(15)), rec(40, 50, soc(17))}, m(0)},
		{"a charge above it ends the episode", []Record{rec(0, 10, soc(15)), rec(20, 30, soc(60)), rec(40, 50, soc(19))}, m(40)},
		{"back above: no episode", []Record{rec(0, 10, soc(15)), rec(20, 30, soc(60))}, time.Time{}},
		{"a reading of another value changes nothing", []Record{rec(0, 10, soc(15)), rec(20, 30, engine(EngineStopped))}, m(0)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := LowSince(tt.records, 20); !got.Equal(tt.want) {
				t.Errorf("LowSince = %s, want %s", got, tt.want)
			}
		})
	}
}

var (
	nightHome = Place{ID: "home", Name: "Home", Position: home, RadiusM: 100, Location: paris}
	nightWork = Place{ID: "work", Name: "Work", Position: work, RadiusM: 100, Location: time.UTC}
)

// at is a Paris wall-clock time of November 2026 (UTC+1), day d at hh:mm.
func at(d, hh, mm int) time.Time { return time.Date(2026, 11, d, hh, mm, 0, 0, paris) }

// night is a reading of the energy state at a time, as one record.
func night(when time.Time, s Snapshot) Record {
	return Record{FetchedAt: when, CheckedAt: when, Snapshot: s}
}

// plugged is an energy reading: soc, status, cable connected, a limit of 80.
func plugged(v float64, st ChargingStatus) Snapshot {
	s := nrg(v, st)
	s.Connection = Some(Connected, time.Time{})
	return s
}

func parkedAt(p Position, when time.Time) Record { return night(when, loc(p, time.Time{})) }

func TestNotCharged(t *testing.T) {
	places := []Place{nightHome, nightWork}
	arrived := parkedAt(home, at(22, 17, 30))
	now := at(23, 7, 5)
	for _, tt := range []struct {
		name    string
		records []Record
		now     time.Time
		// ok, not: the judgement's OK and NotCharged.
		ok, not bool
		soc     float64
	}{
		{
			"plugged at 19:00, the state of charge never rose",
			[]Record{arrived, night(at(22, 19, 0), plugged(40, ChargingScheduled)), night(at(23, 6, 50), plugged(40, ChargingScheduled))},
			now, true, true, 40,
		},
		{
			"plugged before the evening and left: the first reading of the evening counts",
			[]Record{arrived, night(at(22, 16, 0), plugged(40, ChargingDone)), night(at(23, 6, 0), plugged(40, ChargingDone))},
			now, true, true, 40,
		},
		{
			"it charged and stopped at its limit",
			[]Record{arrived, night(at(22, 19, 0), plugged(40, ChargingActive)), night(at(23, 2, 0), plugged(80, ChargingDone)), night(at(23, 6, 30), plugged(80, ChargingDone))},
			now, true, false, 40,
		},
		{
			"it charged a point only",
			[]Record{arrived, night(at(22, 19, 0), plugged(40, ChargingScheduled)), night(at(23, 1, 0), plugged(41, ChargingScheduled)), night(at(23, 6, 30), plugged(41, ChargingScheduled))},
			now, true, false, 40,
		},
		{
			"a charge in progress with the state of charge not read higher",
			[]Record{arrived, night(at(22, 19, 0), plugged(40, ChargingScheduled)), night(at(23, 3, 0), plugged(40, ChargingActive)), night(at(23, 6, 30), plugged(40, ChargingScheduled))},
			now, true, false, 40,
		},
		{
			"already at its limit: nothing to charge",
			[]Record{arrived, night(at(22, 19, 0), plugged(79, ChargingDone)), night(at(23, 6, 30), plugged(79, ChargingDone))},
			now, true, false, 79,
		},
		{
			"unplugged in the night",
			[]Record{arrived, night(at(22, 19, 0), plugged(40, ChargingScheduled)), night(at(23, 3, 0), nrg(40, ChargingIdle)), night(at(23, 6, 30), nrg(40, ChargingIdle))},
			now, true, false, 40,
		},
		{
			"plugged after 02:00 only",
			[]Record{arrived, night(at(22, 19, 0), nrg(40, ChargingIdle)), night(at(23, 3, 0), plugged(40, ChargingScheduled)), night(at(23, 6, 30), plugged(40, ChargingScheduled))},
			now, true, false, 0,
		},
		{
			"plugged away from the places",
			[]Record{parkedAt(Position{Lat: 48.85, Lon: 2.35}, at(22, 17, 30)), night(at(22, 19, 0), plugged(40, ChargingScheduled)), night(at(23, 6, 30), plugged(40, ChargingScheduled))},
			now, true, false, 0,
		},
		{
			"the last reading is older than two hours before the morning",
			[]Record{arrived, night(at(22, 19, 0), plugged(40, ChargingScheduled)), night(at(23, 4, 0), plugged(40, ChargingScheduled))},
			now, false, false, 0,
		},
		{
			"before this morning's check, last night is judged",
			[]Record{parkedAt(home, at(21, 17, 30)), night(at(21, 19, 0), plugged(40, ChargingScheduled)), night(at(22, 6, 55), plugged(40, ChargingScheduled)), night(at(22, 19, 0), plugged(40, ChargingScheduled))},
			at(23, 6, 59), true, true, 40,
		},
		{
			"no state of charge limit read: the evening is not one",
			[]Record{arrived, night(at(22, 19, 0), Snapshot{Covers: energyFields, SoC: Some(40.0, time.Time{}), Connection: Some(Connected, time.Time{})}), night(at(23, 6, 30), plugged(40, ChargingScheduled))},
			now, true, false, 0,
		},
		{
			"the cable never read",
			[]Record{arrived, night(at(23, 6, 30), soc(40))},
			now, false, false, 0,
		},
		{
			"readings after the morning are left out",
			[]Record{arrived, night(at(22, 19, 0), plugged(40, ChargingScheduled)), night(at(23, 6, 30), plugged(40, ChargingScheduled)), night(at(23, 7, 3), plugged(50, ChargingActive))},
			now, true, true, 40,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := NotCharged(tt.records, places, tt.now)
			if got.OK != tt.ok || got.V.NotCharged != tt.not {
				t.Fatalf("NotCharged = %+v, want ok=%v not=%v", got, tt.ok, tt.not)
			}
			if got.OK && got.V.EveningSoC != tt.soc {
				t.Errorf("EveningSoC = %v, want %v", got.V.EveningSoC, tt.soc)
			}
		})
	}
	if got := NotCharged(nil, nil, now); got.OK {
		t.Errorf("without a place: %+v, want unknown", got)
	}
}

// TestNotChargedZones: the evening and the morning are those of the place's zone, daylight
// saving time included.
func TestNotChargedZones(t *testing.T) {
	// Summer: Paris is UTC+2, so 07:00 local is 05:00 UTC.
	summer := func(d, hh, mm int) time.Time { return time.Date(2026, 7, d, hh, mm, 0, 0, paris) }
	records := []Record{
		parkedAt(home, summer(22, 17, 30)), night(summer(22, 19, 0), plugged(40, ChargingScheduled)),
		night(summer(23, 6, 30), plugged(40, ChargingScheduled)),
	}
	if got := NotCharged(records, []Place{nightHome}, summer(23, 7, 0).UTC()); !got.OK || !got.V.NotCharged {
		t.Errorf("at 07:00 local = %+v, want not charged", got)
	}
	if got := NotCharged(records, []Place{nightHome}, summer(23, 6, 59).UTC()); got.OK && got.V.DateKey() != "2026-07-22" {
		t.Errorf("at 06:59 local = %+v, want the night before's", got)
	}
	// The same instants in a place of UTC: 07:00 there comes two hours later.
	utc := nightHome
	utc.Location = time.UTC
	if got := NotCharged(records, []Place{utc}, summer(23, 7, 0)); got.OK && got.V.DateKey() == "2026-07-23" {
		t.Errorf("07:00 in Paris is 05:00 UTC, before the morning check there: %+v", got)
	}
	// The day clocks go back (2026-10-25), the morning stays at 07:00 on the wall.
	d := time.Date(2026, 10, 25, 0, 0, 0, 0, paris)
	if got := clock(d, MorningAt); got.Hour() != 7 || got.UTC().Hour() != 6 {
		t.Errorf("morning of the change = %s, want 07:00 +01:00", got)
	}
	if got := clock(d, NightUntil); got.UTC().Hour() != 1 {
		t.Errorf("02:00 of the change = %s", got)
	}
}

func TestNightStanding(t *testing.T) {
	n := NightCheck{Date: time.Date(2026, 11, 23, 0, 0, 0, 0, paris), NotCharged: true, EveningSoC: 40}
	cur := func(s Snapshot) Current { return Latest([]Record{night(at(23, 8, 0), s)}) }
	for _, tt := range []struct {
		name string
		n    NightCheck
		c    Current
		at   time.Time
		want Value[bool]
	}{
		{"still plugged, not charged", n, cur(plugged(40, ChargingScheduled)), at(23, 8, 0), known(true)},
		{"unplugged since", n, cur(nrg(40, ChargingIdle)), at(23, 8, 0), known(false)},
		{"charging since", n, cur(plugged(40, ChargingActive)), at(23, 8, 0), known(false)},
		{"state of charge rose", n, cur(plugged(45, ChargingDone)), at(23, 8, 0), known(false)},
		{"the evening after", n, cur(plugged(40, ChargingScheduled)), at(23, 18, 0), known(false)},
		{"cable unknown", n, cur(soc(40)), at(23, 8, 0), Value[bool]{}},
		{"the night was fine", NightCheck{Date: n.Date}, cur(plugged(40, ChargingScheduled)), at(23, 8, 0), known(false)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.n.Standing(tt.c, tt.at)
			if got.OK != tt.want.OK || got.V != tt.want.V {
				t.Errorf("Standing = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestNightStandingDST: the evening ends at 18:00 on the wall clock the day the clocks change.
func TestNightStandingDST(t *testing.T) {
	// 2026-10-25: 25 hours, the clocks go back; midnight plus 18 elapsed hours is 17:00.
	n := NightCheck{Date: time.Date(2026, 10, 25, 0, 0, 0, 0, paris), NotCharged: true, EveningSoC: 40}
	cur := func(when time.Time) Current { return Latest([]Record{night(when, plugged(40, ChargingScheduled))}) }
	for _, c := range []struct {
		h, m int
		want bool
	}{{17, 30, true}, {17, 59, true}, {18, 0, false}} {
		at := time.Date(2026, 10, 25, c.h, c.m, 0, 0, paris)
		if got := n.Standing(cur(at), at); !got.OK || got.V != c.want {
			t.Errorf("at %02d:%02d: %+v, want %v", c.h, c.m, got, c.want)
		}
	}
}

// TestNotChargedFirstReadingCharging: a charge in progress at the reading that finds the
// cable plugged in is one delivering, even if the state of charge never rises after.
func TestNotChargedFirstReadingCharging(t *testing.T) {
	places := []Place{nightHome}
	records := []Record{
		parkedAt(home, at(22, 17, 30)), night(at(22, 19, 0), plugged(40, ChargingActive)),
		night(at(23, 1, 0), plugged(40, ChargingScheduled)), night(at(23, 6, 30), plugged(40, ChargingScheduled)),
	}
	if got := NotCharged(records, places, at(23, 7, 5)); !got.OK || got.V.NotCharged {
		t.Errorf("NotCharged = %+v, want charged", got)
	}
}
