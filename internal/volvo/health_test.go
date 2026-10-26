package volvo

import (
	"reflect"
	"testing"
	"time"

	"runsten/internal/core"
)

func TestNormalizeHealth(t *testing.T) {
	at := timestamp
	tyresAt, diagAt := at("2026-09-25T15:37:23.223565206Z"), at("2026-09-25T15:37:22.628157678Z")
	brakesAt, engineAt := at("2026-09-25T15:37:22.832778430Z"), at("2026-09-25T15:37:22.513699531Z")
	lightsAt := at("2026-09-25T15:37:23.351971383Z")

	tests := []struct {
		ep   Endpoint
		file string
		want core.Health
	}{
		{Tyres, "cv-tyres.json", core.Health{
			Covers: core.HealthTyres,
			// frontLeft is UNSPECIFIED: unknown.
			Tyres: map[core.Wheel]core.Tyre{
				core.FrontRight: {Warning: core.Some(core.TyreOK, tyresAt)},
				core.RearLeft:   {Warning: core.Some(core.TyreLow, tyresAt)},
				core.RearRight:  {Warning: core.Some(core.TyreHigh, tyresAt)},
			},
		}},
		{Diagnostics, "cv-diagnostics.json", core.Health{
			Covers:              core.HealthService,
			Service:             core.Some(core.ServiceDue, diagAt),
			DistanceToServiceKm: core.Some(30.0, diagAt),
			MonthsToService:     core.Some(1.0, diagAt),
			// UNSPECIFIED: reported, unknown.
			Checks: map[core.Check]core.Value[bool]{core.CheckWasherFluid: {}},
		}},
		{Brakes, "cv-brakes.json", core.Health{
			Checks: map[core.Check]core.Value[bool]{core.CheckBrakeFluid: core.Some(false, brakesAt)},
		}},
		{Engine, "cv-engine.json", core.Health{
			Checks: map[core.Check]core.Value[bool]{
				core.CheckCoolant: core.Some(true, engineAt), core.CheckOil: core.Some(true, engineAt),
			},
		}},
		{Details, "cv-vehicle.json", core.Health{}},
	}
	for _, tt := range tests {
		t.Run(string(tt.ep), func(t *testing.T) {
			got, err := NormalizeHealth(tt.ep, fixture(t, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}

	// The warnings: every light the vehicle reports, brakeLightLeft UNSPECIFIED.
	h, err := NormalizeHealth(Warnings, fixture(t, "cv-warnings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if h.Covers != 0 || len(h.Checks) != len(lights) || h.Checks[core.CheckLightBrakeRight] != core.Some(true, lightsAt) ||
		h.Checks[core.CheckLightHazard] != core.Some(false, lightsAt) {
		t.Errorf("warnings: %+v", h)
	}
	if v, ok := h.Checks[core.CheckLightBrakeLeft]; !ok || v.OK {
		t.Errorf("an unspecified light: %+v, %t; want reported, unknown", v, ok)
	}

	for _, ep := range HealthEndpoints() {
		if _, err := NormalizeHealth(ep, []byte("[")); err == nil {
			t.Errorf("%s: invalid JSON accepted", ep)
		}
	}
}

func TestHealthNormalizer(t *testing.T) {
	var n Normalizer
	if got := n.HealthKinds(); len(got) != 5 || got[0] != (core.Kind{Source: Source, Endpoint: "tyres"}) {
		t.Errorf("health kinds %v", got)
	}
	if _, err := n.NormalizeHealth(core.Kind{Source: "polestar", Endpoint: "tyres"}, nil); err == nil {
		t.Error("another source's health read")
	}
	if h, err := n.NormalizeHealth(core.Kind{Source: Source, Endpoint: "brakes"}, fixture(t, "cv-brakes.json")); err != nil || len(h.Checks) != 1 {
		t.Errorf("brakes: %+v, %v", h, err)
	}
	for _, tt := range []struct {
		unit         string
		days, months core.Value[float64]
	}{
		{"days", core.Some(3.0, time.Time{}), core.Value[float64]{}},
		{"weeks", core.Some(21.0, time.Time{}), core.Value[float64]{}},
		{"months", core.Value[float64]{}, core.Some(3.0, time.Time{})},
		{"years", core.Value[float64]{}, core.Value[float64]{}},
	} {
		raw := []byte(`{"data": {"timeToService": {"unit": "` + tt.unit + `", "value": 3}}}`)
		h, err := NormalizeHealth(Diagnostics, raw)
		if err != nil || h.DaysToService != tt.days || h.MonthsToService != tt.months || h.Service.OK {
			t.Errorf("%s: %+v, %v", tt.unit, h, err)
		}
	}
}
