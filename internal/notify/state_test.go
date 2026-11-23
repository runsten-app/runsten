package notify

import (
	"context"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/publish"
)

// read is a record of one reading, at the given time.
func read(at time.Time, s core.Snapshot) core.Record {
	return core.Record{FetchedAt: at, CheckedAt: at, Snapshot: s}
}

func reading(soc float64, st core.ChargingStatus, cable core.Connection) core.Snapshot {
	return core.Snapshot{
		Covers: core.FieldSoC | core.FieldCharging | core.FieldConnection | core.FieldEngine | core.FieldTargetSoC,
		SoC:    core.Some(soc, time.Time{}), Charging: core.Some(st, time.Time{}), Connection: core.Some(cable, time.Time{}),
		Engine: core.Some(core.EngineStopped, time.Time{}), TargetSoC: core.Some(80.0, time.Time{}),
	}
}

func TestBatteryLowEvent(t *testing.T) {
	now := t0.Add(10 * time.Minute)
	since := t0.Add(-3 * time.Hour)
	low := core.Latest([]core.Record{read(t0, reading(18, core.ChargingIdle, core.Disconnected))})
	fine := core.Latest([]core.Record{read(t0, reading(30, core.ChargingIdle, core.Disconnected))})
	charging := core.Latest([]core.Record{read(t0, reading(18, core.ChargingActive, core.Connected))})
	e, ok := BatteryLowEvent("a", "v", low, 20, since, now)
	if !ok || e.Kind != BatteryLow || e.VehicleID != "v" || e.Key != "low@"+stamp(since) || e.Args["soc"] != "18" || e.Args["threshold"] != "20" {
		t.Errorf("event %+v, %v", e, ok)
	}
	if _, ok := BatteryLowEvent("a", "v", low, 15, since, now); ok {
		t.Error("18 % is not under a threshold of 15")
	}
	for name, tt := range map[string]struct {
		c     core.Current
		since time.Time
		now   time.Time
	}{
		"not low":             {fine, since, now},
		"charging":            {charging, since, now},
		"no episode":          {low, time.Time{}, now},
		"read long ago":       {low, since, t0.Add(2 * time.Hour)},
		"nothing read":        {core.Current{}, since, now},
		"engine not reported": {core.Latest([]core.Record{read(t0, core.Snapshot{Covers: core.FieldSoC, SoC: core.Some(10.0, time.Time{})})}), since, now},
	} {
		if _, ok := BatteryLowEvent("a", "v", tt.c, 20, tt.since, tt.now); ok {
			t.Errorf("%s: an event", name)
		}
	}
	// Another episode, another key: a charge above the threshold re-arms it.
	again, _ := BatteryLowEvent("a", "v", low, 20, since.Add(5*time.Hour), now)
	if again.Key == e.Key {
		t.Error("two episodes, one key")
	}
}

func TestLeftOpenEvent(t *testing.T) {
	now := t0.Add(time.Hour)
	open := core.Snapshot{Covers: core.FieldDoors, Doors: core.Some(core.Doors{Locked: core.Some(false, time.Time{})}, time.Time{})}
	cur := core.Latest([]core.Record{read(t0, open)})
	since := now.Add(-20 * time.Minute)
	e, ok := LeftOpenEvent("a", "v", cur, since, now)
	if !ok || e.Kind != LeftOpen || e.Key != "open@"+stamp(since) {
		t.Errorf("event %+v, %v", e, ok)
	}
	if _, ok := LeftOpenEvent("a", "v", cur, now.Add(-publish.LeftOpenAfter+time.Second), now); ok {
		t.Error("open for less than the delay")
	}
	if _, ok := LeftOpenEvent("a", "v", cur, time.Time{}, now); ok {
		t.Error("not open")
	}
	// A make that reports neither locks nor windows: unknown, nothing.
	if _, ok := LeftOpenEvent("a", "v", core.Latest([]core.Record{read(t0, reading(50, core.ChargingIdle, core.Disconnected))}), since, now); ok {
		t.Error("left open without a lock or a window told")
	}
}

