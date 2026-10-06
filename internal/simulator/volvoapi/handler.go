// Package volvoapi exposes simulated vehicles as the Volvo Cars API (Connected
// Vehicle v2, Energy v2, Location v1). The response shapes reproduce the real
// responses captured on the Volvo demo car.
package volvoapi

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// Source provides the uploaded state of a simulated vehicle.
type Source interface {
	VIN() string
	ReportAt(at time.Time) vehicle.Report
}

// Handler serves the simulated API.
type Handler struct {
	vins []string          // in the order of the sources
	srcs map[string]Source // by VIN
	clk  clock.Clock
	mux  *http.ServeMux
}

// API names used for quota accounting.
const (
	apiConnectedVehicle = "connected-vehicle"
	apiEnergy           = "energy"
	apiLocation         = "location"
)

// NewHandler creates the simulated API for the vehicles srcs, with the simulated clock
// clk, the limits limits, the faults faults and the authorization server auth. They
// are the vehicles of one account: one Volvo ID, whose tokens reach them all, and one
// quota. Their VINs must be distinct (scenario.NewFleet checks it): NewHandler panics
// otherwise, as http.ServeMux does on a duplicate pattern.
func NewHandler(srcs []Source, clk clock.Clock, limits Limits, faults Faults, auth OAuth) *Handler {
	h := &Handler{srcs: make(map[string]Source, len(srcs)), clk: clk, mux: http.NewServeMux()}
	for _, src := range srcs {
		if _, dup := h.srcs[src.VIN()]; dup {
			panic("volvoapi: duplicate VIN " + src.VIN())
		}
		h.srcs[src.VIN()] = src
		h.vins = append(h.vins, src.VIN())
	}
	iss := newIssuer(clk, auth)
	lim := newLimiter(clk, limits, iss)

	cvRoutes := map[string]func(vehicle.Report) any{
		"fuel":          renderFuel,
		"odometer":      renderOdometer,
		"statistics":    renderStatistics,
		"engine-status": renderEngineStatus,
		"engine":        renderEngine,
		"doors":         renderDoors,
		"windows":       renderWindows,
		"tyres":         renderTyres,
		"brakes":        renderBrakes,
		"diagnostics":   renderDiagnostics,
		"warnings":      renderWarnings,
	}

	cvMux := http.NewServeMux()
	cvMux.HandleFunc("GET /connected-vehicle/v2/vehicles", h.vehicles)
	cvMux.Handle("GET /connected-vehicle/v2/vehicles/{vin}", h.withReport(renderDetails))
	for name, render := range cvRoutes {
		cvMux.Handle("GET /connected-vehicle/v2/vehicles/{vin}/"+name, h.withReport(func(_ string, r vehicle.Report) any {
			return render(r)
		}))
	}
	// Commands are out of scope (Runsten is a logger and only requests read scopes):
	// respond as for a token lacking the required scopes.
	cvMux.HandleFunc("/connected-vehicle/v2/vehicles/{vin}/commands", forbidden)
	cvMux.HandleFunc("/connected-vehicle/v2/vehicles/{vin}/commands/{command}", forbidden)
	cvMux.HandleFunc("GET /connected-vehicle/v2/vehicles/{vin}/command-accessibility", forbidden)
	cvMux.HandleFunc("/", notFound)

	energyMux := http.NewServeMux()
	energyMux.HandleFunc("GET /energy/v2/vehicles/{vin}/state", h.energyState)
	energyMux.HandleFunc("GET /energy/v2/vehicles/{vin}/capabilities", h.energyCapabilities)
	energyMux.HandleFunc("/", notFound)

	locationMux := http.NewServeMux()
	locationMux.HandleFunc("GET /location/v1/vehicles/{vin}/location", h.location)
	locationMux.HandleFunc("/", notFound)

	api := http.NewServeMux()
	api.Handle("/connected-vehicle/", lim.middleware(apiConnectedVehicle, cvMux))
	api.Handle("/energy/v2/", lim.middleware(apiEnergy, energyMux))
	api.HandleFunc("/energy/v1/", gone)
	api.Handle("/location/", lim.middleware(apiLocation, locationMux))
	api.HandleFunc("/", notFound)
	h.mux.Handle("/", faults.middleware(clk, api))
	// Volvo ID is a separate service (volvoid.eu.volvocars.com): the API faults and
	// limits do not apply to it (assumption).
	h.mux.Handle("/as/", iss.routes())
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// report returns the uploaded state of the vehicle of the path's VIN.
func (h *Handler) report(w http.ResponseWriter, r *http.Request) (vehicle.Report, bool) {
	src, ok := h.srcs[r.PathValue("vin")]
	if !ok {
		notFound(w, r) // real behavior for an unknown VIN: not observed
		return vehicle.Report{}, false
	}
	return src.ReportAt(h.clk.Now()), true
}

func (h *Handler) withReport(render func(vin string, r vehicle.Report) any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rep, ok := h.report(w, r); ok {
			writeJSON(w, http.StatusOK, map[string]any{"data": render(r.PathValue("vin"), rep)})
		}
	})
}

