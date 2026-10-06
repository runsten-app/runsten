package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/auth"
)

// A personal access token lets a program (a script, Home Assistant, an assistant's
// connector) read the API on its user's behalf, without a session: it is sent as
// "Authorization: Bearer rst_…". It reads only: an operation that changes state, and
// the routes of the session and of the tokens themselves, require the session cookie.

// AccessTokens issues, authenticates and revokes the personal access tokens
// (auth.Service).
type AccessTokens interface {
	IssueToken(ctx context.Context, accountID, userID, name string, ttl time.Duration) (string, auth.Token, error)
	AuthenticateToken(ctx context.Context, secret string) (auth.Token, error)
	Tokens(ctx context.Context, accountID, userID string) ([]auth.Token, error)
	RevokeToken(ctx context.Context, accountID, userID, tokenID string) error
}

// Rate of the requests of one access token: a burst of tokenBurst, then tokenRate per
// second. A script in a loop must not load the database.
const (
	tokenBurst = 120
	tokenRate  = 2.0
)

// tokenExpiries are the lifetimes a token may be given; zero: none.
var tokenExpiries = map[string]time.Duration{
	"30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour, "365d": 365 * 24 * time.Hour, "never": 0,
}

// principalKey carries the account and user a trusted caller serves a request as
// (ServeAs). It is unexported: no HTTP request can provide it.
type principalKey struct{}

// ServeAs serves r as the user userID of the account accountID, without a cookie nor a
// token: an extension's, such as an assistant's connector built on the JSON API, which
// authenticated the user itself. The request is then treated as one with an access
// token: it reads only.
func (s *Server) ServeAs(w http.ResponseWriter, r *http.Request, accountID, userID string) {
	p := auth.Session{AccountID: accountID, UserID: userID}
	s.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
}

// authFailure is why a request is refused before its operation; zero status: none.
type authFailure struct {
	status     int
	code       errorCode
	message    string
	retryAfter time.Duration
	bearer     string // the WWW-Authenticate challenge of a refused token; empty: none
}

func (f authFailure) write(w http.ResponseWriter) {
	if f.bearer != "" {
		w.Header().Set("WWW-Authenticate", f.bearer)
	}
	if f.retryAfter > 0 {
		w.Header().Set("Retry-After", retryAfter(f.retryAfter))
	}
	writeError(w, f.status, f.code, f.message)
}

// The failures of authentication: no credential in them (gosec reads "token" and a
// string as one).
//
//nolint:gosec // G101: challenges and messages, no credential
var (
	failNoAuth      = authFailure{status: http.StatusUnauthorized, code: codeUnauthorized, message: "authentication required"}
	failBadToken    = authFailure{status: http.StatusUnauthorized, code: codeUnauthorized, message: "invalid or expired access token", bearer: `Bearer error="invalid_token"`}
	failReadOnly    = authFailure{status: http.StatusForbidden, code: codeInsufficientScope, message: "an access token only reads: this operation requires a session", bearer: `Bearer error="insufficient_scope"`}
	failSessionOnly = authFailure{status: http.StatusForbidden, code: codeInsufficientScope, message: "this operation requires a session, not an access token", bearer: `Bearer error="insufficient_scope"`}
)

