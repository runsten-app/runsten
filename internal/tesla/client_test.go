package tesla

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The recorded responses of a Model Y Long Range AWD, Europe, anonymised: the VIN
// without its serial, the identifiers, the name, the position (Brussels' Grand-Place),
// the keys and blobs masked.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // repository file
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const (
	fixtureID  = "1234567890123456"
	fixtureVIN = "LRWYGCEK9MC000000"
	token      = "access-token"
)

// fakeAPI serves the Owner API's paths from the fixtures, and fails the test on any
// other request: a wake_up above all.
func fakeAPI(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	serve := func(name string) http.HandlerFunc {
		return auth(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(fixture(t, name)) })
	}
	mux.HandleFunc("GET /api/1/products", serve("products.json"))
	mux.HandleFunc("GET /api/1/vehicles/"+fixtureID, serve("vehicle.json"))
	mux.HandleFunc("GET /api/1/vehicles/"+fixtureID+"/vehicle_data", auth(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("endpoints"); !strings.Contains(got, "location_data") || !strings.Contains(got, "vehicle_state") {
			t.Errorf("endpoints = %q", got)
		}
		_, _ = w.Write(fixture(t, "vehicle_data.json"))
	}))
	mux.HandleFunc("GET /api/1/vehicles/asleep/vehicle_data", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusRequestTimeout)
		_, _ = w.Write(fixture(t, "vehicle_data-408.json"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewClient(Hosts{API: srv.URL + "/", Auth: srv.URL}, srv.Client())
}

func TestProductsVehicleAndData(t *testing.T) {
	c := fakeAPI(t)
	ctx := context.Background()
	ps, err := c.Products(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Product{{ID: fixtureID, VIN: fixtureVIN}}; !reflect.DeepEqual(ps, want) {
		t.Errorf("products = %+v", ps)
	}

	raw, err := c.Vehicle(ctx, token, fixtureID)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := StateOf(raw); err != nil || st != Online {
		t.Errorf("state = %q, %v", st, err)
	}

	raw, err = c.VehicleData(ctx, token, fixtureID)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := StateOf(raw); err != nil || st != Online {
		t.Errorf("vehicle_data state = %q, %v", st, err)
	}
	// What plays is gone, the rest kept.
	if bytes.Contains(raw, []byte("media_info")) || bytes.Contains(raw, []byte("now_playing")) {
		t.Error("media_info kept")
	}
	want, _ := Normalize(VehicleData, fixture(t, "vehicle_data.json"))
	if got, err := Normalize(VehicleData, raw); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("normalized: %+v, %v", got, err)
	}
	wantH, _ := NormalizeHealth(VehicleData, fixture(t, "vehicle_data.json"))
	if got, err := NormalizeHealth(VehicleData, raw); err != nil || !reflect.DeepEqual(got, wantH) {
		t.Errorf("health: %+v, %v", got, err)
	}

	_, err = c.VehicleData(ctx, token, "asleep")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Kind != KindUnavailable || ae.Endpoint != "vehicle_data" ||
		ae.Message != "vehicle unavailable: vehicle is offline or asleep" {
		t.Errorf("asleep: %#v", err)
	}

	_, err = c.Products(ctx, "expired")
	if !errors.As(err, &ae) || ae.Kind != KindUnauthorized {
		t.Errorf("expired: %v", err)
	}
}

func TestProductsKeepsVehiclesOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A Powerwall has no VIN; an id beyond float64's exact integers without id_s.
		_, _ = w.Write([]byte(`{"response": [
			{"energy_site_id": 42, "resource_type": "battery"},
			{"id": 12345678901234567891, "vin": "5YJ3E1EA7KF000001"}
		], "count": 2}`))
	}))
	t.Cleanup(srv.Close)
	ps, err := NewClient(Hosts{API: srv.URL}, srv.Client()).Products(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Product{{ID: "12345678901234567891", VIN: "5YJ3E1EA7KF000001"}}; !reflect.DeepEqual(ps, want) {
		t.Errorf("products = %+v", ps)
	}
}

