package volvoapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const (
	vin         = "YV1SMLT0000DT0001"
	fixturesDir = "../../../testdata/volvo-demo-car"
)

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

// fakeSource returns a fixed state.
type fakeSource struct{ rep vehicle.Report }

func (f fakeSource) VIN() string                       { return vin }
func (f fakeSource) ReportAt(time.Time) vehicle.Report { return f.rep }

func report(caps vehicle.Capabilities) vehicle.Report {
	st := vehicle.State{
		SoC: 55.4, OdometerKm: 12432.7, Locked: true, PluggedIn: true, Charging: vehicle.Charging,
		ChargeType: vehicle.AC, ChargingPowerW: 7400, TargetSoC: 80,
		Position:        vehicle.Position{Lat: 45.7797, Lon: 4.9270, Heading: 71.6},
		LastTrip:        vehicle.Trip{DistanceKm: 32, Duration: 40 * time.Minute, EnergyKWh: 5.8},
		TotalDistanceKm: 65, TotalEnergyKWh: 11.7, TotalDriveTime: 85 * time.Minute,
		DistanceSinceChargeKm: 65, EnergySinceChargeKWh: 11.7,
	}
	return vehicle.Report{
		Spec: vehicle.Spec{
			Model: "EX-SIM", ModelYear: 2026, BatteryKWh: 80, ConsumptionKWhPer100km: 18,
			ChargingCurrentLimitA: 16, Capabilities: caps,
		},
		Energy: st, EnergyAt: t0.Add(-2 * time.Minute),
		Status: st, StatusAt: t0.Add(-10 * time.Minute),
		Location: st.Position, LocationAt: t0.Add(-3 * time.Hour),
	}
}

func allSupported() vehicle.Capabilities {
	c, _ := vehicle.Profile("bev-generic")
	return c
}

func newServer(t *testing.T, caps vehicle.Capabilities, limits Limits) (*httptest.Server, *clock.Manual) {
	t.Helper()
	clk := clock.NewManual(t0)
	srv := httptest.NewServer(NewHandler([]Source{fakeSource{report(caps)}}, clk, limits, Faults{}, DefaultOAuth()))
	t.Cleanup(srv.Close)
	return srv, clk
}

// get calls path with the application key test-key, and a token if auth.
func get(t *testing.T, srv *httptest.Server, path string, auth bool) (int, map[string]any) {
	t.Helper()
	return getWithKey(t, srv, path, auth, "test-key")
}

func getWithKey(t *testing.T, srv *httptest.Server, path string, auth bool, key string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer test-token")
	}
	if key != "" {
		req.Header.Set("vcc-api-key", key)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s: Content-Type = %q", path, ct)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("%s: invalid JSON: %v", path, err)
	}
	return resp.StatusCode, body
}

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

// sameShape checks that got has the same keys and JSON types as want. null is
// accepted on either side: the value may be missing depending on the vehicle.
func sameShape(path string, want, got any) []string {
	if want == nil || got == nil {
		return nil
	}
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

// TestShapeMatchesRealAPI compares each simulated response with the real response
// captured on the Volvo demo car.
func TestShapeMatchesRealAPI(t *testing.T) {
	srv, _ := newServer(t, allSupported(), Limits{})
	cases := []struct{ path, fixture string }{
		{"/connected-vehicle/v2/vehicles", "vehicles.json"},
		{"/connected-vehicle/v2/vehicles/" + vin, "cv-vehicle.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/fuel", "cv-fuel.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/odometer", "cv-odometer.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/statistics", "cv-statistics.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/engine-status", "cv-engine-status.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/engine", "cv-engine.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/doors", "cv-doors.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/windows", "cv-windows.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/tyres", "cv-tyres.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/brakes", "cv-brakes.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/diagnostics", "cv-diagnostics.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/warnings", "cv-warnings.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/commands", "cv-commands.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/command-accessibility", "cv-command-accessibility.json"},
		{"/connected-vehicle/v2/vehicles/" + vin + "/environment", "cv-environment.json"},
		{"/energy/v2/vehicles/" + vin + "/state", "energy-v2-state.json"},
		{"/energy/v2/vehicles/" + vin + "/capabilities", "energy-v2-capabilities.json"},
		{"/energy/v1/vehicles/" + vin + "/recharge-status", "energy-v1-recharge-status.json"},
		{"/location/v1/vehicles/" + vin + "/location", "location.json"},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			_, got := get(t, srv, c.path, true)
			if diffs := sameShape("$", fixture(t, c.fixture), got); len(diffs) > 0 {
				t.Errorf("shape differs from the real response:\n%s", strings.Join(diffs, "\n"))
			}
		})
	}
}

