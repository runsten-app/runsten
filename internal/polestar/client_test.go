package polestar

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/polestarapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

const simVIN = "LPSVSEDE5RL000001"

// newSim starts the simulator on the Polestar of demonstration: this is the contract
// test between the client and the fake Data Portal.
func newSim(t *testing.T, limits polestarapi.Limits) (*Client, *clock.Manual) {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/polestar2.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	h := polestarapi.NewHandler([]polestarapi.Source{sim}, clk, polestarapi.DevCredential(), limits, polestarapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+polestarapi.Prefix, srv.Client()), clk
}

func devCredential() Credential {
	c := polestarapi.DevCredential()
	return Credential{ClientID: c.ClientID, ClientSecret: c.ClientSecret, AccountID: c.AccountID}
}

func token(ctx context.Context, t *testing.T, c *Client) string {
	t.Helper()
	tok, err := c.Token(ctx, devCredential())
	if err != nil {
		t.Fatal(err)
	}
	return tok.Value
}

func TestTokenAndVehicles(t *testing.T) {
	c, _ := newSim(t, polestarapi.DefaultLimits())
	ctx := context.Background()
	tok, err := c.Token(ctx, devCredential())
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value == "" || tok.ExpiresIn != time.Hour {
		t.Errorf("token = %+v", tok)
	}
	vins, err := c.Vehicles(ctx, tok.Value, devCredential().AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vins) != 1 || vins[0] != simVIN {
		t.Fatalf("vins = %v", vins)
	}
}

func TestFetchDomains(t *testing.T) {
	c, _ := newSim(t, polestarapi.DefaultLimits())
	ctx := context.Background()
	tok := token(ctx, t, c)
	for _, d := range Domains() {
		t.Run(string(d), func(t *testing.T) {
			raw, err := c.Fetch(ctx, tok, devCredential().AccountID, simVIN, d)
			if err != nil {
				t.Fatal(err)
			}
			var v struct {
				Meta struct {
					Domain string `json:"domain"`
				} `json:"meta"`
			}
			if err := json.Unmarshal(raw, &v); err != nil || v.Meta.Domain != string(d) {
				t.Errorf("meta.domain = %q, %v", v.Meta.Domain, err)
			}
			if _, err := Normalize(d, raw); err != nil {
				t.Error(err)
			}
		})
	}
	if _, err := c.Fetch(ctx, tok, devCredential().AccountID, simVIN, "health"); !errors.Is(err, ErrUnknownDomain) {
		t.Errorf("health: %v", err)
	}
}

// TestSimulatorErrors: each error the simulator gives is the kind the collector acts on.
func TestSimulatorErrors(t *testing.T) {
	ctx := context.Background()
	account := devCredential().AccountID

	c, clk := newSim(t, polestarapi.Limits{TokenTTL: time.Hour, PerMinute: 1})
	tok := token(ctx, t, c)
	wrong := devCredential()
	wrong.ClientSecret = "not-the-secret"
	_, err := c.Token(ctx, wrong)
	checkKind(t, "wrong secret", err, KindCredentialRefused, http.StatusUnauthorized)
	if strings.Contains(err.Error(), wrong.ClientSecret) {
		t.Errorf("the secret is in the error: %v", err)
	}

	_, err = c.Vehicles(ctx, tok, "00000000-0000-4000-8000-000000000000")
	checkKind(t, "another account", err, KindCredentialRefused, http.StatusForbidden)
	_, err = c.Fetch(ctx, tok, account, "LPSVSEDE5RL999999", Battery)
	checkKind(t, "VIN not shared", err, KindVINUnauthorized, http.StatusForbidden)
	_, err = c.Fetch(ctx, tok, account, simVIN, Battery)
	checkKind(t, "per minute", err, KindRateLimited, http.StatusTooManyRequests)
	if e := (*APIError)(nil); errors.As(err, &e) && e.RetryIn != defaultRateRetry {
		t.Errorf("retry in %v without Retry-After", e.RetryIn)
	}

	clk.Advance(time.Hour)
	_, err = c.Fetch(ctx, tok, account, simVIN, Battery)
	checkKind(t, "token expired", err, KindUnauthorized, http.StatusUnauthorized)
	if strings.Contains(err.Error(), tok) {
		t.Errorf("the token is in the error: %v", err)
	}
	if _, err := c.Fetch(ctx, token(ctx, t, c), account, simVIN, Battery); err != nil {
		t.Errorf("new token: %v", err)
	}
}

