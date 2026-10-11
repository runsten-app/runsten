// Package aximoteapi mimics Aximote's public API (v1), as its specification describes
// it, for the importer's tests and compose-test: fixed data, pagination by cursor, a
// 429 and a refused token on demand. The format of the tracks' export is not described:
// this one is a FeatureCollection whose features name their trip (assumption).
package aximoteapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DevToken is the token the simulator accepts, development only: axm_pat_ and 26 base62
// characters, as the real ones.
const DevToken = "axm_pat_RunstenSimulatorDevToken00" //nolint:gosec // the simulator's, never a real one

// Prefix is the path the handler serves.
const Prefix = "/api/public/v1/"

// defaultLimit and maxLimit are the page sizes. Assumption: the real ones are not
// documented.
const (
	defaultLimit = 20
	maxLimit     = 100
)

// Vehicle is an Aximote vehicle, as the API writes it.
type Vehicle struct {
	ID                 string   `json:"id"`
	Name               *string  `json:"name"`
	Make               *string  `json:"make"`
	Model              *string  `json:"model"`
	Year               *int     `json:"year"`
	VIN                *string  `json:"vin"`
	FuelType           []string `json:"fuelType"`
	BatteryCapacityKWh *float64 `json:"batteryCapacityKwh"`
	OdometerKm         *float64 `json:"odometerKm"`
}

// Point is a place, as the API writes it.
type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Country   *string `json:"country"`
	Locality  *string `json:"locality"`
}

// Trip is a trip, as the API writes it, with its track: longitude and latitude pairs,
// as GeoJSON writes them.
type Trip struct {
	ID                string       `json:"id"`
	VehicleID         string       `json:"vehicleId"`
	StartTime         time.Time    `json:"startTime"`
	EndTime           *time.Time   `json:"endTime"`
	DurationSec       *int         `json:"durationSec"`
	DistanceKm        *float64     `json:"distanceKm"`
	EnergyConsumedKWh *float64     `json:"energyConsumedKwh"`
	StartLocation     *Point       `json:"startLocation"`
	EndLocation       *Point       `json:"endLocation"`
	Tags              []string     `json:"tags"`
	Track             [][2]float64 `json:"-"`
}

// Refuel is a fuel stop or a charge, as the API writes it.
type Refuel struct {
	ID               string     `json:"id"`
	VehicleID        string     `json:"vehicleId"`
	Type             string     `json:"type"`
	StartTime        time.Time  `json:"startTime"`
	EndTime          *time.Time `json:"endTime"`
	IsActive         bool       `json:"isActive"`
	StartEnergyKWh   *float64   `json:"startEnergyKwh"`
	EndEnergyKWh     *float64   `json:"endEnergyKwh"`
	EnergyChargedKWh *float64   `json:"energyChargedKwh"`
	AvgChargingKW    *float64   `json:"avgChargingKw"`
	CostAmount       *float64   `json:"costAmount"`
	CostCurrency     *string    `json:"costCurrency"`
	Location         *Point     `json:"location"`
	// The window the API "resolves", whose meaning is not documented: the importer
	// leaves it.
	ResolvedStartTime *time.Time `json:"resolvedStartTime"`
	ResolvedEndTime   *time.Time `json:"resolvedEndTime"`
	WindowSource      *string    `json:"windowSource"`
}

// Data is what the account of the token holds.
type Data struct {
	Vehicles []Vehicle
	Trips    []Trip
	Refuels  []Refuel
}

// Handler serves the API for one token.
type Handler struct {
	token string
	data  Data

	mu       sync.Mutex
	revoked  bool
	limitIn  int // calls before the next 429; 0: none
	limitFor time.Duration
	calls    []string
}

// NewHandler serves data to token, and refuses every other.
func NewHandler(token string, data Data) *Handler {
	return &Handler{token: token, data: data}
}

// Revoke refuses the token from now on, as if the user had revoked it.
func (h *Handler) Revoke() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.revoked = true
}

// LimitAfter answers 429, with Retry-After retry, to the call after the next n.
func (h *Handler) LimitAfter(n int, retry time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.limitIn, h.limitFor = n+1, retry
}

