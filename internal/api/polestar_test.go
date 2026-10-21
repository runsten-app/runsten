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
	"runsten/internal/polestar"
	"runsten/internal/simulator/polestarapi"
	"runsten/internal/simulator/vehicle"
)

const polestarVIN = "LPSVSEDE5RL000001"

// memPolestar is an in-memory PolestarCredentials, of one account's connection.
type memPolestar struct {
	mu      sync.Mutex
	cred    PolestarCredential
	setAt   time.Time
	refused bool
	err     error
}

func (m *memPolestar) PolestarConnection(context.Context, string) (PolestarConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setAt.IsZero() {
		return PolestarConnection{}, m.err
	}
	c := PolestarConnection{ID: "pconn", Last4: m.cred.ClientID[len(m.cred.ClientID)-4:], SetAt: m.setAt}
	if m.refused {
		c.RefusedAt = m.setAt.Add(time.Hour)
	}
	return c, m.err
}

func (m *memPolestar) SetPolestarCredential(_ context.Context, _ string, c PolestarCredential, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.cred, m.setAt, m.refused = c, at, false
	return "pconn", nil
}

func (m *memPolestar) DeletePolestarCredential(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cred, m.setAt, m.refused = PolestarCredential{}, time.Time{}, false
	return m.err
}

// psSource is a Polestar the simulated Data Portal lists.
type psSource string

func (s psSource) VIN() string                     { return string(s) }
func (psSource) ReportAt(time.Time) vehicle.Report { return vehicle.Report{} }

// psClient is runsten-api's adapter of the client: a token, then the vehicles.
type psClient struct{ c *polestar.Client }

func (p psClient) PolestarVehicles(ctx context.Context, cred PolestarCredential) ([]string, error) {
	tok, err := p.c.Token(ctx, polestar.Credential(cred))
	if err != nil {
		return nil, err
	}
	return p.c.Vehicles(ctx, tok.Value, cred.AccountID)
}

// polestarSim serves the simulated Data Portal with the vehicles vins, and returns the
// client of runsten-api against it.
func polestarSim(t *testing.T, clk clock.Clock, vins ...string) psClient {
	t.Helper()
	srcs := make([]polestarapi.Source, len(vins))
	for i, vin := range vins {
		srcs[i] = psSource(vin)
	}
	srv := httptest.NewServer(polestarapi.NewHandler(srcs, clk, polestarapi.DevCredential(), polestarapi.DefaultLimits(), polestarapi.DefaultBehavior()))
	t.Cleanup(srv.Close)
	return psClient{polestar.NewClient(srv.URL+polestarapi.Prefix, srv.Client())}
}

func credentialBody(c polestarapi.Credential) string {
	return `{"client_id":"` + c.ClientID + `","client_secret":"` + c.ClientSecret + `","account_id":"` + c.AccountID + `"}`
}