func TestNotChargedEvent(t *testing.T) {
	morning := time.Date(2026, 11, 23, 7, 0, 0, 0, time.UTC)
	night := core.Value[core.NightCheck]{OK: true, V: core.NightCheck{
		Date: time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC), Morning: morning, PluggedAt: morning.Add(-12 * time.Hour),
		EveningSoC: 41.6, NotCharged: true,
	}}
	e, ok := NotChargedEvent("a", "v", night, morning.Add(5*time.Minute))
	if !ok || e.Kind != NotCharged || e.Key != "2026-11-23" || e.Args["soc"] != "42" {
		t.Errorf("event %+v, %v", e, ok)
	}
	// The same key whenever the check runs: once a night.
	again, _ := NotChargedEvent("a", "v", night, morning.Add(time.Hour))
	if again.Key != e.Key {
		t.Errorf("keys %q, %q", e.Key, again.Key)
	}
	if _, ok := NotChargedEvent("a", "v", night, morning.Add(-time.Minute)); ok {
		t.Error("told before the morning")
	}
	if _, ok := NotChargedEvent("a", "v", night, morning.Add(NotChargedWithin+time.Minute)); ok {
		t.Error("told long after the morning")
	}
	night.V.NotCharged = false
	if _, ok := NotChargedEvent("a", "v", night, morning); ok {
		t.Error("told of a night that was fine")
	}
	if _, ok := NotChargedEvent("a", "v", core.Value[core.NightCheck]{}, morning); ok {
		t.Error("told of an unknown night")
	}
}

func TestThreshold(t *testing.T) {
	low, _ := NewRegistry().Spec(BatteryLow)
	if low.Threshold == nil || !low.Default {
		t.Fatalf("spec %+v", low)
	}
	p := Preferences{}
	if got := p.Threshold(low, "v1"); got != 20 {
		t.Errorf("default = %d", got)
	}
	p.VehiclesDefaultThreshold = map[Kind]int{BatteryLow: 30}
	p.VehicleThresholds = map[string]map[Kind]int{"v2": {BatteryLow: 10}}
	if p.Threshold(low, "v1") != 30 || p.Threshold(low, "v2") != 10 || p.DefaultThreshold(low) != 30 {
		t.Errorf("thresholds %d %d %d", p.Threshold(low, "v1"), p.Threshold(low, "v2"), p.DefaultThreshold(low))
	}
	other, _ := NewRegistry().Spec(ChargeFinished)
	if p.Threshold(other, "v1") != 0 {
		t.Error("a kind without a threshold has one")
	}
	left, _ := NewRegistry().Spec(LeftOpen)
	if left.Default || left.Threshold != nil {
		t.Errorf("left_open %+v", left)
	}
	if err := NewRegistry().Register(Spec{Kind: "x_low", Level: AccountLevel, Threshold: &Threshold{Min: 1, Max: 2, Default: 1}}); err == nil {
		t.Error("a threshold of an account's kind registered")
	}
}

func TestHiddenKinds(t *testing.T) {
	st := &memStore{devices: []Device{phone}, prefs: Preferences{VehiclesDefault: map[Kind]bool{LeftOpen: true}}}
	n, s, _ := newNotifier(st)
	e := Event{Kind: LeftOpen, AccountID: "a", VehicleID: "v", Key: "open@1", At: t0}
	if wants, err := n.Wants(t.Context(), "a", "v", LeftOpen); err != nil || !wants {
		t.Fatalf("wants %v, %v", wants, err)
	}
	n.SetHiddenKinds(func(_ context.Context, account string) []Kind {
		if account == "a" {
			return []Kind{LeftOpen}
		}
		return nil
	})
	if wants, _ := n.Wants(t.Context(), "a", "v", LeftOpen); wants {
		t.Error("a hidden kind wanted")
	}
	if err := n.Notify(t.Context(), e); err != nil || len(s.sent) != 0 || len(st.delivered) != 0 {
		t.Errorf("a hidden kind sent: %v %+v %+v", err, s.sent, st.delivered)
	}
	e.AccountID = "b"
	if err := n.Notify(t.Context(), e); err != nil || len(s.sent) != 1 {
		t.Errorf("another account: %v, %d sent", err, len(s.sent))
	}
	if (*Notifier)(nil).Hidden(t.Context(), "a") != nil || n.IsHidden(t.Context(), "b", LeftOpen) {
		t.Error("hidden for no one")
	}
}
