package store

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"runsten/internal/api"
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
		Charges: []core.Charge{charge(t0), charge(t0.Add(24 * time.Hour)), charge(t0.Add(48 * time.Hour)), charge(t0.Add(72 * time.Hour))},
		Cursor:  core.Cursor{Settled: t0.Add(96 * time.Hour), From: t0},
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
		// The fourth: one of its two sessions without an energy, so none for the charge,
		// rather than a part of it.
		session(t0.Add(72*time.Hour+5*time.Minute), time.Hour, 10),
		{Start: t0.Add(74 * time.Hour), End: t0.Add(75 * time.Hour)},
	}
	// Without a snapshot, the vehicle was never read: no cutoff, nothing imported.
	if err := s.SaveChargeSessions(ctx, a, v, "bmw", sessions, core.Value[core.Capacity]{}); err != nil {
		t.Fatal(err)
	}
	// A later response corrects the third, twice in one save: the last wins.
	fix := []core.ChargeSession{session(t0.Add(48*time.Hour+time.Minute), 3*time.Hour, 41), session(t0.Add(48*time.Hour+time.Minute), 3*time.Hour, 42)}
	if err := s.SaveChargeSessions(ctx, a, v, "bmw", fix, core.Value[core.Capacity]{}); err != nil {
		t.Fatal(err)
	}
	charges, err := s.Charges(ctx, a, v)
	if err != nil || len(charges) != 4 {
		t.Fatalf("charges = %+v, %v", charges, err)
	}
	for i, want := range []core.Value[float64]{some(22), {}, some(42), {}} {
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
	if err := s.SaveChargeSessions(ctx, a, v, "bmw", []core.ChargeSession{bad}, core.Value[core.Capacity]{}); err == nil {
		t.Error("a session ending before its start accepted")
	}
}

// TestChargeSessionsImported checks that the sessions that ended by the vehicle's first
// snapshot are imported as charges the first time they are recorded, and only then: a
// session in progress at that snapshot, or after it, is left to the derived charges,
// and an import deleted is not brought back by the next save.
func TestChargeSessionsImported(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "WBA00000000000001")
	first := t0.Add(30 * 24 * time.Hour)
	saveOdometer(t, s, a, v, first)
	capacity := core.Value[core.Capacity]{V: core.Capacity{KWh: 80, Source: core.CapacityCatalogNet}, OK: true}
	at := func(days float64) time.Time { return first.Add(time.Duration(days * 24 * float64(time.Hour))) }
	sessions := []core.ChargeSession{
		{
			Start: at(-20), End: at(-20).Add(5 * time.Hour), EnergyGridKWh: some(35.5), StartSoC: some(30), EndSoC: some(70),
			Position: core.Value[core.Position]{V: core.Position{Lat: 50.85, Lon: 4.35}, OK: true}, OdometerKm: some(12000),
		},
		// Ending at the first snapshot: imported. No state of charge, no energy from it.
		{Start: at(-1), End: first, EnergyGridKWh: some(8)},
		// In progress at the first snapshot: the derived charge's.
		{Start: first.Add(-time.Hour), End: first.Add(time.Hour), EnergyGridKWh: some(10)},
		// After it.
		{Start: at(2), End: at(2).Add(time.Hour), EnergyGridKWh: some(9)},
	}
	if err := s.SaveChargeSessions(ctx, a, v, "bmw", sessions, capacity); err != nil {
		t.Fatal(err)
	}
	imported := func() map[string]core.Charge {
		t.Helper()
		p, err := s.ListCharges(ctx, a, v, api.EventQuery{})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]core.Charge{}
		for _, c := range p.Charges {
			out[c.Import.V.SourceID] = c
		}
		return out
	}
	charges := imported()
	c, ok := charges[strconv.FormatInt(at(-20).Unix(), 10)]
	if len(charges) != 2 || !ok {
		t.Fatalf("charges = %+v", charges)
	}
	if !c.Import.OK || c.Import.V.Source != "bmw" || !slices.Equal(c.Sources, []string{"bmw"}) {
		t.Errorf("import = %+v, %v", c.Import, c.Sources)
	}
	if !c.Start.After.Equal(at(-20)) || !c.Start.Before.Equal(at(-20)) || !c.End.After.Equal(at(-20).Add(5*time.Hour)) {
		t.Errorf("times = %+v, %+v", c.Start, c.End)
	}
	if c.EnergyGridKWh != some(35.5) || c.EnergySoCKWh != some(32) || c.Capacity.V != capacity.V ||
		c.StartSoC != some(30) || c.EndSoC != some(70) || c.OdometerKm != some(12000) || c.Position.V.Lat != 50.85 {
		t.Errorf("charge = %+v", c)
	}
	if c.EnergyPowerKWh.OK || c.Type.OK {
		t.Errorf("an energy into the battery or a type the history does not tell: %+v", c)
	}
	if d := charges[strconv.FormatInt(at(-1).Unix(), 10)]; d.EnergyGridKWh != some(8) || d.EnergySoCKWh.OK || d.Capacity.OK {
		t.Errorf("second charge = %+v", d)
	}

	// The owner deletes the import: the next response, which tells the same sessions,
	// does not bring it back.
	if ok, err := s.DeleteImport(ctx, a, v, "bmw"); err != nil || !ok {
		t.Fatalf("delete = %v, %v", ok, err)
	}
	if err := s.SaveChargeSessions(ctx, a, v, "bmw", sessions, capacity); err != nil {
		t.Fatal(err)
	}
	if charges := imported(); len(charges) != 0 {
		t.Errorf("charges after deletion = %+v", charges)
	}
}
