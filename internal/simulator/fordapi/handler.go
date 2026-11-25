// Package fordapi exposes simulated vehicles through FordConnect Query, the read-only API
// of Ford's developer portal: the sign-in the owner is sent to (auth/init, which here
// consents at once and comes back with a code), the token endpoint (the code and the
// refresh token exchanged with the application's ID and secret), and the two endpoints
// the collector reads, the garage and the telemetry. The shapes are those of Ford's
// specification (FCON Query service 1.0.0) and of what other clients captured; none is
// checked on a real car yet.
//
// The application is DevApplication. A grant reads one car, as Ford's: each sign-in is
// given the next vehicle of the scenarios of brand ford. Two reads of a grant closer than
// Behavior.MinGap are answered 429, as reported of the real API; the token endpoint,
// another service behind Ford's gateway, is not held to it. Faults are set on demand
// under ControlPrefix: a grant withdrawn, the sign-in's outage, a limit of calls.
package fordapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// Prefix is the path the handler serves, standing in for https://api.vehicle.ford.com:
// the sign-in at Prefix+"auth/init", the token endpoint at Prefix+"token", the API under
// Prefix+"v1/". The collector and runsten-api are pointed at the simulator's own address
// for each.
const Prefix = "/ford/"

// ControlPrefix sets the faults, outside the API.
const ControlPrefix = "/sim/ford/"

// Application is an application of the developer portal.
type Application struct {
	ClientID, ClientSecret string
}

// DevApplication is the application the simulator accepts by default, development only:
// it stands for the one an owner creates on Ford's developer portal, and is the one to
// sign in with against the simulator.
func DevApplication() Application {
	return Application{ClientID: "runsten-dev-ford", ClientSecret: "runsten-dev-ford-secret"} //nolint:gosec // the simulator's, never a real one
}

// Behavior groups the assumptions about the cars and the service.
type Behavior struct {
	// AccessTTL is how long an access reads (other clients report 20 minutes).
	AccessTTL time.Duration
	// GrantLife is how long a sign-in refreshes (Ford's answer to a user: a year).
	GrantLife time.Duration
	// Rotate has each refresh give a new refresh token, the previous one no longer
	// accepted (assumption: Azure AD B2C's default).
	Rotate bool
	// MinGap is the least time between two reads of the API by a grant; a read sooner
	// is answered 429 (assumption: reports tell of a 429 for two calls close together,
	// none of the token endpoint's). Zero: no limit.
	MinGap time.Duration
	// SendEvery is how often a car sends its state while it drives or charges; parked,
	// only at a change (assumption, to measure on a real car). Zero: only at a change.
	SendEvery time.Duration
}

// DefaultBehavior returns the assumptions used without explicit configuration.
func DefaultBehavior() Behavior {
	return Behavior{
		AccessTTL: 20 * time.Minute, GrantLife: 365 * 24 * time.Hour, Rotate: true,
		MinGap: 10 * time.Second, SendEvery: time.Minute,
	}
}

// Source provides the uploaded state of a simulated vehicle.
type Source interface {
	VIN() string
	ReportAt(at time.Time) vehicle.Report
}

// codeLife is how long an authorization code is exchanged after the sign-in
// (assumption: minutes, as OAuth asks of a code).
const codeLife = 10 * time.Minute

// grant is a sign-in: the car it reads, when the owner consented, and its tokens.
type grant struct {
	vin        string
	authAt     time.Time
	redirect   string
	refresh    string
	access     string
	accessEnds time.Time
	lastCall   time.Time
	revoked    bool
}

// Handler serves the simulated service.
type Handler struct {
	clk      clock.Clock
	behavior Behavior
	app      Application
	byVIN    map[string]Source
	vins     []string // in their order
	mux      *http.ServeMux

	mu           sync.Mutex
	codes        map[string]*grant // by authorization code, used once, for codeLife
	grants       []*grant
	sent         map[string]sends // by VIN: the latest sends a read was told
	next         int              // the vehicle the next sign-in is given
	outageUntil  time.Time        // the token endpoint fails until
	limitedUntil time.Time        // the API answers 429 until
}

