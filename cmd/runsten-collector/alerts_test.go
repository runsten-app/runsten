package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/notify"
	"runsten/internal/platform/clock"
)

// fleet is a vehicle's readings, the account's places and a clock, as the evaluator reads
// them.
type fleet struct {
	records []core.Record
	places  []core.Place
	clk     *clock.Manual
	// historyReads counts the reads of the history.
	historyReads int
	// stale: the vehicle is not read again after its last reading, which every poll of a
	// vehicle that answers confirms otherwise.
	stale bool
}

func (f *fleet) alerts(prefs notify.Preferences) (*stateAlerts, *sent) {
	s := &sent{}
	return &stateAlerts{
		current: func(_ context.Context, _, _ string, at time.Time) (core.Current, error) {
			var upTo []core.Record
			for _, r := range f.records {
				if !r.FetchedAt.After(at) {
					upTo = append(upTo, r)
				}
			}
			if n := len(upTo); n > 0 && !f.stale {
				upTo[n-1].CheckedAt = at
			}
			return core.Latest(upTo), nil
		},
		records: func(context.Context, string, string, time.Time) ([]core.Record, error) {
			f.historyReads++
			return f.records, nil
		},
		places: func(context.Context, string) ([]core.Place, error) { return f.places, nil },
		targets: func(context.Context) ([]collector.Target, error) {
			return []collector.Target{{AccountID: "a", VehicleID: "v"}}, nil
		},
		notifier: notifierOf(prefs, s), clk: f.clk, log: slog.New(slog.DiscardHandler),
	}, s
}

// read adds a reading at t.
func (f *fleet) read(t time.Time, s core.Snapshot) {
	f.records = append(f.records, core.Record{FetchedAt: t, CheckedAt: t, Snapshot: s})
}

func parked(soc float64, st core.ChargingStatus, cable core.Connection) core.Snapshot {
	return core.Snapshot{
		Covers: core.FieldSoC | core.FieldEngine | core.FieldCharging | core.FieldConnection | core.FieldTargetSoC,
		SoC:    core.Some(soc, time.Time{}), Engine: core.Some(core.EngineStopped, time.Time{}),
		Charging: core.Some(st, time.Time{}), Connection: core.Some(cable, time.Time{}), TargetSoC: core.Some(80.0, time.Time{}),
	}
}

func TestBatteryLowAlert(t *testing.T) {
	f := &fleet{clk: clock.NewManual(now)}
	a, s := f.alerts(notify.Preferences{})
	observe := func() {
		t.Helper()
		if err := a.observe(t.Context(), "a", "v"); err != nil {
			t.Fatal(err)
		}
	}
	f.read(now.Add(-5*time.Hour), parked(40, core.ChargingIdle, core.Disconnected))
	observe()
	if len(s.sorted()) != 0 {
		t.Errorf("above the threshold: %v", s.sorted())
	}
	// It falls under 20 %: told at the next evaluation, once however many follow, and the
	// history read once.
	f.read(now.Add(-2*time.Hour), parked(19, core.ChargingIdle, core.Disconnected))
	f.read(now.Add(-time.Hour), parked(18, core.ChargingIdle, core.Disconnected))
	for range 3 {
		f.clk.Advance(time.Minute)
		observe()
	}
	if got := s.sorted(); !slices.Equal(got, []string{"battery_low v"}) || f.historyReads != 1 {
		t.Errorf("sent %v, %d history reads", got, f.historyReads)
	}
	// A charge that does not reach the threshold, then parked again under it: the same
	// episode, not told again.
	f.read(now.Add(10*time.Minute), parked(19, core.ChargingActive, core.Connected))
	f.read(now.Add(20*time.Minute), parked(19.0, core.ChargingDone, core.Connected))
	f.clk.Advance(time.Hour)
	observe()
	if got := s.sorted(); len(got) != 1 {
		t.Errorf("same episode: %v", got)
	}
	// Back at the threshold or above: re-armed. Then under it again: told again.
	f.read(now.Add(2*time.Hour), parked(20, core.ChargingDone, core.Connected))
	f.clk.Advance(time.Hour)
	observe()
	f.read(now.Add(5*time.Hour), parked(19, core.ChargingIdle, core.Disconnected))
	to(f.clk, now.Add(5*time.Hour+time.Minute))
	observe()
	if got := s.sorted(); len(got) != 2 {
		t.Errorf("a new episode: %v", got)
	}
}

