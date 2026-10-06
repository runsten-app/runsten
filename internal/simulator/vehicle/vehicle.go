// Package vehicle models the simulator's virtual vehicle.
//
// It separates the vehicle's actual state (what happens "physically") from the state
// uploaded to the cloud (what the API can return). The API only sees the latter, along
// with the time of each upload. The upload rules encode assumptions about how often
// the real vehicle uploads its data, and are configured by UploadPolicy.
//
// This package knows nothing about HTTP or the Volvo API format.
package vehicle

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Availability tells whether the vehicle uploads an optional data item.
type Availability int

// Possible values of Availability, based on community feedback.
const (
	Supported   Availability = iota // data is uploaded
	Unsupported                     // the model does not support it (e.g. EX30, charging current limit)
	NotFound                        // property missing on the cloud side (e.g. EX90, charging power)
)

// Capabilities lists the optional data of a vehicle profile.
type Capabilities struct {
	ChargingPower        Availability
	ChargingCurrentLimit Availability
}

// Profile returns the capabilities of a known profile, by name.
func Profile(name string) (Capabilities, bool) {
	switch name {
	case "bev-generic":
		return Capabilities{ChargingPower: Supported, ChargingCurrentLimit: Supported}, true
	case "ex30-like":
		return Capabilities{ChargingPower: Supported, ChargingCurrentLimit: Unsupported}, true
	case "ex90-like":
		return Capabilities{ChargingPower: NotFound, ChargingCurrentLimit: Supported}, true
	}
	return Capabilities{}, false
}

// Spec describes the vehicle's fixed characteristics.
type Spec struct {
	Model     string
	ModelYear int
	// BatteryKWh is the nominal capacity, what the API's batteryCapacityKWH reports:
	// fixed, like the real one.
	BatteryKWh float64
	// UsableKWh is the capacity the SoC of 0 to 100 % runs over (the net one, as the
	// catalog states it and catalog_net assumes). Zero: BatteryKWh.
	UsableKWh float64
	// FadePerYear is the share of the usable capacity lost per simulated year, linear,
	// counted from FadeStart. Zero: no fade.
	FadePerYear float64
	// FadeStart is the instant the fade's years are counted from: the vehicle's own
	// scenario start, set by the scenario (a fleet parks a vehicle before it).
	FadeStart              time.Time
	ConsumptionKWhPer100km float64
	ChargingCurrentLimitA  float64
	Capabilities           Capabilities
}

// Validate checks that the specification is consistent.
func (s Spec) Validate() error {
	switch {
	case s.BatteryKWh <= 0:
		return errors.New("batteryKWh must be > 0")
	case s.UsableKWh < 0 || s.UsableKWh > s.BatteryKWh:
		return errors.New("usableKWh must be in [0, batteryKWh]")
	case s.FadePerYear < 0 || s.FadePerYear >= 1:
		return errors.New("fadePerYear must be in [0, 1)")
	case s.FadePerYear > 0 && s.FadeStart.IsZero():
		return errors.New("fadePerYear requires a FadeStart")
	case s.ConsumptionKWhPer100km <= 0:
		return errors.New("consumptionKWhPer100km must be > 0")
	}
	return nil
}

// UsableKWhAt returns the capacity the SoC of 0 to 100 % runs over at the given
// instant: the usable capacity (BatteryKWh when unset), reduced linearly by
// FadePerYear a year since FadeStart. A year lasts 365.25 days; before FadeStart
// the capacity is the full usable one, never a larger one.
func (s Spec) UsableKWhAt(at time.Time) float64 {
	kwh := s.UsableKWh
	if kwh <= 0 {
		kwh = s.BatteryKWh
	}
	if s.FadePerYear > 0 {
		if elapsed := at.Sub(s.FadeStart); elapsed > 0 {
			kwh *= 1 - s.FadePerYear*(elapsed.Hours()/(365.25*24))
		}
	}
	return kwh
}

// ChargingStatus is the vehicle's charging state.
type ChargingStatus int

// Charging states.
const (
	Idle ChargingStatus = iota
	Charging
	Done
)

// ChargeType is the charging current type.
type ChargeType int

