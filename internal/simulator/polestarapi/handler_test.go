package polestarapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const (
	vin         = "LPSVSEDE5RL000001"
	fixturesDir = "../../../testdata/polestar-data-portal"
)

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

type fakeSource struct{ rep vehicle.Report }

func (f *fakeSource) VIN() string                       { return vin }
func (f *fakeSource) ReportAt(time.Time) vehicle.Report { return f.rep }

// parked is a car parked and unplugged since t0 - 2 h, as the real one was.
func parked() vehicle.Report {
	st := vehicle.State{
		SoC: 52.6, OdometerKm: 23832.4, Locked: true, TargetSoC: 80,
		Position:        vehicle.Position{Lat: 57.7210, Lon: 12.0400, Heading: 63.4},
		LastTrip:        vehicle.Trip{DistanceKm: 9, Duration: 22 * time.Minute, EnergyKWh: 1.7},
		TotalDistanceKm: 32.4, TotalEnergyKWh: 6.2, TotalDriveTime: 50 * time.Minute,
	}
	caps, _ := vehicle.Profile("bev-generic")
	at := t0.Add(-2 * time.Hour)
	return vehicle.Report{
		Spec: vehicle.Spec{
			Model: "Polestar 2", ModelYear: 2024, BatteryKWh: 82, UsableKWh: 79,
			ConsumptionKWhPer100km: 19, Capabilities: caps,
		},
		Energy: st, EnergyAt: at, Status: st, StatusAt: at, Location: st.Position, LocationAt: at,
	}
}

func charging(power float64) vehicle.Report {
	r := parked()
	r.Energy.PluggedIn, r.Energy.Charging, r.Energy.ChargeType, r.Energy.ChargingPowerW = true, vehicle.Charging, vehicle.DC, power
	r.EnergyAt = t0.Add(-time.Minute)
	return r
}

type server struct {
	*httptest.Server
	clk *clock.Manual
	src *fakeSource
}

func newServer(t *testing.T, rep vehicle.Report, limits Limits) *server {
	t.Helper()
	clk := clock.NewManual(t0)
	src := &fakeSource{rep}
	srv := httptest.NewServer(NewHandler([]Source{src}, clk, DevCredential(), limits, DefaultBehavior()))
	t.Cleanup(srv.Close)
	return &server{srv, clk, src}
}

// token asks for a token with the development credential.
func (s *server) token(t *testing.T) string {
	t.Helper()
	status, body := s.post(t, `{"clientId":"runsten-dev","clientSecret":"runsten-dev-secret"}`)
	if status != http.StatusOK {
		t.Fatalf("token: status %d, %v", status, body)
	}
	return body["accessToken"].(string)
}

func (s *server) post(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL+Prefix+"token", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return do(t, req)
}

// get calls path under the prefix with token, and the account in x-client-id unless
// account is empty.
func (s *server) get(t *testing.T, path, token, account string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL+Prefix+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if account != "" {
		req.Header.Set("x-client-id", account)
	}
	return do(t, req)
}

func do(t *testing.T, req *http.Request) (int, map[string]any) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("%s: invalid JSON: %v", req.URL.Path, err)
	}
	return resp.StatusCode, body
}

var account = DevCredential().AccountID

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, name)) //nolint:gosec // repository file
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// sameShape checks that got has the same keys and JSON types as want.
func sameShape(path string, want, got any) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: expected object, got %T", path, got)}
		}
		var diffs []string
		for k := range w {
			if _, ok := g[k]; !ok {
				diffs = append(diffs, fmt.Sprintf("%s.%s: missing key", path, k))
			}
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				diffs = append(diffs, fmt.Sprintf("%s.%s: extra key", path, k))
			}
		}
		for k, wv := range w {
			if gv, ok := g[k]; ok {
				diffs = append(diffs, sameShape(path+"."+k, wv, gv)...)
			}
		}
		sort.Strings(diffs)
		return diffs
	case []any:
		g, ok := got.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: expected array, got %T", path, got)}
		}
		if len(w) > 0 && len(g) > 0 {
			return sameShape(path+"[0]", w[0], g[0])
		}
		return nil
	default:
		if fmt.Sprintf("%T", want) != fmt.Sprintf("%T", got) {
			return []string{fmt.Sprintf("%s: expected type %T, got %T", path, want, got)}
		}
		return nil
	}
}

