package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/skodaapi"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/skoda"
)

const (
	skodaVIN  = "TMBJC9NY0RF000001"
	skodaVIN2 = "TMBNC7NY5SF083963"
)

// memSkoda keeps the account's MyŠkoda key in memory.
type memSkoda struct {
	mu      sync.Mutex
	key     string
	set     time.Time
	expires time.Time
	err     error
}

func (m *memSkoda) SkodaConnection(context.Context, string) (CredentialConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set.IsZero() {
		return CredentialConnection{}, m.err
	}
	return CredentialConnection{ID: "skoda", Last4: m.key[max(0, len(m.key)-4):], SetAt: m.set, ExpiresAt: m.expires}, m.err
}

func (m *memSkoda) SetSkodaCredential(_ context.Context, _, key string, expiresAt, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.key, m.set, m.expires = key, at, expiresAt
	return "skoda", nil
}

func (m *memSkoda) DeleteSkodaCredential(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.key, m.set, m.expires = "", time.Time{}, time.Time{}
	return m.err
}

// skodaCar is a Škoda the fake API reads, of no state at all: the checking of a key
// reads its information alone.
type skodaCar struct{ vin string }

func (s skodaCar) VIN() string { return s.vin }
func (s skodaCar) ReportAt(time.Time) vehicle.Report {
	return vehicle.Report{Spec: vehicle.Spec{Model: "Enyaq"}}
}

// skodaClient is runsten-api's adapter of the MyŠkoda client: a read of the information.
type skodaClient struct{ c *skoda.Client }

func (s skodaClient) CheckSkoda(ctx context.Context, key, vin string) (time.Time, error) {
	resp, err := s.c.Vehicle(ctx, key, vin, "info")
	if err != nil {
		return time.Time{}, err //nolint:wrapcheck // the client's
	}
	return resp.ExpiresAt, nil
}