// Calls returns the calls answered so far, as "METHOD path?query".
func (h *Handler) Calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.admit(w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(Prefix, "/"))
	switch {
	case r.Method == http.MethodGet && path == "/vehicles":
		writeJSON(w, http.StatusOK, h.data.Vehicles)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/vehicles/"):
		id := strings.TrimPrefix(path, "/vehicles/")
		i := slices.IndexFunc(h.data.Vehicles, func(v Vehicle) bool { return v.ID == id })
		if i < 0 {
			writeError(w, http.StatusNotFound, "vehicle not found")
			return
		}
		writeJSON(w, http.StatusOK, h.data.Vehicles[i])
	case r.Method == http.MethodGet && path == "/trips":
		servePage(w, r, h.data.Trips, func(t Trip) (string, time.Time) { return t.VehicleID, t.StartTime })
	case r.Method == http.MethodGet && path == "/refuels":
		servePage(w, r, h.data.Refuels, func(f Refuel) (string, time.Time) { return f.VehicleID, f.StartTime })
	case r.Method == http.MethodPost && path == "/trips/export/geojson":
		h.exportGeoJSON(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// admit checks the token and the limit, and records the call.
func (h *Handler) admit(w http.ResponseWriter, r *http.Request) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+h.token || h.revoked {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return false
	}
	if h.limitIn > 0 {
		h.limitIn--
		if h.limitIn == 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(h.limitFor.Seconds())))
			writeError(w, http.StatusTooManyRequests, "rate limited")
			return false
		}
	}
	call := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" {
		call += "?" + r.URL.RawQuery
	}
	h.calls = append(h.calls, call)
	return true
}

// servePage serves a page of items: those of the vehicle, starting at or after since
// and before until (assumption: the bounds apply to the start), newest first; the
// cursor is the offset of the next page.
func servePage[T any](w http.ResponseWriter, r *http.Request, items []T, of func(T) (string, time.Time)) {
	q := r.URL.Query()
	var since, until time.Time
	for name, dst := range map[string]*time.Time{"since": &since, "until": &until} {
		if v := q.Get(name); v != "" {
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid "+name)
				return
			}
			*dst = t
		}
	}
	limit := defaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	offset := 0
	if v := q.Get("cursor"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		offset = n
	}
	var selected []T
	for _, it := range items {
		vehicle, start := of(it)
		if (q.Has("vehicleId") && vehicle != q.Get("vehicleId")) ||
			(!since.IsZero() && start.Before(since)) || (!until.IsZero() && !start.Before(until)) {
			continue
		}
		selected = append(selected, it)
	}
	slices.SortStableFunc(selected, func(a, b T) int {
		_, sa := of(a)
		_, sb := of(b)
		return sb.Compare(sa)
	})
	out := struct {
		Items      []T     `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}{Items: []T{}}
	if offset < len(selected) {
		out.Items = selected[offset:min(offset+limit, len(selected))]
	}
	if next := offset + limit; next < len(selected) {
		s := strconv.Itoa(next)
		out.NextCursor = &s
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) exportGeoJSON(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TripIDs []string `json:"tripIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TripIDs == nil {
		writeError(w, http.StatusBadRequest, "tripIds required")
		return
	}
	type feature struct {
		Type       string            `json:"type"`
		Properties map[string]string `json:"properties"`
		Geometry   any               `json:"geometry"`
	}
	out := struct {
		Type     string    `json:"type"`
		Features []feature `json:"features"`
	}{Type: "FeatureCollection", Features: []feature{}}
	for _, id := range req.TripIDs {
		i := slices.IndexFunc(h.data.Trips, func(t Trip) bool { return t.ID == id })
		if i < 0 || len(h.data.Trips[i].Track) == 0 {
			continue
		}
		out.Features = append(out.Features, feature{
			Type: "Feature", Properties: map[string]string{"tripId": id},
			Geometry: map[string]any{"type": "LineString", "coordinates": h.data.Trips[i].Track},
		})
	}
	w.Header().Set("Content-Type", "application/geo+json")
	_ = json.NewEncoder(w).Encode(out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"status": status, "message": msg})
}
