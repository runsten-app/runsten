package volvo

import (
	"encoding/json"
	"fmt"
	"strings"

	"runsten/internal/core"
)

// HealthEndpoints lists the endpoints NormalizeHealth reads: the collector reads them
// every rare interval, brakes and engine being optional.
func HealthEndpoints() []Endpoint { return []Endpoint{Tyres, Diagnostics, Warnings, Brakes, Engine} }

// HealthKinds returns the kinds of response NormalizeHealth reads.
func (Normalizer) HealthKinds() []core.Kind { return kinds(HealthEndpoints()...) }

// NormalizeHealth reads a stored response of kind k into a health reading.
func (Normalizer) NormalizeHealth(k core.Kind, raw []byte) (core.Health, error) {
	if k.Source != Source {
		return core.Health{}, fmt.Errorf("normalize health: source %q is not %s", k.Source, Source)
	}
	return NormalizeHealth(Endpoint(k.Endpoint), raw)
}

// NormalizeHealth converts a raw response of ep into a health reading; an endpoint that
// tells nothing of it gives an empty one. Each response is a map of Connected Vehicle
// values: a warning's NO_WARNING is none, UNSPECIFIED or missing unknown, and every
// other value a warning (TOO_LOW, TOO_HIGH, FAILURE, SERVICE_REQUIRED…).
func NormalizeHealth(ep Endpoint, raw []byte) (core.Health, error) {
	var h core.Health
	switch ep {
	case Tyres, Diagnostics, Warnings, Brakes, Engine:
	default:
		return h, nil
	}
	var v struct {
		Data map[string]*cvValue `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return core.Health{}, fmt.Errorf("normalize health %s: %w", ep, err)
	}
	d := v.Data
	check := func(c core.Check, key string) {
		if w := warning(d[key]); w.OK {
			if h.Checks == nil {
				h.Checks = map[core.Check]core.Value[bool]{}
			}
			h.Checks[c] = w
		}
	}
	switch ep {
	case Tyres:
		// The pressure monitoring's warning per wheel, never a pressure.
		tyres := map[core.Wheel]core.Tyre{}
		for key, w := range map[string]core.Wheel{
			"frontLeft": core.FrontLeft, "frontRight": core.FrontRight, "rearLeft": core.RearLeft, "rearRight": core.RearRight,
		} {
			if t := tyreWarning(d[key]); t.OK {
				tyres[w] = core.Tyre{Warning: t}
			}
		}
		if len(tyres) > 0 {
			h.Covers, h.Tyres = core.HealthTyres, tyres
		}
	case Diagnostics:
		h.Covers = core.HealthService
		h.Service = serviceState(d["serviceWarning"])
		h.DistanceToServiceKm = d["distanceToService"].number(km)
		h.DaysToService = d["timeToService"].number(days)
		h.MonthsToService = d["timeToService"].number(months)
		check(core.CheckWasherFluid, "washerFluidLevelWarning")
	case Brakes:
		check(core.CheckBrakeFluid, "brakeFluidLevelWarning")
	case Engine:
		check(core.CheckCoolant, "engineCoolantLevelWarning")
		check(core.CheckOil, "oilLevelWarning")
	case Warnings:
		for key, c := range lights {
			check(c, key)
		}
	}
	return h, nil
}

// lights maps the keys of the warnings endpoint.
var lights = map[string]core.Check{
	"brakeLightCenterWarning":         core.CheckLightBrakeCenter,
	"brakeLightLeftWarning":           core.CheckLightBrakeLeft,
	"brakeLightRightWarning":          core.CheckLightBrakeRight,
	"daytimeRunningLightLeftWarning":  core.CheckLightDaytimeLeft,
	"daytimeRunningLightRightWarning": core.CheckLightDaytimeRight,
	"fogLightFrontWarning":            core.CheckLightFogFront,
	"fogLightRearWarning":             core.CheckLightFogRear,
	"hazardLightsWarning":             core.CheckLightHazard,
	"highBeamLeftWarning":             core.CheckLightHighBeamLeft,
	"highBeamRightWarning":            core.CheckLightHighBeamRight,
	"lowBeamLeftWarning":              core.CheckLightLowBeamLeft,
	"lowBeamRightWarning":             core.CheckLightLowBeamRight,
	"positionLightFrontLeftWarning":   core.CheckLightPositionFrontLeft,
	"positionLightFrontRightWarning":  core.CheckLightPositionFrontRight,
	"positionLightRearLeftWarning":    core.CheckLightPositionRearLeft,
	"positionLightRearRightWarning":   core.CheckLightPositionRearRight,
	"registrationPlateLightWarning":   core.CheckLightRegistrationPlate,
	"reverseLightsWarning":            core.CheckLightReverse,
	"sideMarkLightsWarning":           core.CheckLightSideMark,
	"turnIndicationFrontLeftWarning":  core.CheckLightTurnFrontLeft,
	"turnIndicationFrontRightWarning": core.CheckLightTurnFrontRight,
	"turnIndicationRearLeftWarning":   core.CheckLightTurnRearLeft,
	"turnIndicationRearRightWarning":  core.CheckLightTurnRearRight,
}

// word reads a value's enum; empty when missing, null or UNSPECIFIED.
func (v *cvValue) word() string {
	if v == nil {
		return ""
	}
	s, ok := text(v.Value)
	if !ok || s == "UNSPECIFIED" {
		return ""
	}
	return s
}

func warning(v *cvValue) core.Value[bool] {
	w := v.word()
	if w == "" {
		return core.Value[bool]{}
	}
	return core.Some(w != "NO_WARNING", timestamp(v.Timestamp))
}

var tyreWarnings = map[string]core.TyreWarning{
	"NO_WARNING": core.TyreOK, "LOW_PRESSURE": core.TyreLow, "VERY_LOW_PRESSURE": core.TyreVeryLow,
	"HIGH_PRESSURE": core.TyreHigh,
}

func tyreWarning(v *cvValue) core.Value[core.TyreWarning] {
	w := v.word()
	if w == "" {
		return core.Value[core.TyreWarning]{}
	}
	t, ok := tyreWarnings[w]
	if !ok {
		t = core.TyreFault
	}
	return core.Some(t, timestamp(v.Timestamp))
}

// serviceState reads serviceWarning (REGULAR_MAINTENANCE_ALMOST_TIME_FOR_SERVICE,
// DISTANCE_DRIVEN_OVERDUE_FOR_SERVICE…); another warning is taken as due, an unknown
// one (UNKNOWN_WARNING) as unknown.
func serviceState(v *cvValue) core.Value[core.ServiceState] {
	w := v.word()
	var s core.ServiceState
	switch {
	case w == "" || w == "UNKNOWN_WARNING":
		return core.Value[core.ServiceState]{}
	case w == "NO_WARNING":
		s = core.ServiceOK
	case strings.Contains(w, "OVERDUE"):
		s = core.ServiceOverdue
	case strings.Contains(w, "ALMOST"):
		s = core.ServiceSoon
	default:
		s = core.ServiceDue
	}
	return core.Some(s, timestamp(v.Timestamp))
}

func days(v float64, unit string) (float64, bool) {
	switch unit {
	case "days":
		return v, true
	case "weeks":
		return v * 7, true
	}
	return 0, false
}

func months(v float64, unit string) (float64, bool) { return v, unit == "months" }
