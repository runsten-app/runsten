package tesla

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"runsten/internal/core"
)

// Source names Tesla's Owner API among the sources of readings.
const Source = "tesla"

// Brand is the maker of every vehicle the Owner API reads, as the catalog names it.
const Brand = "tesla"

// kmPerMile converts the API's distances: miles, whatever units the car displays (seen
// with gui_distance_units km/hr).
const kmPerMile = 1.609344

// Normalizer reads the stored responses of the Owner API into snapshots, for the
// derivation. Each Endpoint is an endpoint of the source.
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads: vehicle_data holds every value,
// the vehicle's state whether it sleeps.
func (Normalizer) Kinds() []core.Kind { return kinds(VehicleState, VehicleData) }

// EnergyKinds returns the kinds holding the energy state: the state of charge, the range
// and the charging power.
func (Normalizer) EnergyKinds() []core.Kind { return kinds(VehicleData) }

// HealthKinds returns the kinds of response NormalizeHealth reads.
func (Normalizer) HealthKinds() []core.Kind { return kinds(VehicleData) }

// Normalize reads a stored response of kind k.
func (Normalizer) Normalize(k core.Kind, raw []byte) (core.Snapshot, error) {
	if k.Source != Source {
		return core.Snapshot{}, fmt.Errorf("normalize: source %q is not %s", k.Source, Source)
	}
	return Normalize(Endpoint(k.Endpoint), raw)
}

// NormalizeHealth reads a stored response of kind k into a health reading.
func (Normalizer) NormalizeHealth(k core.Kind, raw []byte) (core.Health, error) {
	if k.Source != Source {
		return core.Health{}, fmt.Errorf("normalize health: source %q is not %s", k.Source, Source)
	}
	return NormalizeHealth(Endpoint(k.Endpoint), raw)
}

func kinds(es ...Endpoint) []core.Kind {
	out := make([]core.Kind, len(es))
	for i, e := range es {
		out[i] = core.Kind{Source: Source, Endpoint: string(e)}
	}
	return out
}

// vehicleData is what the normalization reads of a vehicle_data response. A block left
// out of the response covers nothing; a field of a block present but null is absent.
type vehicleData struct {
	Response *struct {
		VIN           string         `json:"vin"`
		State         *string        `json:"state"`
		ChargeState   *chargeState   `json:"charge_state"`
		DriveState    *driveState    `json:"drive_state"`
		VehicleState  *vehicleState  `json:"vehicle_state"`
		VehicleConfig *vehicleConfig `json:"vehicle_config"`
	} `json:"response"`
}

type chargeState struct {
	Timestamp          *int64   `json:"timestamp"`
	BatteryLevel       *float64 `json:"battery_level"`
	UsableLevel        *float64 `json:"usable_battery_level"`
	BatteryRange       *float64 `json:"battery_range"` // mi, rated: a fixed conversion of the usable level
	ChargingState      *string  `json:"charging_state"`
	FastChargerPresent *bool    `json:"fast_charger_present"`
	ChargerPower       *float64 `json:"charger_power"` // kW, whole
	ChargeLimitSoC     *float64 `json:"charge_limit_soc"`
	EnergyAdded        *float64 `json:"charge_energy_added"` // kWh, this session's
}

type driveState struct {
	Timestamp  *int64   `json:"timestamp"`
	GPSAsOf    *int64   `json:"gps_as_of"` // s
	ShiftState *string  `json:"shift_state"`
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
}

type vehicleState struct {
	Timestamp *int64   `json:"timestamp"`
	Odometer  *float64 `json:"odometer"` // mi

	PressureFL *float64 `json:"tpms_pressure_fl"` // bar
	PressureFR *float64 `json:"tpms_pressure_fr"`
	PressureRL *float64 `json:"tpms_pressure_rl"`
	PressureRR *float64 `json:"tpms_pressure_rr"`
	// When each sensor last told its pressure (s): days before the response for a car
	// parked a while.
	SeenFL *int64 `json:"tpms_last_seen_pressure_time_fl"`
	SeenFR *int64 `json:"tpms_last_seen_pressure_time_fr"`
	SeenRL *int64 `json:"tpms_last_seen_pressure_time_rl"`
	SeenRR *int64 `json:"tpms_last_seen_pressure_time_rr"`
	// The recommended cold pressures (bar).
	RecommendedFront *float64 `json:"tpms_rcp_front_value"`
	RecommendedRear  *float64 `json:"tpms_rcp_rear_value"`
	SoftFL           *bool    `json:"tpms_soft_warning_fl"`
	SoftFR           *bool    `json:"tpms_soft_warning_fr"`
	SoftRL           *bool    `json:"tpms_soft_warning_rl"`
	SoftRR           *bool    `json:"tpms_soft_warning_rr"`
	HardFL           *bool    `json:"tpms_hard_warning_fl"`
	HardFR           *bool    `json:"tpms_hard_warning_fr"`
	HardRL           *bool    `json:"tpms_hard_warning_rl"`
	HardRR           *bool    `json:"tpms_hard_warning_rr"`
}

