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

const (
	renaultVIN      = "VF1AAAAA555777999"
	renaultEmail    = "owner@example.com"
	renaultPassword = "the owner's password"
	renaultToken    = "the-login-token"
)

// memRenault keeps the account's My Renault sign-in in memory.
type memRenault struct {
	mu   sync.Mutex
	cred RenaultCredential
	set  time.Time
	err  error
}

func (m *memRenault) RenaultConnection(context.Context, string) (CredentialConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set.IsZero() {
		return CredentialConnection{}, m.err
	}
	return CredentialConnection{ID: "renault", Last4: m.cred.PersonID[max(0, len(m.cred.PersonID)-4):], SetAt: m.set}, m.err
}

func (m *memRenault) SetRenaultCredential(_ context.Context, _ string, c RenaultCredential, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.cred, m.set = c, at
	return "renault", nil
}

func (m *memRenault) DeleteRenaultCredential(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cred, m.set = RenaultCredential{}, time.Time{}
	return m.err
}

// renaultRefused is Renault's refusal, as the client's errors tell it.
type renaultRefused struct{ wrongCode bool }

func (e renaultRefused) Error() string           { return "refused" }
func (e renaultRefused) CredentialRefused() bool { return !e.wrongCode }
func (e renaultRefused) WrongCode() bool         { return e.wrongCode }

// fakeRenault signs in with renaultEmail and renaultPassword, asking for the code
// "123456" when tfa is set; the account holds vins, and others not electric.
type fakeRenault struct {
	mu       sync.Mutex
	tfa      bool
	vins     []string
	others   int
	err      error
	verified int
}

// set sets the account's vehicles and the error of the next sign-ins.
func (f *fakeRenault) set(vins []string, others int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vins, f.others, f.err = vins, others, err
}

func (f *fakeRenault) signedIn() RenaultSignIn {
	return RenaultSignIn{
		Credential: RenaultCredential{LoginToken: renaultToken, GMID: "gmid", UCID: "ucid", PersonID: "person-0042", Country: "BE"},
		VINs:       f.vins, Others: f.others,
	}
}

func (f *fakeRenault) RenaultSignIn(_ context.Context, email, password, _ string) (RenaultSignIn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.err != nil:
		return RenaultSignIn{}, f.err
	case email != renaultEmail || password != renaultPassword:
		return RenaultSignIn{}, renaultRefused{}
	case f.tfa:
		return RenaultSignIn{Pending: &RenaultPending{MaskedMail: "o***@example.com", State: "challenge"}}, nil
	}
	return f.signedIn(), nil
}

func (f *fakeRenault) RenaultVerify(_ context.Context, state any, email, password, code string) (RenaultSignIn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case state != "challenge" || email != renaultEmail || password != renaultPassword:
		return RenaultSignIn{}, errors.New("not the sign-in waiting")
	case code != "123456":
		return RenaultSignIn{}, renaultRefused{wrongCode: true}
	}
	f.verified++
	return f.signedIn(), nil
}

func renaultBody(email, password string) string {
	return `{"email":"` + email + `","password":"` + password + `","language":"fr"}`
}

// TestRenaultSignIn: the owner signs in; the login token is stored with the person and
// the country, the electric vehicles added, the others counted; the password is never
// stored, returned nor logged, nor the token.
func TestRenaultSignIn(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	e.renaults.vins, e.renaults.others = []string{renaultVIN}, 2

	resp, body := do(t, noFollow(), http.MethodPost, u+"/renault/sign-in", jsonType, renaultBody(renaultEmail, renaultPassword), c)
	if resp.status != http.StatusOK || !strings.Contains(body, `"code_sent_to":null`) || !strings.Contains(body, `"others":2`) {
		t.Fatalf("sign-in: %d %s", resp.status, body)
	}
	if !strings.Contains(body, `"renault":{"last4":"0042"`) {
		t.Errorf("the connection: %s", body)
	}
	for _, secret := range []string{renaultPassword, renaultToken} {
		if strings.Contains(body, secret) || strings.Contains(logs.String(), secret) {
			t.Errorf("%q returned or logged: %s\n%s", secret, body, logs)
		}
	}
	if e.renault.cred.LoginToken != renaultToken || e.renault.cred.Country != "BE" || !e.renault.set.Equal(e.clk.Now()) {
		t.Errorf("stored %+v at %v", e.renault.cred, e.renault.set)
	}
	if len(e.store.vehicles) != 1 || e.store.vehicles[0] != renaultVIN {
		t.Errorf("vehicles added: %v", e.store.vehicles)
	}
	if resp, body = do(t, noFollow(), http.MethodDelete, u+"/renault", "", "", c); resp.status != http.StatusNoContent {
		t.Errorf("DELETE: %d %s", resp.status, body)
	}
	if _, body = get(t, noFollow(), u, c); !strings.Contains(body, `"renault":null`) {
		t.Errorf("after DELETE: %s", body)
	}
}

