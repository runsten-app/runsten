// Package polestarapi exposes simulated vehicles as the Polestar Data Portal's M2M API
// (EU Data Act): a token for the owner's credential, the list of its VINs, and the
// telemetry and charging domains a logger reads. The response shapes reproduce the
// real responses of a Polestar 2 (testdata/polestar-data-portal).
package polestarapi

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// Prefix is the path the handler serves: the real base URL's,
// https://pc-api.polestar.com/eu-north-1/data-portal/m2m.
const Prefix = "/eu-north-1/data-portal/m2m/"

// Source provides the uploaded state of a simulated vehicle.
type Source interface {
	VIN() string
	ReportAt(at time.Time) vehicle.Report
}

// Behavior groups the assumptions about how the car uploads, beyond the vehicle
// model's (vehicle.UploadPolicy).
type Behavior struct {
	// ParkedUpload is how often a car that is not driving uploads its battery and
	// availability, unchanged: observed a few minutes old on a parked Polestar 2.
	// Zero: only on a change.
	ParkedUpload time.Duration
	// AbandonAfter is how long after its last drive or charge a parked car reports
	// itself ABANDONED rather than INACTIVE (observed: ABANDONED on a car parked long,
	// the delay unknown).
	AbandonAfter time.Duration
}

// DefaultBehavior returns the assumptions used without explicit configuration.
func DefaultBehavior() Behavior {
	return Behavior{ParkedUpload: 10 * time.Minute, AbandonAfter: time.Hour}
}

// Handler serves the simulated API.
type Handler struct {
	vins     []string          // in the order of the sources
	srcs     map[string]Source // by VIN
	clk      clock.Clock
	behavior Behavior
	auth     *authority
	mux      *http.ServeMux
}

// domains are every domain the real API has, by kind; the ones a logger reads are
// rendered, the others answer DATA_NOT_AVAILABLE, as a car that does not upload them.
var domains = map[string][]string{
	"telemetry": {
		"availability", "battery", "exterior", "health", "location", "odometer",
		"parking-climatization", "pre-cleaning",
	},
	"charging": {
		"amp-limit", "charge-locations", "charge-now", "global-charge-timer",
		"is-at-charge-location", "parking-climate-timer", "target-soc",
	},
}

// NewHandler creates the simulated API for the vehicles srcs, the vehicles one
// credential reads, with the simulated clock clk. Their VINs must be distinct
// (scenario.NewFleet checks it): NewHandler panics otherwise.
func NewHandler(srcs []Source, clk clock.Clock, cred Credential, limits Limits, behavior Behavior) *Handler {
	h := &Handler{
		srcs: make(map[string]Source, len(srcs)), clk: clk, behavior: behavior,
		auth: newAuthority(clk, cred, limits), mux: http.NewServeMux(),
	}
	for _, src := range srcs {
		if _, dup := h.srcs[src.VIN()]; dup {
			panic("polestarapi: duplicate VIN " + src.VIN())
		}
		h.srcs[src.VIN()] = src
		h.vins = append(h.vins, src.VIN())
	}
	renders := map[string]func(vin string, r vehicle.Report, now time.Time) (any, time.Time){
		"battery":      h.renderBattery,
		"odometer":     renderOdometer,
		"location":     renderLocation,
		"availability": h.renderAvailability,
		"target-soc":   renderTargetSoC,
		"health":       renderHealth,
	}
	v1 := http.NewServeMux()
	v1.HandleFunc("GET "+Prefix+"v1/vehicles", h.vehicles)
	for kind, names := range domains {
		for _, name := range names {
			v1.Handle("GET "+Prefix+"v1/vehicles/{vin}/"+kind+"/"+name, h.domain(name, renders[name]))
		}
	}
	v1.HandleFunc(Prefix+"v1/", h.resourceNotFound)

	h.mux.HandleFunc("POST "+Prefix+"token", h.auth.token)
	h.mux.Handle(Prefix+"v1/", h.auth.middleware(v1))
	h.mux.HandleFunc("/", missingToken)
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// resourceNotFound answers a path of /v1 the API does not have.
func (h *Handler) resourceNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, h.clk.Now(), http.StatusNotFound, "VALIDATION_RESOURCE_NOT_FOUND",
		"The requested resource was not found: "+r.URL.Path, nil)
}

