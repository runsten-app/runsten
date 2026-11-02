package polestarid

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"runsten/internal/core"
)

// Source names the Polestar app's API, read with the Polestar ID, among the sources of
// readings.
const Source = "polestar_id"

// Brand is the maker of every car the API reads, as the catalog names it.
const Brand = "polestar"

// Normalizer reads the stored responses of the API into snapshots, for the derivation.
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads.
func (Normalizer) Kinds() []core.Kind { return kinds(Telematics, Battery, TargetSoC, Cars) }

// HealthKinds returns the kinds of response NormalizeHealth reads.
func (Normalizer) HealthKinds() []core.Kind { return kinds(Telematics) }

// EnergyKinds returns the kinds holding the energy state: the state of charge and the
// range, and the gRPC's battery the charging power.
func (Normalizer) EnergyKinds() []core.Kind { return kinds(Telematics, Battery) }

// StateKinds returns none: the API tells neither the doors nor the windows.
func (Normalizer) StateKinds() []core.Kind { return nil }

// SleepKinds returns none: the API does not tell whether the car sleeps.
func (Normalizer) SleepKinds() []core.Kind { return nil }

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

func kinds(eps ...Endpoint) []core.Kind {
	out := make([]core.Kind, len(eps))
	for i, ep := range eps {
		out[i] = core.Kind{Source: Source, Endpoint: string(ep)}
	}
	return out
}

// Normalize converts a raw response of ep into a vendor-neutral snapshot, each value
// dated by its block's timestamp, the car's.
//
// What the API gives is less than the Data Portal's: no position and no engine, the
// trips derived from the odometer. The GraphQL tells no cable nor charging power either:
// a car the gRPC's battery serves has them, else its charges come from the charging
// status and the state of charge.
func Normalize(ep Endpoint, raw []byte) (core.Snapshot, error) {
	var s core.Snapshot
	var err error
	switch ep {
	case Telematics:
		err = normalizeTelematics(raw, &s)
	case Battery:
		err = normalizeBattery(raw, &s)
	case TargetSoC:
		err = normalizeTargetSoC(raw, &s)
	case Cars:
		err = normalizeCars(raw, &s)
	default:
		return s, nil
	}
	if err != nil {
		return core.Snapshot{}, fmt.Errorf("normalize %s: %w", ep, err)
	}
	return s, nil
}

// stamp is protobuf's Timestamp as the API writes it: {"seconds": "<int64>", "nanos"},
// the seconds in a string (a number is read too).
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

// telematics is a response of carTelematicsV2 for one car: a list of each kind, of one
// entry (or none, a kind the car does not give).
type telematics struct {
	Data struct {
		T struct {
			Battery []struct {
				VIN       string   `json:"vin"`
				Timestamp *stamp   `json:"timestamp"`
				SoC       *float64 `json:"batteryChargeLevelPercentage"`
				RangeKm   *float64 `json:"estimatedDistanceToEmptyKm"`
				Status    string   `json:"chargingStatusV2"`
			} `json:"battery"`
			Odometer []struct {
				Timestamp *stamp   `json:"timestamp"`
				Meters    *float64 `json:"odometerMeters"`
			} `json:"odometer"`
			Health []healthBlock `json:"health"`
		} `json:"carTelematicsV2"`
	} `json:"data"`
}

// chargingStatuses maps chargingStatusV2 of the GraphQL, and chargingStatus of the gRPC's
// battery (its proto's ChargingStatus). pypolestar reads only CHARGING and IDLE of the
// GraphQL; the others are the Data Portal's, assumed the same words 🟡 (to check on a real
// car). An unknown one is absent.
var chargingStatuses = map[string]core.ChargingStatus{
	"CHARGING":              core.ChargingActive,
	"SMART_CHARGING":        core.ChargingActive,
	"DONE":                  core.ChargingDone,
	"SCHEDULED":             core.ChargingScheduled,
	"SMART_CHARGING_PAUSED": core.ChargingScheduled,
	"IDLE":                  core.ChargingIdle,
	"DISCHARGING":           core.ChargingDischarging,
	"ERROR":                 core.ChargingError,
}

// normalizeTelematics reads the battery and the odometer. Not the model: covering it
// covers the family too, which the telematics do not tell and the cars' list does; the
// model is the cars' list's alone.
func normalizeTelematics(raw []byte, s *core.Snapshot) error {
	var v telematics
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	t := v.Data.T
	if len(t.Battery) > 0 {
		b := t.Battery[0]
		at := b.Timestamp.instant()
		s.Covers |= core.FieldSoC | core.FieldRange | core.FieldCharging
		s.SoC = number(b.SoC, at)
		s.RangeKm = number(b.RangeKm, at)
		if name, ok := strings.CutPrefix(b.Status, "CHARGING_STATUS_V2_"); ok {
			if c, ok := chargingStatuses[name]; ok {
				s.Charging = core.Some(c, at)
			}
		}
	}
	if len(t.Odometer) > 0 {
		o := t.Odometer[0]
		s.Covers |= core.FieldOdometer
		if o.Meters != nil {
			s.OdometerKm = core.Some(*o.Meters/1000, o.Timestamp.instant())
		}
	}
	return nil
}

// enum reads an enum value without its prefix (CHARGING_TYPE_AC: AC); absent when it has
// not the prefix, or is not among values.
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

