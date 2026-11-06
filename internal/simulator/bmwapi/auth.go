package bmwapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DevClientID is the CarData client ID the simulator accepts, as the owner would copy it
// from the MyBMW portal: development only. Any other is refused.
const DevClientID = "6a3c1e52-5d0b-4c47-9e0f-7d2b9a4c8e11"

// GCID is the simulated BMW account's identifier, given with the tokens: the stream's
// username and its topics' first level.
const GCID = "0f8e2b7a-3c41-4d59-8a6e-1b2c3d4e5f60"

// The scopes CarData grants. The device code request asks for them with openid and
// authenticate_user; a token without one of them is refused by its API (CU-103).
const (
	ScopeAPI    = "cardata:api:read"
	ScopeStream = "cardata:streaming:read"
)

// The device grant's codes (RFC 8628).
const (
	deviceGrant   = "urn:ietf:params:oauth:grant-type:device_code"
	codeLifetime  = 5 * time.Minute
	pollInterval  = 5 * time.Second
	verifyAddress = "https://customer.bmwgroup.com/oneid/link"
)

// signingKey signs the tokens the simulator issues. It is fixed, so that a token issued
// before a restart of the simulator still reads after it, as a collector keeps it.
var signingKey = []byte("runsten-simulator-bmw")

// sign returns a token of kind holding the fields, and their signature.
func sign(kind string, fields ...string) string {
	body := kind + "." + strings.Join(fields, ".")
	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(body))
	return body + "." + hex.EncodeToString(mac.Sum(nil)[:16])
}

// verify returns the fields of a token of kind signed by the simulator.
func verify(tok, kind string) ([]string, bool) {
	i := strings.LastIndexByte(tok, '.')
	if i < 0 || !strings.HasPrefix(tok, kind+".") {
		return nil, false
	}
	parts := strings.Split(tok[:i], ".")
	if !hmac.Equal([]byte(sign(kind, parts[1:]...)), []byte(tok)) {
		return nil, false
	}
	return parts[1:], true
}

func nonce() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	return hex.EncodeToString(b)
}

// grant is a device code given and not yet exchanged.
type grant struct {
	challenge string // the PKCE challenge, S256
	scopes    []string
	issued    time.Time
	lastPoll  time.Time
}

// deviceCode answers POST /gcdm/oauth/device/code: a device code and the user code the
// owner enters at BMW's. The simulator approves it by itself Behavior.ApproveAfter later,
// as if the owner had.
func (h *Handler) deviceCode(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.PostFormValue("client_id") != DevClientID:
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "Unknown client.")
		return
	case r.PostFormValue("response_type") != "device_code":
		writeOAuthError(w, http.StatusBadRequest, "unsupported_response_type", "Only device_code is supported.")
		return
	case r.PostFormValue("code_challenge_method") != "S256" || r.PostFormValue("code_challenge") == "":
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "A code_challenge with S256 is required.")
		return
	}
	scopes := strings.Fields(r.PostFormValue("scope"))
	device, user := nonce(), strings.ToUpper(nonce()[:8])
	h.mu.Lock()
	h.grants[device] = &grant{challenge: r.PostFormValue("code_challenge"), scopes: scopes, issued: h.clk.Now()}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code":               device,
		"user_code":                 user,
		"verification_uri":          verifyAddress,
		"verification_uri_complete": verifyAddress + "?user_code=" + user,
		"expires_in":                int(codeLifetime.Seconds()),
		"interval":                  int(pollInterval.Seconds()),
	})
}

// token answers POST /gcdm/oauth/token: the device grant, polled until the owner
// approved, and the refresh grant. Each refresh rotates the refresh token, and the one
// given no longer refreshes: BMW issues a new one at each refresh (its integration guide), and
// whether the old one stays valid is unknown, so the strictest catches a client which
// does not keep the new one.
func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("client_id") != DevClientID {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "Unknown client.")
		return
	}
	now := h.clk.Now()
	var scopes []string
	switch r.PostFormValue("grant_type") {
	case deviceGrant:
		var errCode string
		scopes, errCode = h.useDeviceCode(r.PostFormValue("device_code"), r.PostFormValue("code_verifier"), now)
		if errCode != "" {
			writeOAuthError(w, http.StatusBadRequest, errCode, "")
			return
		}
	case "refresh_token":
		var ok bool
		if scopes, ok = h.useRefreshToken(r.PostFormValue("refresh_token"), now); !ok {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "The refresh_token is invalid.")
			return
		}
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "")
		return
	}
	writeJSON(w, http.StatusOK, h.tokens(scopes, now))
}

