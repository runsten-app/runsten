package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const polestarIDVIN = "LPSYCZRE5RL000014"

// memPolestarID is an in-memory PolestarIDCredentials, of one account's connection.
type memPolestarID struct {
	mu      sync.Mutex
	cred    PolestarIDCredential
	setAt   time.Time
	refused bool
	err     error
}

func (m *memPolestarID) PolestarIDConnection(context.Context, string) (PolestarIDConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setAt.IsZero() {
		return PolestarIDConnection{}, m.err
	}
	c := PolestarIDConnection{
		CredentialConnection: CredentialConnection{ID: "piconn", Last4: m.cred.Refresh[len(m.cred.Refresh)-4:], SetAt: m.setAt},
		Password:             m.cred.Email != "",
	}
	if m.refused {
		c.RefusedAt = m.setAt.Add(time.Hour)
	}
	return c, m.err
}

func (m *memPolestarID) SetPolestarID(_ context.Context, _ string, c PolestarIDCredential, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.cred, m.setAt, m.refused = c, at, false
	return "piconn", nil
}

func (m *memPolestarID) DeletePolestarID(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cred, m.setAt, m.refused = PolestarIDCredential{}, time.Time{}, false
	return m.err
}

// refusedErr is a refusal of the Polestar ID, as the client's.
type refusedErr struct{}

func (refusedErr) Error() string { return "invalid_grant" }
func (refusedErr) Refused() bool { return true }

// fakePolestarID is a Polestar ID that knows one code, of the challenge of the last
// authorization, and one password; its account has vins.
type fakePolestarID struct {
	mu        sync.Mutex
	vins      []string
	err       error // when not refused
	challenge string
	issued    int
}

const (
	piCode     = "the-code-of-the-sign-in"
	piEmail    = "owner@example.com"
	piPassword = "the owner's password"
)

func (f *fakePolestarID) AuthorizeURL(challenge, state string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.challenge = challenge
	return "https://polestarid.example/as/authorization.oauth2?" + url.Values{"code_challenge": {challenge}, "state": {state}}.Encode()
}

func (*fakePolestarID) CodeOf(pasted string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(pasted))
	if err != nil || u.Query().Get("code") == "" {
		return "", "", errors.New("no code")
	}
	return u.Query().Get("code"), u.Query().Get("state"), nil
}

func (f *fakePolestarID) cars() (PolestarIDCars, error) {
	if f.err != nil {
		return PolestarIDCars{}, f.err
	}
	f.issued++
	return PolestarIDCars{Refresh: "refresh-token-" + strconv.Itoa(f.issued) + "-1234", VINs: f.vins}, nil
}

func (f *fakePolestarID) Exchange(_ context.Context, code, verifier string) (PolestarIDCars, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256([]byte(verifier))
	if code != piCode || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
		return PolestarIDCars{}, refusedErr{}
	}
	return f.cars()
}

func (f *fakePolestarID) SignIn(_ context.Context, email, password string) (PolestarIDCars, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if email != piEmail || password != piPassword {
		return PolestarIDCars{}, refusedErr{}
	}
	return f.cars()
}

type authorizationBody struct {
	URL, State string
}

// authorize starts a sign-in, and returns its state and the address the owner pastes
// back after it.
func authorize(t *testing.T, e *env, c *http.Cookie) (state, callback string) {
	t.Helper()
	resp, body := do(t, noFollow(), http.MethodPost, e.api.URL+apiPrefix+"/connection/polestar-id/authorization", "", "", c)
	if resp.status != http.StatusOK {
		t.Fatalf("authorization: %d %s", resp.status, body)
	}
	var a authorizationBody
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Query().Get("state") != a.State || a.State == "" {
		t.Fatalf("authorization %+v", a)
	}
	return a.State, "https://www.polestar.com/sign-in-callback?" + url.Values{"code": {piCode}, "state": {a.State}}.Encode()
}

func codeBody(state, callback string) string {
	return `{"state":"` + state + `","callback":"` + callback + `"}`
}

func passwordBody(email, password string) string {
	return `{"email":"` + email + `","password":"` + password + `"}`
}

