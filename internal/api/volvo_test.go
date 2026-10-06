package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/volvoapi"
)

// started is a flow started without following redirects.
type started struct {
	cookie    *http.Cookie
	authorize *url.URL
}

func (e *env) start(t *testing.T, session *http.Cookie) started {
	t.Helper()
	resp, _ := get(t, noFollow(), e.api.URL+"/auth/volvo/start", session)
	if resp.status != http.StatusFound || len(resp.cookies) != 1 {
		t.Fatalf("start: %d, cookies %v", resp.status, resp.cookies)
	}
	u, err := url.Parse(resp.header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return started{cookie: resp.cookies[0], authorize: u}
}

// consent plays the user at Volvo ID and returns the callback URL.
func consent(t *testing.T, authorize *url.URL) string {
	t.Helper()
	resp, _ := get(t, noFollow(), authorize.String())
	if resp.status != http.StatusFound {
		t.Fatalf("authorize: %d", resp.status)
	}
	return resp.header.Get("Location")
}

func TestConnect(t *testing.T) {
	e := newEnv(t, 10)
	b := e.signedIn(t)
	// runsten-api alone: back to its own stand-in for the Connection page.
	resp, body := get(t, b, e.api.URL+"/auth/volvo/start")
	if resp.status != http.StatusOK || !strings.Contains(body, "Volvo ID connected") {
		t.Fatalf("flow: %d %s", resp.status, body)
	}
	if e.store.count() != 1 || len(e.store.vehicles) != 1 || e.store.vehicles[0] != simVIN || e.store.accounts[0] != account {
		t.Fatalf("store = %+v", e.store)
	}
	c := e.store.saved[0]
	now := e.clk.Now()
	if c.AccessToken == "" || c.RefreshToken == "" || !c.AuthorizedAt.Equal(now) || !c.RefreshedAt.Equal(now) ||
		!c.ExpiresAt.Equal(now.Add(volvoapi.DefaultOAuth().AccessTokenTTL)) {
		t.Errorf("credentials = %+v", c)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy"} {
		if resp.header.Get(h) == "" {
			t.Errorf("header %s missing", h)
		}
	}
	u, _ := url.Parse(e.api.URL + "/auth/volvo/")
	if cookies := b.Jar.Cookies(u); len(cookies) != 1 || cookies[0].Name != "runsten_session" {
		t.Errorf("only the session cookie must remain after the callback: %v", cookies)
	}
}

func TestStartCookie(t *testing.T) {
	e := newEnv(t, 10)
	s := e.start(t, e.session(t, "admin"))
	q := s.authorize.Query()
	if !s.cookie.HttpOnly || s.cookie.SameSite != http.SameSiteLaxMode || s.cookie.Secure ||
		s.cookie.Value != q.Get("state") || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		t.Errorf("cookie %+v, authorize %s", s.cookie, s.authorize)
	}
	if !strings.HasPrefix(s.authorize.String(), e.sim.URL+"/as/authorization.oauth2?") {
		t.Errorf("redirected to %s", s.authorize)
	}

	e.server.SecureCookies = true
	e.server.Authorizer = &fakeAuthorizer{}
	token, _, _ := e.sessions.Login(context.Background(), "admin", password, "test")
	resp, _ := get(t, noFollow(), e.api.URL+"/auth/volvo/start", reqCookie("__Host-runsten_session", token))
	if len(resp.cookies) != 1 || !resp.cookies[0].Secure {
		t.Errorf("https redirect URI: cookie %v", resp.cookies)
	}
}

// admission holds back the accounts in held, and fails with err.
type admission struct {
	held map[string]bool
	err  error
}

func (a admission) Admit(_ context.Context, accountID string) (string, error) {
	if a.held[accountID] {
		return "terms", a.err
	}
	return "", a.err
}

// TestAdmission: an account the extension holds back goes to its route of the web
// interface, and no flow starts; once admitted, it connects.
func TestAdmission(t *testing.T) {
	e := newEnv(t, 10)
	session := e.session(t, "admin")
	e.server.Admission = admission{held: map[string]bool{account: true}}
	resp, _ := get(t, noFollow(), e.api.URL+"/auth/volvo/start", session)
	if resp.status != http.StatusSeeOther || resp.header.Get("Location") != "../../terms" || len(resp.cookies) != 0 {
		t.Errorf("held back: %d %s, cookies %v", resp.status, resp.header.Get("Location"), resp.cookies)
	}
	e.server.Admission = admission{err: errors.New("down")}
	if resp, body := get(t, noFollow(), e.api.URL+"/auth/volvo/start", session); resp.status != http.StatusInternalServerError ||
		len(resp.cookies) != 0 || strings.Contains(body, "down") {
		t.Errorf("admission failed: %d %s", resp.status, body)
	}
	e.server.Admission = admission{held: map[string]bool{"another": true}}
	e.start(t, session)
}

