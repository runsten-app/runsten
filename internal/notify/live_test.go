package notify

import (
	"context"
	"slices"
	"testing"
	"time"

	"runsten/internal/core"
)

// memLive is a memStore that keeps the activities too, as the store does: one per device
// and vehicle.
type memLive struct {
	memStore
	acts []LiveActivity
}

func (m *memLive) LiveActivities(_ context.Context, _, vehicleID string) ([]LiveActivity, error) {
	var out []LiveActivity
	for _, a := range m.acts {
		if a.VehicleID == vehicleID {
			out = append(out, a)
		}
	}
	return out, m.fail
}

func (m *memLive) StartLiveActivities(_ context.Context, _, vehicleID, chargeID string, deviceIDs []string, s ChargeState, now time.Time) ([]string, error) {
	var out []string
	for _, id := range deviceIDs {
		if !slices.ContainsFunc(m.acts, func(a LiveActivity) bool { return a.DeviceID == id && a.VehicleID == vehicleID }) {
			m.acts = append(m.acts, LiveActivity{DeviceID: id, VehicleID: vehicleID, ChargeID: chargeID, State: s, PushedAt: now})
			out = append(out, id)
		}
	}
	return out, m.fail
}

func (m *memLive) MarkLiveActivities(_ context.Context, _, vehicleID, chargeID string, s ChargeState, now time.Time) error {
	for i, a := range m.acts {
		if a.VehicleID == vehicleID && a.ChargeID == chargeID {
			m.acts[i].State, m.acts[i].PushedAt = s, now
		}
	}
	return m.fail
}

func (m *memLive) EndLiveActivities(_ context.Context, _, vehicleID string, chargeIDs []string) error {
	m.acts = slices.DeleteFunc(m.acts, func(a LiveActivity) bool {
		return a.VehicleID == vehicleID && slices.Contains(chargeIDs, a.ChargeID)
	})
	return m.fail
}

// register is the app registering the activity's own token, as the API does.
func (m *memLive) register(deviceID, vehicleID, chargeID, token string) {
	for i, a := range m.acts {
		if a.DeviceID == deviceID && a.VehicleID == vehicleID {
			m.acts[i].ChargeID, m.acts[i].Token = chargeID, token
			return
		}
	}
	m.acts = append(m.acts, LiveActivity{DeviceID: deviceID, VehicleID: vehicleID, ChargeID: chargeID, Token: token})
}

type liveSender struct {
	fakeSender
	pushes []LivePush
}

func (l *liveSender) SendLiveActivity(_ context.Context, p LivePush) error {
	l.pushes = append(l.pushes, p)
	return l.fail
}

func (l *liveSender) take() []LivePush {
	out := l.pushes
	l.pushes = nil
	return out
}

func f(v float64) core.Value[float64] { return core.Some(v, t0) }

// charging is a charge detected at t0, at soc % of a 60 kWh battery, read at.
func charging(at time.Time, soc float64) core.ChargeInProgress {
	return core.ChargeInProgress{
		Charge: core.Charge{
			DetectedAt: t0, Start: core.Bounds{After: t0.Add(-time.Minute), Before: t0},
			End:    core.Bounds{After: at, Before: at},
			EndSoC: f(soc), TargetSoC: f(80), EnergySoCKWh: f((soc - 40) * 0.6),
			Capacity: core.Some(core.Capacity{KWh: 60}, t0),
		},
		PowerW: f(11000),
	}
}

func inProgress(c core.ChargeInProgress) core.Result {
	return core.Result{InProgress: core.Some(c, c.End.Before)}
}

