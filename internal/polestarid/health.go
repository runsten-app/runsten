package polestarid

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"runsten/internal/core"
)

// healthBlock is the health of carTelematicsV2: the service and four warnings, no tyre.
type healthBlock struct {
	Timestamp           *stamp   `json:"timestamp"`
	DaysToService       *float64 `json:"daysToService"`
	DistanceToServiceKm *float64 `json:"distanceToServiceKm"`
	ServiceWarning      string   `json:"serviceWarning"`
	BrakeFluid          string   `json:"brakeFluidLevelWarning"`
	Coolant             string   `json:"engineCoolantLevelWarning"`
	Oil                 string   `json:"oilLevelWarning"`
}

// NormalizeHealth converts a raw response of ep into a health reading: the service and
// the warnings of the telematics. The tyres are not covered: the API does not give them,
// and they stay unknown rather than fine.
func NormalizeHealth(ep Endpoint, raw []byte) (core.Health, error) {
	if ep != Telematics {
		return core.Health{}, nil
	}
	var v telematics
	if err := json.Unmarshal(raw, &v); err != nil {
		return core.Health{}, fmt.Errorf("normalize health %s: %w", ep, err)
	}
	var h core.Health
	if len(v.Data.T.Health) == 0 {
		return h, nil
	}
	x := v.Data.T.Health[0]
	at := x.Timestamp.instant()
	h.Covers |= core.HealthService
	h.DaysToService = number(x.DaysToService, at)
	h.DistanceToServiceKm = number(x.DistanceToServiceKm, at)
	h.Service = serviceState(x.ServiceWarning, at)
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
	return h, nil
}

// warning reads a warning enum: NO_WARNING is none, UNSPECIFIED unknown, and every other
// value (TOO_LOW, TOO_HIGH, SERVICE_REQUIRED) a warning.
func warning(v, prefix string, at time.Time) core.Value[bool] {
	name, ok := strings.CutPrefix(v, prefix)
	if !ok || name == "UNSPECIFIED" {
		return core.Value[bool]{}
	}
	return core.Some(name != "NO_WARNING", at)
}

// serviceState reads serviceWarning: pypolestar's words (REGULAR_MAINTENANCE_ALMOST_TIME_
// FOR_SERVICE, DISTANCE_DRIVEN_OVERDUE_FOR_SERVICE, SERVICE_REQUIRED…). Another warning
// is taken as due.
func serviceState(v string, at time.Time) core.Value[core.ServiceState] {
	name, ok := strings.CutPrefix(v, "SERVICE_WARNING_")
	switch {
	case !ok || name == "UNSPECIFIED":
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