func TestStatusCodes(t *testing.T) {
	srv, _ := newServer(t, allSupported(), Limits{})
	cases := []struct {
		path string
		auth bool
		want int
	}{
		{"/connected-vehicle/v2/vehicles/" + vin + "/odometer", true, http.StatusOK},
		{"/connected-vehicle/v2/vehicles/" + vin + "/odometer", false, http.StatusUnauthorized},
		{"/energy/v2/vehicles/" + vin + "/state", false, http.StatusUnauthorized},
		{"/connected-vehicle/v2/vehicles/WRONGVIN000000000/odometer", true, http.StatusNotFound},
		{"/energy/v2/vehicles/WRONGVIN000000000/state", true, http.StatusNotFound},
		{"/location/v1/vehicles/WRONGVIN000000000/location", true, http.StatusNotFound},
		{"/connected-vehicle/v2/vehicles/" + vin + "/commands/lock", true, http.StatusForbidden},
		{"/energy/v1/vehicles/" + vin + "/recharge-status", true, http.StatusGone},
		{"/nothing", true, http.StatusNotFound},
	}
	for _, c := range cases {
		if code, _ := get(t, srv, c.path, c.auth); code != c.want {
			t.Errorf("%s (auth=%v): status %d, want %d", c.path, c.auth, code, c.want)
		}
	}
}

func TestValuesAndTimestamps(t *testing.T) {
	srv, _ := newServer(t, allSupported(), Limits{})
	base := "/connected-vehicle/v2/vehicles/" + vin

	_, odo := get(t, srv, base+"/odometer", true)
	o := odo["data"].(map[string]any)["odometer"].(map[string]any)
	if o["value"] != 12432.0 || o["unit"] != "km" || o["timestamp"] != "2026-09-28T05:50:00.000000000Z" {
		t.Errorf("odometer = %v", o)
	}

	_, stats := get(t, srv, base+"/statistics", true)
	s := stats["data"].(map[string]any)
	if ts := s["tripMeterManual"].(map[string]any)["timestamp"]; ts != "2026-09-28T05:50:00.000Z" {
		t.Errorf("statistics timestamp = %v, want millisecond precision", ts)
	}
	if v := s["averageFuelConsumption"].(map[string]any)["value"]; v != nil {
		t.Errorf("averageFuelConsumption = %v, want null for an electric vehicle", v)
	}
	if v := s["averageSpeedAutomatic"].(map[string]any)["value"]; v != 48.0 {
		t.Errorf("averageSpeedAutomatic = %v, want 48 (32 km in 40 min)", v)
	}

	_, energy := get(t, srv, "/energy/v2/vehicles/"+vin+"/state", true)
	soc := energy["batteryChargeLevel"].(map[string]any)
	if soc["value"] != 55.0 || soc["unit"] != "percentage" || soc["updatedAt"] != "2026-09-28T05:58:00Z" {
		t.Errorf("batteryChargeLevel = %v", soc)
	}
	if _, hasUnit := energy["chargingStatus"].(map[string]any)["unit"]; hasUnit {
		t.Error("Energy states have no unit key")
	}
	// (80 - 55.4) % of 80 kWh = 19.68 kWh at 7.4 kW = 159.6 min, rounded up.
	if eta := energy["estimatedChargingTimeToTargetBatteryChargeLevel"].(map[string]any)["value"]; eta != 160.0 {
		t.Errorf("estimated charging time = %v, want 160", eta)
	}

	_, loc := get(t, srv, "/location/v1/vehicles/"+vin+"/location", true)
	props := loc["data"].(map[string]any)["properties"].(map[string]any)
	if props["heading"] != "72" {
		t.Errorf("heading = %#v, want the string \"72\"", props["heading"])
	}
	if props["timestamp"] != "2026-09-28T03:00:00.000000000Z" {
		t.Errorf("location timestamp = %v", props["timestamp"])
	}
	if id, _ := loc["operationId"].(string); !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
		t.Errorf("operationId = %q", id)
	}
}

