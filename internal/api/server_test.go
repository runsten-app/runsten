package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/auth"
	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

const (
	simVIN   = "YV1SMLT0000DT0001"
	account  = "acc"
	password = "correct horse battery staple"
	flowTTL  = 10 * time.Minute
)

var (
	quiet = slog.New(slog.NewTextHandler(io.Discard, nil))
	t0    = time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
)

// fakeSessions is an in-memory Sessions with two users: admin in account "acc", and
// other in account "other".
type fakeSessions struct {
	mu        sync.Mutex
	sessions  map[string]auth.Session // by token
	next      int
	clk       clock.Clock
	throttled bool
	err       error // returned by Login, Authenticate and Logout when set
	clients   []string
}

func newFakeSessions(clk clock.Clock) *fakeSessions {
	return &fakeSessions{sessions: map[string]auth.Session{}, clk: clk}
}

var users = map[string]string{"admin": account, "other": "other"}

func (f *fakeSessions) Login(_ context.Context, username, pw, client string) (string, auth.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients = append(f.clients, client)
	switch {
	case f.err != nil:
		return "", auth.Session{}, f.err
	case f.throttled:
		return "", auth.Session{}, &auth.ThrottledError{RetryIn: 90 * time.Second}
	}
	acc, ok := users[username]
	if !ok || pw != password {
		return "", auth.Session{}, auth.ErrInvalidCredentials
	}
	f.next++
	token := "token-" + strconv.Itoa(f.next)
	now := f.clk.Now()
	s := auth.Session{
		ID: "session-" + strconv.Itoa(f.next), AccountID: acc, UserID: "user-" + username, Username: username,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	f.sessions[token] = s
	return token, s, nil
}

func (f *fakeSessions) Authenticate(_ context.Context, token string) (auth.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return auth.Session{}, f.err
	}
	s, ok := f.sessions[token]
	if !ok {
		return auth.Session{}, auth.ErrNoSession
	}
	return s, nil
}

func (f *fakeSessions) Logout(_ context.Context, s auth.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for tok, cur := range f.sessions {
		if cur.ID == s.ID {
			delete(f.sessions, tok)
		}
	}
	return f.err
}

func (f *fakeSessions) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

// enrollStore is an in-memory oauth.Enrollment, Keys and Tokens, of one connection.
type enrollStore struct {
	mu       sync.Mutex
	accounts []string
	saved    []oauth.Credentials
	vehicles []string
	key      oauth.APIKey
	refused  bool
	keysErr  error
}

func (e *enrollStore) SetKeyRefused(_ context.Context, _, _ string, setAt time.Time, refused bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if setAt.Equal(e.key.SetAt) {
		e.refused = refused
	}
	return nil
}

func (e *enrollStore) Connection(context.Context, string) (Connection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var c Connection
	if len(e.saved) > 0 || e.key.Value != "" {
		c.ID, c.Connected = "conn", len(e.saved) > 0
	}
	if k := e.key; k.Value != "" {
		c.Key = KeyInfo{Last4: k.Value[len(k.Value)-4:], SetAt: k.SetAt}
		if e.refused {
			c.Key.RefusedAt = k.SetAt.Add(time.Minute)
		}
	}
	return c, e.keysErr
}

func (e *enrollStore) AccountKey(context.Context, string) (oauth.APIKey, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.key, e.keysErr
}

func (e *enrollStore) SetAPIKey(_ context.Context, _, key string, at time.Time) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keysErr != nil {
		return "", e.keysErr
	}
	e.key, e.refused = oauth.APIKey{Value: key, SetAt: at, ConnectionID: "conn"}, false
	return "conn", nil
}

func (e *enrollStore) DeleteAPIKey(context.Context, string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.key, e.refused = oauth.APIKey{}, false
	return e.keysErr
}

// Token gives the latest access token saved.
func (e *enrollStore) Token(context.Context, string, string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.saved[len(e.saved)-1].AccessToken, nil
}

func (e *enrollStore) SaveConnection(_ context.Context, acc string, c oauth.Credentials) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accounts = append(e.accounts, acc)
	e.saved = append(e.saved, c)
	return "conn", nil
}

func (e *enrollStore) AddVehicle(_ context.Context, _, _, vin string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vehicles = append(e.vehicles, vin)
	return "veh", nil
}

func (e *enrollStore) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.saved)
}

