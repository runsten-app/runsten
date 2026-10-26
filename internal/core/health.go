package core

import (
	"sort"
	"time"
)

// Wheel is the position of a tyre.
type Wheel string

// Wheels.
const (
	FrontLeft  Wheel = "front_left"
	FrontRight Wheel = "front_right"
	RearLeft   Wheel = "rear_left"
	RearRight  Wheel = "rear_right"
)

// Wheels returns the four positions, front first, left first.
func Wheels() []Wheel { return []Wheel{FrontLeft, FrontRight, RearLeft, RearRight} }

// Front reports whether the wheel is at the front, whose recommended pressure applies.
func (w Wheel) Front() bool { return w == FrontLeft || w == FrontRight }

// TyreWarning is what the vehicle's tyre pressure monitoring tells of a tyre.
type TyreWarning string

// Tyre warnings.
const (
	TyreOK      TyreWarning = "ok"
	TyreLow     TyreWarning = "low"
	TyreVeryLow TyreWarning = "very_low"
	TyreHigh    TyreWarning = "high"
	// TyreFault is any other warning of a tyre: a sensor or a system fault, or a word
	// of the vendor's not known here.
	TyreFault TyreWarning = "fault"
)

// Tyre is what a reading tells of one tyre.
type Tyre struct {
	PressureKPa Value[float64]
	Warning     Value[TyreWarning]
}

// ServiceState tells how close the next service is, as the vehicle warns of it.
type ServiceState string

// Service states.
const (
	ServiceOK      ServiceState = "ok"
	ServiceSoon    ServiceState = "soon" // almost time for it
	ServiceDue     ServiceState = "due"
	ServiceOverdue ServiceState = "overdue"
)

// Check is a part the vehicle watches and warns about, but the tyres and the service.
type Check string

// Checks: the fluids, the low voltage battery, and each exterior light. A source reports
// the ones it knows; the lights differ from a model to another.
const (
	CheckBrakeFluid        Check = "brake_fluid"
	CheckCoolant           Check = "coolant"
	CheckOil               Check = "oil"
	CheckWasherFluid       Check = "washer_fluid"
	CheckLowVoltageBattery Check = "low_voltage_battery"

	CheckLightBrakeCenter        Check = "light_brake_center"
	CheckLightBrakeLeft          Check = "light_brake_left"
	CheckLightBrakeRight         Check = "light_brake_right"
	CheckLightDaytimeLeft        Check = "light_daytime_left"
	CheckLightDaytimeRight       Check = "light_daytime_right"
	CheckLightFogFront           Check = "light_fog_front"
	CheckLightFogRear            Check = "light_fog_rear"
	CheckLightHazard             Check = "light_hazard"
	CheckLightHighBeamLeft       Check = "light_high_beam_left"
	CheckLightHighBeamRight      Check = "light_high_beam_right"
	CheckLightLowBeamLeft        Check = "light_low_beam_left"
	CheckLightLowBeamRight       Check = "light_low_beam_right"
	CheckLightPositionFrontLeft  Check = "light_position_front_left"
	CheckLightPositionFrontRight Check = "light_position_front_right"
	CheckLightPositionRear       Check = "light_position_rear"
	CheckLightPositionRearLeft   Check = "light_position_rear_left"
	CheckLightPositionRearRight  Check = "light_position_rear_right"
	CheckLightRegistrationPlate  Check = "light_registration_plate"
	CheckLightReverse            Check = "light_reverse"
	CheckLightSideMark           Check = "light_side_mark"
	CheckLightTurnFrontLeft      Check = "light_turn_front_left"
	CheckLightTurnFrontRight     Check = "light_turn_front_right"
	CheckLightTurnRearLeft       Check = "light_turn_rear_left"
	CheckLightTurnRearRight      Check = "light_turn_rear_right"
)

// Checks returns every check, the fluids and the battery first, then the lights.
func Checks() []Check {
	return []Check{
		CheckBrakeFluid, CheckCoolant, CheckOil, CheckWasherFluid, CheckLowVoltageBattery,
		CheckLightBrakeCenter, CheckLightBrakeLeft, CheckLightBrakeRight,
		CheckLightDaytimeLeft, CheckLightDaytimeRight, CheckLightFogFront, CheckLightFogRear,
		CheckLightHazard, CheckLightHighBeamLeft, CheckLightHighBeamRight,
		CheckLightLowBeamLeft, CheckLightLowBeamRight,
		CheckLightPositionFrontLeft, CheckLightPositionFrontRight, CheckLightPositionRear,
		CheckLightPositionRearLeft, CheckLightPositionRearRight,
		CheckLightRegistrationPlate, CheckLightReverse, CheckLightSideMark,
		CheckLightTurnFrontLeft, CheckLightTurnFrontRight, CheckLightTurnRearLeft, CheckLightTurnRearRight,
	}
}

