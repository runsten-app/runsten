package skoda

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"runsten/internal/core"
)

// Source names the MyŠkoda Public API among the sources of readings, and the provider of
// its connections.
const Source = "skoda"

// Brand is the maker the catalog knows the cars by.
const Brand = "skoda"

// Endpoint is what the collector reads of a vehicle: the API has one, the vehicle and
// its state (GET /api/v1/vehicles/{vin}, restricted to Parts).
type Endpoint string

// Vehicle is the one endpoint.
const Vehicle Endpoint = "vehicle"

// Normalizer reads the stored responses of the API into snapshots, for the derivation.
//
// Every value is dated from the car's own send, the carCapturedTimestamp of its part:
// the API returns the last state each part was sent in, without waking the car, and the
// parts of one response may be of different ages (core.Params.VehicleTimestamps). A part
// the response lacks (listed in its errors) and a field it omits are unknown, never a
// value; a value of an enumeration this does not know is left out (it stays in the raw
// response, on the debug page).
type Normalizer struct{}

// Kinds returns the kinds of response Normalize reads: the one endpoint.
func (Normalizer) Kinds() []core.Kind { return kinds(Vehicle) }

// EnergyKinds returns the kind holding the energy state: the same.
func (Normalizer) EnergyKinds() []core.Kind { return kinds(Vehicle) }

// StateKinds returns none: the doors, the windows and the climate ride in the same
// response as the energy, as Tesla's vehicle_data.
func (Normalizer) StateKinds() []core.Kind { return nil }

// HealthKinds returns none: the API tells no tyre, service nor warning.
func (Normalizer) HealthKinds() []core.Kind { return nil }

// SleepKinds returns none: the API does not tell whether the car sleeps.
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

// NormalizeHealth reads nothing: the API tells no health (HealthKinds is empty).
func (Normalizer) NormalizeHealth(k core.Kind, _ []byte) (core.Health, error) {
	return core.Health{}, fmt.Errorf("normalize health: %s tells no health (%s)", Source, k.Endpoint)
}

func kinds(eps ...Endpoint) []core.Kind {
	out := make([]core.Kind, len(eps))
	for i, ep := range eps {
		out[i] = core.Kind{Source: Source, Endpoint: string(ep)}
	}
	return out
}

// The response, as the contract (1.1.0) writes it. Every field is a pointer or a string
// whose empty value is "not told": a part absent stays nil.
type (
	response struct {
		Vehicle *vehicle `json:"vehicle"`
		Errors  []struct {
			Type string `json:"type"`
		} `json:"errors"`
	}
	vehicle struct {
		VIN             string           `json:"vin"`
		Name            string           `json:"name"`
		Status          *status          `json:"status"`
		Odometer        *odometer        `json:"odometer"`
		ParkingPosition *parkingPosition `json:"parkingPosition"`
		AirConditioning *airConditioning `json:"airConditioning"`
		Charging        *charging        `json:"charging"`
	}
	status struct {
		Overall *struct {
			DoorsLocked string `json:"doorsLocked"`
			Locked      string `json:"locked"`
			Doors       string `json:"doors"`
			Windows     string `json:"windows"`
		} `json:"overall"`
		Detail *struct {
			Sunroof string `json:"sunroof"`
			Trunk   string `json:"trunk"`
			Bonnet  string `json:"bonnet"`
		} `json:"detail"`
		At *time.Time `json:"carCapturedTimestamp"`
	}
	odometer struct {
		MileageInKm *float64   `json:"mileageInKm"`
		At          *time.Time `json:"carCapturedTimestamp"`
	}
	parkingPosition struct {
		State          string `json:"state"`
		GPSCoordinates *struct {
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
		} `json:"gpsCoordinates"`
		// carCapturedTimestamp is not in the contract's ParkingPosition: read if a
		// response carries it, else the position is undated.
		At *time.Time `json:"carCapturedTimestamp"`
	}
	airConditioning struct {
		State             string `json:"state"`
		TargetTemperature *struct {
			Value *float64 `json:"value"`
			Unit  string   `json:"unit"`
		} `json:"targetTemperature"`
		At *time.Time `json:"carCapturedTimestamp"`
	}
	charging struct {
		Status *struct {
			ChargePowerInKw                      *float64 `json:"chargePowerInKw"`
			RemainingTimeToFullyChargedInMinutes *float64 `json:"remainingTimeToFullyChargedInMinutes"`
			State                                string   `json:"state"`
			ChargeType                           string   `json:"chargeType"`
			PlugConnectionState                  string   `json:"plugConnectionState"`
			Battery                              *struct {
				RemainingCruisingRangeInMeters *float64 `json:"remainingCruisingRangeInMeters"`
				StateOfChargeInPercent         *float64 `json:"stateOfChargeInPercent"`
			} `json:"battery"`
		} `json:"status"`
		Settings *struct {
			TargetStateOfChargeInPercent *float64 `json:"targetStateOfChargeInPercent"`
			MaxChargeCurrentAcAmpere     *float64 `json:"maxChargeCurrentAcAmpere"`
		} `json:"settings"`
		At *time.Time `json:"carCapturedTimestamp"`
	}
)

