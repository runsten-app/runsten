package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

type fakeHealthStore struct {
	open, maxConns int
	vehicles       int
	passedAt       time.Time
	err            error
}

func (f fakeHealthStore) Connections(context.Context) (int, int, error) {
	return f.open, f.maxConns, f.err
}

func (f fakeHealthStore) LastPass(context.Context) (int, time.Time, error) {
	return f.vehicles, f.passedAt, f.err
}

func TestHealthChecks(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	down := errors.New("dial tcp 10.0.0.1:5432: connection refused")
	for _, tt := range []struct {
		name   string
		check  string
		store  fakeHealthStore
		detail string
		ok     bool
	}{
		{"connections left", "database", fakeHealthStore{open: 79, maxConns: 100}, "79 of 100 connections", true},
		{"connections running out", "database", fakeHealthStore{open: 80, maxConns: 100}, "80 of 100 connections", false},
		{"database down", "database", fakeHealthStore{err: down}, "not reachable", false},
		{"no vehicle", "collector", fakeHealthStore{}, "no vehicle to read", true},
		{"no pass yet", "collector", fakeHealthStore{vehicles: 2}, "no pass yet", false},
		{"pass ahead of the clock", "collector", fakeHealthStore{vehicles: 2, passedAt: now.Add(time.Hour)}, "last pass 0s ago", true},
		{"recent pass", "collector", fakeHealthStore{vehicles: 2, passedAt: now.Add(-40 * time.Second)}, "last pass 40s ago", true},
		{"pass at the limit", "collector", fakeHealthStore{vehicles: 2, passedAt: now.Add(-collectorMaxAge)}, "last pass 15m0s ago", true},
		{"collector stopped", "collector", fakeHealthStore{vehicles: 2, passedAt: now.Add(-collectorMaxAge - time.Second)}, "last pass 15m1s ago", false},
		{"collector unknown", "collector", fakeHealthStore{err: down}, "database not reachable", false},
	} {
		detail, ok := healthChecks(tt.store, clock.NewManual(now))[tt.check](t.Context())
		if detail != tt.detail || ok != tt.ok {
			t.Errorf("%s: %q, %v; want %q, %v", tt.name, detail, ok, tt.detail, tt.ok)
		}
	}
}