type env struct {
	api      *httptest.Server // runsten-api
	sim      *httptest.Server // Volvo API and Volvo ID
	clk      *clock.Manual
	sessions *fakeSessions
	tokens   *fakeTokens
	store    *enrollStore
	reader   *memReader
	settings *memSettings
	brokers  *memBrokers
	flows    *oauth.Flows
	server   *Server
}

// newEnv starts runsten-api against the simulator, with the redirect URI registered
// at the simulated Volvo ID, and an instance's application key.
func newEnv(t *testing.T, maxFlows int) *env {
	t.Helper()
	return newEnvKeys(t, maxFlows, "key", nil)
}

// newEnvKeys is newEnv with the instance's key instanceKey (empty: none), and the keys
// the simulator accepts (nil: any).
func newEnvKeys(t *testing.T, maxFlows int, instanceKey string, accepted []string) *env {
	t.Helper()
	e := &env{store: &enrollStore{}, settings: newMemSettings(), brokers: newMemBrokers()}
	e.reader = newMemReader(e.settings)
	var handler http.Handler
	e.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(e.api.Close)

	sc := &scenario.Scenario{
		VIN: simVIN, Start: t0,
		Vehicle: scenario.VehicleConfig{
			Profile: "bev-generic", Model: "EX-SIM", ModelYear: 2026, BatteryKWh: 80,
			ConsumptionKWhPer100km: 18, SoC: 60, TargetSoC: 80, Place: "home",
		},
		Places: map[string]scenario.Place{"home": {Lat: 45.764, Lon: 4.8357}},
		Steps:  []scenario.Step{{Park: &scenario.Park{Duration: time.Hour}}},
	}
	sim, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	e.clk = clock.NewManual(sc.Start)
	cfg := volvoapi.DefaultOAuth()
	cfg.RedirectURI = e.api.URL + "/auth/volvo/callback"
	limits := volvoapi.DefaultLimits()
	limits.Keys = accepted
	e.sim = httptest.NewServer(volvoapi.NewHandler([]volvoapi.Source{sim}, e.clk, limits, volvoapi.Faults{}, cfg))
	t.Cleanup(e.sim.Close)

	authz := volvo.NewAuthClient(volvo.AuthConfig{
		BaseURL: e.sim.URL, ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
		RedirectURI: cfg.RedirectURI, Scopes: volvo.DefaultScopes(),
	}, e.sim.Client())
	e.flows = oauth.NewFlows(e.clk, flowTTL, maxFlows)
	e.sessions = newFakeSessions(e.clk)
	e.tokens = newFakeTokens(e.clk)
	e.server = New(Config{
		Sessions: e.sessions, AccessTokens: e.tokens, Reader: e.reader, States: e.reader, Readings: e.reader, Settings: e.settings, Costs: e.reader, Models: e.reader,
		Authorizer: authz, Flows: e.flows, Enrollment: e.store, Vehicles: volvo.NewClient(e.sim.URL, instanceKey, e.sim.Client()),
		Keys: e.store, Tokens: e.store, InstanceKey: instanceKey != "", ClientID: cfg.ClientID,
		InstanceKeyLast4: instanceKey[max(0, len(instanceKey)-4):], Brokers: e.brokers,
		Params: core.DefaultParams(), CostParams: core.DefaultCostParams(), CapacityParams: core.DefaultCapacityParams(),
		Catalog: mustCatalog(t), Clock: e.clk, Log: quiet,
	})
	handler = checkedAPI(t, e.server)
	return e
}

func mustCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// session logs a user in directly and returns their session cookie.
func (e *env) session(t *testing.T, username string) *http.Cookie {
	t.Helper()
	token, _, err := e.sessions.Login(context.Background(), username, password, "test")
	if err != nil {
		t.Fatal(err)
	}
	return reqCookie("runsten_session", token)
}

// reqCookie is a cookie sent by the client: only its name and value travel.
func reqCookie(name, value string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
}

// browser follows redirects and keeps cookies, like the user's browser.
func browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

// signedIn is a browser in which admin signed in through the login form.
func (e *env) signedIn(t *testing.T) *http.Client {
	t.Helper()
	b := browser(t)
	resp, body := do(t, b, http.MethodPost, e.api.URL+"/login", "application/x-www-form-urlencoded",
		url.Values{"username": {"admin"}, "password": {password}}.Encode())
	if resp.status != http.StatusOK || !strings.Contains(body, "Signed in as <strong>admin</strong>") {
		t.Fatalf("login: %d %s", resp.status, body)
	}
	return b
}