// TestBatteryLowRestart: an evaluator without memory (a restart) finds the episode in the
// history, and its key is the same: nothing is told twice.
func TestBatteryLowRestart(t *testing.T) {
	f := &fleet{clk: clock.NewManual(now)}
	store := &notifyStore{seen: map[string]bool{}}
	sender := &sent{}
	n := notify.New(notify.NewRegistry(), store, f.clk, slog.New(slog.DiscardHandler))
	n.SetSender(sender)
	f.read(now.Add(-2*time.Hour), parked(15, core.ChargingIdle, core.Disconnected))
	for range 2 {
		a, _ := f.alerts(notify.Preferences{})
		a.notifier = n
		if err := a.observe(t.Context(), "a", "v"); err != nil {
			t.Fatal(err)
		}
	}
	if got := sender.sorted(); len(got) != 1 {
		t.Errorf("sent %v, want one", got)
	}
}

func TestBatteryLowThresholds(t *testing.T) {
	for name, tt := range map[string]struct {
		soc   float64
		prefs notify.Preferences
		want  int
	}{
		"default 20 %":       {19, notify.Preferences{}, 1},
		"at the default":     {20, notify.Preferences{}, 0},
		"default raised":     {24, notify.Preferences{VehiclesDefaultThreshold: map[notify.Kind]int{notify.BatteryLow: 25}}, 1},
		"the vehicle's own":  {24, notify.Preferences{VehiclesDefaultThreshold: map[notify.Kind]int{notify.BatteryLow: 25}, VehicleThresholds: map[string]map[notify.Kind]int{"v": {notify.BatteryLow: 10}}}, 0},
		"turned off":         {5, notify.Preferences{VehiclesDefault: map[notify.Kind]bool{notify.BatteryLow: false}}, 0},
		"turned off for it":  {5, notify.Preferences{Vehicles: map[string]map[notify.Kind]bool{"v": {notify.BatteryLow: false}}}, 0},
		"the lowest allowed": {4, notify.Preferences{VehicleThresholds: map[string]map[notify.Kind]int{"v": {notify.BatteryLow: 5}}}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fleet{clk: clock.NewManual(now)}
			a, s := f.alerts(tt.prefs)
			f.read(now.Add(-time.Hour), parked(tt.soc, core.ChargingIdle, core.Disconnected))
			if err := a.observe(t.Context(), "a", "v"); err != nil {
				t.Fatal(err)
			}
			if got := len(s.sorted()); got != tt.want {
				t.Errorf("sent %d, want %d", got, tt.want)
			}
		})
	}
}

// TestBatteryLowUnknowns: no state of charge, driving, charging, a reading too old: nothing.
func TestBatteryLowUnknowns(t *testing.T) {
	driving := parked(10, core.ChargingIdle, core.Disconnected)
	driving.Engine = core.Some(core.EngineRunning, time.Time{})
	noEngine := core.Snapshot{Covers: core.FieldSoC, SoC: core.Some(10.0, time.Time{})}
	for name, snap := range map[string]core.Snapshot{
		"no state of charge": {Covers: core.FieldEngine, Engine: core.Some(core.EngineStopped, time.Time{})},
		"driving":            driving,
		"charging":           parked(10, core.ChargingActive, core.Connected),
		"no engine":          noEngine,
	} {
		t.Run(name, func(t *testing.T) {
			f := &fleet{clk: clock.NewManual(now)}
			a, s := f.alerts(notify.Preferences{})
			f.read(now.Add(-time.Minute), snap)
			if err := a.observe(t.Context(), "a", "v"); err != nil || len(s.sorted()) != 0 {
				t.Errorf("%v, sent %v", err, s.sorted())
			}
		})
	}
	t.Run("read long ago", func(t *testing.T) {
		f := &fleet{clk: clock.NewManual(now), stale: true}
		a, s := f.alerts(notify.Preferences{})
		f.read(now.Add(-5*time.Hour), parked(10, core.ChargingIdle, core.Disconnected))
		if err := a.observe(t.Context(), "a", "v"); err != nil || len(s.sorted()) != 0 {
			t.Errorf("%v, sent %v", err, s.sorted())
		}
	})
}

