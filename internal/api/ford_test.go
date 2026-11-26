package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fordVIN      = "WF0TESTPUMA000001"
	fordClientID = "ford-client-id-abcd"
	fordSecret   = "ford-s3cr3t-value"
	fordRefresh  = "ford-refresh-token"
)

// memFord keeps the account's Ford connection in memory.
type memFord struct {
	mu      sync.Mutex
	cred    FordCredential
	set     time.Time
	expires time.Time
}

func (m *memFord) FordConnection(context.Context, string) (CredentialConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set.IsZero() {
		return CredentialConnection{}, nil
	}
	return CredentialConnection{ID: "ford", Last4: m.cred.ClientID[max(0, len(m.cred.ClientID)-4):], SetAt: m.set, ExpiresAt: m.expires}, nil
}

func (m *memFord) SetFord(_ context.Context, _ string, c FordCredential, expiresAt, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cred, m.set, m.expires = c, at, expiresAt
	return "ford", nil
}

func (m *memFord) DeleteFord(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cred, m.set, m.expires = FordCredential{}, time.Time{}, time.Time{}
	return nil
}

// fakeFord is Ford's sign-in: it accepts the code "good" of the application given, and
// answers err instead when set.
type fakeFord struct {
	mu      sync.Mutex
	err     error
	expires time.Time
	asked   []FordCredential
	// noRefresh: the grant comes without a refresh token.
	noRefresh bool
	// during runs while Ford answers a code.
	during func()
}

func (f *fakeFord) AuthorizeURL(c FordCredential, state string) (string, error) {
	q := url.Values{"client_id": {c.ClientID}, "redirect_uri": {c.RedirectURI}, "state": {state}}
	return "https://ford.example/auth/init?" + q.Encode(), nil
}

func (f *fakeFord) Connect(_ context.Context, c FordCredential, code string) (FordGrant, error) {
	f.mu.Lock()
	during := f.during
	f.mu.Unlock()
	if during != nil {
		during()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, c)
	switch {
	case f.err != nil:
		return FordGrant{}, f.err
	case code != "good":
		return FordGrant{}, &FordError{Outcome: FordCodeRefused, Err: errors.New("token: HTTP 400 invalid_grant")}
	}
	if f.noRefresh {
		return FordGrant{VIN: fordVIN, ExpiresAt: f.expires}, nil
	}
	return FordGrant{Refresh: fordRefresh, VIN: fordVIN, ExpiresAt: f.expires}, nil
}

// fordEnv is an environment whose account is offered Ford.
func fordEnv(t *testing.T) (*env, *http.Cookie, string) {
	t.Helper()
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	return e, e.session(t, "admin"), e.api.URL + apiPrefix + "/connection"
}

// fordStart starts a sign-in and returns what the API answers.
func fordStart(t *testing.T, u string, c *http.Cookie) fordAuthorizationJSON {
	t.Helper()
	resp, body := do(t, noFollow(), http.MethodPost, u+"/ford/authorization", jsonType,
		`{"client_id":"`+fordClientID+`","client_secret":"`+fordSecret+`"}`, c)
	if resp.status != http.StatusOK {
		t.Fatalf("POST authorization: %d %s", resp.status, body)
	}
	var a fordAuthorizationJSON
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, fordSecret) || strings.Contains(a.URL, fordSecret) {
		t.Errorf("the secret returned: %s", body)
	}
	return a
}

func fordCode(state, callback string) string {
	b, _ := json.Marshal(map[string]string{"state": state, "callback": callback})
	return string(b)
}

