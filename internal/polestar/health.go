package polestar

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"runsten/internal/core"
)

// NormalizeHealth converts a raw response of domain d into a health reading; a domain
// that tells nothing of it gives an empty one.
//
// Assumption: the tyres' fields are named after the community's reading of the Data
// Portal's documentation, never seen in a response (the Polestar 2 recorded reports
// none): the pressures <wheel>TyrePressureKpa, the recommended ones
// recommended<Front|Rear>TyrePressureKpa, the warnings tyrePressure<Wheel>Warning
// (TYRE_PRESSURE_WARNING_*). Corrected here, a rebuild needs nothing: the history is
// read from the stored responses.
func NormalizeHealth(d Domain, raw []byte) (core.Health, error) {
	if d != Health {
		return core.Health{}, nil
	}
	var v struct {
		Data struct {
			Timestamp *stamp `json:"timestamp"`

			FrontLeftKPa      *float64 `json:"frontLeftTyrePressureKpa"`
			FrontRightKPa     *float64 `json:"frontRightTyrePressureKpa"`
			RearLeftKPa       *float64 `json:"rearLeftTyrePressureKpa"`
			RearRightKPa      *float64 `json:"rearRightTyrePressureKpa"`
			RecommendedFront  *float64 `json:"recommendedFrontTyrePressureKpa"`
			RecommendedRear   *float64 `json:"recommendedRearTyrePressureKpa"`
			FrontLeftWarning  string   `json:"tyrePressureFrontLeftWarning"`
			FrontRightWarning string   `json:"tyrePressureFrontRightWarning"`
			RearLeftWarning   string   `json:"tyrePressureRearLeftWarning"`
			RearRightWarning  string   `json:"tyrePressureRearRightWarning"`

			DaysToService       *float64 `json:"daysToService"`
			DistanceToServiceKm *float64 `json:"distanceToServiceKm"`
			ServiceWarning      string   `json:"serviceWarning"`

			BrakeFluid        string            `json:"brakeFluidLevelWarning"`
			Coolant           string            `json:"engineCoolantLevelWarning"`
			Oil               string            `json:"oilLevelWarning"`
			WasherFluid       string            `json:"washerFluidLevelWarning"`
			LowVoltageBattery string            `json:"lowVoltageBatteryWarning"`
			Lights            map[string]string `json:"lightWarnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return core.Health{}, fmt.Errorf("normalize health %s: %w", d, err)
	}
	x, at := v.Data, v.Data.Timestamp.instant()
	var h core.Health

	tyres := map[core.Wheel]core.Tyre{}
	for w, t := range map[core.Wheel]struct {
		kPa     *float64
		warning string
	}{
		core.FrontLeft: {x.FrontLeftKPa, x.FrontLeftWarning}, core.FrontRight: {x.FrontRightKPa, x.FrontRightWarning},
		core.RearLeft: {x.RearLeftKPa, x.RearLeftWarning}, core.RearRight: {x.RearRightKPa, x.RearRightWarning},
	} {
		tyre := core.Tyre{PressureKPa: number(t.kPa, at), Warning: tyreWarning(t.warning, at)}
		if tyre.PressureKPa.OK || tyre.Warning.OK {
			tyres[w] = tyre
		}
	}
	// A model whose response holds no tyre at all does not report them: the reading
	// leaves them unknown rather than covering them with nothing.
	if len(tyres) > 0 || x.RecommendedFront != nil || x.RecommendedRear != nil {
		h.Covers |= core.HealthTyres
		h.Tyres = tyres
		h.RecommendedFrontKPa, h.RecommendedRearKPa = number(x.RecommendedFront, at), number(x.RecommendedRear, at)
	}

	h.Covers |= core.HealthService
	h.DaysToService = number(x.DaysToService, at)
	h.DistanceToServiceKm = number(x.DistanceToServiceKm, at)
	h.Service = serviceState(x.ServiceWarning, "SERVICE_WARNING_", at)

	h.Checks = map[core.Check]core.Value[bool]{}
	// A check the response holds stays, unknown when its value is: it replaces the one
	// before, and an aggregate of them is unknown rather than fine.
	check := func(c core.Check, value, prefix string) {
		if value != "" {
			h.Checks[c] = warning(value, prefix, at)
		}
	}
	check(core.CheckBrakeFluid, x.BrakeFluid, "BRAKE_FLUID_LEVEL_WARNING_")
	check(core.CheckCoolant, x.Coolant, "ENGINE_COOLANT_LEVEL_WARNING_")
	check(core.CheckOil, x.Oil, "OIL_LEVEL_WARNING_")
	check(core.CheckWasherFluid, x.WasherFluid, "WASHER_FLUID_LEVEL_WARNING_")
	check(core.CheckLowVoltageBattery, x.LowVoltageBattery, "LOW_VOLTAGE_BATTERY_WARNING_")
	for name, value := range x.Lights {
		if c, ok := lights[name]; ok {
			check(c, value, "EXTERIOR_LIGHT_WARNING_")
		}
	}
	return h, nil
}

// lights maps the keys of lightWarnings.
var lights = map[string]core.Check{
	"brakeLightCenter":         core.CheckLightBrakeCenter,
	"brakeLightLeft":           core.CheckLightBrakeLeft,
	"brakeLightRight":          core.CheckLightBrakeRight,
	"daytimeRunningLightLeft":  core.CheckLightDaytimeLeft,
	"daytimeRunningLightRight": core.CheckLightDaytimeRight,
	"fogLightFront":            core.CheckLightFogFront,
	"fogLightRear":             core.CheckLightFogRear,
	"highBeamLeft":             core.CheckLightHighBeamLeft,
	"highBeamRight":            core.CheckLightHighBeamRight,
	"lowBeamLeft":              core.CheckLightLowBeamLeft,
	"lowBeamRight":             core.CheckLightLowBeamRight,
	"positionLightFrontLeft":   core.CheckLightPositionFrontLeft,
	"positionLightFrontRight":  core.CheckLightPositionFrontRight,
	"positionLightRear":        core.CheckLightPositionRear,
	"registrationPlateLight":   core.CheckLightRegistrationPlate,
	"turnIndicatorFrontLeft":   core.CheckLightTurnFrontLeft,
	"turnIndicatorFrontRight":  core.CheckLightTurnFrontRight,
	"turnIndicatorRearLeft":    core.CheckLightTurnRearLeft,
	"turnIndicatorRearRight":   core.CheckLightTurnRearRight,
}

// warning reads a warning enum: NO_WARNING is none, the zero (UNSPECIFIED, left out)
// unknown, and every other value a warning (assumption: the documentation lists
// TOO_LOW, TOO_HIGH, FAILURE, BULB_FAILURE… and may add others).
func warning(v, prefix string, at time.Time) core.Value[bool] {
	name, ok := strings.CutPrefix(v, prefix)
	if !ok || name == "UNSPECIFIED" {
		return core.Value[bool]{}
	}
	return core.Some(name != "NO_WARNING", at)
}

func tyreWarning(v string, at time.Time) core.Value[core.TyreWarning] {
	name, ok := strings.CutPrefix(v, "TYRE_PRESSURE_WARNING_")
	if !ok || name == "UNSPECIFIED" {
		return core.Value[core.TyreWarning]{}
	}
	w, known := map[string]core.TyreWarning{
		"NO_WARNING": core.TyreOK, "LOW": core.TyreLow, "LOW_PRESSURE": core.TyreLow,
		"VERY_LOW": core.TyreVeryLow, "VERY_LOW_PRESSURE": core.TyreVeryLow,
		"HIGH": core.TyreHigh, "HIGH_PRESSURE": core.TyreHigh,
	}[name]
	if !known {
		w = core.TyreFault
	}
	return core.Some(w, at)
}

// serviceState reads a service warning: Volvo's words (REGULAR_MAINTENANCE_ALMOST_TIME_FOR_
// SERVICE, DISTANCE_DRIVEN_OVERDUE_FOR_SERVICE…), assumed to be the Data Portal's too.
// Another warning is taken as due; an unknown one (UNKNOWN_WARNING) as unknown.
func serviceState(v, prefix string, at time.Time) core.Value[core.ServiceState] {
	name, ok := strings.CutPrefix(v, prefix)
	switch {
	case !ok || name == "UNSPECIFIED" || name == "UNKNOWN_WARNING":
		return core.Value[core.ServiceState]{}
	case name == "NO_WARNING":
		return core.Some(core.ServiceOK, at)
	case strings.Contains(name, "OVERDUE"):
		return core.Some(core.ServiceOverdue, at)
	case strings.Contains(name, "ALMOST"):
		return core.Some(core.ServiceSoon, at)
	}
	return core.Some(core.ServiceDue, at)
}
