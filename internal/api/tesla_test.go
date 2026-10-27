package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/teslaapi"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/tesla"
)

const teslaVIN = "LRW3E7EK1RC000001"

// memTesla is an in-memory TeslaTokens, of one account's connection.
type memTesla struct {
	mu           sync.Mutex
	token, given string
	setAt        time.Time
	refused      bool
	err          error
}

func (m *memTesla) TeslaConnection(context.Context, string) (TeslaConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setAt.IsZero() {
		return TeslaConnection{}, m.err
	}
	c := TeslaConnection{ID: "tconn", Last4: m.given[len(m.given)-4:], SetAt: m.setAt}
	if m.refused {
		c.RefusedAt = m.setAt.Add(time.Hour)
	}
	return c, m.err
}

func (m *memTesla) SetTeslaToken(_ context.Context, _, token, given string, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.token, m.given, m.setAt, m.refused = token, given, at, false
	return "tconn", nil
}

func (m *memTesla) DeleteTeslaToken(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token, m.given, m.setAt, m.refused = "", "", time.Time{}, false
	return m.err
}

// tsSource is a Tesla the simulated Owner API lists.
type tsSource string

func (s tsSource) VIN() string                     { return string(s) }
func (tsSource) ReportAt(time.Time) vehicle.Report { return vehicle.Report{} }
func (tsSource) StateAt(time.Time) vehicle.State   { return vehicle.State{} }

// tsClient is runsten-api's adapter of the client: a refresh, then the vehicles.
type tsClient struct{ c *tesla.Client }

func (t tsClient) TeslaVehicles(ctx context.Context, refreshToken string) ([]string, string, error) {
	tok, err := t.c.Refresh(ctx, refreshToken)
	if err != nil {
		return nil, "", err
	}
	products, err := t.c.Products(ctx, tok.Access)
	if err != nil {
		return nil, "", err
	}
	vins := make([]string, len(products))
	for i, p := range products {
		vins[i] = p.VIN
	}
	return vins, tok.Refresh, nil
}

// teslaServer is the simulated Owner API, and runsten-api's client of it.
type teslaServer struct {
	srv    *httptest.Server
	client tsClient
}

// teslaSim serves the simulated Owner API with the vehicles vins.
func teslaSim(t *testing.T, clk clock.Clock, vins ...string) teslaServer {
	t.Helper()
	srcs := make([]teslaapi.Source, len(vins))
	for i, vin := range vins {
		srcs[i] = tsSource(vin)
	}
	srv := httptest.NewServer(teslaapi.NewHandler(srcs, clk, teslaapi.DefaultBehavior()))
	t.Cleanup(srv.Close)
	return teslaServer{srv: srv, client: tsClient{tesla.NewClient(tesla.Hosts{API: srv.URL, Auth: srv.URL + teslaapi.AuthPrefix}, srv.Client())}}
}

// control sets a fault of the simulator, and returns its body.
func (s teslaServer) control(t *testing.T, path string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.srv.URL+teslaapi.ControlPrefix+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// issued is a new refresh token of the simulator, as Tesla Auth gives the owner.
func (s teslaServer) issued(t *testing.T) string {
	t.Helper()
	body := s.control(t, "refresh-token")
	_, tok, ok := strings.Cut(body, `"refresh_token":"`)
	if !ok {
		t.Fatalf("issued: %s", body)
	}
	return strings.TrimSuffix(strings.TrimSpace(tok), `"}`)
}

func tokenBody(tok string) string { return `{"refresh_token":"` + tok + `"}` }

// TestTeslaToken: the owner's refresh token is checked, the one Tesla gives in exchange
// stored with its vehicles, shown again by the last four characters of the one given
// only, and removed; neither is returned nor logged.
func TestTeslaToken(t *testing.T) {
	e := newEnv(t, 10)
	sim := teslaSim(t, e.clk, teslaVIN)
	e.server.TeslaVehicles = sim.client
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	given := sim.issued(t)

	resp, body := do(t, noFollow(), http.MethodPut, u+"/tesla", jsonType, tokenBody(given), c)
	if resp.status != http.StatusOK {
		t.Fatalf("PUT: %d %s", resp.status, body)
	}
	if !strings.Contains(body, `"tesla":{"last4":"`+given[len(given)-4:]+`"`) {
		t.Errorf("connection %s", body)
	}
	// The simulator's refresh tokens are single use: the one kept is the one given back.
	if e.tesla.given != given || e.tesla.token == given || e.tesla.token == "" || !e.tesla.setAt.Equal(e.clk.Now()) {
		t.Errorf("stored %q for %q at %v", e.tesla.token, e.tesla.given, e.tesla.setAt)
	}
	for _, secret := range []string{given, e.tesla.token} {
		if strings.Contains(body, secret) || strings.Contains(logs.String(), secret) {
			t.Errorf("a token returned or logged: %s\n%s", body, logs)
		}
	}
	if len(e.store.vehicles) != 1 || e.store.vehicles[0] != teslaVIN {
		t.Errorf("vehicles added: %v", e.store.vehicles)
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}
	if resp, body := do(t, noFollow(), http.MethodDelete, u+"/tesla", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	if _, body := get(t, noFollow(), u, c); !strings.HasSuffix(body, `"tesla":null}`+"\n") || !e.tesla.setAt.IsZero() {
		t.Errorf("after DELETE: %s", body)
	}

	// Used already, then revoked: refused, neither stored nor its vehicles added.
	e.store.vehicles = nil
	resp, body = do(t, noFollow(), http.MethodPut, u+"/tesla", jsonType, tokenBody(given), c)
	if resp.status != http.StatusBadRequest || !e.tesla.setAt.IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("a token used: %d %s", resp.status, body)
	}
	wantError(t, body, codeCredentialRefused)

	// The Owner API closed to the account.
	sim.control(t, "owner-api?closed=true")
	resp, body = do(t, noFollow(), http.MethodPut, u+"/tesla", jsonType, tokenBody(sim.issued(t)), c)
	if resp.status != http.StatusConflict || !e.tesla.setAt.IsZero() {
		t.Errorf("closed: %d %s", resp.status, body)
	}
	wantError(t, body, codeAPIClosed)
	sim.control(t, "owner-api?closed=false")

	for _, tt := range []struct{ name, body string }{
		{"too short", `{"refresh_token":"abc"}`},
		{"a space", `{"refresh_token":"eyJhbGciOiJSUzI1 NiIsInR5cCI6IkpXVCJ9"}`},
		{"none", `{}`},
		{"unknown field", `{"refresh_token":"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9","access_token":"x"}`},
	} {
		resp, body := do(t, noFollow(), http.MethodPut, u+"/tesla", jsonType, tt.body, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		wantError(t, body, codeInvalidBody)
	}
	resp, body = do(t, noFollow(), http.MethodPut, u+"/tesla", "text/plain", tokenBody(given), c)
	if resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d", resp.status)
	}
	wantError(t, body, codeUnsupportedMediaType)
	for _, req := range []struct{ method, body string }{{http.MethodPut, tokenBody(given)}, {http.MethodDelete, ""}} {
		resp, body := do(t, noFollow(), req.method, u+"/tesla", jsonType, req.body)
		if resp.status != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d", req.method, resp.status)
		}
		wantError(t, body, codeUnauthorized)
	}

	e.tesla.err = errors.New("boom")
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "", ""}, {http.MethodPut, "/tesla", tokenBody(sim.issued(t))}, {http.MethodDelete, "/tesla", ""},
	} {
		resp, body := do(t, noFollow(), req.method, u+req.path, jsonType, req.body, c)
		if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
			t.Errorf("%s, store down: %d %s", req.method, resp.status, body)
		}
		wantError(t, body, codeInternal)
	}
}

