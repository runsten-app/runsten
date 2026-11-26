package ford

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"runsten/internal/core"
)

// Source names FordConnect Query among the sources of readings, and the provider of its
// connections.
const Source = "ford"

// Brand is the maker the catalog knows the cars by.
const Brand = "ford"

// Endpoint is what the collector reads of the car, named as the API's path names it.
type Endpoint string

// The endpoints read.
const (
	// Telemetry is the car's state: GET /v1/telemetry.
	Telemetry Endpoint = "telemetry"
	// Garage is the car itself: GET /v1/garage.
	Garage Endpoint = "garage"
)

// Normalizer reads the stored responses of the API into snapshots, for the derivation.
//
// Every value of the telemetry is dated from the car's own send, the updateTime of its
// metric: Ford returns the last value the car sent of each, without waking it, and the
// metrics of one response may be of different ages (core.Params.VehicleTimestamps). A
// metric the response lacks is left out of Covers, so that it keeps its previous value:
// which metrics a model tells differs from one to another. A metric the car sent with a
// word this does not know (a value new to the enumeration, UNKNOWN, UNRECOGNIZED) is
// covered and absent: the car said something else than before, so the previous value no
// longer holds, and a charge or a drive must not go on for a word not understood. The
// word stays in the raw response, on the debug page.
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads.
func (Normalizer) Kinds() []core.Kind { return kinds(Telemetry, Garage) }

// EnergyKinds returns the kind holding the energy state: the telemetry.
func (Normalizer) EnergyKinds() []core.Kind { return kinds(Telemetry) }

// StateKinds returns none: the doors and the windows ride in the telemetry, with the
// energy.
func (Normalizer) StateKinds() []core.Kind { return nil }

// HealthKinds returns the kind holding the tyres: the telemetry.
func (Normalizer) HealthKinds() []core.Kind { return kinds(Telemetry) }

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
	if k.Source != Source || Endpoint(k.Endpoint) != Telemetry {
		return core.Health{}, fmt.Errorf("normalize health: %s/%s tells no health", k.Source, k.Endpoint)
	}
	return NormalizeHealth(raw)
}

func kinds(eps ...Endpoint) []core.Kind {
	out := make([]core.Kind, len(eps))
	for i, ep := range eps {
		out[i] = core.Kind{Source: Source, Endpoint: string(ep)}
	}
	return out
}

// Normalize converts a raw response of endpoint ep into a vendor-neutral snapshot.
func Normalize(ep Endpoint, raw []byte) (core.Snapshot, error) {
	switch ep {
	case Telemetry:
		m, err := metricsOf(raw)
		if err != nil {
			return core.Snapshot{}, fmt.Errorf("normalize: %w", err)
		}
		var s core.Snapshot
		normalizeDrive(m, &s)
		normalizeBattery(m, &s)
		normalizeCharge(m, &s)
		normalizeOpenings(m, &s)
		return s, nil
	case Garage:
		return normalizeGarage(raw)
	}
	return core.Snapshot{}, fmt.Errorf("normalize: unknown endpoint %q", ep)
}

// metrics are the telemetry's metrics by name, each decoded on its own: one of a shape
// this does not expect is left out, never the whole response.
type metrics map[string]json.RawMessage