// authenticate returns who r is from: the principal of ServeAs, an access token, or the
// session cookie, in this order. tokens: an access token (or a principal) may serve r,
// which then reads only. err is set if the authentication could not be checked.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, tokens bool) (auth.Session, authFailure, error) {
	readOnly := r.Method == http.MethodGet || r.Method == http.MethodHead
	viaToken := func(sess auth.Session) (auth.Session, authFailure, error) {
		switch {
		case !tokens:
			return auth.Session{}, failSessionOnly, nil
		case !readOnly:
			return auth.Session{}, failReadOnly, nil
		}
		return sess, authFailure{}, nil
	}
	if p, ok := r.Context().Value(principalKey{}).(auth.Session); ok {
		return viaToken(p)
	}
	if h := r.Header.Get("Authorization"); h != "" {
		// A request with credentials of its own is judged on them alone, never on the
		// cookie the browser may add.
		scheme, secret, _ := strings.Cut(h, " ")
		if !strings.EqualFold(scheme, "Bearer") || s.AccessTokens == nil {
			return auth.Session{}, failBadToken, nil
		}
		t, err := s.AccessTokens.AuthenticateToken(r.Context(), strings.TrimSpace(secret))
		switch {
		case errors.Is(err, auth.ErrNoToken):
			return auth.Session{}, failBadToken, nil
		case err != nil:
			return auth.Session{}, authFailure{}, err //nolint:wrapcheck // logged by the caller
		}
		sess, f, err := viaToken(auth.Session{AccountID: t.AccountID, UserID: t.UserID, TokenID: t.ID})
		if f.status == 0 && err == nil {
			if wait := s.tokenLimiter.take(t.ID, s.Clock.Now()); wait > 0 {
				return auth.Session{}, authFailure{
					status: http.StatusTooManyRequests, code: codeRateLimited,
					message: "too many requests with this access token, retry later", retryAfter: wait,
				}, nil
			}
		}
		return sess, f, err
	}
	sess, ok, err := s.session(w, r)
	switch {
	case err != nil:
		return auth.Session{}, authFailure{}, err
	case !ok:
		return auth.Session{}, failNoAuth, nil
	}
	return sess, authFailure{}, nil
}

// limiter is a token bucket per key, in memory: each instance of runsten-api counts
// its own requests.
type limiter struct {
	mu      sync.Mutex
	burst   float64
	rate    float64 // per second
	buckets map[string]*bucket
}

type bucket struct {
	left float64
	at   time.Time
}

func newLimiter(burst int, rate float64) *limiter {
	return &limiter{burst: float64(burst), rate: rate, buckets: map[string]*bucket{}}
}

// take spends one request of key at now, and returns zero; or, when none is left, how
// long to wait for the next.
func (l *limiter) take(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{left: l.burst, at: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.at).Seconds(); elapsed > 0 {
		b.left = math.Min(l.burst, b.left+elapsed*l.rate)
		b.at = now
	}
	if b.left < 1 {
		return time.Duration((1 - b.left) / l.rate * float64(time.Second))
	}
	b.left--
	return 0
}

type accessTokenJSON struct {
	ID         string     `json:"id" format:"uuid"`
	Name       string     `json:"name" minLength:"1" maxLength:"64"`
	CreatedAt  time.Time  `json:"created_at" pattern:"Z$"`
	LastUsedAt *time.Time `json:"last_used_at" pattern:"Z$" doc:"The latest request made with it, to the hour; null: never used."`
	ExpiresAt  *time.Time `json:"expires_at" pattern:"Z$" doc:"From then on it no longer authenticates, and stays listed until it is revoked; null: never expires."`
}

type accessTokensJSON struct {
	Items []accessTokenJSON `json:"items" nullable:"false" doc:"Newest first, expired ones included."`
}

type newAccessTokenJSON struct {
	Name   string `json:"name" minLength:"1" maxLength:"64" doc:"To tell the tokens apart, such as the program that uses it." example:"Home Assistant"`
	Expiry string `json:"expiry" enum:"30d,90d,365d,never" doc:"How long it authenticates, from now."`
}

type createdAccessTokenJSON struct {
	Token      string     `json:"token" pattern:"^rst_[A-Za-z0-9_-]{43}$" doc:"The secret, to send as Authorization: Bearer. Shown this once: only its hash is kept."`
	ID         string     `json:"id" format:"uuid"`
	Name       string     `json:"name" minLength:"1" maxLength:"64"`
	CreatedAt  time.Time  `json:"created_at" pattern:"Z$"`
	LastUsedAt *time.Time `json:"last_used_at" pattern:"Z$" doc:"Always null: it was never used."`
	ExpiresAt  *time.Time `json:"expires_at" pattern:"Z$" doc:"null: never expires."`
}