func TestCallbackRejections(t *testing.T) {
	t.Run("replayed callback", func(t *testing.T) {
		e := newEnv(t, 10)
		session := e.session(t, "admin")
		s := e.start(t, session)
		cb := consent(t, s.authorize)
		if resp, _ := get(t, noFollow(), cb, s.cookie, session); resp.status != http.StatusSeeOther ||
			resp.header.Get("Location") != "../../connection?volvo=connected" {
			t.Fatalf("first callback: %d %s", resp.status, resp.header.Get("Location"))
		}
		if resp, _ := get(t, noFollow(), cb, s.cookie, session); resp.status != http.StatusBadRequest {
			t.Errorf("replay: %d", resp.status)
		}
		if e.store.count() != 1 {
			t.Errorf("%d connections saved", e.store.count())
		}
	})
	t.Run("another browser: rejected, the owner can still finish", func(t *testing.T) {
		e := newEnv(t, 10)
		session := e.session(t, "admin")
		s := e.start(t, session)
		cb := consent(t, s.authorize)
		if resp, _ := get(t, noFollow(), cb, session); resp.status != http.StatusBadRequest {
			t.Errorf("without the cookie: %d", resp.status)
		}
		if resp, _ := get(t, noFollow(), cb, session, reqCookie(stateCookie, "forged")); resp.status != http.StatusBadRequest {
			t.Errorf("with another state cookie: %d", resp.status)
		}
		if resp, _ := get(t, noFollow(), cb, s.cookie, session); resp.status != http.StatusSeeOther || e.store.count() != 1 {
			t.Errorf("owner: %d, %d saved", resp.status, e.store.count())
		}
	})
	t.Run("without a session: sent to the login, nothing saved", func(t *testing.T) {
		e := newEnv(t, 10)
		s := e.start(t, e.session(t, "admin"))
		cb := consent(t, s.authorize)
		if resp, _ := get(t, noFollow(), cb, s.cookie); resp.status != http.StatusSeeOther || e.store.count() != 0 {
			t.Errorf("status %d, %d saved", resp.status, e.store.count())
		}
	})
	t.Run("finished by another user: rejected", func(t *testing.T) {
		e := newEnv(t, 10)
		s := e.start(t, e.session(t, "admin"))
		cb := consent(t, s.authorize)
		resp, body := get(t, noFollow(), cb, s.cookie, e.session(t, "other"))
		if resp.status != http.StatusBadRequest || !strings.Contains(body, "another user") || e.store.count() != 0 {
			t.Errorf("status %d, %d saved: %s", resp.status, e.store.count(), body)
		}
	})
	t.Run("forged state", func(t *testing.T) {
		e := newEnv(t, 10)
		forged := reqCookie(stateCookie, "forged")
		if resp, _ := get(t, noFollow(), e.api.URL+"/auth/volvo/callback?state=forged&code=x", forged, e.session(t, "admin")); resp.status != http.StatusBadRequest {
			t.Errorf("status %d", resp.status)
		}
	})
	t.Run("expired state", func(t *testing.T) {
		e := newEnv(t, 10)
		session := e.session(t, "admin")
		s := e.start(t, session)
		cb := consent(t, s.authorize)
		e.clk.Advance(flowTTL)
		if resp, _ := get(t, noFollow(), cb, s.cookie, session); resp.status != http.StatusBadRequest || e.store.count() != 0 {
			t.Errorf("status %d, %d saved", resp.status, e.store.count())
		}
	})
	t.Run("wrong PKCE: the code was requested with another challenge", func(t *testing.T) {
		e := newEnv(t, 10)
		session := e.session(t, "admin")
		first, second := e.start(t, session), e.start(t, session)
		u := *first.authorize
		q := u.Query()
		q.Set("code_challenge", second.authorize.Query().Get("code_challenge"))
		u.RawQuery = q.Encode()
		cb := consent(t, &u)
		resp, body := get(t, noFollow(), cb, first.cookie, session)
		if resp.status != http.StatusBadGateway || e.store.count() != 0 {
			t.Errorf("status %d, %d saved: %s", resp.status, e.store.count(), body)
		}
	})
	t.Run("refused by the user", func(t *testing.T) {
		e := newEnv(t, 10)
		session := e.session(t, "admin")
		s := e.start(t, session)
		state := s.authorize.Query().Get("state")
		resp, body := get(t, noFollow(), e.api.URL+"/auth/volvo/callback?error=access_denied&state="+state, s.cookie, session)
		if resp.status != http.StatusBadRequest || !strings.Contains(body, "access_denied") || e.store.count() != 0 ||
			!strings.Contains(body, `href="../../connection"`) {
			t.Errorf("status %d: %s", resp.status, body)
		}
	})
	t.Run("no code", func(t *testing.T) {
		e := newEnv(t, 10)
		session := e.session(t, "admin")
		s := e.start(t, session)
		state := s.authorize.Query().Get("state")
		if resp, _ := get(t, noFollow(), e.api.URL+"/auth/volvo/callback?state="+state, s.cookie, session); resp.status != http.StatusBadRequest {
			t.Errorf("status %d", resp.status)
		}
	})
	t.Run("error page escapes the provider's answer", func(t *testing.T) {
		e := newEnv(t, 10)
		session := e.session(t, "admin")
		s := e.start(t, session)
		state := s.authorize.Query().Get("state")
		_, body := get(t, noFollow(), e.api.URL+"/auth/volvo/callback?error=%3Cscript%3E&state="+state, s.cookie, session)
		if strings.Contains(body, "<script>") {
			t.Errorf("unescaped: %s", body)
		}
	})
}

