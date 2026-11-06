package bmwapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const vin = "WBY31AW02RFR00001"

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

// fakeSource is a car whose state the test sets.
type fakeSource struct {
	brand string
	spec  vehicle.Spec
	st    vehicle.State
}

func (f *fakeSource) VIN() string   { return vin }
func (f *fakeSource) Brand() string { return f.brand }
func (f *fakeSource) ReportAt(time.Time) vehicle.Report {
	return vehicle.Report{Spec: f.spec}
}
func (f *fakeSource) StateAt(time.Time) vehicle.State { return f.st }

// parked is an i4 parked and unplugged.
func parked() *fakeSource {
	return &fakeSource{
		brand: "bmw",
		spec:  vehicle.Spec{Model: "i4 eDrive40", ModelYear: 2024, BatteryKWh: 83.9, UsableKWh: 80.7, ConsumptionKWhPer100km: 16},
		st: vehicle.State{
			SoC: 64.2, OdometerKm: 21034.6, TargetSoC: 80,
			Position: vehicle.Position{Lat: 50.8270, Lon: 4.3720, Heading: 91},
		},
	}
}

type server struct {
	*httptest.Server
	clk    *clock.Manual
	h      *Handler
	src    *fakeSource
	stream *Stream
}

func newServer(t *testing.T) *server {
	t.Helper()
	src := parked()
	clk := clock.NewManual(t0)
	h := NewHandler([]Source{src}, clk, DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &server{Server: srv, clk: clk, h: h, src: src}
}

// call sends a request and returns its status and JSON body.
func (s *server) call(t *testing.T, method, path, tok string, body any, header map[string]string) (int, any) {
	t.Helper()
	var r *bytes.Reader
	switch b := body.(type) {
	case nil:
		r = bytes.NewReader(nil)
	case url.Values:
		r = bytes.NewReader([]byte(b.Encode()))
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, s.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body.(url.Values); ok {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out any
	if resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode, out
}

// get calls the API with its version header.
func (s *server) get(t *testing.T, tok, path string) (int, any) {
	t.Helper()
	return s.call(t, http.MethodGet, path, tok, nil, map[string]string{"x-version": "v1"})
}

const verifier = "a-code-verifier-of-at-least-forty-three-characters-long"

func challenge() string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// deviceCode asks for a device code with the scopes.
func (s *server) deviceCode(t *testing.T, scope string) string {
	t.Helper()
	status, body := s.call(t, http.MethodPost, AuthPrefix+"device/code", "", url.Values{
		"client_id": {DevClientID}, "response_type": {"device_code"}, "scope": {scope},
		"code_challenge": {challenge()}, "code_challenge_method": {"S256"},
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("device code: %d %v", status, body)
	}
	m := body.(map[string]any)
	if m["user_code"] == "" || m["interval"] != 5.0 || !strings.HasPrefix(m["verification_uri_complete"].(string), verifyAddress) {
		t.Fatalf("device code: %v", m)
	}
	return m["device_code"].(string)
}

// poll polls the token endpoint for a device code.
func (s *server) poll(t *testing.T, device, codeVerifier string) (int, map[string]any) {
	t.Helper()
	status, body := s.call(t, http.MethodPost, AuthPrefix+"token", "", url.Values{
		"client_id": {DevClientID}, "grant_type": {deviceGrant}, "device_code": {device}, "code_verifier": {codeVerifier},
	}, nil)
	return status, body.(map[string]any)
}

const allScopes = "authenticate_user openid " + ScopeStream + " " + ScopeAPI

// signIn runs the device flow to its tokens.
func (s *server) signIn(t *testing.T, scope string) map[string]any {
	t.Helper()
	device := s.deviceCode(t, scope)
	s.clk.Advance(DefaultBehavior().ApproveAfter)
	status, body := s.poll(t, device, verifier)
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, body)
	}
	return body
}

func TestDeviceFlow(t *testing.T) {
	s := newServer(t)
	device := s.deviceCode(t, allScopes)
	if status, body := s.poll(t, device, verifier); status != http.StatusBadRequest || body["error"] != "authorization_pending" {
		t.Fatalf("before approval: %d %v", status, body)
	}
	s.clk.Advance(time.Second)
	if _, body := s.poll(t, device, verifier); body["error"] != "slow_down" {
		t.Fatalf("polled too fast: %v", body)
	}
	s.clk.Advance(10 * time.Second)
	status, body := s.poll(t, device, verifier)
	if status != http.StatusOK || body["gcid"] != GCID || body["expires_in"] != 3600.0 || body["scope"] != allScopes {
		t.Fatalf("approved: %d %v", status, body)
	}
	for _, k := range []string{"access_token", "id_token", "refresh_token"} {
		if body[k] == "" {
			t.Errorf("no %s", k)
		}
	}
	if _, body := s.poll(t, device, verifier); body["error"] != "invalid_grant" {
		t.Errorf("used twice: %v", body)
	}
}

func TestDeviceFlowRefusals(t *testing.T) {
	s := newServer(t)
	device := s.deviceCode(t, allScopes)
	s.clk.Advance(time.Minute)
	if _, body := s.poll(t, device, "another-verifier"); body["error"] != "invalid_grant" {
		t.Errorf("wrong verifier: %v", body)
	}
	device = s.deviceCode(t, allScopes)
	s.clk.Advance(6 * time.Minute)
	if _, body := s.poll(t, device, verifier); body["error"] != "expired_token" {
		t.Errorf("expired: %v", body)
	}
	status, body := s.call(t, http.MethodPost, AuthPrefix+"device/code", "", url.Values{
		"client_id": {"another"}, "response_type": {"device_code"}, "code_challenge": {challenge()}, "code_challenge_method": {"S256"},
	}, nil)
	if status != http.StatusUnauthorized || body.(map[string]any)["error"] != "invalid_client" {
		t.Errorf("unknown client: %d %v", status, body)
	}
}

func (s *server) refresh(t *testing.T, tok string) (int, map[string]any) {
	t.Helper()
	status, body := s.call(t, http.MethodPost, AuthPrefix+"token", "", url.Values{
		"client_id": {DevClientID}, "grant_type": {"refresh_token"}, "refresh_token": {tok},
	}, nil)
	return status, body.(map[string]any)
}

func TestRefreshRotates(t *testing.T) {
	s := newServer(t)
	first := s.signIn(t, allScopes)["refresh_token"].(string)
	status, body := s.refresh(t, first)
	if status != http.StatusOK || body["refresh_token"] == first || body["scope"] != "openid authenticate_user "+ScopeAPI+" "+ScopeStream {
		t.Fatalf("refresh: %d %v", status, body)
	}
	if _, again := s.refresh(t, first); again["error"] != "invalid_grant" {
		t.Errorf("the rotated token refreshed again: %v", again)
	}
	second := body["refresh_token"].(string)
	s.clk.Advance(15 * 24 * time.Hour)
	if _, expired := s.refresh(t, second); expired["error"] != "invalid_grant" {
		t.Errorf("a refresh token of 15 days refreshed: %v", expired)
	}
}

func TestRevoke(t *testing.T) {
	s := newServer(t)
	tok := s.signIn(t, allScopes)["refresh_token"].(string)
	if status, _ := s.call(t, http.MethodPost, ControlPrefix+"revoke", "", nil, nil); status != http.StatusNoContent {
		t.Fatal(status)
	}
	if _, body := s.refresh(t, tok); body["error"] != "invalid_grant" {
		t.Errorf("revoked: %v", body)
	}
}

func TestAPIRefusals(t *testing.T) {
	s := newServer(t)
	tok := s.signIn(t, allScopes)["access_token"].(string)
	code := func(body any) any { return body.(map[string]any)["exveErrorId"] }
	if status, body := s.call(t, http.MethodGet, APIPrefix+"vehicles/mappings", tok, nil, nil); status != http.StatusBadRequest {
		t.Errorf("without x-version: %d %v", status, body)
	}
	if status, body := s.get(t, "", APIPrefix+"vehicles/mappings"); status != http.StatusUnauthorized || code(body) != "CU-100" {
		t.Errorf("without a token: %d %v", status, body)
	}
	if status, body := s.get(t, tok, APIPrefix+"vehicles/WBY00000000000000/basicData"); status != http.StatusForbidden || code(body) != "CU-104" {
		t.Errorf("another VIN: %d %v", status, body)
	}
	s.clk.Advance(time.Hour)
	if status, body := s.get(t, tok, APIPrefix+"vehicles/mappings"); status != http.StatusUnauthorized || code(body) != "CU-102" {
		t.Errorf("expired: %d %v", status, body)
	}
	streamOnly := s.signIn(t, "openid "+ScopeStream)["access_token"].(string)
	if status, body := s.get(t, streamOnly, APIPrefix+"vehicles/mappings"); status != http.StatusForbidden || code(body) != "CU-103" {
		t.Errorf("without the API's scope: %d %v", status, body)
	}
}

func TestQuota(t *testing.T) {
	s := newServer(t)
	tok := s.signIn(t, allScopes)["access_token"].(string)
	for i := range DailyQuota {
		if status, body := s.get(t, tok, APIPrefix+"vehicles/mappings"); status != http.StatusOK {
			t.Fatalf("call %d: %d %v", i+1, status, body)
		}
	}
	status, body := s.get(t, tok, APIPrefix+"vehicles/mappings")
	if status != http.StatusTooManyRequests || body.(map[string]any)["exveErrorId"] != "CU-429" {
		t.Fatalf("call 51: %d %v", status, body)
	}
	s.clk.Advance(18 * time.Hour) // past midnight UTC, the token's hour long gone
	tok = s.signIn(t, allScopes)["access_token"].(string)
	if status, _ := s.get(t, tok, APIPrefix+"vehicles/mappings"); status != http.StatusOK {
		t.Errorf("the next day: %d", status)
	}
	if status, _ := s.call(t, http.MethodPost, ControlPrefix+"quota?used=50", "", nil, nil); status != http.StatusNoContent {
		t.Fatal(status)
	}
	if status, _ := s.get(t, tok, APIPrefix+"vehicles/mappings"); status != http.StatusTooManyRequests {
		t.Errorf("quota set as spent: %d", status)
	}
}

func TestVehicles(t *testing.T) {
	s := newServer(t)
	tok := s.signIn(t, allScopes)["access_token"].(string)
	_, body := s.get(t, tok, APIPrefix+"vehicles/mappings")
	if m := body.([]any); len(m) != 1 || m[0].(map[string]any)["vin"] != vin || m[0].(map[string]any)["mappingType"] != "PRIMARY" {
		t.Errorf("mappings: %v", body)
	}
	_, body = s.get(t, tok, APIPrefix+"vehicles/"+vin+"/basicData")
	b := body.(map[string]any)
	if b["brand"] != "BMWi" || b["modelName"] != "i4 eDrive40" || b["series"] != "i4" || b["propulsionType"] != "BEV" || b["hvsMaxEnergyAbsolute"] != "83.9" {
		t.Errorf("basic data: %v", b)
	}
	s.src.brand = "mini"
	if _, body := s.get(t, tok, APIPrefix+"vehicles/"+vin+"/basicData"); body.(map[string]any)["brand"] != "MINI" {
		t.Errorf("a Mini: %v", body)
	}
}

func TestContainers(t *testing.T) {
	s := newServer(t)
	tok := s.signIn(t, allScopes)["access_token"].(string)
	create := func(descriptors []string) (int, map[string]any) {
		status, body := s.call(t, http.MethodPost, APIPrefix+"containers", tok,
			map[string]any{"name": "runsten", "purpose": "logger", "technicalDescriptors": descriptors}, nil)
		return status, body.(map[string]any)
	}
	if status, body := create([]string{"vehicle.unknown"}); status != http.StatusBadRequest || body["exveErrorId"] != "CU-402" {
		t.Errorf("an unknown key: %d %v", status, body)
	}
	status, ct := create([]string{DescSoC, DescOdometer, DescLatitude})
	if status != http.StatusCreated || len(ct["containerId"].(string)) != 13 || ct["state"] != "ACTIVE" {
		t.Fatalf("created: %d %v", status, ct)
	}
	id := ct["containerId"].(string)

	_, body := s.get(t, tok, APIPrefix+"vehicles/"+vin+"/telematicData?containerId="+id)
	data := body.(map[string]any)["telematicData"].(map[string]any)
	soc := data[DescSoC].(map[string]any)
	if len(data) != 3 || soc["value"] != "64.2" || soc["unit"] != "%" || soc["timestamp"] != "2026-09-28T06:00:10.000Z" {
		t.Errorf("telematic data: %v", data)
	}
	if odo := data[DescOdometer].(map[string]any); odo["value"] != "21034" || odo["unit"] != "km" {
		t.Errorf("odometer: %v", odo)
	}

	_, body = s.get(t, tok, APIPrefix+"containers")
	if list := body.(map[string]any)["containers"].([]any); len(list) != 1 || list[0].(map[string]any)["containerId"] != id {
		t.Errorf("list: %v", body)
	}
	if status, _ := s.call(t, http.MethodDelete, APIPrefix+"containers/"+id, tok, nil, nil); status != http.StatusNoContent {
		t.Errorf("delete: %d", status)
	}
	if status, _ := s.get(t, tok, APIPrefix+"vehicles/"+vin+"/telematicData?containerId="+id); status != http.StatusNotFound {
		t.Errorf("a deleted container read: %d", status)
	}
	for i := range maxContainers {
		if status, _ := create([]string{DescSoC}); status != http.StatusCreated {
			t.Fatalf("container %d: %d", i+1, status)
		}
	}
	if status, body := create([]string{DescSoC}); status != http.StatusBadRequest || body["exveErrorId"] != "CU-124" {
		t.Errorf("an eleventh: %d %v", status, body)
	}
}

// sent returns the car's last value of a descriptor, and when it was sent.
func (s *server) sent(d string) (any, time.Time) {
	s.h.mu.Lock()
	defer s.h.mu.Unlock()
	k := s.h.cars[0].known[d]
	return k.val.v, k.at
}

func TestSendEvery(t *testing.T) {
	s := newServer(t)
	s.h.observe(s.clk.Now())
	s.src.st.Driving, s.src.st.SpeedKmh = true, 50
	for range 6 { // a minute of driving, read every 10 s
		s.clk.Advance(10 * time.Second)
		s.src.st.OdometerKm += 0.14
		s.src.st.Position.Lat += 0.001
		s.h.observe(s.clk.Now())
	}
	if v, at := s.sent(DescIsMoving); v != "ASN_isTrue" || !at.Equal(t0.Add(10*time.Second)) {
		t.Errorf("moving sent at once: %v at %v", v, at)
	}
	lat, latAt := s.sent(DescLatitude)
	_, lonAt := s.sent(DescLongitude)
	if !latAt.Equal(t0.Add(time.Minute)) || !lonAt.Equal(latAt) || lat != 50.833 {
		t.Errorf("position: %v at %v, longitude at %v", lat, latAt, lonAt)
	}
	if odo, at := s.sent(DescOdometer); odo != 21035 || !at.Equal(t0.Add(30*time.Second)) {
		t.Errorf("odometer: %v at %v", odo, at)
	}
}

func TestChargingHistory(t *testing.T) {
	s := newServer(t)
	st := &s.src.st
	st.PluggedIn, st.Charging, st.ChargeType, st.ChargingPowerW = true, vehicle.Charging, vehicle.AC, 11000
	s.h.observe(s.clk.Now())
	if v, _ := s.sent(DescChargingMethod); v != "AC_TYPE2PLUG" {
		t.Errorf("method: %v", v)
	}
	if v, _ := s.sent(DescPhaseNumber); v != "3-PHASES" {
		t.Errorf("phases: %v", v)
	}
	for range 3 { // read along the charge: still one session
		s.clk.Advance(15 * time.Minute)
		st.SoC += 3
		s.h.observe(s.clk.Now())
	}
	s.clk.Advance(15 * time.Minute)
	st.SoC, st.Charging, st.ChargingPowerW, st.ChargeAddedKWh = 77.8, vehicle.Done, 0, 11
	s.h.observe(s.clk.Now())
	if v, _ := s.sent(DescHVStatus); v != "FINISHED_NOT_FULL" {
		t.Errorf("hv status: %v", v)
	}
	tok := s.signIn(t, allScopes)["access_token"].(string)
	q := "?from=" + url.QueryEscape(t0.Add(-24*time.Hour).Format(time.RFC3339)) + "&to=" + url.QueryEscape(t0.Add(24*time.Hour).Format(time.RFC3339))
	status, body := s.get(t, tok, APIPrefix+"vehicles/"+vin+"/chargingHistory"+q)
	data, _ := body.(map[string]any)["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("sessions: %d %v", status, body)
	}
	c := data[0].(map[string]any)
	if c["startTime"] != float64(t0.Unix()) || c["displayedStartSoc"] != 64.0 || c["displayedSoc"] != 77.0 ||
		c["energyConsumedFromPowerGridKwh"] != 12.22 || c["mileage"] != 21034.0 || c["totalChargingDurationSec"] != 3600.0 {
		t.Errorf("session: %v", c)
	}
}

func TestDCCharge(t *testing.T) {
	s := newServer(t)
	st := &s.src.st
	st.PluggedIn, st.Charging, st.ChargeType, st.ChargingPowerW = true, vehicle.Charging, vehicle.DC, 150000
	s.h.observe(s.clk.Now())
	for d, want := range map[string]any{
		DescChargingMethod: "DC", DescPortDCStatus: "CONNECTED", DescChargingPower: 150000, DescACAmpere: 0, DescPhaseNumber: "NO_CHARGING",
	} {
		if v, _ := s.sent(d); v != want {
			t.Errorf("%s = %v, want %v", d, v, want)
		}
	}
}

func TestDuplicateVIN(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	NewHandler([]Source{parked(), parked()}, clock.NewManual(t0), DefaultBehavior())
}