// Charge types.
const (
	NoCharge ChargeType = iota
	AC
	DC
)

// Position is a geographic position with a heading in degrees.
type Position struct {
	Lat, Lon float64
	Heading  float64
}

// Trip summarizes the last completed trip.
type Trip struct {
	DistanceKm float64
	Duration   time.Duration
	EnergyKWh  float64
}

// State is the vehicle's complete state at a given time.
type State struct {
	SoC            float64 // 0 to 100
	OdometerKm     float64
	Driving        bool
	Position       Position
	Locked         bool
	PluggedIn      bool
	Charging       ChargingStatus
	ChargeType     ChargeType
	ChargingPowerW float64
	TargetSoC      float64

	LastTrip              Trip
	TotalDistanceKm       float64 // since the start of the simulation
	TotalEnergyKWh        float64
	TotalDriveTime        time.Duration // completed trips only
	DistanceSinceChargeKm float64
	EnergySinceChargeKWh  float64
}

// UploadPolicy groups the assumptions about upload frequency.
type UploadPolicy struct {
	// DriveInterval is the interval between two uploads during a trip.
	// The actual value is unknown.
	DriveInterval time.Duration
}

// DefaultUploadPolicy returns the policy used without explicit configuration.
func DefaultUploadPolicy() UploadPolicy { return UploadPolicy{DriveInterval: time.Minute} }

// Report is what the cloud knows about the vehicle: one state per data group, with
// the time of each group's last upload.
type Report struct {
	Spec Spec

	// Energy: SoC, range, charging. Uploaded on every % or charging state change.
	Energy   State
	EnergyAt time.Time

	// Status: odometer, statistics, engine, doors and windows. Uploaded during trips,
	// at their transitions and at the end of charging.
	Status   State
	StatusAt time.Time

	// Location: uploaded only at the end of a trip.
	Location   Position
	LocationAt time.Time
}

// Vehicle is a virtual vehicle. It is not safe for concurrent use: the caller (the
// simulation) serializes access.
type Vehicle struct {
	spec   Spec
	policy UploadPolicy
	state  State
	report Report

	tripStart     time.Time
	tripStartOdo  float64
	tripStartKWh  float64
	tripOrigin    Position
	lastSoCReport int
}

// New creates a vehicle in the given initial state. All groups are considered
// uploaded at time at.
func New(spec Spec, initial State, policy UploadPolicy, at time.Time) (*Vehicle, error) {
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid spec: %w", err)
	}
	if initial.SoC < 0 || initial.SoC > 100 {
		return nil, fmt.Errorf("initial SoC out of range: %v", initial.SoC)
	}
	if policy.DriveInterval <= 0 {
		return nil, errors.New("DriveInterval must be > 0")
	}
	if initial.TargetSoC == 0 {
		initial.TargetSoC = 90
	}
	v := &Vehicle{spec: spec, policy: policy, state: initial}
	v.report = Report{Spec: spec}
	v.uploadEnergy(at)
	v.uploadStatus(at)
	v.uploadLocation(at)
	return v, nil
}

// State returns the current actual state.
func (v *Vehicle) State() State { return v.state }

// Report returns the state uploaded to the cloud.
func (v *Vehicle) Report() Report { return v.report }

// StartDrive starts a trip. A plugged-in vehicle is unplugged first.
func (v *Vehicle) StartDrive(at time.Time) {
	if v.state.PluggedIn {
		v.Unplug(at)
	}
	v.state.Driving = true
	v.state.Locked = false
	v.tripStart = at
	v.tripStartOdo = v.state.OdometerKm
	v.tripStartKWh = v.state.TotalEnergyKWh
	v.tripOrigin = v.state.Position
	v.uploadStatus(at)
}

// Drive moves the vehicle forward by distanceKm during an ongoing trip.
func (v *Vehicle) Drive(at time.Time, distanceKm float64) {
	if !v.state.Driving || distanceKm <= 0 {
		return
	}
	kwh := distanceKm * v.spec.ConsumptionKWhPer100km / 100
	v.state.OdometerKm += distanceKm
	v.state.TotalDistanceKm += distanceKm
	v.state.DistanceSinceChargeKm += distanceKm
	v.state.TotalEnergyKWh += kwh
	v.state.EnergySinceChargeKWh += kwh
	v.setSoC(at, v.state.SoC-kwh/v.spec.UsableKWhAt(at)*100)
	if at.Sub(v.report.StatusAt) >= v.policy.DriveInterval {
		v.uploadStatus(at)
	}
}

