package polestar

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"runsten/internal/core"
)

// Source names the Polestar Data Portal among the sources of readings.
const Source = "polestar"

// Brand is the maker of every vehicle the Data Portal reads, as the catalog names it.
const Brand = "polestar"

// Normalizer reads the stored responses of the Data Portal into snapshots, for the
// derivation. Each domain is an endpoint of the source, named as the API names it.
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads.
func (Normalizer) Kinds() []core.Kind {
	return kinds(Battery, Availability, Odometer, Location, TargetSoC)
}

// HealthKinds returns the kinds of response NormalizeHealth reads.
func (Normalizer) HealthKinds() []core.Kind { return kinds(Health) }

// NormalizeHealth reads a stored response of kind k into a health reading.
func (Normalizer) NormalizeHealth(k core.Kind, raw []byte) (core.Health, error) {
	if k.Source != Source {
		return core.Health{}, fmt.Errorf("normalize health: source %q is not %s", k.Source, Source)
	}
	return NormalizeHealth(Domain(k.Endpoint), raw)
}

// EnergyKinds returns the kinds holding the energy state: the state of charge, the range
// and the charging power.
func (Normalizer) EnergyKinds() []core.Kind { return kinds(Battery) }

// Normalize reads a stored response of kind k.
func (Normalizer) Normalize(k core.Kind, raw []byte) (core.Snapshot, error) {
	if k.Source != Source {
		return core.Snapshot{}, fmt.Errorf("normalize: source %q is not %s", k.Source, Source)
	}
	return Normalize(Domain(k.Endpoint), raw)
}

func kinds(ds ...Domain) []core.Kind {
	out := make([]core.Kind, len(ds))
	for i, d := range ds {
		out[i] = core.Kind{Source: Source, Endpoint: string(d)}
	}
	return out
}

// Normalize converts a raw response of domain d into a vendor-neutral snapshot, its
// values dated by the car's timestamp. The API writes protobuf's JSON, which leaves out
// zeros, false and empty lists, and the zero of an enum (UNSPECIFIED): a missing field
// is absent, never zero, but where the zero is the only reading (the charging power out
// of an active charge). An unknown enum value is absent too.
func Normalize(d Domain, raw []byte) (core.Snapshot, error) {
	var s core.Snapshot
	var err error
	switch d {
	case Battery:
		err = normalizeBattery(raw, &s)
	case Odometer:
		err = normalizeOdometer(raw, &s)
	case Location:
		err = normalizeLocation(raw, &s)
	case Availability:
		err = normalizeAvailability(raw, &s)
	case TargetSoC:
		err = normalizeTargetSoC(raw, &s)
	default:
		return s, nil
	}
	if err != nil {
		return core.Snapshot{}, fmt.Errorf("normalize %s: %w", d, err)
	}
	return s, nil
}

// stamp is protobuf's Timestamp as the API writes it: {"seconds": "<int64>", "nanos"},
// the seconds in a string (a number is read too), nanos left out at 0.
type stamp struct {
	Seconds json.RawMessage `json:"seconds"`
	Nanos   json.RawMessage `json:"nanos"`
}

// instant returns the instant; zero if missing or unreadable.
func (t *stamp) instant() time.Time {
	if t == nil {
		return time.Time{}
	}
	s, err := strconv.ParseInt(strings.Trim(string(t.Seconds), `"`), 10, 64)
	if err != nil || s <= 0 {
		return time.Time{}
	}
	var ns int64
	if len(t.Nanos) > 0 && (json.Unmarshal(t.Nanos, &ns) != nil || ns < 0 || ns >= int64(time.Second)) {
		return time.Time{}
	}
	return time.Unix(s, ns).UTC()
}

func number(v *float64, at time.Time) core.Value[float64] {
	if v == nil {
		return core.Value[float64]{}
	}
	return core.Some(*v, at)
}