func (h *Handler) vehicles(w http.ResponseWriter, _ *http.Request) {
	data := make([]map[string]string, len(h.vins))
	for i, vin := range h.vins {
		data[i] = map[string]string{"vin": vin}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func renderDetails(vin string, r vehicle.Report) any {
	return map[string]any{
		"vin":                vin,
		"modelYear":          r.Spec.ModelYear,
		"gearbox":            "AUTOMATIC",
		"fuelType":           "ELECTRIC",
		"externalColour":     "SIMULATED",
		"batteryCapacityKWH": r.Spec.BatteryKWh,
		"images":             map[string]string{"exteriorImageUrl": "", "internalImageUrl": ""},
		"descriptions":       map[string]string{"model": r.Spec.Model, "steering": "LEFT", "upholstery": "SIMULATED"},
	}
}

func renderFuel(r vehicle.Report) any {
	return map[string]cvValue{
		"batteryChargeLevel": cv(r.EnergyAt, tsNano, "%", round1(r.Energy.SoC)),
		"fuelAmount":         cv(r.StatusAt, tsNano, "l", 0.0), // electric vehicle: real value unknown
	}
}

func renderOdometer(r vehicle.Report) any {
	return map[string]cvValue{"odometer": cv(r.StatusAt, tsNano, "km", int(r.Status.OdometerKm))}
}

func renderStatistics(r vehicle.Report) any {
	s, at := r.Status, r.StatusAt
	avg := func(kwh, km float64) any {
		if km <= 0 {
			return nil
		}
		return math.Round(kwh/km*100*10000) / 10000
	}
	speed := func(km float64, d time.Duration) any {
		if d <= 0 {
			return nil
		}
		return int(math.Round(km / d.Hours()))
	}
	// Combustion fields are null on an electric vehicle (community feedback reports the
	// reverse: electric fields are null on a combustion car).
	return map[string]cvValue{
		"averageEnergyConsumption":            cv(at, tsMillis, "kWh/100km", avg(s.TotalEnergyKWh, s.TotalDistanceKm)),
		"averageEnergyConsumptionAutomatic":   cv(at, tsMillis, "kWh/100km", avg(s.LastTrip.EnergyKWh, s.LastTrip.DistanceKm)),
		"averageEnergyConsumptionSinceCharge": cv(at, tsMillis, "kWh/100km", avg(s.EnergySinceChargeKWh, s.DistanceSinceChargeKm)),
		"averageFuelConsumption":              cv(at, tsMillis, "l/100km", nil),
		"averageFuelConsumptionAutomatic":     cv(at, tsMillis, "l/100km", nil),
		"averageSpeed":                        cv(at, tsMillis, "km/h", speed(s.TotalDistanceKm, s.TotalDriveTime)),
		"averageSpeedAutomatic":               cv(at, tsMillis, "km/h", speed(s.LastTrip.DistanceKm, s.LastTrip.Duration)),
		"distanceToEmptyBattery":              cv(at, tsMillis, "km", electricRange(r.Spec, at, s.SoC)),
		"distanceToEmptyTank":                 cv(at, tsMillis, "km", nil),
		"tripMeterAutomatic":                  cv(at, tsMillis, "km", round1(s.LastTrip.DistanceKm)),
		"tripMeterManual":                     cv(at, tsMillis, "km", round1(s.TotalDistanceKm)),
	}
}

func renderEngineStatus(r vehicle.Report) any {
	status := "STOPPED"
	if r.Status.Driving {
		status = "RUNNING" // assumption: a driving electric vehicle is "RUNNING"
	}
	return map[string]cvValue{"engineStatus": cv(r.StatusAt, tsNano, "", status)}
}

func renderDoors(r vehicle.Report) any {
	lock := "UNLOCKED"
	if r.Status.Locked {
		lock = "LOCKED"
	}
	out := statusMap(r.StatusAt, "CLOSED", "frontLeftDoor", "frontRightDoor", "rearLeftDoor",
		"rearRightDoor", "hood", "tailgate", "tankLid")
	out["centralLock"] = cv(r.StatusAt, tsNano, "", lock)
	return out
}

func renderWindows(r vehicle.Report) any {
	return statusMap(r.StatusAt, "CLOSED", "frontLeftWindow", "frontRightWindow", "rearLeftWindow",
		"rearRightWindow", "sunroof")
}

func renderTyres(r vehicle.Report) any {
	return statusMap(r.StatusAt, "NO_WARNING", "frontLeft", "frontRight", "rearLeft", "rearRight")
}

func renderBrakes(r vehicle.Report) any {
	return statusMap(r.StatusAt, "NO_WARNING", "brakeFluidLevelWarning")
}

func renderEngine(r vehicle.Report) any {
	return statusMap(r.StatusAt, "NO_WARNING", "engineCoolantLevelWarning", "oilLevelWarning")
}

func renderDiagnostics(r vehicle.Report) any {
	out := statusMap(r.StatusAt, "NO_WARNING", "serviceWarning", "serviceTrigger", "washerFluidLevelWarning")
	out["distanceToService"] = cv(r.StatusAt, tsNano, "km", 20000)
	out["engineHoursToService"] = cv(r.StatusAt, tsNano, "h", 1000)
	out["timeToService"] = cv(r.StatusAt, tsNano, "months", 12)
	return out
}

func renderWarnings(r vehicle.Report) any {
	return statusMap(r.StatusAt, "NO_WARNING",
		"brakeLightCenterWarning", "brakeLightLeftWarning", "brakeLightRightWarning",
		"daytimeRunningLightLeftWarning", "daytimeRunningLightRightWarning",
		"fogLightFrontWarning", "fogLightRearWarning", "hazardLightsWarning",
		"highBeamLeftWarning", "highBeamRightWarning", "lowBeamLeftWarning", "lowBeamRightWarning",
		"positionLightFrontLeftWarning", "positionLightFrontRightWarning",
		"positionLightRearLeftWarning", "positionLightRearRightWarning",
		"registrationPlateLightWarning", "reverseLightsWarning", "sideMarkLightsWarning",
		"turnIndicationFrontLeftWarning", "turnIndicationFrontRightWarning",
		"turnIndicationRearLeftWarning", "turnIndicationRearRightWarning")
}

func statusMap(at time.Time, value string, fields ...string) map[string]cvValue {
	out := make(map[string]cvValue, len(fields))
	for _, f := range fields {
		out[f] = cv(at, tsNano, "", value)
	}
	return out
}

func (h *Handler) energyState(w http.ResponseWriter, r *http.Request) {
	rep, ok := h.report(w, r)
	if !ok {
		return
	}
	e, at, caps := rep.Energy, rep.EnergyAt, rep.Spec.Capabilities

	connection, power := "DISCONNECTED", "NO_POWER_AVAILABLE" // assumed enum values
	if e.PluggedIn {
		connection = "CONNECTED"
	}
	if e.Charging == vehicle.Charging {
		power = "PROVIDING_POWER"
	}
	minutes := 0
	if e.Charging == vehicle.Charging && e.ChargingPowerW > 0 {
		// Over the usable capacity, which the SoC runs over (only batteryCapacityKWH
		// reports the nominal one).
		kwh := (e.TargetSoC - e.SoC) / 100 * rep.Spec.UsableKWhAt(at)
		minutes = int(math.Ceil(kwh / (e.ChargingPowerW / 1000) * 60))
	}

	state := map[string]energyValue{
		"batteryChargeLevel":      energyOK(at, "percentage", int(e.SoC)),
		"electricRange":           energyOK(at, "km", electricRange(rep.Spec, at, e.SoC)),
		"chargerConnectionStatus": energyOK(at, "", connection),
		"chargingStatus":          energyOK(at, "", chargingStatus(e.Charging)),
		"chargingType":            energyOK(at, "", chargeType(e.ChargeType)),
		"chargerPowerStatus":      energyOK(at, "", power),
		"estimatedChargingTimeToTargetBatteryChargeLevel": energyOK(at, "minutes", minutes),
		"targetBatteryChargeLevel":                        energyOK(at, "percentage", int(e.TargetSoC)),
		"chargingCurrentLimit":                            optional(caps.ChargingCurrentLimit, energyOK(at, "ampere", int(rep.Spec.ChargingCurrentLimitA))),
		"chargingPower":                                   optional(caps.ChargingPower, energyOK(at, "watts", int(e.ChargingPowerW))),
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handler) energyCapabilities(w http.ResponseWriter, r *http.Request) {
	rep, ok := h.report(w, r)
	if !ok {
		return
	}
	caps := rep.Spec.Capabilities
	supported := func(ok bool) map[string]bool { return map[string]bool{"isSupported": ok} }
	state := map[string]any{"isSupported": true}
	for _, f := range []string{
		"batteryChargeLevel", "electricRange", "chargerConnectionStatus",
		"chargingSystemStatus", "chargingType", "chargerPowerStatus",
		"estimatedChargingTimeToTargetBatteryChargeLevel", "targetBatteryChargeLevel",
	} {
		state[f] = supported(true)
	}
	state["chargingCurrentLimit"] = supported(caps.ChargingCurrentLimit == vehicle.Supported)
	state["chargingPower"] = supported(caps.ChargingPower == vehicle.Supported)
	writeJSON(w, http.StatusOK, map[string]any{"getEnergyState": state})
}

func (h *Handler) location(w http.ResponseWriter, r *http.Request) {
	rep, ok := h.report(w, r)
	if !ok {
		return
	}
	p := rep.Location
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"type": "Feature",
			"geometry": map[string]any{
				"type":        "Point",
				"coordinates": []float64{p.Lon, p.Lat, 0.0},
			},
			"properties": map[string]string{
				"timestamp": rep.LocationAt.UTC().Format(tsNano),
				"heading":   strconv.Itoa(int(math.Round(p.Heading)) % 360), // string, like the real API
			},
		},
		"operationId": operationID(),
		"status":      http.StatusOK,
	})
}

