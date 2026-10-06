package volvoapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"runsten/internal/platform/clock"
)

// OAuth configures the simulated Volvo ID: the registered application and the
// lifetimes of what it issues. The simulated user consents immediately: the
// authorization endpoint redirects without showing a page.
type OAuth struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string // registered redirect URI, compared exactly
	// AccessTokenTTL is announced in expires_in (undocumented: 1799 s, then 299 s observed).
	AccessTokenTTL time.Duration
	// RefreshTokenTTL: a refresh token must be used within this delay (7 days, documented).
	RefreshTokenTTL time.Duration
	// GrantTTL: the grant ends this long after the authorization (6 months at most,
	// documented; taken as 180 days).
	GrantTTL time.Duration
	// CodeTTL is the validity of an authorization code (assumption, not documented).
	CodeTTL time.Duration
	// ReuseRevokesGrant: presenting a refresh token already used revokes the whole
	// grant, as the OAuth security best practices recommend. Assumption: Volvo's actual
	// behavior is unknown; this is the strict case.
	ReuseRevokesGrant bool
	// Ledger, when set, records what is issued, so that a restarted simulator still
	// knows the grants, as the real Volvo ID does; Issued is what it recorded before,
	// restored at start. Without it, everything is lost with the process.
	Ledger Ledger
	Issued Issued
}

// Ledger records what the simulated Volvo ID issues. The issuer calls it under its
// lock, before answering: a call that fails leaves the issuer unchanged and answers
// 500. Authorization codes, valid one minute, are not recorded.
type Ledger interface {
	// Issue records an access and a refresh token for the grant g, the grant itself
	// at its first issue, and the refresh token used, unless empty, as used.
	Issue(ctx context.Context, g Grant, used string, access AccessToken, refresh RefreshToken) error
	// Revoke records the revocation of the grant id.
	Revoke(ctx context.Context, id string) error
}

// Issued is what a Ledger recorded. Its times are those of the simulated clock, which
// starts again at the scenario's start when the simulator restarts: tokens recorded
// before then simply live longer.
type Issued struct {
	Grants  []Grant
	Access  []AccessToken
	Refresh []RefreshToken
}

// Grant is the user's consent, from which the tokens are issued.
type Grant struct {
	ID           string
	AuthorizedAt time.Time
	Scope        string
	Revoked      bool
}

// AccessToken is an access token of the grant Grant.
type AccessToken struct {
	Token, Grant string
	Expires      time.Time
}

// RefreshToken is a refresh token of the grant Grant.
type RefreshToken struct {
	Token, Grant string
	Issued       time.Time
	Used         bool
}

// DefaultOAuth returns the development application (the same defaults as
// runsten-api and runsten-collector against the simulator) and the documented
// lifetimes.
func DefaultOAuth() OAuth {
	const day = 24 * time.Hour
	return OAuth{ //nolint:gosec // development application of the simulator, not a secret
		ClientID:          "runsten-dev",
		ClientSecret:      "runsten-dev-secret",
		RedirectURI:       "http://127.0.0.1:8081/auth/volvo/callback",
		AccessTokenTTL:    1799 * time.Second,
		RefreshTokenTTL:   7 * day,
		GrantTTL:          180 * day,
		CodeTTL:           time.Minute,
		ReuseRevokesGrant: true,
	}
}

type grant struct {
	id           string
	authorizedAt time.Time
	scope        string
	revoked      bool
}

type issuedAccess struct {
	grant   *grant
	expires time.Time
}

type issuedRefresh struct {
	grant  *grant
	issued time.Time
	used   bool
}

type authCode struct {
	challenge, redirectURI, scope string
	expires                       time.Time
}

// issuer is the simulated authorization server. Issued tokens are kept for the whole
// simulation: used refresh tokens must be recognized to detect their reuse.
type issuer struct {
	cfg OAuth
	clk clock.Clock

	mu      sync.Mutex
	codes   map[string]authCode
	access  map[string]issuedAccess
	refresh map[string]*issuedRefresh
}

func newIssuer(clk clock.Clock, cfg OAuth) *issuer {
	s := &issuer{
		cfg: cfg, clk: clk,
		codes: map[string]authCode{}, access: map[string]issuedAccess{}, refresh: map[string]*issuedRefresh{},
	}
	grants := make(map[string]*grant, len(cfg.Issued.Grants))
	for _, g := range cfg.Issued.Grants {
		grants[g.ID] = &grant{id: g.ID, authorizedAt: g.AuthorizedAt, scope: g.Scope, revoked: g.Revoked}
	}
	for _, a := range cfg.Issued.Access {
		if g, ok := grants[a.Grant]; ok {
			s.access[a.Token] = issuedAccess{grant: g, expires: a.Expires}
		}
	}
	for _, r := range cfg.Issued.Refresh {
		if g, ok := grants[r.Grant]; ok {
			s.refresh[r.Token] = &issuedRefresh{grant: g, issued: r.Issued, used: r.Used}
		}
	}
	return s
}

func (s *issuer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /as/authorization.oauth2", s.authorize)
	mux.HandleFunc("POST /as/token.oauth2", s.token)
	mux.HandleFunc("/", notFound)
	return mux
}

// check reports whether token was issued here and, if so, whether it is still valid.
func (s *issuer) check(token string, now time.Time) (known, valid bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.access[token]
	if !ok {
		return false, false
	}
	return true, !a.grant.revoked && now.Before(a.expires)
}