// EndDrive ends the trip at the given destination.
func (v *Vehicle) EndDrive(at time.Time, dest Position) {
	if !v.state.Driving {
		return
	}
	dest.Heading = bearing(v.tripOrigin, dest)
	v.state.Driving = false
	v.state.Locked = true
	v.state.Position = dest
	v.state.LastTrip = Trip{
		DistanceKm: v.state.OdometerKm - v.tripStartOdo,
		Duration:   at.Sub(v.tripStart),
		EnergyKWh:  v.state.TotalEnergyKWh - v.tripStartKWh,
	}
	v.state.TotalDriveTime += v.state.LastTrip.Duration
	v.uploadStatus(at)
	v.uploadLocation(at)
}

// PlugIn plugs the vehicle in and starts charging up to targetSoC.
func (v *Vehicle) PlugIn(at time.Time, typ ChargeType, powerW, targetSoC float64) {
	v.state.PluggedIn = true
	v.state.ChargeType = typ
	v.state.TargetSoC = targetSoC
	if v.state.SoC >= targetSoC {
		v.state.Charging = Done
		v.state.ChargingPowerW = 0
	} else {
		v.state.Charging = Charging
		v.state.ChargingPowerW = powerW
	}
	v.uploadEnergy(at)
}

// Charge advances an ongoing charge by dt. It returns true when the target is
// reached (or if no charge is in progress).
func (v *Vehicle) Charge(at time.Time, dt time.Duration) bool {
	if v.state.Charging != Charging {
		return true
	}
	// The charging power is the one entering the battery: no efficiency applies, and
	// the SoC runs over the usable capacity.
	kwh := v.state.ChargingPowerW / 1000 * dt.Hours()
	soc := v.state.SoC + kwh/v.spec.UsableKWhAt(at)*100
	if soc < v.state.TargetSoC {
		v.setSoC(at, soc)
		return false
	}
	v.state.SoC = v.state.TargetSoC
	v.state.Charging = Done
	v.state.ChargingPowerW = 0
	v.state.DistanceSinceChargeKm = 0
	v.state.EnergySinceChargeKWh = 0
	v.uploadEnergy(at)
	v.uploadStatus(at)
	return true
}

// Unplug unplugs the vehicle.
func (v *Vehicle) Unplug(at time.Time) {
	v.state.PluggedIn = false
	v.state.Charging = Idle
	v.state.ChargeType = NoCharge
	v.state.ChargingPowerW = 0
	v.uploadEnergy(at)
}

// setSoC updates the SoC and uploads the energy group on every % change.
func (v *Vehicle) setSoC(at time.Time, soc float64) {
	v.state.SoC = math.Max(0, math.Min(100, soc))
	if int(v.state.SoC) != v.lastSoCReport {
		v.uploadEnergy(at)
	}
}

func (v *Vehicle) uploadEnergy(at time.Time) {
	v.report.Energy = v.state
	v.report.EnergyAt = at
	v.lastSoCReport = int(v.state.SoC)
}

func (v *Vehicle) uploadStatus(at time.Time) {
	v.report.Status = v.state
	v.report.StatusAt = at
}

func (v *Vehicle) uploadLocation(at time.Time) {
	v.report.Location = v.state.Position
	v.report.LocationAt = at
}

// bearing computes the initial heading from a to b, in degrees [0, 360).
func bearing(a, b Position) float64 {
	if a.Lat == b.Lat && a.Lon == b.Lon {
		return a.Heading
	}
	lat1, lat2 := rad(a.Lat), rad(b.Lat)
	dLon := rad(b.Lon - a.Lon)
	y := math.Sin(dLon) * math.Cos(lat2)
	x := math.Cos(lat1)*math.Sin(lat2) - math.Sin(lat1)*math.Cos(lat2)*math.Cos(dLon)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }
