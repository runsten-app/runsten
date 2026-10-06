package store

import (
	"context"
	"testing"
	"time"

	"runsten/internal/collector"
)

func TestLastPass(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	last := func() (int, time.Time) {
		t.Helper()
		n, at, err := s.LastPass(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n, at
	}
	if n, at := last(); n != 0 || !at.IsZero() {
		t.Errorf("empty: %d, %v", n, at)
	}
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if n, at := last(); n != 2 || !at.IsZero() {
		t.Errorf("before any pass: %d, %v", n, at)
	}
	t0 := time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC)
	// Across the accounts: the latest of every vehicle's.
	for _, st := range []collector.Status{
		{AccountID: a, VehicleID: va, PassedAt: t0.Add(time.Minute), Mode: "parked"},
		{AccountID: b, VehicleID: vb, PassedAt: t0, Mode: "parked"},
	} {
		if err := s.SaveStatus(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	if n, at := last(); n != 2 || !at.Equal(t0.Add(time.Minute)) {
		t.Errorf("after the passes: %d, %v", n, at)
	}
}

func TestConnections(t *testing.T) {
	s, _ := testStore(t)
	open, maxConns, err := s.Connections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if open < 1 || maxConns < open {
		t.Errorf("open %d of %d", open, maxConns)
	}
}