// TestFordPaste: on an instance Ford cannot send back to, the owner declares a localhost
// address, signs in, and pastes back the address of the page Ford sent them to. The
// connection is stored with its car, shown again by the client ID's last four characters
// only, and removed; the secret and the refresh token are never returned nor logged.
func TestFordPaste(t *testing.T) {
	e, c, u := fordEnv(t)
	logs := logged(e)
	e.fords.expires = e.clk.Now().Add(365 * 24 * time.Hour)

	_, body := get(t, noFollow(), u, c)
	var settings struct {
		Ford    *fordJSON           `json:"ford"`
		FordApp fordApplicationJSON `json:"ford_application"`
	}
	if err := json.Unmarshal([]byte(body), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Ford != nil || settings.FordApp.Callback || settings.FordApp.RedirectURI != "http://localhost/auth/ford/callback" {
		t.Fatalf("settings before: %+v", settings)
	}

	a := fordStart(t, u, c)
	if a.Callback || a.RedirectURI != settings.FordApp.RedirectURI || len(a.State) != fordStateLen {
		t.Fatalf("authorization %+v", a)
	}
	if q := mustQuery(t, a.URL); q.Get("client_id") != fordClientID || q.Get("redirect_uri") != a.RedirectURI || q.Get("state") != a.State {
		t.Errorf("sign-in address %s", a.URL)
	}

	// An address of another sign-in, then one without a code: neither spends this one.
	pasted := "http://localhost/auth/ford/callback?code=good&state="
	resp, body := do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(a.State, pasted+"another"), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("another sign-in's address: %d %s", resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(a.State, "http://localhost/auth/ford/callback?state="+a.State), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("an address without a code: %d %s", resp.status, body)
	}
	wantError(t, body, codeInvalidBody)

	resp, body = do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(a.State, " "+pasted+a.State+" "), c)
	if resp.status != http.StatusOK {
		t.Fatalf("PUT code: %d %s", resp.status, body)
	}
	golden(t, "connection-ford.json", body)
	for _, secret := range []string{fordSecret, fordRefresh, fordClientID} {
		if strings.Contains(body, secret) || strings.Contains(logs.String(), secret) {
			t.Errorf("%s returned or logged: %s\n%s", secret, body, logs)
		}
	}
	want := FordCredential{ClientID: fordClientID, ClientSecret: fordSecret, RedirectURI: a.RedirectURI, Refresh: fordRefresh}
	if e.ford.cred != want || !e.ford.set.Equal(e.clk.Now()) || !e.ford.expires.Equal(e.fords.expires) {
		t.Errorf("stored %+v at %v, ending %v", e.ford.cred, e.ford.set, e.ford.expires)
	}
	if len(e.store.vehicles) != 1 || e.store.vehicles[0] != fordVIN {
		t.Errorf("vehicles added: %v", e.store.vehicles)
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}

	// The sign-in is spent.
	resp, body = do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(a.State, "good"), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("a sign-in used twice: %d %s", resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)

	if resp, body = do(t, noFollow(), http.MethodDelete, u+"/ford", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	if !e.ford.set.IsZero() {
		t.Error("the connection was not removed")
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

// TestFordCodeErrors: what Ford's refusals of a sign-in mean; a sign-in older than its
// time is over; a new one replaces the account's previous.
func TestFordCodeErrors(t *testing.T) {
	e, c, u := fordEnv(t)
	for name, tc := range map[string]struct {
		err    error
		code   string
		status int
		want   errorCode
	}{
		"a code Ford refuses":     {nil, "bad", http.StatusBadRequest, codeAuthorizationExpired},
		"an application refused":  {&FordError{Outcome: FordApplicationRefused, Err: errors.New("invalid_client")}, "good", http.StatusBadRequest, codeCredentialRefused},
		"a grant without a car":   {&FordError{Outcome: FordNoVehicle, Err: errors.New("no vin")}, "good", http.StatusConflict, codeNoVehicle},
		"Ford out of reach":       {&FordError{Outcome: FordUnavailable, Err: errors.New("503")}, "good", http.StatusBadGateway, codeSourceUnavailable},
		"an error of the network": {errors.New("dial tcp: timeout"), "good", http.StatusBadGateway, codeSourceUnavailable},
	} {
		e.fords.err = tc.err
		a := fordStart(t, u, c)
		resp, body := do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(a.State, tc.code), c)
		if resp.status != tc.status {
			t.Errorf("%s: %d %s", name, resp.status, body)
		}
		wantError(t, body, tc.want)
	}
	e.fords.err = nil
	e.fords.noRefresh = true
	a := fordStart(t, u, c)
	resp, body := do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(a.State, "good"), c)
	if resp.status != http.StatusBadGateway {
		t.Errorf("a grant without a refresh token: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)
	e.fords.noRefresh = false
	if !e.ford.set.IsZero() || len(e.store.vehicles) != 0 {
		t.Errorf("a refused sign-in stored something: %+v %v", e.ford.cred, e.store.vehicles)
	}

	old := fordStart(t, u, c)
	e.clk.Advance(fordFlowTTL)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(old.State, "good"), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("a sign-in %v old: %d %s", fordFlowTTL, resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)

	first := fordStart(t, u, c)
	second := fordStart(t, u, c)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(first.State, "good"), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("a sign-in replaced: %d %s", resp.status, body)
	}
	if resp, body = do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(second.State, "good"), c); resp.status != http.StatusOK {
		t.Errorf("the latest sign-in: %d %s", resp.status, body)
	}
}

// TestFordCallback: on an instance whose address Ford accepts, Ford sends the owner back
// to it, which ends the sign-in and goes back to the Connection page with its outcome.
func TestFordCallback(t *testing.T) {
	e, c, u := fordEnv(t)
	e.server.AppURL = "https://runsten.example/"
	callback := func(query string) string {
		t.Helper()
		resp, body := get(t, noFollow(), e.api.URL+"/auth/ford/callback?"+query, c)
		if resp.status != http.StatusSeeOther {
			t.Fatalf("callback %s: %d %s", query, resp.status, body)
		}
		return resp.header.Get("Location")
	}
	const back = "https://runsten.example/connection?ford="

	a := fordStart(t, u, c)
	if !a.Callback || a.RedirectURI != "https://runsten.example/auth/ford/callback" {
		t.Fatalf("authorization %+v", a)
	}
	if got := callback("code=good&state=" + a.State); got != back+"connected" {
		t.Errorf("came back to %s", got)
	}
	if e.ford.cred.RedirectURI != a.RedirectURI || e.ford.cred.Refresh != fordRefresh || len(e.store.vehicles) != 1 {
		t.Errorf("stored %+v, vehicles %v", e.ford.cred, e.store.vehicles)
	}

	if got := callback("code=good&state=" + a.State); got != back+"expired" {
		t.Errorf("a sign-in used twice: %s", got)
	}
	if got := callback("code=good&state=unknown"); got != back+"expired" {
		t.Errorf("an unknown state: %s", got)
	}
	a = fordStart(t, u, c)
	if got := callback("state=" + a.State); got != back+"expired" {
		t.Errorf("no code: %s", got)
	}
	a = fordStart(t, u, c)
	if got := callback("error=access_denied&state=" + a.State); got != back+"denied" {
		t.Errorf("a consent refused: %s", got)
	}
	if got := callback("code=good&state=" + a.State); got != back+"expired" {
		t.Errorf("a refused sign-in is spent: %s", got)
	}
	for outcome, err := range map[string]error{
		"application_refused": &FordError{Outcome: FordApplicationRefused, Err: errors.New("invalid_client")},
		"no_vehicle":          &FordError{Outcome: FordNoVehicle, Err: errors.New("no vin")},
		"unavailable":         &FordError{Outcome: FordUnavailable, Err: errors.New("503")},
	} {
		e.fords.err = err
		a = fordStart(t, u, c)
		if got := callback("code=good&state=" + a.State); got != back+outcome {
			t.Errorf("%s: came back to %s", outcome, got)
		}
	}
	e.fords.err = nil

	// Without a session, the sign-in page: the code is not exchanged.
	a = fordStart(t, u, c)
	if resp, _ := get(t, noFollow(), e.api.URL+"/auth/ford/callback?code=good&state="+a.State); resp.status != http.StatusSeeOther ||
		strings.Contains(resp.header.Get("Location"), "ford=") {
		t.Errorf("without a session: %d %s", resp.status, resp.header.Get("Location"))
	}
}

// TestFordRedirect: the redirect address is the instance's own when Ford accepts it
// (https, or http on localhost), else a localhost one to paste back.
func TestFordRedirect(t *testing.T) {
	for appURL, want := range map[string]struct {
		uri      string
		callback bool
	}{
		"https://runsten.example/":     {"https://runsten.example/auth/ford/callback", true},
		"https://example.org/runsten/": {"https://example.org/runsten/auth/ford/callback", true},
		"http://localhost:5173/":       {"http://localhost:5173/auth/ford/callback", true},
		"http://127.0.0.1:8080/":       {"http://127.0.0.1:8080/auth/ford/callback", true},
		"http://192.168.1.20:8080/":    {fordPasteRedirect, false},
		"http://runsten.home.arpa/":    {fordPasteRedirect, false},
		"":                             {fordPasteRedirect, false},
	} {
		s := &Server{}
		s.AppURL = appURL
		if uri, callback := s.fordRedirect(); uri != want.uri || callback != want.callback {
			t.Errorf("%q: %s, %v", appURL, uri, callback)
		}
	}
}

// TestFordCodeOf: the address pasted, or its code alone.
func TestFordCodeOf(t *testing.T) {
	for pasted, want := range map[string][2]string{
		"http://localhost/auth/ford/callback?code=abc&state=xyz": {"abc", "xyz"},
		"  https://runsten.example/cb?state=s&code=c%2B1  ":      {"c+1", "s"},
		"abc-DEF_123":                         {"abc-DEF_123", ""},
		"http://localhost/auth/ford/callback": {"", ""},
		"code=abc&state=xyz":                  {"", ""},
		"two words":                           {"", ""},
	} {
		if code, state := fordCodeOf(pasted); code != want[0] || state != want[1] {
			t.Errorf("%q: %q, %q", pasted, code, state)
		}
	}
}

// TestFordEndedMeanwhile: a connection removed, or another sign-in started, while Ford
// answers a sign-in's code: that sign-in stores nothing, and the removal holds.
func TestFordEndedMeanwhile(t *testing.T) {
	e, c, u := fordEnv(t)
	for name, during := range map[string]func(){
		"the connection removed": func() {
			if resp, body := do(t, noFollow(), http.MethodDelete, u+"/ford", "", "", c); resp.status != http.StatusNoContent {
				t.Errorf("delete: %d %s", resp.status, body)
			}
		},
		"another sign-in started": func() {
			resp, body := do(t, noFollow(), http.MethodPost, u+"/ford/authorization", jsonType,
				`{"client_id":"`+fordClientID+`","client_secret":"`+fordSecret+`"}`, c)
			if resp.status != http.StatusOK {
				t.Errorf("start: %d %s", resp.status, body)
			}
		},
	} {
		a := fordStart(t, u, c)
		e.fords.mu.Lock()
		e.fords.during = during
		e.fords.mu.Unlock()
		resp, body := do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(a.State, "good"), c)
		e.fords.mu.Lock()
		e.fords.during = nil
		e.fords.mu.Unlock()
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, resp.status, body)
		}
		wantError(t, body, codeAuthorizationExpired)
		if !e.ford.set.IsZero() || len(e.store.vehicles) != 0 {
			t.Errorf("%s: something was stored: %+v %v", name, e.ford.cred, e.store.vehicles)
		}
	}
	// Nothing ended meanwhile: the sign-in is stored.
	a := fordStart(t, u, c)
	if resp, body := do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode(a.State, "good"), c); resp.status != http.StatusOK {
		t.Errorf("a sign-in left alone: %d %s", resp.status, body)
	}
	if e.ford.set.IsZero() {
		t.Error("the sign-in left alone was not stored")
	}
}

