// Package bmwapi exposes simulated vehicles as BMW CarData, the access BMW gives the
// owner of a BMW or a Mini to their car's data (ADR 0045): the OAuth device flow of the
// owner's client ID, the REST API (the account's vehicles, their basic data, containers
// and the last value of their descriptors, the charging history) and the MQTT stream the
// car pushes its changes to (Stream). The shapes are those of BMW's OpenAPI description
// and of its streaming guide; none is checked on a real car yet.
//
// One account, of GCID, is simulated, and its client ID is DevClientID. Faults are set
// on demand under ControlPrefix: the day's quota spent, the refresh tokens revoked, the
// stream dropped.
package bmwapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// The paths the handler serves. The real hosts serve them under
// https://customer.bmwgroup.com and https://api-cardata.bmwgroup.com: the client is
// given the simulator's address for both.
const (
	// AuthPrefix is the device flow's and the tokens'.
	AuthPrefix = "/gcdm/oauth/"
	// APIPrefix is CarData's API.
	APIPrefix = "/customers/"
	// ControlPrefix sets the faults, outside the API.
	ControlPrefix = "/sim/bmw/"
)

// DailyQuota is the calls a day CarData's API answers an account, reset at midnight UTC
// (BMW does not say per what: per account, as the Home Assistant integration counts them).
const DailyQuota = 50

// maxContainers is the containers an account may have (CU-124).
const maxContainers = 10

// Source provides the state of a simulated vehicle.
type Source interface {
	VIN() string
	// Brand is the scenario's: bmw or mini.
	Brand() string
	// ReportAt gives the vehicle's spec.
	ReportAt(at time.Time) vehicle.Report
	// StateAt gives its actual state: the car pushes its changes as they come.
	StateAt(at time.Time) vehicle.State
}

// Behavior groups the assumptions about the car and the API.
type Behavior struct {
	// TokenTTL is the lifetime of the access and ID tokens: one hour at BMW's. The
	// stream is closed when the ID token it was opened with expires.
	TokenTTL time.Duration
	// ApproveAfter is how long after a device code the simulated owner has approved it.
	ApproveAfter time.Duration
	// SendEvery is how often at most the car sends a descriptor that changes all along
	// a drive or a charge (the SoC, the odometer, the position, the range): unknown on
	// the real car, to measure (ADR 0045 §9). The others are sent at each change.
	SendEvery time.Duration
}

// DefaultBehavior returns the assumptions used without explicit configuration.
func DefaultBehavior() Behavior {
	return Behavior{TokenTTL: time.Hour, ApproveAfter: 10 * time.Second, SendEvery: 30 * time.Second}
}

// known is a descriptor's last value the car sent, and when.
type known struct {
	val value
	at  time.Time
}

// session is a charging session, as the charging history gives it.
type session struct {
	start, end       time.Time
	startSoC, endSoC float64
	addedKWh         float64 // into the battery, at its end
	mileageKm        int
	lat, lon         float64
	averageKW        float64
}

// car is a vehicle of the account.
type car struct {
	src     Source
	spec    vehicle.Spec
	known   map[string]known
	pending map[string]bool // sent since the stream last took them
	charge  *session        // in progress
	history []session
}

// container is a container the account created.
type container struct {
	ID, Name, Purpose string
	Descriptors       []string
	Created           time.Time
	Deleted           bool
}

// Handler serves the simulated API.
type Handler struct {
	clk      clock.Clock
	behavior Behavior
	cars     []*car
	byVIN    map[string]*car
	mux      *http.ServeMux

	mu         sync.Mutex
	grants     map[string]*grant // the device codes given
	used       map[string]bool   // the refresh tokens consumed
	generation int               // of the refresh tokens accepted, raised by a revocation
	containers []*container
	day        string // the UTC day the calls are counted on
	calls      int
	stream     *Stream // the stream, if one serves the account
}

