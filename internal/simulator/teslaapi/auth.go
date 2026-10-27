package teslaapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// clientID is the Tesla app's OAuth client, the only one the Owner API accepts tokens of.
const clientID = "ownerapi"

// signingKey signs the tokens the simulator issues. It is fixed, so that a token issued
// before a restart of the simulator still reads after it, as a collector keeps it.
var signingKey = []byte("runsten-simulator-tesla")

// DevRefreshToken returns the refresh token the simulator accepts by default, as the
// owner would paste it from Tesla Auth: development only. Like any refresh token, it no
// longer refreshes once rotated, until the simulator restarts.
func DevRefreshToken() string { return sign("rt", "0", "dev") }

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

// newRefreshToken issues a refresh token of the current generation: a revocation starts
// the next one, and refuses the tokens of the previous ones. A restarted simulator is back
// at generation 0, and accepts them all again.
func (h *Handler) newRefreshToken() string {
	h.mu.Lock()
	gen := h.generation
	h.mu.Unlock()
	return sign("rt", strconv.Itoa(gen), nonce())
}

// token answers POST /oauth2/v3/token, the refresh grant of the Tesla app's client, in a
// form or in JSON. Each refresh rotates the refresh token, and the one given no longer
// refreshes: the strictest of what Tesla may do (ADR 0040 §2, unknown), so that a
// collector which does not keep the new one is caught.
func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GrantType    string `json:"grant_type"`
		ClientID     string `json:"client_id"`
		RefreshToken string `json:"refresh_token"`
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "The request body is not valid JSON.")
			return
		}
	} else {
		req.GrantType, req.ClientID, req.RefreshToken = r.PostFormValue("grant_type"), r.PostFormValue("client_id"), r.PostFormValue("refresh_token")
	}
	switch {
	case req.GrantType != "refresh_token":
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "Only the refresh_token grant is simulated.")
		return
	case req.ClientID != clientID:
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "Unknown client.")
		return
	}
	now := h.clk.Now()
	if !h.useRefreshToken(req.RefreshToken) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "The refresh_token is invalid.")
		return
	}
	ttl := h.behavior.TokenTTL
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  sign("at", strconv.FormatInt(now.Add(ttl).Unix(), 10), nonce()),
		"refresh_token": h.newRefreshToken(),
		"id_token":      sign("id", nonce()),
		"expires_in":    int(ttl.Seconds()),
		"token_type":    "Bearer",
	})
}

// useRefreshToken consumes a refresh token: signed by the simulator, issued since the last
// revocation, and not used yet.
func (h *Handler) useRefreshToken(tok string) bool {
	fields, ok := verify(tok, "rt")
	if !ok || len(fields) != 2 {
		return false
	}
	gen, err := strconv.Atoi(fields[0])
	if err != nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.used[tok] || gen < h.generation {
		return false
	}
	h.used[tok] = true
	return true
}

// authorized checks the access token of a call of /api/1: signed by the simulator and not
// expired. A revocation leaves the access tokens issued alive until their expiry.
func (h *Handler) authorized(r *http.Request, now time.Time) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	fields, ok := verify(tok, "at")
	if !ok || len(fields) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(fields[0], 10, 64)
	return err == nil && now.Unix() < exp
}
