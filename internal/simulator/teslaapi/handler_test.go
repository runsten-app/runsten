package teslaapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const vin = "LRW3E7EK1RC000001"

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

// fakeSource is a car whose state the test sets.
type fakeSource struct {
	rep vehicle.Report
	st  vehicle.State
}

func (f *fakeSource) VIN() string                       { return vin }
func (f *fakeSource) ReportAt(time.Time) vehicle.Report { return f.rep }
func (f *fakeSource) StateAt(time.Time) vehicle.State   { return f.st }

// parked is a Model 3 parked and unplugged since t0, its last activity.
func parked() *fakeSource {
	st := vehicle.State{
		SoC: 58.4, OdometerKm: 18214, Locked: true, TargetSoC: 80,
		Position: vehicle.Position{Lat: 50.8930, Lon: 4.4180, Heading: 24.6},
	}
	return &fakeSource{
		rep: vehicle.Report{
			Spec:   vehicle.Spec{Model: "Model 3", Trim: "74d", ModelYear: 2024, BatteryKWh: 78, UsableKWh: 75, ConsumptionKWhPer100km: 15},
			Energy: st, EnergyAt: t0, Status: st, StatusAt: t0,
		},
		st: st,
	}
}

type server struct {
	*httptest.Server
	clk *clock.Manual
	h   *Handler
	id  string
}

func newServer(t *testing.T, src *fakeSource) *server {
	t.Helper()
	clk := clock.NewManual(t0)
	h := NewHandler([]Source{src}, clk, DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &server{Server: srv, clk: clk, h: h, id: strconv.FormatUint(h.cars[0].id, 10)}
}

// call sends a request and returns its status, its JSON body (numbers as written) and
// its headers.
func (s *server) call(t *testing.T, method, path, tok, contentType string, body []byte) (int, map[string]any, http.Header) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, s.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	_ = dec.Decode(&out) // a 204 has no body
	return resp.StatusCode, out, resp.Header
}

// refresh posts form to the token endpoint.
func (s *server) refresh(t *testing.T, form url.Values) (int, map[string]any) {
	t.Helper()
	status, body, _ := s.call(t, http.MethodPost, AuthPrefix+"token", "", "application/x-www-form-urlencoded", []byte(form.Encode()))
	return status, body
}

func grant(refresh string) url.Values {
	return url.Values{"grant_type": {"refresh_token"}, "client_id": {"ownerapi"}, "refresh_token": {refresh}, "scope": {"openid email offline_access"}}
}

// accessToken refreshes the development token.
func (s *server) accessToken(t *testing.T) string {
	t.Helper()
	status, body := s.refresh(t, grant(DevRefreshToken()))
	if status != http.StatusOK {
		t.Fatalf("refresh: %d %v", status, body)
	}
	return body["access_token"].(string)
}

// get calls path of the API with tok.
func (s *server) get(t *testing.T, tok, path string) (int, map[string]any, http.Header) {
	t.Helper()
	return s.call(t, http.MethodGet, path, tok, "", nil)
}

// post calls path of the control API, and returns the status and the body.
func (s *server) post(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	status, body, _ := s.call(t, http.MethodPost, path, "", "", nil)
	return status, body
}

func TestRefreshRotates(t *testing.T) {
	s := newServer(t, parked())
	status, body := s.refresh(t, grant(DevRefreshToken()))
	if status != http.StatusOK || body["token_type"] != "Bearer" || body["expires_in"] != json.Number("28800") {
		t.Fatalf("refresh: %d %v", status, body)
	}
	next, _ := body["refresh_token"].(string)
	if next == "" || next == DevRefreshToken() {
		t.Fatalf("refresh token %q, want a new one", next)
	}
	// The one given no longer refreshes; the new one does, once.
	if status, body := s.refresh(t, grant(DevRefreshToken())); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("reused: %d %v, want 400 invalid_grant", status, body)
	}
	if status, _ := s.refresh(t, grant(next)); status != http.StatusOK {
		t.Errorf("rotated: %d, want 200", status)
	}
}

