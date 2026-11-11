package pleosapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const (
	vin  = "KNAPC81ABPP000001" // a Kia
	vin2 = "TMAPJ61AB0P000002" // a Hyundai
)

var t0 = time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)

// fakeSource is a car whose state the test sets.
type fakeSource struct {
	brand, vin string
	spec       vehicle.Spec
	rep        vehicle.Report
}

func (f *fakeSource) VIN() string {
	if f.vin == "" {
		return vin
	}
	return f.vin
}

func (f *fakeSource) Brand() string {
	if f.brand == "" {
		return Kia
	}
	return f.brand
}
func (f *fakeSource) ReportAt(time.Time) vehicle.Report { return f.rep }

// parked is an EV6 parked at home, unplugged, every group uploaded at t0.
func parked() *fakeSource {
	spec := vehicle.Spec{Model: "EV6", ModelYear: 2023, BatteryKWh: 77.4, UsableKWh: 74, ConsumptionKWhPer100km: 18}
	return &fakeSource{
		brand: Kia,
		spec:  spec,
		rep: vehicle.Report{
			Spec: spec,
			Energy: vehicle.State{
				SoC: 42.5, OdometerKm: 21034.6, TargetSoC: 80,
				Position: vehicle.Position{Lat: 48.8566, Lon: 2.3522, Heading: 270},
			},
			EnergyAt: t0,
			Status: vehicle.State{
				SoC: 42.5, OdometerKm: 21034.6, Locked: true,
				Position: vehicle.Position{Lat: 48.8566, Lon: 2.3522, Heading: 270},
			},
			StatusAt:   t0,
			Location:   vehicle.Position{Lat: 48.8566, Lon: 2.3522, Heading: 270},
			LocationAt: t0,
		},
	}
}

// hyundai is an Ioniq 5 the same key does not read (one key per brand).
func hyundai() *fakeSource {
	spec := vehicle.Spec{Model: "IONIQ 5", ModelYear: 2022, BatteryKWh: 72.6, UsableKWh: 70}
	return &fakeSource{
		brand: Hyundai,
		vin:   vin2,
		spec:  spec,
		rep:   vehicle.Report{Spec: spec},
	}
}

type server struct {
	*httptest.Server
	clk *clock.Manual
	h   *Handler
	src *fakeSource
}

func newServer(t *testing.T, behavior Behavior) *server {
	t.Helper()
	src := parked()
	clk := clock.NewManual(t0)
	h := NewHandler([]Source{src, hyundai()}, clk, DevCredential(), behavior)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &server{Server: srv, clk: clk, h: h, src: src}
}

// call sends a request and returns its status, JSON body and headers.
func (s *server) call(t *testing.T, method, path string, body []byte, header map[string]string) (int, any, http.Header) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, s.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
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
	return resp.StatusCode, out, resp.Header
}

// get calls the API with the token and its brand.
func (s *server) get(t *testing.T, tok, brand, path string) (int, any, http.Header) {
	t.Helper()
	return s.call(t, http.MethodGet, path, nil, map[string]string{"Authorization": "Bearer " + tok, "Brand": brand})
}

// token asks for the key's token, reading brand.
func (s *server) token(t *testing.T, brand string) string {
	t.Helper()
	body := tokenBody(t, DevCredential().ClientID, DevCredential().ClientSecret)
	status, out, _ := s.call(t, http.MethodPost, Prefix+"v1/auth/personal-token", body,
		map[string]string{"Brand": brand, "Content-Type": "application/json"})
	if status != http.StatusOK {
		t.Fatalf("personal-token: HTTP %d: %v", status, out)
	}
	return obj(t, obj(t, out)["data"])["accessToken"].(string)
}

func tokenBody(t *testing.T, id, secret string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"clientId": id, "clientSecret": secret})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// obj returns the JSON value as an object.
func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want a JSON object, got %T", v)
	}
	return m
}

