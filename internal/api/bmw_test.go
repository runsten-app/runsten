package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const bmwVIN = "WBY31AW02RFR00001"

// memBMW is an in-memory BMWCredentials, of one account's connection.
type memBMW struct {
	mu      sync.Mutex
	cred    BMWCredential
	setAt   time.Time
	refused bool
	err     error
}

func (m *memBMW) BMWConnection(context.Context, string) (CredentialConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setAt.IsZero() {
		return CredentialConnection{}, m.err
	}
	c := CredentialConnection{ID: "bmwconn", Last4: m.cred.ClientID[len(m.cred.ClientID)-4:], SetAt: m.setAt}
	if m.refused {
		c.RefusedAt = m.setAt.Add(time.Hour)
	}
	return c, m.err
}

func (m *memBMW) SetBMW(_ context.Context, _ string, c BMWCredential, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.cred, m.setAt, m.refused = c, at, false
	return "bmwconn", nil
}

func (m *memBMW) DeleteBMW(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cred, m.setAt, m.refused = BMWCredential{}, time.Time{}, false
	return m.err
}

const (
	bmwClientID = "00000000-0000-4000-8000-00000000c1d0"
	bmwRefresh  = "the-refresh-token-bmw"
)

// fakeBMW is BMW's device flow for one client ID: each poll answers the next of polls
// (a nil: the tokens), then the account's vins.
type fakeBMW struct {
	mu        sync.Mutex
	polls     []error
	polled    int
	codes     int
	vins      []string
	codeErr   error
	vinsErr   error
	verifiers []string
	page      string // the complete verification page, if any
}

func (f *fakeBMW) DeviceCode(_ context.Context, clientID string) (BMWDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.codeErr != nil {
		return BMWDevice{}, f.codeErr
	}
	if clientID != bmwClientID {
		return BMWDevice{}, &BMWError{Outcome: BMWClientRefused, Err: errors.New("invalid_client")}
	}
	f.codes++
	return BMWDevice{
		DeviceCode: "device-code", Verifier: "verifier", UserCode: "ABCD-EFGH",
		VerificationURI: "https://customer.bmwgroup.com/oneid/link", VerificationURIComplete: f.page,
		ExpiresIn: 5 * time.Minute, Interval: 5 * time.Second,
	}, nil
}

func (f *fakeBMW) PollToken(_ context.Context, clientID, deviceCode, verifier string) (BMWTokens, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifiers = append(f.verifiers, verifier)
	if clientID != bmwClientID || deviceCode != "device-code" {
		return BMWTokens{}, &BMWError{Outcome: BMWExpired, Err: errors.New("expired_token")}
	}
	var err error
	if f.polled < len(f.polls) {
		err = f.polls[f.polled]
	}
	f.polled++
	if err != nil {
		return BMWTokens{}, err
	}
	return BMWTokens{Access: "access", Refresh: bmwRefresh, GCID: "gcid"}, nil
}

func (f *fakeBMW) PrimaryVINs(context.Context, string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vins, f.vinsErr
}

func clientBody(id string) string { return `{"client_id":"` + id + `"}` }

var bmwPending = &BMWError{Outcome: BMWPending, Err: errors.New("authorization_pending")}

