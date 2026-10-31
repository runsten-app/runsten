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

// SleepState is whether the vehicle is awake, for a source that lets it fall asleep and
// tells it without waking it (Tesla).
type SleepState string

// Sleep states.
const (
	Awake  SleepState = "awake"
	Asleep SleepState = "asleep"
	// Offline is a vehicle out of reach: asleep, or without a network, the source cannot
	// tell.
	Offline SleepState = "offline"
)

// Powertrain is what moves the vehicle.
type Powertrain string

// Powertrains.
const (
	// Electric runs on its battery alone.
	Electric Powertrain = "electric"
	// PlugInHybrid has a battery charged on a plug and an engine, which may also move the
	// SoC while driving (charge sustain, a charge mode).
	PlugInHybrid Powertrain = "plugin_hybrid"
	// OtherPowertrain is a combustion car, or a fuel the source names that Runsten does
	// not know.
	OtherPowertrain Powertrain = "other"
)

// Field identifies a value of Snapshot.
type Field uint32

// Snapshot fields, in processing order for readings taken at the same time: the engine
// and energy states first, as the collector reads them first in a pass.
const (
	FieldEngine Field = 1 << iota
	// FieldSoC covers SoC and UsableSoC: a source that tells the usable level tells it
	// with the SoC, in the same block.
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
	// FieldModel covers Brand, Family, ModelYear, Trim and Powertrain: what identifies
	// the model, always reported together, by the same response as the capacity.
	FieldModel
	// FieldSleep covers Sleep: no other value depends on it.
	FieldSleep
	FieldEnergyAdded
	// FieldSoftware covers Software and SoftwareUpdate, told in the same block.
	FieldSoftware
	// FieldClimate covers OutsideTempC, CabinTempC and Climate, told in the same block.
	FieldClimate

	fieldCount = iota
)

// index returns the position of the lowest field of f.
func (f Field) index() int { return bits.TrailingZeros32(uint32(f)) }

// Snapshot is a normalized reading of some of the vehicle's values. Covers lists the
// fields the reading reports on: a covered field that is absent replaces the previous
// value, an uncovered field leaves it unchanged.
type Snapshot struct {
	Covers Field

	Engine  Value[EngineState]
	SoC     Value[float64] // %
	RangeKm Value[float64]
	// UsableSoC is the part of the battery the vehicle can draw on now, in % of it (Tesla's
	// usable_battery_level): below SoC when the cold locks some away.
	UsableSoC   Value[float64]
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
	// EnergyAddedKWh is the energy the vehicle counts into its battery since its charging
	// session began (Tesla's charge_energy_added): it holds after the charge, and starts
	// again from zero with the next session.
	EnergyAddedKWh Value[float64]

	// Brand is the maker (volvo, polestar), when the source tells it; Family the model as
	// the vendor names it (XC40, EX30), without its powertrain or battery.
	Brand     Value[string]
	Family    Value[string]
	ModelYear Value[int]
	// Trim is the vendor's badge of the powertrain and battery, when it tells one (Tesla's
	// trim_badging: 74d, p74d, 50).
	Trim Value[string]
	// Powertrain tells whether the vehicle runs on its battery alone, is a plug-in hybrid,
	// or neither.
	Powertrain Value[Powertrain]

	// Sleep is whether the vehicle is awake, asleep or out of reach.
	Sleep Value[SleepState]

	// Software is the version of the vehicle's software (Tesla's car_version, without
	// its build hash), and SoftwareUpdate the update waiting for it: absent when none is.
	Software       Value[string]
	SoftwareUpdate Value[SoftwareUpdate]

	// OutsideTempC is the temperature the vehicle measures outside, CabinTempC inside, in
	// °C; Climate what heats or cools it.
	OutsideTempC Value[float64]
	CabinTempC   Value[float64]
	Climate      Value[Climate]
}

// Climate is what of the vehicle's climate runs. Each is absent when the source does not
// tell it.
type Climate struct {
	// On is the cabin's climate running, by hand, from the app or on a schedule.
	On Value[bool]
	// Preconditioning is the vehicle heating or cooling its cabin and battery before a
	// departure.
	Preconditioning Value[bool]
	// BatteryHeater is the battery being heated.
	BatteryHeater Value[bool]
}

// UpdateStatus is where an update of the vehicle's software stands.
type UpdateStatus string

// Update statuses.
const (
	UpdateAvailable   UpdateStatus = "available"
	UpdateScheduled   UpdateStatus = "scheduled"
	UpdateDownloading UpdateStatus = "downloading"
	// UpdateWaitingWiFi is an update the vehicle downloads only over Wi-Fi, waiting for
	// one.
	UpdateWaitingWiFi UpdateStatus = "waiting_wifi"
	UpdateInstalling  UpdateStatus = "installing"
	// UpdateUnknown is an update whose status the source tells in a word Runsten does not
	// know: one waits all the same.
	UpdateUnknown UpdateStatus = "unknown"
)

// SoftwareUpdate is an update waiting for the vehicle's software.
type SoftwareUpdate struct {
	Status UpdateStatus
	// Version is the version it installs; empty when the source does not tell it.
	Version string
	// DownloadPct and InstallPct are its progress, in %.
	DownloadPct, InstallPct Value[float64]
	// Duration is how long the vehicle expects its installation to take.
	Duration Value[time.Duration]
	// ScheduledAt is when the owner set it to install.
	ScheduledAt Value[time.Time]
}

// BatteryElectric is whether the vehicle is known to run on its battery alone.
func (s Snapshot) BatteryElectric() bool { return s.Powertrain.OK && s.Powertrain.V == Electric }

// PlugInHybrid is whether the vehicle is known to be a plug-in hybrid.
func (s Snapshot) PlugInHybrid() bool { return s.Powertrain.OK && s.Powertrain.V == PlugInHybrid }

// merge copies the fields covered by o.
func (s *Snapshot) merge(o Snapshot) {
	s.Covers |= o.Covers
	c := o.Covers
	set := func(f Field) bool { return c&f != 0 }
	if set(FieldEngine) {
		s.Engine = o.Engine
	}
	if set(FieldSoC) {
		s.SoC, s.UsableSoC = o.SoC, o.UsableSoC
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
		s.Brand, s.Family, s.ModelYear, s.Trim, s.Powertrain = o.Brand, o.Family, o.ModelYear, o.Trim, o.Powertrain
	}
	if set(FieldSleep) {
		s.Sleep = o.Sleep
	}
	if set(FieldEnergyAdded) {
		s.EnergyAddedKWh = o.EnergyAddedKWh
	}
	if set(FieldSoftware) {
		s.Software, s.SoftwareUpdate = o.Software, o.SoftwareUpdate
	}
	if set(FieldClimate) {
		s.OutsideTempC, s.CabinTempC, s.Climate = o.OutsideTempC, o.CabinTempC, o.Climate
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