// errorCode returns the error envelope's code as a string, whatever the API wrote it as
// (the doc's examples put codes in strings; a number has been observed too).
func errorCode(t *testing.T, v any) string {
	t.Helper()
	e := obj(t, obj(t, v)["error"])
	switch c := e["code"].(type) {
	case string:
		return c
	case float64:
		return strconv.FormatFloat(c, 'f', -1, 64)
	default:
		t.Fatalf("error code %T: neither a string nor a number", e["code"])
		return ""
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func jsonOf(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPersonalToken(t *testing.T) {
	s := newServer(t, DefaultBehavior())
	for _, brand := range Brands() {
		if tok := s.token(t, brand); tok == "" || strings.Count(tok, ".") != 2 {
			t.Errorf("%s: no three-part JWT: %q", brand, tok)
		}
	}
	// Without a Brand header: the documented invalid parameters.
	status, out, _ := s.call(t, http.MethodPost, Prefix+"v1/auth/personal-token",
		tokenBody(t, DevCredential().ClientID, DevCredential().ClientSecret), map[string]string{"Content-Type": "application/json"})
	if status != http.StatusBadRequest || errorCode(t, out) != "40001" {
		t.Errorf("no brand: HTTP %d code %s", status, errorCode(t, out))
	}
	// A wrong secret: the documented user or key not found.
	status, out, _ = s.call(t, http.MethodPost, Prefix+"v1/auth/personal-token",
		tokenBody(t, DevCredential().ClientID, "wrong"), map[string]string{"Brand": Kia, "Content-Type": "application/json"})
	if status != http.StatusNotFound || errorCode(t, out) != "40404" {
		t.Errorf("wrong secret: HTTP %d code %s", status, errorCode(t, out))
	}
}

func TestOneLiveTokenPerKey(t *testing.T) {
	s := newServer(t, DefaultBehavior())
	first := s.token(t, Kia)
	// Another client asks for a token with the same key: the previous one stops
	// reading, as observed on the real API (only one lives at a time).
	second := s.token(t, Kia)
	status, out, _ := s.get(t, first, Kia, Prefix+"v1/vehicles/consent")
	if status != http.StatusForbidden || errorCode(t, out) != "403" {
		t.Errorf("the previous token: HTTP %d code %s", status, errorCode(t, out))
	}
	if status, _, _ := s.get(t, second, Kia, Prefix+"v1/vehicles/consent"); status != http.StatusOK {
		t.Errorf("the new token: HTTP %d", status)
	}
	// The token reads its own brand only (assumption: the forbidden of the doc's
	// table, the answer of another brand not being published).
	if status, _, _ := s.get(t, second, Hyundai, Prefix+"v1/vehicles/consent"); status != http.StatusBadRequest {
		t.Errorf("the kia token reading hyundai: HTTP %d", status)
	}
}

func TestTokenExpiry(t *testing.T) {
	s := newServer(t, Behavior{TokenTTL: time.Hour})
	tok := s.token(t, Kia)
	s.clk.Advance(61 * time.Minute)
	status, out, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/consent")
	if status != http.StatusBadRequest || errorCode(t, out) != "40003" {
		t.Errorf("the expired token: HTTP %d code %s", status, errorCode(t, out))
	}
	s.clk.Advance(0)
	// A new token reads again, as the documentation tells a client to ask for.
	if tok := s.token(t, Kia); tok == "" {
		t.Error("no new token after the expiry")
	}
}

func TestConsent(t *testing.T) {
	s := newServer(t, DefaultBehavior())
	tok := s.token(t, Kia)
	status, out, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/consent")
	if status != http.StatusOK {
		t.Fatalf("consent: HTTP %d", status)
	}
	vs := obj(t, obj(t, out)["data"])["vehicles"].([]any)
	if len(vs) != 1 {
		t.Fatalf("the kia key lists %d vehicles, want its own", len(vs))
	}
	v := obj(t, vs[0])
	for k, w := range map[string]any{"vin": vin, "nickname": "EV6", "name": "EV6", "type": "EV", "sellname": "EV6"} {
		if v[k] != w {
			t.Errorf("%s: %v, want %v", k, v[k], w)
		}
	}
	// The key of one brand reads no other brand's car, with the token of its own.
	hyundaiTok := s.token(t, Hyundai)
	status, out, _ = s.get(t, hyundaiTok, Hyundai, Prefix+"v1/vehicles/consent")
	if status != http.StatusOK {
		t.Fatalf("consent: HTTP %d", status)
	}
	vs = obj(t, obj(t, out)["data"])["vehicles"].([]any)
	if len(vs) != 1 || obj(t, vs[0])["sellname"] != "IONIQ 5" {
		t.Errorf("the hyundai key lists %v, want the Ioniq 5", vs)
	}
	status, out, _ = s.get(t, hyundaiTok, Hyundai, Prefix+"v1/vehicles/"+vin+"/batteries")
	if status != http.StatusBadRequest || errorCode(t, out) != "403" {
		t.Errorf("another brand's VIN: HTTP %d code %s", status, errorCode(t, out))
	}
	// No Authorization header: the doc's own answer.
	status, out, _ = s.call(t, http.MethodGet, Prefix+"v1/vehicles/consent", nil, map[string]string{"Brand": Kia})
	if status != http.StatusBadRequest || errorCode(t, out) != "40002" {
		t.Errorf("no header: HTTP %d code %s", status, errorCode(t, out))
	}
}

// keptOut are the doc example's fields the simulator leaves out or answers differently
// on purpose: no electric car reports them (the compatibility matrix), or the model
// simulates nothing of them (no warning ever lights).
var keptOut = map[string][]string{
	"status": {"data.outsideTemperature.value", "data.outsideTemperature.unit", "data.warnings[0]"},
}

// TestShapes answers each endpoint and checks its response's shape against the doc's
// example, kept in testdata: every field of the example is there, of its kind.
func TestShapes(t *testing.T) {
	s := newServer(t, DefaultBehavior())
	tok := s.token(t, Kia)
	for _, ep := range append(Endpoints(), "consent") {
		path := Prefix + "v1/vehicles/consent"
		if ep != "consent" {
			path = Prefix + "v1/vehicles/" + vin + "/" + ep
		}
		status, out, _ := s.get(t, tok, Kia, path)
		if status != http.StatusOK {
			t.Fatalf("%s: HTTP %d", ep, status)
		}
		got := map[string]string{}
		paths("", jsonOf(t, mustMarshal(t, out)), got)
		doc := map[string]string{}
		raw, err := os.ReadFile("testdata/" + ep + ".json") //nolint:gosec // the test's own file
		if err != nil {
			t.Fatal(err)
		}
		paths("", jsonOf(t, raw), doc)
		for p, kind := range doc {
			if !strings.HasPrefix(p, "data.") || contains(keptOut[ep], p) {
				continue // the envelope's own half, and the fields kept out
			}
			g, ok := got[p]
			switch {
			case !ok:
				t.Errorf("%s: no %s", ep, p)
			case g != kind && g != "null" && kind != "null":
				// A null is the API's answer for what it does not collect: it may
				// be a value on a car that does.
				t.Errorf("%s: %s is a %s, the doc's a %s", ep, p, g, kind)
			}
		}
	}
	// The vehicle's send time is in every response's data, not the read's.
	status, out, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/"+vin+"/driving")
	if status != http.StatusOK {
		t.Fatalf("driving: HTTP %d", status)
	}
	at := obj(t, obj(t, out)["data"])["timestamp"].(string)
	if want := t0.Format("2006-01-02T15:04:05Z"); at != want {
		t.Errorf("timestamp %q, want %q", at, want)
	}
}

// paths walks v and collects its leaves' paths with their JSON kind: an object's fields
// as name.field, an array's elements as name[index]; a null as "null".
func paths(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			paths(p, x, out)
		}
	case []any:
		for i, x := range t {
			paths(prefix+"["+strconv.Itoa(i)+"]", x, out)
		}
	case float64:
		out[prefix] = "number"
	case string:
		out[prefix] = "string"
	case bool:
		out[prefix] = "bool"
	case nil:
		out[prefix] = "null"
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