// TestLive: a charge started on the devices that gave a token for it, once; updated at
// each new reading of the activities whose token the app registered, a minute apart at
// best; ended with its final figures once over, and forgotten.
func TestLive(t *testing.T) {
	st := &memLive{memStore: memStore{devices: []Device{
		{ID: "d1", Platform: APNs, Token: "tok", LiveActivityToken: "start1"},
		{ID: "d2", Platform: FCM, Token: "tok2"}, // gave none: never started
	}}}
	n, _, clk := newNotifier(&st.memStore)
	n.st = st
	s := &liveSender{}
	n.SetSender(s)
	ctx := t.Context()
	charge := EventID(t0)

	live := func(res core.Result) []LivePush {
		t.Helper()
		if err := n.Live(ctx, "a", "v", res); err != nil {
			t.Fatal(err)
		}
		return s.take()
	}

	p := live(inProgress(charging(t0, 50)))
	if len(p) != 1 || p[0].Event != LiveStart || p[0].ChargeID != charge || len(p[0].Devices) != 1 || p[0].Devices[0].LiveActivityToken != "start1" {
		t.Fatalf("start: %+v", p)
	}
	if st := p[0].State; st.Status != LiveCharging || *st.SoCPct != 50 || *st.TargetSoCPct != 80 || *st.PowerW != 11000 || *st.EnergyKWh != 6 {
		t.Errorf("start's state: %+v", st)
	}
	// 30 % of 60 kWh at 11 kW: 1 h 38 min.
	if want := t0.Add(time.Duration(hours(18.0 / 11))).Unix(); *p[0].State.EndsAt != want {
		t.Errorf("ends at %d, want %d", *p[0].State.EndsAt, want)
	}

	// The same reading again, then a new one: never started twice, and no token yet.
	clk.Advance(time.Minute)
	if p := live(inProgress(charging(t0, 50))); len(p) != 0 {
		t.Errorf("again: %+v", p)
	}
	if p := live(inProgress(charging(t0.Add(time.Minute), 52))); len(p) != 0 {
		t.Errorf("before the token: %+v", p)
	}

	// The app registered the activity's token; another device started one itself.
	st.register("d1", "v", charge, "act1")
	st.register("d2", "v", charge, "act2")
	clk.Advance(time.Minute)
	p = live(inProgress(charging(t0.Add(2*time.Minute), 54)))
	if len(p) != 1 || p[0].Event != LiveUpdate || len(p[0].Activities) != 2 || *p[0].State.SoCPct != 54 {
		t.Fatalf("update: %+v", p)
	}
	// The same reading: nothing. A new one too soon: nothing.
	clk.Advance(time.Minute)
	if p := live(inProgress(charging(t0.Add(2*time.Minute), 54))); len(p) != 0 {
		t.Errorf("same reading: %+v", p)
	}
	clk.Advance(-50 * time.Second) // 10 s after the update
	if p := live(inProgress(charging(t0.Add(3*time.Minute), 55))); len(p) != 0 {
		t.Errorf("too soon: %+v", p)
	}
	clk.Advance(50 * time.Second)

	// d2 dismissed its activity: told nothing more.
	st.register("d2", "v", charge, "")
	p = live(inProgress(charging(t0.Add(4*time.Minute), 56)))
	if len(p) != 1 || len(p[0].Activities) != 1 || p[0].Activities[0].Token != "act1" {
		t.Fatalf("after the dismissal: %+v", p)
	}

	// Over: ended with the charge's final figures, then forgotten.
	over := charging(t0.Add(90*time.Minute), 80).Charge
	clk.Advance(90 * time.Minute)
	p = live(core.Result{Charges: []core.Charge{over}})
	if len(p) != 1 || p[0].Event != LiveEnd || len(p[0].Activities) != 1 || p[0].State.Status != LiveFinished || *p[0].State.SoCPct != 80 || p[0].State.PowerW != nil {
		t.Fatalf("end: %+v", p)
	}
	if len(st.acts) != 0 {
		t.Errorf("kept %+v", st.acts)
	}
	if p := live(core.Result{}); len(p) != 0 {
		t.Errorf("after the end: %+v", p)
	}
}

// TestLiveNextCharge: a charge over unseen ends with the figures last told, and the next
// one starts at once on the device.
func TestLiveNextCharge(t *testing.T) {
	st := &memLive{memStore: memStore{devices: []Device{{ID: "d1", Platform: APNs, Token: "tok", LiveActivityToken: "start1"}}}}
	n, _, clk := newNotifier(&st.memStore)
	n.st = st
	s := &liveSender{}
	n.SetSender(s)
	old := "2026-11-19T18:00:00.000000Z"
	soc := 70.0
	st.acts = []LiveActivity{{DeviceID: "d1", VehicleID: "v", ChargeID: old, Token: "act0", State: ChargeState{Status: LiveCharging, SoCPct: &soc, ReadAt: 1}}}
	clk.Advance(time.Minute)
	if err := n.Live(t.Context(), "a", "v", inProgress(charging(t0, 45))); err != nil {
		t.Fatal(err)
	}
	p := s.take()
	if len(p) != 2 || p[0].Event != LiveEnd || p[0].ChargeID != old || p[0].State.Status != LiveEnded || *p[0].State.SoCPct != 70 ||
		p[1].Event != LiveStart || p[1].ChargeID != EventID(t0) {
		t.Fatalf("pushes %+v", p)
	}
	if len(st.acts) != 1 || st.acts[0].ChargeID != EventID(t0) || st.acts[0].Token != "" {
		t.Errorf("activities %+v", st.acts)
	}
}