func TestCredentialExpired(t *testing.T) {
	c, clk := newSim(t, polestarapi.Limits{TokenTTL: time.Hour, CredentialTTL: 90 * 24 * time.Hour})
	clk.Advance(90 * 24 * time.Hour)
	_, err := c.Token(context.Background(), devCredential())
	checkKind(t, "after 90 days", err, KindCredentialRefused, http.StatusUnauthorized)
}

func checkKind(t *testing.T, name string, err error, kind Kind, status int) {
	t.Helper()
	var e *APIError
	if !errors.As(err, &e) {
		t.Fatalf("%s: err = %v, want APIError", name, err)
	}
	if e.Kind != kind || e.Status != status {
		t.Errorf("%s: kind %v, status %d; want %v, %d (%v)", name, e.Kind, e.Status, kind, status, err)
	}
}

// TestErrorKinds: the bodies observed on the real API, the specification's and the
// gateway's.
func TestErrorKinds(t *testing.T) {
	notAvailable, err := os.ReadFile(fixturesDir + "/charging/is-at-charge-location.json")
	if err != nil {
		t.Fatal(err)
	}
	m2m := func(status int, code string) string {
		return `{"error":{"code":"` + code + `","message":"m","httpStatus":` + strconv.Itoa(status) + `,"timestamp":"2026-09-18T13:39:03.772Z"}}`
	}
	for _, tt := range []struct {
		name   string
		call   call
		status int
		body   string
		kind   Kind
		code   string
	}{
		{"credential refused", tokenCall, 401, `{"error":"invalid_client","error_description":"Invalid client credentials.","timestamp":"x"}`, KindCredentialRefused, "invalid_client"},
		{"token request invalid", tokenCall, 400, `{"error":"invalid_request","error_description":"clientId and clientSecret are required.","timestamp":"x"}`, KindOther, "invalid_request"},
		{"identity provider down", tokenCall, 503, `{"error":"temporarily_unavailable","error_description":"x","timestamp":"x"}`, KindOther, "temporarily_unavailable"},
		{"token limited", tokenCall, 429, `{"message":"Too Many Requests"}`, KindRateLimited, ""},
		{"token expired", domainCall, 401, `{"message":"Unauthorized"}`, KindUnauthorized, ""},
		{"token expired, simulator", listCall, 401, m2m(401, "AUTHN_INVALID_TOKEN"), KindUnauthorized, "AUTHN_INVALID_TOKEN"},
		{"another account", domainCall, 403, m2m(403, "AUTHZ_CLIENT_ID_MISMATCH"), KindCredentialRefused, "AUTHZ_CLIENT_ID_MISMATCH"},
		{"VIN not shared", domainCall, 403, m2m(403, "AUTHZ_VIN_UNAUTHORIZED"), KindVINUnauthorized, "AUTHZ_VIN_UNAUTHORIZED"},
		{"scope not granted", domainCall, 403, m2m(403, "AUTHZ_SCOPE_MISSING"), KindDomainNotGranted, "AUTHZ_SCOPE_MISSING"},
		{"no binding", listCall, 403, m2m(403, "AUTHZ_NO_BINDING"), KindOther, "AUTHZ_NO_BINDING"},
		{"gateway", domainCall, 403, `{"message":"Missing Authentication Token"}`, KindOther, ""},
		{"not available", domainCall, 404, string(notAvailable), KindNotAvailable, "DATA_NOT_AVAILABLE"},
		{"unknown resource", domainCall, 404, m2m(404, "VALIDATION_RESOURCE_NOT_FOUND"), KindOther, "VALIDATION_RESOURCE_NOT_FOUND"},
		{"no account", listCall, 400, m2m(400, "VALIDATION_INVALID_PARAMETER"), KindOther, "VALIDATION_INVALID_PARAMETER"},
		{"server", domainCall, 500, "boom", KindOther, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newAPIError("x", tt.call, &http.Response{StatusCode: tt.status, Header: http.Header{}}, []byte(tt.body))
			if e.Kind != tt.kind || e.Code != tt.code || e.Message == "" {
				t.Errorf("kind %v, code %q, message %q; want %v, %q", e.Kind, e.Code, e.Message, tt.kind, tt.code)
			}
		})
	}
	e := newAPIError("battery", domainCall, &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}}, notAvailable)
	if want := "battery: HTTP 404 DATA_NOT_AVAILABLE: No is-at-charge-location data is available for this vehicle."; e.Error() != want {
		t.Errorf("Error() = %q", e.Error())
	}
	if e := newAPIError("x", domainCall, &http.Response{StatusCode: http.StatusInternalServerError}, []byte(strings.Repeat("a", 300))); len(e.Message) != 200 || e.Error() != "x: HTTP 500: "+e.Message {
		t.Errorf("long body: %q", e.Error())
	}
}

