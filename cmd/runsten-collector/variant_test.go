package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
)

type deriverFunc func(ctx context.Context, accountID, vehicleID string) error

func (f deriverFunc) Update(ctx context.Context, accountID, vehicleID string) error {
	return f(ctx, accountID, vehicleID)
}

func TestVariants(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	ex30 := core.Snapshot{
		Covers: core.FieldCapacity | core.FieldModel, CapacityKWh: some(69.0),
		Family: some("EX30"), ModelYear: some(2024), BatteryElectric: some(true),
	}
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	boom := errors.New("boom")
	tests := []struct {
		name    string
		vin     string
		chosen  string
		snap    core.Snapshot
		derived error
		want    string
	}{
		{"recognized", "YV12ZEL9XRS000001", "", ex30, nil, "ex30-er-2024"},
		{"chosen over the recognition", "YV12ZEL9XRS000001", "ex30-lfp-2024", ex30, nil, "ex30-lfp-2024"},
		{"several fit: none", "YV1SMLT0000DT0001", "", ex30, nil, ""},
		{"details never read: none", "YV12ZEL9XRS000001", "", core.Snapshot{}, nil, ""},
		{"written when the derivation fails", "YV12ZEL9XRS000001", "", ex30, boom, "ex30-er-2024"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *string
			v := variants{
				Deriver: deriverFunc(func(context.Context, string, string) error { return tt.derived }),
				capacities: capacities{catalog: cat, vehicle: func(context.Context, string, string) (string, string, bool, error) {
					return tt.vin, tt.chosen, true, nil
				}},
				current: func(_ context.Context, _, _ string, at time.Time) (core.Current, error) {
					if !at.Equal(now) {
						t.Errorf("current at %v, want %v", at, now)
					}
					return core.Latest([]core.Record{{Snapshot: tt.snap}}), nil
				},
				set: func(_ context.Context, _, _, id string) error { got = &id; return nil },
				clk: clock.NewManual(now),
			}
			err := v.Update(context.Background(), "a", "v")
			if !errors.Is(err, tt.derived) || (tt.derived == nil && err != nil) {
				t.Errorf("update: %v, want %v", err, tt.derived)
			}
			if got == nil || *got != tt.want {
				t.Errorf("written %v, want %q", got, tt.want)
			}
		})
	}

	t.Run("vehicle gone: nothing written", func(t *testing.T) {
		v := variants{
			Deriver: deriverFunc(func(context.Context, string, string) error { return nil }),
			capacities: capacities{catalog: cat, vehicle: func(context.Context, string, string) (string, string, bool, error) {
				return "", "", false, nil
			}},
			set: func(context.Context, string, string, string) error { t.Error("written"); return nil },
			clk: clock.NewManual(now),
		}
		if err := v.Update(context.Background(), "a", "v"); err != nil {
			t.Error(err)
		}
	})
}