// NewHandler creates the simulated service for the vehicles srcs, with the application
// app and the simulated clock clk. Their VINs must be distinct (scenario.NewFleet checks
// it): NewHandler panics otherwise.
func NewHandler(srcs []Source, clk clock.Clock, app Application, behavior Behavior) *Handler {
	h := &Handler{
		clk: clk, behavior: behavior, app: app, byVIN: make(map[string]Source, len(srcs)),
		codes: map[string]*grant{}, sent: map[string]sends{},
	}
	for _, src := range srcs {
		if _, dup := h.byVIN[src.VIN()]; dup {
			panic("fordapi: duplicate VIN " + src.VIN())
		}
		h.byVIN[src.VIN()] = src
		h.vins = append(h.vins, src.VIN())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+Prefix+"auth/init", h.authorize)
	mux.HandleFunc("POST "+Prefix+"token", h.token)
	mux.HandleFunc("GET "+Prefix+"v1/garage", h.api(h.garage))
	mux.HandleFunc("GET "+Prefix+"v1/telemetry", h.api(h.telemetry))
	mux.HandleFunc("POST "+ControlPrefix+"revoke", h.setRevoke)
	mux.HandleFunc("POST "+ControlPrefix+"outage", h.setOutage)
	mux.HandleFunc("POST "+ControlPrefix+"limit", h.setLimit)
	mux.HandleFunc("GET "+ControlPrefix+"vehicles", h.vehicles)
	h.mux = mux
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// redirectAllowed applies Ford's rule to a redirect address: https, or http on
// localhost.
func redirectAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return false
}

// authorize answers the sign-in: the real page has the owner sign in to Ford, choose a
// car and consent; here the consent is given at once, for the next vehicle, and the
// browser sent back to the redirect address with a code and the state.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	switch {
	case q.Get("client_id") != h.app.ClientID:
		http.Error(w, "Unknown application.", http.StatusBadRequest)
		return
	case q.Get("response_type") != "code":
		http.Error(w, "Unsupported response type.", http.StatusBadRequest)
		return
	case !redirectAllowed(redirect):
		http.Error(w, "The redirect address must be in https, or in http on localhost.", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	if len(h.vins) == 0 {
		h.mu.Unlock()
		http.Error(w, "No vehicle in this account.", http.StatusBadRequest)
		return
	}
	now := h.clk.Now()
	// The codes of the sign-ins left there go.
	for c, g := range h.codes {
		if !now.Before(g.authAt.Add(codeLife)) {
			delete(h.codes, c)
		}
	}
	vin := h.vins[h.next%len(h.vins)]
	h.next++
	code := rand.Text()
	h.codes[code] = &grant{vin: vin, authAt: now, redirect: redirect}
	h.mu.Unlock()

	u, _ := url.Parse(redirect) // parsed by redirectAllowed
	back := u.Query()
	back.Set("code", code)
	if state := q.Get("state"); state != "" {
		back.Set("state", state)
	}
	u.RawQuery = back.Encode()
	// Back to the application's redirect address, as an authorization server does.
	http.Redirect(w, r, u.String(), http.StatusFound) //nolint:gosec // the simulator's sign-in: the address is the request's own
}

// token answers the token endpoint: a code or a refresh token exchanged, the
// application's ID and secret in the form.
func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "AADB2C90083: The request is missing required parameter.")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.clk.Now()
	switch {
	case now.Before(h.limitedUntil):
		w.Header().Set("Retry-After", retryAfter(h.limitedUntil.Sub(now)))
		writeOAuthError(w, http.StatusTooManyRequests, "", "Too Many Requests")
		return
	case now.Before(h.outageUntil):
		// What the real endpoint answered every account during an outage of the sign-in.
		writeOAuthError(w, http.StatusBadRequest, "access_denied",
			"AADB2C90027: Basic credentials specified for 'RESTAcquireAccessToken' are invalid. Check that the credentials are correct and that access has been granted by the resource.")
		return
	case r.PostForm.Get("client_id") != h.app.ClientID || r.PostForm.Get("client_secret") != h.app.ClientSecret:
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "AADB2C90081: The specified client_secret does not match the expected value for this client.")
		return
	}
	var g *grant
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		code := r.PostForm.Get("code")
		g = h.codes[code]
		delete(h.codes, code)
		if g == nil || !now.Before(g.authAt.Add(codeLife)) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "AADB2C90080: The provided grant has expired. Please re-authenticate and try again.")
			return
		}
		if r.PostForm.Get("redirect_uri") != g.redirect {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "AADB2C90006: The redirect URI provided in the request is not the one of the authorization.")
			return
		}
		h.grants = append(h.grants, g)
	case "refresh_token":
		sent := r.PostForm.Get("refresh_token")
		for _, c := range h.grants {
			if sent != "" && c.refresh == sent {
				g = c
			}
		}
		if g == nil || g.revoked || !now.Before(g.authAt.Add(h.behavior.GrantLife)) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "AADB2C90080: The provided grant has expired. Please re-authenticate and try again.")
			return
		}
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "AADB2C90086: The supplied grant_type is not supported.")
		return
	}
	if g.refresh == "" || h.behavior.Rotate {
		g.refresh = rand.Text()
	}
	g.access, g.accessEnds = rand.Text(), now.Add(h.behavior.AccessTTL)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": g.access, "refresh_token": g.refresh, "token_type": "Bearer",
		"expires_in": int(h.behavior.AccessTTL.Seconds()), "id_token": idToken(g.authAt, now, h.behavior.AccessTTL),
	})
}