// TestPolestarCredential: the owner's credential of the Data Portal is checked, stored
// with its vehicles, shown again by its last four characters only, and removed; it is
// never returned nor logged.
func TestPolestarCredential(t *testing.T) {
	e := newEnv(t, 10)
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	dev := polestarapi.DevCredential()

	resp, body := do(t, noFollow(), http.MethodPut, u+"/polestar", jsonType, credentialBody(dev), c)
	if resp.status != http.StatusOK {
		t.Fatalf("PUT: %d %s", resp.status, body)
	}
	golden(t, "connection-polestar.json", body)
	// The simulator's client ID is also the Volvo application's (client_id): only the
	// logs are searched for it.
	for _, secret := range []string{dev.ClientSecret, dev.AccountID, dev.ClientID} {
		if (secret != dev.ClientID && strings.Contains(body, secret)) || strings.Contains(logs.String(), secret) {
			t.Errorf("%q returned or logged: %s\n%s", secret, body, logs)
		}
	}
	if e.polestar.cred != PolestarCredential(dev) || !e.polestar.setAt.Equal(e.clk.Now()) {
		t.Errorf("stored %+v at %v", e.polestar.cred, e.polestar.setAt)
	}
	if len(e.store.vehicles) != 1 || e.store.vehicles[0] != polestarVIN {
		t.Errorf("vehicles added: %v", e.store.vehicles)
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}
	if resp, body := do(t, noFollow(), http.MethodDelete, u+"/polestar", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	if _, body := get(t, noFollow(), u, c); !strings.HasSuffix(body, `"polestar":null}`+"\n") || !e.polestar.setAt.IsZero() {
		t.Errorf("after DELETE: %s", body)
	}

	// Refused: neither stored nor its vehicles added.
	e.store.vehicles = nil
	wrong := dev
	wrong.ClientSecret = "not-the-secret"
	resp, body = do(t, noFollow(), http.MethodPut, u+"/polestar", jsonType, credentialBody(wrong), c)
	if resp.status != http.StatusBadRequest || !e.polestar.setAt.IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("a credential refused: %d %s", resp.status, body)
	}
	wantError(t, body, codeCredentialRefused)
	wrong = dev
	wrong.AccountID = "00000000-0000-4000-8000-000000000000" // another account: AUTHZ_CLIENT_ID_MISMATCH
	resp, body = do(t, noFollow(), http.MethodPut, u+"/polestar", jsonType, credentialBody(wrong), c)
	if resp.status != http.StatusBadRequest || !e.polestar.setAt.IsZero() {
		t.Errorf("another account ID: %d %s", resp.status, body)
	}
	wantError(t, body, codeCredentialRefused)

	for _, tt := range []struct{ name, body string }{
		{"too short", `{"client_id":"abc","client_secret":"runsten-dev-secret","account_id":"` + dev.AccountID + `"}`},
		{"a space", `{"client_id":"runsten dev","client_secret":"runsten-dev-secret","account_id":"` + dev.AccountID + `"}`},
		{"no secret", `{"client_id":"runsten-dev","account_id":"` + dev.AccountID + `"}`},
		{"unknown field", `{"client_id":"runsten-dev","client_secret":"runsten-dev-secret","account_id":"x1234","scope":"all"}`},
	} {
		resp, body := do(t, noFollow(), http.MethodPut, u+"/polestar", jsonType, tt.body, c)
		if resp.status != http.StatusBadRequest || strings.Contains(body, "runsten-dev-secret") {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		wantError(t, body, codeInvalidBody)
	}
	resp, body = do(t, noFollow(), http.MethodPut, u+"/polestar", "text/plain", credentialBody(dev), c)
	if resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d", resp.status)
	}
	wantError(t, body, codeUnsupportedMediaType)
	for _, req := range []struct{ method, body string }{{http.MethodPut, credentialBody(dev)}, {http.MethodDelete, ""}} {
		resp, body := do(t, noFollow(), req.method, u+"/polestar", jsonType, req.body)
		if resp.status != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d", req.method, resp.status)
		}
		wantError(t, body, codeUnauthorized)
	}
	if !e.polestar.setAt.IsZero() {
		t.Error("a credential stored without a session, or an invalid one")
	}

	e.polestar.err = errors.New("boom")
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "", ""}, {http.MethodPut, "/polestar", credentialBody(dev)}, {http.MethodDelete, "/polestar", ""},
	} {
		resp, body := do(t, noFollow(), req.method, u+req.path, jsonType, req.body, c)
		if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
			t.Errorf("%s, store down: %d %s", req.method, resp.status, body)
		}
		wantError(t, body, codeInternal)
	}
}

// TestPolestarCredentialUnchecked: a credential that reads no vehicle, or that the Data
// Portal could not check, or that brings the account over its cap, is not stored.
func TestPolestarCredentialUnchecked(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/polestar"
	dev := credentialBody(polestarapi.DevCredential())

	e.server.PolestarVehicles = polestarSim(t, e.clk)
	resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, dev, c)
	if resp.status != http.StatusConflict || !e.polestar.setAt.IsZero() {
		t.Errorf("no vehicle: %d %s", resp.status, body)
	}
	wantError(t, body, codeNoVehicle)

	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	e.server.PolestarVehicles = psClient{polestar.NewClient(down.URL, down.Client())}
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, dev, c)
	if resp.status != http.StatusBadGateway || !e.polestar.setAt.IsZero() {
		t.Errorf("unreachable: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)

	// The cap counts the account's other vehicles.
	e.server.PolestarVehicles = polestarSim(t, e.clk, polestarVIN)
	e.server.MaxVehicles = 1
	e.reader.vehicles[account] = []Vehicle{{ID: car, VIN: "YV1SMLT0000DT0001", Provider: "volvo"}}
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, dev, c)
	if resp.status != http.StatusConflict || !e.polestar.setAt.IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("over the cap: %d %s", resp.status, body)
	}
	wantError(t, body, codeTooManyVehicles)
	// Given again, the same vehicle does not count twice.
	e.reader.vehicles[account] = []Vehicle{{ID: car, VIN: polestarVIN, Provider: "polestar"}}
	if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, dev, c); resp.status != http.StatusOK {
		t.Errorf("the same vehicle again: %d %s", resp.status, body)
	}
}