// reply is a response whose body has been read and closed.
type reply struct {
	status  int
	header  http.Header
	cookies []*http.Cookie
}

func do(t *testing.T, c *http.Client, method, u, contentType, body string, cookies ...*http.Cookie) (reply, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), method, u, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, header: resp.Header, cookies: resp.Cookies()}, string(b)
}

func get(t *testing.T, c *http.Client, u string, cookies ...*http.Cookie) (reply, string) {
	t.Helper()
	return do(t, c, http.MethodGet, u, "", "", cookies...)
}

func noFollow() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// wantError decodes a JSON error body and checks its code.
func wantError(t *testing.T, body string, code errorCode) {
	t.Helper()
	var e errorJSON
	if err := json.Unmarshal([]byte(body), &e); err != nil || e.Detail.Code != code || e.Detail.Message == "" {
		t.Errorf("error body %s, want code %s (%v)", body, code, err)
	}
}

func TestLoginForm(t *testing.T) {
	e := newEnv(t, 10)
	form := func(user, pw string) string { return url.Values{"username": {user}, "password": {pw}}.Encode() }
	const formType = "application/x-www-form-urlencoded"

	t.Run("the pages require a session", func(t *testing.T) {
		for path, want := range map[string]string{"/": "login", "/auth/volvo/start": "../../login", "/auth/volvo/callback": "../../login"} {
			resp, _ := get(t, noFollow(), e.api.URL+path)
			if resp.status != http.StatusSeeOther || resp.header.Get("Location") != want {
				t.Errorf("%s: %d → %s", path, resp.status, resp.header.Get("Location"))
			}
		}
		resp, body := get(t, noFollow(), e.api.URL+"/login")
		if resp.status != http.StatusOK || !strings.Contains(body, `<form method="post" action="login">`) ||
			!strings.Contains(resp.header.Get("Content-Security-Policy"), "form-action 'self'") {
			t.Errorf("login page: %d %s", resp.status, body)
		}
	})
	t.Run("sign in, home page, sign out", func(t *testing.T) {
		resp, _ := do(t, noFollow(), http.MethodPost, e.api.URL+"/login", formType, form("admin", password))
		if resp.status != http.StatusSeeOther || resp.header.Get("Location") != "./" || len(resp.cookies) != 1 {
			t.Fatalf("login: %d → %s, cookies %v", resp.status, resp.header.Get("Location"), resp.cookies)
		}
		c := resp.cookies[0]
		if c.Name != "runsten_session" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure ||
			!c.Expires.Equal(e.clk.Now().Add(30*24*time.Hour)) {
			t.Errorf("session cookie %+v", c)
		}
		if resp, _ := get(t, noFollow(), e.api.URL+"/login", c); resp.status != http.StatusSeeOther {
			t.Errorf("login page when signed in: %d", resp.status)
		}
		resp, body := get(t, noFollow(), e.api.URL+"/", c)
		if resp.status != http.StatusOK || !strings.Contains(body, `href="auth/volvo/start"`) || !strings.Contains(body, `action="logout"`) {
			t.Errorf("home: %d %s", resp.status, body)
		}
		resp, _ = do(t, noFollow(), http.MethodPost, e.api.URL+"/logout", formType, "", c)
		if resp.status != http.StatusSeeOther || resp.header.Get("Location") != "login" || e.sessions.count() != 0 ||
			len(resp.cookies) != 1 || resp.cookies[0].MaxAge >= 0 {
			t.Errorf("logout: %d, %d sessions, cookies %v", resp.status, e.sessions.count(), resp.cookies)
		}
		if resp, _ := get(t, noFollow(), e.api.URL+"/", c); resp.status != http.StatusSeeOther || len(resp.cookies) != 1 {
			t.Errorf("home after logout: %d, the stale cookie must be cleared: %v", resp.status, resp.cookies)
		}
		// Signing out without a session only clears the cookie.
		if resp, _ := do(t, noFollow(), http.MethodPost, e.api.URL+"/logout", formType, ""); resp.status != http.StatusSeeOther {
			t.Errorf("logout without session: %d", resp.status)
		}
	})
	t.Run("rejections", func(t *testing.T) {
		for _, tt := range []struct {
			name, body string
			want       int
		}{
			{"wrong password", form("admin", "wrong"), http.StatusUnauthorized},
			{"unknown user", form("nobody", password), http.StatusUnauthorized},
			{"unreadable form", "%zz", http.StatusBadRequest},
		} {
			resp, body := do(t, noFollow(), http.MethodPost, e.api.URL+"/login", formType, tt.body)
			if resp.status != tt.want || len(resp.cookies) != 0 || !strings.Contains(body, `class="error"`) {
				t.Errorf("%s: %d, cookies %v", tt.name, resp.status, resp.cookies)
			}
		}
		e.sessions.throttled = true
		resp, body := do(t, noFollow(), http.MethodPost, e.api.URL+"/login", formType, form("admin", password))
		if resp.status != http.StatusTooManyRequests || resp.header.Get("Retry-After") != "90" || !strings.Contains(body, "2m0s") {
			t.Errorf("throttled: %d, Retry-After %q: %s", resp.status, resp.header.Get("Retry-After"), body)
		}
		e.sessions.throttled = false
	})
	t.Run("the peer address identifies the client", func(t *testing.T) {
		clients := e.sessions.clients
		if last := clients[len(clients)-1]; last != "127.0.0.1" {
			t.Errorf("client %q", last)
		}
	})
	t.Run("store failures", func(t *testing.T) {
		c := e.session(t, "admin")
		e.sessions.err = errors.New("boom")
		defer func() { e.sessions.err = nil }()
		if resp, _ := do(t, noFollow(), http.MethodPost, e.api.URL+"/login", formType, form("admin", password)); resp.status != http.StatusInternalServerError {
			t.Errorf("login: %d", resp.status)
		}
		if resp, _ := get(t, noFollow(), e.api.URL+"/", c); resp.status != http.StatusInternalServerError {
			t.Errorf("session check: %d", resp.status)
		}
	})
}