// NewHandler creates the simulated API for the vehicles srcs, the vehicles of one
// account, with the simulated clock clk. Their VINs must be distinct (scenario.NewFleet
// checks it): NewHandler panics otherwise.
func NewHandler(srcs []Source, clk clock.Clock, behavior Behavior) *Handler {
	h := &Handler{
		clk: clk, behavior: behavior, byVIN: map[string]*car{}, mux: http.NewServeMux(),
		grants: map[string]*grant{}, used: map[string]bool{},
	}
	for _, src := range srcs {
		if h.byVIN[src.VIN()] != nil {
			panic("bmwapi: duplicate VIN " + src.VIN())
		}
		c := &car{src: src, known: map[string]known{}, pending: map[string]bool{}}
		h.cars = append(h.cars, c)
		h.byVIN[src.VIN()] = c
	}
	api := http.NewServeMux()
	api.HandleFunc("GET "+APIPrefix+"vehicles/mappings", h.mappings)
	api.HandleFunc("GET "+APIPrefix+"vehicles/{vin}/basicData", h.basicData)
	api.HandleFunc("GET "+APIPrefix+"vehicles/{vin}/telematicData", h.telematicData)
	api.HandleFunc("GET "+APIPrefix+"vehicles/{vin}/chargingHistory", h.chargingHistory)
	api.HandleFunc("POST "+APIPrefix+"containers", h.createContainer)
	api.HandleFunc("GET "+APIPrefix+"containers", h.listContainers)
	api.HandleFunc("GET "+APIPrefix+"containers/{id}", h.getContainer)
	api.HandleFunc("DELETE "+APIPrefix+"containers/{id}", h.deleteContainer)
	api.HandleFunc(APIPrefix, func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "CU-404", "Not found.") // assumed
	})

	h.mux.HandleFunc("POST "+AuthPrefix+"device/code", h.deviceCode)
	h.mux.HandleFunc("POST "+AuthPrefix+"token", h.token)
	h.mux.Handle(APIPrefix, h.middleware(api))
	h.mux.HandleFunc("POST "+ControlPrefix+"quota", h.setQuota)
	h.mux.HandleFunc("POST "+ControlPrefix+"revoke", h.revoke)
	h.mux.HandleFunc("POST "+ControlPrefix+"stream/drop", h.dropStream)
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// observe brings the cars' last values up to now, as they would have sent them: a
// descriptor that changed is sent, unless it is one that changes all along a drive or a
// charge and was sent less than SendEvery ago. A position is sent whole, its latitude,
// longitude and heading with the same timestamp, which is how a client pairs them.
func (h *Handler) observe(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.cars {
		rep, st := c.src.ReportAt(now), c.src.StateAt(now)
		c.spec = rep.Spec
		vals := render(rep.Spec, st, now)
		changed := map[string]bool{}
		for k, v := range vals {
			old, ok := c.known[k]
			if ok && old.val == v {
				continue
			}
			if ok && throttled[k] && now.Sub(old.at) < h.behavior.SendEvery {
				continue
			}
			changed[k] = true
		}
		if changed[DescLatitude] || changed[DescLongitude] || changed[DescHeading] {
			changed[DescLatitude], changed[DescLongitude], changed[DescHeading] = true, true, true
		}
		for k := range changed {
			c.known[k] = known{val: vals[k], at: now}
			c.pending[k] = true
		}
		c.follow(st, now)
	}
}

// follow records the car's charging sessions for its history.
func (c *car) follow(st vehicle.State, now time.Time) {
	charging := st.Charging == vehicle.Charging
	switch {
	case charging && c.charge == nil:
		c.charge = &session{start: now, startSoC: st.SoC, mileageKm: int(st.OdometerKm), lat: st.Position.Lat, lon: st.Position.Lon}
	case c.charge != nil:
		s := *c.charge
		s.end, s.endSoC, s.addedKWh = now, st.SoC, st.ChargeAddedKWh
		if hours := s.end.Sub(s.start).Hours(); hours > 0 {
			s.averageKW = s.addedKWh / chargingEfficiency / hours
		}
		c.history = append(c.history, s)
		c.charge = nil
	}
}

// chargingEfficiency turns the energy a charge put into the battery into the energy it
// drew from the grid: assumed.
const chargingEfficiency = 0.9

// middleware checks the header, the access token and the quota of a call of the API.
func (h *Handler) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := h.clk.Now()
		if r.Method == http.MethodGet && r.Header.Get("x-version") != "v1" {
			writeError(w, http.StatusBadRequest, "CU-400", "Missing or unsupported x-version header.") // assumed
			return
		}
		flags, code := accessScopes(r, now)
		if code != "" {
			writeError(w, http.StatusUnauthorized, code, "Invalid or expired token.")
			return
		}
		if !strings.Contains(flags, "a") {
			writeError(w, http.StatusForbidden, "CU-103", "The token has no CarData scope.")
			return
		}
		h.mu.Lock()
		if day := now.Format(time.DateOnly); day != h.day {
			h.day, h.calls = day, 0
		}
		h.calls++
		over := h.calls > DailyQuota
		h.mu.Unlock()
		if over {
			writeError(w, http.StatusTooManyRequests, "CU-429", "Daily rate limit exceeded.")
			return
		}
		h.observe(now)
		next.ServeHTTP(w, r)
	})
}