// TestRenaultCode: an account that asks for its code gets it by email, the sign-in waits
// for it; a wrong code keeps it waiting, the right one connects; a code without a sign-in
// waiting, or after ten minutes, is refused.
func TestRenaultCode(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/renault"
	e.renaults.vins, e.renaults.tfa = []string{renaultVIN}, true

	resp, body := do(t, noFollow(), http.MethodPost, u+"/code", jsonType, `{"code":"123456"}`, c)
	if resp.status != http.StatusConflict {
		t.Errorf("a code without a sign-in: %d %s", resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)

	resp, body = do(t, noFollow(), http.MethodPost, u+"/sign-in", jsonType, renaultBody(renaultEmail, renaultPassword), c)
	if resp.status != http.StatusOK || !strings.Contains(body, `"code_sent_to":"o***@example.com"`) || !strings.Contains(body, `"connection":null`) {
		t.Fatalf("sign-in: %d %s", resp.status, body)
	}
	if !e.renault.set.IsZero() || len(e.store.vehicles) != 0 {
		t.Error("stored before the code")
	}
	resp, body = do(t, noFollow(), http.MethodPost, u+"/code", jsonType, `{"code":"654321"}`, c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("a wrong code: %d %s", resp.status, body)
	}
	wantError(t, body, codeWrongCode)
	resp, body = do(t, noFollow(), http.MethodPost, u+"/code", jsonType, `{"code":"123456"}`, c)
	if resp.status != http.StatusOK || !strings.Contains(body, `"renault":{"last4":"0042"`) || !strings.Contains(body, `"code_sent_to":null`) {
		t.Fatalf("the code: %d %s", resp.status, body)
	}
	if e.renault.cred.LoginToken != renaultToken || len(e.store.vehicles) != 1 {
		t.Errorf("stored %+v, vehicles %v", e.renault.cred, e.store.vehicles)
	}
	// Used: no sign-in waits any more.
	if resp, _ = do(t, noFollow(), http.MethodPost, u+"/code", jsonType, `{"code":"123456"}`, c); resp.status != http.StatusConflict {
		t.Errorf("a code used twice: %d", resp.status)
	}
	// Ten minutes and the sign-in waiting is forgotten.
	do(t, noFollow(), http.MethodPost, u+"/sign-in", jsonType, renaultBody(renaultEmail, renaultPassword), c)
	e.clk.Advance(renaultCodeLife)
	if resp, body = do(t, noFollow(), http.MethodPost, u+"/code", jsonType, `{"code":"123456"}`, c); resp.status != http.StatusConflict {
		t.Errorf("a code too late: %d %s", resp.status, body)
	}
}

// TestRenaultRefused: the flag off, a password refused, no electric vehicle, Renault
// down, the account over its cap: nothing is stored.
func TestRenaultRefused(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/renault/sign-in"
	ok := renaultBody(renaultEmail, renaultPassword)

	// The flag's default: off.
	resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, ok, c)
	if resp.status != http.StatusForbidden {
		t.Errorf("the flag off: %d %s", resp.status, body)
	}
	wantError(t, body, codeFeatureUnavailable)
	e.server.Flags = &accountFlags{}

	tests := []struct {
		name   string
		body   string
		setup  func()
		status int
		code   errorCode
	}{
		{name: "a password refused", body: renaultBody(renaultEmail, "wrong"), status: http.StatusBadRequest, code: codeCredentialRefused},
		{name: "not an email", body: renaultBody("owner", renaultPassword), status: http.StatusBadRequest, code: codeInvalidBody},
		{name: "no electric vehicle", body: ok, setup: func() { e.renaults.set(nil, 1, nil) }, status: http.StatusConflict, code: codeNoVehicle},
		{name: "Renault down", body: ok, setup: func() { e.renaults.set(nil, 1, errors.New("no answer")) }, status: http.StatusBadGateway, code: codeSourceUnavailable},
		{
			name: "the cap", body: ok, setup: func() {
				e.renaults.set([]string{renaultVIN, "VF1AAAAA555777998"}, 0, nil)
				e.server.MaxVehicles = 1
			},
			status: http.StatusConflict, code: codeTooManyVehicles,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, tt.body, c)
			if resp.status != tt.status || !e.renault.set.IsZero() || len(e.store.vehicles) != 0 {
				t.Errorf("%d %s", resp.status, body)
			}
			wantError(t, body, tt.code)
		})
	}
}