// TestPolestarIDCode: the token mode. The owner starts a sign-in, pastes back the
// address, whose code is exchanged with the verifier Runsten kept; only the refresh token
// is stored, with the cars; the sign-in is spent.
func TestPolestarIDCode(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"

	state, callback := authorize(t, e, c)
	resp, body := do(t, noFollow(), http.MethodPut, u+"/polestar-id/code", jsonType, codeBody(state, callback), c)
	if resp.status != http.StatusOK {
		t.Fatalf("PUT: %d %s", resp.status, body)
	}
	golden(t, "connection-polestar-id.json", body)
	if e.polestarID.cred.Refresh == "" || e.polestarID.cred.Email != "" || !e.polestarID.setAt.Equal(e.clk.Now()) {
		t.Errorf("stored %+v at %v", e.polestarID.cred, e.polestarID.setAt)
	}
	if strings.Contains(body, e.polestarID.cred.Refresh) || strings.Contains(logs.String(), e.polestarID.cred.Refresh) ||
		strings.Contains(logs.String(), piCode) {
		t.Errorf("a token returned or logged: %s\n%s", body, logs)
	}
	if len(e.store.vehicles) != 1 || e.store.vehicles[0] != polestarIDVIN {
		t.Errorf("vehicles added: %v", e.store.vehicles)
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}

	// Spent: the same address again is refused, nothing asked of the Polestar ID.
	issued := e.polestarIDs.issued
	resp, body = do(t, noFollow(), http.MethodPut, u+"/polestar-id/code", jsonType, codeBody(state, callback), c)
	if resp.status != http.StatusBadRequest || e.polestarIDs.issued != issued {
		t.Errorf("spent: %d %s", resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)

	// The address of another sign-in than the state sent.
	state, _ = authorize(t, e, c)
	_, other := authorize(t, e, c)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/polestar-id/code", jsonType, codeBody(state, other), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("another sign-in: %d %s", resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)

	// An address without its state is taken with the state sent; a code the
	// Polestar ID refuses is not stored.
	e.polestarID.setAt = time.Time{}
	state, _ = authorize(t, e, c)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/polestar-id/code", jsonType,
		codeBody(state, "https://www.polestar.com/sign-in-callback?code=another-code"), c)
	if resp.status != http.StatusBadRequest || !e.polestarID.setAt.IsZero() {
		t.Errorf("refused: %d %s", resp.status, body)
	}
	wantError(t, body, codeCredentialRefused)

	// An address without a code leaves the sign-in to try again.
	state, callback = authorize(t, e, c)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/polestar-id/code", jsonType,
		codeBody(state, "https://www.polestar.com/uk/auth/error"), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("no code: %d %s", resp.status, body)
	}
	wantError(t, body, codeInvalidBody)
	if resp, body := do(t, noFollow(), http.MethodPut, u+"/polestar-id/code", jsonType, codeBody(state, callback), c); resp.status != http.StatusOK {
		t.Errorf("after an address without a code: %d %s", resp.status, body)
	}

	// Another account's sign-in is not this one's.
	state, callback = authorize(t, e, e.session(t, "other"))
	resp, body = do(t, noFollow(), http.MethodPut, u+"/polestar-id/code", jsonType, codeBody(state, callback), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("another account's: %d %s", resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)

	if resp, body := do(t, noFollow(), http.MethodDelete, u+"/polestar-id", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	if _, body := get(t, noFollow(), u, c); !strings.HasSuffix(body, `"polestar_id":null,"bmw":null,"pleos":{"hyundai":null,"kia":null,"genesis":null},"skoda":null}`+"\n") || !e.polestarID.setAt.IsZero() {
		t.Errorf("after DELETE: %s", body)
	}
}

// TestPolestarIDPassword: the password mode keeps the email and password with the
// refresh token, and never returns nor logs them.
func TestPolestarIDPassword(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/polestar-id/password"

	resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, passwordBody(piEmail, "wrong"), c)
	if resp.status != http.StatusBadRequest || !e.polestarID.setAt.IsZero() {
		t.Errorf("wrong password: %d %s", resp.status, body)
	}
	wantError(t, body, codeCredentialRefused)

	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, passwordBody(piEmail, piPassword), c)
	if resp.status != http.StatusOK || !strings.Contains(body, `"polestar_id":{"mode":"password"`) {
		t.Fatalf("PUT: %d %s", resp.status, body)
	}
	if e.polestarID.cred.Email != piEmail || e.polestarID.cred.Password != piPassword || e.polestarID.cred.Refresh == "" {
		t.Errorf("stored %+v", e.polestarID.cred)
	}
	for _, secret := range []string{piEmail, piPassword, e.polestarID.cred.Refresh} {
		if strings.Contains(body, secret) || strings.Contains(logs.String(), secret) {
			t.Errorf("%q returned or logged: %s\n%s", secret, body, logs)
		}
	}

	for _, tt := range []struct{ name, body string }{
		{"no email", passwordBody("owner", piPassword)},
		{"no password", passwordBody(piEmail, "")},
		{"unknown field", `{"email":"owner@example.com","password":"x","refresh":"y"}`},
	} {
		resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, tt.body, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		wantError(t, body, codeInvalidBody)
	}
	for _, req := range []struct{ method, path, body string }{
		{http.MethodPut, "/password", passwordBody(piEmail, piPassword)},
		{http.MethodPost, "/authorization", ""},
		{http.MethodDelete, "", ""},
	} {
		resp, body := do(t, noFollow(), req.method, e.api.URL+apiPrefix+"/connection/polestar-id"+req.path, jsonType, req.body)
		if resp.status != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", req.method, req.path, resp.status)
		}
		wantError(t, body, codeUnauthorized)
	}

	e.polestarID.err = errors.New("boom")
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, passwordBody(piEmail, piPassword), c)
	if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
		t.Errorf("store down: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)
	resp, body = do(t, noFollow(), http.MethodDelete, e.api.URL+apiPrefix+"/connection/polestar-id", "", "", c)
	if resp.status != http.StatusInternalServerError {
		t.Errorf("DELETE, store down: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)
}

// TestPolestarIDFlag: with source.polestar-id off, its default, nothing is asked of the
// Polestar ID nor stored; one already given may still be removed.
func TestPolestarIDFlag(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/polestar-id"
	for _, req := range []struct{ method, path, body string }{
		{http.MethodPost, "/authorization", ""},
		{http.MethodPut, "/password", passwordBody(piEmail, piPassword)},
		{http.MethodPut, "/code", codeBody("state", "https://www.polestar.com/sign-in-callback?code="+piCode)},
	} {
		resp, body := do(t, noFollow(), req.method, u+req.path, jsonType, req.body, c)
		if resp.status != http.StatusForbidden || e.polestarIDs.issued != 0 {
			t.Errorf("%s, flag off: %d %s", req.path, resp.status, body)
		}
		wantError(t, body, codeFeatureUnavailable)
	}
	if resp, body := do(t, noFollow(), http.MethodDelete, u, "", "", c); resp.status != http.StatusNoContent {
		t.Errorf("DELETE, flag off: %d %s", resp.status, body)
	}
}

// TestPolestarIDUnchecked: a Polestar ID without a car, or that did not answer, or that
// brings the account over its cap, is not stored; a car another connection reads stays
// with it.
func TestPolestarIDUnchecked(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/polestar-id/password"
	body := passwordBody(piEmail, piPassword)

	e.polestarIDs.vins = nil
	resp, got := do(t, noFollow(), http.MethodPut, u, jsonType, body, c)
	if resp.status != http.StatusConflict || !e.polestarID.setAt.IsZero() {
		t.Errorf("no car: %d %s", resp.status, got)
	}
	wantError(t, got, codeNoVehicle)

	e.polestarIDs.vins, e.polestarIDs.err = []string{polestarIDVIN}, errors.New("unreachable")
	resp, got = do(t, noFollow(), http.MethodPut, u, jsonType, body, c)
	if resp.status != http.StatusBadGateway || !e.polestarID.setAt.IsZero() {
		t.Errorf("unreachable: %d %s", resp.status, got)
	}
	wantError(t, got, codeSourceUnavailable)
	e.polestarIDs.err = nil

	e.server.MaxVehicles = 1
	e.reader.vehicles[account] = []Vehicle{{ID: car, VIN: "YV1SMLT0000DT0001", Provider: "volvo"}}
	resp, got = do(t, noFollow(), http.MethodPut, u, jsonType, body, c)
	if resp.status != http.StatusConflict || !e.polestarID.setAt.IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("over the cap: %d %s", resp.status, got)
	}
	wantError(t, got, codeTooManyVehicles)

	// Read by the Data Portal: it stays there, and counts once.
	e.reader.vehicles[account] = []Vehicle{{ID: car, VIN: polestarIDVIN, Provider: "polestar"}}
	resp, got = do(t, noFollow(), http.MethodPut, u, jsonType, body, c)
	if resp.status != http.StatusOK || e.polestarID.setAt.IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("read by the Data Portal: %d %s, added %v", resp.status, got, e.store.vehicles)
	}
}

