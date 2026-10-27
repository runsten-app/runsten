// Package teslaapi exposes simulated vehicles as Tesla's Owner API, the unofficial API of
// the Tesla app read with the owner's tokens: the refresh of a token, the account's
// products, a vehicle's state and its data. The response shapes reproduce the real
// responses of a Model Y (vehicle_data.json, anonymised).
//
// A car falls asleep when nothing keeps it awake, and no read wakes it up: asleep, it
// answers vehicle_data with a 408, as the real one. Faults are set on demand under
// ControlPrefix: a 429, the Owner API closed to the account, the refresh tokens revoked.
package teslaapi

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// The paths the handler serves. The real hosts serve them under
// https://owner-api.teslamotors.com and https://auth.tesla.com: the client is given the
// simulator's address for both.
const (
	// APIPrefix is the Owner API's.
	APIPrefix = "/api/1/"
	// AuthPrefix is the authentication's: the token endpoint is its token.
	AuthPrefix = "/oauth2/v3/"
	// ControlPrefix sets the faults, outside the API.
	ControlPrefix = "/sim/tesla/"
)

// Source provides the state of a simulated vehicle.
type Source interface {
	VIN() string
	// ReportAt gives the vehicle's spec, and what it uploaded (the instants of its last
	// activity).
	ReportAt(at time.Time) vehicle.Report
	// StateAt gives its actual state: an awake Tesla answers with the live one.
	StateAt(at time.Time) vehicle.State
}

// Behavior groups the assumptions about the car and the API.
type Behavior struct {
	// SleepAfter is how long after its last activity a car neither driving nor charging
	// falls asleep: a drive, a change of its charge, or a read of vehicle_data (a read of
	// the vehicle's state does not count). Unknown on the real car, to measure (ADR 0040
	// §8): TeslaMate leaves it 15 min. Zero: the car never sleeps.
	SleepAfter time.Duration
	// TokenTTL is the lifetime of an access token (8 h according to the community).
	TokenTTL time.Duration
}

// DefaultBehavior returns the assumptions used without explicit configuration.
func DefaultBehavior() Behavior {
	return Behavior{SleepAfter: 10 * time.Minute, TokenTTL: 8 * time.Hour}
}

// userID is the account's identifier in the responses.
const userID = 1111111111

// car is a vehicle of the account.
type car struct {
	src       Source
	id        uint64 // Tesla's identifier, which the Owner API reads it by
	vehicleID uint64
	lastRead  time.Time // the last vehicle_data answered, which keeps the car awake
}

// Handler serves the simulated API.
type Handler struct {
	clk      clock.Clock
	behavior Behavior
	cars     []*car
	byID     map[string]*car
	mux      *http.ServeMux

	mu          sync.Mutex
	used        map[string]bool // the refresh tokens consumed
	generation  int             // of the refresh tokens accepted, raised by a revocation
	rateLimited time.Time       // 429 until then
	closed      bool            // the Owner API closed to the account
}

// NewHandler creates the simulated API for the vehicles srcs, the vehicles of one
// account, with the simulated clock clk. Their VINs must be distinct (scenario.NewFleet
// checks it): NewHandler panics otherwise.
func NewHandler(srcs []Source, clk clock.Clock, behavior Behavior) *Handler {
	h := &Handler{clk: clk, behavior: behavior, byID: map[string]*car{}, mux: http.NewServeMux(), used: map[string]bool{}}
	vins := map[string]bool{}
	for _, src := range srcs {
		if vins[src.VIN()] {
			panic("teslaapi: duplicate VIN " + src.VIN())
		}
		vins[src.VIN()] = true
		c := &car{src: src, id: idOf(src.VIN(), "id"), vehicleID: idOf(src.VIN(), "vehicle_id")}
		h.cars = append(h.cars, c)
		h.byID[strconv.FormatUint(c.id, 10)] = c
	}
	api := http.NewServeMux()
	api.HandleFunc("GET "+APIPrefix+"products", h.products)
	api.HandleFunc("GET "+APIPrefix+"vehicles/{id}", h.vehicle)
	api.HandleFunc("GET "+APIPrefix+"vehicles/{id}/vehicle_data", h.vehicleData)
	api.HandleFunc(APIPrefix, notFound)

	h.mux.HandleFunc("POST "+AuthPrefix+"token", h.token)
	h.mux.Handle(APIPrefix, h.middleware(api))
	h.mux.HandleFunc("POST "+ControlPrefix+"rate-limit", h.setRateLimit)
	h.mux.HandleFunc("POST "+ControlPrefix+"owner-api", h.setClosed)
	h.mux.HandleFunc("POST "+ControlPrefix+"revoke", h.revoke)
	h.mux.HandleFunc("POST "+ControlPrefix+"refresh-token", h.issueRefreshToken)
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// idOf is a stable identifier of 16 digits, as Tesla's: some beyond float64's exact
// integers, which a client must read as written.
func idOf(vin, salt string) uint64 {
	sum := sha256.Sum256([]byte(vin + "|" + salt))
	return 1_000_000_000_000_000 + binary.BigEndian.Uint64(sum[:8])%9_000_000_000_000_000
}

// middleware checks the access token, then the faults set, of a call of /api/1.
func (h *Handler) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := h.clk.Now()
		if !h.authorized(r, now) {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		h.mu.Lock()
		closed, until := h.closed, h.rateLimited
		h.mu.Unlock()
		if closed {
			// The 403 of an account the Owner API is closed to (ADR 0040 §3).
			writeError(w, http.StatusForbidden, "forbidden, see https://developer.tesla.com/docs/fleet-api")
			return
		}
		if now.Before(until) {
			// Assumed: no 429 of the Owner API seen yet, nor its headers.
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(until.Sub(now).Seconds()))))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// asleepState is how /vehicles/{id} reports a car asleep: offline, as the maintainer's
// car does, the sleep and a lost network not told apart.
const asleepState = "offline"