// at is a part's send time, UTC; zero when not told.
func at(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

// Normalize converts a raw response of the vehicle into a vendor-neutral snapshot. A part
// the response has is covered: its values replace the previous ones, absent ones
// included; a part it lacks is not, and keeps its previous values.
func Normalize(raw []byte) (core.Snapshot, error) {
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return core.Snapshot{}, fmt.Errorf("normalize: %w", err)
	}
	if r.Vehicle == nil {
		return core.Snapshot{}, fmt.Errorf("normalize: no vehicle in the response")
	}
	var s core.Snapshot
	v := r.Vehicle
	normalizeModel(v, &s)
	if v.Charging != nil {
		normalizeCharging(v.Charging, &s)
	}
	if v.Odometer != nil {
		s.Covers |= core.FieldOdometer
		if v.Odometer.MileageInKm != nil {
			s.OdometerKm = core.Some(*v.Odometer.MileageInKm, at(v.Odometer.At))
		}
	}
	if v.ParkingPosition != nil {
		normalizeParking(v.ParkingPosition, &s)
	}
	if v.Status != nil {
		normalizeStatus(v.Status, &s)
	}
	if v.AirConditioning != nil {
		normalizeClimate(v.AirConditioning, &s)
	}
	return s, nil
}

// chargingStates maps charging.status.state, which the API derives from the charging
// process and the plug as the MyŠkoda app does. CONNECT_CABLE is its word for a plug
// disconnected; an omitted state is unknown (the cable plugged and unlocked with no
// charge running, or a status the car reported invalid after a drive).
var chargingStates = map[string]core.ChargingStatus{
	"CHARGING":             core.ChargingActive,
	"CONSERVING":           core.ChargingDone,
	"READY_FOR_CHARGING":   core.ChargingIdle,
	"CONNECT_CABLE":        core.ChargingIdle,
	"CHARGING_INTERRUPTED": core.ChargingError,
	"DISCHARGING":          core.ChargingDischarging,
}