// TestUsableCapacityDrivesTheConversions: the SoC runs over the usable capacity — the
// estimated charging time and the range convert over it — while batteryCapacityKWH
// keeps reporting the nominal one.
func TestUsableCapacityDrivesTheConversions(t *testing.T) {
	rep := report(allSupported())
	rep.Spec.BatteryKWh, rep.Spec.UsableKWh = 69, 64 // an EX30: gross reported, net used
	srv := httptest.NewServer(NewHandler([]Source{fakeSource{rep}}, clock.NewManual(t0), Limits{}, Faults{}, DefaultOAuth()))
	t.Cleanup(srv.Close)

	_, details := get(t, srv, "/connected-vehicle/v2/vehicles/"+vin, true)
	if got := details["data"].(map[string]any)["batteryCapacityKWH"]; got != 69.0 {
		t.Errorf("batteryCapacityKWH = %v, want the nominal 69", got)
	}

	// (80 - 55.4) % of the usable 64 kWh at 7.4 kW = 127.6 min, rounded up: 128
	// (160 over the nominal 80).
	_, energy := get(t, srv, "/energy/v2/vehicles/"+vin+"/state", true)
	if eta := energy["estimatedChargingTimeToTargetBatteryChargeLevel"].(map[string]any)["value"]; eta != 128.0 {
		t.Errorf("estimated charging time = %v, want 128", eta)
	}
	// 55.4 % of the usable 64 kWh at 18 kWh/100 km = 196.98 km → 196 (246 over the
	// nominal 80).
	if r := energy["electricRange"].(map[string]any)["value"]; r != 196.0 {
		t.Errorf("electricRange = %v, want 196", r)
	}

	_, stats := get(t, srv, "/connected-vehicle/v2/vehicles/"+vin+"/statistics", true)
	if d := stats["data"].(map[string]any)["distanceToEmptyBattery"].(map[string]any)["value"]; d != 196.0 {
		t.Errorf("distanceToEmptyBattery = %v, want 196", d)
	}
}

func TestProfilesProduceFieldErrors(t *testing.T) {
	tests := []struct {
		profile, field, code string
	}{
		{"ex30-like", "chargingCurrentLimit", "NOT_SUPPORTED"},
		{"ex90-like", "chargingPower", "PROPERTY_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			caps, _ := vehicle.Profile(tt.profile)
			srv, _ := newServer(t, caps, Limits{})
			_, state := get(t, srv, "/energy/v2/vehicles/"+vin+"/state", true)
			f := state[tt.field].(map[string]any)
			if f["status"] != "ERROR" || f["code"] != tt.code || f["value"] != nil {
				t.Errorf("%s = %v", tt.field, f)
			}
			_, capsBody := get(t, srv, "/energy/v2/vehicles/"+vin+"/capabilities", true)
			c := capsBody["getEnergyState"].(map[string]any)[tt.field].(map[string]any)
			if c["isSupported"] != false {
				t.Errorf("capabilities.%s = %v, want isSupported false", tt.field, c)
			}
		})
	}
}

func TestRateLimitPerMinute(t *testing.T) {
	srv, clk := newServer(t, allSupported(), Limits{PerMinute: 3})
	path := "/connected-vehicle/v2/vehicles/" + vin + "/odometer"
	for i := range 3 {
		if code, _ := get(t, srv, path, true); code != http.StatusOK {
			t.Fatalf("call %d: status %d", i+1, code)
		}
	}
	code, body := get(t, srv, path, true)
	if code != http.StatusTooManyRequests || !strings.HasPrefix(body["message"].(string), "Rate limit is exceeded") {
		t.Fatalf("4th call: %d %v", code, body)
	}
	clk.Advance(time.Minute)
	if code, _ := get(t, srv, path, true); code != http.StatusOK {
		t.Fatalf("after one minute: status %d", code)
	}
}