// Light reports whether the check is an exterior light's.
func (c Check) Light() bool { return len(c) > 6 && c[:6] == "light_" }

// HealthField identifies a part of Health.
type HealthField uint8

// Health fields.
const (
	// HealthTyres covers Tyres and the recommended pressures.
	HealthTyres HealthField = 1 << iota
	// HealthService covers the service's state and what is left until it.
	HealthService
)

// Health is a normalized reading of the vehicle's health: its tyres, its next service
// and its warnings. As a Snapshot, Covers lists the parts the reading reports on, and a
// covered part replaces the previous one; Checks holds the checks the reading reports
// on, each replacing its previous value, the others left unchanged.
type Health struct {
	Covers HealthField

	Tyres map[Wheel]Tyre // a covered wheel the source does not report is absent
	// The pressures the vehicle recommends at the front and at the rear, for its load.
	RecommendedFrontKPa Value[float64]
	RecommendedRearKPa  Value[float64]

	Service Value[ServiceState]
	// Until the next service: some sources count days, others months.
	DaysToService       Value[float64]
	MonthsToService     Value[float64]
	DistanceToServiceKm Value[float64]

	// Checks are true where the vehicle warns, false where it does not; a check reported
	// with an unknown state is absent (OK false).
	Checks map[Check]Value[bool]
}

// merge copies what o covers.
func (h *Health) merge(o Health) {
	h.Covers |= o.Covers
	if o.Covers&HealthTyres != 0 {
		h.Tyres = o.Tyres
		h.RecommendedFrontKPa, h.RecommendedRearKPa = o.RecommendedFrontKPa, o.RecommendedRearKPa
	}
	if o.Covers&HealthService != 0 {
		h.Service = o.Service
		h.DaysToService, h.MonthsToService, h.DistanceToServiceKm = o.DaysToService, o.MonthsToService, o.DistanceToServiceKm
	}
	if len(o.Checks) > 0 && h.Checks == nil {
		h.Checks = make(map[Check]Value[bool], len(o.Checks))
	}
	for c, v := range o.Checks {
		h.Checks[c] = v
	}
}

// Pressures reports whether the reading gives a tyre's pressure.
func (h Health) Pressures() bool {
	for _, t := range h.Tyres {
		if t.PressureKPa.OK {
			return true
		}
	}
	return false
}

// HealthRecord is a stored response normalized into a health reading, read at FetchedAt
// and again, unchanged, at CheckedAt.
type HealthRecord struct {
	Source    string
	FetchedAt time.Time
	CheckedAt time.Time
	Health    Health
}

// CurrentHealth is the latest known health of a vehicle.
type CurrentHealth struct {
	Health
	// CheckedAt is the latest time a response of its health was read; zero: none was.
	CheckedAt time.Time
}

// LatestHealth merges the records, sorted or not, oldest first: the latest record of
// each kind is enough.
func LatestHealth(records []HealthRecord) CurrentHealth {
	rs := append([]HealthRecord(nil), records...)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].FetchedAt.Before(rs[j].FetchedAt) })
	var c CurrentHealth
	for _, r := range rs {
		c.merge(r.Health)
		c.CheckedAt = later(c.CheckedAt, r.CheckedAt.UTC())
	}
	return c
}

// TyreReading is the tyres' pressures as one response gave them, at one instant.
type TyreReading struct {
	At                  time.Time
	PressureKPa         map[Wheel]Value[float64]
	RecommendedFrontKPa Value[float64]
	RecommendedRearKPa  Value[float64]
}

// TyreRuns lays the pressures of the records within [from, to] out in runs, as Runs does
// the energy states: only the records giving a pressure count, a new run starts where
// more than gap separates two of them, and nothing is interpolated.
func TyreRuns(records []HealthRecord, from, to time.Time, gap time.Duration) [][]TyreReading {
	var spans []readSpan
	var kept []Health
	for _, r := range records {
		if r.Health.Covers&HealthTyres != 0 && r.Health.Pressures() {
			spans = append(spans, readSpan{fetched: r.FetchedAt, checked: r.CheckedAt, i: len(kept)})
			kept = append(kept, r.Health)
		}
	}
	return layOut(spans, from, to, gap, func(i int, at time.Time) TyreReading {
		h := kept[i]
		p := make(map[Wheel]Value[float64], len(h.Tyres))
		for w, t := range h.Tyres {
			p[w] = t.PressureKPa
		}
		return TyreReading{At: at, PressureKPa: p, RecommendedFrontKPa: h.RecommendedFrontKPa, RecommendedRearKPa: h.RecommendedRearKPa}
	})
}