// awake reports whether the car is awake at now, its last read of vehicle_data being
// lastRead.
func (h *Handler) awake(r vehicle.Report, st vehicle.State, lastRead, now time.Time) bool {
	if st.Driving || st.Charging == vehicle.Charging || h.behavior.SleepAfter <= 0 {
		return true
	}
	last := lastRead
	for _, t := range []time.Time{r.StatusAt, r.EnergyAt} {
		if t.After(last) {
			last = t
		}
	}
	return now.Sub(last) < h.behavior.SleepAfter
}

// look returns the car's report, state and whether it is awake at now; read marks a read
// of vehicle_data, which keeps an awake car awake.
func (h *Handler) look(c *car, now time.Time, read bool) (vehicle.Report, vehicle.State, bool) {
	r, st := c.src.ReportAt(now), c.src.StateAt(now)
	h.mu.Lock()
	defer h.mu.Unlock()
	awake := h.awake(r, st, c.lastRead, now)
	if awake && read {
		c.lastRead = now
	}
	return r, st, awake
}

func (h *Handler) products(w http.ResponseWriter, _ *http.Request) {
	now := h.clk.Now()
	out := make([]map[string]any, 0, len(h.cars))
	for _, c := range h.cars {
		_, _, awake := h.look(c, now, false)
		p := summary(c, awake)
		p["user_id"] = userID
		p["mobile_access_disabled"] = false
		p["cached_data"] = ""
		p["infotainment_public_key"] = ""
		p["vcsec_public_key"] = ""
		p["device_type"] = "vehicle"
		p["command_signing"] = "required"
		p["release_notes_supported"] = true
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"response": out, "count": len(out)})
}

// vehicle answers /vehicles/{id}: the car's state, read without waking it nor keeping it
// awake.
func (h *Handler) vehicle(w http.ResponseWriter, r *http.Request) {
	c, ok := h.byID[r.PathValue("id")]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	_, _, awake := h.look(c, h.clk.Now(), false)
	writeJSON(w, http.StatusOK, map[string]any{"response": summary(c, awake)})
}

// vehicleData answers /vehicles/{id}/vehicle_data: everything, when the car is awake; a
// 408 otherwise, without waking it.
func (h *Handler) vehicleData(w http.ResponseWriter, r *http.Request) {
	c, ok := h.byID[r.PathValue("id")]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	now := h.clk.Now()
	rep, st, awake := h.look(c, now, true)
	if !awake {
		writeError(w, http.StatusRequestTimeout, "vehicle unavailable: vehicle is offline or asleep")
		return
	}
	data, err := renderVehicleData(c, rep, st, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"response": data})
}

// summary is a vehicle as /vehicles/{id} and /products describe it.
func summary(c *car, awake bool) map[string]any {
	state := asleepState
	if awake {
		state = "online"
	}
	return map[string]any{
		"id": c.id, "id_s": strconv.FormatUint(c.id, 10), "vehicle_id": c.vehicleID, "vin": c.src.VIN(),
		"color": nil, "access_type": "OWNER", "display_name": "Demo", "option_codes": nil,
		"granular_access": map[string]bool{"hide_private": false}, "tokens": nil, "state": state,
		"in_service": false, "calendar_enabled": true, "api_version": 95, "backseat_token": nil,
		"backseat_token_updated_at": nil, "ble_autopair_enrolled": false,
	}
}

// setRateLimit answers 429 to every call of /api/1 for the duration ?for= (a Go duration,
// 0 to lift it), from now on the simulated clock.
func (h *Handler) setRateLimit(w http.ResponseWriter, r *http.Request) {
	d, err := time.ParseDuration(r.URL.Query().Get("for"))
	if err != nil || d < 0 {
		http.Error(w, "for: a duration ≥ 0 expected", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	h.rateLimited = h.clk.Now().Add(d)
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// setClosed closes the Owner API to the account (?closed=true), or opens it again.
func (h *Handler) setClosed(w http.ResponseWriter, r *http.Request) {
	closed, err := strconv.ParseBool(r.URL.Query().Get("closed"))
	if err != nil {
		http.Error(w, "closed: true or false expected", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	h.closed = closed
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// revoke refuses every refresh token issued so far, as a sign-out of the Tesla account
// would.
func (h *Handler) revoke(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	h.generation++
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// issueRefreshToken gives a new refresh token, as Tesla Auth gives the owner one.
func (h *Handler) issueRefreshToken(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"refresh_token": h.newRefreshToken()})
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not_found")
}

// writeError writes the error body of /api/1, as observed on a 408.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"response": nil, "error": msg, "error_description": ""})
}

// writeOAuthError writes the token endpoint's error (RFC 6749 §5.2).
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}