// TestPolestarIDCodeUnchecked: the token mode's refusals before anything is stored: no
// car, the Polestar ID down, a body not JSON, too many sign-ins pending.
func TestPolestarIDCodeUnchecked(t *testing.T) {
	e := newEnv(t, 1)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/polestar-id"

	e.polestarIDs.vins = nil
	state, callback := authorize(t, e, c)
	resp, body := do(t, noFollow(), http.MethodPost, u+"/authorization", "", "", c)
	if resp.status != http.StatusServiceUnavailable {
		t.Errorf("too many pending: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/code", jsonType, codeBody(state, callback), c)
	if resp.status != http.StatusConflict || !e.polestarID.setAt.IsZero() {
		t.Errorf("no car: %d %s", resp.status, body)
	}
	wantError(t, body, codeNoVehicle)

	e.polestarIDs.err = errors.New("unreachable")
	state, callback = authorize(t, e, c)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/code", jsonType, codeBody(state, callback), c)
	if resp.status != http.StatusBadGateway || !e.polestarID.setAt.IsZero() {
		t.Errorf("unreachable: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)

	for path, body := range map[string]string{"/code": codeBody(state, callback), "/password": passwordBody(piEmail, piPassword)} {
		resp, got := do(t, noFollow(), http.MethodPut, u+path, "text/plain", body, c)
		if resp.status != http.StatusUnsupportedMediaType {
			t.Errorf("%s, text/plain: %d", path, resp.status)
		}
		wantError(t, got, codeUnsupportedMediaType)
	}
}