func optional(a vehicle.Availability, ok energyValue) energyValue {
	switch a {
	case vehicle.Unsupported:
		return energyErr("NOT_SUPPORTED", "Property is not supported by this vehicle")
	case vehicle.NotFound:
		return energyErr("PROPERTY_NOT_FOUND", "Property not found")
	default:
		return ok
	}
}

// electricRange is the range the vehicle forecasts at the SoC's instant: over the
// usable capacity, which the SoC runs over.
func electricRange(s vehicle.Spec, at time.Time, soc float64) int {
	return int(soc / 100 * s.UsableKWhAt(at) / s.ConsumptionKWhPer100km * 100)
}

func chargingStatus(c vehicle.ChargingStatus) string {
	switch c {
	case vehicle.Charging:
		return "CHARGING"
	case vehicle.Done:
		return "DONE"
	default:
		return "IDLE"
	}
}

func chargeType(t vehicle.ChargeType) string {
	switch t {
	case vehicle.AC:
		return "AC"
	case vehicle.DC:
		return "DC"
	default:
		return "NONE"
	}
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeStatusError(w, http.StatusNotFound, "Resource not found")
}

func forbidden(w http.ResponseWriter, _ *http.Request) {
	writeCVError(w, http.StatusForbidden, "FORBIDDEN", "Access Denied")
}

func gone(w http.ResponseWriter, _ *http.Request) {
	writeCVError(w, http.StatusGone, "GONE", "Version 1 is removed. See https://developer.volvocars.com/apis/energy/v2/overview/")
}
