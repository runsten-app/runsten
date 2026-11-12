package skodaapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const testVIN = "TMBJC9NY0RF000001"

// car is a vehicle of a fixed report: parked and plugged, or as set.
type car struct{ rep vehicle.Report }

func (c car) VIN() string                       { return testVIN }
func (c car) ReportAt(time.Time) vehicle.Report { return c.rep }

func parked(at time.Time) vehicle.Report {
	return vehicle.Report{
		Spec:     vehicle.Spec{Model: "Enyaq", BatteryKWh: 82, UsableKWh: 77, ConsumptionKWhPer100km: 17},
		Energy:   vehicle.State{SoC: 61.6, TargetSoC: 80, PluggedIn: true, Charging: vehicle.Charging, ChargeType: vehicle.AC, ChargingPowerW: 11000},
		EnergyAt: at.Add(-time.Minute),
		Status:   vehicle.State{OdometerKm: 18420.7, Locked: true},
		StatusAt: at.Add(-time.Hour), Location: vehicle.Position{Lat: 45.764, Lon: 4.8357}, LocationAt: at.Add(-time.Hour),
	}
}

func newHandler(t *testing.T, rep vehicle.Report) (*httptest.Server, *clock.Manual, *Handler) {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC))
	if rep.Spec.Model == "" {
		rep = parked(clk.Now())
	}
	h := NewHandler([]Source{car{rep}}, clk, DevAPIKey, DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, clk, h
}

// reply is a response read whole: its status, its headers and its JSON body.
type reply struct {
	StatusCode int
	Header     http.Header
}

func get(t *testing.T, srv *httptest.Server, key, query string) (reply, map[string]any) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+Prefix+"api/v1/vehicles/"+testVIN+query, http.NoBody)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return reply{StatusCode: resp.StatusCode, Header: resp.Header}, v
}

func post(t *testing.T, srv *httptest.Server, path string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+ControlPrefix+path, http.NoBody)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("%s: %d", path, resp.StatusCode)
	}
}

// TestShape checks the response against the contract's required fields and enumerations.
func TestShape(t *testing.T) {
	srv, _, h := newHandler(t, vehicle.Report{})
	resp, v := get(t, srv, DevAPIKey, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if at, err := time.Parse(time.RFC3339, resp.Header.Get("X-API-Key-Expires-At")); err != nil || !at.Equal(h.ExpiresAt()) {
		t.Errorf("expiry %q", resp.Header.Get("X-API-Key-Expires-At"))
	}
	veh := v["vehicle"].(map[string]any)
	if veh["vin"] != testVIN || veh["name"] != "Enyaq" {
		t.Errorf("info: %v", veh)
	}
	ch := veh["charging"].(map[string]any)
	st := ch["status"].(map[string]any)
	if st["state"] != "CHARGING" || st["chargeType"] != "AC" || st["plugConnectionState"] != "CONNECTED" || st["chargePowerInKw"] != 11.0 {
		t.Errorf("charging status %v", st)
	}
	if b := st["battery"].(map[string]any); b["stateOfChargeInPercent"] != 62.0 || b["remainingCruisingRangeInMeters"] == nil {
		t.Errorf("battery %v", b)
	}
	if _, ok := ch["isVehicleInSavedLocation"]; !ok || ch["carCapturedTimestamp"] != "2026-11-11T17:59:00Z" {
		t.Errorf("charging %v", ch)
	}
	// The status is sent every 5 minutes while the car charges.
	if o := veh["odometer"].(map[string]any); o["mileageInKm"] != 18420.0 || o["carCapturedTimestamp"] != "2026-11-11T18:00:00Z" {
		t.Errorf("odometer %v", o)
	}
	if p := veh["parkingPosition"].(map[string]any); p["state"] != "PARKED" || p["gpsCoordinates"] == nil {
		t.Errorf("parking %v", p)
	}
	ov := veh["status"].(map[string]any)["overall"].(map[string]any)
	for _, k := range []string{"doors", "doorsLocked", "lights", "locked", "windows"} {
		if ov[k] == nil {
			t.Errorf("overall lacks %s", k)
		}
	}
	if ov["doorsLocked"] != "YES" {
		t.Errorf("overall %v", ov)
	}
	if ac := veh["airConditioning"].(map[string]any); ac["state"] != "OFF" {
		t.Errorf("air conditioning %v", ac)
	}
	for _, p := range []string{"fuelStatus", "chargingProfiles", "operations"} {
		if _, ok := veh[p]; ok {
			t.Errorf("an unsupported part rendered: %s", p)
		}
	}
	if errs := v["errors"].([]any); len(errs) != 0 {
		t.Errorf("errors %v: unsupported parts are told only when asked for", errs)
	}
}

func TestInclude(t *testing.T) {
	srv, _, h := newHandler(t, vehicle.Report{})
	_, v := get(t, srv, DevAPIKey, "?include=info,fuelStatus&include=odometer")
	veh := v["vehicle"].(map[string]any)
	if _, ok := veh["charging"]; ok || veh["odometer"] == nil {
		t.Errorf("parts %v", veh)
	}
	if errs := v["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["type"] != "FUEL_STATUS_UNSUPPORTED" {
		t.Errorf("errors %v", errs)
	}
	h.MakeUnavailable("odometer")
	_, v = get(t, srv, DevAPIKey, "?include=odometer")
	if _, ok := v["vehicle"].(map[string]any)["odometer"]; ok || !strings.Contains(toJSON(v["errors"]), "ODOMETER_UNAVAILABLE") {
		t.Errorf("unavailable: %v", v)
	}
	resp, v := get(t, srv, DevAPIKey, "?include=nonsense")
	if resp.StatusCode != http.StatusBadRequest || resp.Header.Get("Content-Type") != "application/problem+json" || v["status"] != 400.0 {
		t.Errorf("unknown include: %d %v", resp.StatusCode, v)
	}
}

func TestFaults(t *testing.T) {
	tests := []struct {
		path    string
		status  int
		problem string
	}{
		{"expire", http.StatusUnauthorized, "api-key-expired"},
		{"uncover?vin=" + testVIN, http.StatusForbidden, "api-key-not-authorized"},
		{"uncover", http.StatusForbidden, "api-key-not-authorized"},
		{"outage?for=1h", http.StatusServiceUnavailable, ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			srv, _, _ := newHandler(t, vehicle.Report{})
			post(t, srv, tt.path)
			resp, v := get(t, srv, DevAPIKey, "")
			if resp.StatusCode != tt.status || (tt.problem != "" && v["type"] != problemBase+tt.problem) {
				t.Errorf("%d %v", resp.StatusCode, v)
			}
			// A 5xx counts in the quota and carries its headers; a 401 or a 403 does not.
			if counted := resp.Header.Get("RateLimit-Remaining") == "19"; counted != (tt.status >= 500) {
				t.Errorf("remaining %q", resp.Header.Get("RateLimit-Remaining"))
			}
		})
	}
	srv, _, _ := newHandler(t, vehicle.Report{})
	post(t, srv, "unavailable")
	_, v := get(t, srv, DevAPIKey, "")
	if len(v["errors"].([]any)) != 5 {
		t.Errorf("every part unavailable: %v", v["errors"])
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+ControlPrefix+"vehicles", http.NoBody)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var list struct{ VINs []string }
	_ = json.NewDecoder(resp.Body).Decode(&list)
	_ = resp.Body.Close()
	if len(list.VINs) != 1 || list.VINs[0] != testVIN {
		t.Errorf("vehicles %v", list.VINs)
	}
	if resp, _ := get(t, srv, "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no key: %d", resp.StatusCode)
	}
}

