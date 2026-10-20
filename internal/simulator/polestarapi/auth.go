package polestarapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
)

// Credential is what the owner creates in the Data Portal: the OAuth client and the
// account it reads, whose ID goes in the x-client-id header.
type Credential struct {
	ClientID, ClientSecret, AccountID string
}

// DevCredential returns the credential the simulator accepts by default, development
// only.
func DevCredential() Credential {
	return Credential{ //nolint:gosec // the simulator's, never a real one
		ClientID:     "runsten-dev",
		ClientSecret: "runsten-dev-secret",
		AccountID:    "5e1ad0c3-7a2b-4c1d-9e8f-0123456789ab",
	}
}

// Limits groups the simulated limits and lifetimes. A zero value disables the
// corresponding limit.
type Limits struct {
	// DailyQuota and PerMinute count the calls of /v1 per credential (10,000 and 100
	// according to the community, not documented).
	DailyQuota, PerMinute int
	// TokenTTL is the lifetime of an access token (3,600 s observed).
	TokenTTL time.Duration
	// CredentialTTL is the credential's lifetime, from the simulation's start (90 days
	// according to the community): past it, /token refuses it.
	CredentialTTL time.Duration
}

// DefaultLimits returns the observed limits. The credential's expiry is disabled by
// default: it would stop a collector after 90 simulated days.
func DefaultLimits() Limits { return Limits{DailyQuota: 10_000, PerMinute: 100, TokenTTL: time.Hour} }

// authority issues the tokens and checks the calls of /v1.
type authority struct {
	clk    clock.Clock
	cred   Credential
	limits Limits
	since  time.Time // the credential's creation

	mu     sync.Mutex
	tokens map[string]time.Time // token → expiry
	day    time.Time
	daily  int
	minute time.Time
	perMin int
}

func newAuthority(clk clock.Clock, cred Credential, limits Limits) *authority {
	return &authority{clk: clk, cred: cred, limits: limits, since: clk.Now(), tokens: map[string]time.Time{}}
}

// token answers POST /token: clientId and clientSecret, in JSON or a form, without
// grant_type. The scope is accepted as it is: the simulated credential holds them all.
func (a *authority) token(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	now := a.clk.Now()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeOAuthError(w, now, http.StatusBadRequest, "invalid_request", "The request body is not valid JSON.")
			return
		}
	} else {
		req.ClientID, req.ClientSecret = r.PostFormValue("clientId"), r.PostFormValue("clientSecret")
	}
	switch {
	case req.ClientID == "" || req.ClientSecret == "":
		writeOAuthError(w, now, http.StatusBadRequest, "invalid_request", "clientId and clientSecret are required.")
		return
	case req.ClientID != a.cred.ClientID || req.ClientSecret != a.cred.ClientSecret,
		a.limits.CredentialTTL > 0 && !now.Before(a.since.Add(a.limits.CredentialTTL)):
		writeOAuthError(w, now, http.StatusUnauthorized, "invalid_client", "Invalid client credentials.")
		return
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	tok := hex.EncodeToString(b)
	ttl := a.limits.TokenTTL
	a.mu.Lock()
	a.tokens[tok] = now.Add(ttl)
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"accessToken": tok, "expiresIn": int(ttl.Seconds()), "tokenType": "Bearer"})
}

// middleware checks the token, the x-client-id header and the limits of a call of /v1,
// in that order.
func (a *authority) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := a.clk.Now()
		tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !a.valid(tok, now) {
			// Assumed: the body of a 401 on /v1 has not been published.
			writeError(w, now, http.StatusUnauthorized, "AUTHN_INVALID_TOKEN", "The access token is missing, invalid or expired.", nil)
			return
		}
		switch id := r.Header.Get("x-client-id"); {
		case id == "":
			// Observed: the message is itself a JSON string.
			writeError(w, now, http.StatusBadRequest, "VALIDATION_INVALID_PARAMETER",
				`{"field":"x-client-id","reason":"Required header is missing"}`, nil)
			return
		case id != a.cred.AccountID:
			writeError(w, now, http.StatusForbidden, "AUTHZ_CLIENT_ID_MISMATCH",
				"The x-client-id header does not match the identity associated with this token.", nil)
			return
		}
		if !a.allow(now) {
			// Assumed: a 429 was seen, never its body nor its headers.
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "Too Many Requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *authority) valid(tok string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.tokens[tok]
	return ok && (a.limits.TokenTTL <= 0 || now.Before(exp))
}

// allow counts a call against the credential's minute and day.
func (a *authority) allow(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if day := now.UTC().Truncate(24 * time.Hour); !day.Equal(a.day) {
		a.day, a.daily = day, 0
	}
	if m := now.Truncate(time.Minute); !m.Equal(a.minute) {
		a.minute, a.perMin = m, 0
	}
	if (a.limits.DailyQuota > 0 && a.daily >= a.limits.DailyQuota) ||
		(a.limits.PerMinute > 0 && a.perMin >= a.limits.PerMinute) {
		return false
	}
	a.daily++
	a.perMin++
	return true
}