// TestRenaultEdges: a body that is not JSON, Renault down while the code is entered, too
// many sign-ins waiting on the instance.
func TestRenaultEdges(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/renault"
	e.renaults.vins, e.renaults.tfa = []string{renaultVIN}, true
	for _, path := range []string{"/sign-in", "/code"} {
		resp, body := do(t, noFollow(), http.MethodPost, u+path, "text/plain", renaultBody(renaultEmail, renaultPassword), c)
		if resp.status != http.StatusUnsupportedMediaType {
			t.Errorf("%s text/plain: %d", path, resp.status)
		}
		wantError(t, body, codeUnsupportedMediaType)
	}
	// Renault down once the code is sent: the sign-in is forgotten, the owner starts again.
	do(t, noFollow(), http.MethodPost, u+"/sign-in", jsonType, renaultBody(renaultEmail, renaultPassword), c)
	e.renaults.mu.Lock()
	e.renaults.tfa = false
	e.renaults.mu.Unlock()
	e.server.RenaultAccounts = renaultDown{}
	resp, body := do(t, noFollow(), http.MethodPost, u+"/code", jsonType, `{"code":"123456"}`, c)
	if resp.status != http.StatusBadGateway {
		t.Errorf("Renault down: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)
	// One sign-in waiting on the instance at most: another account's is refused.
	e.server.RenaultAccounts = e.renaults
	e.renaults.mu.Lock()
	e.renaults.tfa = true
	e.renaults.mu.Unlock()
	e.server.renaultPending.max = 1
	do(t, noFollow(), http.MethodPost, u+"/sign-in", jsonType, renaultBody(renaultEmail, renaultPassword), c)
	other := e.session(t, "other")
	resp, body = do(t, noFollow(), http.MethodPost, u+"/sign-in", jsonType, renaultBody(renaultEmail, renaultPassword), other)
	if resp.status != http.StatusTooManyRequests {
		t.Errorf("too many waiting: %d %s", resp.status, body)
	}
	wantError(t, body, codeRateLimited)
}

// renaultDown answers every call with an error.
type renaultDown struct{}

func (renaultDown) RenaultSignIn(context.Context, string, string, string) (RenaultSignIn, error) {
	return RenaultSignIn{}, errors.New("no answer")
}

func (renaultDown) RenaultVerify(context.Context, any, string, string, string) (RenaultSignIn, error) {
	return RenaultSignIn{}, errors.New("no answer")
}

// TestRenaultSuperseded: a sign-in made without a code while another waits for its code
// supersedes it: that code, entered after, connects nothing.
func TestRenaultSuperseded(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/renault"
	e.renaults.vins, e.renaults.tfa = []string{renaultVIN}, true
	do(t, noFollow(), http.MethodPost, u+"/sign-in", jsonType, renaultBody(renaultEmail, renaultPassword), c)
	e.renaults.mu.Lock()
	e.renaults.tfa = false
	e.renaults.mu.Unlock()
	if resp, body := do(t, noFollow(), http.MethodPost, u+"/sign-in", jsonType, renaultBody(renaultEmail, renaultPassword), c); resp.status != http.StatusOK {
		t.Fatalf("sign-in without a code: %d %s", resp.status, body)
	}
	resp, body := do(t, noFollow(), http.MethodPost, u+"/code", jsonType, `{"code":"123456"}`, c)
	if resp.status != http.StatusConflict {
		t.Errorf("the superseded code: %d %s", resp.status, body)
	}
	wantError(t, body, codeAuthorizationExpired)
}

// TestRenaultPendingIdentity: a sign-in is taken only by the request that read it, and
// leaves on its own at its expiry.
func TestRenaultPendingIdentity(t *testing.T) {
	var p renaultPending
	now := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	a, _ := p.put("acc", renaultWaiting{email: "a", expires: now.Add(renaultCodeLife)}, now)
	b, _ := p.put("acc", renaultWaiting{email: "b", expires: now.Add(renaultCodeLife)}, now)
	if p.take("acc", a.id) {
		t.Error("the replaced sign-in taken")
	}
	if w, ok := p.get("acc", now); !ok || w.id != b.id {
		t.Errorf("the replacement lost: %+v %v", w, ok)
	}
	if !p.take("acc", b.id) || p.take("acc", b.id) {
		t.Error("the current sign-in not taken once")
	}
	// Its timer forgets it, with its password, without another request.
	p.put("acc", renaultWaiting{password: "secret", expires: now.Add(10 * time.Millisecond)}, now)
	for i := 0; ; i++ {
		p.mu.Lock()
		n := len(p.waiting)
		p.mu.Unlock()
		if n == 0 {
			break
		}
		if i == 400 {
			t.Fatal("the expired sign-in kept")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