// TestBackToApp: with the web interface elsewhere (Vite in development), the flow comes
// back to it.
func TestBackToApp(t *testing.T) {
	e := newEnv(t, 10)
	e.server.AppURL = "http://127.0.0.1:5173/"
	session := e.session(t, "admin")
	s := e.start(t, session)
	cb := consent(t, s.authorize)
	if resp, _ := get(t, noFollow(), cb, s.cookie, session); resp.status != http.StatusSeeOther ||
		resp.header.Get("Location") != "http://127.0.0.1:5173/connection?volvo=connected" {
		t.Errorf("status %d, location %s", resp.status, resp.header.Get("Location"))
	}
	if _, body := get(t, noFollow(), cb, s.cookie, session); !strings.Contains(body, `href="http://127.0.0.1:5173/connection"`) {
		t.Errorf("replay: no way back: %s", body)
	}
}

func TestTooManyFlows(t *testing.T) {
	e := newEnv(t, 1)
	session := e.session(t, "admin")
	e.start(t, session)
	if resp, _ := get(t, noFollow(), e.api.URL+"/auth/volvo/start", session); resp.status != http.StatusServiceUnavailable {
		t.Errorf("status %d", resp.status)
	}
}

// fakeAuthorizer accepts any code.
type fakeAuthorizer struct{ err error }

func (f *fakeAuthorizer) AuthorizeURL(state, _ string) string {
	return "https://volvoid.test/as/authorization.oauth2?state=" + state
}

func (f *fakeAuthorizer) Exchange(context.Context, string, string) (oauth.Grant, error) {
	return oauth.Grant{AccessToken: "a", RefreshToken: "r"}, f.err
}

type noVehicles struct{}

func (noVehicles) Vehicles(context.Context, string, string) ([]string, error) {
	return nil, errors.New("403")
}

func TestEnrollmentFailure(t *testing.T) {
	clk := clock.NewManual(t0)
	flows := oauth.NewFlows(clk, time.Minute, 1)
	st := &enrollStore{}
	sessions := newFakeSessions(clk)
	token, _, _ := sessions.Login(context.Background(), "admin", password, "test")
	s := New(Config{
		Sessions: sessions, Authorizer: &fakeAuthorizer{}, Flows: flows, Enrollment: st, Vehicles: noVehicles{},
		Keys: st, Tokens: st, InstanceKey: true,
		Clock: clk, Log: quiet,
	})
	state, _, _ := flows.Start(account)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/volvo/callback?code=c&state="+state, http.NoBody)
	req.AddCookie(reqCookie(stateCookie, state))
	req.AddCookie(reqCookie("runsten_session", token))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || st.count() != 0 {
		t.Errorf("status %d, %d saved", rec.Code, st.count())
	}
}

// twoVehicles lists two vehicles with any key.
type twoVehicles struct{}

func (twoVehicles) Vehicles(context.Context, string, string) ([]string, error) {
	return []string{"VIN1", "VIN2"}, nil
}

// TestTooManyVehicles: a Volvo ID that gives access to more vehicles than the cap is not
// connected, and the Connection page tells why.
func TestTooManyVehicles(t *testing.T) {
	clk := clock.NewManual(t0)
	flows := oauth.NewFlows(clk, time.Minute, 1)
	st := &enrollStore{}
	sessions := newFakeSessions(clk)
	token, _, _ := sessions.Login(context.Background(), "admin", password, "test")
	s := New(Config{
		Sessions: sessions, Authorizer: &fakeAuthorizer{}, Flows: flows, Enrollment: st, Vehicles: twoVehicles{},
		Keys: st, Tokens: st, InstanceKey: true, MaxVehicles: 1,
		Clock: clk, Log: quiet,
	})
	state, _, _ := flows.Start(account)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/volvo/callback?code=c&state="+state, http.NoBody)
	req.AddCookie(reqCookie(stateCookie, state))
	req.AddCookie(reqCookie("runsten_session", token))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.HasSuffix(rec.Header().Get("Location"), "connection?volvo="+outcomeTooManyVehicles) ||
		st.count() != 0 || len(st.vehicles) != 0 {
		t.Errorf("status %d, Location %q, %d saved, vehicles %v", rec.Code, rec.Header().Get("Location"), st.count(), st.vehicles)
	}
}
