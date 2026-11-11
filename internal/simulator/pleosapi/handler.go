// Package pleosapi exposes simulated vehicles through the Pleos Vehicle Data API, the
// access Hyundai, Kia and Genesis opened to private owners in Europe (the EU Data Act):
// a token asked with the owner's key, the key's vehicles (consent), and the status
// endpoints a logger reads (batteries, powertrains, locations, driving, status). The
// shapes are those of Pleos's documentation; none is checked on a real car yet.
//
// One key reads one brand's vehicles, named by the Brand header of every call; the
// development key is DevCredential. Only one token lives at a time per key: asking for
// another has the previous answered with 403 from then, as observed on the real API.
// Faults are set on demand under ControlPrefix: the key revoked, a vehicle's consent
// withdrawn, an endpoint without data, calls answered 429.
package pleosapi

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// Prefix is the path the handler serves, standing in for https://api.pleos.ai: the
// collector and runsten-api are pointed at the simulator's own address for it.
const Prefix = "/pleos/"

// ControlPrefix sets the faults, outside the API.
const ControlPrefix = "/sim/pleos/"

// Credential is the key the owner creates in the Pleos Playground: a client ID and its
// secret, one key per brand.
type Credential struct {
	ClientID, ClientSecret string
}

// DevClientID is the client ID of the key the simulator accepts by default, development
// only: connect-sim and the Compose stack give it to runsten-api.
const DevClientID = "runsten-dev"

// DevCredential returns the key the simulator accepts by default, development only.
func DevCredential() Credential {
	return Credential{ //nolint:gosec // the simulator's, never a real one
		ClientID:     DevClientID,
		ClientSecret: "runsten-dev-secret",
	}
}

// The brands the API reads, as their header names them.
const (
	Hyundai = "hyundai"
	Kia     = "kia"
	Genesis = "genesis"
)

// Brands lists the brands the API reads.
func Brands() []string { return []string{Hyundai, Kia, Genesis} }

// Behavior groups the assumptions about the cars and the API, none of them published by
// Pleos: to measure on a real one (ADR 0049 §10).
type Behavior struct {
	// SendEvery is how often a car sends while it drives or charges; parked, it sends
	// only at a change (observed once: a car parked overnight that uploaded nothing,
	// ADR 0049 §10.1). Zero: only at a change.
	SendEvery time.Duration
	// TokenTTL is the lifetime of the tokens the API issues: their body carries no
	// expiry (the client reads it in the token's JWT); 3,600 s has been observed.
	TokenTTL time.Duration
}

// DefaultBehavior returns the assumptions used without explicit configuration.
func DefaultBehavior() Behavior { return Behavior{SendEvery: 5 * time.Minute, TokenTTL: time.Hour} }

// Endpoints lists the status endpoints a logger reads.
func Endpoints() []string {
	return []string{"batteries", "powertrains", "locations", "driving", "status"}
}

// Source provides the uploaded state of a simulated vehicle.
type Source interface {
	VIN() string
	// Brand is the scenario's: hyundai, kia or genesis, the brand of the key that
	// reads the vehicle.
	Brand() string
	ReportAt(at time.Time) vehicle.Report
}

// token is one the API issued for the key: the brand it reads and when it ends.
type token struct {
	value, brand string
	until        time.Time
}

// Handler serves the simulated API.
type Handler struct {
	clk      clock.Clock
	behavior Behavior
	srcs     []Source          // in their order
	byVIN    map[string]Source // by VIN
	cred     Credential
	mux      *http.ServeMux

	mu        sync.Mutex
	refused   bool            // the key revoked: /auth answers 40404
	live      *token          // the one token living for the key
	withdrawn map[string]bool // the VINs whose consent is withdrawn
	noData    map[string]bool // the endpoints with no data; "": every one of them
	rateUntil time.Time       // calls answered 429 until, with Retry-After
}

// NewHandler creates the simulated API for the vehicles srcs, read with the key cred and
// the simulated clock clk. Their VINs must be distinct (scenario.NewFleet checks it):
// NewHandler panics otherwise.
func NewHandler(srcs []Source, clk clock.Clock, cred Credential, behavior Behavior) *Handler {
	h := &Handler{
		clk: clk, behavior: behavior, srcs: srcs, byVIN: make(map[string]Source, len(srcs)),
		cred: cred, withdrawn: map[string]bool{}, noData: map[string]bool{},
	}
	for _, src := range srcs {
		if _, dup := h.byVIN[src.VIN()]; dup {
			panic("pleosapi: duplicate VIN " + src.VIN())
		}
		h.byVIN[src.VIN()] = src
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+Prefix+"v1/auth/personal-token", h.personalToken)
	mux.HandleFunc("GET "+Prefix+"v1/vehicles/consent", h.consent)
	for _, ep := range Endpoints() {
		mux.HandleFunc("GET "+Prefix+"v1/vehicles/{vin}/"+ep, h.endpoint(ep))
	}
	mux.HandleFunc("POST "+ControlPrefix+"revoke", h.setRevoke)
	mux.HandleFunc("POST "+ControlPrefix+"withdraw", h.setWithdraw)
	mux.HandleFunc("POST "+ControlPrefix+"no-data", h.setNoData)
	mux.HandleFunc("POST "+ControlPrefix+"rate-limit", h.setRateLimit)
	h.mux = mux
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// personalToken answers POST /v1/auth/personal-token: a token of the key, reading the
// brand of the Brand header. Asking for another answers the previous with 403 from then:
// on the real API, only one token lives at a time per key (observed; the research §1.8).
func (h *Handler) personalToken(w http.ResponseWriter, r *http.Request) {
	now := h.clk.Now()
	var req struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if decodeJSON(w, r, &req, now) {
		return
	}
	brand := r.Header.Get("Brand")
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case req.ClientID == "" || req.ClientSecret == "" || brand == "" ||
		!sameFold(brand, Hyundai, Kia, Genesis):
		// A missing parameter, among them the Brand header: the documented answer,
		// its values the doc's.
		writeError(w, now, http.StatusBadRequest, "40001", "Invalid Parameters")
	case h.refused:
		// The key revoked, or reissued: the documented answer, user or key not found.
		writeError(w, now, http.StatusNotFound, "40404", "User or key not found")
	case req.ClientID != h.cred.ClientID || req.ClientSecret != h.cred.ClientSecret:
		// A refused secret has been observed as a 400, its body unknown: answered
		// with the documented key-not-found error (assumption).
		writeError(w, now, http.StatusNotFound, "40404", "User or key not found")
	default:
		until := now.Add(h.behavior.TokenTTL)
		h.live = &token{value: jwt(brand, until), brand: brand, until: until}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"accessToken": h.live.value},
			"meta": meta(now),
		})
	}
}