func (h *Handler) vehicles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.vins, "meta": map[string]int{"count": len(h.vins)}})
}

// domain serves one domain of a vehicle: render returns its data and the instant the
// car uploaded it; a nil render answers as for a domain the car does not upload.
func (h *Handler) domain(name string, render func(string, vehicle.Report, time.Time) (any, time.Time)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vin := r.PathValue("vin")
		src, ok := h.srcs[vin]
		if !ok {
			writeError(w, h.clk.Now(), http.StatusForbidden, "AUTHZ_VIN_UNAUTHORIZED",
				"The caller is not authorized to access this vehicle.", map[string]string{"requested_vin": vin})
			return
		}
		if render == nil {
			writeError(w, h.clk.Now(), http.StatusNotFound, "DATA_NOT_AVAILABLE",
				"No "+name+" data is available for this vehicle.", map[string]string{"requested_vin": vin})
			return
		}
		now := h.clk.Now()
		data, at := render(vin, src.ReportAt(now), now)
		m := data.(map[string]any) //nolint:forcetypeassert // every render returns an object
		m["vin"] = vin
		m["metaReceivedAt"] = at.Add(receiveDelay).UTC().Format(tsReceived)
		m["metaEventId"] = eventID(vin, name, at)
		writeJSON(w, http.StatusOK, map[string]any{"data": m, "meta": map[string]string{"domain": name, "vin": vin}})
	})
}

// uploadedAt is the instant of the car's last upload of a group it uploaded at at, and
// again every ParkedUpload while it does not drive.
func (h *Handler) uploadedAt(at, now time.Time, driving bool) time.Time {
	every := h.behavior.ParkedUpload
	if every <= 0 || driving || now.Before(at) {
		return at
	}
	return at.Add(now.Sub(at) / every * every)
}

func (h *Handler) renderBattery(_ string, r vehicle.Report, now time.Time) (any, time.Time) {
	e := r.Energy
	at := h.uploadedAt(r.EnergyAt, now, r.Status.Driving)
	connection, power := "DISCONNECTED", "NO_POWER_AVAILABLE"
	if e.PluggedIn {
		connection, power = "CONNECTED", "POWER_AVAILABLE_BUT_NOT_ACTIVATED"
	}
	minutes := 0
	if e.Charging == vehicle.Charging {
		power = "PROVIDING_POWER"
		if e.ChargingPowerW > 0 {
			kwh := (e.TargetSoC - e.SoC) / 100 * r.Spec.UsableKWhAt(at)
			minutes = int(math.Ceil(kwh / (e.ChargingPowerW / 1000) * 60))
		}
	}
	km := electricRange(r.Spec, at, e.SoC)
	m := map[string]any{
		"timestamp":                          timestamp(at),
		"batteryChargeLevelPercentage":       int(e.SoC),
		"estimatedDistanceToEmptyKm":         km,
		"estimatedDistanceToEmptyMiles":      int(float64(km) / kmPerMile),
		"estimatedChargingTimeToFullMinutes": minutes, // present at 0 on the real car
		"chargerConnectionStatus":            "CHARGER_CONNECTION_STATUS_" + connection,
		"chargerPowerStatus":                 "CHARGER_POWER_STATUS_" + power,
		"chargingStatus":                     "CHARGING_STATUS_" + chargingStatus(e.Charging),
		"chargingStatusV2":                   "CHARGING_STATUS_V2_" + chargingStatus(e.Charging),
		"chargingType":                       "CHARGING_TYPE_" + chargeType(e.ChargeType),
	}
	if e.TotalDistanceKm > 0 {
		m["averageEnergyConsumptionKwhPer100Km"] = round1(e.TotalEnergyKWh / e.TotalDistanceKm * 100)
	}
	// Power, current and voltage only while charging, as on the real car (a zero is
	// omitted), and only on a model that reports them.
	if e.Charging == vehicle.Charging && e.ChargingPowerW > 0 && r.Spec.Capabilities.ChargingPower.While(true) == vehicle.Supported {
		volts := 400.0 // assumption: 400 V, three-phase AC or a DC charge
		m["chargingPowerWatts"] = int(e.ChargingPowerW)
		m["chargingVoltageVolts"] = int(volts)
		m["chargingCurrentAmps"] = round1(e.ChargingPowerW / volts)
	}
	return m, at
}