func TestDailyQuotaPerAPI(t *testing.T) {
	srv, clk := newServer(t, allSupported(), Limits{DailyQuota: 2})
	cvPath := "/connected-vehicle/v2/vehicles/" + vin + "/odometer"
	for range 2 {
		get(t, srv, cvPath, true)
	}
	code, body := get(t, srv, cvPath, true)
	if code != http.StatusForbidden {
		t.Fatalf("quota exceeded: status %d, want 403", code)
	}
	if msg := body["message"]; msg != "Out of call volume quota. Quota will be replenished in 18:00:00." {
		t.Errorf("message = %q", msg)
	}
	// The quota is counted per API: the Energy API remains available.
	if code, _ := get(t, srv, "/energy/v2/vehicles/"+vin+"/state", true); code != http.StatusOK {
		t.Errorf("Energy API: status %d, want 200", code)
	}
	// New UTC day: quota reset.
	clk.Advance(18 * time.Hour)
	if code, _ := get(t, srv, cvPath, true); code != http.StatusOK {
		t.Errorf("next day: status %d, want 200", code)
	}
}

// TestQuotaPerKey: each application key has its own quota, and a key not accepted is
// refused before any API, whatever the token.
func TestQuotaPerKey(t *testing.T) {
	srv, _ := newServer(t, allSupported(), Limits{DailyQuota: 2, Keys: []string{"key-a", "key-b"}})
	path := "/connected-vehicle/v2/vehicles/" + vin + "/odometer"
	for range 2 {
		getWithKey(t, srv, path, true, "key-a")
	}
	if code, _ := getWithKey(t, srv, path, true, "key-a"); code != http.StatusForbidden {
		t.Errorf("key-a after its quota: status %d, want 403", code)
	}
	if code, _ := getWithKey(t, srv, path, true, "key-b"); code != http.StatusOK {
		t.Errorf("key-b: status %d, want 200: its quota is its own", code)
	}
	for _, key := range []string{"key-c", ""} {
		code, body := getWithKey(t, srv, "/energy/v2/vehicles/"+vin+"/state", true, key)
		e, _ := body["error"].(map[string]any)
		if msg, _ := e["message"].(string); code != http.StatusUnauthorized || !strings.Contains(msg, "VCC-API-KEY") {
			t.Errorf("key %q: %d %v, want the gateway's 401", key, code, body)
		}
	}
	// Without a list, any key is accepted.
	open, _ := newServer(t, allSupported(), Limits{})
	if code, _ := getWithKey(t, open, path, true, "anything"); code != http.StatusOK {
		t.Errorf("any key: status %d", code)
	}
}

func TestUnauthorizedUsesEachAPIFormat(t *testing.T) {
	srv, _ := newServer(t, allSupported(), Limits{})
	_, cvBody := get(t, srv, "/connected-vehicle/v2/vehicles/"+vin+"/odometer", false)
	if diffs := sameShape("$", fixture(t, "cv-commands.json"), cvBody); len(diffs) > 0 {
		t.Errorf("Connected Vehicle: %v", diffs)
	}
	_, enBody := get(t, srv, "/energy/v2/vehicles/"+vin+"/state", false)
	if diffs := sameShape("$", map[string]any{"code": "", "message": "", "details": []any{}}, enBody); len(diffs) > 0 || enBody["code"] != "AUTHORIZATION_ERROR" {
		t.Errorf("Energy : %v %v", diffs, enBody)
	}
}

// namedSource is a vehicle of a fleet: a fixed state under its own VIN.
type namedSource struct {
	vin string
	rep vehicle.Report
}

func (n namedSource) VIN() string                       { return n.vin }
func (n namedSource) ReportAt(time.Time) vehicle.Report { return n.rep }

const vin2 = "YV1SMLT0000DT0002"