// mappings answers the account's vehicles, a list of mappings (assumed a bare array:
// BMW's description gives the item, and its clients read both an array and an object
// holding one).
func (h *Handler) mappings(w http.ResponseWriter, _ *http.Request) {
	out := make([]map[string]string, 0, len(h.cars))
	for _, c := range h.cars {
		out = append(out, map[string]string{"vin": c.src.VIN(), "mappedSince": "2025-03-01T08:00:00.000Z", "mappingType": "PRIMARY"})
	}
	writeJSON(w, http.StatusOK, out)
}

// carOf returns the car of the path's VIN, or answers CU-104.
func (h *Handler) carOf(w http.ResponseWriter, r *http.Request) *car {
	c := h.byVIN[r.PathValue("vin")]
	if c == nil {
		writeError(w, http.StatusForbidden, "CU-104", "No permission for this VIN.")
	}
	return c
}

func (h *Handler) basicData(w http.ResponseWriter, r *http.Request) {
	c := h.carOf(w, r)
	if c == nil {
		return
	}
	h.mu.Lock()
	spec := c.spec
	h.mu.Unlock()
	brand, series := "BMW", strings.Fields(spec.Model + " ")[0]
	if c.src.Brand() == "mini" {
		brand = "MINI"
	}
	propulsion := "BEV"
	if spec.PlugInHybrid {
		propulsion = "PHEV"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"vin": c.src.VIN(), "brand": brand, "isTelematicsCapable": true, "modelName": spec.Model,
		"series": series, "modelRange": series, "modelKey": "", "bodyType": "", "numberOfDoors": 4,
		"hasNavi": true, "headUnit": "MGU", "hasSunRoof": false, "countryCodeISO": "BE", "steering": "LL",
		"engine": "", "driveTrain": propulsion, "propulsionType": propulsion, "chargingModes": []string{"AC_TYPE2PLUG", "DC"},
		"constructionDate":     strconv.Itoa(spec.ModelYear-1) + "-11-01T00:00:00.000Z",
		"hvsMaxEnergyAbsolute": strconv.FormatFloat(spec.BatteryKWh, 'f', 1, 64), "simStatus": "ACTIVE",
		"colourCode": "", "colourCodeRaw": "", "puStep": "", "fullSAList": "",
	})
}

// telematicData answers the last value of each descriptor of the container, as strings,
// with the time the car sent it; a descriptor never sent is left out.
func (h *Handler) telematicData(w http.ResponseWriter, r *http.Request) {
	c := h.carOf(w, r)
	if c == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ct := h.container(r.URL.Query().Get("containerId"))
	if ct == nil {
		writeError(w, http.StatusNotFound, "CU-404", "Unknown or deleted container.") // assumed
		return
	}
	data := map[string]any{}
	for _, d := range ct.Descriptors {
		if k, ok := c.known[d]; ok {
			data[d] = map[string]string{"value": stringOf(k.val.v), "unit": k.val.unit, "timestamp": stamp(k.at)}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"telematicData": data})
}