func renderOdometer(_ string, r vehicle.Report, _ time.Time) (any, time.Time) {
	s, at := r.Status, r.StatusAt
	m := map[string]any{
		"timestamp":      timestamp(at),
		"odometerMeters": int(math.Round(s.OdometerKm * 1000)),
	}
	omitZero(m, "tripMeterManualKm", float64(float32(s.TotalDistanceKm)))
	omitZero(m, "tripMeterAutomaticKm", float64(float32(s.LastTrip.DistanceKm)))
	omitZero(m, "averageSpeedKmPerHour", speed(s.TotalDistanceKm, s.TotalDriveTime))
	omitZero(m, "averageSpeedKmPerHourAutomatic", speed(s.LastTrip.DistanceKm, s.LastTrip.Duration))
	return m, at
}

// renderLocation reports the position uploaded at the end of the last trip: whether
// the car uploads it while driving is unknown, and the vehicle model does not move it
// along the way.
func renderLocation(_ string, r vehicle.Report, _ time.Time) (any, time.Time) {
	p, at := r.Location, r.LocationAt
	return map[string]any{
		"timestamp":  timestamp(at.Truncate(time.Second)), // nanos 0 on the real car
		"coordinate": map[string]float64{"latitude": p.Lat, "longitude": p.Lon},
		"heading":    int(math.Round(p.Heading)) % 360,
		"altitude":   "0",
		"speed":      "0",
	}, at
}

func (h *Handler) renderAvailability(_ string, r vehicle.Report, now time.Time) (any, time.Time) {
	s := r.Status
	at := h.uploadedAt(r.StatusAt, now, s.Driving)
	mode := "INACTIVE"
	switch {
	case s.Driving:
		mode = "DRIVING"
	case h.behavior.AbandonAfter > 0 && !at.Before(latest(r.StatusAt, r.EnergyAt).Add(h.behavior.AbandonAfter)):
		mode = "ABANDONED"
	}
	return map[string]any{
		"timestamp":          timestamp(at),
		"availabilityStatus": "AVAILABILITY_STATUS_AVAILABLE",
		"usageMode":          "USAGE_MODE_" + mode,
	}, at
}

// renderTargetSoC reports the charge limit, set by the scenario's last charge. The
// instant of the setting is the last energy upload: the real one is unknown.
func renderTargetSoC(vin string, r vehicle.Report, _ time.Time) (any, time.Time) {
	at := r.EnergyAt
	millis := strconv.FormatInt(at.UnixMilli(), 10)
	return map[string]any{
		"id":        settingID(vin, "target-soc"),
		"timestamp": timestamp(at),
		"updatedAt": millis,
		"targetSoc": map[string]any{
			"batteryChargeTargetLevel":     int(r.Energy.TargetSoC),
			"chargeTargetLevelSettingType": "CUSTOM",
			"id":                           settingID(vin, "target-soc-value"),
			"source":                       "RCS",
			"timestamp":                    timestamp(at),
			"updatedAt":                    millis,
		},
	}, at
}