type vehicleConfig struct {
	CarType     *string `json:"car_type"`
	TrimBadging *string `json:"trim_badging"`
}

func parseVehicleData(raw []byte) (vehicleData, error) {
	var v vehicleData
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, err //nolint:wrapcheck // wrapped by the callers
	}
	return v, nil
}

// sleepStates maps the vehicle's State; an unknown word is absent.
var sleepStates = map[State]core.SleepState{
	Online:  core.Awake,
	Asleep:  core.Asleep,
	Offline: core.Offline,
}

// Normalize converts a raw response of endpoint e into a vendor-neutral snapshot, each
// value dated by its block's timestamp (the car's, in milliseconds). The vehicle's state
// gives whether the car sleeps, and nothing else. vehicle_data tells it too, but changes
// at each read (its temperatures): the state read from it would seem first read at the
// latest change, so the vehicle's state alone gives it.
func Normalize(e Endpoint, raw []byte) (core.Snapshot, error) {
	var s core.Snapshot
	if e != VehicleData && e != VehicleState {
		return s, nil
	}
	// Both responses have the vehicle's fields under response; the state's has no block.
	v, err := parseVehicleData(raw)
	if err != nil {
		return s, fmt.Errorf("normalize %s: %w", e, err)
	}
	r := v.Response
	if r == nil {
		return s, nil
	}
	if e == VehicleState {
		// Undated: the API tells no time of the state. Without one, the previous holds.
		if r.State != nil {
			s.Covers |= core.FieldSleep
			if st, ok := sleepStates[State(*r.State)]; ok {
				s.Sleep = core.Some(st, time.Time{})
			}
		}
		return s, nil
	}
	if c := r.ChargeState; c != nil {
		normalizeCharge(c, &s)
	}
	if d := r.DriveState; d != nil {
		normalizeDrive(d, &s)
	}
	if vs := r.VehicleState; vs != nil {
		s.Covers |= core.FieldOdometer
		if vs.Odometer != nil {
			s.OdometerKm = core.Some(*vs.Odometer*kmPerMile, millis(vs.Timestamp))
		}
	}
	if r.VehicleConfig != nil {
		model(r.VIN, r.VehicleConfig, &s)
	}
	return s, nil
}

// millis returns the instant of a block's timestamp; zero if missing.
func millis(ms *int64) time.Time {
	if ms == nil || *ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(*ms).UTC()
}

// seconds returns the instant of a timestamp in seconds; zero if missing.
func seconds(s *int64) time.Time {
	if s == nil || *s <= 0 {
		return time.Time{}
	}
	return time.Unix(*s, 0).UTC()
}

func number(v *float64, at time.Time) core.Value[float64] {
	if v == nil {
		return core.Value[float64]{}
	}
	return core.Some(*v, at)
}

func scaled(v *float64, factor float64, at time.Time) core.Value[float64] {
	if v == nil {
		return core.Value[float64]{}
	}
	return core.Some(*v*factor, at)
}

// chargingStates maps charging_state. Starting is the charge beginning; NoPower is
// plugged to a station that gives nothing, Stopped a charge stopped before its limit:
// both idle. Assumption: the words listed by the community, Complete and Disconnected
// seen on a real car; an unknown one is absent.
var chargingStates = map[string]core.ChargingStatus{
	"Charging":     core.ChargingActive,
	"Starting":     core.ChargingActive,
	"Complete":     core.ChargingDone,
	"Stopped":      core.ChargingIdle,
	"NoPower":      core.ChargingIdle,
	"Disconnected": core.ChargingIdle,
}

func normalizeCharge(c *chargeState, s *core.Snapshot) {
	at := millis(c.Timestamp)
	s.Covers |= core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
		core.FieldChargeType | core.FieldPower | core.FieldTargetSoC | core.FieldEnergyAdded
	s.SoC = number(c.BatteryLevel, at)
	s.UsableSoC = number(c.UsableLevel, at)
	s.EnergyAddedKWh = number(c.EnergyAdded, at)
	s.RangeKm = scaled(c.BatteryRange, kmPerMile, at)
	s.PowerW = scaled(c.ChargerPower, 1000, at)
	s.TargetSoC = number(c.ChargeLimitSoC, at)
	if c.ChargingState == nil {
		return
	}
	status, ok := chargingStates[*c.ChargingState]
	if !ok {
		return
	}
	s.Charging = core.Some(status, at)
	if *c.ChargingState == "Disconnected" {
		s.Connection = core.Some(core.Disconnected, at)
	} else {
		s.Connection = core.Some(core.Connected, at)
	}
	switch {
	case status != core.ChargingActive:
		s.ChargeType = core.Some(core.NoCharge, at)
	case c.FastChargerPresent != nil && *c.FastChargerPresent:
		s.ChargeType = core.Some(core.DC, at)
	case c.FastChargerPresent != nil:
		// fast_charger_type is no guide: it reads MCSingleWireCAN on an AC station.
		s.ChargeType = core.Some(core.AC, at)
	}
}

// shiftStates maps shift_state: the API has no engine status, and a car in gear drives.
// null is parked (seen on a car parked, asleep a moment before).
var shiftStates = map[string]core.EngineState{
	"D": core.EngineRunning, "R": core.EngineRunning, "N": core.EngineRunning,
	"P": core.EngineStopped,
}

func normalizeDrive(d *driveState, s *core.Snapshot) {
	at := millis(d.Timestamp)
	engine, ok := core.EngineStopped, true
	if d.ShiftState != nil {
		engine, ok = shiftStates[*d.ShiftState]
	}
	// An unknown gear does not cover the engine, which keeps its previous state.
	if ok {
		s.Covers |= core.FieldEngine
		s.Engine = core.Some(engine, at)
	}
	s.Covers |= core.FieldPosition
	if d.Latitude != nil && d.Longitude != nil {
		// The position is the latest fix's, older than the block's timestamp.
		fix := seconds(d.GPSAsOf)
		if fix.IsZero() {
			fix = at
		}
		s.Position = core.Some(core.Position{Lat: *d.Latitude, Lon: *d.Longitude}, fix)
	}
}

// families maps car_type to the families of the catalog.
var families = map[string]string{
	"models":     "Model S",
	"modelx":     "Model X",
	"model3":     "Model 3",
	"modely":     "Model Y",
	"cybertruck": "Cybertruck",
}

// model reads the model: the brand, the family from car_type (absent when unknown: the
// brand stands in for it), the model year from the VIN. The VIN's year is the one ISO
// 3779 gives, which may differ from Tesla's model year (a 2021 VIN on a car whose
// efficiency_package reads MY2020): the catalog's recognition does not rest on it alone.
func model(vin string, c *vehicleConfig, s *core.Snapshot) {
	s.Covers |= core.FieldModel
	s.Brand = core.Some(Brand, time.Time{})
	if c.CarType != nil {
		if f, ok := families[strings.ToLower(*c.CarType)]; ok {
			s.Family = core.Some(f, time.Time{})
		}
	}
	if c.TrimBadging != nil && *c.TrimBadging != "" {
		s.Trim = core.Some(strings.ToLower(*c.TrimBadging), time.Time{})
	}
	if y, ok := modelYear(vin); ok {
		s.ModelYear = core.Some(y, time.Time{})
	}
	// Every car_type the Owner API reads is battery electric.
	s.BatteryElectric = core.Some(true, time.Time{})
}

// modelYear reads the model year at the VIN's tenth position (ISO 3779, on a 30-year
// cycle), from 2010 to 2039: the Owner API reads the Model S onwards (2012).
func modelYear(vin string) (int, bool) {
	if len(vin) != 17 {
		return 0, false
	}
	const codes = "ABCDEFGHJKLMNPRSTVWXY123456789"
	i := strings.IndexByte(codes, vin[9])
	if i < 0 {
		return 0, false
	}
	return 2010 + i, true
}
