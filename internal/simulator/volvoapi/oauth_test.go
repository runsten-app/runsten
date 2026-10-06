package volvoapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

// The RFC 7636 appendix B pair.
const (
	verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

func oauthServer(t *testing.T, cfg OAuth) (*httptest.Server, *clock.Manual) {
	t.Helper()
	clk := clock.NewManual(t0)
	srv := httptest.NewServer(NewHandler([]Source{fakeSource{report(allSupported())}}, clk, Limits{}, Faults{}, cfg))
	t.Cleanup(srv.Close)
	return srv, clk
}

func noRedirect(srv *httptest.Server) *http.Client {
	c := *srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// authorizeQuery returns a valid authorization request, modified by edit.
func authorizeQuery(cfg OAuth, edit func(url.Values)) string {
	q := url.Values{
		"response_type": {"code"}, "client_id": {cfg.ClientID}, "redirect_uri": {cfg.RedirectURI},
		"scope": {"openid conve:odometer_status"}, "state": {"st"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	if edit != nil {
		edit(q)
	}
	return q.Encode()
}

// authorizeCode runs the authorization request and returns the redirect parameters.
func authorizeCode(t *testing.T, srv *httptest.Server, query string) (int, url.Values) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/as/authorization.oauth2?"+query, http.NoBody)
	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, loc.Query()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
}

func postToken(t *testing.T, srv *httptest.Server, cfg OAuth, form url.Values) (int, tokenResponse) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/as/token.oauth2", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(cfg.ClientID, cfg.ClientSecret)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, tr
}

func exchangeForm(code string) url.Values {
	return url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {DefaultOAuth().RedirectURI}, "code_verifier": {verifier},
	}
}

func refreshForm(rt string) url.Values {
	return url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}}
}

func apiStatus(t *testing.T, srv *httptest.Server, token string) int {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/connected-vehicle/v2/vehicles/"+vin+"/odometer", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("vcc-api-key", "key")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestAuthorizationCodeFlow(t *testing.T) {
	cfg := DefaultOAuth()
	srv, clk := oauthServer(t, cfg)

	status, q := authorizeCode(t, srv, authorizeQuery(cfg, nil))
	if status != http.StatusFound || q.Get("code") == "" || q.Get("state") != "st" {
		t.Fatalf("authorize: %d %v", status, q)
	}
	code := q.Get("code")
	status, tok := postToken(t, srv, cfg, exchangeForm(code))
	if status != http.StatusOK || tok.AccessToken == "" || tok.RefreshToken == "" || tok.TokenType != "Bearer" || tok.ExpiresIn != 1799 {
		t.Fatalf("exchange: %d %+v", status, tok)
	}
	if status, again := postToken(t, srv, cfg, exchangeForm(code)); status != http.StatusBadRequest || again.Error != "invalid_grant" {
		t.Errorf("code reused: %d %+v", status, again)
	}

	if s := apiStatus(t, srv, tok.AccessToken); s != http.StatusOK {
		t.Fatalf("API with the issued token: %d", s)
	}
	clk.Advance(cfg.AccessTokenTTL)
	if s := apiStatus(t, srv, tok.AccessToken); s != http.StatusUnauthorized {
		t.Errorf("API with an expired token: %d", s)
	}

	status, next := postToken(t, srv, cfg, refreshForm(tok.RefreshToken))
	if status != http.StatusOK || next.RefreshToken == tok.RefreshToken || next.AccessToken == tok.AccessToken {
		t.Fatalf("refresh: %d %+v", status, next)
	}
	if s := apiStatus(t, srv, next.AccessToken); s != http.StatusOK {
		t.Errorf("API with the refreshed token: %d", s)
	}

	// Rotation: the old refresh token is refused, and its reuse revokes the grant.
	if status, reused := postToken(t, srv, cfg, refreshForm(tok.RefreshToken)); status != http.StatusBadRequest || reused.Error != "invalid_grant" {
		t.Errorf("old refresh token: %d %+v", status, reused)
	}
	if s := apiStatus(t, srv, next.AccessToken); s != http.StatusUnauthorized {
		t.Errorf("access token of a revoked grant: %d", s)
	}
	if status, _ := postToken(t, srv, cfg, refreshForm(next.RefreshToken)); status != http.StatusBadRequest {
		t.Errorf("refresh token of a revoked grant: %d", status)
	}
}

