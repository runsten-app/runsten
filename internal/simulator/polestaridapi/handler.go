// Package polestaridapi exposes simulated vehicles as the Polestar app's API, read with
// the owner's Polestar ID (ADR 0042): the Polestar ID's sign-in (PingFederate's form, the
// authorization code with PKCE, the refresh token rotated at each refresh) and the
// GraphQL of mystar-v2 (the account's cars, their telematics). The sign-in reproduces
// what was checked on a real Polestar ID; the GraphQL's shapes are pypolestar's, no real
// response of a car having been seen yet.
//
// Everything is under Prefix: the simulator serves the Volvo ID on the same paths as the
// Polestar ID's (/as/…). A revocation of the refresh tokens is set on demand under
// ControlPrefix.
package polestaridapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// The paths the handler serves. The real hosts serve them at the root of
// https://polestarid.eu.polestar.com (the sign-in) and under
// https://pc-api.polestar.com/eu-north-1/mystar-v2/ (the GraphQL): the client is given
// the simulator's address with these paths for both.
const (
	// Prefix is the simulated Polestar ID's issuer, which every path is under.
	Prefix = "/polestar-id/"
	// APIPath is the GraphQL's.
	APIPath = Prefix + "eu-north-1/mystar-v2/"
	// ControlPrefix sets the faults, outside the API.
	ControlPrefix = "/sim/polestar-id/"
)

// Source provides the uploaded state of a simulated vehicle.
type Source interface {
	VIN() string
	ReportAt(at time.Time) vehicle.Report
}

// Behavior groups the assumptions about the sign-in and the car.
type Behavior struct {
	// AccessTokenTTL is the lifetime of an access token: 300 s on the real one.
	AccessTokenTTL time.Duration
	// CodeTTL is how long an authorization code waits for its exchange: unknown, a
	// minute assumed (the real one still exchanged one pasted within a minute).
	CodeTTL time.Duration
	// ParkedUpload is how often a car that is not driving uploads its battery, unchanged:
	// unknown, the Data Portal's 10 minutes assumed.
	ParkedUpload time.Duration
}

// DefaultBehavior returns the assumptions used without explicit configuration.
func DefaultBehavior() Behavior {
	return Behavior{AccessTokenTTL: 300 * time.Second, CodeTTL: time.Minute, ParkedUpload: 10 * time.Minute}
}

// Handler serves the simulated API.
type Handler struct {
	vins     []string          // in the order of the sources
	srcs     map[string]Source // by VIN
	clk      clock.Clock
	behavior Behavior
	mux      *http.ServeMux

	mu         sync.Mutex
	flows      map[string]flow  // sign-ins waiting for their form
	codes      map[string]grant // codes waiting for their exchange
	used       map[string]bool  // the refresh tokens consumed
	generation int              // of the refresh tokens accepted, raised by a revocation
}