// consent answers the vehicles of the key, those of the token's brand. A vehicle
// deactivated later leaves the list; here it stays, the faults of a vehicle being its
// data endpoints' alone.
func (h *Handler) consent(w http.ResponseWriter, r *http.Request) {
	now := h.clk.Now()
	if h.check(w, r, "", "", now) == nil {
		return
	}
	h.mu.Lock()
	brand := h.live.brand
	h.mu.Unlock()
	vehicles := []map[string]any{}
	for _, src := range h.srcs {
		if src.Brand() != brand {
			continue
		}
		spec := src.ReportAt(now).Spec
		t := "EV"
		if spec.PlugInHybrid {
			t = "PE"
		}
		// The vehicle's name (its model code) and its nickname are the model's here:
		// their real forms are to check on a tester's car (ADR 0049 §10.11). The
		// sales name is what recognizes the family.
		vehicles = append(vehicles, map[string]any{
			"vin": src.VIN(), "nickname": spec.Model, "name": spec.Model,
			"type": t, "sellname": spec.Model,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"vehicles": vehicles}, "meta": meta(now)})
}

// endpoint serves one status endpoint of one vehicle: its data dated from the car's
// last send, which happens at each change of its group, and every SendEvery while the
// car drives or charges.
func (h *Handler) endpoint(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := h.clk.Now()
		vin := r.PathValue("vin")
		if h.check(w, r, vin, name, now) == nil {
			return
		}
		src := h.byVIN[vin]
		rep := src.ReportAt(now)
		active := rep.Status.Driving || rep.Energy.Charging == vehicle.Charging
		writeJSON(w, http.StatusOK, map[string]any{
			"data": dataset(name, h.behavior, rep, active, now), "meta": meta(now),
		})
	}
}

// check lets a call of the API through (consent and the status endpoints alike) or
// answers its refusal: the 429 first, then the token, its brand, the vehicle, and the
// faults set on demand. It returns the token the call reads with.
func (h *Handler) check(w http.ResponseWriter, r *http.Request, vin, ep string, now time.Time) *token {
	if !h.rateUntil.IsZero() && now.Before(h.rateUntil) {
		// No 429 is documented: its body is assumed, its header what its clients
		// honour.
		w.Header().Set("Retry-After", retryAfter(h.rateUntil, now))
		writeError(w, now, http.StatusTooManyRequests, "429", "Too Many Requests")
		return nil
	}
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	h.mu.Lock()
	defer h.mu.Unlock()
	live := h.live
	switch {
	case tok == "":
		// No Authorization header: the documented answer, its code the doc's.
		writeError(w, now, http.StatusBadRequest, "40002", "Missing Authorization Header")
		return nil
	case live == nil || tok != live.value:
		// A token asked after this one has taken its place, or one never issued: the
		// 403 observed on the real API, its code a number (the research §1.8).
		writeError(w, now, http.StatusForbidden, 403, "Forbidden")
		return nil
	case now.After(live.until):
		// Expired: the documented answer, its code the doc's.
		writeError(w, now, http.StatusBadRequest, "40003", "Token expired")
		return nil
	case r.Header.Get("Brand") != live.brand:
		// The token, the brand and the VIN must belong together: the documented
		// forbidden, the answer of a brand the token does not read being assumed the
		// same (its own is not published).
		writeError(w, now, http.StatusBadRequest, "403", "Forbidden")
		return nil
	case vin != "" && (h.byVIN[vin] == nil || h.byVIN[vin].Brand() != live.brand):
		// Another brand's VIN, or one the key does not read: its own refusal is not
		// published either, and the forbidden is assumed (assumption).
		writeError(w, now, http.StatusBadRequest, "403", "Forbidden")
		return nil
	case vin != "" && h.withdrawn[vin]:
		// The consent the owner withdrew: the documented answer, no permission.
		writeError(w, now, http.StatusBadRequest, "5006", "No Permission")
		return nil
	case ep != "" && (h.noData[""] || h.noData[ep]):
		// No data for the endpoint (an unknown VIN aside): the documented answer, no
		// data, its code the doc's.
		writeError(w, now, http.StatusNotFound, "4045", "No Data")
		return nil
	}
	return live
}

// sameFold reports whether s equals any of the values, without case.
func sameFold(s string, values ...string) bool {
	for _, v := range values {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}
