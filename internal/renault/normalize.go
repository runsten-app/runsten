package renault

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"runsten/internal/core"
)

// Source names the My Renault app's API among the sources of readings: the one source of
// Renault, Dacia and Alpine.
const Source = "renault"

// Normalizer reads the stored responses of the API into snapshots, for the derivation.
// Each endpoint is a kind of the source, named as Kamereon names it.
//
// Every value is dated from the car's own send (timestamp, lastUpdateTime): Kamereon
// returns the last state the car sent, without waking it, and a read may be hours old.
// What a response does not tell is left out of Covers, so that it keeps its previous
// value.
//
// A response is read without its vehicle's model: the quirks of a model (the Zoe's
// power in watts, its battery temperature) are read from the model's code the details
// tell, which the collector keeps in each response it stores (WithModel).
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads.
func (Normalizer) Kinds() []core.Kind {
	return kinds(BatteryStatus, Cockpit, Location, HVACStatus, Details)
}

// EnergyKinds returns the kinds holding the energy state: the battery's.
func (Normalizer) EnergyKinds() []core.Kind { return kinds(BatteryStatus) }

// StateKinds returns none: the climate's response tells nothing that reveals an event.
func (Normalizer) StateKinds() []core.Kind { return nil }

// HealthKinds returns none: the endpoints a logger reads tell no tyre, service nor
// warning (lock-status and pressure are refused on nearly every electric car).
func (Normalizer) HealthKinds() []core.Kind { return nil }

// SleepKinds returns none: Kamereon does not tell whether the car sleeps.
func (Normalizer) SleepKinds() []core.Kind { return nil }

// NormalizeHealth reads nothing: HealthKinds is empty.
func (Normalizer) NormalizeHealth(k core.Kind, _ []byte) (core.Health, error) {
	return core.Health{}, fmt.Errorf("normalize health: %s tells no health (%s)", Source, k.Endpoint)
}

// Normalize reads a stored response of kind k.
func (Normalizer) Normalize(k core.Kind, raw []byte) (core.Snapshot, error) {
	if k.Source != Source {
		return core.Snapshot{}, fmt.Errorf("normalize: source %q is not %s", k.Source, Source)
	}
	return Normalize(Endpoint(k.Endpoint), raw)
}

func kinds(eps ...Endpoint) []core.Kind {
	out := make([]core.Kind, len(eps))
	for i, ep := range eps {
		out[i] = core.Kind{Source: Source, Endpoint: string(ep)}
	}
	return out
}

// modelKey is the field the collector adds to a stored response: the model's code of
// its vehicle, which the response itself never tells.
const modelKey = "runstenModel"

// WithModel returns a car-adapter response with the vehicle's model code added beside
// its data, so that its normalization knows the model's quirks. The details are left as
// they are: they tell the model themselves.
func WithModel(raw []byte, code string) ([]byte, error) {
	if code == "" {
		return raw, nil
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	c, err := json.Marshal(code)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	v[modelKey] = c
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	return out, nil
}

// Normalize converts a raw response of endpoint ep into a vendor-neutral snapshot.
func Normalize(ep Endpoint, raw []byte) (core.Snapshot, error) {
	var s core.Snapshot
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return core.Snapshot{}, fmt.Errorf("normalize: %w", err)
	}
	if ep == Details {
		normalizeDetails(v, &s)
		return s, nil
	}
	data, _ := v["data"].(map[string]any)
	if data == nil {
		return core.Snapshot{}, fmt.Errorf("normalize %s: no data in the response", ep)
	}
	attrs, _ := data["attributes"].(map[string]any)
	if attrs == nil {
		// An answer without its attributes tells nothing: not covered.
		return s, nil
	}
	model, _ := ModelOf(text(v[modelKey]))
	switch ep {
	case BatteryStatus:
		normalizeBattery(attrs, model, &s)
	case Cockpit:
		normalizeCockpit(attrs, &s)
	case Location:
		normalizeLocation(attrs, &s)
	case HVACStatus:
		normalizeHVAC(attrs, &s)
	default:
		return core.Snapshot{}, fmt.Errorf("normalize %s: %w", ep, ErrUnknownEndpoint)
	}
	return s, nil
}

// timeOf reads when the car sent a value: one of the keys, RFC 3339 with Z or an offset;
// zero when none is told.
func timeOf(attrs map[string]any, keys ...string) time.Time {
	for _, k := range keys {
		if at, err := time.Parse(time.RFC3339, text(attrs[k])); err == nil {
			return at.UTC()
		}
	}
	return time.Time{}
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
	}
	return ""
}