// fleetServer serves two vehicles: vin, as report builds it, and vin2, further on the
// odometer and on another battery.
func fleetServer(t *testing.T, limits Limits) *httptest.Server {
	t.Helper()
	second := report(allSupported())
	second.Status.OdometerKm, second.Spec.BatteryKWh = 500, 64
	srcs := []Source{namedSource{vin, report(allSupported())}, namedSource{vin2, second}}
	srv := httptest.NewServer(NewHandler(srcs, clock.NewManual(t0), limits, Faults{}, DefaultOAuth()))
	t.Cleanup(srv.Close)
	return srv
}

func TestSeveralVehicles(t *testing.T) {
	srv := fleetServer(t, Limits{})

	_, list := get(t, srv, "/connected-vehicle/v2/vehicles", true)
	data, _ := list["data"].([]any)
	var vins []string
	for _, d := range data {
		v, _ := d.(map[string]any)["vin"].(string)
		vins = append(vins, v)
	}
	if strings.Join(vins, ",") != vin+","+vin2 {
		t.Errorf("listed VINs = %v, want both, in the order of the sources", vins)
	}

	value := func(body map[string]any, keys ...string) any {
		var v any = body
		for _, k := range keys {
			m, _ := v.(map[string]any)
			v = m[k]
		}
		return v
	}
	tests := []struct {
		path string
		keys []string
		want any
	}{
		{"/connected-vehicle/v2/vehicles/" + vin + "/odometer", []string{"data", "odometer", "value"}, 12432.0},
		{"/connected-vehicle/v2/vehicles/" + vin2 + "/odometer", []string{"data", "odometer", "value"}, 500.0},
		{"/connected-vehicle/v2/vehicles/" + vin, []string{"data", "vin"}, vin},
		{"/connected-vehicle/v2/vehicles/" + vin2, []string{"data", "vin"}, vin2},
		{"/connected-vehicle/v2/vehicles/" + vin2, []string{"data", "batteryCapacityKWH"}, 64.0},
		{"/energy/v2/vehicles/" + vin2 + "/state", []string{"batteryChargeLevel", "value"}, 55.0},
		{"/location/v1/vehicles/" + vin2 + "/location", []string{"data", "properties", "heading"}, "72"},
	}
	for _, tt := range tests {
		code, body := get(t, srv, tt.path, true)
		if got := value(body, tt.keys...); code != http.StatusOK || got != tt.want {
			t.Errorf("%s: %d, %s = %v, want %v", tt.path, code, strings.Join(tt.keys, "."), got, tt.want)
		}
	}
	for _, path := range []string{
		"/connected-vehicle/v2/vehicles/WRONGVIN000000000/odometer",
		"/energy/v2/vehicles/WRONGVIN000000000/state",
		"/location/v1/vehicles/WRONGVIN000000000/location",
	} {
		if code, _ := get(t, srv, path, true); code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, code)
		}
	}
}

// TestQuotaSharedByVehicles: the calls of every vehicle count against one quota per
// API and application key (assumption, see Limits.DailyQuota).
func TestQuotaSharedByVehicles(t *testing.T) {
	srv := fleetServer(t, Limits{DailyQuota: 2})
	get(t, srv, "/connected-vehicle/v2/vehicles/"+vin+"/odometer", true)
	get(t, srv, "/connected-vehicle/v2/vehicles/"+vin2+"/odometer", true)
	for _, v := range []string{vin, vin2} {
		if code, _ := get(t, srv, "/connected-vehicle/v2/vehicles/"+v+"/odometer", true); code != http.StatusForbidden {
			t.Errorf("%s after two calls, one per vehicle: status %d, want 403", v, code)
		}
	}
}

func TestDuplicateVINPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("two sources with the same VIN: no panic")
		}
	}()
	src := fakeSource{report(allSupported())}
	NewHandler([]Source{src, src}, clock.NewManual(t0), Limits{}, Faults{}, DefaultOAuth())
}

func TestHMS(t *testing.T) {
	if got := hms(3*time.Hour + 4*time.Minute + 5*time.Second); got != "03:04:05" {
		t.Errorf("hms = %q", got)
	}
}
