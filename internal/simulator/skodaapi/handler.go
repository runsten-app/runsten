// Package skodaapi exposes simulated vehicles through the MyŠkoda Public API, the access
// Škoda opened to its owners: one endpoint, GET /api/v1/vehicles/{vin}, read with a key
// the owner creates in the MyŠkoda app (the X-API-Key header). The shapes are those of
// the API's contract (OpenAPI 1.1.0) and documentation; none is checked on a real car yet.
//
// The key is DevAPIKey, covering every vehicle of the scenarios of brand skoda; it expires
// (X-API-Key-Expires-At on every success). The calls are limited per VIN, 20 an hour by
// default, with the RateLimit-* headers on every authenticated response, a 5xx counted, a
// 401 or a 403 not, a 429 not prolonging the window. Faults are set on demand under
// ControlPrefix: the key expired, a vehicle no longer covered, a part unavailable, an
// outage answered 503.
package skodaapi

import (
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// Prefix is the path the handler serves, standing in for
// https://public.api.connect.skoda-auto.cz: the collector and runsten-api are pointed at
// the simulator's own address for it.
const Prefix = "/skoda/"

// ControlPrefix sets the faults, outside the API.
const ControlPrefix = "/sim/skoda/"

// DevAPIKey is the key the simulator accepts by default, development only: connect-sim
// and the Compose stack give it to runsten-api.
const DevAPIKey = "runsten-dev-skoda-key" //nolint:gosec // the simulator's, never a real one

// problemBase is the base of the problem types, as the real API writes them.
const problemBase = "https://public.api.connect.skoda-auto.cz/problems/"

// Behavior groups the assumptions about the cars and the API.
type Behavior struct {
	// PerHour is the calls a VIN is allowed an hour (the docs' 20, "not final"); zero
	// or less: unlimited, without the headers.
	PerHour int
	// KeyLifetime is how long the key reads from the simulator's start: the API tells
	// the expiry, not how it is chosen in the app (assumption: a year).
	KeyLifetime time.Duration
	// SendEvery is how often a car sends its state while it drives or charges; parked,
	// only at a change (assumption, to measure on a real car). Zero: only at a change.
	SendEvery time.Duration
}

// DefaultBehavior returns the assumptions used without explicit configuration.
func DefaultBehavior() Behavior {
	return Behavior{PerHour: 20, KeyLifetime: 365 * 24 * time.Hour, SendEvery: 5 * time.Minute}
}

// Parts are the values of include the API accepts, in the order of its contract.
func Parts() []string {
	return []string{
		"info", "status", "fuelStatus", "odometer", "parkingPosition", "airConditioning",
		"auxiliaryHeating", "activeVentilation", "charging", "chargingProfiles", "operations",
	}
}

// Source provides the uploaded state of a simulated vehicle.
type Source interface {
	VIN() string
	ReportAt(at time.Time) vehicle.Report
}

// window is a VIN's quota: the calls counted since it opened.
type window struct {
	start time.Time
	used  int
}

// Handler serves the simulated API.
type Handler struct {
	clk      clock.Clock
	behavior Behavior
	key      string
	byVIN    map[string]Source
	vins     []string // in their order
	mux      *http.ServeMux

	mu          sync.Mutex
	expiresAt   time.Time
	uncovered   map[string]bool   // the VINs the key no longer covers
	unavailable map[string]bool   // the parts that could not be read
	outageUntil time.Time         // answered 503 until
	windows     map[string]window // by VIN
}

// NewHandler creates the simulated API for the vehicles srcs, read with key and the
// simulated clock clk. Their VINs must be distinct (scenario.NewFleet checks it):
// NewHandler panics otherwise.
func NewHandler(srcs []Source, clk clock.Clock, key string, behavior Behavior) *Handler {
	h := &Handler{
		clk: clk, behavior: behavior, key: key, byVIN: make(map[string]Source, len(srcs)),
		expiresAt: clk.Now().Add(behavior.KeyLifetime).Truncate(time.Second),
		uncovered: map[string]bool{}, unavailable: map[string]bool{}, windows: map[string]window{},
	}
	for _, src := range srcs {
		if _, dup := h.byVIN[src.VIN()]; dup {
			panic("skodaapi: duplicate VIN " + src.VIN())
		}
		h.byVIN[src.VIN()] = src
		h.vins = append(h.vins, src.VIN())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+Prefix+"api/v1/vehicles/{vin}", h.vehicle)
	mux.HandleFunc("POST "+ControlPrefix+"expire", h.setExpire)
	mux.HandleFunc("POST "+ControlPrefix+"uncover", h.setUncover)
	mux.HandleFunc("POST "+ControlPrefix+"unavailable", h.setUnavailable)
	mux.HandleFunc("POST "+ControlPrefix+"outage", h.setOutage)
	mux.HandleFunc("GET "+ControlPrefix+"vehicles", h.vehicles)
	h.mux = mux
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// vehicle answers GET /api/v1/vehicles/{vin}: the key first (401, 403, which count in no
// quota), then the quota (429, which does not prolong the window), the VIN, the include,
// an outage; then the vehicle, its parts dated from their last send.
func (h *Handler) vehicle(w http.ResponseWriter, r *http.Request) {
	now := h.clk.Now()
	vin := r.PathValue("vin")
	h.mu.Lock()
	defer h.mu.Unlock()
	switch key := r.Header.Get("X-API-Key"); {
	case key == "" || key != h.key:
		writeProblem(w, http.StatusUnauthorized, "about:blank", "Unauthorized", "The API key is missing or invalid.")
		return
	case !now.Before(h.expiresAt):
		writeProblem(w, http.StatusUnauthorized, problemBase+"api-key-expired", "API key expired",
			"The API key has expired. Create or update a key in the MyŠkoda app.")
		return
	case len(vin) != 17 || h.byVIN[vin] == nil || h.uncovered[vin]:
		// A VIN the key does not cover: the documented 403, whatever the reason.
		writeProblem(w, http.StatusForbidden, problemBase+"api-key-not-authorized", "API key not authorized",
			"The API key is not authorized to execute the operation.")
		return
	}
	if !h.count(w, vin, now) {
		return
	}
	parts, explicit, bad := includeOf(r.URL.Query()["include"])
	switch {
	case bad != "":
		writeProblem(w, http.StatusBadRequest, "about:blank", "Bad Request", "Unknown include value: "+bad)
		return
	case now.Before(h.outageUntil):
		writeProblem(w, http.StatusServiceUnavailable, "about:blank", "Service Unavailable", "Try again later.")
		return
	}
	rep := h.byVIN[vin].ReportAt(now)
	w.Header().Set("X-API-Key-Expires-At", h.expiresAt.UTC().Format(time.RFC3339))
	writeJSON(w, http.StatusOK, response(vin, rep, parts, explicit, h.unavailable, h.behavior, now))
}

// count counts the call in the VIN's window and writes its RateLimit-* headers, or
// answers 429 when the window is spent; it reports whether the call goes on. A window
// opens at the first call past the previous one's end.
func (h *Handler) count(w http.ResponseWriter, vin string, now time.Time) bool {
	if h.behavior.PerHour <= 0 {
		return true
	}
	win := h.windows[vin]
	if win.start.IsZero() || !now.Before(win.start.Add(time.Hour)) {
		win = window{start: now}
	}
	// Rounded up: a wait left is never told as none.
	reset := strconv.Itoa(int(math.Ceil(win.start.Add(time.Hour).Sub(now).Seconds())))
	w.Header().Set("RateLimit-Limit", strconv.Itoa(h.behavior.PerHour))
	w.Header().Set("RateLimit-Reset", reset)
	if win.used >= h.behavior.PerHour {
		h.windows[vin] = win
		w.Header().Set("RateLimit-Remaining", "0")
		w.Header().Set("Retry-After", reset)
		writeProblem(w, http.StatusTooManyRequests, problemBase+"rate-limit-exceeded", "Rate limit exceeded",
			"The rate limit for the API key has been exceeded.")
		return false
	}
	win.used++
	h.windows[vin] = win
	w.Header().Set("RateLimit-Remaining", strconv.Itoa(h.behavior.PerHour-win.used))
	return true
}

// includeOf reads the include parameter, comma-separated or repeated; every part when
// none, explicit false. It returns the first unknown value, refused with a 400.
func includeOf(values []string) (parts map[string]bool, explicit bool, bad string) {
	parts = map[string]bool{}
	for _, v := range values {
		for p := range strings.SplitSeq(v, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !slices.Contains(Parts(), p) {
				return nil, false, p
			}
			parts[p] = true
		}
	}
	if len(parts) > 0 {
		return parts, true, ""
	}
	for _, p := range Parts() {
		parts[p] = true
	}
	return parts, false, ""
}