// number reads a value as a number, whatever the JSON wrote it as.
func number(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

// Kamereon's codes of chargingStatus (renault-api's enums.py), as the domain names
// them. Those not listed (-1.1 unavailable, an error on a Zoe 50, the vehicle-to-load
// and vehicle-to-grid ones) tell no charge's state: absent.
var chargingStatus = map[float64]core.ChargingStatus{
	0.0:  core.ChargingIdle,      // not in charge
	0.1:  core.ChargingScheduled, // waiting for a planned charge
	0.2:  core.ChargingDone,      // the charge ended
	0.3:  core.ChargingIdle,      // waiting for the current (a charger that does not deliver yet)
	0.4:  core.ChargingIdle,      // the energy flap opened
	1.0:  core.ChargingActive,    // charging
	-1.0: core.ChargingIdle,      // not charging, on a Zoe 40 and older
	-1.4: core.ChargingIdle,      // vehicle-to-load connected
	-1.5: core.ChargingDischarging,
}

// normalizeBattery reads the charge. chargingInstantaneousPower is read on a Zoe phase 1
// alone, in watts: elsewhere it is the power the car would accept, never the charge's.
// batteryCapacity is always 0, and batteryAvailableEnergy the energy left, not the
// capacity: neither is read.
func normalizeBattery(a map[string]any, m Model, s *core.Snapshot) {
	at := timeOf(a, "timestamp")
	s.Covers = core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection | core.FieldChargeDetails
	if soc, ok := number(a["batteryLevel"]); ok && soc >= 0 && soc <= 100 {
		s.SoC = core.Some(soc, at)
	}
	if r, ok := number(a["batteryAutonomy"]); ok && r >= 0 {
		s.RangeKm = core.Some(r, at)
	}
	// plugStatus 3 says plugged on a Twingo, unplugged on a Spring (renault-api #2377):
	// absent, as the error and the unknown are.
	if p, ok := number(a["plugStatus"]); ok {
		switch p {
		case 0:
			s.Connection = core.Some(core.Disconnected, at)
		case 1:
			s.Connection = core.Some(core.Connected, at)
		}
	}
	if c, ok := number(a["chargingStatus"]); ok {
		if v, known := chargingStatus[roundTenth(c)]; known {
			s.Charging = core.Some(v, at)
		}
	}
	if m.PowerInWatts && s.Charging.OK && s.Charging.V == core.ChargingActive {
		if w, ok := number(a["chargingInstantaneousPower"]); ok && w >= 0 {
			s.Covers |= core.FieldPower
			s.PowerW = core.Some(w, at)
		}
	}
	// The time left is told while charging alone: a car that ended keeps its last one
	// (a Renault 5 seen with 3 minutes left, ten hours after).
	if s.Charging.OK && s.Charging.V == core.ChargingActive {
		if minutes, ok := number(a["chargingRemainingTime"]); ok && minutes > 0 {
			s.TimeLeft = core.Some(core.TimeLeft{Duration: time.Duration(minutes * float64(time.Minute)), To: core.ToTarget}, at)
		}
	}
}

// roundTenth rounds a status to its tenth, as Kamereon writes it (1.0, 0.1): a float
// written 0.30000001 is still 0.3.
func roundTenth(f float64) float64 {
	n, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', 1, 64), 64)
	return n
}

// normalizeCockpit reads the odometer, in km, a number or a real.
func normalizeCockpit(a map[string]any, s *core.Snapshot) {
	s.Covers = core.FieldOdometer
	if km, ok := number(a["totalMileage"]); ok && km > 0 {
		s.OdometerKm = core.Some(km, timeOf(a, "timestamp"))
	}
}

// normalizeLocation reads the position. 91/181 is Kamereon's answer of a car without a
// fix (renault-api #2250): not covered, as an answer without coordinates.
func normalizeLocation(a map[string]any, s *core.Snapshot) {
	lat, latOK := number(a["gpsLatitude"])
	lon, lonOK := number(a["gpsLongitude"])
	if !latOK || !lonOK || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return
	}
	s.Covers = core.FieldPosition
	s.Position = core.Some(core.Position{Lat: lat, Lon: lon}, timeOf(a, "lastUpdateTime"))
}

// normalizeHVAC reads the climate and the temperatures. A Zoe 40 says off while it
// preconditions (renault-api's docs): read as told.
func normalizeHVAC(a map[string]any, s *core.Snapshot) {
	at := timeOf(a, "lastUpdateTime")
	s.Covers = core.FieldClimate
	var c core.Climate
	switch text(a["hvacStatus"]) {
	case "on":
		c.On = core.Some(true, at)
	case "off":
		c.On = core.Some(false, at)
	}
	if c != (core.Climate{}) {
		s.Climate = core.Some(c, at)
	}
	if t, ok := number(a["externalTemperature"]); ok {
		s.OutsideTempC = core.Some(t, at)
	}
	if t, ok := number(a["internalTemperature"]); ok {
		s.CabinTempC = core.Some(t, at)
	}
}

// brands are the brands of Kamereon's details, as the catalog names them.
var brands = map[string]string{"RENAULT": "renault", "DACIA": "dacia", "ALPINE": "alpine"}

// normalizeDetails reads the model: the brand, the family the model's code names (the
// label when the code is not known), and the powertrain the energy tells. Kamereon
// tells no year and no capacity: the catalog matches on the family.
func normalizeDetails(v map[string]any, s *core.Snapshot) {
	s.Covers = core.FieldModel
	brand, _ := v["brand"].(map[string]any)
	if b, ok := brands[strings.ToUpper(text(brand["label"]))]; ok {
		s.Brand = core.Some(b, time.Time{})
	}
	model, _ := v["model"].(map[string]any)
	if m, ok := ModelOf(text(model["code"])); ok {
		s.Family = core.Some(m.Family, time.Time{})
	} else if label := text(model["label"]); label != "" {
		s.Family = core.Some(label, time.Time{})
	}
	energy, _ := v["energy"].(map[string]any)
	switch text(energy["code"]) {
	case "ELEC", "ELECX":
		s.Powertrain = core.Some(core.Electric, time.Time{})
	case "PHEV":
		s.Powertrain = core.Some(core.PlugInHybrid, time.Time{})
	case "":
	default:
		s.Powertrain = core.Some(core.OtherPowertrain, time.Time{})
	}
}

// vinOf reports whether s is a VIN: 17 characters, without I, O nor Q (ISO 3779).
func vinOf(s string) bool {
	if len(s) != 17 {
		return false
	}
	return !slices.ContainsFunc([]byte(s), func(c byte) bool { return c == 'I' || c == 'O' || c == 'Q' })
}