func TestRefreshRefused(t *testing.T) {
	s := newServer(t, parked())
	for name, tc := range map[string]struct {
		form   url.Values
		status int
		code   string
	}{
		"forged":       {grant("rt.0.dev.0000"), http.StatusBadRequest, "invalid_grant"},
		"other client": {url.Values{"grant_type": {"refresh_token"}, "client_id": {"fleet"}, "refresh_token": {DevRefreshToken()}}, http.StatusUnauthorized, "invalid_client"},
		"other grant":  {url.Values{"grant_type": {"password"}, "client_id": {"ownerapi"}}, http.StatusBadRequest, "unsupported_grant_type"},
	} {
		if status, body := s.refresh(t, tc.form); status != tc.status || body["error"] != tc.code {
			t.Errorf("%s: %d %v, want %d %s", name, status, body, tc.status, tc.code)
		}
	}
}

func TestRevoke(t *testing.T) {
	s := newServer(t, parked())
	tok := s.accessToken(t)
	s.post(t, ControlPrefix+"revoke")
	_, body := s.refresh(t, grant(DevRefreshToken()))
	if body["error"] != "invalid_grant" {
		t.Errorf("after the revocation: %v, want invalid_grant", body)
	}
	// The access token stays valid until its expiry.
	if status, _, _ := s.get(t, tok, APIPrefix+"products"); status != http.StatusOK {
		t.Errorf("products with the access token: %d, want 200", status)
	}
	// A new token from Tesla Auth reads again.
	_, fresh := s.post(t, ControlPrefix+"refresh-token")
	if status, body := s.refresh(t, grant(fresh["refresh_token"].(string))); status != http.StatusOK {
		t.Errorf("new token: %d %v", status, body)
	}
}

func TestAccessToken(t *testing.T) {
	s := newServer(t, parked())
	tok := s.accessToken(t)
	for name, bad := range map[string]string{"none": "", "forged": "at.9999999999.x.0000", "a refresh token": DevRefreshToken()} {
		if status, body, _ := s.get(t, bad, APIPrefix+"products"); status != http.StatusUnauthorized || body["error"] != "invalid bearer token" {
			t.Errorf("%s: %d %v, want 401 invalid bearer token", name, status, body)
		}
	}
	s.clk.Advance(8 * time.Hour)
	if status, _, _ := s.get(t, tok, APIPrefix+"products"); status != http.StatusUnauthorized {
		t.Errorf("expired: %d, want 401", status)
	}
}

func TestProducts(t *testing.T) {
	s := newServer(t, parked())
	status, body, _ := s.get(t, s.accessToken(t), APIPrefix+"products")
	list, _ := body["response"].([]any)
	if status != http.StatusOK || len(list) != 1 || body["count"] != json.Number("1") {
		t.Fatalf("products: %d %v", status, body)
	}
	p := list[0].(map[string]any)
	if p["vin"] != vin || p["id_s"] != s.id || p["id"].(json.Number).String() != s.id || p["state"] != "online" || p["device_type"] != "vehicle" {
		t.Errorf("product: %v", p)
	}
	if len(s.id) != 16 {
		t.Errorf("id %s, want 16 digits", s.id)
	}
}

func TestNotFound(t *testing.T) {
	s := newServer(t, parked())
	tok := s.accessToken(t)
	for _, path := range []string{"vehicles/1", "vehicles/1/vehicle_data", "vehicles/" + s.id + "/wake_up", "users/me"} {
		if status, body, _ := s.get(t, tok, APIPrefix+path); status != http.StatusNotFound || body["error"] != "not_found" {
			t.Errorf("%s: %d %v, want 404 not_found", path, status, body)
		}
	}
}

// vehicleData reads vehicle_data and returns its status and response.
func (s *server) vehicleData(t *testing.T, tok string) (int, map[string]any) {
	t.Helper()
	status, body, _ := s.get(t, tok, APIPrefix+"vehicles/"+s.id+"/vehicle_data")
	resp, _ := body["response"].(map[string]any)
	return status, resp
}

func (s *server) state(t *testing.T, tok string) any {
	t.Helper()
	_, body, _ := s.get(t, tok, APIPrefix+"vehicles/"+s.id)
	return body["response"].(map[string]any)["state"]
}

