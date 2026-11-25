package fordapi

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const redirect = "http://localhost:5173/auth/ford/callback"

// car is a vehicle of a fixed report.
type car struct {
	vin string
	rep *vehicle.Report
}

func (c car) VIN() string                       { return c.vin }
func (c car) ReportAt(time.Time) vehicle.Report { return *c.rep }

func parked(at time.Time, model string) *vehicle.Report {
	return &vehicle.Report{
		Spec:     vehicle.Spec{Model: model, ModelYear: 2025, BatteryKWh: 46, UsableKWh: 43, ConsumptionKWhPer100km: 14},
		Energy:   vehicle.State{SoC: 61.6, TargetSoC: 80},
		EnergyAt: at.Add(-time.Minute),
		Status:   vehicle.State{OdometerKm: 4804.7, Locked: true},
		StatusAt: at.Add(-time.Hour), Location: vehicle.Position{Lat: 45.764, Lon: 4.8357, Heading: 90}, LocationAt: at.Add(-time.Hour),
	}
}

type sim struct {
	t   *testing.T
	srv *httptest.Server
	clk *clock.Manual
	h   *Handler
	rep *vehicle.Report
}

func newSim(t *testing.T) *sim {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC))
	rep := parked(clk.Now(), "Puma")
	other := parked(clk.Now(), "Explorer")
	h := NewHandler([]Source{car{"WF0TESTPUMA000001", rep}, car{"WF0TESTEXPL000002", other}}, clk, DevApplication(), DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &sim{t: t, srv: srv, clk: clk, h: h, rep: rep}
}

// signIn opens the sign-in as a browser that does not follow the redirect, and returns
// its status, where Ford sends back to, and what it comes back with.
func (s *sim) signIn(query url.Values) (int, string, url.Values) {
	s.t.Helper()
	req, _ := http.NewRequestWithContext(s.t.Context(), http.MethodGet, s.srv.URL+Prefix+"auth/init?"+query.Encode(), http.NoBody)
	hc := *s.srv.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	location := resp.Header.Get("Location")
	back, _ := url.Parse(location)
	return resp.StatusCode, location, back.Query()
}

func signInQuery() url.Values {
	return url.Values{
		"response_type": {"code"}, "client_id": {DevApplication().ClientID}, "redirect_uri": {redirect},
		"scope": {"openid offline_access"}, "state": {"0123456789abcdef"},
	}
}

