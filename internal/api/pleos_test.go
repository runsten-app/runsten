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
	"runsten/internal/pleos"
	"runsten/internal/simulator/pleosapi"
	"runsten/internal/simulator/vehicle"
)

const pleosVIN = "KNAPC81ABPP000001"

// memPleos keeps the account's key of each brand in memory.
type memPleos struct {
	mu   sync.Mutex
	keys map[string]PleosCredential
	set  map[string]time.Time
	err  error
}

func (m *memPleos) PleosConnection(_ context.Context, _ string, brand string) (CredentialConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.set[brand]
	if !ok || at.IsZero() {
		return CredentialConnection{}, m.err
	}
	c := CredentialConnection{ID: "pleos-" + brand, Last4: m.keys[brand].ClientID[max(0, len(m.keys[brand].ClientID)-4):], SetAt: at}
	return c, m.err
}

func (m *memPleos) SetPleosCredential(_ context.Context, _ string, brand string, c PleosCredential, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	if m.keys == nil {
		m.keys, m.set = map[string]PleosCredential{}, map[string]time.Time{}
	}
	m.keys[brand], m.set[brand] = c, at
	return "pleos-" + brand, nil
}

func (m *memPleos) DeletePleosCredential(_ context.Context, _ string, brand string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, brand)
	delete(m.set, brand)
	return m.err
}

// pleosKeyBody is the key the owner pastes, as the request carries it.
func pleosKeyBody(c pleosapi.Credential) string {
	return `{"client_id":"` + c.ClientID + `","client_secret":"` + c.ClientSecret + `"}`
}

// pleosSource is a car of one brand the fake API lists, of no state at all: the
// checking of a key reads the list alone.
type pleosSource struct {
	vin   string
	brand string
}

func (s pleosSource) VIN() string                       { return s.vin }
func (s pleosSource) Brand() string                     { return s.brand }
func (s pleosSource) ReportAt(time.Time) vehicle.Report { return vehicle.Report{} }

// pleosClient is runsten-api's adapter of the Pleos client: a token, then the VINs.
type pleosClient struct{ c *pleos.Client }

func (p pleosClient) PleosVehicles(ctx context.Context, brand string, c PleosCredential) ([]string, error) {
	tok, err := p.c.Token(ctx, pleos.Brand(brand), pleos.Credential{ClientID: c.ClientID, ClientSecret: c.ClientSecret})
	if err != nil {
		return nil, err //nolint:wrapcheck // the client's, with its context
	}
	return p.c.Vehicles(ctx, tok.Value, pleos.Brand(brand)) //nolint:wrapcheck // the client's
}

// pleosVehicles says no key reads any car, until a test gives one that does.
type pleosVehicles struct{}

func (pleosVehicles) PleosVehicles(context.Context, string, PleosCredential) ([]string, error) {
	return nil, nil //nolint:nilnil // no vehicle: the answer the checking expects
}

// pleosSim serves the fake Vehicle Data API with the vehicles of one brand.
func pleosSim(t *testing.T, clk clock.Clock, brand string, vins ...string) pleosClient {
	t.Helper()
	srcs := make([]pleosapi.Source, len(vins))
	for i, vin := range vins {
		srcs[i] = pleosSource{vin: vin, brand: brand}
	}
	srv := httptest.NewServer(pleosapi.NewHandler(srcs, clk, pleosapi.DevCredential(), pleosapi.DefaultBehavior()))
	t.Cleanup(srv.Close)
	return pleosClient{c: pleos.NewClient(srv.URL+pleosapi.Prefix, srv.Client())}
}

