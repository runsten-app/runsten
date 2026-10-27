package collector

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

// The endpoints of sleepyCar, as Tesla's.
const (
	carState Endpoint = "vehicle"
	carData  Endpoint = "vehicle_data"
)

// sleepyCar is a Source whose car falls asleep sleepAfter its last read of carData, when
// parked; carState is read without waking it, and an asleep car answers carData with
// KindAsleep. drives and wakes tell when it drives, and when someone wakes it.
type sleepyCar struct {
	clk        clock.Clock
	sleepAfter time.Duration
	drives     func(at time.Time) bool
	wakes      func(at time.Time) bool
	dataErr    error // answered to carData when not nil
	lastActive time.Time
	calls      map[Endpoint][]time.Time
}

func newSleepyCar(clk clock.Clock) *sleepyCar {
	return &sleepyCar{
		clk: clk, sleepAfter: 10 * time.Minute, lastActive: clk.Now(), calls: map[Endpoint][]time.Time{},
		drives: func(time.Time) bool { return false }, wakes: func(time.Time) bool { return false },
	}
}

func (*sleepyCar) Spec() SourceSpec {
	return SourceSpec{
		Name: "sleepy",
		Endpoints: []EndpointSpec{
			{Endpoint: carState, API: "sleepy", Parked: PaceParked, Driving: PaceRare, Charging: PaceParked},
			{Endpoint: carData, API: "sleepy", Parked: PaceParked, Driving: PaceActive, Charging: PaceParked, Awake: true},
		},
		Settled:   []Endpoint{carData},
		Daily:     100_000,
		Intervals: Intervals{Parked: time.Minute, Active: 15 * time.Second, Rare: time.Hour, Details: 24 * time.Hour},
		Sleep:     Sleep{IdleBefore: 3 * time.Minute, Window: 15 * time.Minute},
	}
}

func (c *sleepyCar) Login(context.Context, string, string) (Session, time.Duration, error) {
	return c, 8 * time.Hour, nil
}

func (c *sleepyCar) awake(at time.Time) bool {
	if c.drives(at) || c.wakes(at) {
		c.lastActive = at
	}
	return at.Sub(c.lastActive) < c.sleepAfter
}

func (c *sleepyCar) Fetch(_ context.Context, _ string, ep Endpoint) ([]byte, error) {
	now := c.clk.Now()
	c.calls[ep] = append(c.calls[ep], now)
	awake := c.awake(now)
	switch {
	case ep == carState && awake:
		return []byte("online"), nil
	case ep == carState:
		return []byte("asleep"), nil
	case c.dataErr != nil:
		return nil, c.dataErr
	case !awake:
		return nil, &SourceError{Kind: KindAsleep, Status: 408, Err: errors.New("vehicle unavailable")}
	}
	c.lastActive = now // a read of the data keeps it awake
	if c.drives(now) {
		return []byte("D"), nil
	}
	return []byte("P"), nil
}

func (*sleepyCar) Observe(ep Endpoint, raw []byte, a *Activity) error {
	switch ep {
	case carState:
		a.Asleep = string(raw) != "online"
	case carData:
		a.Asleep = false
		a.Engine, a.Driving = string(raw), string(raw) == "D"
	}
	return nil
}

