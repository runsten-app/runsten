package pleos

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"runsten/internal/core"
)

// Source names the Pleos Vehicle Data API among the sources of readings: the one source
// of the three brands, each their own connection and key.
const Source = "pleos"

// Normalizer reads the stored responses of the API into snapshots, for the derivation.
// Each status endpoint is a kind of the source, named as the API names it.
//
// Every value is dated from the car's own send, in the response's data.timestamp: the
// API returns the last state the car sent, without waking it, and a read may be hours
// old (core.Params.VehicleTimestamps). A value null, the word "invalid" or the
// string "-" is absent, never zero. What the API does not cover is left out of Covers,
// so that it keeps its previous value.
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads.
func (Normalizer) Kinds() []core.Kind {
	return kinds(Driving, Batteries, Powertrains, Locations, Status, Consent)
}

// EnergyKinds returns the kinds holding the energy state: the state of charge and the
// charge (the batteries), and the range (the powertrains).
func (Normalizer) EnergyKinds() []core.Kind { return kinds(Batteries, Powertrains) }

// StateKinds returns none: the status endpoint tells the doors, the tyres and the
// climate as the Kinds hold it, as Tesla's vehicle_data does; each of its readings is
// one the derivation takes too, and none of its values reveals an event.
func (Normalizer) StateKinds() []core.Kind { return nil }

// HealthKinds returns the kinds of response NormalizeHealth reads.
func (Normalizer) HealthKinds() []core.Kind { return kinds(Status) }

// SleepKinds returns the endpoint telling whether the car sleeps: the driving's
// sleepMode feeds the statistic of the time asleep. The collector reads it at the same
// pace parked, taking nothing from it: no read of the API wakes the car.
func (Normalizer) SleepKinds() []core.Kind { return kinds(Driving) }

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

// Normalize converts a raw response of endpoint ep into a vendor-neutral snapshot.
func Normalize(ep Endpoint, raw []byte) (core.Snapshot, error) {
	var s core.Snapshot
	var inner any
	if err := json.Unmarshal(raw, &struct {
		Data *any `json:"data"`
	}{&inner}); err != nil {
		return core.Snapshot{}, fmt.Errorf("normalize: %w", err)
	}
	data, _ := inner.(map[string]any)
	if data == nil {
		return core.Snapshot{}, fmt.Errorf("normalize: no data in the response")
	}
	t := timeOf(data)
	switch ep {
	case Driving:
		normalizeDriving(data, t, &s)
	case Batteries:
		normalizeBatteries(data, t, &s)
	case Powertrains:
		normalizePowertrains(data, t, &s)
	case Locations:
		normalizeLocations(data, t, &s)
	case Status:
		normalizeStatus(data, t, &s)
	case Consent:
		normalizeConsent(data, &s)
	}
	return s, nil
}

// timeOf reads the instant the car sent what the response holds: no value of a record's
// tells more than its own send (zero when the API writes none).
func timeOf(data map[string]any) time.Time {
	at, err := time.Parse(time.RFC3339, text(data["timestamp"]))
	if err != nil {
		return time.Time{}
	}
	return at.UTC()
}

// text reads a value as a string, whatever the JSON wrote it as; empty when unreadable.
func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	}
	return ""
}

