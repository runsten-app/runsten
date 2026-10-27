// Package core is the vendor-neutral domain: normalized snapshots, and the trips and
// charges derived from them.
//
// Vendor formats never reach this package: an adapter (internal/volvo today, Polestar
// later) converts its responses into Snapshot values, in km, kWh, W and %, with UTC
// timestamps.
package core

import (
	"math/bits"
	"time"
)

// Value is an optional reading. An absent value (OK false) is never a zero: the vendor
// did not report it, reported it as null, or as unsupported.
type Value[T any] struct {
	V  T
	At time.Time // vehicle-side timestamp of the reading, UTC; zero if unknown
	OK bool
}

// Some returns a present value read by the vehicle at at.
func Some[T any](v T, at time.Time) Value[T] { return Value[T]{V: v, At: at.UTC(), OK: true} }

// Position is a WGS 84 position, in degrees.
type Position struct {
	Lat, Lon float64
}

// EngineState tells whether the vehicle reports itself as running.
type EngineState string

// Engine states.
const (
	EngineRunning EngineState = "running"
	EngineStopped EngineState = "stopped"
)

// ChargingStatus is the state of the charging system.
type ChargingStatus string

// Charging statuses.
const (
	ChargingIdle        ChargingStatus = "idle"
	ChargingActive      ChargingStatus = "charging"
	ChargingDone        ChargingStatus = "done"
	ChargingScheduled   ChargingStatus = "scheduled"
	ChargingDischarging ChargingStatus = "discharging"
	ChargingError       ChargingStatus = "error"
)

// Connection is the state of the charging cable.
type Connection string

// Cable states.
const (
	Connected       Connection = "connected"
	Disconnected    Connection = "disconnected"
	ConnectionFault Connection = "fault"
)

// ChargeType is the current type of a charge.
type ChargeType string

// Charge types.
const (
	AC ChargeType = "AC"
	DC ChargeType = "DC"
	// NoCharge is a vehicle's own word that no charge runs: a reading of the current
	// state, never a charge's type.
	NoCharge ChargeType = "none"
)

// Field identifies a value of Snapshot.
type Field uint16

// Snapshot fields, in processing order for readings taken at the same time: the engine
// and energy states first, as the collector reads them first in a pass.
const (
	FieldEngine Field = 1 << iota
	FieldSoC
	FieldRange
	FieldCharging
	FieldConnection
	FieldChargeType
	FieldPower
	FieldTargetSoC
	FieldOdometer
	FieldPosition
	FieldTripMeter
	FieldConsumption
	FieldCapacity
	// FieldModel covers Brand, Family, ModelYear, Trim and BatteryElectric: what identifies
	// the model, always reported together, by the same response as the capacity.
	FieldModel

	fieldCount = iota
)

// index returns the position of the lowest field of f.
func (f Field) index() int { return bits.TrailingZeros16(uint16(f)) }

// Snapshot is a normalized reading of some of the vehicle's values. Covers lists the
// fields the reading reports on: a covered field that is absent replaces the previous
// value, an uncovered field leaves it unchanged.
type Snapshot struct {
	Covers Field

	Engine      Value[EngineState]
	SoC         Value[float64] // %
	RangeKm     Value[float64]
	Charging    Value[ChargingStatus]
	Connection  Value[Connection]
	ChargeType  Value[ChargeType]
	PowerW      Value[float64] // charging power
	TargetSoC   Value[float64] // %
	OdometerKm  Value[float64]
	Position    Value[Position]
	TripMeterKm Value[float64] // vehicle trip meter, stored for comparison only
	// ConsumptionKWhPer100km is the vehicle's average consumption for its trip meter,
	// stored for comparison only.
	ConsumptionKWhPer100km Value[float64]
	CapacityKWh            Value[float64] // battery capacity

	// Brand is the maker (volvo, polestar), when the source tells it; Family the model as
	// the vendor names it (XC40, EX30), without its powertrain or battery.
	Brand     Value[string]
	Family    Value[string]
	ModelYear Value[int]
	// Trim is the vendor's badge of the powertrain and battery, when it tells one (Tesla's
	// trim_badging: 74d, p74d, 50).
	Trim Value[string]
	// BatteryElectric tells whether the vehicle runs on its battery alone: false for a
	// hybrid or a combustion car.
	BatteryElectric Value[bool]
}

// merge copies the fields covered by o.
func (s *Snapshot) merge(o Snapshot) {
	s.Covers |= o.Covers
	c := o.Covers
	set := func(f Field) bool { return c&f != 0 }
	if set(FieldEngine) {
		s.Engine = o.Engine
	}
	if set(FieldSoC) {
		s.SoC = o.SoC
	}
	if set(FieldRange) {
		s.RangeKm = o.RangeKm
	}
	if set(FieldCharging) {
		s.Charging = o.Charging
	}
	if set(FieldConnection) {
		s.Connection = o.Connection
	}
	if set(FieldChargeType) {
		s.ChargeType = o.ChargeType
	}
	if set(FieldPower) {
		s.PowerW = o.PowerW
	}
	if set(FieldTargetSoC) {
		s.TargetSoC = o.TargetSoC
	}
	if set(FieldOdometer) {
		s.OdometerKm = o.OdometerKm
	}
	if set(FieldPosition) {
		s.Position = o.Position
	}
	if set(FieldTripMeter) {
		s.TripMeterKm = o.TripMeterKm
	}
	if set(FieldConsumption) {
		s.ConsumptionKWhPer100km = o.ConsumptionKWhPer100km
	}
	if set(FieldCapacity) {
		s.CapacityKWh = o.CapacityKWh
	}
	if set(FieldModel) {
		s.Brand, s.Family, s.ModelYear, s.Trim, s.BatteryElectric = o.Brand, o.Family, o.ModelYear, o.Trim, o.BatteryElectric
	}
}

// Kind is a kind of stored response: the source of the readings (the Volvo Cars API, an
// app, an import) and one of its endpoints. Each source names its own endpoints.
type Kind struct {
	Source, Endpoint string
}

// Record is a stored snapshot. Its values were read at FetchedAt and were read again,
// unchanged, at CheckedAt (the last identical response). Nothing is known in between:
// failed calls do not interrupt a run of identical responses.
type Record struct {
	Source    string // the source of the readings, as in Kind
	FetchedAt time.Time
	CheckedAt time.Time
	Snapshot  Snapshot
}