func TestWithoutMedia(t *testing.T) {
	for _, raw := range []string{
		`{"response": null, "error": "x"}`,
		`{"response": {"charge_state": {}}}`,
		`{"response": {"vehicle_state": {"odometer": 1}}}`,
		`{"response": "odd"}`,
	} {
		if got, err := withoutMedia([]byte(raw)); err != nil || string(got) != raw {
			t.Errorf("%s: %s, %v", raw, got, err)
		}
	}
	got, err := withoutMedia([]byte(`{"response": {"vehicle_state": {"odometer": 1, "media_info": {"now_playing_title": "x"}}}, "x": [1]}`))
	if err != nil || string(got) != `{"response":{"vehicle_state":{"odometer":1}},"x":[1]}` {
		t.Errorf("%s, %v", got, err)
	}
	if _, err := withoutMedia([]byte(`[`)); err == nil {
		t.Error("invalid JSON: no error")
	}
}

func TestStateOf(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want State
		err  bool
	}{
		{`{"response": {"state": "offline"}}`, Offline, false},
		{`{"response": {"state": "asleep"}}`, Asleep, false},
		{`{"response": null, "error": "x"}`, "", true},
		{`{"response": {}}`, "", true},
		{`not json`, "", true},
	} {
		got, err := StateOf([]byte(tc.raw))
		if got != tc.want || (err != nil) != tc.err {
			t.Errorf("%s: %q, %v", tc.raw, got, err)
		}
	}
}

// tokenServer answers the token endpoint with status and body, after checking the form.
func tokenServer(t *testing.T, status int, body string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth2/v3/token" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		for k, v := range map[string]string{
			"grant_type": "refresh_token", "client_id": "ownerapi", "refresh_token": "refresh-1",
			"scope": "openid email offline_access",
		} {
			if got := r.PostForm.Get(k); got != v {
				t.Errorf("%s = %q", k, got)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func TestRefresh(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		status int
		body   string
		want   Tokens
		kind   Kind
		code   string
		err    bool
	}{
		{
			"rotated", 200, `{"access_token": "a", "refresh_token": "refresh-2", "expires_in": 28800, "token_type": "Bearer"}`,
			Tokens{Access: "a", ExpiresIn: 8 * time.Hour, Refresh: "refresh-2"},
			0, "", false,
		},
		{
			"kept", 200, `{"access_token": "a", "expires_in": 28800, "token_type": "bearer"}`,
			Tokens{Access: "a", ExpiresIn: 8 * time.Hour, Refresh: "refresh-1"},
			0, "", false,
		},
		{"no token", 200, `{"expires_in": 28800, "token_type": "Bearer"}`, Tokens{}, KindOther, "", true},
		{
			"invalid grant", 400, `{"error": "invalid_grant", "error_description": "The refresh_token is invalid"}`,
			Tokens{},
			KindTokenRefused, "invalid_grant", true,
		},
		{"login required", 401, `{"error": "login_required"}`, Tokens{}, KindTokenRefused, "login_required", true},
		{"other", 400, `{"error": "invalid_request"}`, Tokens{}, KindOther, "invalid_request", true},
		{"server", 503, `<html>down</html>`, Tokens{}, KindOther, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(tokenServer(t, tc.status, tc.body))
			t.Cleanup(srv.Close)
			c := NewClient(Hosts{API: srv.URL, Auth: srv.URL + "/oauth2/v3/"}, srv.Client())
			got, err := c.Refresh(ctx, "refresh-1")
			if (err != nil) != tc.err || got != tc.want {
				t.Fatalf("got %+v, %v", got, err)
			}
			var ae *APIError
			if errors.As(err, &ae) {
				if ae.Kind != tc.kind || ae.Code != tc.code || ae.TokenRefused() != (tc.kind == KindTokenRefused) {
					t.Errorf("error %#v", ae)
				}
			} else if tc.kind != KindOther {
				t.Errorf("err = %v, want an APIError", err)
			}
		})
	}
}