// TestShapeMatchesRealAPI compares each simulated response with the real response of
// a parked Polestar 2.
func TestShapeMatchesRealAPI(t *testing.T) {
	s := newServer(t, parked(), Limits{})
	tok := s.token(t)
	cases := []struct{ path, fixture string }{
		{"v1/vehicles", "vehicles.json"},
		{"v1/vehicles/" + vin + "/telemetry/battery", "telemetry/battery.json"},
		{"v1/vehicles/" + vin + "/telemetry/odometer", "telemetry/odometer.json"},
		{"v1/vehicles/" + vin + "/telemetry/location", "telemetry/location.json"},
		{"v1/vehicles/" + vin + "/telemetry/availability", "telemetry/availability.json"},
		{"v1/vehicles/" + vin + "/charging/target-soc", "charging/target-soc.json"},
		{"v1/vehicles/" + vin + "/charging/is-at-charge-location", "charging/is-at-charge-location.json"},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			_, got := s.get(t, c.path, tok, account)
			want := fixture(t, c.fixture)
			if e, ok := want["error"].(map[string]any); ok { // an error's request ID and time vary
				delete(e, "requestId")
				delete(got["error"].(map[string]any), "requestId")
			}
			if diffs := sameShape("$", want, got); len(diffs) > 0 {
				t.Errorf("shape differs from the real response:\n%s", strings.Join(diffs, "\n"))
			}
		})
	}
	t.Run("token.json", func(t *testing.T) {
		_, got := s.post(t, `{"clientId":"runsten-dev","clientSecret":"runsten-dev-secret"}`)
		if diffs := sameShape("$", fixture(t, "token.json"), got); len(diffs) > 0 {
			t.Errorf("shape differs from the real response:\n%s", strings.Join(diffs, "\n"))
		}
	})
}

func TestToken(t *testing.T) {
	s := newServer(t, parked(), DefaultLimits())
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"json", `{"clientId":"runsten-dev","clientSecret":"runsten-dev-secret","scope":"pdp-telemetry/battery"}`, 200, ""},
		{"wrong secret", `{"clientId":"runsten-dev","clientSecret":"nope"}`, 401, "invalid_client"},
		{"missing secret", `{"clientId":"runsten-dev"}`, 400, "invalid_request"},
		{"not json", `{`, 400, "invalid_request"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, body := s.post(t, c.body)
			if status != c.status || (c.code != "" && body["error"] != c.code) {
				t.Errorf("status %d %v, want %d %s", status, body, c.status, c.code)
			}
			if c.status == 200 && (body["expiresIn"] != 3600.0 || body["tokenType"] != "Bearer") {
				t.Errorf("token %v", body)
			}
		})
	}
	t.Run("form", func(t *testing.T) {
		form := url.Values{"clientId": {"runsten-dev"}, "clientSecret": {"runsten-dev-secret"}}
		resp, err := http.PostForm(s.URL+Prefix+"token", form) //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status %d", resp.StatusCode)
		}
	})
}

func TestCallsAreChecked(t *testing.T) {
	s := newServer(t, parked(), DefaultLimits())
	tok := s.token(t)
	battery := "v1/vehicles/" + vin + "/telemetry/battery"
	cases := []struct {
		name, path, token, account string
		status                     int
		code                       string
	}{
		{"ok", battery, tok, account, 200, ""},
		{"no token", battery, "", account, 401, "AUTHN_INVALID_TOKEN"},
		{"unknown token", battery, "forged", account, 401, "AUTHN_INVALID_TOKEN"},
		{"no x-client-id", battery, tok, "", 400, "VALIDATION_INVALID_PARAMETER"},
		{"client ID as x-client-id", battery, tok, "runsten-dev", 403, "AUTHZ_CLIENT_ID_MISMATCH"},
		{"another VIN", "v1/vehicles/LPSVSEDE5RL000002/telemetry/battery", tok, account, 403, "AUTHZ_VIN_UNAUTHORIZED"},
		{"a domain not uploaded", "v1/vehicles/" + vin + "/telemetry/health", tok, account, 404, "DATA_NOT_AVAILABLE"},
		{"an unknown domain", "v1/vehicles/" + vin + "/telemetry/tyres", tok, account, 404, "VALIDATION_RESOURCE_NOT_FOUND"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, body := s.get(t, c.path, c.token, c.account)
			code := ""
			if e, ok := body["error"].(map[string]any); ok {
				code, _ = e["code"].(string)
			}
			if status != c.status || code != c.code {
				t.Errorf("got %d %q, want %d %q", status, code, c.status, c.code)
			}
		})
	}
	t.Run("outside the API", func(t *testing.T) {
		resp, err := http.Get(s.URL + "/somewhere") //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status %d, want 403", resp.StatusCode)
		}
	})
}