// TestQuota: 20 an hour from the first call, a 429 that does not prolong the window.
func TestQuota(t *testing.T) {
	srv, clk, _ := newHandler(t, vehicle.Report{})
	for range 20 {
		if resp, _ := get(t, srv, DevAPIKey, "?include=info"); resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}
	clk.Advance(30 * time.Minute)
	resp, v := get(t, srv, DevAPIKey, "?include=info")
	if resp.StatusCode != http.StatusTooManyRequests || v["type"] != problemBase+"rate-limit-exceeded" ||
		resp.Header.Get("Retry-After") != "1800" || resp.Header.Get("RateLimit-Remaining") != "0" {
		t.Fatalf("21st: %d %v %v", resp.StatusCode, v, resp.Header)
	}
	clk.Advance(30 * time.Minute)
	if resp, _ := get(t, srv, DevAPIKey, "?include=info"); resp.StatusCode != http.StatusOK || resp.Header.Get("RateLimit-Reset") != "3600" {
		t.Errorf("a new window: %d %v", resp.StatusCode, resp.Header)
	}
}

// TestDriving: IN_MOTION, without a position; a car parked unplugged asks for its cable.
func TestDriving(t *testing.T) {
	at := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	rep := parked(at)
	rep.Status.Driving = true
	rep.Energy = vehicle.State{SoC: 50}
	srv, _, _ := newHandler(t, rep)
	_, v := get(t, srv, DevAPIKey, "")
	veh := v["vehicle"].(map[string]any)
	if p := veh["parkingPosition"].(map[string]any); p["state"] != "IN_MOTION" || p["gpsCoordinates"] != nil {
		t.Errorf("parking %v", p)
	}
	st := veh["charging"].(map[string]any)["status"].(map[string]any)
	if st["state"] != "CONNECT_CABLE" || st["plugConnectionState"] != "DISCONNECTED" || st["chargeType"] != "OFF" {
		t.Errorf("charging %v", st)
	}
	if veh["airConditioning"].(map[string]any)["state"] != "HEATING" {
		t.Error("the cabin blows while driving")
	}
}

func TestSendAt(t *testing.T) {
	at := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	b := Behavior{SendEvery: 5 * time.Minute}
	if got := sendAt(b, at, true, at.Add(12*time.Minute)); !got.Equal(at.Add(10 * time.Minute)) {
		t.Errorf("active: %v", got)
	}
	if got := sendAt(b, at, false, at.Add(12*time.Minute)); !got.Equal(at) {
		t.Errorf("parked: %v", got)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