func doors(locked bool) core.Snapshot {
	return core.Snapshot{
		Covers: core.FieldDoors | core.FieldEngine, Engine: core.Some(core.EngineStopped, time.Time{}),
		Doors: core.Some(core.Doors{Locked: core.Some(locked, time.Time{})}, time.Time{}),
	}
}

func TestLeftOpenAlert(t *testing.T) {
	on := notify.Preferences{VehiclesDefault: map[notify.Kind]bool{notify.LeftOpen: true}}
	t.Run("off by default", func(t *testing.T) {
		f := &fleet{clk: clock.NewManual(now)}
		a, s := f.alerts(notify.Preferences{})
		f.read(now.Add(-time.Hour), doors(false))
		if err := a.observe(t.Context(), "a", "v"); err != nil || len(s.sorted()) != 0 {
			t.Errorf("%v, sent %v", err, s.sorted())
		}
	})
	t.Run("told once an episode, after the delay", func(t *testing.T) {
		f := &fleet{clk: clock.NewManual(now)}
		a, s := f.alerts(on)
		f.read(now.Add(-20*time.Minute), doors(true))
		f.read(now.Add(-10*time.Minute), doors(false))
		observe := func() {
			t.Helper()
			if err := a.observe(t.Context(), "a", "v"); err != nil {
				t.Fatal(err)
			}
		}
		observe() // open for 10 minutes: not yet
		if len(s.sorted()) != 0 {
			t.Fatalf("told early: %v", s.sorted())
		}
		f.clk.Advance(6 * time.Minute)
		observe()
		f.clk.Advance(time.Hour)
		observe()
		if got := s.sorted(); !slices.Equal(got, []string{"left_open v"}) || f.historyReads != 1 {
			t.Errorf("sent %v, %d history reads", got, f.historyReads)
		}
		// Locked, then left open again: a new episode.
		f.read(now.Add(2*time.Hour), doors(true))
		to(f.clk, now.Add(2*time.Hour+time.Minute))
		observe()
		f.read(now.Add(3*time.Hour), doors(false))
		to(f.clk, now.Add(3*time.Hour+20*time.Minute))
		observe()
		if got := s.sorted(); len(got) != 2 {
			t.Errorf("a new episode: %v", got)
		}
	})
	t.Run("a make that reports neither locks nor windows", func(t *testing.T) {
		f := &fleet{clk: clock.NewManual(now)}
		a, s := f.alerts(on)
		f.read(now.Add(-time.Hour), parked(50, core.ChargingIdle, core.Disconnected))
		if err := a.observe(t.Context(), "a", "v"); err != nil || len(s.sorted()) != 0 || f.historyReads != 0 {
			t.Errorf("%v, sent %v", err, s.sorted())
		}
	})
	t.Run("driving", func(t *testing.T) {
		f := &fleet{clk: clock.NewManual(now)}
		a, s := f.alerts(on)
		d := doors(false)
		d.Engine = core.Some(core.EngineRunning, time.Time{})
		f.read(now.Add(-time.Hour), d)
		if err := a.observe(t.Context(), "a", "v"); err != nil || len(s.sorted()) != 0 {
			t.Errorf("%v, sent %v", err, s.sorted())
		}
	})
}