func TestSleep(t *testing.T) {
	src := parked()
	s := newServer(t, src)
	tok := s.accessToken(t)

	// Read every minute, the car stays awake.
	for range 20 {
		s.clk.Advance(time.Minute)
		if status, _ := s.vehicleData(t, tok); status != http.StatusOK {
			t.Fatalf("at %v: vehicle_data %d, want 200", s.clk.Now(), status)
		}
	}
	// Its state alone does not keep it awake: asleep 10 min after the last vehicle_data.
	for range 9 {
		s.clk.Advance(time.Minute)
		if st := s.state(t, tok); st != "online" {
			t.Fatalf("at %v: %v, want online", s.clk.Now(), st)
		}
	}
	s.clk.Advance(time.Minute)
	if st := s.state(t, tok); st != "offline" {
		t.Fatalf("10 min after the last read: %v, want offline", st)
	}
	status, _, _ := s.get(t, tok, APIPrefix+"vehicles/"+s.id+"/vehicle_data")
	if status != http.StatusRequestTimeout {
		t.Fatalf("asleep: vehicle_data %d, want 408", status)
	}
	// A 408 does not wake it.
	s.clk.Advance(time.Hour)
	if st := s.state(t, tok); st != "offline" {
		t.Errorf("an hour later: %v, want offline", st)
	}

	// It wakes as it drives.
	src.st.Driving, src.st.SpeedKmh, src.st.Locked = true, 50, false
	if st := s.state(t, tok); st != "online" {
		t.Errorf("driving: %v, want online", st)
	}
	_, vd := s.vehicleData(t, tok)
	ds := vd["drive_state"].(map[string]any)
	if ds["shift_state"] != "D" || ds["speed"] != json.Number("31") || ds["power"] != json.Number("8") {
		t.Errorf("drive_state: %v", ds)
	}
}

func TestChargeKeepsAwake(t *testing.T) {
	src := parked()
	src.st.PluggedIn, src.st.Charging, src.st.ChargeType, src.st.ChargingPowerW, src.st.ChargeAddedKWh = true, vehicle.Charging, vehicle.AC, 11000, 3.41
	s := newServer(t, src)
	tok := s.accessToken(t)
	s.clk.Advance(time.Hour)
	status, vd := s.vehicleData(t, tok)
	if status != http.StatusOK {
		t.Fatalf("charging, an hour later: %d, want 200", status)
	}
	cs := vd["charge_state"].(map[string]any)
	want := map[string]any{
		"charging_state": "Charging", "charger_power": json.Number("11"), "charger_phases": json.Number("3"),
		"charger_voltage": json.Number("230"), "charger_actual_current": json.Number("16"), "fast_charger_present": false,
		"charge_energy_added": json.Number("3.41"), "battery_level": json.Number("58"), "charge_limit_soc": json.Number("80"),
		"minutes_to_full_charge": json.Number("89"), "charge_port_door_open": true,
	}
	for k, v := range want {
		if cs[k] != v {
			t.Errorf("charge_state.%s = %v, want %v", k, cs[k], v)
		}
	}
	if ds := vd["drive_state"].(map[string]any); ds["power"] != json.Number("-11") || ds["shift_state"] != nil {
		t.Errorf("drive_state while charging: %v", ds)
	}

	// Complete, still plugged in: it falls asleep.
	src.st.Charging, src.st.ChargingPowerW = vehicle.Done, 0
	src.rep.EnergyAt = s.clk.Now()
	s.clk.Advance(10 * time.Minute)
	if st := s.state(t, tok); st != "offline" {
		t.Errorf("charge complete, 10 min later: %v, want offline", st)
	}
}

func TestDCCharge(t *testing.T) {
	src := parked()
	src.st.PluggedIn, src.st.Charging, src.st.ChargeType, src.st.ChargingPowerW = true, vehicle.Charging, vehicle.DC, 120000
	s := newServer(t, src)
	_, vd := s.vehicleData(t, s.accessToken(t))
	cs := vd["charge_state"].(map[string]any)
	if cs["fast_charger_present"] != true || cs["charger_power"] != json.Number("120") || cs["charger_phases"] != nil {
		t.Errorf("charge_state on DC: present %v, power %v, phases %v", cs["fast_charger_present"], cs["charger_power"], cs["charger_phases"])
	}
}