// metricsOf reads the metrics of a telemetry response.
func metricsOf(raw []byte) (metrics, error) {
	var r struct {
		Metrics metrics `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err //nolint:wrapcheck // wrapped by the caller
	}
	if r.Metrics == nil {
		return nil, fmt.Errorf("no metrics in the response")
	}
	return r.Metrics, nil
}

// metric is a metric of one value, and when the car sent it.
type metric[T any] struct {
	At    time.Time `json:"updateTime"`
	Value *T        `json:"value"`
}

// read decodes the metric name; false when the response lacks it, or gives it no value
// or another shape.
func read[T any](m metrics, name string) (T, time.Time, bool) {
	var v metric[T]
	raw, ok := m[name]
	if !ok || json.Unmarshal(raw, &v) != nil || v.Value == nil {
		var zero T
		return zero, time.Time{}, false
	}
	return *v.Value, v.At.UTC(), true
}

// num reads a number metric, the first of names the response has.
func num(m metrics, names ...string) (float64, time.Time, bool) {
	for _, name := range names {
		if v, at, ok := read[float64](m, name); ok {
			return v, at, true
		}
	}
	return 0, time.Time{}, false
}

// word reads an enumeration metric, upper-cased (the specification writes ON, a car was
// seen to send Run), the first of names the response has.
func word(m metrics, names ...string) (string, time.Time, bool) {
	for _, name := range names {
		if v, at, ok := read[string](m, name); ok {
			return strings.ToUpper(strings.TrimSpace(v)), at, true
		}
	}
	return "", time.Time{}, false
}

// ignitions maps ignitionStatus: the specification's OFF, ACCESSORY and ON, and the RUN
// and START the cars' own words carry in other clients' captures.
var ignitions = map[string]core.EngineState{
	"ON": core.EngineRunning, "RUN": core.EngineRunning, "START": core.EngineRunning,
	"OFF": core.EngineStopped, "ACCESSORY": core.EngineStopped,
}

// normalizeDrive reads the ignition, the odometer, the position and the outside
// temperature.
func normalizeDrive(m metrics, s *core.Snapshot) {
	if v, at, ok := word(m, "ignitionStatus"); ok {
		s.Covers |= core.FieldEngine
		if e, known := ignitions[v]; known {
			s.Engine = core.Some(e, at)
		}
	}
	// In km (assumption: metric whatever the car displays, as the other clients read it;
	// a car was seen to round it to the kilometre).
	if v, at, ok := num(m, "odometer"); ok {
		s.Covers |= core.FieldOdometer
		s.OdometerKm = core.Some(v, at)
	}
	type position struct {
		Location *struct {
			Lat *float64 `json:"lat"`
			Lon *float64 `json:"lon"`
		} `json:"location"`
		Dimension string `json:"gpsDimension"`
	}
	if p, at, ok := read[position](m, "position"); ok {
		// A position without a fix, or at the origin, is unknown, never the previous one:
		// the car may have moved since.
		s.Covers |= core.FieldPosition
		if l := p.Location; l != nil && l.Lat != nil && l.Lon != nil && p.Dimension != "NONE" && (*l.Lat != 0 || *l.Lon != 0) {
			s.Position = core.Some(core.Position{Lat: *l.Lat, Lon: *l.Lon}, at)
		}
	}
	// In °C (assumption, as the other clients read it).
	if v, at, ok := num(m, "outsideTemperature", "ambientTemp"); ok {
		s.Covers |= core.FieldClimate
		s.OutsideTempC = core.Some(v, at)
	}
}

// normalizeBattery reads the state of charge, the range and the charge's target.
//
// The level is the displayed one (xevBatteryStateOfCharge), which the car's screen
// shows; the actual one (xevBatteryActualStateOfCharge) stays in the raw response.
// batteryStateOfCharge is never read: it is the 12 V battery's on the cars seen. The
// capacity (xevBatteryCapacity) and the energy remaining are left in the raw response
// too: what they count, gross, net or the pack's present capacity, is not known.
func normalizeBattery(m metrics, s *core.Snapshot) {
	if v, at, ok := num(m, "xevBatteryStateOfCharge"); ok {
		s.Covers |= core.FieldSoC
		s.SoC = core.Some(v, at)
	}
	// In km (assumption, as the odometer).
	if v, at, ok := num(m, "xevBatteryRange"); ok {
		s.Covers |= core.FieldRange
		s.RangeKm = core.Some(v, at)
	}
	var conf struct {
		Target *metric[float64] `json:"xevBatteryTargetStateOfChargeSetting"`
	}
	if raw, ok := m["configurations"]; ok && json.Unmarshal(raw, &conf) == nil && conf.Target != nil && conf.Target.Value != nil {
		s.Covers |= core.FieldTargetSoC
		if v := *conf.Target.Value; v > 0 && v <= 100 {
			s.TargetSoC = core.Some(v, conf.Target.At.UTC())
		}
	}
}

// chargeStates maps xevBatteryChargeDisplayStatus, the charge as the car's screen tells
// it (the specification's values).
var chargeStates = map[string]core.ChargingStatus{
	"IN_PROGRESS":            core.ChargingActive,
	"COMPLETED":              core.ChargingDone,
	"SCHEDULED":              core.ChargingScheduled,
	"PAUSED":                 core.ChargingIdle,
	"STOPPED":                core.ChargingIdle,
	"NOT_READY":              core.ChargingIdle,
	"NOT_PLUGGED_IN":         core.ChargingIdle,
	"STATION_NOT_DETECTED":   core.ChargingIdle,
	"FAULT":                  core.ChargingError,
	"STATION_NOT_COMPATIBLE": core.ChargingError,
	"DISCHARGING":            core.ChargingDischarging,
}

// legacyChargeStates maps evBatteryChargeDisplayStatus, the metric's former name, read
// when the response lacks the other.
var legacyChargeStates = map[string]core.ChargingStatus{
	"IN_PROGRESS":            core.ChargingActive,
	"FAST_CHARGING":          core.ChargingActive,
	"COMPLETE":               core.ChargingDone,
	"SCHEDULED":              core.ChargingScheduled,
	"NOT_READY":              core.ChargingIdle,
	"FAULT_UNKNOWN_LOCATION": core.ChargingError,
	"FAULT_INSIDE_CAR":       core.ChargingError,
	"FAULT_OUTSIDE_CAR":      core.ChargingError,
}

// chargeTypes maps xevChargeStationPowerType.
var chargeTypes = map[string]core.ChargeType{
	"AC_BASIC": core.AC, "AC_SMART": core.AC, "DC_FAST": core.DC,
}

// normalizeCharge reads the charge, the plug, the current's kind, and while a charge
// runs its current, voltage, power and time left.
func normalizeCharge(m metrics, s *core.Snapshot) {
	charging := false
	if v, at, ok := word(m, "xevBatteryChargeDisplayStatus"); ok {
		s.Covers |= core.FieldCharging
		if st, known := chargeStates[v]; known {
			s.Charging = core.Some(st, at)
			charging = st == core.ChargingActive
		}
	} else if v, at, ok := word(m, "evBatteryChargeDisplayStatus"); ok {
		s.Covers |= core.FieldCharging
		if st, known := legacyChargeStates[v]; known {
			s.Charging = core.Some(st, at)
			charging = st == core.ChargingActive
		}
	}
	plugged, pluggedAt := false, time.Time{}
	if v, at, ok := word(m, "xevPlugChargerStatus", "evPlugChargerStatus"); ok {
		s.Covers |= core.FieldConnection
		switch v {
		case "CONNECTED":
			s.Connection = core.Some(core.Connected, at)
			plugged, pluggedAt = true, at
		case "DISCONNECTED":
			s.Connection = core.Some(core.Disconnected, at)
		}
	}
	// The station's kind stays in Ford's copy after the cable is out: it is this
	// charge's only when the car sent it since it was plugged in.
	if v, at, ok := word(m, "xevChargeStationPowerType"); ok {
		s.Covers |= core.FieldChargeType
		if t, known := chargeTypes[v]; known && plugged && !at.Before(pluggedAt) {
			s.ChargeType = core.Some(t, at)
		}
	}
	// The charger's current and voltage keep their last values once the charge is over,
	// and the voltage idles at a few volts (reported by other clients): they are read
	// while a charge runs only, and otherwise told absent.
	volts, voltsAt, okV := num(m, "xevBatteryChargerVoltageOutput")
	amps, ampsAt, okA := num(m, "xevBatteryChargerCurrentOutput")
	if okV || okA {
		s.Covers |= core.FieldChargeDetails
	}
	if okV && okA {
		s.Covers |= core.FieldPower
	}
	if charging {
		if okV && volts > 0 {
			s.VoltageV = core.Some(volts, voltsAt)
		}
		if okA && amps > 0 {
			s.CurrentA = core.Some(amps, ampsAt)
		}
		if okV && okA && volts > 0 && amps > 0 {
			s.PowerW = core.Some(volts*amps, later(voltsAt, ampsAt))
		}
	}
	// In minutes, to a full battery by its name (assumption: it may run to the target).
	if v, at, ok := num(m, "xevBatteryTimeToFullCharge"); ok {
		s.Covers |= core.FieldChargeDetails
		if charging && v > 0 {
			s.TimeLeft = core.Some(core.TimeLeft{Duration: time.Duration(v * float64(time.Minute)), To: core.ToFull}, at)
		}
	}
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// opening is an entry of doorStatus or doorLockStatus: a door, and its state.
type opening struct {
	At    time.Time `json:"updateTime"`
	Value string    `json:"value"`
	Door  string    `json:"vehicleDoor"`
}

// window is an entry of windowStatus: how far the window is open, as a range, both
// bounds at zero when it is closed.
type window struct {
	At    time.Time `json:"updateTime"`
	Value *struct {
		Range *struct {
			Lower float64 `json:"lowerBound"`
			Upper float64 `json:"upperBound"`
		} `json:"doubleRange"`
	} `json:"value"`
	Window string `json:"vehicleWindow"`
}

// list decodes a metric that is a list of entries; nil when absent or of another shape.
func list[T any](m metrics, name string) []T {
	var out []T
	if raw, ok := m[name]; !ok || json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// normalizeOpenings reads the lock, the doors, the lids and the windows.
//
// A door or a window the car names by its place (FRONT_LEFT) is read as it is. Some cars
// name them by the driver's or the passenger's side instead, which is left or right
// depending on the market: those are placed only when every entry says closed, each of
// the four then closed; one of them open is not placed, and the doors left unknown.
func normalizeOpenings(m metrics, s *core.Snapshot) {
	var d core.Doors
	var seen time.Time
	doors := list[opening](m, "doorStatus")
	locks := list[opening](m, "doorLockStatus")
	hood, hoodAt, hasHood := word(m, "hoodStatus")
	if doors != nil || locks != nil || hasHood {
		s.Covers |= core.FieldDoors
	}
	states := map[string]core.Opening{"CLOSED": core.Closed, "AJAR": core.Open}
	placed := map[string]*core.Value[core.Opening]{
		"FRONT_LEFT": &d.FrontLeft, "FRONT_RIGHT": &d.FrontRight, "REAR_LEFT": &d.RearLeft, "REAR_RIGHT": &d.RearRight,
		"TAILGATE": &d.Tailgate, "FRUNK": &d.Hood,
	}
	allClosed, some := true, false
	for _, e := range doors {
		st, known := states[strings.ToUpper(e.Value)]
		if !known {
			allClosed = false
			continue
		}
		some = true
		seen = later(seen, e.At.UTC())
		if st != core.Closed {
			allClosed = false
		}
		if p, ok := placed[e.Door]; ok {
			*p = core.Some(st, e.At)
		}
	}
	if some && allClosed {
		for _, p := range []*core.Value[core.Opening]{&d.FrontLeft, &d.FrontRight, &d.RearLeft, &d.RearRight} {
			if !p.OK {
				*p = core.Some(core.Closed, seen)
			}
		}
	}
	if st, known := states[hood]; hasHood && known {
		d.Hood = core.Some(st, hoodAt)
		seen = later(seen, hoodAt)
	}
	for _, e := range locks {
		// ALL_DOORS is the car's own summary of its locks.
		if e.Door != "ALL_DOORS" {
			continue
		}
		switch strings.ToUpper(e.Value) {
		case "LOCKED", "DOUBLE_LOCKED":
			d.Locked = core.Some(true, e.At)
		case "UNLOCKED":
			d.Locked = core.Some(false, e.At)
		}
		seen = later(seen, e.At.UTC())
	}
	if d != (core.Doors{}) {
		s.Doors = core.Some(d, seen)
	}

	wins := list[window](m, "windowStatus")
	if wins == nil {
		return
	}
	s.Covers |= core.FieldWindows
	var w core.Windows
	seen = time.Time{}
	places := map[string]*core.Value[core.Opening]{
		"FRONT_LEFT": &w.FrontLeft, "FRONT_RIGHT": &w.FrontRight, "REAR_LEFT": &w.RearLeft, "REAR_RIGHT": &w.RearRight,
	}
	allClosed, some = true, false
	for _, e := range wins {
		if e.Value == nil || e.Value.Range == nil {
			allClosed = false
			continue
		}
		some = true
		seen = later(seen, e.At.UTC())
		st := core.Closed
		if e.Value.Range.Lower != 0 || e.Value.Range.Upper != 0 {
			st, allClosed = core.Open, false
		}
		if p, ok := places[e.Window]; ok {
			*p = core.Some(st, e.At)
		}
	}
	if some && allClosed {
		for _, p := range places {
			if !p.OK {
				*p = core.Some(core.Closed, seen)
			}
		}
	}
	if w != (core.Windows{}) {
		s.Windows = core.Some(w, seen)
	}
}

// garage is the car as the garage tells it. The year is a string in the specification;
// a number is read too.
type garage struct {
	VIN        string          `json:"vin"`
	NickName   string          `json:"nickName"`
	Color      string          `json:"color"`
	ModelName  string          `json:"modelName"`
	ModelYear  json.RawMessage `json:"modelYear"`
	Make       string          `json:"make"`
	EngineType string          `json:"engineType"`
}

// GarageVehicle is what the garage tells of the car the grant reads.
type GarageVehicle struct {
	VIN string
}

// VehicleOf reads the car of a garage response: the connection's vehicle.
func VehicleOf(raw []byte) (GarageVehicle, error) {
	g, err := garageOf(raw)
	if err != nil {
		return GarageVehicle{}, err
	}
	vin := strings.ToUpper(strings.TrimSpace(g.VIN))
	if len(vin) != 17 || !vinPattern.MatchString(vin) {
		return GarageVehicle{}, fmt.Errorf("garage: no VIN in the response")
	}
	return GarageVehicle{VIN: vin}, nil
}

// VINOf returns the VIN of the car a response of ep is about, upper-cased; empty when it
// tells none. A grant reads one car, and neither endpoint is asked for one: this is how
// a reader knows whose values it holds.
func VINOf(ep Endpoint, raw []byte) string {
	vin := ""
	switch ep {
	case Telemetry:
		var r struct {
			VIN string `json:"vin"`
		}
		if json.Unmarshal(raw, &r) == nil {
			vin = r.VIN
		}
	case Garage:
		if g, err := garageOf(raw); err == nil {
			vin = g.VIN
		}
	}
	return strings.ToUpper(strings.TrimSpace(vin))
}

// garageOf reads a garage response: the car as an object, which the specification and
// the other clients show; a list of cars, or an object holding one under vehicles, is
// read too, its first car taken (a grant reads one).
func garageOf(raw []byte) (garage, error) {
	var g garage
	trimmed := bytes.TrimSpace(raw)
	var many []garage
	switch {
	case len(trimmed) > 0 && trimmed[0] == '[':
		if err := json.Unmarshal(trimmed, &many); err != nil {
			return g, fmt.Errorf("garage: %w", err)
		}
	default:
		var wrapped struct {
			garage
			Vehicles []garage `json:"vehicles"`
		}
		if err := json.Unmarshal(trimmed, &wrapped); err != nil {
			return g, fmt.Errorf("garage: %w", err)
		}
		if wrapped.VIN != "" || len(wrapped.Vehicles) == 0 {
			return wrapped.garage, nil
		}
		many = wrapped.Vehicles
	}
	if len(many) == 0 {
		return g, fmt.Errorf("garage: no vehicle in the response")
	}
	return many[0], nil
}

// engineTypes maps the garage's engineType. BEV is the one seen; PHEV is assumed for a
// plug-in hybrid. Another word is left untold, and the user picks the variant.
var engineTypes = map[string]core.Powertrain{"BEV": core.Electric, "PHEV": core.PlugInHybrid}

// normalizeGarage reads what the car is: the brand, the model as Ford names it, its
// year, its powertrain, and for its sheet the name its owner gave it and its colour.
func normalizeGarage(raw []byte) (core.Snapshot, error) {
	g, err := garageOf(raw)
	if err != nil {
		return core.Snapshot{}, fmt.Errorf("normalize: %w", err)
	}
	var s core.Snapshot
	s.Covers |= core.FieldModel | core.FieldSheet
	// Ford's garage holds Fords and Lincolns: the brand is told for a Ford only.
	if mk := strings.TrimSpace(g.Make); mk == "" || strings.EqualFold(mk, "Ford") {
		s.Brand = core.Some(Brand, time.Time{})
	}
	if name := strings.TrimSpace(g.ModelName); name != "" {
		s.Family = core.Some(name, time.Time{})
	}
	if year, err := strconv.Atoi(strings.Trim(strings.TrimSpace(string(g.ModelYear)), `"`)); err == nil && year > 1900 {
		s.ModelYear = core.Some(year, time.Time{})
	}
	if p, ok := engineTypes[strings.ToUpper(strings.TrimSpace(g.EngineType))]; ok {
		s.Powertrain = core.Some(p, time.Time{})
	}
	sheet := core.Sheet{Name: strings.TrimSpace(g.NickName), Colour: strings.TrimSpace(g.Color)}
	if sheet != (core.Sheet{}) {
		s.Sheet = core.Some(sheet, time.Time{})
	}
	return s, nil
}