// TestBMW: the owner gives the client ID and gets a code; the token is asked no sooner
// than BMW's interval, pending until approved, slowed down when BMW says so; approved,
// the vehicles are added and the credential stored, never returned nor logged; then
// removed.
func TestBMW(t *testing.T) {
	e := newEnv(t, 10)
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	e.bmws.polls = []error{bmwPending, &BMWError{Outcome: BMWSlowDown, Err: errors.New("slow_down")}}

	resp, body := do(t, noFollow(), http.MethodPost, u+"/bmw/token", "", "", c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("no flow: %d %s", resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)

	resp, body = do(t, noFollow(), http.MethodPost, u+"/bmw/authorization", jsonType, clientBody(bmwClientID), c)
	if resp.status != http.StatusOK {
		t.Fatalf("authorization: %d %s", resp.status, body)
	}
	golden(t, "bmw-authorization.json", body)

	// Before the interval: pending, without asking BMW.
	resp, body = do(t, noFollow(), http.MethodPost, u+"/bmw/token", "", "", c)
	if resp.status != http.StatusConflict || resp.header.Get("Retry-After") != "5" || e.bmws.polled != 0 {
		t.Errorf("too soon: %d %v %s", resp.status, resp.header, body)
	}
	wantError(t, body, codeAuthorizationPending)
	// Pending at BMW, then slowed down: 5 s more.
	e.clk.Advance(5 * time.Second)
	resp, body = do(t, noFollow(), http.MethodPost, u+"/bmw/token", "", "", c)
	if resp.status != http.StatusConflict || resp.header.Get("Retry-After") != "5" || e.bmws.polled != 1 {
		t.Errorf("pending: %d %v %s", resp.status, resp.header, body)
	}
	e.clk.Advance(5 * time.Second)
	resp, _ = do(t, noFollow(), http.MethodPost, u+"/bmw/token", "", "", c)
	if resp.status != http.StatusConflict || resp.header.Get("Retry-After") != "10" || e.bmws.polled != 2 {
		t.Errorf("slow down: %d %v", resp.status, resp.header)
	}
	e.clk.Advance(9 * time.Second)
	if resp, _ := do(t, noFollow(), http.MethodPost, u+"/bmw/token", "", "", c); resp.status != http.StatusConflict || e.bmws.polled != 2 {
		t.Errorf("within the new interval: %d, %d polls", resp.status, e.bmws.polled)
	}

	// Approved.
	e.bmws.vins = []string{bmwVIN}
	e.clk.Advance(time.Second)
	resp, body = do(t, noFollow(), http.MethodPost, u+"/bmw/token", "", "", c)
	if resp.status != http.StatusOK {
		t.Fatalf("approved: %d %s", resp.status, body)
	}
	golden(t, "connection-bmw.json", body)
	want := BMWCredential{ClientID: bmwClientID, GCID: "gcid", Refresh: bmwRefresh}
	if e.bmw.cred != want || !e.bmw.setAt.Equal(e.clk.Now()) || e.bmws.verifiers[len(e.bmws.verifiers)-1] != "verifier" {
		t.Errorf("stored %+v at %v", e.bmw.cred, e.bmw.setAt)
	}
	if len(e.store.vehicles) != 1 || e.store.vehicles[0] != bmwVIN {
		t.Errorf("vehicles added: %v", e.store.vehicles)
	}
	for _, secret := range []string{bmwRefresh, bmwClientID, "device-code", "verifier"} {
		if strings.Contains(body, secret) || strings.Contains(logs.String(), secret) {
			t.Errorf("%q returned or logged: %s\n%s", secret, body, logs)
		}
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}
	// Spent.
	e.clk.Advance(time.Minute)
	if resp, body := do(t, noFollow(), http.MethodPost, u+"/bmw/token", "", "", c); resp.status != http.StatusBadRequest {
		t.Errorf("spent: %d %s", resp.status, body)
	}

	if resp, body := do(t, noFollow(), http.MethodDelete, u+"/bmw", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	if _, body := get(t, noFollow(), u, c); !strings.HasSuffix(body, `"bmw":null}`+"\n") || !e.bmw.setAt.IsZero() {
		t.Errorf("after DELETE: %s", body)
	}
}

// TestBMWRefusals: what ends a flow without storing anything, and what leaves it to try
// again.
func TestBMWRefusals(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/bmw"
	start := func(t *testing.T) {
		t.Helper()
		if resp, body := do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, clientBody(bmwClientID), c); resp.status != http.StatusOK {
			t.Fatalf("authorization: %d %s", resp.status, body)
		}
		e.clk.Advance(5 * time.Second)
	}
	token := func(t *testing.T, status int, code errorCode) {
		t.Helper()
		resp, body := do(t, noFollow(), http.MethodPost, u+"/token", "", "", c)
		if resp.status != status || !e.bmw.setAt.IsZero() {
			t.Errorf("%d %s, want %d", resp.status, body, status)
		}
		wantError(t, body, code)
	}

	resp, body := do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, clientBody("unknown-client-id"), c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("unknown client ID: %d %s", resp.status, body)
	}
	wantError(t, body, codeCredentialRefused)
	for _, bad := range []string{`{"client_id":"short"}`, `{"client_id":"with spaces in it"}`, `{"client_id":"x","more":1}`} {
		resp, body := do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, bad, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, resp.status, body)
		}
		wantError(t, body, codeInvalidBody)
	}
	e.bmws.codeErr = errors.New("dial tcp: refused")
	resp, body = do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, clientBody(bmwClientID), c)
	if resp.status != http.StatusBadGateway {
		t.Errorf("BMW down: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)
	e.bmws.codeErr = nil
	for _, page := range []string{"http://customer.bmwgroup.com/oneid/link", "javascript:alert(1)"} {
		e.bmws.page = page
		resp, body := do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, clientBody(bmwClientID), c)
		if resp.status != http.StatusBadGateway {
			t.Errorf("verification page %q: %d %s", page, resp.status, body)
		}
		wantError(t, body, codeSourceUnavailable)
	}
	e.bmws.page = ""
	resp, body = do(t, noFollow(), http.MethodPost, u+"/authorization", "text/plain", clientBody(bmwClientID), c)
	if resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("not JSON: %d %s", resp.status, body)
	}
	wantError(t, body, codeUnsupportedMediaType)
	// Too many flows pending on the instance: another account's waits; this one's may
	// start again.
	e.server.bmwFlows.max = 1
	if resp, _ := do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, clientBody(bmwClientID), c); resp.status != http.StatusOK {
		t.Errorf("first: %d", resp.status)
	}
	resp, body = do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, clientBody(bmwClientID), e.session(t, "other"))
	if resp.status != http.StatusServiceUnavailable {
		t.Errorf("too many: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)
	if resp, _ := do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, clientBody(bmwClientID), c); resp.status != http.StatusOK {
		t.Errorf("the same account again: %d", resp.status)
	}
	e.server.bmwFlows.max = 0

	t.Run("denied", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled = []error{&BMWError{Outcome: BMWDenied, Err: errors.New("access_denied")}}, 0
		start(t)
		token(t, http.StatusBadRequest, codeCredentialRefused)
		token(t, http.StatusBadRequest, codeAuthorizationExpired) // ended
	})
	t.Run("expired at BMW", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled = []error{&BMWError{Outcome: BMWExpired, Err: errors.New("expired_token")}}, 0
		start(t)
		token(t, http.StatusBadRequest, codeAuthorizationExpired)
	})
	t.Run("expired here", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled = nil, 0
		start(t)
		e.clk.Advance(5 * time.Minute)
		token(t, http.StatusBadRequest, codeAuthorizationExpired)
		if e.bmws.polled != 0 {
			t.Errorf("asked BMW %d times", e.bmws.polled)
		}
	})
	t.Run("BMW down: tried again", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled = []error{errors.New("dial tcp: refused")}, 0
		e.bmws.vins = []string{bmwVIN}
		start(t)
		token(t, http.StatusBadGateway, codeSourceUnavailable)
		e.clk.Advance(5 * time.Second)
		if resp, body := do(t, noFollow(), http.MethodPost, u+"/token", "", "", c); resp.status != http.StatusOK {
			t.Errorf("again: %d %s", resp.status, body)
		}
		_ = e.bmw.DeleteBMW(t.Context(), "")
	})
	t.Run("scope missing", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled = nil, 0
		e.bmws.vinsErr = &BMWError{Outcome: BMWScopeMissing, Err: errors.New("CU-103")}
		start(t)
		token(t, http.StatusConflict, codeScopeMissing)
		e.bmws.vinsErr = nil
	})
	t.Run("vehicles not listed", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled = nil, 0
		e.bmws.vinsErr = errors.New("CU-500")
		start(t)
		token(t, http.StatusBadGateway, codeSourceUnavailable)
		e.bmws.vinsErr = nil
	})
	t.Run("no vehicle", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled, e.bmws.vins = nil, 0, nil
		start(t)
		token(t, http.StatusConflict, codeNoVehicle)
	})
	t.Run("too many vehicles", func(t *testing.T) {
		e.server.MaxVehicles = 1
		e.bmws.polls, e.bmws.polled, e.bmws.vins = nil, 0, []string{bmwVIN, "WBY31AW02RFR00002"}
		start(t)
		token(t, http.StatusConflict, codeTooManyVehicles)
		e.server.MaxVehicles = 0
	})
	t.Run("store down", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled, e.bmws.vins = nil, 0, []string{bmwVIN}
		e.bmw.err = errors.New("boom")
		start(t)
		token(t, http.StatusInternalServerError, codeInternal)
		resp, body := do(t, noFollow(), http.MethodDelete, u, "", "", c)
		if resp.status != http.StatusInternalServerError {
			t.Errorf("DELETE, store down: %d %s", resp.status, body)
		}
		wantError(t, body, codeInternal)
		e.bmw.err = nil
	})
	t.Run("another account's flow", func(t *testing.T) {
		e.bmws.polls, e.bmws.polled = nil, 0
		if resp, _ := do(t, noFollow(), http.MethodPost, u+"/authorization", jsonType, clientBody(bmwClientID), e.session(t, "other")); resp.status != http.StatusOK {
			t.Fatal(resp.status)
		}
		e.clk.Advance(5 * time.Second)
		token(t, http.StatusBadRequest, codeAuthorizationExpired)
	})
	for _, req := range []struct{ method, path, body string }{
		{http.MethodPost, "/authorization", clientBody(bmwClientID)},
		{http.MethodPost, "/token", ""},
		{http.MethodDelete, "", ""},
	} {
		resp, body := do(t, noFollow(), req.method, u+req.path, jsonType, req.body)
		if resp.status != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", req.method, req.path, resp.status)
		}
		wantError(t, body, codeUnauthorized)
	}
}