// normalizeBattery reads GetLatestBattery's battery, the Data Portal's battery domain
// over gRPC: the cable, the type and power of the charge, its current and voltage, the
// time left.
//
// The state of charge, the range and the charging status are the telematics' too: the
// battery covers each only when it tells it, so that a value protobuf leaves out (a zero,
// UNSPECIFIED) does not erase the one the telematics read. The rest only the battery
// tells.
func normalizeBattery(raw []byte, s *core.Snapshot) error {
	var v struct {
		Battery *struct {
			Timestamp     *stamp   `json:"timestamp"`
			SoC           *float64 `json:"batteryChargeLevelPercentage"`
			RangeKm       *float64 `json:"estimatedDistanceToEmptyKm"`
			Connection    string   `json:"chargerConnectionStatus"`
			Status        string   `json:"chargingStatus"`
			Type          string   `json:"chargingType"`
			PowerW        *float64 `json:"chargingPowerWatts"`
			CurrentA      *float64 `json:"chargingCurrentAmps"`
			VoltageV      *float64 `json:"chargingVoltageVolts"`
			Consumption   *float64 `json:"averageEnergyConsumptionKwhPer100Km"`
			MinutesToFull *float64 `json:"estimatedChargingTimeToFullMinutes"`
		} `json:"battery"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	b := v.Battery
	if b == nil {
		return nil
	}
	at := b.Timestamp.instant()
	s.Covers = core.FieldConnection | core.FieldChargeType | core.FieldPower | core.FieldConsumption | core.FieldChargeDetails
	if b.SoC != nil {
		s.Covers |= core.FieldSoC
		s.SoC = core.Some(*b.SoC, at)
	}
	if b.RangeKm != nil {
		s.Covers |= core.FieldRange
		s.RangeKm = core.Some(*b.RangeKm, at)
	}
	// Its first version's words (chargingStatus), the GraphQL's being V2: the same,
	// without V2_.
	if c := enum(b.Status, "CHARGING_STATUS_", at, chargingStatuses); c.OK {
		s.Covers |= core.FieldCharging
		s.Charging = c
	}
	s.Connection = enum(b.Connection, "CHARGER_CONNECTION_STATUS_", at, map[string]core.Connection{
		"CONNECTED": core.Connected, "DISCONNECTED": core.Disconnected, "FAULT": core.ConnectionFault,
	})
	// WIRELESS is no type a charge has here: absent.
	s.ChargeType = enum(b.Type, "CHARGING_TYPE_", at, map[string]core.ChargeType{
		"AC": core.AC, "DC": core.DC, "NONE": core.NoCharge,
	})
	charging := s.Charging.OK && s.Charging.V == core.ChargingActive
	s.PowerW = number(b.PowerW, at)
	if !s.PowerW.OK && s.Charging.OK && !charging {
		// protobuf leaves a zero out: out of a charge, the power is 0. During one, a power
		// missing is a car that does not tell it: absent.
		s.PowerW = core.Some(0.0, at)
	}
	s.ConsumptionKWhPer100km = number(b.Consumption, at)
	// Assumption, as the Data Portal's: the battery's side on DC, the grid's on AC; the
	// phases are not told.
	s.CurrentA, s.VoltageV = number(b.CurrentA, at), number(b.VoltageV, at)
	// Said only while charging: a real car idle and plugged in told 1 minute (pypolestar's
	// fixture). To full, as told, which the car may count to its target as well.
	if m := b.MinutesToFull; charging && m != nil && *m > 0 {
		s.TimeLeft = core.Some(core.TimeLeft{Duration: time.Duration(*m * float64(time.Minute)), To: core.ToFull}, at)
	}
	return nil
}

// normalizeTargetSoC reads GetTargetSoc's charge limit. The response holds no reading's
// time: its updatedAt is when the owner set it, not when the car told it.
func normalizeTargetSoC(raw []byte, s *core.Snapshot) error {
	var v struct {
		TargetSoC *struct {
			Level *float64 `json:"batteryChargeTargetLevel"`
		} `json:"targetSoc"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	s.Covers = core.FieldTargetSoC
	if t := v.TargetSoC; t != nil {
		s.TargetSoC = number(t.Level, time.Time{})
	}
	return nil
}

// familyRe reads modelName, written without its space ("Polestar4", as pypolestar notes).
var familyRe = regexp.MustCompile(`^Polestar\s?(\d+)$`)

// normalizeCars reads the model: the family from modelName, which the Data Portal does
// not give, so that the variant is recognized without asking when its family has one.
func normalizeCars(raw []byte, s *core.Snapshot) error {
	var v struct {
		Data struct {
			Cars []struct {
				VIN       string `json:"vin"`
				ModelName string `json:"modelName"`
				ModelYear string `json:"modelYear"`
			} `json:"getConsumerCarsV2"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err //nolint:wrapcheck // wrapped by Normalize
	}
	if len(v.Data.Cars) == 0 {
		return nil
	}
	c := v.Data.Cars[0]
	s.Covers = core.FieldModel
	s.Brand = core.Some(Brand, time.Time{})
	if m := familyRe.FindStringSubmatch(strings.TrimSpace(c.ModelName)); m != nil {
		s.Family = core.Some("Polestar "+m[1], time.Time{})
	}
	if y, err := strconv.Atoi(c.ModelYear); err == nil && y >= 2017 && y < 2100 {
		s.ModelYear = core.Some(y, time.Time{})
	} else if y, ok := modelYear(c.VIN); ok {
		s.ModelYear = core.Some(y, time.Time{})
	}
	// Assumption: every car of the Polestar app is battery electric but the Polestar 1, a
	// plug-in hybrid, which pypolestar does not read.
	s.Powertrain = core.Some(core.Electric, time.Time{})
	return nil
}

// modelYear reads the model year at the VIN's tenth position (ISO 3779, on a 30-year
// cycle), from 2010 to 2039: Polestar has made cars since 2017 only.
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