func (s *sim) post(form url.Values) (int, http.Header, map[string]any) {
	s.t.Helper()
	req, _ := http.NewRequestWithContext(s.t.Context(), http.MethodPost, s.srv.URL+Prefix+"token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(req)
}

func (s *sim) get(path, access string) (int, http.Header, map[string]any) {
	s.t.Helper()
	req, _ := http.NewRequestWithContext(s.t.Context(), http.MethodGet, s.srv.URL+Prefix+"v1/"+path, http.NoBody)
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	return s.do(req)
}

func (s *sim) do(req *http.Request) (int, http.Header, map[string]any) {
	s.t.Helper()
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, resp.Header, body
}

func tokenForm(grant, key, value string) url.Values {
	a := DevApplication()
	return url.Values{
		"grant_type": {grant}, key: {value}, "client_id": {a.ClientID}, "client_secret": {a.ClientSecret}, "redirect_uri": {redirect},
	}
}

// grant signs in and exchanges the code: the access and the refresh token.
func (s *sim) grant() (access, refresh string) {
	s.t.Helper()
	_, _, back := s.signIn(signInQuery())
	status, _, body := s.post(tokenForm("authorization_code", "code", back.Get("code")))
	if status != http.StatusOK {
		s.t.Fatalf("exchange: %d %v", status, body)
	}
	return body["access_token"].(string), body["refresh_token"].(string)
}

// TestSignIn: the sign-in comes back to the redirect address with a code and the state;
// Ford's rule on the address, the application, the response type.
func TestSignIn(t *testing.T) {
	s := newSim(t)
	status, location, back := s.signIn(signInQuery())
	if status != http.StatusFound || back.Get("code") == "" || back.Get("state") != "0123456789abcdef" {
		t.Fatalf("sign-in: %d %v", status, back)
	}
	if !strings.HasPrefix(location, redirect+"?") {
		t.Errorf("came back to %s", location)
	}
	for name, change := range map[string][2]string{
		"another application":     {"client_id", "someone-else"},
		"http on a local network": {"redirect_uri", "http://192.168.1.10:8080/auth/ford/callback"},
		"no redirect address":     {"redirect_uri", ""},
		"another response type":   {"response_type", "token"},
	} {
		q := signInQuery()
		q.Set(change[0], change[1])
		if status, _, _ := s.signIn(q); status != http.StatusBadRequest {
			t.Errorf("%s: %d", name, status)
		}
	}
	q := signInQuery()
	q.Set("redirect_uri", "https://runsten.example/auth/ford/callback")
	if status, _, _ := s.signIn(q); status != http.StatusFound {
		t.Errorf("https: %d", status)
	}
}

// TestTokens: a code is exchanged once, with the application's secret and the sign-in's
// redirect address; a refresh gives a new refresh token, the previous one refused; the
// ID token tells when the owner consented.
func TestTokens(t *testing.T) {
	s := newSim(t)
	_, _, back := s.signIn(signInQuery())
	code := back.Get("code")
	wrong := tokenForm("authorization_code", "code", code)
	wrong.Set("client_secret", "nope")
	if status, _, body := s.post(wrong); status != http.StatusUnauthorized || body["error"] != "invalid_client" {
		t.Errorf("a wrong secret: %d %v", status, body)
	}
	status, _, body := s.post(tokenForm("authorization_code", "code", code))
	if status != http.StatusOK || body["expires_in"] != 1200.0 {
		t.Fatalf("exchange: %d %v", status, body)
	}
	claims, _ := base64.RawURLEncoding.DecodeString(strings.Split(body["id_token"].(string), ".")[1])
	var c struct {
		AuthTime int64 `json:"auth_time"`
	}
	if json.Unmarshal(claims, &c) != nil || c.AuthTime != s.clk.Now().Unix() {
		t.Errorf("id token claims %s", claims)
	}
	if status, _, body := s.post(tokenForm("authorization_code", "code", code)); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("a code used twice: %d %v", status, body)
	}
	first := body["refresh_token"].(string)
	status, _, body = s.post(tokenForm("refresh_token", "refresh_token", first))
	if status != http.StatusOK || body["refresh_token"] == first {
		t.Fatalf("refresh: %d %v", status, body)
	}
	if status, _, _ := s.post(tokenForm("refresh_token", "refresh_token", first)); status != http.StatusBadRequest {
		t.Errorf("the refresh token rotated away still refreshes: %d", status)
	}
	// Another redirect address than the sign-in's.
	_, _, back = s.signIn(signInQuery())
	other := tokenForm("authorization_code", "code", back.Get("code"))
	other.Set("redirect_uri", "https://elsewhere.example/callback")
	if status, _, body := s.post(other); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("another redirect address: %d %v", status, body)
	}
	// The grant's year over.
	s.clk.Advance(366 * 24 * time.Hour)
	if status, _, body := s.post(tokenForm("refresh_token", "refresh_token", body["refresh_token"].(string))); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("a grant a year old: %d %v", status, body)
	}
}

// TestGarageAndTelemetry: a grant reads its one car; each sign-in is given the next.
func TestGarageAndTelemetry(t *testing.T) {
	s := newSim(t)
	access, _ := s.grant()
	status, _, g := s.get("garage", access)
	if status != http.StatusOK || g["vin"] != "WF0TESTPUMA000001" || g["modelName"] != "Puma" || g["modelYear"] != "2025" || g["engineType"] != "BEV" {
		t.Fatalf("garage: %d %v", status, g)
	}
	s.clk.Advance(time.Minute)
	status, _, tel := s.get("telemetry", access)
	if status != http.StatusOK || tel["vin"] != "WF0TESTPUMA000001" {
		t.Fatalf("telemetry: %d %v", status, tel)
	}
	m := tel["metrics"].(map[string]any)
	value := func(name string) any { return m[name].(map[string]any)["value"] }
	if value("ignitionStatus") != "OFF" || value("odometer") != 4804.0 || value("xevBatteryStateOfCharge") != 61.5 {
		t.Errorf("metrics: %v %v %v", value("ignitionStatus"), value("odometer"), value("xevBatteryStateOfCharge"))
	}
	if value("xevPlugChargerStatus") != "DISCONNECTED" || value("xevBatteryChargeDisplayStatus") != "NOT_READY" {
		t.Errorf("charge: %v %v", value("xevPlugChargerStatus"), value("xevBatteryChargeDisplayStatus"))
	}
	if value("xevBatteryChargerVoltageOutput") != 5.0 || m["tirePressure"] == nil || m["xevChargeStationPowerType"] != nil {
		t.Errorf("a rich model parked: %v", m)
	}
	if at := m["odometer"].(map[string]any)["updateTime"]; at != "2026-11-11T17:00:00Z" {
		t.Errorf("odometer sent at %v: parked, only at a change", at)
	}
	lock := m["doorLockStatus"].([]any)[0].(map[string]any)
	if lock["value"] != "LOCKED" || lock["vehicleDoor"] != "ALL_DOORS" {
		t.Errorf("lock %v", lock)
	}

	// Charging: the status, the station's kind, the charger's output, each minute.
	s.rep.Energy.PluggedIn, s.rep.Energy.Charging = true, vehicle.Charging
	s.rep.Energy.ChargeType, s.rep.Energy.ChargingPowerW = vehicle.AC, 10500
	s.clk.Advance(5*time.Minute + 20*time.Second)
	_, _, tel = s.get("telemetry", access)
	m = tel["metrics"].(map[string]any)
	if value("xevBatteryChargeDisplayStatus") != "IN_PROGRESS" || value("xevChargeStationPowerType") != "AC_BASIC" ||
		value("xevBatteryChargerVoltageOutput") != 350.0 || value("xevBatteryChargerCurrentOutput") != 30.0 {
		t.Errorf("charging: %v", m)
	}
	if at := m["xevBatteryStateOfCharge"].(map[string]any)["updateTime"]; at != "2026-11-11T18:06:00Z" {
		t.Errorf("level sent at %v: charging, every minute", at)
	}

	// The second sign-in reads the second car, a model that tells less.
	other, _ := s.grant()
	_, _, g = s.get("garage", other)
	if g["vin"] != "WF0TESTEXPL000002" {
		t.Errorf("the second grant's car: %v", g["vin"])
	}
	s.clk.Advance(time.Minute)
	_, _, tel = s.get("telemetry", other)
	m = tel["metrics"].(map[string]any)
	for _, name := range []string{"speed", "outsideTemperature", "tirePressure", "xevBatteryChargerCurrentOutput"} {
		if m[name] != nil {
			t.Errorf("an Explorer tells %s", name)
		}
	}
	if m["odometer"] == nil || m["xevBatteryStateOfCharge"] == nil || m["position"] == nil {
		t.Errorf("an Explorer's metrics: %v", m)
	}
}

