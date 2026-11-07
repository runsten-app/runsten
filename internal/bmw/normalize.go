package bmw

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"runsten/internal/core"
)

// Normalizer reads the stored responses of CarData into snapshots, for the derivation.
// Each Endpoint is an endpoint of the source.
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads: the stream's values and the
// container's, which are the same descriptors, and the basic data.
func (Normalizer) Kinds() []core.Kind { return kinds(Stream, TelematicData, BasicData) }

// EnergyKinds returns the kinds holding the energy state.
func (Normalizer) EnergyKinds() []core.Kind { return kinds(Stream, TelematicData) }

// StateKinds returns nothing: the values Kinds holds are all there is.
func (Normalizer) StateKinds() []core.Kind { return nil }

// HealthKinds returns the kinds of response NormalizeHealth reads.
func (Normalizer) HealthKinds() []core.Kind { return kinds(Stream, TelematicData) }

// SleepKinds returns nothing: CarData does not tell whether the car sleeps, and reading
// it never wakes the car.
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

func kinds(es ...Endpoint) []core.Kind {
	out := make([]core.Kind, len(es))
	for i, e := range es {
		out[i] = core.Kind{Source: Source, Endpoint: string(e)}
	}
	return out
}

// values are a response's descriptors, by name.
type values map[string]datum

