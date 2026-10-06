package main

import (
	"context"
	"errors"
	"testing"

	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/publish"
)

func TestModel(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	ex30 := core.Snapshot{
		Covers: core.FieldCapacity | core.FieldModel, CapacityKWh: some(69.0),
		Family: some("EX30"), ModelYear: some(2024), BatteryElectric: some(true),
	}
	er, _ := cat.Variant("ex30-er-2024")
	tests := []struct {
		name   string
		vin    string
		chosen string
		snap   core.Snapshot
		want   publish.Model
	}{
		{"recognized", "YV1EL3AV0R2000001", "", ex30, publish.Model{Variant: er.Name, Family: "EX30", Brand: "Volvo", Year: 2024}},
		{"several fit: the family", "YV1SMLT0000DT0001", "", ex30, publish.Model{Family: "EX30", Year: 2024}},
		{"chosen", "YV1SMLT0000DT0001", "ex30-er-2024", ex30, publish.Model{Variant: er.Name, Family: "EX30", Brand: "Volvo", Year: 2024}},
		{"details never read", "YV1EL3AV0R2000001", "", core.Snapshot{}, publish.Model{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := models{catalog: cat, vehicle: func(context.Context, string, string) (string, string, bool, error) {
				return tt.vin, tt.chosen, true, nil
			}}
			got, err := m.Model(context.Background(), "a", "v", core.Latest([]core.Record{{Snapshot: tt.snap}}))
			if err != nil || got != tt.want {
				t.Errorf("model = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}

	boom := errors.New("boom")
	for name, vehicle := range map[string]func(context.Context, string, string) (string, string, bool, error){
		"store error":  func(context.Context, string, string) (string, string, bool, error) { return "", "", false, boom },
		"vehicle gone": func(context.Context, string, string) (string, string, bool, error) { return "", "", false, nil },
	} {
		if _, err := (models{catalog: cat, vehicle: vehicle}).Model(context.Background(), "a", "v", core.Current{}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