func TestReuseWithoutRevocation(t *testing.T) {
	cfg := DefaultOAuth()
	cfg.ReuseRevokesGrant = false
	srv, _ := oauthServer(t, cfg)
	_, q := authorizeCode(t, srv, authorizeQuery(cfg, nil))
	_, tok := postToken(t, srv, cfg, exchangeForm(q.Get("code")))
	_, next := postToken(t, srv, cfg, refreshForm(tok.RefreshToken))
	if status, _ := postToken(t, srv, cfg, refreshForm(tok.RefreshToken)); status != http.StatusBadRequest {
		t.Errorf("old refresh token: %d", status)
	}
	if status, _ := postToken(t, srv, cfg, refreshForm(next.RefreshToken)); status != http.StatusOK {
		t.Errorf("current refresh token after a reuse: %d", status)
	}
}

func TestTokenLifetimes(t *testing.T) {
	cfg := DefaultOAuth()
	cfg.RefreshTokenTTL, cfg.GrantTTL = 7*24*time.Hour, 20*24*time.Hour
	start := func(t *testing.T) (*httptest.Server, *clock.Manual, string) {
		t.Helper()
		srv, clk := oauthServer(t, cfg)
		_, q := authorizeCode(t, srv, authorizeQuery(cfg, nil))
		_, tok := postToken(t, srv, cfg, exchangeForm(q.Get("code")))
		return srv, clk, tok.RefreshToken
	}

	t.Run("refresh token unused for 7 days", func(t *testing.T) {
		srv, clk, rt := start(t)
		clk.Advance(cfg.RefreshTokenTTL)
		if status, tok := postToken(t, srv, cfg, refreshForm(rt)); status != http.StatusBadRequest || tok.Error != "invalid_grant" {
			t.Errorf("%d %+v", status, tok)
		}
	})
	t.Run("grant of 20 days, refreshed every 6 days", func(t *testing.T) {
		srv, clk, rt := start(t)
		for day := 6; day <= 18; day += 6 {
			clk.Advance(6 * 24 * time.Hour)
			status, tok := postToken(t, srv, cfg, refreshForm(rt))
			if status != http.StatusOK {
				t.Fatalf("day %d: %d %+v", day, status, tok)
			}
			rt = tok.RefreshToken
		}
		clk.Advance(2 * 24 * time.Hour) // day 20
		if status, tok := postToken(t, srv, cfg, refreshForm(rt)); status != http.StatusBadRequest || tok.Error != "invalid_grant" {
			t.Errorf("after the grant: %d %+v", status, tok)
		}
	})
	t.Run("authorization code expires", func(t *testing.T) {
		srv, clk := oauthServer(t, cfg)
		_, q := authorizeCode(t, srv, authorizeQuery(cfg, nil))
		clk.Advance(cfg.CodeTTL)
		if status, _ := postToken(t, srv, cfg, exchangeForm(q.Get("code"))); status != http.StatusBadRequest {
			t.Errorf("expired code: %d", status)
		}
	})
}