func (s *issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectURI, err := url.Parse(s.cfg.RedirectURI)
	if err != nil || q.Get("client_id") != s.cfg.ClientID || q.Get("redirect_uri") != s.cfg.RedirectURI {
		// RFC 6749 §4.1.2.1: never redirect to an unverified address.
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unknown client or unregistered redirect_uri")
		return
	}
	redirect := func(v url.Values) {
		if state := q.Get("state"); state != "" {
			v.Set("state", state)
		}
		u := *redirectURI
		query := u.Query()
		for k, vs := range v {
			query[k] = vs
		}
		u.RawQuery = query.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
	fail := func(code, description string) {
		redirect(url.Values{"error": {code}, "error_description": {description}})
	}
	switch {
	case q.Get("response_type") != "code":
		fail("unsupported_response_type", "only the authorization code flow is supported")
		return
	case q.Get("code_challenge") == "":
		fail("invalid_request", "PKCE is required: code_challenge missing")
		return
	case q.Get("code_challenge_method") != "S256":
		fail("invalid_request", "code_challenge_method must be S256")
		return
	}
	code := rand.Text()
	s.mu.Lock()
	s.codes[code] = authCode{
		challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri"), scope: q.Get("scope"),
		expires: s.clk.Now().Add(s.cfg.CodeTTL),
	}
	s.mu.Unlock()
	redirect(url.Values{"code": {code}})
}

// maxForm caps the size of a token request.
const maxForm = 16 << 10

func (s *issuer) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	id, _ = url.QueryUnescape(id)
	secret, _ = url.QueryUnescape(secret)
	if !ok || !equal(id, s.cfg.ClientID) || !equal(secret, s.cfg.ClientSecret) {
		w.Header().Set("WWW-Authenticate", `Basic realm="volvoid"`)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxForm)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unreadable form")
		return
	}
	f := r.PostForm
	switch f.Get("grant_type") {
	case "authorization_code":
		s.exchange(r.Context(), w, f.Get("code"), f.Get("redirect_uri"), f.Get("code_verifier"))
	case "refresh_token":
		s.renew(r.Context(), w, f.Get("refresh_token"))
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (s *issuer) exchange(ctx context.Context, w http.ResponseWriter, code, redirectURI, verifier string) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[code]
	delete(s.codes, code) // single use
	switch {
	case !ok || !now.Before(c.expires):
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown, used or expired authorization code")
	case redirectURI != c.redirectURI:
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri differs from the authorization request")
	case verifier == "" || !equal(oauth2.S256ChallengeFromVerifier(verifier), c.challenge):
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
	default:
		s.issue(ctx, w, &grant{id: rand.Text(), authorizedAt: now, scope: c.scope}, "", now)
	}
}

// renew implements the refresh with rotation. Assumption: every refusal is
// invalid_grant (RFC 6749 §5.2); Volvo's actual error codes are not documented.
func (s *issuer) renew(ctx context.Context, w http.ResponseWriter, token string) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, ok := s.refresh[token]
	switch {
	case !ok:
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown refresh token")
	case rt.used:
		if s.cfg.ReuseRevokesGrant && !rt.grant.revoked {
			if s.cfg.Ledger != nil {
				if err := s.cfg.Ledger.Revoke(ctx, rt.grant.id); err != nil {
					writeLedgerError(w)
					return
				}
			}
			rt.grant.revoked = true
		}
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token already used")
	case rt.grant.revoked:
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "grant revoked")
	case !now.Before(rt.issued.Add(s.cfg.RefreshTokenTTL)):
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token expired")
	case !now.Before(rt.grant.authorizedAt.Add(s.cfg.GrantTTL)):
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "grant expired: authorize again")
	default:
		s.issue(ctx, w, rt.grant, token, now)
	}
}

// issue creates an access token and a refresh token for g, and marks the refresh
// token used, unless empty, as used. The caller holds s.mu.
func (s *issuer) issue(ctx context.Context, w http.ResponseWriter, g *grant, used string, now time.Time) {
	access, refresh := rand.Text(), rand.Text()
	expires := now.Add(s.cfg.AccessTokenTTL)
	if s.cfg.Ledger != nil {
		err := s.cfg.Ledger.Issue(ctx, Grant{ID: g.id, AuthorizedAt: g.authorizedAt, Scope: g.scope, Revoked: g.revoked}, used,
			AccessToken{Token: access, Grant: g.id, Expires: expires}, RefreshToken{Token: refresh, Grant: g.id, Issued: now})
		if err != nil {
			writeLedgerError(w)
			return
		}
	}
	if used != "" {
		s.refresh[used].used = true
	}
	s.access[access] = issuedAccess{grant: g, expires: expires}
	s.refresh[refresh] = &issuedRefresh{grant: g, issued: now}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
		"expires_in":    int(s.cfg.AccessTokenTTL.Seconds()),
		"scope":         g.scope,
	})
}

// writeLedgerError answers a request whose outcome the Ledger could not record. A 5xx
// is temporary for the client: it does not take the grant as lost.
func writeLedgerError(w http.ResponseWriter) {
	writeOAuthError(w, http.StatusInternalServerError, "server_error", "grant storage unavailable")
}

// writeOAuthError writes an RFC 6749 §5.2 error.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