// TestBMWFlag: with source.bmw off for the account, nothing is asked of BMW nor stored;
// a connection already given may still be removed.
func TestBMWFlag(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{off: account}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/bmw"
	for _, req := range []struct{ path, body string }{{"/authorization", clientBody(bmwClientID)}, {"/token", ""}} {
		resp, body := do(t, noFollow(), http.MethodPost, u+req.path, jsonType, req.body, c)
		if resp.status != http.StatusForbidden || e.bmws.codes != 0 {
			t.Errorf("%s, flag off: %d %s", req.path, resp.status, body)
		}
		wantError(t, body, codeFeatureUnavailable)
	}
	if resp, body := do(t, noFollow(), http.MethodDelete, u, "", "", c); resp.status != http.StatusNoContent {
		t.Errorf("DELETE, flag off: %d %s", resp.status, body)
	}
}

// TestBMWConcurrentPolls: two requests at once (two tabs) ask BMW once; the other is
// told to wait, and the flow polls again after the answer.
func TestBMWConcurrentPolls(t *testing.T) {
	var f bmwFlows
	t0 := time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	f.start("a", bmwFlow{expires: t0.Add(5 * time.Minute), interval: 5 * time.Second, next: t0}, t0)
	if _, retry, ok := f.take("a", t0); !ok || retry != 0 {
		t.Fatalf("first: %v, %v", retry, ok)
	}
	if _, retry, ok := f.take("a", t0); !ok || retry != 5*time.Second {
		t.Errorf("while polling: %v, %v", retry, ok)
	}
	f.wait("a", 5*time.Second, t0)
	if _, retry, ok := f.take("a", t0.Add(5*time.Second)); !ok || retry != 0 {
		t.Errorf("after the answer: %v, %v", retry, ok)
	}
}