func TestVehicleData(t *testing.T) {
	s := newServer(t, parked())
	status, vd := s.vehicleData(t, s.accessToken(t))
	if status != http.StatusOK {
		t.Fatalf("vehicle_data: %d", status)
	}
	if vd["vin"] != vin || vd["id_s"] != s.id || vd["state"] != "online" {
		t.Errorf("vehicle: vin %v, id_s %v, state %v", vd["vin"], vd["id_s"], vd["state"])
	}
	cfg := vd["vehicle_config"].(map[string]any)
	if cfg["car_type"] != "model3" || cfg["trim_badging"] != "74d" {
		t.Errorf("vehicle_config: car_type %v, trim_badging %v", cfg["car_type"], cfg["trim_badging"])
	}
	vs := vd["vehicle_state"].(map[string]any)
	// 18,214 km in miles; the template's media_info left for the client to remove.
	if vs["odometer"] != json.Number("11317.654895") || vs["locked"] != true || vs["media_info"] == nil {
		t.Errorf("vehicle_state: odometer %v, locked %v, media_info %v", vs["odometer"], vs["locked"], vs["media_info"])
	}
	cs := vd["charge_state"].(map[string]any)
	// A SoC of 58.4 %, a usable level of 57.4 % of 75 kWh at 15 kWh/100 km: 287 km,
	// 178.33 mi.
	if cs["battery_range"] != json.Number("178.33") || cs["usable_battery_level"] != json.Number("57") || cs["charging_state"] != "Disconnected" || cs["charger_power"] != json.Number("0") {
		t.Errorf("charge_state: range %v, state %v, power %v", cs["battery_range"], cs["charging_state"], cs["charger_power"])
	}
	ds := vd["drive_state"].(map[string]any)
	if ds["latitude"] != json.Number("50.893") || ds["heading"] != json.Number("25") || ds["shift_state"] != nil ||
		ds["gps_as_of"] != json.Number(strconv.FormatInt(t0.Unix(), 10)) {
		t.Errorf("drive_state: %v", ds)
	}
	// Parked, the climate off and the cabin at the outside temperature.
	if cl := vd["climate_state"].(map[string]any); cl["is_climate_on"] != false || cl["inside_temp"] != cl["outside_temp"] {
		t.Errorf("climate_state: on %v, inside %v, outside %v", cl["is_climate_on"], cl["inside_temp"], cl["outside_temp"])
	}
	ms := json.Number(strconv.FormatInt(t0.UnixMilli(), 10))
	for _, b := range []string{"charge_state", "climate_state", "drive_state", "gui_settings", "vehicle_config", "vehicle_state"} {
		if ts := vd[b].(map[string]any)["timestamp"]; ts != ms {
			t.Errorf("%s.timestamp = %v, want %v", b, ts, ms)
		}
	}
}

func TestFaults(t *testing.T) {
	s := newServer(t, parked())
	tok := s.accessToken(t)

	if status, _ := s.post(t, ControlPrefix+"rate-limit?for=90s"); status != http.StatusNoContent {
		t.Fatalf("rate-limit: %d", status)
	}
	status, body, header := s.get(t, tok, APIPrefix+"products")
	if status != http.StatusTooManyRequests || header.Get("Retry-After") != "90" || body["error"] != "rate limit exceeded" {
		t.Errorf("rate limited: %d %v Retry-After %q", status, body, header.Get("Retry-After"))
	}
	s.clk.Advance(90 * time.Second)
	if status, _, _ := s.get(t, tok, APIPrefix+"products"); status != http.StatusOK {
		t.Errorf("after the limit: %d, want 200", status)
	}

	s.post(t, ControlPrefix+"owner-api?closed=true")
	status, body, _ = s.get(t, tok, APIPrefix+"products")
	if msg, _ := body["error"].(string); status != http.StatusForbidden || !strings.Contains(msg, "fleet-api") {
		t.Errorf("closed: %d %v", status, body)
	}
	s.post(t, ControlPrefix+"owner-api?closed=false")
	if status, _, _ := s.get(t, tok, APIPrefix+"products"); status != http.StatusOK {
		t.Errorf("open again: %d, want 200", status)
	}

	for _, path := range []string{"rate-limit?for=soon", "owner-api?closed=maybe"} {
		if status, _ := s.post(t, ControlPrefix+path); status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", path, status)
		}
	}
}

func TestDuplicateVIN(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic on a duplicate VIN")
		}
	}()
	NewHandler([]Source{parked(), parked()}, clock.NewManual(t0), DefaultBehavior())
}

func TestTokenInJSON(t *testing.T) {
	s := newServer(t, parked())
	b, _ := json.Marshal(map[string]string{"grant_type": "refresh_token", "client_id": "ownerapi", "refresh_token": DevRefreshToken()})
	if status, body, _ := s.call(t, http.MethodPost, AuthPrefix+"token", "", "application/json", b); status != http.StatusOK {
		t.Errorf("JSON refresh: %d %v, want 200", status, body)
	}
}