// normalizeCharging reads the state of charge, the range, the charge and its power, the
// plug and the target.
func normalizeCharging(c *charging, s *core.Snapshot) {
	t := at(c.At)
	s.Covers |= core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
		core.FieldChargeType | core.FieldPower | core.FieldTargetSoC | core.FieldChargeDetails
	if st := c.Status; st != nil {
		if b := st.Battery; b != nil {
			if b.StateOfChargeInPercent != nil {
				s.SoC = core.Some(*b.StateOfChargeInPercent, t)
			}
			if b.RemainingCruisingRangeInMeters != nil {
				s.RangeKm = core.Some(*b.RemainingCruisingRangeInMeters/1000, t)
			}
		}
		if v, ok := chargingStates[st.State]; ok {
			s.Charging = core.Some(v, t)
		}
		// The plug as the car reports it (1.1.0); without it, CONNECT_CABLE alone
		// tells a cable out.
		switch {
		case st.PlugConnectionState == "CONNECTED":
			s.Connection = core.Some(core.Connected, t)
		case st.PlugConnectionState == "DISCONNECTED", st.PlugConnectionState == "" && st.State == "CONNECT_CABLE":
			s.Connection = core.Some(core.Disconnected, t)
		}
		switch st.ChargeType {
		case "AC":
			s.ChargeType = core.Some(core.AC, t)
		case "DC":
			s.ChargeType = core.Some(core.DC, t)
		case "OFF":
			s.ChargeType = core.Some(core.NoCharge, t)
		}
		if st.ChargePowerInKw != nil {
			s.PowerW = core.Some(*st.ChargePowerInKw*1000, t)
		}
		// Assumption (to check on a real car): the time left runs to the charge's
		// target, as the app shows it, despite its name. Zero tells no charge.
		if m := st.RemainingTimeToFullyChargedInMinutes; m != nil && *m > 0 {
			s.TimeLeft = core.Some(core.TimeLeft{
				Duration: time.Duration(*m * float64(time.Minute)), To: core.ToTarget,
			}, t)
		}
	}
	if set := c.Settings; set != nil {
		if set.TargetStateOfChargeInPercent != nil {
			s.TargetSoC = core.Some(*set.TargetStateOfChargeInPercent, t)
		}
		if set.MaxChargeCurrentAcAmpere != nil && *set.MaxChargeCurrentAcAmpere > 0 {
			s.CurrentLimitA = core.Some(*set.MaxChargeCurrentAcAmpere, t)
		}
	}
}

// normalizeParking reads whether the car drives: the API has neither ignition nor speed,
// and its parking position says IN_MOTION while the car moves, PARKED with its position
// once parked (assumption, to check on a real car: IN_MOTION holds for the whole trip,
// and not during a charge). The formatted address is not kept: the core geocodes.
func normalizeParking(p *parkingPosition, s *core.Snapshot) {
	t := at(p.At)
	switch p.State {
	case "IN_MOTION":
		s.Covers |= core.FieldEngine
		s.Engine = core.Some(core.EngineRunning, t)
	case "PARKED":
		s.Covers |= core.FieldEngine
		s.Engine = core.Some(core.EngineStopped, t)
		if g := p.GPSCoordinates; g != nil && g.Latitude != nil && g.Longitude != nil {
			s.Covers |= core.FieldPosition
			s.Position = core.Some(core.Position{Lat: *g.Latitude, Lon: *g.Longitude}, t)
		}
	}
}

// openings maps the status's words of a door, a lid or the windows.
var openings = map[string]core.Opening{"OPEN": core.Open, "CLOSED": core.Closed}

// normalizeStatus reads the lock, the doors, the lids and the windows, which the API
// aggregates: the doors as one (each of the four is given the aggregate when it is
// closed, since all are; when one is open, which one is not told), the side windows as
// one.
func normalizeStatus(st *status, s *core.Snapshot) {
	t := at(st.At)
	s.Covers |= core.FieldDoors | core.FieldWindows
	var d core.Doors
	var w core.Windows
	if o := st.Overall; o != nil {
		// locked tells the lock (YES, NO), doorsLocked too when locked is unknown:
		// OPENED and TRUNK_OPENED tell something open, so not locked.
		switch o.Locked {
		case "YES":
			d.Locked = core.Some(true, t)
		case "NO":
			d.Locked = core.Some(false, t)
		default:
			switch o.DoorsLocked {
			case "YES":
				d.Locked = core.Some(true, t)
			case "NO", "OPENED", "TRUNK_OPENED":
				d.Locked = core.Some(false, t)
			}
		}
		if o.Doors == "CLOSED" {
			for _, door := range []*core.Value[core.Opening]{&d.FrontLeft, &d.FrontRight, &d.RearLeft, &d.RearRight} {
				*door = core.Some(core.Closed, t)
			}
		}
		if o.Windows == "CLOSED" {
			for _, win := range []*core.Value[core.Opening]{&w.FrontLeft, &w.FrontRight, &w.RearLeft, &w.RearRight} {
				*win = core.Some(core.Closed, t)
			}
		}
	}
	if dt := st.Detail; dt != nil {
		if v, ok := openings[dt.Bonnet]; ok {
			d.Hood = core.Some(v, t)
		}
		if v, ok := openings[dt.Trunk]; ok {
			d.Tailgate = core.Some(v, t)
		}
		if v, ok := openings[dt.Sunroof]; ok {
			w.Sunroof = core.Some(v, t)
		}
	}
	if d != (core.Doors{}) {
		s.Doors = core.Some(d, t)
	}
	if w != (core.Windows{}) {
		s.Windows = core.Some(w, t)
	}
}