// TestTeslaTokenFlag: with source.tesla off for the account, a token is refused before
// Tesla is asked, nothing stored; one already given may still be removed.
func TestTeslaTokenFlag(t *testing.T) {
	e := newEnv(t, 10)
	sim := teslaSim(t, e.clk, teslaVIN)
	e.server.TeslaVehicles = sim.client
	e.server.Flags = &accountFlags{off: account}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/tesla"
	given := sim.issued(t)
	resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, tokenBody(given), c)
	if resp.status != http.StatusForbidden || !e.tesla.setAt.IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("flag off: %d %s", resp.status, body)
	}
	wantError(t, body, codeFeatureUnavailable)
	if resp, body := do(t, noFollow(), http.MethodDelete, u, "", "", c); resp.status != http.StatusNoContent {
		t.Errorf("DELETE, flag off: %d %s", resp.status, body)
	}
	// Not asked: the token given still refreshes.
	e.server.Flags = nil
	if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, tokenBody(given), c); resp.status != http.StatusOK {
		t.Errorf("flag on: %d %s", resp.status, body)
	}
}

// TestTeslaTokenUnchecked: a token whose account has no vehicle, or that Tesla could not
// check, or that brings the account over its cap, is not stored.
func TestTeslaTokenUnchecked(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/tesla"

	empty := teslaSim(t, e.clk)
	e.server.TeslaVehicles = empty.client
	resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, tokenBody(empty.issued(t)), c)
	if resp.status != http.StatusConflict || !e.tesla.setAt.IsZero() {
		t.Errorf("no vehicle: %d %s", resp.status, body)
	}
	wantError(t, body, codeNoVehicle)

	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	e.server.TeslaVehicles = tsClient{tesla.NewClient(tesla.Hosts{API: down.URL, Auth: down.URL}, down.Client())}
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, tokenBody(teslaapi.DevRefreshToken()), c)
	if resp.status != http.StatusBadGateway || !e.tesla.setAt.IsZero() {
		t.Errorf("unreachable: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)

	// The cap counts the account's other vehicles.
	sim := teslaSim(t, e.clk, teslaVIN)
	e.server.TeslaVehicles = sim.client
	e.server.MaxVehicles = 1
	e.reader.vehicles[account] = []Vehicle{{ID: car, VIN: "YV1SMLT0000DT0001", Provider: "volvo"}}
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, tokenBody(sim.issued(t)), c)
	if resp.status != http.StatusConflict || !e.tesla.setAt.IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("over the cap: %d %s", resp.status, body)
	}
	wantError(t, body, codeTooManyVehicles)
	e.reader.vehicles[account] = []Vehicle{{ID: car, VIN: teslaVIN, Provider: "tesla"}}
	if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, tokenBody(sim.issued(t)), c); resp.status != http.StatusOK {
		t.Errorf("the same vehicle again: %d %s", resp.status, body)
	}
}