// NewHandler creates the simulated API for the vehicles srcs, the cars of one Polestar
// ID, with the simulated clock clk. Their VINs must be distinct (scenario.NewFleet checks
// it): NewHandler panics otherwise.
func NewHandler(srcs []Source, clk clock.Clock, behavior Behavior) *Handler {
	h := &Handler{
		srcs: map[string]Source{}, clk: clk, behavior: behavior, mux: http.NewServeMux(),
		flows: map[string]flow{}, codes: map[string]grant{}, used: map[string]bool{},
	}
	for _, src := range srcs {
		if _, dup := h.srcs[src.VIN()]; dup {
			panic("polestaridapi: duplicate VIN " + src.VIN())
		}
		h.srcs[src.VIN()] = src
		h.vins = append(h.vins, src.VIN())
	}
	h.mux.HandleFunc("GET "+Prefix+".well-known/openid-configuration", h.openIDConfiguration)
	h.mux.HandleFunc("GET "+Prefix+"as/authorization.oauth2", h.authorize)
	h.mux.HandleFunc("POST "+Prefix+"as/{flow}/resume/as/authorization.ping", h.resume)
	h.mux.HandleFunc("POST "+Prefix+"as/token.oauth2", h.token)
	h.mux.HandleFunc("POST "+APIPath, h.graphql)
	h.mux.HandleFunc("POST "+ControlPrefix+"revoke", h.revoke)
	h.mux.HandleFunc("POST "+ControlPrefix+"refresh-token", h.issueRefreshToken)
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// graphql answers the two queries pypolestar sends, recognized by their field:
// getConsumerCarsV2 and carTelematicsV2(vins: $vins). Another query is an error, as a
// GraphQL server answers a field it does not have.
func (h *Handler) graphql(w http.ResponseWriter, r *http.Request) {
	now := h.clk.Now()
	if !h.authorized(r, now) {
		// Assumed: the real answer to an expired token is not seen yet.
		writeJSON(w, http.StatusUnauthorized, gqlErrors("Unauthorized"))
		return
	}
	var req struct {
		Query     string `json:"query"`
		Variables struct {
			VINs []string `json:"vins"`
		} `json:"variables"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, gqlErrors("The request body is not valid JSON."))
		return
	}
	switch {
	case strings.Contains(req.Query, "getConsumerCarsV2"):
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"getConsumerCarsV2": h.cars(now)}})
	case strings.Contains(req.Query, "carTelematicsV2"):
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"carTelematicsV2": h.telematics(req.Variables.VINs, now)}})
	default:
		writeJSON(w, http.StatusOK, gqlErrors("Validation error: unknown field"))
	}
}

// cars are the account's cars as getConsumerCarsV2 lists them: the model's name without
// its space ("Polestar4", as pypolestar reads it), the model year in a string.
func (h *Handler) cars(now time.Time) []map[string]any {
	out := make([]map[string]any, 0, len(h.vins))
	for i, vin := range h.vins {
		s := h.srcs[vin].ReportAt(now).Spec
		out = append(out, map[string]any{
			"vin":                       vin,
			"internalVehicleIdentifier": fmt.Sprintf("%08d", i+1),
			"registrationNo":            "",
			"modelYear":                 strconv.Itoa(s.ModelYear),
			"modelName":                 strings.ReplaceAll(s.Model, " ", ""),
			"pno34":                     "",
			"structureWeek":             "",
		})
	}
	return out
}

// telematics answers carTelematicsV2 for the VINs asked that the account has: a list of
// each kind, a car each.
func (h *Handler) telematics(vins []string, now time.Time) map[string]any {
	health, battery, odometer := []any{}, []any{}, []any{}
	for _, vin := range vins {
		src, ok := h.srcs[vin]
		if !ok {
			continue
		}
		r := src.ReportAt(now)
		battery = append(battery, h.renderBattery(vin, r, now))
		odometer = append(odometer, map[string]any{
			"vin": vin, "odometerMeters": int(math.Round(r.Status.OdometerKm * 1000)), "timestamp": timestamp(r.StatusAt),
		})
		health = append(health, renderHealth(vin, r))
	}
	return map[string]any{"health": health, "battery": battery, "odometer": odometer}
}

// renderBattery reports the battery as pypolestar reads it: the charging status in two
// values only (CHARGING_STATUS_V2_CHARGING, …_IDLE), no cable nor power.
func (h *Handler) renderBattery(vin string, r vehicle.Report, now time.Time) map[string]any {
	e := r.Energy
	at := r.EnergyAt
	if every := h.behavior.ParkedUpload; every > 0 && !r.Status.Driving && !now.Before(at) {
		at = at.Add(now.Sub(at) / every * every)
	}
	status, minutes := "IDLE", 0
	if e.Charging == vehicle.Charging {
		status = "CHARGING"
		if e.ChargingPowerW > 0 {
			kwh := (e.TargetSoC - e.SoC) / 100 * r.Spec.UsableKWhAt(at)
			minutes = int(math.Ceil(kwh / (e.ChargingPowerW / 1000) * 60))
		}
	}
	return map[string]any{
		"vin":                                vin,
		"batteryChargeLevelPercentage":       int(e.SoC),
		"chargingStatusV2":                   "CHARGING_STATUS_V2_" + status,
		"estimatedChargingTimeToFullMinutes": minutes,
		"estimatedDistanceToEmptyKm":         int(e.SoC / 100 * r.Spec.UsableKWhAt(at) / r.Spec.ConsumptionKWhPer100km * 100),
		"timestamp":                          timestamp(at),
	}
}

// renderHealth reports the service and the warnings, uploaded with the status.
func renderHealth(vin string, r vehicle.Report) map[string]any {
	const serviceKm = 30_000
	return map[string]any{
		"vin":                       vin,
		"brakeFluidLevelWarning":    "BRAKE_FLUID_LEVEL_WARNING_NO_WARNING",
		"daysToService":             300,
		"distanceToServiceKm":       int(serviceKm - math.Mod(r.Status.OdometerKm, serviceKm)),
		"engineCoolantLevelWarning": "ENGINE_COOLANT_LEVEL_WARNING_NO_WARNING",
		"oilLevelWarning":           "OIL_LEVEL_WARNING_NO_WARNING",
		"serviceWarning":            "SERVICE_WARNING_NO_WARNING",
		"timestamp":                 timestamp(r.StatusAt),
	}
}

// timestamp is protobuf's Timestamp as the API writes it: the seconds in a string.
func timestamp(at time.Time) map[string]any {
	return map[string]any{"seconds": strconv.FormatInt(at.Unix(), 10), "nanos": at.Nanosecond()}
}

// revoke refuses every refresh token issued so far, as a sign-out of the Polestar ID or a
// new password would (assumed).
func (h *Handler) revoke(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	h.generation++
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// issueRefreshToken gives a new refresh token, as an exchange of a code would: the
// development's shortcut past the sign-in.
func (h *Handler) issueRefreshToken(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"refresh_token": h.newRefreshToken()})
}

func gqlErrors(msg string) map[string]any {
	return map[string]any{"errors": []map[string]any{{"message": msg}}}
}

// writeOAuthError writes the token endpoint's error (RFC 6749 §5.2), as the real one does.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}
