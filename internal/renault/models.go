package renault

// Endpoint is a state read for a vehicle, named as Kamereon names it.
type Endpoint string

// The endpoints a logger reads. The others (charge-mode, charging-settings, the
// schedules, lock-status, pressure…) are refused on nearly every electric car
// (renault-api's map); charges, the car's own sessions, comes later (ADR 0050 §6).
const (
	// BatteryStatus tells the charge: the state of charge, the range, the plug, the
	// charging state and the time left.
	BatteryStatus Endpoint = "battery-status"
	// Cockpit tells the odometer.
	Cockpit Endpoint = "cockpit"
	// Location tells the position.
	Location Endpoint = "location"
	// HVACStatus tells the climate and the temperatures outside and inside.
	HVACStatus Endpoint = "hvac-status"
	// Details tells the model: the brand, the model's code and name, the energy. Read
	// at the details pace so that a vehicle's model stays known.
	Details Endpoint = "details"
)

// Endpoints lists the endpoints the collector reads, in its pass order.
func Endpoints() []Endpoint { return []Endpoint{BatteryStatus, Cockpit, Location, HVACStatus, Details} }

// path is the endpoint's path under the account (/commerce/v1/accounts/{id}).
func (e Endpoint) path() (string, bool) {
	switch e {
	case BatteryStatus:
		return "/kamereon/kca/car-adapter/v2/cars/{vin}/battery-status", true
	case Cockpit, Location, HVACStatus:
		return "/kamereon/kca/car-adapter/v1/cars/{vin}/" + string(e), true
	case Details:
		return "/vehicles/{vin}/details", true
	}
	return "", false
}

// unserved are the endpoints a model does not serve, by its code (renault-api's
// kamereon/models.py, the electric ones and the plug-in hybrids: ADR 0050 §4). A model
// not listed is read on every endpoint, as renault-api does, its refusals left out for
// a while by the collector.
var unserved = map[string][]Endpoint{
	"X101VE": {Location}, // Zoe phase 1: err.tech.501
	"XFB2BI": {Location}, // Mégane IV
	// The combustion cars and the full hybrids serve no battery nor climate; they are
	// not read (Vehicle.Electric), listed for a car whose energy reads otherwise.
	"BD11SL": {BatteryStatus, HVACStatus},
	"DU31SU": {BatteryStatus, HVACStatus},
	"XHN1ML": {BatteryStatus, HVACStatus},
	"XHN1SU": {BatteryStatus, HVACStatus},
	"XJA1VP": {BatteryStatus, HVACStatus},
	"XJA2VP": {BatteryStatus, HVACStatus},
	"XJB2CP": {BatteryStatus, HVACStatus},
	"XJB2SU": {BatteryStatus, HVACStatus},
	"XJF2BI": {BatteryStatus, HVACStatus},
	"XJL2TR": {HVACStatus},
}

// Serves reports whether a model, by its code, serves the endpoint.
func Serves(modelCode string, ep Endpoint) bool {
	for _, u := range unserved[modelCode] {
		if u == ep {
			return false
		}
	}
	return true
}

// Model is what Runsten knows of a model code: its family as the catalog names it, and
// its quirks.
type Model struct {
	Family string
	// PowerInWatts: chargingInstantaneousPower is the charge's power, in watts (Zoe
	// phase 1 alone); elsewhere it is the power the car would accept, in kilowatts,
	// never read.
	PowerInWatts bool
	// BatteryTempWrong: batteryTemperature means nothing (Zoe phase 2).
	BatteryTempWrong bool
}

// models are the electric models and their families (renault-api's map, its comments
// naming them).
var models = map[string]Model{
	"A4E1VE": {Family: "Renault 4 E-Tech"},
	"A5E1AE": {Family: "Alpine A290"},
	"R5E1VE": {Family: "Renault 5 E-Tech"},
	"TWE1VE": {Family: "Twingo E-Tech"},
	"X071VE": {Family: "Twingo Electric"},
	"X101VE": {Family: "Zoe", PowerInWatts: true},
	"X102VE": {Family: "Zoe", BatteryTempWrong: true},
	"XBG1VE": {Family: "Spring"},
	"XBG2VE": {Family: "Spring"},
	"XBG2DE": {Family: "Spring"},
	"XCB1SE": {Family: "Scenic E-Tech"},
	"XCB1VE": {Family: "Megane E-Tech"},
	"XDD1VE": {Family: "Master E-Tech"},
}

// ModelOf returns what Runsten knows of a model code; false for one it does not.
func ModelOf(code string) (Model, bool) {
	m, ok := models[code]
	return m, ok
}