// idToken is an ID token of the sign-in, unsigned: its claims tell when the owner
// consented (auth_time), which a client reads without checking a signature.
func idToken(authAt, now time.Time, ttl time.Duration) string {
	enc := func(v any) string {
		raw, _ := json.Marshal(v) // a map of strings and numbers: cannot fail
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return enc(map[string]any{"alg": "none", "typ": "JWT"}) + "." +
		enc(map[string]any{"auth_time": authAt.Unix(), "iat": now.Unix(), "exp": now.Add(ttl).Unix()}) + ".sim"
}

// api wraps an endpoint of the API: the access first (401), then the limits (429), then
// the grant's car.
func (h *Handler) api(serve func(w http.ResponseWriter, g *grant, now time.Time)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		access, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		h.mu.Lock()
		defer h.mu.Unlock()
		now := h.clk.Now()
		var g *grant
		for _, c := range h.grants {
			if bearer && access != "" && c.access == access {
				g = c
			}
		}
		switch {
		case g == nil || g.revoked || !now.Before(g.accessEnds):
			writeError(w, http.StatusUnauthorized, "Unauthorized", r.URL.Path)
			return
		case now.Before(h.limitedUntil):
			w.Header().Set("Retry-After", retryAfter(h.limitedUntil.Sub(now)))
			writeError(w, http.StatusTooManyRequests, "Too Many Requests", r.URL.Path)
			return
		case h.behavior.MinGap > 0 && !g.lastCall.IsZero() && now.Sub(g.lastCall) < h.behavior.MinGap:
			// A call refused does not move the gap: the next may come at its end.
			w.Header().Set("Retry-After", retryAfter(g.lastCall.Add(h.behavior.MinGap).Sub(now)))
			writeError(w, http.StatusTooManyRequests, "Too Many Requests", r.URL.Path)
			return
		}
		g.lastCall = now
		serve(w, g, now)
	}
}

func (h *Handler) garage(w http.ResponseWriter, g *grant, now time.Time) {
	writeJSON(w, http.StatusOK, garageOf(g.vin, h.byVIN[g.vin].ReportAt(now)))
}

// telemetry answers the car's state. A send a read was told is never taken back: a car
// that stops driving keeps the time of its last send, though the model behind dates its
// values from their last change.
func (h *Handler) telemetry(w http.ResponseWriter, g *grant, now time.Time) {
	r := h.byVIN[g.vin].ReportAt(now)
	s := h.sent[g.vin].after(sendsOf(r, h.behavior, now))
	h.sent[g.vin] = s
	writeJSON(w, http.StatusOK, telemetryOf(g.vin, r, s))
}

// retryAfter writes a wait in whole seconds, rounded up: a wait left is never told as
// none.
func retryAfter(d time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(d.Seconds()))))
}

// writeOAuthError answers as the token endpoint does: an OAuth error and Azure AD B2C's
// description.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	body := map[string]any{"error_description": description}
	if code != "" {
		body["error"] = code
	}
	writeJSON(w, status, body)
}

// writeError answers with the specification's ErrorDTO.
func writeError(w http.ResponseWriter, status int, message, path string) {
	writeJSON(w, status, map[string]any{"error": status, "message": message, "path": path})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}