func TestLogoutFailure(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	failing := &failingLogout{fakeSessions: e.sessions}
	e.server.Sessions = failing
	resp, _ := do(t, noFollow(), http.MethodPost, e.api.URL+"/logout", "application/x-www-form-urlencoded", "", c)
	if resp.status != http.StatusInternalServerError {
		t.Errorf("logout form: %d", resp.status)
	}
	resp, body := do(t, noFollow(), http.MethodDelete, e.api.URL+"/api/v1/session", "", "", c)
	if resp.status != http.StatusInternalServerError {
		t.Errorf("DELETE session: %d", resp.status)
	}
	wantError(t, body, "internal")
}

type failingLogout struct{ *fakeSessions }

func (failingLogout) Logout(context.Context, auth.Session) error { return errors.New("boom") }

func TestSessionAPI(t *testing.T) {
	e := newEnv(t, 10)
	u := e.api.URL + "/api/v1/session"
	const jsonType = "application/json"

	resp, body := get(t, noFollow(), u)
	if resp.status != http.StatusUnauthorized {
		t.Errorf("no session: %d", resp.status)
	}
	wantError(t, body, "unauthorized")

	for _, tt := range []struct {
		name, contentType, body string
		code                    errorCode
		status                  int
	}{
		{"form instead of JSON", "application/x-www-form-urlencoded", "username=admin", "unsupported_media_type", http.StatusUnsupportedMediaType},
		{"no content type", "", `{}`, "unsupported_media_type", http.StatusUnsupportedMediaType},
		{"not JSON", jsonType, "{", "invalid_body", http.StatusBadRequest},
		{"unknown field", jsonType, `{"username":"admin","password":"x","remember":true}`, "invalid_body", http.StatusBadRequest},
		{"trailing data", jsonType, `{"username":"admin","password":"x"} {}`, "invalid_body", http.StatusBadRequest},
		{"too large", jsonType, `{"username":"` + strings.Repeat("a", maxLoginBody) + `"}`, "invalid_body", http.StatusBadRequest},
		{"wrong password", jsonType, `{"username":"admin","password":"wrong"}`, "invalid_credentials", http.StatusUnauthorized},
	} {
		resp, body := do(t, noFollow(), http.MethodPost, u, tt.contentType, tt.body)
		if resp.status != tt.status || len(resp.cookies) != 0 {
			t.Errorf("%s: %d, cookies %v", tt.name, resp.status, resp.cookies)
		}
		wantError(t, body, tt.code)
	}

	e.sessions.throttled = true
	resp, body = do(t, noFollow(), http.MethodPost, u, jsonType, `{"username":"admin","password":"x"}`)
	if resp.status != http.StatusTooManyRequests || resp.header.Get("Retry-After") != "90" {
		t.Errorf("throttled: %d", resp.status)
	}
	wantError(t, body, "too_many_attempts")
	e.sessions.throttled = false

	e.sessions.err = errors.New("boom")
	resp, body = do(t, noFollow(), http.MethodPost, u, jsonType, `{"username":"admin","password":"x"}`)
	if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
		t.Errorf("store failure: %d %s", resp.status, body)
	}
	wantError(t, body, "internal")
	e.sessions.err = nil

	b := browser(t)
	resp, body = do(t, b, http.MethodPost, u, "application/json; charset=utf-8", `{"username":"admin","password":"`+password+`"}`)
	if resp.status != http.StatusOK || resp.header.Get("Content-Type") != "application/json" {
		t.Fatalf("login: %d %s", resp.status, body)
	}
	golden(t, "session.json", body)
	if _, got := get(t, b, u); got != body {
		t.Errorf("GET session = %s, want %s", got, body)
	}
	resp, body = do(t, b, http.MethodDelete, u, "", "")
	if resp.status != http.StatusNoContent || body != "" || e.sessions.count() != 0 {
		t.Errorf("logout: %d %q, %d sessions", resp.status, body, e.sessions.count())
	}
	if resp, _ := get(t, b, u); resp.status != http.StatusUnauthorized {
		t.Errorf("after logout: %d", resp.status)
	}
}

