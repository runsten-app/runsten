package demo

import (
	"math"
	"testing"
	"time"

	"runsten/internal/catalog"
	"runsten/internal/core"
)

func mustNew(t *testing.T) *Demo {
	t.Helper()
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func read(t *testing.T, c *Cursor, at time.Time) Reading {
	t.Helper()
	r, err := c.ReadingAt(at)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestSameInstantSameReading: the state at an instant depends on that instant alone, not on
// the simulation that reads it, nor on what it read before.
func TestSameInstantSameReading(t *testing.T) {
	d := mustNew(t)
	at := Origin().Add(31*Week + sat + 9*time.Hour + 17*time.Minute + 23*time.Second + 700*time.Millisecond)
	a := read(t, d.Cursor(), at)

	warm := d.Cursor() // one that has read other weeks and instants, back and forth
	for _, off := range []time.Duration{3 * Week, 40 * Week, 31*Week + 5*time.Hour, 31*Week + sun} {
		read(t, warm, Origin().Add(off))
	}
	if b := read(t, warm, at); a != b {
		t.Errorf("a cursor that read other instants first: %+v, want %+v", b, a)
	}
	// Forward in small steps to the same instant.
	step := d.Cursor()
	for off := 31*Week + sat; off < at.Sub(Origin()); off += 53 * time.Second {
		read(t, step, Origin().Add(off))
	}
	if got := read(t, step, at); got != a {
		t.Errorf("walked to it: %+v, want %+v", got, a)
	}
}

// TestPluggedOnArriving: at the very second a drive ends, the vehicle is plugged in and
// charging, never parked and unplugged for a moment.
func TestPluggedOnArriving(t *testing.T) {
	c := mustNew(t).Cursor()
	for _, off := range []time.Duration{mon + hm(16, 17), tue + hm(16, 17) + 400*time.Millisecond, sat + hm(8, 35), sun + hm(15, 30)} {
		if r := read(t, c, Origin().Add(3*Week+off)); r.Driving || !r.Plugged || r.Charging != "charging" {
			t.Errorf("at %v: %+v", off, r)
		}
	}
}

// TestWeeksRepeat: a week later the vehicle is in the same state but for the odometer,
// which grew by what a week drives, and the weather.
func TestWeeksRepeat(t *testing.T) {
	d := mustNew(t)
	c := d.Cursor()
	for off := time.Duration(0); off < Week; off += 7*time.Minute + 11*time.Second {
		a := read(t, c, Origin().Add(10*Week+off))
		b := read(t, c, Origin().Add(11*Week+off))
		if math.Abs(a.OdometerKm+d.WeekKm()-b.OdometerKm) > 0.11 {
			t.Fatalf("offset %v: odometer %v then %v, a week of %v km", off, a.OdometerKm, b.OdometerKm, d.WeekKm())
		}
		a.At, b.At, a.OutsideTempC, b.OutsideTempC, a.OdometerKm, b.OdometerKm = time.Time{}, time.Time{}, 0, 0, 0, 0
		if a != b {
			t.Fatalf("offset %v: %+v, a week later %+v", off, a, b)
		}
	}
}

// TestWeekIsSane walks a week: the state of charge stays in bounds and starts the week as
// it ends, the vehicle drives and charges as the timetable says, the odometer never goes
// back, the fast charge is a DC one and the home charges AC, and the vehicle ends where it
// starts.
func TestWeekIsSane(t *testing.T) {
	d := mustNew(t)
	c := d.Cursor()
	start := Origin().Add(20 * Week)
	const every = 30 * time.Second
	var prev, first, last Reading
	var driving, ac, dc time.Duration
	minSoC, maxSoC := 100.0, 0.0
	for off := time.Duration(0); off < Week; off += every {
		r := read(t, c, start.Add(off))
		if off == 0 {
			first = r
		}
		last = r
		minSoC, maxSoC = math.Min(minSoC, r.SoC), math.Max(maxSoC, r.SoC)
		switch {
		case r.SoC < 0 || r.SoC > 100 || r.RangeKm < 0:
			t.Fatalf("offset %v: %+v out of bounds", off, r)
		case r.OdometerKm < prev.OdometerKm:
			t.Fatalf("offset %v: odometer went back, %v to %v", off, prev.OdometerKm, r.OdometerKm)
		case r.Driving && r.Plugged:
			t.Fatalf("offset %v: plugged in while driving", off)
		case r.Charging == "charging" && (r.PowerKW <= 0 || !r.Plugged):
			t.Fatalf("offset %v: charging without power or plug: %+v", off, r)
		case r.Charging == "done" && r.SoC < chargeSoC-0.05 && prev.Charging == "charging":
			// The steps of the fast charge's taper follow each other: never a reading in between,
			// which the collector would follow by ten minutes without a read.
			t.Fatalf("offset %v: done at %v %% between two steps of a charge", off, r.SoC)
		case r.Charging != "charging" && r.PowerKW != 0:
			t.Fatalf("offset %v: power %v while not charging", off, r.PowerKW)
		}
		if r.Driving {
			driving += every
		}
		if r.Charging == "charging" {
			switch r.ChargeType {
			case "dc":
				dc += every
			case "ac":
				ac += every
			default:
				t.Fatalf("offset %v: charging with no type", off)
			}
		}
		prev = r
	}
	if driving < 3*time.Hour || driving > 12*time.Hour {
		t.Errorf("drives %v a week", driving)
	}
	if dc < 15*time.Minute || dc > 40*time.Minute {
		t.Errorf("fast charges %v a week, want the Saturday's alone", dc)
	}
	if ac < 3*time.Hour {
		t.Errorf("charges %v at home a week", ac)
	}
	if minSoC < 15 || maxSoC > chargeSoC+0.05 {
		t.Errorf("state of charge from %v to %v", minSoC, maxSoC)
	}
	if first.SoC != chargeSoC || first.Charging != "done" || !first.Plugged {
		t.Errorf("the week starts %+v, plugged in and charged", first)
	}
	// The last reading, just before the next Monday, is where and as the next week starts.
	if last.SoC != chargeSoC || last.Charging != "done" || !last.Plugged || last.Latitude != first.Latitude || last.Longitude != first.Longitude {
		t.Errorf("the week ends %+v, not as it starts %+v", last, first)
	}
	if got := last.OdometerKm - first.OdometerKm; math.Abs(got-d.WeekKm()) > 0.5 {
		t.Errorf("odometer grew %v over a week of %v km", got, d.WeekKm())
	}
}

// TestTimetableOnTime: the drives start at the time the timetable gives them, the vehicle
// parked and charged before; Friday evening's is not plugged in.
func TestTimetableOnTime(t *testing.T) {
	d := mustNew(t)
	c := d.Cursor()
	w := Origin().Add(5 * Week)
	for _, tc := range []struct {
		at      time.Duration
		driving bool
		soc     func(float64) bool
	}{
		{mon + hm(6, 39), false, func(s float64) bool { return s == chargeSoC }},
		{mon + hm(6, 41), true, func(s float64) bool { return s < chargeSoC }},
		{fri + hm(20, 0), false, func(s float64) bool { return s < chargeSoC-5 }},
		{sat + hm(6, 29), false, func(s float64) bool { return s < chargeSoC-5 }},
		{sat + hm(7, 30), true, func(s float64) bool { return s < 60 }},
		{sat + hm(9, 31), true, func(s float64) bool { return s > chargeSoC-1 }}, // charged on arriving
		{sat + hm(14, 0), false, func(s float64) bool { return s > 30 && s < chargeSoC }},
		{sun + hm(8, 0), false, func(s float64) bool { return s == chargeSoC }},
	} {
		r := read(t, c, w.Add(tc.at))
		if r.Driving != tc.driving || !tc.soc(r.SoC) {
			t.Errorf("at %v: driving %v, %v %%", tc.at, r.Driving, r.SoC)
		}
	}
	if r := read(t, c, w.Add(fri+hm(20, 0))); r.Plugged {
		t.Errorf("plugged in on Friday evening: %+v", r)
	}
}

// TestPositions: the vehicle is at home at the start, away from it on the way, and the
// track of a drive leaves the origin and reaches the destination.
func TestPositions(t *testing.T) {
	d := mustNew(t)
	c := d.Cursor()
	w := Origin().Add(2 * Week)
	pl := places()
	home, work := pl["home"], pl["work"]
	at := func(off time.Duration) Reading { return read(t, c, w.Add(off)) }
	if r := at(mon + hm(3, 0)); r.Latitude != round(home.lat, 5) || r.Longitude != round(home.lon, 5) {
		t.Errorf("at home: %+v", r)
	}
	mid := at(mon + hm(6, 54))
	if !mid.Driving || mid.Latitude == round(home.lat, 5) || mid.Latitude == round(work.lat, 5) {
		t.Errorf("on the way: %+v", mid)
	}
	if r := at(mon + hm(8, 0)); r.Latitude != round(work.lat, 5) || r.Longitude != round(work.lon, 5) {
		t.Errorf("at work: %+v", r)
	}
	if r := at(sat + hm(8, 50)); r.Latitude != round(pl["dinant"].lat, 5) || r.Charging != "charging" {
		t.Errorf("at the weekend's destination: %+v", r)
	}
}

// TestBeforeOrigin: an instant before the origin reads as the origin.
func TestBeforeOrigin(t *testing.T) {
	c := mustNew(t).Cursor()
	if a, b := read(t, c, Origin().Add(-30*24*time.Hour)), read(t, c, Origin()); a.OdometerKm != b.OdometerKm || a.SoC != b.SoC {
		t.Errorf("%+v, want %+v", a, b)
	}
}

// TestHistory: the pace follows the activity, instants are in order, a parked vehicle's
// fingerprints repeat, and the fingerprint ignores the instant alone.
func TestHistory(t *testing.T) {
	d := mustNew(t)
	to := Origin().Add(40*Week + 2*sat)
	got, err := d.History(to.Add(-HistoryDays*24*time.Hour), to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2000 || len(got) > 12000 {
		t.Fatalf("%d samples over %d days", len(got), HistoryDays)
	}
	same, active := 0, 0
	for i := 1; i < len(got); i++ {
		gap := got[i].At.Sub(got[i-1].At)
		if gap != ActiveEvery && gap != ParkedEvery {
			t.Fatalf("sample %d: %v after the previous one", i, gap)
		}
		if string(got[i].Hash) == string(got[i-1].Hash) {
			same++
		}
		if gap == ActiveEvery {
			active++
		}
	}
	if same < len(got)/3 || active == 0 {
		t.Errorf("%d of %d samples repeat the previous one, %d at the active pace", same, len(got), active)
	}
	r := read(t, d.Cursor(), to)
	raw, _ := Marshal(r)
	r.At = r.At.Add(time.Hour)
	later, _ := Marshal(r)
	a, _ := Fingerprint(raw)
	if b, _ := Fingerprint(later); string(a) != string(b) {
		t.Error("the fingerprint depends on the instant")
	}
	r.SoC++
	other, _ := Marshal(r)
	if c, _ := Fingerprint(other); string(a) == string(c) {
		t.Error("the fingerprint ignores the state of charge")
	}
	if _, err := Fingerprint([]byte(`{`)); err == nil {
		t.Error("garbage fingerprinted")
	}
}

// TestNormalize: the snapshot of a reading says what the vehicle does, and the model is
// the catalog's EX30 Single Motor Extended Range.
func TestNormalize(t *testing.T) {
	d := mustNew(t)
	c := d.Cursor()
	norm := Normalizer{}
	kind := norm.Kinds()[0]
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		at   time.Duration
		want func(core.Snapshot) bool
	}{
		{"parked and charged", mon + hm(3, 0), func(s core.Snapshot) bool {
			return s.Engine.V == core.EngineStopped && s.Connection.V == core.Connected && s.Charging.V == core.ChargingDone &&
				s.ChargeType.V == core.AC && s.PowerW.V == 0 && s.SoC.V == chargeSoC
		}},
		{"driving", mon + hm(6, 55), func(s core.Snapshot) bool {
			return s.Engine.V == core.EngineRunning && s.Connection.V == core.Disconnected && s.Charging.V == core.ChargingIdle &&
				s.ChargeType.V == core.NoCharge && s.OdometerKm.V > firstOdometerKm
		}},
		{"home charge", mon + hm(16, 30), func(s core.Snapshot) bool {
			return s.Charging.V == core.ChargingActive && s.ChargeType.V == core.AC && s.PowerW.V == 11000 && s.Connection.V == core.Connected
		}},
	} {
		r := read(t, c, Origin().Add(7*Week+tc.at))
		raw, _ := Marshal(r)
		s, err := norm.Normalize(kind, raw)
		if err != nil {
			t.Fatal(err)
		}
		if !tc.want(s) {
			t.Errorf("%s: %+v from %+v", tc.name, s, r)
		}
		for name, ok := range map[string]bool{
			"soc": s.SoC.OK, "range": s.RangeKm.OK, "odometer": s.OdometerKm.OK, "position": s.Position.OK,
			"outside": s.OutsideTempC.OK, "capacity": s.CapacityKWh.OK, "family": s.Family.OK, "sheet": s.Sheet.OK,
		} {
			if !ok {
				t.Errorf("%s: %s not told", tc.name, name)
			}
		}
		v, recognized := cat.Recognize(catalog.Reading{
			Brand: catalog.Brand(s.Brand.V), Family: s.Family.V, ModelYear: s.ModelYear.V, CapacityKWh: s.CapacityKWh.V,
			VIN: VIN, Powertrain: catalog.Powertrain(s.Powertrain.V),
		})
		if !recognized || v.ID != "ex30-er-2024" {
			t.Errorf("%s: recognized as %q (%v)", tc.name, v.ID, recognized)
		}
	}
	if _, err := norm.Normalize(core.Kind{Source: "volvo", Endpoint: "vehicle"}, nil); err == nil {
		t.Error("another source's response read")
	}
	if _, err := norm.Normalize(core.Kind{Source: Source, Endpoint: "other"}, nil); err == nil {
		t.Error("another endpoint read")
	}
	if _, err := Normalize([]byte(`{`)); err == nil {
		t.Error("garbage read")
	}
	if _, err := norm.NormalizeHealth(kind, nil); err == nil {
		t.Error("a health read")
	}
	if len(norm.StateKinds())+len(norm.HealthKinds())+len(norm.SleepKinds()) != 0 || len(norm.EnergyKinds()) != 1 {
		t.Error("kinds beyond the one")
	}
}

// TestTimetable: the week compiles, and a timetable that cannot is refused.
func TestTimetable(t *testing.T) {
	if _, err := compile([]step{drive(hm(8, 0), "work", 14, time.Hour), drive(hm(8, 30), "home", 14, time.Hour)}); err == nil {
		t.Error("overlapping drives compiled")
	}
	if _, err := compile([]step{drive(hm(8, 0), "work", 14, time.Hour)}); err == nil {
		t.Error("a week that ends away from home compiled")
	}
	if _, err := compile([]step{drive(hm(8, 0), "dinant", 400, 4*time.Hour), drive(hm(15, 0), "home", 400, 4*time.Hour)}); err == nil {
		t.Error("a week that empties the battery compiled")
	}
	if _, err := compile([]step{drive(hm(8, 0), "work", 14, time.Hour), drive(hm(9, 0), "home", 14, time.Hour)}); err == nil {
		t.Error("a week that ends uncharged compiled")
	}
	if _, err := compile([]step{drive(Week, "work", 1, time.Minute), drive(Week+hm(1, 0), "home", 1, time.Minute)}); err == nil {
		t.Error("a week of more than a week compiled")
	}
	if km := mustNew(t).WeekKm(); km < 300 || km > 800 {
		t.Errorf("a week of %v km", km)
	}
}