func TestRateRetry(t *testing.T) {
	date := "Tue, 20 Oct 2026 10:00:00 GMT"
	for _, tt := range []struct {
		retryAfter, date string
		want             time.Duration
	}{
		{"12", "", 12 * time.Second},
		{"Tue, 20 Oct 2026 10:00:30 GMT", date, 30 * time.Second},
		{"Tue, 20 Oct 2026 09:59:00 GMT", date, defaultRateRetry}, // in the past
		{"Tue, 20 Oct 2026 10:00:30 GMT", "", defaultRateRetry},   // no date to count from
		{"0", "", defaultRateRetry},
		{"soon", "", defaultRateRetry},
		{"", "", defaultRateRetry},
	} {
		h := http.Header{"Retry-After": {tt.retryAfter}, "Date": {tt.date}}
		if got := rateRetry(h); got != tt.want {
			t.Errorf("Retry-After %q, Date %q: %v, want %v", tt.retryAfter, tt.date, got, tt.want)
		}
	}
}

// TestRequests: what the client sends, once per call, and what it accepts back.
func TestRequests(t *testing.T) {
	var calls []string
	var reply func(w http.ResponseWriter, r *http.Request)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		reply(w, r)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL+"/m2m/", srv.Client())
	ctx := context.Background()

	reply = func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Type") != "application/json" ||
			string(body) != `{"clientId":"id","clientSecret":"secret"}` {
			t.Errorf("token request: %s %s", r.Header.Get("Content-Type"), body)
		}
		_, _ = w.Write([]byte(`{"accessToken":"tok","expiresIn":3600,"tokenType":"Bearer"}`))
	}
	if tok, err := c.Token(ctx, Credential{ClientID: "id", ClientSecret: "secret", AccountID: "acc"}); err != nil || tok.Value != "tok" {
		t.Errorf("token = %+v, %v", tok, err)
	}

	reply = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("x-client-id") != "acc" {
			t.Errorf("headers: %v", r.Header)
		}
		_, _ = w.Write([]byte(`{"data":{},"meta":{"domain":"target-soc"}}`))
	}
	if _, err := c.Fetch(ctx, "tok", "acc", "VIN/1", TargetSoC); err != nil {
		t.Error(err)
	}
	reply = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"meta":{"count":0}}`)) }
	if vins, err := c.Vehicles(ctx, "tok", "acc"); err != nil || vins == nil || len(vins) != 0 {
		t.Errorf("no vehicle: %v, %v", vins, err)
	}
	want := []string{"POST /m2m/token", "GET /m2m/v1/vehicles/VIN%2F1/charging/target-soc", "GET /m2m/v1/vehicles"}
	if strings.Join(calls, ", ") != strings.Join(want, ", ") {
		t.Errorf("calls %v, want %v", calls, want)
	}

	// One call, never retried, whatever the error.
	calls = nil
	reply = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }
	if _, err := c.Fetch(ctx, "tok", "acc", "VIN1", Battery); err == nil || len(calls) != 1 {
		t.Errorf("503: %v, %d calls", err, len(calls))
	}
}

func TestInvalidResponses(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, srv.Client())
	ctx := context.Background()
	for _, b := range []string{
		"not json",
		`{"accessToken":"","expiresIn":3600,"tokenType":"Bearer"}`,
		`{"accessToken":"tok","tokenType":"Bearer"}`,
		`{"accessToken":"tok","expiresIn":3600,"tokenType":"MAC"}`,
		`{"accessToken":1}`,
	} {
		body = b
		if tok, err := c.Token(ctx, Credential{}); err == nil {
			t.Errorf("token %s accepted: %+v", b, tok)
		}
	}
	body = "not json"
	if _, err := c.Fetch(ctx, "t", "a", simVIN, Battery); err == nil {
		t.Error("invalid JSON accepted")
	}
	body = `{"data":[1]}`
	if _, err := c.Vehicles(ctx, "t", "a"); err == nil {
		t.Error("invalid list accepted")
	}
	body = `{"data":"` + strings.Repeat("a", maxBody) + `"}`
	if _, err := c.Fetch(ctx, "t", "a", simVIN, Battery); err == nil {
		t.Error("a body over maxBody accepted")
	}

	srv.Close()
	if _, err := c.Fetch(ctx, "t", "a", simVIN, Battery); err == nil {
		t.Error("server stopped: expected error")
	}
	if _, err := c.Token(ctx, Credential{}); err == nil {
		t.Error("server stopped: expected error")
	}
}