// TestFordFlag: with source.ford off for the account, no sign-in starts nor ends.
func TestFordFlag(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	// Off by default.
	resp, body := do(t, noFollow(), http.MethodPost, u+"/ford/authorization", jsonType,
		`{"client_id":"`+fordClientID+`","client_secret":"`+fordSecret+`"}`, c)
	if resp.status != http.StatusForbidden {
		t.Errorf("the flag off: %d %s", resp.status, body)
	}
	wantError(t, body, codeFeatureUnavailable)
	resp, body = do(t, noFollow(), http.MethodPut, u+"/ford/code", jsonType, fordCode("state", "good"), c)
	if resp.status != http.StatusForbidden {
		t.Errorf("the flag off: %d %s", resp.status, body)
	}
	e.server.AppURL = "https://runsten.example/"
	if resp, _ := get(t, noFollow(), e.api.URL+"/auth/ford/callback?code=good&state=s", c); resp.header.Get("Location") != "https://runsten.example/connection?ford=not_offered" {
		t.Errorf("the callback, the flag off: %s", resp.header.Get("Location"))
	}
	if len(e.fords.asked) != 0 {
		t.Errorf("Ford asked %d times", len(e.fords.asked))
	}
}

// TestFordRequests: a body that is not the one asked for, another content type, and too
// many sign-ins pending on the instance.
func TestFordRequests(t *testing.T) {
	e, c, u := fordEnv(t)
	resp, body := do(t, noFollow(), http.MethodPost, u+"/ford/authorization", jsonType, `{"client_id":"short","client_secret":"`+fordSecret+`"}`, c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("a client ID too short: %d %s", resp.status, body)
	}
	wantError(t, body, codeInvalidBody)
	for _, r := range [][2]string{{http.MethodPost, "/ford/authorization"}, {http.MethodPut, "/ford/code"}} {
		if resp, body := do(t, noFollow(), r[0], u+r[1], "text/plain", `{}`, c); resp.status != http.StatusUnsupportedMediaType {
			t.Errorf("%s %s as text: %d %s", r[0], r[1], resp.status, body)
		}
	}
	// The instance's sign-ins pending, of other accounts: no room for one more.
	e.server.fordFlows.pending = map[string]fordFlow{}
	for i := range fordMaxFlows {
		e.server.fordFlows.pending[rand16(i)] = fordFlow{account: "other", expires: e.clk.Now().Add(time.Minute)}
	}
	resp, body = do(t, noFollow(), http.MethodPost, u+"/ford/authorization", jsonType,
		`{"client_id":"`+fordClientID+`","client_secret":"`+fordSecret+`"}`, c)
	if resp.status != http.StatusServiceUnavailable {
		t.Errorf("too many sign-ins pending: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)
	// Once they are over, a sign-in starts again.
	e.clk.Advance(time.Minute)
	fordStart(t, u, c)
}

// rand16 is a state of a test, distinct for each i.
func rand16(i int) string {
	const digits = "0123456789abcdef"
	b := []byte("state-0000000000")
	for p := len(b) - 1; i > 0; p, i = p-1, i/16 {
		b[p] = digits[i%16]
	}
	return string(b)
}