// enum reads an enum value without its prefix (CHARGING_TYPE_AC: AC).
func enum[T ~string](v, prefix string, at time.Time, values map[string]T) core.Value[T] {
	name, ok := strings.CutPrefix(v, prefix)
	if !ok {
		return core.Value[T]{}
	}
	t, ok := values[name]
	if !ok {
		return core.Value[T]{}
	}
	return core.Some(t, at)
}

// chargingStatuses maps chargingStatusV2 (chargingStatus, its first version, is
// deprecated). Every state that charges is a charge, smart or towards the minimum
// level; every state that waits for a time is scheduled.
var chargingStatuses = map[string]core.ChargingStatus{
	"CHARGING":                 core.ChargingActive,
	"SMART_CHARGING":           core.ChargingActive,
	"CHARGING_TOWARDS_MIN_SOC": core.ChargingActive,
	"CHARGING_IS_EN_ROUTE":     core.ChargingActive,
	// Assumption: a smart charge that will not finish by the departure still charges.
	"SMART_CHARGING_WILL_NOT_FINISH":     core.ChargingActive,
	"DONE":                               core.ChargingDone,
	"CHARGE_LEVEL_IS_GOOD_TO_GO":         core.ChargingDone,
	"SCHEDULED":                          core.ChargingScheduled,
	"SCHEDULED_CHARGING_CANNOT_COMPLETE": core.ChargingScheduled,
	"SCHEDULED_CHARGING_WILL_COMPLETE":   core.ChargingScheduled,
	"SMART_CHARGING_PAUSED":              core.ChargingScheduled,
	"IDLE":                               core.ChargingIdle,
	"DISCHARGING":                        core.ChargingDischarging,
	"DISCHARGING_V2H":                    core.ChargingDischarging,
	"DISCHARGING_V2L":                    core.ChargingDischarging,
	"ERROR":                              core.ChargingError,
}

func normalizeBattery(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			VIN                     string   `json:"vin"`
			Timestamp               *stamp   `json:"timestamp"`
			SoC                     *float64 `json:"batteryChargeLevelPercentage"`
			RangeKm                 *float64 `json:"estimatedDistanceToEmptyKm"`
			ChargerConnectionStatus string   `json:"chargerConnectionStatus"`
			ChargingStatusV2        string   `json:"chargingStatusV2"`
			ChargingType            string   `json:"chargingType"`
			PowerW                  *float64 `json:"chargingPowerWatts"`
			Consumption             *float64 `json:"averageEnergyConsumptionKwhPer100Km"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	d, at := v.Data, v.Data.Timestamp.instant()
	s.Covers = core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
		core.FieldChargeType | core.FieldPower | core.FieldConsumption
	s.SoC = number(d.SoC, at)
	s.RangeKm = number(d.RangeKm, at)
	s.Connection = enum(d.ChargerConnectionStatus, "CHARGER_CONNECTION_STATUS_", at, map[string]core.Connection{
		"CONNECTED": core.Connected, "DISCONNECTED": core.Disconnected, "FAULT": core.ConnectionFault,
	})
	s.Charging = enum(d.ChargingStatusV2, "CHARGING_STATUS_V2_", at, chargingStatuses)
	// WIRELESS is no type a charge of the domain has: absent.
	s.ChargeType = enum(d.ChargingType, "CHARGING_TYPE_", at, map[string]core.ChargeType{
		"AC": core.AC, "DC": core.DC, "NONE": core.NoCharge,
	})
	s.PowerW = number(d.PowerW, at)
	if !s.PowerW.OK && s.Charging.OK && s.Charging.V != core.ChargingActive {
		// The power is only reported while charging, its zero left out. During an active
		// charge, a missing power is a model that does not report it: absent.
		s.PowerW = core.Some(0.0, at)
	}
	s.ConsumptionKWhPer100km = number(d.Consumption, at)
	model(d.VIN, s)
	return nil
}

// model reads what the VIN tells of the model: the Data Portal gives nothing else
// (GET /v1/vehicles lists VINs alone). The family is left absent: which VIN positions
// tell a Polestar 2 from a 3, 4 or 5 is not known from real VINs, and a wrong family
// would recognize a wrong variant; the brand stands in for it, among whose variants the
// user chooses. Read with the battery, the domain read most often.
func model(vin string, s *core.Snapshot) {
	if len(vin) != 17 {
		return
	}
	s.Covers |= core.FieldModel
	s.Brand = core.Some(Brand, time.Time{})
	if y, ok := modelYear(vin); ok {
		s.ModelYear = core.Some(y, time.Time{})
	}
	// Assumption: the Data Portal serves the Polestar 2, 3, 4 and 5, all battery
	// electric; the Polestar 1, a plug-in hybrid, is not among its eligible models.
	s.BatteryElectric = core.Some(true, time.Time{})
}

// modelYear reads the model year at the VIN's tenth position (ISO 3779, on a 30-year
// cycle), from 2010 to 2039: Polestar has made cars since 2017 only.
func modelYear(vin string) (int, bool) {
	const codes = "ABCDEFGHJKLMNPRSTVWXY123456789"
	i := strings.IndexByte(codes, vin[9])
	if i < 0 {
		return 0, false
	}
	return 2010 + i, true
}

func normalizeOdometer(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			Timestamp      *stamp   `json:"timestamp"`
			OdometerMeters *float64 `json:"odometerMeters"`
			TripMeterKm    *float64 `json:"tripMeterAutomaticKm"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	at := v.Data.Timestamp.instant()
	s.Covers = core.FieldOdometer | core.FieldTripMeter
	if m := v.Data.OdometerMeters; m != nil {
		s.OdometerKm = core.Some(*m/1000, at)
	}
	s.TripMeterKm = number(v.Data.TripMeterKm, at)
	return nil
}