// TestPleosCredential: the owner's key of a brand is checked, stored with its vehicles,
// shown again by its last four characters only, and removed; it is never returned nor
// logged, and one brand's key leaves the others' alone.
func TestPleosCredential(t *testing.T) {
	e := newEnv(t, 10)
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	dev := pleosapi.DevCredential()
	e.server.PleosVehicles = pleosSim(t, e.clk, string(pleos.Kia), pleosVIN)

	resp, body := do(t, noFollow(), http.MethodPut, u+"/pleos/kia", jsonType, pleosKeyBody(dev), c)
	if resp.status != http.StatusOK {
		t.Fatalf("PUT: %d %s", resp.status, body)
	}
	golden(t, "connection-pleos.json", body)
	// The simulator's client ID is also the Volvo application's (client_id): only the
	// secret is searched for in the body, the two of them in the logs.
	if strings.Contains(body, dev.ClientSecret) || strings.Contains(logs.String(), dev.ClientSecret) ||
		strings.Contains(logs.String(), dev.ClientID) {
		t.Errorf("the key returned or logged: %s\n%s", body, logs)
	}
	if len(e.store.vehicles) != 1 || e.store.vehicles[0] != pleosVIN {
		t.Errorf("vehicles added: %v", e.store.vehicles)
	}
	if !e.pleos.set["kia"].Equal(e.clk.Now()) {
		t.Errorf("stored at %v", e.pleos.set["kia"])
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}
	// Another brand's key, kept in its own right beside the Kia's.
	e.server.PleosVehicles = pleosSim(t, e.clk, string(pleos.Hyundai), "TMAPJ61AB0P000002")
	if resp, body = do(t, noFollow(), http.MethodPut, u+"/pleos/hyundai", jsonType, pleosKeyBody(dev), c); resp.status != http.StatusOK {
		t.Fatalf("the second brand: %d %s", resp.status, body)
	}
	if e.pleos.keys["hyundai"].ClientID == "" || e.pleos.keys["kia"].ClientID == "" {
		t.Errorf("one brand's key lost another's: %+v", e.pleos.keys)
	}
	if resp, body = do(t, noFollow(), http.MethodDelete, u+"/pleos/kia", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	// The removal keeps the Hyundai's key and the vehicles.
	if _, body = get(t, noFollow(), u, c); !strings.Contains(body, `"hyundai":{"last4"`) || strings.Contains(body, `"kia":{"last4"`) {
		t.Errorf("after DELETE: %s", body)
	}
	if len(e.store.vehicles) == 0 {
		t.Error("the removal took the vehicles")
	}

	// Not a JSON body: refused before anything is asked of Pleos.
	resp, body = do(t, noFollow(), http.MethodPut, u+"/pleos/kia", "text/plain", pleosKeyBody(dev), c)
	if resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d", resp.status)
	}
	wantError(t, body, codeUnsupportedMediaType)

	// Refused: neither stored nor its vehicles added.
	e.store.vehicles = nil
	wrong := dev
	wrong.ClientSecret = "not-the-secret"
	resp, body = do(t, noFollow(), http.MethodPut, u+"/pleos/kia", jsonType, pleosKeyBody(wrong), c)
	if resp.status != http.StatusBadRequest || !e.pleos.set["kia"].IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("a key refused: %d %s", resp.status, body)
	}
	wantError(t, body, codeCredentialRefused)
}

// TestPleosCredentialUnchecked: a key that reads no vehicle, that the API could not
// check, or that brings the account over its cap, is not stored.
func TestPleosCredentialUnchecked(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/pleos"
	key := pleosKeyBody(pleosapi.DevCredential())

	// No vehicle: the common case of a key made with another account than the car's
	// primary user's. The three causes are named in the answer.
	e.server.PleosVehicles = pleosSim(t, e.clk, string(pleos.Kia))
	resp, body := do(t, noFollow(), http.MethodPut, u+"/kia", jsonType, key, c)
	if resp.status != http.StatusConflict || len(e.store.vehicles) != 0 {
		t.Errorf("no vehicle: %d %s", resp.status, body)
	}
	wantError(t, body, codeNoVehicle)
	if !strings.Contains(body, "primary") {
		t.Errorf("the causes untold: %s", body)
	}
	// The API did not answer.
	e.server.PleosVehicles = pleosDown{err: errors.New("no answer")}
	if resp, body = do(t, noFollow(), http.MethodPut, u+"/kia", jsonType, key, c); resp.status != http.StatusBadGateway {
		t.Errorf("the API down: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)
	// The cap on vehicles.
	e.server.MaxVehicles = 1
	e.server.PleosVehicles = pleosSim(t, e.clk, string(pleos.Kia), pleosVIN, "TMAPJ61AB0P000002")
	if resp, body = do(t, noFollow(), http.MethodPut, u+"/kia", jsonType, key, c); resp.status != http.StatusConflict {
		t.Errorf("the cap: %d %s", resp.status, body)
	}
	wantError(t, body, codeTooManyVehicles)
}

// pleosDown answers the checking of a key with an error.
type pleosDown struct{ err error }

func (f pleosDown) PleosVehicles(context.Context, string, PleosCredential) ([]string, error) {
	return nil, f.err //nolint:wrapcheck // the fake's own error
}

// TestPleosFlag: with source.pleos off for the account, no key is taken; a connection
// given before may still be removed.
func TestPleosFlag(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{off: account}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/pleos"
	e.server.PleosVehicles = pleosSim(t, e.clk, string(pleos.Kia), pleosVIN)
	key := pleosKeyBody(pleosapi.DevCredential())
	resp, body := do(t, noFollow(), http.MethodPut, u+"/kia", jsonType, key, c)
	if resp.status != http.StatusForbidden {
		t.Errorf("the flag off: %d %s", resp.status, body)
	}
	wantError(t, body, codeFeatureUnavailable)
	// The brands are the path's own words: only the API's three are a route.
	if resp, _ := do(t, noFollow(), http.MethodPut, u+"/volvo", jsonType, key, c); resp.status != http.StatusBadRequest {
		t.Errorf("a brand of no API: %d", resp.status)
	}
}