// number reads a value as a number, whatever the JSON wrote it as: null, the word
// "invalid" and "-" tell nothing (false).
func number(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case bool:
		return b2f(t), true // the tell of a charge, written as a bool
	case string:
		if t == "invalid" || t == "-" || t == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// unit reads the distance a value is in, as kilometers (false for an unknown unit or
// value).
//
// Assumption, to check on a real car (ADR 0049 §10.6): some British cars are said to
// report kilometres under the label mile; the label is trusted until then, a false
// odometer falling on the distances, never the energies.
func km(v any, unit any) (float64, bool) {
	n, ok := number(v)
	if !ok {
		return 0, false
	}
	switch text(unit) {
	case "km":
		return n, true
	case "mile", "miles":
		return n * 1.609344, true
	case "meter":
		return n / 1000, true
	case "feet":
		return n * 0.0003048, true
	}
	return 0, false
}

// charging maps charge.status: every word that charges is a charge, the one a
// scheduled departure waits for is scheduled, the discharges tell the plug drawn on.
// wirelessCharging has no type a charge of the domain has: absent.
var charging = map[string]core.ChargingStatus{
	"charging":         core.ChargingActive, // "slow charging", an AC charge
	"fastCharging":     core.ChargingActive, // a DC one
	"wirelessCharging": core.ChargingActive,
	"reservedCharging": core.ChargingScheduled,
	"notCharging":      core.ChargingIdle,
	"v2lStop":          core.ChargingIdle,
	"v2lOperating":     core.ChargingDischarging,
	"v2xOperating":     core.ChargingDischarging,
}

// ac is the current a slow charge draws; fastCharging a fast one draws.
const (
	statusCharging = "charging"
	statusFast     = "fastCharging"
)

// NormalizeBatteries reads the charge: the state of charge, the plug, the target, the
// time left and the battery's stated capacity. The domain has no power, current nor
// voltage: a charge keeps the energy of its state of charge alone.
func normalizeBatteries(data map[string]any, at time.Time, s *core.Snapshot) {
	c, _ := data["charge"].(map[string]any)
	if c == nil {
		return
	}
	s.Covers = core.FieldSoC | core.FieldCharging | core.FieldConnection | core.FieldChargeType |
		core.FieldTargetSoC | core.FieldChargeDetails | core.FieldCapacity
	if soc, ok := number(c["stateOfCharge"]); ok {
		s.SoC = core.Some(soc, at)
	}
	switch text(c["plugin"]) {
	case "connected":
		s.Connection = core.Some(core.Connected, at)
	case "disconnected":
		s.Connection = core.Some(core.Disconnected, at)
	}
	status := text(c["status"])
	if v, ok := charging[status]; ok {
		s.Charging = core.Some(v, at)
	} else if b, ok := number(c["charging"]); ok {
		// charge.charging in support when status tells nothing.
		if b != 0 {
			s.Charging = core.Some(core.ChargingActive, at)
		} else {
			s.Charging = core.Some(core.ChargingIdle, at)
		}
	}
	if s.Charging.OK && s.Charging.V == core.ChargingActive {
		// The charge's type, a charge active alone. Wireless has none read.
		switch status {
		case statusCharging:
			s.ChargeType = core.Some(core.AC, at)
		case statusFast:
			s.ChargeType = core.Some(core.DC, at)
		}
	}
	if t, _ := c["targetStateOfCharge"].(map[string]any); t != nil {
		if target, ok := number(t["standard"]); ok {
			s.TargetSoC = core.Some(target, at)
		}
		// The DC target (quick) stays in the raw response: core has one target.
	}
	// Assumption (ADR 0049 §5), a single observation behind it: batteryCapacity is in
	// kilojoules (256,320 for 71.2 kWh). Read as the capacity the car states, never in
	// place of the net one of the catalog (catalog_net).
	if cap, ok := number(c["batteryCapacity"]); ok && cap > 0 {
		s.CapacityKWh = core.Some(cap/3600, at)
	}
	// Assumption (ADR 0049 §5): the time left runs to the charge's target.
	// Zero is what a car that does not charge tells: no time left, as nothing told.
	if minutes, ok := number(c["remainTime"]); ok && minutes > 0 {
		s.TimeLeft = core.Some(core.TimeLeft{
			Duration: time.Duration(minutes * float64(time.Minute)), To: core.ToTarget,
		}, at)
	}
}

// NormalizePowertrains reads the range the car forecasts, on the battery alone of an
// electric (the type EV of the distances). What it says of another powertrain's, and
// the remainRatio beside, stay in the raw response: the state of charge is read from
// the batteries, and no hybrid is catalogued yet (ADR 0041).
func normalizePowertrains(data map[string]any, at time.Time, s *core.Snapshot) {
	dtes, _ := data["distanceToEmpties"].([]any)
	s.Covers = core.FieldRange
	for _, d := range dtes {
		m, _ := d.(map[string]any)
		if m == nil || text(m["type"]) != "EV" {
			continue
		}
		if km, ok := km(m["value"], m["unit"]); ok {
			s.RangeKm = core.Some(km, at)
		}
		break
	}
}

// NormalizeDriving reads the odometer, the engine, and the sleep mode.
func normalizeDriving(data map[string]any, at time.Time, s *core.Snapshot) {
	s.Covers = core.FieldEngine | core.FieldOdometer | core.FieldSleep
	if ignition, _ := data["ignition"].(map[string]any); ignition != nil {
		// Assumption (ADR 0049 §10.2): whether ignition.status reads on while the
		// car charges is unknown; read as told.
		switch text(ignition["status"]) {
		case "on":
			s.Engine = core.Some(core.EngineRunning, at)
		case "off":
			s.Engine = core.Some(core.EngineStopped, at)
		}
	}
	switch text(data["sleepMode"]) {
	case "on":
		s.Sleep = core.Some(core.Asleep, at)
	case "off":
		s.Sleep = core.Some(core.Awake, at)
	}
	if o, _ := data["odometer"].(map[string]any); o != nil {
		if km, ok := km(o["value"], o["unit"]); ok {
			s.OdometerKm = core.Some(km, at)
		}
	}
}

// NormalizeLocations reads the position, in WGS 84 alone: the Bessel and CoordShift
// coordinate systems of Korea are not converted here (assumption: no car in list uses
// them for its position; a European one tells 0).
func normalizeLocations(data map[string]any, at time.Time, s *core.Snapshot) {
	location, _ := data["location"].(map[string]any)
	if location == nil {
		return
	}
	lat, latOK := number(location["latitude"])
	lon, lonOK := number(location["longitude"])
	t, tok := number(location["type"])
	if !tok || t != 0 || !latOK || !lonOK {
		// Not covered: a position this does not read keeps the previous one.
		return
	}
	s.Covers = core.FieldPosition
	s.Position = core.Some(core.Position{Lat: lat, Lon: lon}, at)
}

// doorsNormal maps a door's word: locked is a closed door's own word (the two sides
// are told, not the driver's and the passenger's).
var doorsNormal = map[string]core.Opening{"open": core.Open, "closed": core.Closed, "locked": core.Closed}

// normalizeStatus reads the doors, the climate and the outside temperature; the tyres
// and the warnings go to the health (health.go). The state holds them all in one
// response, as Tesla's vehicle_data does: each of its readings is one the derivation
// takes too.
func normalizeStatus(data map[string]any, at time.Time, s *core.Snapshot) {
	s.Covers = core.FieldDoors | core.FieldWindows | core.FieldClimate
	doors(s, data, at)
	if climate, _ := data["climateControl"].(map[string]any); climate != nil {
		c := core.Climate{}
		switch text(climate["status"]) {
		case "on":
			c.On = core.Some(true, at)
		case "off":
			c.On = core.Some(false, at)
		}
		if temp, _ := climate["temperature"].(map[string]any); temp != nil {
			// The doc gives the setpoint in Celsius alone; another unit is not read
			// (assumption: none has been seen).
			if v, ok := number(temp["value"]); ok && text(temp["unit"]) == "C" {
				c.SetpointC = core.Some(v, at)
			}
		}
		s.Climate = core.Some(c, at)
	}
	// outsideTemperature: no electric car reports it, says the API's compatibility
	// matrix; read when one does.
	if temp, _ := data["outsideTemperature"].(map[string]any); temp != nil {
		if v, ok := number(temp["value"]); ok && text(temp["unit"]) == "C" {
			s.OutsideTempC = core.Some(v, at)
		}
	}
}

// doors reads the doors, the hood, the trunk and the sunroof, and the central lock:
// locked is a door's word of its own, and the lock is true when every door the API
// names says it (the four of its two rows), false when one says closed or open (a car
// that opens behind the front seats alone may name two).
func doors(s *core.Snapshot, data map[string]any, at time.Time) {
	rows, _ := data["doors"].([]any)
	var d core.Doors
	positions := [4]*core.Value[core.Opening]{&d.FrontLeft, &d.FrontRight, &d.RearLeft, &d.RearRight}
	words := [4]string{} // front left, front right, rear left, rear right
	named := 0
	for i, row := range rows {
		m, _ := row.(map[string]any)
		if i > 1 {
			break
		}
		for j, side := range []string{"left", "right"} {
			word := text(m[side])
			if _, ok := doorsNormal[word]; !ok {
				continue
			}
			words[i*2+j] = word
			named++
			*positions[i*2+j] = core.Some(doorsNormal[word], at)
		}
	}
	if named > 0 {
		locked := true
		for _, word := range words {
			if word != "" && word != "locked" {
				locked = false
			}
		}
		d.Locked = core.Some(locked, at)
	}
	if v, ok := doorsNormal[text(data["hood"])]; ok {
		d.Hood = core.Some(v, at)
	}
	if v, ok := doorsNormal[text(data["trunk"])]; ok {
		d.Tailgate = core.Some(v, at)
	}
	if d != (core.Doors{}) {
		s.Doors = core.Some(d, at)
	}
	if v, ok := doorsNormal[text(data["sunroof"])]; ok {
		var w core.Windows
		w.Sunroof = core.Some(v, at)
		s.Windows = core.Some(w, at)
	}
}

// powertrains maps a vehicle's type (consent's) to what the catalog matches.
var powertrains = map[string]core.Powertrain{
	"EV": core.Electric,
	"PE": core.PlugInHybrid,
}

// normalizeConsent reads what the vehicles' list tells of the model, which no status
// endpoint does: the powertrain and the sales name, the year at the VIN's tenth
// position, and the nickname the owner gave the car.
//
// The brand is the connection's provider, which the source of the readings does not
// know: the recognition reads it there (the provider tells it, as the connection's
// provider does for other sources' response). The name, the model's code, stays in the
// raw response: sellname names the family (the catalog's families are the API's sales
// names).
func normalizeConsent(data map[string]any, s *core.Snapshot) {
	vehicles, _ := data["vehicles"].([]any)
	for _, v := range vehicles {
		m, _ := v.(map[string]any)
		if m == nil || !vinOf(text(m["vin"])) {
			continue
		}
		s.Covers = core.FieldModel | core.FieldSheet
		if pt, ok := powertrains[text(m["type"])]; ok {
			s.Powertrain = core.Some(pt, time.Time{})
		} else {
			// GN (combustion), HV (hybrid), FV (fuel cell) and what is unknown: the
			// catalog offers no variant of it.
			s.Powertrain = core.Some(core.OtherPowertrain, time.Time{})
		}
		if sell := text(m["sellname"]); sell != "" {
			s.Family = core.Some(sell, time.Time{})
		}
		if year, ok := modelYear(text(m["vin"])); ok {
			s.ModelYear = core.Some(year, time.Time{})
		}
		if name := text(m["nickname"]); name != "" {
			s.Sheet = core.Some(core.Sheet{Name: name}, time.Time{})
		}
		return
	}
	if len(vehicles) > 0 {
		// A list of the key's vehicles, none with a VIN: nothing of the model.
		s.Covers = core.FieldModel | core.FieldSheet
	}
}

// vinOf reports whether s is a VIN: 17 characters, without I, O nor Q (ISO 3779).
func vinOf(s string) bool {
	if len(s) != 17 {
		return false
	}
	return !slices.ContainsFunc([]byte(s), func(c byte) bool { return c == 'I' || c == 'O' || c == 'Q' })
}

// modelYear reads the model year at the VIN's tenth position (ISO 3779, on a 30-year
// cycle), from 2010 to 2039.
func modelYear(s string) (int, bool) {
	if len(s) != 17 {
		return 0, false
	}
	const codes = "ABCDEFGHJKLMNPRSTVWXY123456789"
	i := strings.IndexByte(codes, s[9])
	if i < 0 {
		return 0, false
	}
	return 2010 + i, true
}