// skodaSim serves the fake MyŠkoda Public API with the vehicles given.
func skodaSim(t *testing.T, clk clock.Clock, vins ...string) (skodaClient, *skodaapi.Handler) {
	t.Helper()
	srcs := make([]skodaapi.Source, len(vins))
	for i, vin := range vins {
		srcs[i] = skodaCar{vin: vin}
	}
	h := skodaapi.NewHandler(srcs, clk, skodaapi.DevAPIKey, skodaapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return skodaClient{c: skoda.NewClient(srv.URL+skodaapi.Prefix, srv.Client())}, h
}

// skodaDown answers the checking of a key with an error.
type skodaDown struct{ err error }

func (f skodaDown) CheckSkoda(context.Context, string, string) (time.Time, error) {
	return time.Time{}, f.err //nolint:wrapcheck // the fake's own error
}

func skodaBody(key string, vins ...string) string {
	return `{"api_key":"` + key + `","vins":["` + strings.Join(vins, `","`) + `"]}`
}

// TestSkodaCredential: the owner's key is checked on each VIN, stored with its expiry and
// its vehicles, shown again by its last four characters only, and removed; it is never
// returned nor logged.
func TestSkodaCredential(t *testing.T) {
	e := newEnv(t, 10)
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	client, h := skodaSim(t, e.clk, skodaVIN, skodaVIN2)
	e.server.SkodaVehicles = client

	// A VIN pasted in lower case, with spaces, is read as the papers write it.
	resp, body := do(t, noFollow(), http.MethodPut, u+"/skoda", jsonType,
		`{"api_key":"`+skodaapi.DevAPIKey+`","vins":[" tmbjc9ny0rf000001 ","`+skodaVIN2+`"]}`, c)
	if resp.status != http.StatusOK {
		t.Fatalf("PUT: %d %s", resp.status, body)
	}
	golden(t, "connection-skoda.json", body)
	if strings.Contains(body, skodaapi.DevAPIKey) || strings.Contains(logs.String(), skodaapi.DevAPIKey) {
		t.Errorf("the key returned or logged: %s\n%s", body, logs)
	}
	if len(e.store.vehicles) != 2 || e.store.vehicles[0] != skodaVIN {
		t.Errorf("vehicles added: %v", e.store.vehicles)
	}
	if !e.skoda.set.Equal(e.clk.Now()) || !e.skoda.expires.Equal(h.ExpiresAt()) {
		t.Errorf("stored at %v, expiring %v", e.skoda.set, e.skoda.expires)
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}
	if resp, body = do(t, noFollow(), http.MethodDelete, u+"/skoda", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	if _, body = get(t, noFollow(), u, c); !strings.Contains(body, `"skoda":null`) || len(e.store.vehicles) == 0 {
		t.Errorf("after DELETE: %s, vehicles %v", body, e.store.vehicles)
	}
}

// TestSkodaCredentialRefused: a key refused, expired, that does not read a VIN, a VIN
// mistyped, the API down or the account over its cap: nothing is stored.
func TestSkodaCredentialRefused(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/skoda"
	client, h := skodaSim(t, e.clk, skodaVIN, skodaVIN2)
	e.server.SkodaVehicles = client
	tests := []struct {
		name   string
		body   string
		fault  func()
		status int
		code   errorCode
		says   string
	}{
		{name: "a key unknown", body: skodaBody("not-the-key", skodaVIN), status: http.StatusBadRequest, code: codeCredentialRefused},
		{name: "a VIN mistyped", body: skodaBody(skodaapi.DevAPIKey, "TMBJC9NY0RF00000O"), status: http.StatusBadRequest, code: codeInvalidBody},
		{name: "a VIN short", body: skodaBody(skodaapi.DevAPIKey, "TMBJC9NY0"), status: http.StatusBadRequest, code: codeInvalidBody},
		{name: "no VIN", body: `{"api_key":"` + skodaapi.DevAPIKey + `","vins":[]}`, status: http.StatusBadRequest, code: codeInvalidBody},
		{
			name: "a VIN the key does not read", body: skodaBody(skodaapi.DevAPIKey, skodaVIN, "TMBJC9NY0RF000003"),
			status: http.StatusConflict, code: codeVINNotCovered, says: "TMBJC9NY0RF000003",
		},
		{
			name: "expired", body: skodaBody(skodaapi.DevAPIKey, skodaVIN), fault: func() { h.ExpireIn(0) },
			status: http.StatusBadRequest, code: codeKeyExpired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fault != nil {
				tt.fault()
			}
			resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, tt.body, c)
			if resp.status != tt.status || !e.skoda.set.IsZero() || len(e.store.vehicles) != 0 {
				t.Errorf("%d %s", resp.status, body)
			}
			wantError(t, body, tt.code)
			if !strings.Contains(body, tt.says) {
				t.Errorf("the message does not say %q: %s", tt.says, body)
			}
		})
	}
	// Not a JSON body: refused before anything is asked of Škoda.
	resp, body := do(t, noFollow(), http.MethodPut, u, "text/plain", skodaBody(skodaapi.DevAPIKey, skodaVIN), c)
	if resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d", resp.status)
	}
	wantError(t, body, codeUnsupportedMediaType)
	e.server.SkodaVehicles = skodaDown{err: errors.New("no answer")}
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, skodaBody(skodaapi.DevAPIKey, skodaVIN), c)
	if resp.status != http.StatusBadGateway {
		t.Errorf("the API down: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)
	e.server.MaxVehicles = 1
	e.server.SkodaVehicles, _ = skodaSim(t, e.clk, skodaVIN, skodaVIN2)
	if resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, skodaBody(skodaapi.DevAPIKey, skodaVIN, skodaVIN2), c); resp.status != http.StatusConflict {
		t.Errorf("the cap: %d %s", resp.status, body)
	}
	wantError(t, body, codeTooManyVehicles)
	// The same car pasted twice is one: within the cap.
	if resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, skodaBody(skodaapi.DevAPIKey, skodaVIN, strings.ToLower(skodaVIN)), c); resp.status != http.StatusOK || len(e.store.vehicles) != 1 {
		t.Errorf("the same VIN twice: %d %s, vehicles %v", resp.status, body, e.store.vehicles)
	}
	e.skoda.set, e.store.vehicles = time.Time{}, nil
	// The store failing.
	e.server.MaxVehicles = 0
	e.skoda.err = errors.New("database down")
	if resp, _ = do(t, noFollow(), http.MethodPut, u, jsonType, skodaBody(skodaapi.DevAPIKey, skodaVIN), c); resp.status != http.StatusInternalServerError {
		t.Errorf("the store down: %d", resp.status)
	}
	if resp, _ = do(t, noFollow(), http.MethodDelete, u, "", "", c); resp.status != http.StatusInternalServerError {
		t.Errorf("DELETE, the store down: %d", resp.status)
	}
	if resp, _ = get(t, noFollow(), e.api.URL+apiPrefix+"/connection", c); resp.status != http.StatusInternalServerError {
		t.Errorf("GET, the store down: %d", resp.status)
	}
}

// TestSkodaFlag: with source.skoda off for the account, no key is taken.
func TestSkodaFlag(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{off: account}
	c := e.session(t, "admin")
	e.server.SkodaVehicles, _ = skodaSim(t, e.clk, skodaVIN)
	resp, body := do(t, noFollow(), http.MethodPut, e.api.URL+apiPrefix+"/connection/skoda", jsonType,
		skodaBody(skodaapi.DevAPIKey, skodaVIN), c)
	if resp.status != http.StatusForbidden {
		t.Errorf("the flag off: %d %s", resp.status, body)
	}
	wantError(t, body, codeFeatureUnavailable)
}
