package polestar

import (
	"reflect"
	"testing"
	"time"

	"runsten/internal/core"
)

func TestNormalizeHealth(t *testing.T) {
	// The recorded Polestar 2 reports no tyre: they stay unknown.
	at := time.Unix(1789729423, 41718424).UTC()
	h, err := NormalizeHealth(Health, fixture(t, "telemetry/health.json"))
	if err != nil {
		t.Fatal(err)
	}
	if h.Covers != core.HealthService || h.Tyres != nil {
		t.Errorf("covers %b, tyres %v", h.Covers, h.Tyres)
	}
	if h.DaysToService != core.Some(313.0, at) || h.DistanceToServiceKm != core.Some(4150.0, at) ||
		h.Service != core.Some(core.ServiceOK, at) || h.MonthsToService.OK {
		t.Errorf("service %+v %+v %+v %+v", h.Service, h.DaysToService, h.DistanceToServiceKm, h.MonthsToService)
	}
	// Five fluids and the battery, nineteen lights, none warning.
	if len(h.Checks) != 5+len(lights) {
		t.Errorf("%d checks: %v", len(h.Checks), h.Checks)
	}
	for c, v := range h.Checks {
		if v != core.Some(false, at) {
			t.Errorf("%s: %+v", c, v)
		}
	}

	// The assumed fields of the tyres, and warnings.
	raw := []byte(`{"data": {
		"timestamp": {"seconds": "1789729423"},
		"frontLeftTyrePressureKpa": 282, "frontRightTyrePressureKpa": 280, "rearLeftTyrePressureKpa": 241,
		"recommendedFrontTyrePressureKpa": 280, "recommendedRearTyrePressureKpa": 290,
		"tyrePressureFrontLeftWarning": "TYRE_PRESSURE_WARNING_NO_WARNING",
		"tyrePressureRearLeftWarning": "TYRE_PRESSURE_WARNING_LOW",
		"tyrePressureRearRightWarning": "TYRE_PRESSURE_WARNING_SYSTEM_FAULT",
		"serviceWarning": "SERVICE_WARNING_DISTANCE_DRIVEN_OVERDUE_FOR_SERVICE",
		"washerFluidLevelWarning": "WASHER_FLUID_LEVEL_WARNING_TOO_LOW",
		"oilLevelWarning": "OIL_LEVEL_WARNING_UNSPECIFIED",
		"lightWarnings": {"brakeLightLeft": "EXTERIOR_LIGHT_WARNING_BULB_FAILURE", "unknownLight": "EXTERIOR_LIGHT_WARNING_FAILURE"}
	}}`)
	at = time.Unix(1789729423, 0).UTC()
	h, err = NormalizeHealth(Health, raw)
	if err != nil {
		t.Fatal(err)
	}
	want := core.Health{
		Covers: core.HealthTyres | core.HealthService,
		Tyres: map[core.Wheel]core.Tyre{
			core.FrontLeft:  {PressureKPa: core.Some(282.0, at), Warning: core.Some(core.TyreOK, at)},
			core.FrontRight: {PressureKPa: core.Some(280.0, at)},
			core.RearLeft:   {PressureKPa: core.Some(241.0, at), Warning: core.Some(core.TyreLow, at)},
			core.RearRight:  {Warning: core.Some(core.TyreFault, at)},
		},
		RecommendedFrontKPa: core.Some(280.0, at),
		RecommendedRearKPa:  core.Some(290.0, at),
		Service:             core.Some(core.ServiceOverdue, at),
		Checks: map[core.Check]core.Value[bool]{
			core.CheckWasherFluid:    core.Some(true, at),
			core.CheckLightBrakeLeft: core.Some(true, at),
		},
	}
	if !reflect.DeepEqual(h, want) {
		t.Errorf("got  %+v\nwant %+v", h, want)
	}

	// Another domain tells nothing of the health.
	if h, err := NormalizeHealth(Battery, fixture(t, "telemetry/battery.json")); err != nil || h.Covers != 0 || h.Checks != nil {
		t.Errorf("battery: %+v, %v", h, err)
	}
}

func TestServiceState(t *testing.T) {
	for v, want := range map[string]core.Value[core.ServiceState]{
		"SERVICE_WARNING_NO_WARNING":                                  core.Some(core.ServiceOK, time.Time{}),
		"SERVICE_WARNING_REGULAR_MAINTENANCE_ALMOST_TIME_FOR_SERVICE": core.Some(core.ServiceSoon, time.Time{}),
		"SERVICE_WARNING_ENGINE_HOURS_TIME_FOR_SERVICE":               core.Some(core.ServiceDue, time.Time{}),
		"SERVICE_WARNING_DISTANCE_DRIVEN_OVERDUE_FOR_SERVICE":         core.Some(core.ServiceOverdue, time.Time{}),
		"SERVICE_WARNING_SOMETHING_NEW":                               core.Some(core.ServiceDue, time.Time{}),
		"SERVICE_WARNING_UNKNOWN_WARNING":                             {},
		"SERVICE_WARNING_UNSPECIFIED":                                 {},
		"":                                                            {},
		"OTHER_PREFIX_NO_WARNING":                                     {},
	} {
		if got := serviceState(v, "SERVICE_WARNING_", time.Time{}); got != want {
			t.Errorf("%q: %+v, want %+v", v, got, want)
		}
	}
}