// Tyres of the simulated car: the recommended pressure, and the rear left tyre's slow
// leak, refilled every leakRefill driven, which its warning follows.
const (
	recommendedKPa = 280
	leakPer100km   = 1.0
	leakRefill     = 3000.0 // km
)

// renderHealth reports the tyres, the service and the warnings, uploaded with the status.
// The fields of the tyres are the client's assumption: the real car's are unknown.
func renderHealth(_ string, r vehicle.Report, _ time.Time) (any, time.Time) {
	s, at := r.Status, r.StatusAt
	leaked := math.Round(math.Mod(s.OdometerKm, leakRefill) / 100 * leakPer100km)
	rearLeft := recommendedKPa - leaked
	tyre := "TYRE_PRESSURE_WARNING_NO_WARNING"
	if rearLeft < recommendedKPa*0.9 {
		tyre = "TYRE_PRESSURE_WARNING_LOW"
	}
	const serviceKm = 30_000
	lights := map[string]any{}
	for _, name := range []string{
		"brakeLightCenter", "brakeLightLeft", "brakeLightRight", "daytimeRunningLightLeft",
		"daytimeRunningLightRight", "fogLightFront", "fogLightRear", "highBeamLeft", "highBeamRight",
		"lowBeamLeft", "lowBeamRight", "positionLightFrontLeft", "positionLightFrontRight",
		"positionLightRear", "registrationPlateLight", "turnIndicatorFrontLeft",
		"turnIndicatorFrontRight", "turnIndicatorRearLeft", "turnIndicatorRearRight",
	} {
		lights[name] = "EXTERIOR_LIGHT_WARNING_NO_WARNING"
	}
	return map[string]any{
		"timestamp":                       timestamp(at),
		"frontLeftTyrePressureKpa":        recommendedKPa + 2,
		"frontRightTyrePressureKpa":       recommendedKPa,
		"rearLeftTyrePressureKpa":         rearLeft,
		"rearRightTyrePressureKpa":        recommendedKPa - 1,
		"recommendedFrontTyrePressureKpa": recommendedKPa,
		"recommendedRearTyrePressureKpa":  recommendedKPa,
		"tyrePressureFrontLeftWarning":    "TYRE_PRESSURE_WARNING_NO_WARNING",
		"tyrePressureFrontRightWarning":   "TYRE_PRESSURE_WARNING_NO_WARNING",
		"tyrePressureRearLeftWarning":     tyre,
		"tyrePressureRearRightWarning":    "TYRE_PRESSURE_WARNING_NO_WARNING",
		"daysToService":                   300,
		"distanceToServiceKm":             int(serviceKm - math.Mod(s.OdometerKm, serviceKm)),
		"serviceWarning":                  "SERVICE_WARNING_NO_WARNING",
		"brakeFluidLevelWarning":          "BRAKE_FLUID_LEVEL_WARNING_NO_WARNING",
		"engineCoolantLevelWarning":       "ENGINE_COOLANT_LEVEL_WARNING_NO_WARNING",
		"oilLevelWarning":                 "OIL_LEVEL_WARNING_NO_WARNING",
		"washerFluidLevelWarning":         "WASHER_FLUID_LEVEL_WARNING_NO_WARNING",
		"lowVoltageBatteryWarning":        "LOW_VOLTAGE_BATTERY_WARNING_NO_WARNING",
		"lightWarnings":                   lights,
	}, at
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

const kmPerMile = 1.609344

// electricRange is the range the car forecasts at the SoC's instant, over the usable
// capacity the SoC runs over.
func electricRange(s vehicle.Spec, at time.Time, soc float64) int {
	return int(soc / 100 * s.UsableKWhAt(at) / s.ConsumptionKWhPer100km * 100)
}

func speed(km float64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return math.Round(km / d.Hours())
}

// omitZero sets key to v unless v is zero: the API writes protobuf's JSON, which
// leaves zeros out.
func omitZero(m map[string]any, key string, v float64) {
	if v != 0 {
		m[key] = v
	}
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }
