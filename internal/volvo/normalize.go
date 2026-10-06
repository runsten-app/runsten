package volvo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"runsten/internal/core"
)

// kmPerMile converts electricRange and distances reported in miles (market-dependent).
const kmPerMile = 1.609344

// NormalizedEndpoints lists the endpoints Normalize reads; the others carry nothing the
// domain uses.
func NormalizedEndpoints() []Endpoint {
	return []Endpoint{EngineStatus, EnergyState, Odometer, Location, Statistics, Details}
}

// Normalize converts a raw response of ep into a vendor-neutral snapshot. A value that
// is null, missing, in error (NOT_SUPPORTED, PROPERTY_NOT_FOUND…), in an unknown unit
// or with an unknown enum value is absent, never zero.
func Normalize(ep Endpoint, raw []byte) (core.Snapshot, error) {
	var s core.Snapshot
	var err error
	switch ep {
	case EngineStatus:
		err = normalizeEngine(raw, &s)
	case EnergyState:
		err = normalizeEnergy(raw, &s)
	case Odometer:
		err = normalizeOdometer(raw, &s)
	case Location:
		err = normalizeLocation(raw, &s)
	case Statistics:
		err = normalizeStatistics(raw, &s)
	case Details:
		err = normalizeDetails(raw, &s)
	default:
		return s, nil
	}
	if err != nil {
		return core.Snapshot{}, fmt.Errorf("normalize %s: %w", ep, err)
	}
	return s, nil
}

// cvValue is a Connected Vehicle value: {timestamp, unit, value}.
type cvValue struct {
	Timestamp string          `json:"timestamp"`
	Unit      *string         `json:"unit"`
	Value     json.RawMessage `json:"value"`
}

func (v *cvValue) number(convert func(float64, string) (float64, bool)) core.Value[float64] {
	if v == nil || v.Unit == nil {
		return core.Value[float64]{}
	}
	n, ok := number(v.Value)
	if !ok {
		return core.Value[float64]{}
	}
	if n, ok = convert(n, *v.Unit); !ok {
		return core.Value[float64]{}
	}
	return core.Some(n, timestamp(v.Timestamp))
}

// energyValue is an Energy v2 value: {status, value, unit, updatedAt}, or
// {status: ERROR, code, message}.
type energyValue struct {
	Status    string          `json:"status"`
	Value     json.RawMessage `json:"value"`
	Unit      string          `json:"unit"`
	UpdatedAt string          `json:"updatedAt"`
}

func (v *energyValue) ok() bool { return v != nil && v.Status == "OK" }

func (v *energyValue) number(convert func(float64, string) (float64, bool)) core.Value[float64] {
	if !v.ok() {
		return core.Value[float64]{}
	}
	n, ok := number(v.Value)
	if !ok {
		return core.Value[float64]{}
	}
	if n, ok = convert(n, v.Unit); !ok {
		return core.Value[float64]{}
	}
	return core.Some(n, timestamp(v.UpdatedAt))
}

func energyEnum[T ~string](v *energyValue, values map[string]T) core.Value[T] {
	if !v.ok() {
		return core.Value[T]{}
	}
	return enum(v.Value, timestamp(v.UpdatedAt), values)
}

func enum[T ~string](raw json.RawMessage, at time.Time, values map[string]T) core.Value[T] {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return core.Value[T]{}
	}
	t, ok := values[s]
	if !ok {
		return core.Value[T]{}
	}
	return core.Some(t, at)
}

func number(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		return 0, false
	}
	return n, true
}

// text reads a string; an empty one is absent, like null.
func text(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" {
		return "", false
	}
	return s, true
}

// timestamp parses a vehicle timestamp; zero if missing or unreadable.
func timestamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func km(v float64, unit string) (float64, bool) {
	switch unit {
	case "km":
		return v, true
	case "mi", "miles":
		return v * kmPerMile, true
	}
	return 0, false
}

func percent(v float64, unit string) (float64, bool) {
	return v, unit == "percentage" || unit == "%"
}

func watts(v float64, unit string) (float64, bool) {
	switch unit {
	case "watts", "W":
		return v, true
	case "kW":
		return v * 1000, true
	}
	return 0, false
}

func kWhPer100km(v float64, unit string) (float64, bool) { return v, unit == "kWh/100km" }

func normalizeEngine(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			EngineStatus *cvValue `json:"engineStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	s.Covers = core.FieldEngine
	if e := v.Data.EngineStatus; e != nil {
		// Assumption: an electric vehicle reports RUNNING while driving, as the simulator does.
		s.Engine = enum(e.Value, timestamp(e.Timestamp), map[string]core.EngineState{
			"RUNNING": core.EngineRunning, "STOPPED": core.EngineStopped,
		})
	}
	return nil
}

func normalizeEnergy(raw []byte, s *core.Snapshot) error {
	var v struct {
		BatteryChargeLevel       *energyValue `json:"batteryChargeLevel"`
		ElectricRange            *energyValue `json:"electricRange"`
		ChargerConnectionStatus  *energyValue `json:"chargerConnectionStatus"`
		ChargingStatus           *energyValue `json:"chargingStatus"`
		ChargingType             *energyValue `json:"chargingType"`
		ChargingPower            *energyValue `json:"chargingPower"`
		TargetBatteryChargeLevel *energyValue `json:"targetBatteryChargeLevel"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	s.Covers = core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
		core.FieldChargeType | core.FieldPower | core.FieldTargetSoC
	s.SoC = v.BatteryChargeLevel.number(percent)
	s.RangeKm = v.ElectricRange.number(km)
	s.Connection = energyEnum(v.ChargerConnectionStatus, map[string]core.Connection{
		"CONNECTED": core.Connected, "DISCONNECTED": core.Disconnected, "FAULT": core.ConnectionFault,
	})
	s.Charging = energyEnum(v.ChargingStatus, map[string]core.ChargingStatus{
		"IDLE": core.ChargingIdle, "CHARGING": core.ChargingActive, "DONE": core.ChargingDone,
		"SCHEDULED": core.ChargingScheduled, "DISCHARGING": core.ChargingDischarging, "ERROR": core.ChargingError,
	})
	s.ChargeType = energyEnum(v.ChargingType, map[string]core.ChargeType{"AC": core.AC, "DC": core.DC}) // NONE: absent
	s.PowerW = v.ChargingPower.number(watts)
	s.TargetSoC = v.TargetBatteryChargeLevel.number(percent)
	return nil
}

func normalizeOdometer(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			Odometer *cvValue `json:"odometer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	s.Covers = core.FieldOdometer
	s.OdometerKm = v.Data.Odometer.number(km)
	return nil
}

func normalizeLocation(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			Geometry struct {
				Coordinates []*float64 `json:"coordinates"` // GeoJSON: longitude, latitude, altitude
			} `json:"geometry"`
			Properties struct {
				Timestamp string `json:"timestamp"`
			} `json:"properties"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	s.Covers = core.FieldPosition
	if c := v.Data.Geometry.Coordinates; len(c) >= 2 && c[0] != nil && c[1] != nil {
		s.Position = core.Some(core.Position{Lat: *c[1], Lon: *c[0]}, timestamp(v.Data.Properties.Timestamp))
	}
	return nil
}

func normalizeStatistics(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			TripMeterAutomatic                *cvValue `json:"tripMeterAutomatic"`
			AverageEnergyConsumptionAutomatic *cvValue `json:"averageEnergyConsumptionAutomatic"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	s.Covers = core.FieldTripMeter | core.FieldConsumption
	s.TripMeterKm = v.Data.TripMeterAutomatic.number(km)
	s.ConsumptionKWhPer100km = v.Data.AverageEnergyConsumptionAutomatic.number(kWhPer100km)
	return nil
}

func normalizeDetails(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			BatteryCapacityKWH json.RawMessage `json:"batteryCapacityKWH"`
			ModelYear          json.RawMessage `json:"modelYear"`
			FuelType           json.RawMessage `json:"fuelType"`
			Descriptions       struct {
				Model json.RawMessage `json:"model"`
			} `json:"descriptions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	// No timestamp on vehicle details.
	s.Covers = core.FieldCapacity | core.FieldModel
	if n, ok := number(v.Data.BatteryCapacityKWH); ok && n > 0 {
		s.CapacityKWh = core.Some(n, time.Time{})
	}
	if m, ok := text(v.Data.Descriptions.Model); ok {
		s.Family = core.Some(m, time.Time{})
	}
	if y, ok := number(v.Data.ModelYear); ok && y > 0 && y == math.Trunc(y) {
		s.ModelYear = core.Some(int(y), time.Time{})
	}
	if f, ok := text(v.Data.FuelType); ok {
		s.BatteryElectric = core.Some(batteryElectric(f), time.Time{})
	}
	return nil
}

// batteryElectric reads fuelType. The EX30 reports NONE, as if it had no fuel. A hybrid
// reports its fuels joined by a slash (PETROL/ELECTRIC). Any other value is not taken
// for a battery electric vehicle: recognizing its variant would only mislead.
func batteryElectric(fuelType string) bool {
	return fuelType == "ELECTRIC" || fuelType == "NONE"
}