// TestLiveTurnedOff: the vehicle's charges turned off (ChargeLiveActivity): none starts,
// and those shown end as if the charge were over.
func TestLiveTurnedOff(t *testing.T) {
	st := &memLive{memStore: memStore{devices: []Device{{ID: "d1", Platform: APNs, Token: "tok", LiveActivityToken: "start1"}}}}
	n, _, clk := newNotifier(&st.memStore)
	n.st = st
	s := &liveSender{}
	n.SetSender(s)
	off := false
	st.prefs = Preferences{Vehicles: map[string]map[Kind]bool{"v": {ChargeLiveActivity: off}}}
	if err := n.Live(t.Context(), "a", "v", inProgress(charging(t0, 50))); err != nil || len(s.take()) != 0 || len(st.acts) != 0 {
		t.Fatalf("started: %v %+v", err, st.acts)
	}
	// Another vehicle follows the default: on.
	if err := n.Live(t.Context(), "a", "w", inProgress(charging(t0, 50))); err != nil || len(s.take()) != 1 {
		t.Errorf("another vehicle: %v", err)
	}
	st.acts = []LiveActivity{{DeviceID: "d1", VehicleID: "v", ChargeID: EventID(t0), Token: "act1"}}
	clk.Advance(time.Minute)
	s.take()
	if err := n.Live(t.Context(), "a", "v", inProgress(charging(t0.Add(time.Minute), 52))); err != nil {
		t.Fatal(err)
	}
	if p := s.take(); len(p) != 1 || p[0].Event != LiveEnd || slices.ContainsFunc(st.acts, func(a LiveActivity) bool { return a.VehicleID == "v" }) {
		t.Errorf("turned off while shown: %+v %+v", p, st.acts)
	}
}

// TestLiveNothing: no sender of activities, a reconstructed charge or readings long
// stopped: nothing read, nothing sent.
func TestLiveNothing(t *testing.T) {
	st := &memLive{memStore: memStore{devices: []Device{{ID: "d1", Platform: APNs, Token: "tok", LiveActivityToken: "start1"}}}}
	n, plain, clk := newNotifier(&st.memStore)
	n.st = st
	if err := n.Live(t.Context(), "a", "v", inProgress(charging(t0, 50))); err != nil || len(st.acts) != 0 || len(plain.sent) != 0 {
		t.Errorf("a sender of notifications only: %v %+v", err, st.acts)
	}
	s := &liveSender{}
	n.SetSender(s)
	rebuilt := charging(t0, 50)
	rebuilt.Reconstructed = true
	clk.Advance(2 * Fresh)
	for name, res := range map[string]core.Result{"reconstructed": inProgress(rebuilt), "stale": inProgress(charging(t0, 50))} {
		if err := n.Live(t.Context(), "a", "v", res); err != nil || len(s.take()) != 0 || len(st.acts) != 0 {
			t.Errorf("%s: %v %+v", name, err, st.acts)
		}
	}
}

// TestChargeStates: a charge paused has neither power nor end; one without what the
// estimate rests on has no end; one over below its limit is interrupted.
func TestChargeStates(t *testing.T) {
	paused := charging(t0, 50)
	paused.End.Before = t0.Add(5 * time.Minute)
	if s := InProgressState(paused); s.Status != LivePaused || s.PowerW != nil || s.EndsAt != nil || s.ReadAt != paused.End.Before.Unix() {
		t.Errorf("paused: %+v", s)
	}
	unknown := charging(t0, 50)
	unknown.Capacity = core.Value[core.Capacity]{}
	unknown.EnergySoCKWh = core.Value[float64]{}
	if s := InProgressState(unknown); s.EndsAt != nil || s.EnergyKWh != nil {
		t.Errorf("without a capacity: %+v", s)
	}
	measured := charging(t0, 50)
	measured.EnergyMeasuredKWh = f(5.5)
	if s := InProgressState(measured); *s.EnergyKWh != 5.5 {
		t.Errorf("measured: %+v", s)
	}
	if s := EndedState(charging(t0, 60).Charge); s.Status != LiveInterrupted || s.EndsAt != nil {
		t.Errorf("interrupted: %+v", s)
	}
	if s := EndedState(charging(t0, 79).Charge); s.Status != LiveFinished {
		t.Errorf("finished within the margin: %+v", s)
	}
}

func hours(h float64) float64 { return h * float64(time.Hour) }