func parseValues(e Endpoint, raw []byte) (values, error) {
	var v struct {
		TelematicData values `json:"telematicData"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("normalize %s: %w", e, err)
	}
	return v.TelematicData, nil
}

// stampOf reads a value's timestamp, ISO 8601 in UTC; zero if it does not read.
func stampOf(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// text reads a value as a string: CarData writes some as strings, others as JSON values,
// depending on the descriptor and the path (the API's are all strings).
func (d datum) text() (string, bool) {
	var s string
	if json.Unmarshal(d.Value, &s) == nil {
		return strings.TrimSpace(s), true
	}
	v := bytes.TrimSpace(d.Value)
	if len(v) == 0 || bytes.Equal(v, []byte("null")) {
		return "", false
	}
	return string(v), true
}

// number reads a value as a number, written as one or as a string.
func (d datum) number() (float64, bool) {
	s, ok := d.text()
	if !ok || s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// flag reads CarData's boolean: ASN_isTrue or ASN_isFalse (a JSON boolean read too);
// ASN_isUnknown is unknown.
func (d datum) flag() (bool, bool) {
	s, ok := d.text()
	if !ok {
		return false, false
	}
	switch strings.ToLower(s) {
	case "asn_istrue", "true":
		return true, true
	case "asn_isfalse", "false":
		return false, true
	}
	return false, false
}

// get returns the descriptor's value and its time.
func (v values) get(k string) (datum, time.Time, bool) {
	d, ok := v[k]
	if !ok {
		return datum{}, time.Time{}, false
	}
	return d, stampOf(d.Timestamp), true
}

// number returns the descriptor's numeric value, absent when it does not read.
func (v values) number(k string) (core.Value[float64], bool) {
	d, at, ok := v.get(k)
	if !ok {
		return core.Value[float64]{}, false
	}
	if f, ok := d.number(); ok {
		return core.Some(f, at), true
	}
	return core.Value[float64]{}, true
}

// Normalize converts a raw response of endpoint e into a vendor-neutral snapshot, each
// value dated by the time the car recorded it. A descriptor missing from the response
// covers nothing: the stream's values are those the car pushed so far.
func Normalize(e Endpoint, raw []byte) (core.Snapshot, error) {
	var s core.Snapshot
	switch e {
	case Stream, TelematicData:
		v, err := parseValues(e, raw)
		if err != nil {
			return s, err
		}
		normalizeValues(v, &s)
	case BasicData:
		return normalizeBasicData(raw)
	case ChargingHistory:
		// No reading: its sessions are read by NormalizeSessions.
	}
	return s, nil
}

func normalizeValues(v values, s *core.Snapshot) {
	soc, ok := v.number(DescSoC)
	if !ok || !soc.OK {
		if displayed, ok2 := v.number(DescSoCDisplayed); ok2 {
			soc, ok = displayed, true
		}
	}
	if ok {
		s.Covers |= core.FieldSoC
		s.SoC = soc
	}
	if r, ok := v.number(DescRange); ok {
		s.Covers |= core.FieldRange
		s.RangeKm = r
	}
	if t, ok := v.number(DescTargetSoC); ok {
		s.Covers |= core.FieldTargetSoC
		s.TargetSoC = t
	}
	if o, ok := v.number(DescOdometer); ok {
		s.Covers |= core.FieldOdometer
		s.OdometerKm = o
	}
	if p, ok := v.number(DescChargingPower); ok {
		s.Covers |= core.FieldPower
		s.PowerW = p
	}
	position(v, s)
	engine(v, s)
	charging(v, s)
	chargeDetails(v, s)
	doors(v, s)
	windows(v, s)
	climate(v, s)
}

// openings maps the doors', the lids', the windows' and the sunroof's words (BMW's
// catalogue), upper-cased; INVALID, UNKNOWN or another word is absent. The doors are
// typed boolean in the catalogue, with the words as values: a boolean is read too
// (assumed: true is open). INTERMEDIATE is a window or a lid half open: ajar.
var openings = map[string]core.Opening{
	"CLOSED": core.Closed, "FALSE": core.Closed, "ASN_ISFALSE": core.Closed,
	"OPEN": core.Open, "TRUE": core.Open, "ASN_ISTRUE": core.Open,
	"INTERMEDIATE": core.Ajar,
}

// locks maps door.lock.status: SECURED is locked with the alarm armed; SELECTIVE-LOCKED
// (SELECTIVELOCKED in the catalogue's text) leaves the driver's door unlocked, after a
// remote unlock: unlocked. INVALID and UNKNOWN are absent.
var locks = map[string]bool{
	"LOCKED": true, "SECURED": true,
	"UNLOCKED": false, "SELECTIVE-LOCKED": false, "SELECTIVELOCKED": false,
}

// opening reads the descriptor's opening; at is moved to its time when later and the
// value known, so that a word not read never dates what was.
func opening(v values, k string, at *time.Time) (core.Value[core.Opening], bool) {
	d, t, ok := v.get(k)
	if !ok {
		return core.Value[core.Opening]{}, false
	}
	w, _ := d.text()
	if o, ok := openings[strings.ToUpper(w)]; ok {
		if t.After(*at) {
			*at = t
		}
		return core.Some(o, t), true
	}
	return core.Value[core.Opening]{}, true
}

// doors reads the doors, the hood, the trunk and the central lock, covered when the
// response holds one of them; dated by the latest. The tank lid is not read: an electric
// car has none, and the charging port's flap is no opening of the body.
func doors(v values, s *core.Snapshot) {
	var d core.Doors
	var at time.Time
	seen := false
	for _, o := range []struct {
		k string
		v *core.Value[core.Opening]
	}{
		{DescDoorFrontLeft, &d.FrontLeft},
		{DescDoorFrontRight, &d.FrontRight},
		{DescDoorRearLeft, &d.RearLeft},
		{DescDoorRearRight, &d.RearRight},
		{DescHood, &d.Hood},
		{DescTrunk, &d.Tailgate},
	} {
		if val, ok := opening(v, o.k, &at); ok {
			*o.v, seen = val, true
		}
	}
	if l, t, ok := v.get(DescLock); ok {
		seen = true
		w, _ := l.text()
		if locked, ok := locks[strings.ToUpper(w)]; ok {
			if t.After(at) {
				at = t
			}
			d.Locked = core.Some(locked, t)
		}
	}
	if !seen {
		return
	}
	s.Covers |= core.FieldDoors
	if d != (core.Doors{}) {
		s.Doors = core.Some(d, at)
	}
}

// windows reads the windows and the sunroof, covered when the response holds one of
// them; dated by the latest.
func windows(v values, s *core.Snapshot) {
	var w core.Windows
	var at time.Time
	seen := false
	for _, o := range []struct {
		k string
		v *core.Value[core.Opening]
	}{
		{DescWindowFrontLeft, &w.FrontLeft},
		{DescWindowFrontRight, &w.FrontRight},
		{DescWindowRearLeft, &w.RearLeft},
		{DescWindowRearRight, &w.RearRight},
		{DescSunroof, &w.Sunroof},
	} {
		if val, ok := opening(v, o.k, &at); ok {
			*o.v, seen = val, true
		}
	}
	if !seen {
		return
	}
	s.Covers |= core.FieldWindows
	if w != (core.Windows{}) {
		s.Windows = core.Some(w, at)
	}
}

// preconditionings maps preConditioning.activity, lower-cased: heating, cooling or
// ventilating is preconditioning; standby (booked, not running) and inactive are not.
var preconditionings = map[string]bool{
	"heating": true, "cooling": true, "ventilation": true,
	"standby": false, "inactive": false,
}

// climate reads the preconditioning. CarData tells neither the outside nor the cabin's
// temperature, nor whether the climate runs otherwise (its catalogue has no such key):
// they stay absent.
func climate(v values, s *core.Snapshot) {
	d, at, ok := v.get(DescPreconditioning)
	if !ok {
		return
	}
	s.Covers |= core.FieldClimate
	w, _ := d.text()
	if on, ok := preconditionings[strings.ToLower(w)]; ok {
		s.Climate = core.Some(core.Climate{Preconditioning: core.Some(on, at)}, at)
	}
}

// position reads the position: a latitude and a longitude with the same timestamp, which
// the car may push in two messages (ADR 0045 §4). Apart, the previous position holds.
func position(v values, s *core.Snapshot) {
	lat, latAt, ok1 := v.get(DescLatitude)
	lon, lonAt, ok2 := v.get(DescLongitude)
	if !ok1 || !ok2 || !latAt.Equal(lonAt) {
		return
	}
	s.Covers |= core.FieldPosition
	la, ok1 := lat.number()
	lo, ok2 := lon.number()
	if ok1 && ok2 && (la != 0 || lo != 0) {
		s.Position = core.Some(core.Position{Lat: la, Lon: lo}, latAt)
	}
}

// engine reads the engine: running when the ignition is on or the car moves; stopped
// when the ignition is off and the car does not move. isMoving is missing on some cars:
// the ignition alone tells then.
func engine(v values, s *core.Snapshot) {
	var on, off bool
	var at time.Time
	if d, t, ok := v.get(DescIgnition); ok {
		if b, ok := d.flag(); ok {
			on, off, at = b, !b, t
		}
	}
	if d, t, ok := v.get(DescIsMoving); ok {
		if b, ok := d.flag(); ok && b {
			if !on || t.After(at) {
				at = t
			}
			on, off = true, false
		}
	}
	switch {
	case on:
		s.Covers |= core.FieldEngine
		s.Engine = core.Some(core.EngineRunning, at)
	case off:
		s.Covers |= core.FieldEngine
		s.Engine = core.Some(core.EngineStopped, at)
	}
}

// chargingStatuses maps charging.status (BMW's catalogue); an unknown word is absent.
var chargingStatuses = map[string]core.ChargingStatus{
	"CHARGINGACTIVE": core.ChargingActive,
	"INITIALIZATION": core.ChargingActive,
	"CHARGINGENDED":  core.ChargingDone,
	"NOCHARGING":     core.ChargingIdle,
	"CHARGINGPAUSED": core.ChargingIdle,
	"CHARGINGERROR":  core.ChargingError,
}

// hvStatuses maps charging.hvStatus, read when charging.status is missing. Assumed: the
// words of the simulator, from the free clients; an unknown one is absent.
var hvStatuses = map[string]core.ChargingStatus{
	"CHARGING":               core.ChargingActive,
	"FINISHED_FULLY_CHARGED": core.ChargingDone,
	"FINISHED_NOT_FULL":      core.ChargingDone,
	"WAITING_FOR_CHARGING":   core.ChargingIdle,
	"NOT_CHARGING":           core.ChargingIdle,
}

// charging reads the charge's status, the cable and the current type. The DC value of
// charging.method is not documented: a method naming DC, or the DC port connected, is
// DC; a method starting with AC is AC (AC_TYPE2PLUG).
func charging(v values, s *core.Snapshot) {
	if d, at, ok := v.get(DescPortStatus); ok {
		if w, _ := d.text(); w == "CONNECTED" || w == "DISCONNECTED" {
			s.Covers |= core.FieldConnection
			s.Connection = core.Some(core.Disconnected, at)
			if w == "CONNECTED" {
				s.Connection = core.Some(core.Connected, at)
			}
		}
	}
	status, at, ok := word(v, DescChargingStatus, chargingStatuses)
	if !ok {
		status, at, ok = word(v, DescHVStatus, hvStatuses)
	}
	if !ok {
		return
	}
	s.Covers |= core.FieldCharging | core.FieldChargeType
	s.Charging = core.Some(status, at)
	if status != core.ChargingActive {
		s.ChargeType = core.Some(core.NoCharge, at)
		return
	}
	method := ""
	if d, _, ok := v.get(DescChargingMethod); ok {
		method, _ = d.text()
	}
	dc := false
	if d, _, ok := v.get(DescPortDCStatus); ok {
		w, _ := d.text()
		dc = w == "CONNECTED"
	}
	switch {
	case dc || strings.Contains(method, "DC"):
		s.ChargeType = core.Some(core.DC, at)
	case strings.HasPrefix(method, "AC"):
		s.ChargeType = core.Some(core.AC, at)
	}
}

// word maps the descriptor's value through m.
func word(v values, k string, m map[string]core.ChargingStatus) (core.ChargingStatus, time.Time, bool) {
	d, at, ok := v.get(k)
	if !ok {
		return "", time.Time{}, false
	}
	w, _ := d.text()
	st, ok := m[w]
	return st, at, ok
}

// minChargerVoltage is the AC voltage below which the charger draws nothing.
const minChargerVoltage = 50

// chargeDetails reads the AC charge's current, voltage and phases, and the time left.
// Assumed: timeToFullyCharged runs to the charge's target, what the app shows as left.
func chargeDetails(v values, s *core.Snapshot) {
	amps, okA := v.number(DescACAmpere)
	volts, okV := v.number(DescACVoltage)
	phases, phasesAt, okP := v.get(DescPhaseNumber)
	left, okL := v.number(DescTimeToFull)
	if !okA && !okV && !okP && !okL {
		return
	}
	s.Covers |= core.FieldChargeDetails
	if okV && volts.OK && volts.V >= minChargerVoltage {
		s.VoltageV = volts
		if okA {
			s.CurrentA = amps
		}
		if okP {
			if n, ok := phaseCount(phases); ok {
				s.Phases = core.Some(n, phasesAt)
			}
		}
	}
	if okL && left.OK && left.V > 0 {
		s.TimeLeft = core.Some(core.TimeLeft{Duration: time.Duration(left.V * float64(time.Minute)), To: core.ToTarget}, left.At)
	}
}

// phaseCount reads phaseNumber: 3-PHASES (the catalogue's words), or a number.
func phaseCount(d datum) (int, bool) {
	s, ok := d.text()
	if !ok {
		return 0, false
	}
	s, _, _ = strings.Cut(s, "-")
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0
}

// basicData is what the normalization reads of a vehicle's basic data.
type basicData struct {
	VIN              string `json:"vin"`
	Brand            string `json:"brand"`
	ModelName        string `json:"modelName"`
	Series           string `json:"series"`
	PropulsionType   string `json:"propulsionType"`
	DriveTrain       string `json:"driveTrain"`
	ConstructionDate string `json:"constructionDate"`
	Steering         string `json:"steering"`
	ColourCode       string `json:"colourCode"`
	ColourCodeRaw    string `json:"colourCodeRaw"`
	// HVSMaxEnergy is the high-voltage battery's capacity, in kWh, a string in CarData's
	// OpenAPI; a number read too.
	HVSMaxEnergy json.RawMessage `json:"hvsMaxEnergyAbsolute"`
}

// brands maps basicData's brand, upper-cased, to the catalog's: BMWi is the OpenAPI's
// for the i models. Another brand (a BMW motorcycle, a Rolls-Royce) is absent.
var brands = map[string]string{"BMW": "bmw", "BMWI": "bmw", "BMW I": "bmw", "MINI": "mini"}

// steerings maps basicData's steering: LL and RL, BMW's codes (Linkslenker, Rechtslenker).
var steerings = map[string]core.Steering{"LL": core.LeftHand, "RL": core.RightHand}

// normalizeBasicData reads the model: the brand, the family and the trim from the model's
// name (i4 eDrive40: i4 and edrive40), the powertrain, and the model year; the battery's
// capacity; and the sheet: the colour, as BMW codes it, and the steering side. Assumed:
// the model year is the construction's, or the next one for a car built from July on,
// when BMW's model year changes; hvsMaxEnergyAbsolute is the gross capacity, as the
// catalog's gross_kwh it is matched with.
func normalizeBasicData(raw []byte) (core.Snapshot, error) {
	var s core.Snapshot
	var b basicData
	if err := json.Unmarshal(raw, &b); err != nil {
		return s, fmt.Errorf("normalize %s: %w", BasicData, err)
	}
	s.Covers |= core.FieldModel | core.FieldCapacity | core.FieldSheet
	if n, ok := kWh(b.HVSMaxEnergy); ok {
		s.CapacityKWh = core.Some(n, time.Time{})
	}
	sheet := core.Sheet{
		Colour:   strings.TrimSpace(cmp.Or(b.ColourCode, b.ColourCodeRaw)),
		Steering: steerings[strings.ToUpper(strings.TrimSpace(b.Steering))],
	}
	if sheet != (core.Sheet{}) {
		s.Sheet = core.Some(sheet, time.Time{})
	}
	if brand, ok := brands[strings.ToUpper(strings.TrimSpace(b.Brand))]; ok {
		s.Brand = core.Some(brand, time.Time{})
	}
	if f := strings.Fields(b.ModelName); len(f) > 0 {
		s.Family = core.Some(f[0], time.Time{})
		if len(f) > 1 {
			s.Trim = core.Some(strings.ToLower(strings.Join(f[1:], " ")), time.Time{})
		}
	}
	switch p := strings.ToUpper(b.PropulsionType + " " + b.DriveTrain); {
	case strings.Contains(p, "PHEV"):
		s.Powertrain = core.Some(core.PlugInHybrid, time.Time{})
	case strings.Contains(p, "BEV"):
		s.Powertrain = core.Some(core.Electric, time.Time{})
	case strings.TrimSpace(p) != "":
		s.Powertrain = core.Some(core.OtherPowertrain, time.Time{})
	}
	if built := stampOf(b.ConstructionDate); !built.IsZero() {
		y := built.Year()
		if built.Month() >= time.July {
			y++
		}
		s.ModelYear = core.Some(y, time.Time{})
	}
	return s, nil
}

// kWh reads a capacity above zero, a JSON string or number.
func kWh(raw json.RawMessage) (float64, bool) {
	var str string
	if json.Unmarshal(raw, &str) != nil {
		str = string(raw)
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(str), 64)
	return n, err == nil && n > 0 && !math.IsInf(n, 0)
}

// NormalizeHealth converts a raw response of endpoint e into a health reading: the
// tyres' pressures. CarData's row1 is the front axle.
func NormalizeHealth(e Endpoint, raw []byte) (core.Health, error) {
	if e != Stream && e != TelematicData {
		return core.Health{}, nil
	}
	v, err := parseValues(e, raw)
	if err != nil {
		return core.Health{}, err
	}
	tyres := map[core.Wheel]core.Tyre{}
	for w, k := range map[core.Wheel]string{
		core.FrontLeft: DescTyreFrontLeft, core.FrontRight: DescTyreFrontRight,
		core.RearLeft: DescTyreRearLeft, core.RearRight: DescTyreRearRight,
	} {
		if p, ok := v.number(k); ok && p.OK && p.V > 0 {
			tyres[w] = core.Tyre{PressureKPa: p}
		}
	}
	var h core.Health
	if len(tyres) > 0 {
		h.Covers = core.HealthTyres
		h.Tyres = tyres
	}
	return h, nil
}