// useDeviceCode exchanges a device code for its scopes, or says why not, as RFC 8628
// §3.5: pending until the simulator approves it, slow_down when polled faster than its
// interval, expired after its lifetime.
func (h *Handler) useDeviceCode(device, verifier string, now time.Time) ([]string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g, ok := h.grants[device]
	switch {
	case !ok:
		return nil, "invalid_grant"
	case now.Sub(g.issued) > h.behavior.CodeLifetime:
		delete(h.grants, device)
		return nil, "expired_token"
	case !g.lastPoll.IsZero() && now.Sub(g.lastPoll) < pollInterval:
		g.lastPoll = now
		return nil, "slow_down"
	}
	g.lastPoll = now
	if now.Sub(g.issued) < h.behavior.ApproveAfter {
		return nil, "authorization_pending"
	}
	delete(h.grants, device)
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		return nil, "invalid_grant"
	}
	return g.scopes, ""
}

// tokens issues the access and ID tokens, of Behavior.TokenTTL, and a refresh token of
// refreshLifetime, for the scopes granted.
func (h *Handler) tokens(scopes []string, now time.Time) map[string]any {
	ttl := h.behavior.TokenTTL
	exp := strconv.FormatInt(now.Add(ttl).Unix(), 10)
	granted := scopeFlags(scopes)
	h.mu.Lock()
	gen := h.generation
	h.mu.Unlock()
	return map[string]any{
		"access_token":  sign("at", exp, granted, nonce()),
		"id_token":      sign("id", exp, GCID, granted, nonce()),
		"refresh_token": sign("rt", strconv.Itoa(gen), strconv.FormatInt(now.Add(refreshLifetime).Unix(), 10), granted, nonce()),
		"token_type":    "Bearer",
		"expires_in":    int(ttl.Seconds()),
		"scope":         strings.Join(scopes, " "),
		"gcid":          GCID,
	}
}

// refreshLifetime is a refresh token's: two weeks, BMW's integration guide says.
const refreshLifetime = 14 * 24 * time.Hour

// scopeFlags writes which CarData scopes a token holds: a for the API, s for the stream.
func scopeFlags(scopes []string) string {
	f := "-"
	if slices.Contains(scopes, ScopeAPI) {
		f += "a"
	}
	if slices.Contains(scopes, ScopeStream) {
		f += "s"
	}
	return f
}

// useRefreshToken consumes a refresh token: signed by the simulator, unexpired, issued
// since the last revocation, and not used yet. It returns the scopes it holds.
func (h *Handler) useRefreshToken(tok string, now time.Time) ([]string, bool) {
	fields, ok := verify(tok, "rt")
	if !ok || len(fields) != 4 {
		return nil, false
	}
	gen, err1 := strconv.Atoi(fields[0])
	exp, err2 := strconv.ParseInt(fields[1], 10, 64)
	if err1 != nil || err2 != nil || now.Unix() >= exp {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.used[tok] || gen < h.generation {
		return nil, false
	}
	h.used[tok] = true
	scopes := []string{"openid", "authenticate_user"}
	if strings.Contains(fields[2], "a") {
		scopes = append(scopes, ScopeAPI)
	}
	if strings.Contains(fields[2], "s") {
		scopes = append(scopes, ScopeStream)
	}
	return scopes, true
}

// accessScopes checks the access token of a call of the API: signed by the simulator
// and unexpired. It returns its scope flags, or the CarData error code of the refusal.
func accessScopes(r *http.Request, now time.Time) (string, string) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return "", "CU-100"
	}
	fields, ok := verify(tok, "at")
	if !ok || len(fields) != 3 {
		return "", "CU-101"
	}
	exp, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || now.Unix() >= exp {
		return "", "CU-102"
	}
	return fields[1], ""
}

// idTokenExpiry checks an ID token given as the stream's password: signed by the
// simulator for the GCID, with the stream's scope. It returns its expiry.
func idTokenExpiry(tok, gcid string) (time.Time, bool) {
	fields, ok := verify(tok, "id")
	if !ok || len(fields) != 4 || fields[1] != gcid || !strings.Contains(fields[2], "s") {
		return time.Time{}, false
	}
	exp, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(exp, 0).UTC(), true
}
