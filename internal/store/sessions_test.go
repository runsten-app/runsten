package store

import (
	"context"
	"testing"
	"time"

	"runsten/internal/core"
)

// TestChargeSessions checks that a charge reads the energy drawn from the grid of the
// sessions whose middle falls within its window, summed, and that a later save of a
// session replaces it.
func TestChargeSessions(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "WBA00000000000001")
	charge := func(at time.Time) core.Charge {
		return core.Charge{
			DetectedAt: at, Sources: []string{"bmw"},
			Start:    core.Bounds{After: at, Before: at.Add(10 * time.Minute)},
			End:      core.Bounds{After: at.Add(3 * time.Hour), Before: at.Add(4 * time.Hour)},
			StartSoC: some(40), EndSoC: some(80),
		}
	}
	res := core.Result{
		Charges: []core.Charge{charge(t0), charge(t0.Add(24 * time.Hour)), charge(t0.Add(48 * time.Hour))},
		Cursor:  core.Cursor{Settled: t0.Add(72 * time.Hour), From: t0},
	}
	if err := s.SaveDerivation(ctx, a, v, time.Time{}, res); err != nil {
		t.Fatal(err)
	}
	session := func(start time.Time, d time.Duration, kwh float64) core.ChargeSession {
		return core.ChargeSession{Start: start, End: start.Add(d), EnergyGridKWh: some(kwh), StartSoC: some(40), EndSoC: some(80)}
	}
	sessions := []core.ChargeSession{
		// The first charge: two sessions, a pause between them.
		session(t0.Add(5*time.Minute), time.Hour, 10),
		session(t0.Add(2*time.Hour), time.Hour, 12),
		// The second: its middle out of the window, though it overlaps it.
		session(t0.Add(26*time.Hour), 6*time.Hour, 30),
		// The third: corrected below.
		session(t0.Add(48*time.Hour+time.Minute), 3*time.Hour, 40),
	}
	if err := s.SaveChargeSessions(ctx, a, v, "bmw", sessions); err != nil {
		t.Fatal(err)
	}
	// A later response corrects the third, twice in one save: the last wins.
	fix := []core.ChargeSession{session(t0.Add(48*time.Hour+time.Minute), 3*time.Hour, 41), session(t0.Add(48*time.Hour+time.Minute), 3*time.Hour, 42)}
	if err := s.SaveChargeSessions(ctx, a, v, "bmw", fix); err != nil {
		t.Fatal(err)
	}
	charges, err := s.Charges(ctx, a, v)
	if err != nil || len(charges) != 3 {
		t.Fatalf("charges = %+v, %v", charges, err)
	}
	for i, want := range []core.Value[float64]{some(22), {}, some(42)} {
		if got := charges[i].EnergyGridKWh; got != want {
			t.Errorf("charge %d: energy from the grid %+v, want %+v", i, got, want)
		}
	}

	// Another account sees none of them.
	b, w := newVehicle(t, s, "token-b", "WBA00000000000002")
	if err := s.SaveDerivation(ctx, b, w, time.Time{}, core.Result{Charges: []core.Charge{charge(t0)}, Cursor: res.Cursor}); err != nil {
		t.Fatal(err)
	}
	if charges, err := s.Charges(ctx, b, w); err != nil || charges[0].EnergyGridKWh.OK {
		t.Errorf("another account's charge = %+v, %v", charges, err)
	}
	// A session ending before it starts is refused.
	bad := core.ChargeSession{Start: t0, End: t0.Add(-time.Minute)}
	if err := s.SaveChargeSessions(ctx, a, v, "bmw", []core.ChargeSession{bad}); err == nil {
		t.Error("a session ending before its start accepted")
	}
}