func TestTokenExpires(t *testing.T) {
	s := newServer(t, parked(), DefaultLimits())
	tok := s.token(t)
	s.clk.Advance(59 * time.Minute)
	if status, _ := s.get(t, "v1/vehicles", tok, account); status != 200 {
		t.Fatalf("before expiry: %d", status)
	}
	s.clk.Advance(time.Minute)
	if status, _ := s.get(t, "v1/vehicles", tok, account); status != 401 {
		t.Errorf("after expiry: %d, want 401", status)
	}
	if status, _ := s.get(t, "v1/vehicles", s.token(t), account); status != 200 {
		t.Errorf("with a new token: %d", status)
	}
}

func TestCredentialExpires(t *testing.T) {
	l := DefaultLimits()
	l.CredentialTTL = 90 * 24 * time.Hour
	s := newServer(t, parked(), l)
	s.clk.Advance(l.CredentialTTL)
	if status, body := s.post(t, `{"clientId":"runsten-dev","clientSecret":"runsten-dev-secret"}`); status != 401 {
		t.Errorf("expired credential: %d %v, want 401", status, body)
	}
}

func TestLimits(t *testing.T) {
	s := newServer(t, parked(), Limits{PerMinute: 3, DailyQuota: 5, TokenTTL: time.Hour})
	tok := s.token(t)
	statuses := func(n int) []int {
		var out []int
		for range n {
			status, _ := s.get(t, "v1/vehicles", tok, account)
			out = append(out, status)
		}
		return out
	}
	if got := fmt.Sprint(statuses(4)); got != "[200 200 200 429]" {
		t.Errorf("per minute: %s", got)
	}
	s.clk.Advance(time.Minute)
	if got := fmt.Sprint(statuses(3)); got != "[200 200 429]" {
		t.Errorf("per day: %s", got)
	}
	s.clk.Advance(24 * time.Hour)
	tok = s.token(t)
	if got := fmt.Sprint(statuses(1)); got != "[200]" {
		t.Errorf("the next day: %s", got)
	}
}

func data(t *testing.T, s *server, tok, domain string) map[string]any {
	t.Helper()
	kind := "telemetry"
	if domain == "target-soc" {
		kind = "charging"
	}
	status, body := s.get(t, "v1/vehicles/"+vin+"/"+kind+"/"+domain, tok, account)
	if status != http.StatusOK {
		t.Fatalf("%s: status %d, %v", domain, status, body)
	}
	return body["data"].(map[string]any)
}

func seconds(d map[string]any) string { return d["timestamp"].(map[string]any)["seconds"].(string) }

func TestValues(t *testing.T) {
	s := newServer(t, parked(), Limits{})
	tok := s.token(t)

	b := data(t, s, tok, "battery")
	for k, want := range map[string]any{
		"batteryChargeLevelPercentage": 52.0, "estimatedDistanceToEmptyKm": 218.0,
		"chargerConnectionStatus": "CHARGER_CONNECTION_STATUS_DISCONNECTED",
		"chargingStatusV2":        "CHARGING_STATUS_V2_IDLE", "chargingType": "CHARGING_TYPE_NONE",
		"averageEnergyConsumptionKwhPer100Km": 19.1,
	} {
		if b[k] != want {
			t.Errorf("battery %s = %v, want %v", k, b[k], want)
		}
	}
	if _, ok := b["chargingPowerWatts"]; ok {
		t.Error("battery: a power while not charging")
	}

	o := data(t, s, tok, "odometer")
	if o["odometerMeters"] != 23832400.0 || o["tripMeterAutomaticKm"] != 9.0 {
		t.Errorf("odometer %v", o)
	}
	l := data(t, s, tok, "location")
	if c := l["coordinate"].(map[string]any); c["latitude"] != 57.7210 || l["heading"] != 63.0 || l["speed"] != "0" {
		t.Errorf("location %v", l)
	}
	if ts := data(t, s, tok, "target-soc"); ts["targetSoc"].(map[string]any)["batteryChargeTargetLevel"] != 80.0 {
		t.Errorf("target-soc %v", ts)
	}
}