func normalizeLocation(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			Timestamp  *stamp `json:"timestamp"`
			Coordinate *struct {
				Latitude  *float64 `json:"latitude"`
				Longitude *float64 `json:"longitude"`
			} `json:"coordinate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	s.Covers = core.FieldPosition
	// A coordinate at 0 would be left out; one without both is taken as unknown.
	if c := v.Data.Coordinate; c != nil && c.Latitude != nil && c.Longitude != nil {
		s.Position = core.Some(core.Position{Lat: *c.Latitude, Lon: *c.Longitude}, v.Data.Timestamp.instant())
	}
	return nil
}

// usageModes maps usageMode: the API has no engine status, and a car driving reports
// DRIVING (ENGINE_ON: a combustion engine's, taken the same).
var usageModes = map[string]core.EngineState{
	"DRIVING": core.EngineRunning, "ENGINE_ON": core.EngineRunning,
	"ABANDONED": core.EngineStopped, "INACTIVE": core.EngineStopped, "CONVENIENCE": core.EngineStopped,
	"ACTIVE": core.EngineStopped, "ENGINE_OFF": core.EngineStopped,
}

func normalizeAvailability(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			Timestamp *stamp `json:"timestamp"`
			UsageMode string `json:"usageMode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	// UNSPECIFIED, left out, or unknown: the reading does not cover the engine, which
	// keeps its previous state.
	if e := enum(v.Data.UsageMode, "USAGE_MODE_", v.Data.Timestamp.instant(), usageModes); e.OK {
		s.Covers, s.Engine = core.FieldEngine, e
	}
	return nil
}

func normalizeTargetSoC(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			Timestamp *stamp `json:"timestamp"`
			TargetSoC *struct {
				Level *float64 `json:"batteryChargeTargetLevel"`
			} `json:"targetSoc"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	s.Covers = core.FieldTargetSoC
	if t := v.Data.TargetSoC; t != nil {
		s.TargetSoC = number(t.Level, v.Data.Timestamp.instant())
	}
	return nil
}