func accessTokenOf(t auth.Token) accessTokenJSON {
	j := accessTokenJSON{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt}
	if !t.LastUsedAt.IsZero() {
		j.LastUsedAt = &t.LastUsedAt
	}
	if !t.ExpiresAt.IsZero() {
		j.ExpiresAt = &t.ExpiresAt
	}
	return j
}

type newAccessTokenInput struct {
	Body newAccessTokenJSON
}

type accessTokenInput struct {
	Token string `path:"token" doc:"The token's ID."`
}

func (s *Server) listAccessTokens(ctx context.Context, _ *struct{}) (*body[accessTokensJSON], error) {
	sess := sessionFrom(ctx)
	ts, err := s.AccessTokens.Tokens(ctx, sess.AccountID, sess.UserID)
	if err != nil {
		return nil, s.internal("access tokens not listed", err)
	}
	out := accessTokensJSON{Items: make([]accessTokenJSON, 0, len(ts))}
	for _, t := range ts {
		out.Items = append(out.Items, accessTokenOf(t))
	}
	return respond(out), nil
}

func (s *Server) createAccessToken(ctx context.Context, in *newAccessTokenInput) (*body[createdAccessTokenJSON], error) {
	sess := sessionFrom(ctx)
	secret, t, err := s.AccessTokens.IssueToken(ctx, sess.AccountID, sess.UserID, in.Body.Name, tokenExpiries[in.Body.Expiry])
	switch {
	case errors.Is(err, auth.ErrInvalidTokenName):
		return nil, apiError(http.StatusBadRequest, codeInvalidBody, "body.name: "+err.Error())
	case err != nil:
		return nil, s.internal("access token not issued", err)
	}
	s.Log.Info("access token issued", "token", t.ID, "user", t.UserID, "account", t.AccountID)
	j := accessTokenOf(t)
	return respond(createdAccessTokenJSON{
		Token: secret, ID: j.ID, Name: j.Name, CreatedAt: j.CreatedAt, LastUsedAt: j.LastUsedAt, ExpiresAt: j.ExpiresAt,
	}), nil
}

func (s *Server) deleteAccessToken(ctx context.Context, in *accessTokenInput) (*struct{}, error) {
	sess := sessionFrom(ctx)
	if !uuidPattern.MatchString(in.Token) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such access token")
	}
	err := s.AccessTokens.RevokeToken(ctx, sess.AccountID, sess.UserID, in.Token)
	switch {
	case errors.Is(err, auth.ErrUnknownToken):
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such access token")
	case err != nil:
		return nil, s.internal("access token not revoked", err)
	}
	s.Log.Info("access token revoked", "token", in.Token, "user", sess.UserID, "account", sess.AccountID)
	return &struct{}{}, nil
}

// registerTokens registers the management of the personal access tokens: with the
// session only, never with a token.
func (s *Server) registerTokens(api huma.API) {
	const errToken = "`not_found`: no such token of the user. Another user's token is not found either." //nolint:gosec // a description, no credential
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "listAccessTokens", Method: http.MethodGet, Path: "/tokens", Tags: []string{"tokens"},
		Summary: "The user's access tokens",
	}, "The tokens, without their secrets.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errSessionOnly,
		http.StatusInternalServerError: errInternal,
	}), s.listAccessTokens)
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "createAccessToken", Method: http.MethodPost, Path: "/tokens", Tags: []string{"tokens"},
		Summary:       "Issue an access token",
		Description:   "Its secret is in this response only.",
		DefaultStatus: http.StatusCreated, Middlewares: huma.Middlewares{s.requireJSON},
	}, "The token, with its secret.", writeErrors(map[int]string{
		http.StatusForbidden: errCrossOrigin + " " + errSessionOnly,
	})), s.createAccessToken)
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "deleteAccessToken", Method: http.MethodDelete, Path: "/tokens/{token}", Tags: []string{"tokens"},
		Summary: "Revoke an access token", Description: "It no longer authenticates, at once.",
		DefaultStatus: http.StatusNoContent,
	}, "Revoked.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin + " " + errSessionOnly,
		http.StatusNotFound: errToken, http.StatusInternalServerError: errInternal,
	}), s.deleteAccessToken)
}
