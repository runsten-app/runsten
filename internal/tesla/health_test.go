package tesla

import (
	"reflect"
	"testing"
	"time"

	"runsten/internal/core"
)

func TestNormalizeHealthFixture(t *testing.T) {
	at := time.UnixMilli(1793032171549).UTC()
	// The sensors last told their pressures three days before the response.
	seen, seenRL := time.Unix(1792789370, 0).UTC(), time.Unix(1792789400, 0).UTC()
	ok := core.Some(core.TyreOK, at)
	want := core.Health{
		Covers: core.HealthTyres,
		Tyres: map[core.Wheel]core.Tyre{
			core.FrontLeft:  {PressureKPa: core.Some(bar(2.7), seen), Warning: ok},
			core.FrontRight: {PressureKPa: core.Some(bar(2.75), seen), Warning: ok},
			core.RearLeft:   {PressureKPa: core.Some(bar(2.7), seenRL), Warning: ok},
			core.RearRight:  {PressureKPa: core.Some(bar(2.7), seen), Warning: ok},
		},
		RecommendedFrontKPa: core.Some(bar(2.9), at),
		RecommendedRearKPa:  core.Some(bar(2.9), at),
	}
	n := Normalizer{}
	got, err := n.NormalizeHealth(core.Kind{Source: Source, Endpoint: string(VehicleData)}, fixture(t, "vehicle_data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if _, err := n.NormalizeHealth(core.Kind{Source: "polestar", Endpoint: string(VehicleData)}, nil); err == nil {
		t.Error("another source: no error")
	}
}

func TestNormalizeHealth(t *testing.T) {
	at := time.UnixMilli(1793032171549).UTC()
	tests := []struct {
		name, body string
		want       core.Health
	}{
		{"warnings", `"tpms_pressure_fl": 1.9, "tpms_soft_warning_fl": true, "tpms_hard_warning_fl": false,
			"tpms_soft_warning_fr": true, "tpms_hard_warning_fr": true, "tpms_pressure_rl": null`, core.Health{
			Covers: core.HealthTyres,
			Tyres: map[core.Wheel]core.Tyre{
				// Without its last time seen, the block's.
				core.FrontLeft:  {PressureKPa: core.Some(bar(1.9), at), Warning: core.Some(core.TyreLow, at)},
				core.FrontRight: {Warning: core.Some(core.TyreVeryLow, at)},
			},
		}},
		{"recommended only", `"tpms_rcp_rear_value": 3.1`, core.Health{
			Covers: core.HealthTyres, Tyres: map[core.Wheel]core.Tyre{}, RecommendedRearKPa: core.Some(bar(3.1), at),
		}},
		{"no tyre", `"odometer": 10`, core.Health{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"response": {"vehicle_state": {` + tc.body + `, "timestamp": 1793032171549}}}`)
			got, err := NormalizeHealth(VehicleData, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
	for name, raw := range map[string]string{"no block": `{"response": {}}`, "408": `{"response": null}`} {
		if h, err := NormalizeHealth(VehicleData, []byte(raw)); err != nil || h.Covers != 0 {
			t.Errorf("%s: %+v, %v", name, h, err)
		}
	}
	if h, err := NormalizeHealth(VehicleState, fixture(t, "vehicle.json")); err != nil || h.Covers != 0 {
		t.Errorf("vehicle: %+v, %v", h, err)
	}
	if _, err := NormalizeHealth(VehicleData, []byte(`[`)); err == nil {
		t.Error("invalid JSON: no error")
	}
}