func TestCrossOrigin(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	for _, tt := range []struct {
		method, path, contentType, body string
		api                             bool
	}{
		{http.MethodPost, "/login", "application/x-www-form-urlencoded", "username=admin&password=x", false},
		{http.MethodPost, "/logout", "application/x-www-form-urlencoded", "", false},
		{http.MethodPost, "/api/v1/session", "application/json", `{"username":"admin","password":"x"}`, true},
		{http.MethodDelete, "/api/v1/session", "", "", true},
		{http.MethodPut, "/api/v1/settings", "application/json", `{"currency":"EUR"}`, true},
		{http.MethodPost, "/api/v1/places", "application/json", homeBody, true},
		{http.MethodPut, "/api/v1/places/" + placeID(1), "application/json", homeBody, true},
		{http.MethodDelete, "/api/v1/places/" + placeID(1), "", "", true},
		{http.MethodPut, "/api/v1/vehicles/" + car + "/charges/2026-09-28T18:30:00Z/cost", "application/json", costBody, true},
		{http.MethodDelete, "/api/v1/vehicles/" + car + "/charges/2026-09-28T18:30:00Z/cost", "", "", true},
		{http.MethodPut, "/api/v1/charge-costs/orphans/" + costID(1) + "/charge", "application/json", `{"vehicle":"` + car + `","charge":"2026-09-28T18:30:00Z"}`, true},
		{http.MethodDelete, "/api/v1/charge-costs/orphans/" + costID(1), "", "", true},
		{http.MethodPut, "/api/v1/vehicles/" + car + "/model", "application/json", `{"variant_id":null,"ac_max_kw":null}`, true},
		{http.MethodPut, "/api/v1/connection/api-key", "application/json", `{"key":"0123456789abcdef0123456789abcdef"}`, true},
		{http.MethodDelete, "/api/v1/connection/api-key", "", "", true},
		{http.MethodPut, "/api/v1/mqtt", "application/json", brokerBody, true},
		{http.MethodDelete, "/api/v1/mqtt", "", "", true},
	} {
		for name, set := range map[string]func(h http.Header){
			"Sec-Fetch-Site": func(h http.Header) { h.Set("Sec-Fetch-Site", "cross-site") },
			"Origin":         func(h http.Header) { h.Set("Origin", "https://attacker.example") },
		} {
			req, _ := http.NewRequestWithContext(t.Context(), tt.method, e.api.URL+tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			req.AddCookie(c)
			set(req.Header)
			res, err := noFollow().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if res.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s, cross-origin by %s: %d", tt.method, tt.path, name, res.StatusCode)
			}
			if tt.api {
				wantError(t, string(body), "cross_origin")
			}
		}
	}
	if e.sessions.count() != 1 {
		t.Errorf("a cross-origin request changed the sessions: %d", e.sessions.count())
	}
	if e.settings.writes != 0 || e.reader.writes != 0 || e.store.key.Value != "" || e.brokers.writes != 0 {
		t.Errorf("a cross-origin request changed the settings, costs, key or broker: %d, %d and %d writes",
			e.settings.writes, e.reader.writes, e.brokers.writes)
	}
	// Same origin, as a browser sends it.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodDelete, e.api.URL+"/api/v1/session", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(c)
	res, err := noFollow().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("same-origin logout: %d", res.StatusCode)
	}
}