// TestRefreshRequiresTLS13: below it, the authentication gives a token the Owner API
// refuses. The API's own calls keep the client's settings.
func TestRefreshRequiresTLS13(t *testing.T) {
	srv := httptest.NewUnstartedServer(tokenServer(t, 200, `{"access_token": "a", "expires_in": 1, "token_type": "Bearer"}`))
	srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS12} //nolint:gosec // the server the client must refuse
	srv.StartTLS()
	t.Cleanup(srv.Close)
	hc := srv.Client()
	c := NewClient(Hosts{API: srv.URL, Auth: srv.URL + "/oauth2/v3"}, hc)
	if _, err := c.Refresh(context.Background(), "refresh-1"); err == nil || !strings.Contains(err.Error(), "protocol version") {
		t.Errorf("TLS 1.2: %v", err)
	}
	if v := hc.Transport.(*http.Transport).TLSClientConfig.MinVersion; v == tls.VersionTLS13 { //nolint:forcetypeassert // httptest's
		t.Error("the caller's transport was changed")
	}

	// A transport the client cannot hold to TLS 1.3 refreshes nothing.
	c = NewClient(Hosts{Auth: srv.URL}, &http.Client{Transport: roundTripper(nil)})
	if _, err := c.Refresh(context.Background(), "refresh-1"); !errors.Is(err, errAuthTransport) {
		t.Errorf("custom transport: %v", err)
	}
	// Without a transport, the default one, held to TLS 1.3.
	c = NewClient(Hosts{Auth: srv.URL}, &http.Client{})
	if c.authHC.Transport.(*http.Transport).TLSClientConfig.MinVersion != tls.VersionTLS13 { //nolint:forcetypeassert // the client's
		t.Error("default transport not held to TLS 1.3")
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestAPIErrors: each response is the kind the collector acts on.
func TestAPIErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		status  int
		header  http.Header
		body    string
		kind    Kind
		retry   time.Duration
		message string
	}{
		{
			"expired", 401, nil, `{"response": null, "error": "invalid bearer token", "error_description": ""}`,
			KindUnauthorized, 0, "invalid bearer token",
		},
		{
			"owner api closed", 403, nil, `{"response": null, "error": "forbidden, see https://developer.tesla.com/docs/fleet-api"}`,
			KindOwnerAPIClosed, 0, "forbidden, see https://developer.tesla.com/docs/fleet-api",
		},
		{"other 403", 403, nil, `{"response": null, "error": "forbidden"}`, KindOther, 0, "forbidden"},
		{
			"rate limited", 429,
			http.Header{"Retry-After": {"30"}},
			`{"error": "too many requests"}`,
			KindRateLimited, 30 * time.Second, "too many requests",
		},
		{"rate limited, a date", 429, http.Header{
			"Retry-After": {"Mon, 26 Oct 2026 10:02:00 GMT"}, "Date": {"Mon, 26 Oct 2026 10:00:00 GMT"},
		}, ``, KindRateLimited, 2 * time.Minute, ""},
		{"rate limited, nothing said", 429, nil, `{}`, KindRateLimited, 5 * time.Minute, "{}"},
		{"server", 502, nil, `<html>Bad gateway</html>`, KindOther, 0, "<html>Bad gateway</html>"},
		{
			"a VIN masked", 404, nil, `{"response": null, "error": "not_found", "error_description": "vehicle LRWYGCEK9MC000000 not found"}`,
			KindOther, 0, "vehicle [VIN] not found",
		},
		{"408 of a list", 408, nil, `{"error": "timeout"}`, KindOther, 0, "timeout"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.header {
					w.Header()[k] = v
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			_, err := NewClient(Hosts{API: srv.URL}, srv.Client()).Products(ctx, token)
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("err = %v", err)
			}
			if ae.Kind != tc.kind || ae.RetryIn != tc.retry || ae.Message != tc.message || ae.Status != tc.status || ae.Code != "" {
				t.Errorf("%#v", ae)
			}
			if strings.Contains(err.Error(), fixtureVIN) {
				t.Errorf("VIN in %q", err)
			}
		})
	}
}

func TestAPIErrorText(t *testing.T) {
	e := &APIError{Endpoint: "products", Status: 429, Message: "slow down", Headers: []string{"Retry-After=30"}}
	if got := e.Error(); got != "products: HTTP 429: slow down [Retry-After=30]" {
		t.Errorf("%q", got)
	}
	e = &APIError{Endpoint: "token", Status: 400, Code: "invalid_grant", Message: "expired"}
	if got := e.Error(); got != "token: HTTP 400 invalid_grant: expired" {
		t.Errorf("%q", got)
	}
	if _, m := errorBody([]byte(strings.Repeat("x", 300))); len(m) != 200 {
		t.Errorf("message of %d bytes", len(m))
	}
}

func TestInvalidResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Hosts{API: srv.URL}, srv.Client())
	if _, err := c.VehicleData(context.Background(), token, "1"); err == nil {
		t.Error("vehicle_data: no error")
	}
	srv.Close()
	if _, err := c.Vehicle(context.Background(), token, "1"); err == nil {
		t.Error("closed server: no error")
	}
}