func TestNotChargedAlert(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("no time zone database:", err)
	}
	home := core.Position{Lat: 45.764, Lon: 4.8357}
	place := core.Place{ID: "home", Name: "Home", Position: home, RadiusM: 100, Location: paris}
	at := func(d, hh, mm int) time.Time { return time.Date(2026, 11, d, hh, mm, 0, 0, paris) }
	plugged := func(soc float64) core.Snapshot { return parked(soc, core.ChargingScheduled, core.Connected) }
	setup := func(clock time.Time) (*fleet, *stateAlerts, *sent) {
		f := &fleet{places: []core.Place{place}, clk: clock2(clock)}
		a, s := f.alerts(notify.Preferences{})
		f.read(at(22, 17, 30), core.Snapshot{Covers: core.FieldPosition, Position: core.Some(home, time.Time{})})
		f.read(at(22, 19, 0), plugged(40))
		f.read(at(23, 6, 30), plugged(40))
		return f, a, s
	}
	t.Run("told in the morning, once", func(t *testing.T) {
		f, a, s := setup(at(23, 6, 55))
		check := func() {
			t.Helper()
			if err := a.check(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		check() // before 07:00 local
		if len(s.sorted()) != 0 {
			t.Fatalf("told before the morning: %v", s.sorted())
		}
		for _, m := range []time.Duration{6 * time.Minute, 11 * time.Minute, 30 * time.Minute} {
			to(f.clk, at(23, 6, 55).Add(m))
			check()
		}
		if got := s.sorted(); !slices.Equal(got, []string{"not_charged v"}) {
			t.Errorf("sent %v", got)
		}
		// Not told long after, nor the day after.
		to(f.clk, at(23, 14, 0))
		before := f.historyReads
		check()
		if f.historyReads != before {
			t.Error("history read outside the morning")
		}
	})
	t.Run("it charged", func(t *testing.T) {
		f, a, s := setup(at(23, 7, 10))
		f.read(at(23, 3, 0), plugged(60))
		f.read(at(23, 6, 40), plugged(60))
		if err := a.check(t.Context()); err != nil || len(s.sorted()) != 0 {
			t.Errorf("%v, sent %v", err, s.sorted())
		}
	})
	t.Run("no place", func(t *testing.T) {
		f, a, s := setup(at(23, 7, 10))
		f.places = nil
		if err := a.check(t.Context()); err != nil || len(s.sorted()) != 0 {
			t.Errorf("%v, sent %v", err, s.sorted())
		}
	})
	t.Run("turned off", func(t *testing.T) {
		f, _, _ := setup(at(23, 7, 10))
		a, s := f.alerts(notify.Preferences{VehiclesDefault: map[notify.Kind]bool{notify.NotCharged: false}})
		if err := a.check(t.Context()); err != nil || len(s.sorted()) != 0 || f.historyReads != 0 {
			t.Errorf("%v, sent %v, %d reads", err, s.sorted(), f.historyReads)
		}
	})
	t.Run("failures are joined", func(t *testing.T) {
		f, a, _ := setup(at(23, 7, 10))
		down := errors.New("down")
		a.records = func(context.Context, string, string, time.Time) ([]core.Record, error) { return nil, down }
		if err := a.check(t.Context()); !errors.Is(err, down) {
			t.Errorf("err = %v", err)
		}
		_ = f
	})
	t.Run("without a sender nothing is read", func(t *testing.T) {
		f, a, _ := setup(at(23, 7, 10))
		a.notifier = notifierOf(notify.Preferences{}, nil)
		if err := a.check(t.Context()); err != nil || f.historyReads != 0 {
			t.Errorf("%v, %d reads", err, f.historyReads)
		}
	})
}

func clock2(t time.Time) *clock.Manual { return clock.NewManual(t) }

// TestAlertStates: MQTT's states read the vehicle's threshold and its night, the night
// judged at most every few minutes.
func TestAlertStates(t *testing.T) {
	f := &fleet{clk: clock.NewManual(now)}
	a, _ := f.alerts(notify.Preferences{VehicleThresholds: map[string]map[notify.Kind]int{"v": {notify.BatteryLow: 33}}})
	states := &alertStates{notifier: a.notifier, records: a.records, places: func(context.Context, string) ([]core.Place, error) {
		return []core.Place{{ID: "home", Location: time.UTC}}, nil
	}}
	if n, err := states.BatteryLowThreshold(t.Context(), "a", "v"); err != nil || n != 33 {
		t.Errorf("threshold %d, %v", n, err)
	}
	if n, err := states.BatteryLowThreshold(t.Context(), "a", "other"); err != nil || n != 20 {
		t.Errorf("default %d, %v", n, err)
	}
	for range 3 {
		if _, err := states.NightCheck(t.Context(), "a", "v", now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if f.historyReads != 1 {
		t.Errorf("%d history reads for one judgement", f.historyReads)
	}
	if _, err := states.NightCheck(t.Context(), "a", "v", now.Add(10*time.Minute)); err != nil || f.historyReads != 2 {
		t.Errorf("%d history reads after the delay, %v", f.historyReads, err)
	}
}

// to moves the clock to t.
func to(c *clock.Manual, t time.Time) { c.Advance(t.Sub(c.Now())) }

// blocking is a sender that holds the first delivery until released.
type blocking struct {
	sent
	started chan struct{}
	release chan struct{}
}

func (b *blocking) Send(ctx context.Context, d notify.Delivery) error {
	close(b.started)
	<-b.release
	return b.sent.Send(ctx, d)
}

// TestAlertsSerialized: the evaluation after a reading and the timer's meet for the same
// vehicle; the second waits for the first, delivery included, so that the end of an
// episode never comes between the check that an alert is due and its delivery.
func TestAlertsSerialized(t *testing.T) {
	f := &fleet{clk: clock.NewManual(now)}
	a, _ := f.alerts(notify.Preferences{})
	b := &blocking{started: make(chan struct{}), release: make(chan struct{})}
	a.notifier = notifierOf(notify.Preferences{}, b)
	f.read(now.Add(-time.Hour), parked(15, core.ChargingIdle, core.Disconnected))
	first := make(chan error, 1)
	go func() { first <- a.observe(t.Context(), "a", "v") }()
	<-b.started // the first is about to deliver, its check passed
	// A charge above the threshold arrives; the second evaluation sees it.
	f.read(now.Add(-time.Minute), parked(60, core.ChargingDone, core.Connected))
	second := make(chan error, 1)
	go func() { second <- a.observe(t.Context(), "a", "v") }()
	select {
	case <-second:
		t.Fatal("the second evaluation ran while the first was delivering")
	case <-time.After(100 * time.Millisecond):
	}
	close(b.release)
	for _, c := range []chan error{first, second} {
		if err := <-c; err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	low := a.episode("v").low
	a.mu.Unlock()
	if !low.IsZero() || len(b.sorted()) != 1 {
		t.Errorf("after both: low %s, sent %v", low, b.sorted())
	}
	// Another vehicle is not held back.
	if err := a.observe(t.Context(), "a", "other"); err != nil {
		t.Fatal(err)
	}
}

// TestBatteryLowDisabled: a kind off or hidden loads nothing and keeps no episode; turned on
// again, the episode is found again, and told.
func TestBatteryLowDisabled(t *testing.T) {
	f := &fleet{clk: clock.NewManual(now)}
	a, s := f.alerts(notify.Preferences{VehiclesDefault: map[notify.Kind]bool{notify.BatteryLow: false}})
	f.read(now.Add(-time.Hour), parked(15, core.ChargingIdle, core.Disconnected))
	a.notifier.SetHiddenKinds(func(context.Context, string) []notify.Kind { return nil })
	if err := a.observe(t.Context(), "a", "v"); err != nil || f.historyReads != 0 || len(s.sorted()) != 0 {
		t.Fatalf("off: %v, %d reads, sent %v", err, f.historyReads, s.sorted())
	}
	// Hidden by an extension, though on.
	a, s = f.alerts(notify.Preferences{})
	a.notifier.SetHiddenKinds(func(context.Context, string) []notify.Kind { return []notify.Kind{notify.BatteryLow} })
	if err := a.observe(t.Context(), "a", "v"); err != nil || f.historyReads != 0 || len(s.sorted()) != 0 {
		t.Fatalf("hidden: %v, %d reads, sent %v", err, f.historyReads, s.sorted())
	}
	a.notifier.SetHiddenKinds(nil)
	if err := a.observe(t.Context(), "a", "v"); err != nil || len(s.sorted()) != 1 || f.historyReads != 1 {
		t.Errorf("shown again: %v, %d reads, sent %v", err, f.historyReads, s.sorted())
	}
}