func TestSecureCookies(t *testing.T) {
	e := newEnv(t, 10)
	e.server.SecureCookies = true
	resp, _ := do(t, noFollow(), http.MethodPost, e.api.URL+"/login", "application/x-www-form-urlencoded",
		url.Values{"username": {"admin"}, "password": {password}}.Encode())
	if len(resp.cookies) != 1 {
		t.Fatalf("cookies %v", resp.cookies)
	}
	if c := resp.cookies[0]; c.Name != "__Host-runsten_session" || !c.Secure || c.Path != "/" || c.Domain != "" {
		t.Errorf("session cookie %+v", c)
	}
}

func TestAPIRoutes(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	for _, tt := range []struct {
		method, path string
		status       int
		code         errorCode
		allow        string
	}{
		{http.MethodGet, "/api/v1/nothing", http.StatusNotFound, "not_found", ""},
		{http.MethodGet, "/api/v2/vehicles", http.StatusNotFound, "not_found", ""},
		{http.MethodPost, "/api/v1/vehicles", http.StatusMethodNotAllowed, "method_not_allowed", "GET"},
		{http.MethodPut, "/api/v1/session", http.StatusMethodNotAllowed, "method_not_allowed", "GET, POST, DELETE"},
	} {
		resp, body := do(t, noFollow(), tt.method, e.api.URL+tt.path, "", "", c)
		if resp.status != tt.status || resp.header.Get("Allow") != tt.allow {
			t.Errorf("%s %s: %d, Allow %q", tt.method, tt.path, resp.status, resp.header.Get("Allow"))
		}
		wantError(t, body, tt.code)
	}
	for _, path := range []string{"/nothing", "/auth/volvo/other"} {
		if resp, _ := get(t, noFollow(), e.api.URL+path, c); resp.status != http.StatusNotFound {
			t.Errorf("%s: %d", path, resp.status)
		}
	}
	resp, _ := get(t, noFollow(), e.api.URL+"/api/v1/vehicles", c)
	for h, want := range map[string]string{
		"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer",
		"Content-Type": "application/json",
	} {
		if got := resp.header.Get(h); got != want {
			t.Errorf("%s: %q, want %q", h, got, want)
		}
	}
}

// TestAPIRequiresSession checks every route of the JSON API but the login without a
// session, then with a session that cannot be checked.
func TestAPIRequiresSession(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	path := strings.NewReplacer("{vehicle}", car, "{id}", "2026-09-28T07:01:00Z", "{place}", placeID(1), "{token}", tokenID(1))
	for _, op := range specOperations(t) {
		if op == "POST /session" {
			continue
		}
		method, p, _ := strings.Cut(op, " ")
		u := e.api.URL + apiPrefix + path.Replace(p)
		resp, body := do(t, noFollow(), method, u, "", "")
		if resp.status != http.StatusUnauthorized {
			t.Errorf("%s without session: %d", op, resp.status)
		}
		wantError(t, body, "unauthorized")

		e.sessions.err = errors.New("boom")
		resp, body = do(t, noFollow(), method, u, "", "", c)
		e.sessions.err = nil
		if resp.status != http.StatusInternalServerError {
			t.Errorf("%s, session check failed: %d", op, resp.status)
		}
		wantError(t, body, "internal")
	}
}

func TestRedirect(t *testing.T) {
	for _, tt := range []struct{ path, target, want string }{
		{"/", "login", "login"},
		{"/", "", "./"},
		{"/login", "", "./"},
		{"/auth/volvo/start", "login", "../../login"},
	} {
		rec := httptest.NewRecorder()
		redirect(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody), tt.target)
		if got := rec.Header().Get("Location"); got != tt.want || rec.Code != http.StatusSeeOther {
			t.Errorf("redirect(%s, %q) = %d %s, want %s", tt.path, tt.target, rec.Code, got, tt.want)
		}
	}
	if got := retryAfter(1500 * time.Millisecond); got != "2" {
		t.Errorf("retryAfter = %s", got)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "pipe"
	if client(r.RemoteAddr) != "pipe" {
		t.Errorf("client of an address without port: %q", client(r.RemoteAddr))
	}
}