func (*sleepyCar) Fingerprint(raw []byte) ([]byte, error) {
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

func (*sleepyCar) Upload(Endpoint, []byte) (Upload, error) { return Upload{}, nil }

func newSleepy(t *testing.T) (*Collector, *sleepyCar, *sourceStore, *clock.Manual) {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	car := newSleepyCar(clk)
	tg := sourceTarget()
	tg.Provider = "sleepy"
	st := &sourceStore{memStore: memStore{targets: []Target{tg}}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	c.AddSource(car, st)
	return c, car, st, clk
}

// run polls every 10 s, as the collector's tick, until d after now.
func run(t *testing.T, c *Collector, clk *clock.Manual, d time.Duration) {
	t.Helper()
	end := clk.Now().Add(d)
	for clk.Now().Before(end) {
		poll(t, c)
		clk.Advance(10 * time.Second)
	}
}

// TestSleepWindow: parked and awake, the data is read for IdleBefore, then left alone;
// the car falls asleep within the window, and is no longer read but for its state,
// at the source's pace rather than the collector's.
func TestSleepWindow(t *testing.T) {
	c, car, st, clk := newSleepy(t)
	t0 := clk.Now()
	run(t, c, clk, 2*time.Hour)
	if n := countIn(car.calls[carData], t0, t0.Add(4*time.Minute)); n < 3 || n > 4 {
		t.Errorf("first minutes: data read %d times, want 3 or 4", n)
	}
	if n := countIn(car.calls[carData], t0.Add(4*time.Minute), t0.Add(2*time.Hour)); n != 0 {
		t.Errorf("data read %d times once the window opened: the car never sleeps", n)
	}
	if car.awake(clk.Now()) {
		t.Error("the car is awake after 2 h parked")
	}
	if n := countIn(car.calls[carState], t0, t0.Add(2*time.Hour)); n < 115 || n > 121 {
		t.Errorf("state read %d times in 2 h, want one a minute", n)
	}
	vs := c.vehicles["p"]
	if !vs.activity.Asleep || vs.mode != parked || vs.failure != (Failure{}) {
		t.Errorf("asleep: activity %+v, mode %s, failure %+v", vs.activity, vs.mode, vs.failure)
	}
	// The status tells the state's next read, not the data's, past due.
	s := st.statuses[len(st.statuses)-1]
	if next := vs.next[carState]; !s.NextAt.Equal(next) && !s.NextAt.Equal(clk.Now().Add(-10*time.Second)) {
		t.Errorf("next at %v, want the state's %v", s.NextAt, next)
	}
}

// TestSleepWindowStaysAwake: a car that does not fall asleep is read again at the
// window's end, and the next window opens IdleBefore later.
func TestSleepWindowStaysAwake(t *testing.T) {
	c, car, _, clk := newSleepy(t)
	car.sleepAfter = 24 * time.Hour
	t0 := clk.Now()
	run(t, c, clk, 37*time.Minute)
	// Read in [0, 3 min], left alone 15 min, read in [18, 21], left alone until 36.
	if n := countIn(car.calls[carData], t0.Add(4*time.Minute), t0.Add(18*time.Minute)); n != 0 {
		t.Errorf("data read %d times in the first window", n)
	}
	if n := countIn(car.calls[carData], t0.Add(18*time.Minute), t0.Add(22*time.Minute)); n < 3 || n > 4 {
		t.Errorf("data read %d times between the windows, want 3 or 4", n)
	}
	if n := countIn(car.calls[carData], t0.Add(23*time.Minute), t0.Add(36*time.Minute)); n != 0 {
		t.Errorf("data read %d times in the second window", n)
	}
}

// TestWokenAndDriving: an asleep car woken is read at once, its window closed; driving,
// its data is read every active interval and its state seldom; the trip's end reads the
// data at once, and the car falls asleep again.
func TestWokenAndDriving(t *testing.T) {
	c, car, _, clk := newSleepy(t)
	t0 := clk.Now()
	woken, driveFrom, driveTo := t0.Add(time.Hour), t0.Add(time.Hour+2*time.Minute), t0.Add(time.Hour+32*time.Minute)
	car.wakes = func(at time.Time) bool { return !at.Before(woken) && at.Before(driveFrom) }
	car.drives = func(at time.Time) bool { return !at.Before(driveFrom) && at.Before(driveTo) }
	run(t, c, clk, 3*time.Hour)

	var firstData time.Time
	for _, at := range car.calls[carData] {
		if !at.Before(woken) {
			firstData = at
			break
		}
	}
	if firstData.Sub(woken) > time.Minute+10*time.Second {
		t.Errorf("woken at %v, data read at %v", woken, firstData)
	}
	seen := driveFrom.Add(time.Minute)
	if n := countIn(car.calls[carData], seen, driveTo); n < 80 {
		t.Errorf("driving: data read %d times in 29 minutes, want every 15 s (a 10 s tick)", n)
	}
	if n := countIn(car.calls[carState], seen, driveTo); n > 1 {
		t.Errorf("driving: state read %d times", n)
	}
	if n := countIn(car.calls[carData], driveTo.Add(time.Hour), t0.Add(3*time.Hour)); n != 0 {
		t.Errorf("data read %d times an hour after the trip: the car does not sleep again", n)
	}
	if vs := c.vehicles["p"]; !vs.activity.Asleep {
		t.Errorf("activity %+v, want asleep", vs.activity)
	}
}

// TestAsleepAnswer: a car that answers its data asleep (since its state was read) is no
// failure: it is parked, asleep, and its data waits until it is seen awake.
func TestAsleepAnswer(t *testing.T) {
	c, car, st, clk := newSleepy(t)
	car.sleepAfter = 30 * time.Second // its state says it asleep from the next read on
	car.dataErr = &SourceError{Kind: KindAsleep, Status: 408, Err: errors.New("vehicle unavailable")}
	t0 := clk.Now()
	run(t, c, clk, 10*time.Minute)
	if n := len(car.calls[carData]); n != 1 {
		t.Errorf("data called %d times, want once", n)
	}
	vs := c.vehicles["p"]
	if !vs.activity.Asleep || vs.failure != (Failure{}) || st.statuses[len(st.statuses)-1].Failure != (Failure{}) {
		t.Errorf("activity %+v, failure %+v", vs.activity, vs.failure)
	}
	// The state, read next, says it is awake: its data is read at once.
	car.dataErr = nil
	car.wakes = func(at time.Time) bool { return !at.Before(t0.Add(10 * time.Minute)) }
	run(t, c, clk, 2*time.Minute)
	if n := countIn(car.calls[carData], t0.Add(10*time.Minute), clk.Now()); n == 0 {
		t.Error("data not read once awake")
	}
}

// TestAPIClosed: the API closed to the credential marks it refused, the failure told
// as such.
func TestAPIClosed(t *testing.T) {
	c, car, st, clk := newSleepy(t)
	car.dataErr = &SourceError{Kind: KindAPIClosed, Status: 403, Err: errors.New("forbidden, see fleet-api")}
	run(t, c, clk, 5*time.Minute)
	if n := len(car.calls[carData]); n != 1 {
		t.Errorf("data called %d times, want once", n)
	}
	if !st.refused["pc"].Equal(credentialSetAt) {
		t.Errorf("refused %v", st.refused)
	}
	if f := c.vehicles["p"].failure; f.Kind != FailAPIClosed || f.Status != 403 {
		t.Errorf("failure %+v", f)
	}
}

// TestGate: a closed gate leaves the provider's vehicles out, asked once per pass and
// account; opened again, they are read again.
func TestGate(t *testing.T) {
	c, car, _, clk := newSleepy(t)
	open, asked := false, 0
	c.SetGate("sleepy", func(_ context.Context, account string) bool {
		if account != "a" {
			t.Errorf("gate asked for %q", account)
		}
		asked++
		return open
	})
	run(t, c, clk, 5*time.Minute)
	if n := len(car.calls[carState]) + len(car.calls[carData]); n != 0 {
		t.Errorf("%d calls with the gate closed", n)
	}
	if asked != 30 {
		t.Errorf("gate asked %d times in 30 passes", asked)
	}
	open = true
	run(t, c, clk, time.Minute)
	if len(car.calls[carState]) == 0 {
		t.Error("not read once the gate opened")
	}
}