// climateOn are the air conditioning's states that run it; OFF and COMPLETED do not;
// UNKNOWN, UNSUPPORTED and a word this does not know tell nothing.
var climateOn = map[string]bool{
	"COOLING": true, "HEATING": true, "HEATING_AUXILIARY": true, "VENTILATION": true,
	"OFF": false, "COMPLETED": false,
}

// normalizeClimate reads whether the climate runs, and its setpoint in °C.
func normalizeClimate(a *airConditioning, s *core.Snapshot) {
	t := at(a.At)
	s.Covers |= core.FieldClimate
	var c core.Climate
	if on, ok := climateOn[a.State]; ok {
		c.On = core.Some(on, t)
	}
	if tt := a.TargetTemperature; tt != nil && tt.Value != nil {
		switch tt.Unit {
		case "CELSIUS":
			c.SetpointC = core.Some(*tt.Value, t)
		case "FAHRENHEIT":
			c.SetpointC = core.Some((*tt.Value-32)*5/9, t)
		}
	}
	if c != (core.Climate{}) {
		s.Climate = core.Some(c, t)
	}
}

// familyOf tells the catalog's family from a Škoda's VIN: NY at its 7th and 8th
// characters is the MEB platform every Enyaq, Enyaq Coupé and Elroq shares, and its 4th
// tells them apart (assumption, from published VINs and the community's tables, to
// confirm on real cars: N or R an Elroq, G a Coupé, the rest an Enyaq). Empty for
// another model.
func familyOf(vin string) string {
	if vin[6:8] != "NY" {
		return ""
	}
	switch vin[3] {
	case 'N', 'R':
		return "Elroq"
	case 'G':
		return "Enyaq Coupé"
	}
	return "Enyaq"
}

// modelNames are the model names the API gives as the vehicle's name when the owner
// did not name it, by the family the catalog knows them as.
var modelNames = map[string]string{
	"enyaq": "Enyaq", "enyaq coupé": "Enyaq Coupé", "enyaq coupe": "Enyaq Coupé", "elroq": "Elroq",
}

// normalizeModel reads what the response tells of the model: the brand (the source's),
// the family from the name when it is a model's, else from the VIN, the
// year from the VIN's tenth character, and the name the owner gave the car. The
// powertrain is electric for a family the catalog knows (every Enyaq and Elroq is);
// otherwise not told, and the user picks among Škoda's variants.
func normalizeModel(v *vehicle, s *core.Snapshot) {
	if !vinOf(v.VIN) {
		return
	}
	s.Covers |= core.FieldModel | core.FieldSheet
	s.Brand = core.Some(Brand, time.Time{})
	family, named := familyOf(v.VIN), false
	if f, ok := modelNames[strings.ToLower(strings.TrimSpace(v.Name))]; ok {
		// The model's name, which the API gives when the owner did not name the car:
		// it settles the family the VIN only assumes.
		family, named = f, true
	}
	if family != "" {
		s.Family = core.Some(family, time.Time{})
		s.Powertrain = core.Some(core.Electric, time.Time{})
	}
	if year, ok := modelYear(v.VIN); ok {
		s.ModelYear = core.Some(year, time.Time{})
	}
	if name := strings.TrimSpace(v.Name); name != "" && !named {
		s.Sheet = core.Some(core.Sheet{Name: name}, time.Time{})
	}
}

// vinOf reports whether s is a VIN: 17 characters, without I, O nor Q (ISO 3779).
func vinOf(s string) bool {
	return len(s) == 17 && vinPattern.MatchString(s)
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