// chargingHistory answers the charging sessions that started within [from, to), ISO
// dates (assumed), newest first, in one page.
func (h *Handler) chargingHistory(w http.ResponseWriter, r *http.Request) {
	c := h.carOf(w, r)
	if c == nil {
		return
	}
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "CU-400", "from and to: ISO 8601 date-times expected.") // assumed
		return
	}
	h.mu.Lock()
	sessions := append([]session(nil), c.history...)
	h.mu.Unlock()
	data := []map[string]any{}
	for i := len(sessions) - 1; i >= 0; i-- {
		s := sessions[i]
		if s.start.Before(from) || !s.start.Before(to) {
			continue
		}
		data = append(data, map[string]any{
			"startTime": s.start.Unix(), "endTime": s.end.Unix(), "timeZone": "Europe/Brussels",
			"displayedStartSoc": int(s.startSoC), "displayedSoc": int(s.endSoC),
			"totalChargingDurationSec":       int(s.end.Sub(s.start).Seconds()),
			"energyConsumedFromPowerGridKwh": round(s.addedKWh/chargingEfficiency, 2),
			"isPreconditioningActivated":     false, "mileage": s.mileageKm, "mileageUnits": "MileageUnits.KM",
			"chargingLocation": map[string]any{
				"municipality": "Bruxelles", "formattedAddress": "", "streetAddress": "",
				"mapMatchedLatitude": s.lat, "mapMatchedLongitude": s.lon,
			},
			"chargingBlocks": []map[string]any{{"startTime": s.start.Unix(), "endTime": s.end.Unix(), "averagePowerGridKw": round(s.averageKW, 2)}},
			"businessErrors": []any{},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// container returns the active container of the ID; the caller holds h.mu.
func (h *Handler) container(id string) *container {
	for _, c := range h.containers {
		if c.ID == id && !c.Deleted {
			return c
		}
	}
	return nil
}

func (h *Handler) createContainer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name                 string   `json:"name"`
		Purpose              string   `json:"purpose"`
		TechnicalDescriptors []string `json:"technicalDescriptors"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil || len(req.TechnicalDescriptors) == 0 {
		writeError(w, http.StatusBadRequest, "CU-400", "A name, a purpose and technicalDescriptors are required.") // assumed
		return
	}
	valid := map[string]bool{}
	for _, d := range Descriptors() {
		valid[d] = true
	}
	for _, d := range req.TechnicalDescriptors {
		if !valid[d] {
			writeError(w, http.StatusBadRequest, "CU-402", "Invalid telematic key: "+d)
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	active := 0
	for _, c := range h.containers {
		if !c.Deleted {
			active++
		}
	}
	if active >= maxContainers {
		writeError(w, http.StatusBadRequest, "CU-124", "Maximum number of containers reached.")
		return
	}
	c := &container{
		ID: strings.ToUpper(nonce()[:13]), Name: req.Name, Purpose: req.Purpose,
		Descriptors: req.TechnicalDescriptors, Created: h.clk.Now(),
	}
	h.containers = append(h.containers, c)
	writeJSON(w, http.StatusCreated, c.details())
}

func (h *Handler) listContainers(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []map[string]any{}
	for _, c := range h.containers {
		if !c.Deleted {
			d := c.details()
			delete(d, "technicalDescriptors")
			out = append(out, d)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": out})
}

func (h *Handler) getContainer(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.container(r.PathValue("id"))
	if c == nil {
		writeError(w, http.StatusNotFound, "CU-404", "Unknown or deleted container.") // assumed
		return
	}
	writeJSON(w, http.StatusOK, c.details())
}

func (h *Handler) deleteContainer(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.container(r.PathValue("id"))
	if c == nil {
		writeError(w, http.StatusNotFound, "CU-404", "Unknown or deleted container.") // assumed
		return
	}
	c.Deleted = true
	w.WriteHeader(http.StatusNoContent)
}

func (c *container) details() map[string]any {
	return map[string]any{
		"containerId": c.ID, "name": c.Name, "purpose": c.Purpose, "state": "ACTIVE",
		"created": stamp(c.Created), "technicalDescriptors": c.Descriptors,
	}
}

// setQuota sets the calls already made today (?used=), 50 to spend the day's quota.
func (h *Handler) setQuota(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("used"))
	if err != nil || n < 0 {
		http.Error(w, "used: an integer ≥ 0 expected", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	h.day, h.calls = h.clk.Now().Format(time.DateOnly), n
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// revoke refuses every refresh token issued so far, as an unsubscription in the portal
// would.
func (h *Handler) revoke(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	h.generation++
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// dropStream closes the stream's connection, as BMW's broker sometimes does.
func (h *Handler) dropStream(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	s := h.stream
	h.mu.Unlock()
	if s != nil {
		s.drop()
	}
	w.WriteHeader(http.StatusNoContent)
}

// stamp writes a time as CarData does: ISO 8601 in UTC, to the millisecond.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// stringOf writes a value as the API gives every value: a string.
func stringOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// writeError writes CarData's error body (CustomerErrorResponse).
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"exveErrorId": code, "exveErrorMsg": msg, "exveErrorRef": nonce(), "exveNote": ""})
}

// writeOAuthError writes the token endpoint's error (RFC 6749 §5.2, RFC 8628 §3.5).
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}