func TestAuthorizeErrors(t *testing.T) {
	cfg := DefaultOAuth()
	srv, _ := oauthServer(t, cfg)
	for name, tt := range map[string]struct {
		edit     func(url.Values)
		redirect bool   // errors about the client itself are never redirected
		error    string // error code in the redirect
	}{
		"unknown client":        {edit: func(q url.Values) { q.Set("client_id", "evil") }},
		"unregistered redirect": {edit: func(q url.Values) { q.Set("redirect_uri", "https://evil.test/cb") }},
		"no PKCE":               {edit: func(q url.Values) { q.Del("code_challenge") }, redirect: true, error: "invalid_request"},
		"plain PKCE":            {edit: func(q url.Values) { q.Set("code_challenge_method", "plain") }, redirect: true, error: "invalid_request"},
		"implicit flow":         {edit: func(q url.Values) { q.Set("response_type", "token") }, redirect: true, error: "unsupported_response_type"},
	} {
		t.Run(name, func(t *testing.T) {
			status, q := authorizeCode(t, srv, authorizeQuery(cfg, tt.edit))
			switch {
			case !tt.redirect && status != http.StatusBadRequest:
				t.Errorf("status %d, want 400 without redirect", status)
			case tt.redirect && (status != http.StatusFound || q.Get("error") != tt.error || q.Get("state") != "st" || q.Get("code") != ""):
				t.Errorf("status %d, redirect %v", status, q)
			}
		})
	}
}

func TestTokenErrors(t *testing.T) {
	cfg := DefaultOAuth()
	srv, _ := oauthServer(t, cfg)
	code := func() string {
		_, q := authorizeCode(t, srv, authorizeQuery(cfg, nil))
		return q.Get("code")
	}
	wrongSecret := cfg
	wrongSecret.ClientSecret = "guess"
	for name, tt := range map[string]struct {
		cfg    OAuth
		form   func() url.Values
		status int
		error  string
	}{
		"wrong PKCE verifier": {cfg, func() url.Values {
			f := exchangeForm(code())
			f.Set("code_verifier", "not-the-verifier-not-the-verifier-not-the-v")
			return f
		}, http.StatusBadRequest, "invalid_grant"},
		"no PKCE verifier": {cfg, func() url.Values {
			f := exchangeForm(code())
			f.Del("code_verifier")
			return f
		}, http.StatusBadRequest, "invalid_grant"},
		"other redirect URI": {cfg, func() url.Values {
			f := exchangeForm(code())
			f.Set("redirect_uri", "https://evil.test/cb")
			return f
		}, http.StatusBadRequest, "invalid_grant"},
		"wrong client secret":   {wrongSecret, func() url.Values { return exchangeForm(code()) }, http.StatusUnauthorized, "invalid_client"},
		"unknown refresh token": {cfg, func() url.Values { return refreshForm("forged") }, http.StatusBadRequest, "invalid_grant"},
		"unknown grant type": {cfg, func() url.Values {
			return url.Values{"grant_type": {"password"}, "username": {"u"}, "password": {"p"}}
		}, http.StatusBadRequest, "unsupported_grant_type"},
	} {
		t.Run(name, func(t *testing.T) {
			status, tok := postToken(t, srv, tt.cfg, tt.form())
			if status != tt.status || tok.Error != tt.error || tok.AccessToken != "" {
				t.Errorf("%d %+v, want %d %s", status, tok, tt.status, tt.error)
			}
		})
	}

	t.Run("unreadable form", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/as/token.oauth2",
			strings.NewReader(strings.Repeat("a", maxForm+1)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(cfg.ClientID, cfg.ClientSecret)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status %d", resp.StatusCode)
		}
	})
}

// TestPortalTokens: a token the simulator did not issue (a test token from the
// developer portal) keeps the former behavior: accepted, then expired after TokenTTL.
func TestPortalTokens(t *testing.T) {
	clk := clock.NewManual(t0)
	srv := httptest.NewServer(NewHandler([]Source{fakeSource{report(allSupported())}}, clk, Limits{TokenTTL: 30 * time.Minute}, Faults{}, DefaultOAuth()))
	t.Cleanup(srv.Close)
	if s := apiStatus(t, srv, "portal-token"); s != http.StatusOK {
		t.Fatalf("first use: %d", s)
	}
	clk.Advance(30 * time.Minute)
	if s := apiStatus(t, srv, "portal-token"); s != http.StatusUnauthorized {
		t.Errorf("after TokenTTL: %d", s)
	}
}

