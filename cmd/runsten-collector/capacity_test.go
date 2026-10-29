package main

import (
	"context"
	"errors"
	"testing"

	"runsten/internal/catalog"
	"runsten/internal/core"
)

func some[T any](v T) core.Value[T] { return core.Value[T]{V: v, OK: true} }

func TestNetCapacity(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	ex30 := core.Snapshot{
		Covers: core.FieldCapacity | core.FieldModel, CapacityKWh: some(69.0),
		Family: some("EX30"), ModelYear: some(2024), Powertrain: some(core.Electric),
	}
	hybrid := ex30
	hybrid.Powertrain = some(core.PlugInHybrid)
	// The Data Portal tells the brand, never the family: the variant chosen sets it.
	polestar2 := core.Snapshot{
		Covers: core.FieldModel, Brand: some("polestar"), ModelYear: some(2024), Powertrain: some(core.Electric),
	}
	tests := []struct {
		name   string
		vin    string
		chosen string
		snap   core.Snapshot
		want   float64 // 0: unknown
	}{
		{"recognized: its net capacity", "YV12ZEL9XRS000001", "", ex30, 64},
		// Two EX30 have 69 kWh: without the VIN's motor code, nothing is recognized.
		{"several variants fit", "YV1SMLT0000DT0001", "", ex30, 0},
		{"chosen where nothing is recognized", "YV1SMLT0000DT0001", "ex30-er-2024", ex30, 64},
		{"chosen over the recognition", "YV12ZEL9XRS000001", "ex30-lfp-2024", ex30, 49},
		{"a choice of another family is ignored", "YV12ZEL9XRS000001", "xc40-twin-2021", ex30, 64},
		{"not battery electric", "YV12ZEL9XRS000001", "ex30-er-2024", hybrid, 0},
		{"details never read", "YV12ZEL9XRS000001", "", core.Snapshot{}, 0},
		{"a family unknown: the variant of the brand chosen", "LPSVSEDE5RL000001", "ps2-sr-2024", polestar2, 66},
		{"a family unknown: a variant of another brand", "LPSVSEDE5RL000001", "ex30-er-2024", polestar2, 0},
		{"a family unknown, nothing chosen", "LPSVSEDE5RL000001", "", polestar2, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := capacities{catalog: cat, vehicle: func(context.Context, string, string) (string, string, bool, error) {
				return tt.vin, tt.chosen, true, nil
			}}
			net, err := c.NetCapacity(context.Background(), "a", "v")
			if err != nil {
				t.Fatal(err)
			}
			if kwh, ok := net(tt.snap); kwh != tt.want || ok != (tt.want != 0) {
				t.Errorf("net capacity %v, %v; want %v", kwh, ok, tt.want)
			}
		})
	}

	boom := errors.New("boom")
	for name, vehicle := range map[string]func(context.Context, string, string) (string, string, bool, error){
		"store error":  func(context.Context, string, string) (string, string, bool, error) { return "", "", false, boom },
		"vehicle gone": func(context.Context, string, string) (string, string, bool, error) { return "", "", false, nil },
	} {
		if _, err := (capacities{catalog: cat, vehicle: vehicle}).NetCapacity(context.Background(), "a", "v"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