func TestChargingReportsPower(t *testing.T) {
	s := newServer(t, charging(120_000), Limits{})
	b := data(t, s, s.token(t), "battery")
	for k, want := range map[string]any{
		"chargingStatusV2": "CHARGING_STATUS_V2_CHARGING", "chargingType": "CHARGING_TYPE_DC",
		"chargerPowerStatus": "CHARGER_POWER_STATUS_PROVIDING_POWER", "chargingPowerWatts": 120000.0,
		"chargingVoltageVolts": 400.0, "chargingCurrentAmps": 300.0,
		"estimatedChargingTimeToFullMinutes": 11.0, // 27.4 % of 79 kWh at 120 kW
	} {
		if b[k] != want {
			t.Errorf("battery %s = %v, want %v", k, b[k], want)
		}
	}

	rep := charging(120_000)
	rep.Spec.Capabilities.ChargingPower = vehicle.NotFound
	s.src.rep = rep
	if _, ok := data(t, s, s.token(t), "battery")["chargingPowerWatts"]; ok {
		t.Error("a power on a model that does not report it")
	}
}

// TestParkedUploads checks that a parked car uploads its battery and availability every
// ParkedUpload, unchanged, and turns ABANDONED after AbandonAfter; the other domains keep
// the time of their last change.
func TestParkedUploads(t *testing.T) {
	rep, last := parked(), t0
	rep.EnergyAt, rep.StatusAt, rep.LocationAt = last, last, last
	s := newServer(t, rep, Limits{})
	tok := s.token(t)
	s.clk.Advance(25 * time.Minute)
	at := func(d time.Time) string { return fmt.Sprint(d.Unix()) }

	b, a := data(t, s, tok, "battery"), data(t, s, tok, "availability")
	if seconds(b) != at(last.Add(20*time.Minute)) || seconds(a) != at(last.Add(20*time.Minute)) {
		t.Errorf("battery at %s, availability at %s, want the upload of +20 min", seconds(b), seconds(a))
	}
	if a["usageMode"] != "USAGE_MODE_INACTIVE" {
		t.Errorf("usage mode %v, want INACTIVE", a["usageMode"])
	}
	if o := data(t, s, tok, "odometer"); seconds(o) != at(last) {
		t.Errorf("odometer at %s, want its last change", seconds(o))
	}
	again := data(t, s, tok, "battery")
	if again["metaEventId"] != b["metaEventId"] {
		t.Error("the same upload has another event ID")
	}

	s.clk.Advance(35 * time.Minute)
	a = data(t, s, tok, "availability")
	if a["usageMode"] != "USAGE_MODE_ABANDONED" {
		t.Errorf("after an hour: %v, want ABANDONED", a["usageMode"])
	}
	if data(t, s, tok, "battery")["metaEventId"] == b["metaEventId"] {
		t.Error("a new upload kept the event ID")
	}
}

func TestDrivingCar(t *testing.T) {
	rep := parked()
	rep.Status.Driving = true
	rep.StatusAt = t0.Add(-30 * time.Second)
	s := newServer(t, rep, Limits{})
	a := data(t, s, s.token(t), "availability")
	if a["usageMode"] != "USAGE_MODE_DRIVING" || seconds(a) != fmt.Sprint(rep.StatusAt.Unix()) {
		t.Errorf("availability %v", a)
	}
}

func TestDuplicateVINPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	NewHandler([]Source{&fakeSource{}, &fakeSource{}}, clock.NewManual(t0), DevCredential(), Limits{}, Behavior{})
}