// memLedger is a Ledger in memory: what it recorded restores another simulator.
type memLedger struct {
	is   Issued
	fail error
}

func (l *memLedger) Issue(_ context.Context, g Grant, used string, access AccessToken, refresh RefreshToken) error {
	if l.fail != nil {
		return l.fail
	}
	if !slices.ContainsFunc(l.is.Grants, func(k Grant) bool { return k.ID == g.ID }) {
		l.is.Grants = append(l.is.Grants, g)
	}
	for i := range l.is.Refresh {
		if l.is.Refresh[i].Token == used {
			l.is.Refresh[i].Used = true
		}
	}
	l.is.Access = append(l.is.Access, access)
	l.is.Refresh = append(l.is.Refresh, refresh)
	return nil
}

func (l *memLedger) Revoke(_ context.Context, id string) error {
	if l.fail != nil {
		return l.fail
	}
	for i := range l.is.Grants {
		if l.is.Grants[i].ID == id {
			l.is.Grants[i].Revoked = true
		}
	}
	return nil
}

// restarted returns a simulator restored from what ledger recorded, recording there.
func restarted(t *testing.T, ledger *memLedger) *httptest.Server {
	t.Helper()
	cfg := DefaultOAuth()
	cfg.Ledger, cfg.Issued = ledger, ledger.is
	srv, _ := oauthServer(t, cfg)
	return srv
}

func TestGrantsOutliveRestart(t *testing.T) {
	ledger := &memLedger{}
	cfg := DefaultOAuth()
	cfg.Ledger = ledger
	srv, _ := oauthServer(t, cfg)
	_, q := authorizeCode(t, srv, authorizeQuery(cfg, nil))
	_, tok := postToken(t, srv, cfg, exchangeForm(q.Get("code")))
	_, next := postToken(t, srv, cfg, refreshForm(tok.RefreshToken))

	srv = restarted(t, ledger)
	if s := apiStatus(t, srv, next.AccessToken); s != http.StatusOK {
		t.Errorf("API with the access token issued before: %d", s)
	}
	status, again := postToken(t, srv, cfg, refreshForm(next.RefreshToken))
	if status != http.StatusOK {
		t.Fatalf("refresh with the token issued before: %d %+v", status, again)
	}

	// The reuse of a token used before the restart is still detected, and its
	// revocation outlives the next restart.
	if status, _ := postToken(t, srv, cfg, refreshForm(tok.RefreshToken)); status != http.StatusBadRequest {
		t.Errorf("token used before the restart: %d", status)
	}
	srv = restarted(t, ledger)
	if status, revoked := postToken(t, srv, cfg, refreshForm(again.RefreshToken)); status != http.StatusBadRequest || revoked.Error != "invalid_grant" {
		t.Errorf("token of a grant revoked before the restart: %d %+v", status, revoked)
	}
}

func TestLedgerFailure(t *testing.T) {
	ledger := &memLedger{}
	cfg := DefaultOAuth()
	cfg.Ledger = ledger
	srv, _ := oauthServer(t, cfg)
	_, q := authorizeCode(t, srv, authorizeQuery(cfg, nil))
	_, tok := postToken(t, srv, cfg, exchangeForm(q.Get("code")))

	// A refresh the ledger cannot record fails temporarily and uses nothing up.
	ledger.fail = errors.New("database down")
	if status, resp := postToken(t, srv, cfg, refreshForm(tok.RefreshToken)); status != http.StatusInternalServerError || resp.Error != "server_error" {
		t.Errorf("refresh while the ledger fails: %d %+v", status, resp)
	}
	ledger.fail = nil
	if status, _ := postToken(t, srv, cfg, refreshForm(tok.RefreshToken)); status != http.StatusOK {
		t.Errorf("refresh once the ledger is back: %d", status)
	}
}