// TestLimits: no access, an access over, two calls close together, a limit set, a grant
// withdrawn, the sign-in's outage.
func TestLimits(t *testing.T) {
	s := newSim(t)
	if status, _, _ := s.get("telemetry", ""); status != http.StatusUnauthorized {
		t.Errorf("no access: %d", status)
	}
	access, refresh := s.grant()
	if status, _, _ := s.get("telemetry", access); status != http.StatusOK {
		t.Fatalf("telemetry: %d", status)
	}
	s.clk.Advance(4 * time.Second)
	status, header, _ := s.get("garage", access)
	if status != http.StatusTooManyRequests || header.Get("Retry-After") != "6" {
		t.Errorf("a call 4 s after another: %d, Retry-After %q", status, header.Get("Retry-After"))
	}
	s.clk.Advance(6 * time.Second)
	if status, _, _ := s.get("garage", access); status != http.StatusOK {
		t.Errorf("a call past the gap: %d", status)
	}

	s.h.Limit(5 * time.Minute)
	s.clk.Advance(time.Minute)
	if status, header, _ := s.get("telemetry", access); status != http.StatusTooManyRequests || header.Get("Retry-After") != "240" {
		t.Errorf("limited: %d, Retry-After %q", status, header.Get("Retry-After"))
	}
	if status, _, _ := s.post(tokenForm("refresh_token", "refresh_token", refresh)); status != http.StatusTooManyRequests {
		t.Errorf("the token endpoint, limited: %d", status)
	}
	s.clk.Advance(5 * time.Minute)

	s.h.Outage(time.Hour)
	status, _, body := s.post(tokenForm("refresh_token", "refresh_token", refresh))
	if status != http.StatusBadRequest || !strings.Contains(body["error_description"].(string), "AADB2C90027") {
		t.Errorf("outage: %d %v", status, body)
	}
	if status, _, _ := s.get("telemetry", access); status != http.StatusOK {
		t.Errorf("the API during the sign-in's outage: %d", status)
	}
	s.clk.Advance(time.Hour)
	if status, _, _ := s.get("telemetry", access); status != http.StatusUnauthorized {
		t.Errorf("an access an hour old: %d", status)
	}
	status, _, body = s.post(tokenForm("refresh_token", "refresh_token", refresh))
	if status != http.StatusOK {
		t.Fatalf("refresh after the outage: %d %v", status, body)
	}
	access, refresh = body["access_token"].(string), body["refresh_token"].(string)

	s.h.Revoke("WF0TESTPUMA000001")
	if status, _, _ := s.get("telemetry", access); status != http.StatusUnauthorized {
		t.Errorf("a grant withdrawn reads: %d", status)
	}
	if status, _, body := s.post(tokenForm("refresh_token", "refresh_token", refresh)); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("a grant withdrawn refreshes: %d %v", status, body)
	}
}
