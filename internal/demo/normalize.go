package demo

import (
	"crypto/sha256"
	"fmt"
	"time"

	"runsten/internal/core"
)

// Normalizer reads the stored readings into snapshots, for the derivation. Every value is
// read at the time its snapshot was fetched: the vehicle is read at once, never from a
// cache of its own.
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads: the one endpoint.
func (Normalizer) Kinds() []core.Kind { return kinds() }

// EnergyKinds returns the kind holding the energy state: the same.
func (Normalizer) EnergyKinds() []core.Kind { return kinds() }

// StateKinds returns none: everything rides in the one reading.
func (Normalizer) StateKinds() []core.Kind { return nil }

// HealthKinds returns none: the demo vehicle tells no tyre, service nor warning.
func (Normalizer) HealthKinds() []core.Kind { return nil }

// SleepKinds returns none: the vehicle never sleeps.
func (Normalizer) SleepKinds() []core.Kind { return nil }

// Normalize reads a stored response of kind k.
func (Normalizer) Normalize(k core.Kind, raw []byte) (core.Snapshot, error) {
	if k.Source != Source {
		return core.Snapshot{}, fmt.Errorf("normalize: source %q is not %s", k.Source, Source)
	}
	if Endpoint(k.Endpoint) != Vehicle {
		return core.Snapshot{}, fmt.Errorf("normalize: unknown endpoint %q", k.Endpoint)
	}
	return Normalize(raw)
}

// NormalizeHealth reads nothing: the demo vehicle tells no health (HealthKinds is empty).
func (Normalizer) NormalizeHealth(k core.Kind, _ []byte) (core.Health, error) {
	return core.Health{}, fmt.Errorf("normalize health: %s tells no health (%s)", Source, k.Endpoint)
}

func kinds() []core.Kind { return []core.Kind{{Source: Source, Endpoint: string(Vehicle)}} }

// Normalize converts a stored reading into a vendor-neutral snapshot.
func Normalize(raw []byte) (core.Snapshot, error) {
	r, err := Parse(raw)
	if err != nil {
		return core.Snapshot{}, fmt.Errorf("normalize: %w", err)
	}
	var none time.Time // read when the snapshot was fetched
	s := core.Snapshot{Covers: core.FieldEngine | core.FieldSoC | core.FieldRange | core.FieldCharging |
		core.FieldConnection | core.FieldChargeType | core.FieldPower | core.FieldTargetSoC | core.FieldOdometer |
		core.FieldPosition | core.FieldClimate | core.FieldCapacity | core.FieldModel | core.FieldSheet}
	s.Engine = core.Some(core.EngineStopped, none)
	if r.Driving {
		s.Engine = core.Some(core.EngineRunning, none)
	}
	s.SoC = core.Some(r.SoC, none)
	s.RangeKm = core.Some(r.RangeKm, none)
	s.TargetSoC = core.Some(r.TargetSoC, none)
	s.OdometerKm = core.Some(r.OdometerKm, none)
	s.Position = core.Some(core.Position{Lat: r.Latitude, Lon: r.Longitude}, none)
	s.PowerW = core.Some(r.PowerKW*1000, none)
	s.Connection = core.Some(core.Disconnected, none)
	if r.Plugged {
		s.Connection = core.Some(core.Connected, none)
	}
	switch r.Charging {
	case "charging":
		s.Charging = core.Some(core.ChargingActive, none)
	case "done":
		s.Charging = core.Some(core.ChargingDone, none)
	default:
		s.Charging = core.Some(core.ChargingIdle, none)
	}
	switch r.ChargeType {
	case "ac":
		s.ChargeType = core.Some(core.AC, none)
	case "dc":
		s.ChargeType = core.Some(core.DC, none)
	default:
		s.ChargeType = core.Some(core.NoCharge, none)
	}
	s.OutsideTempC = core.Some(r.OutsideTempC, none)
	s.CapacityKWh = core.Some(r.CapacityKWh, none)
	s.Brand = core.Some(r.Brand, none)
	s.Family = core.Some(r.Family, none)
	s.ModelYear = core.Some(r.ModelYear, none)
	s.Powertrain = core.Some(core.Electric, none)
	if r.Name != "" {
		s.Sheet = core.Some(core.Sheet{Name: r.Name}, none)
	}
	return s, nil
}

// Fingerprint returns the fingerprint of a reading's values, its instant excluded: two
// readings with the same bring nothing new.
func Fingerprint(raw []byte) ([]byte, error) {
	r, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	r.At = time.Time{}
	canonical, err := Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return sum[:], nil
}
