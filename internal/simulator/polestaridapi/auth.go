package polestaridapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The Polestar ID's client and redirection, polestar.com's: the only redirection it
// accepts (checked on the real one, ADR 0042).
const (
	ClientID    = "l3oopkc_10"
	RedirectURI = "https://www.polestar.com/sign-in-callback"
)

// The account the simulator signs in: development only.
const (
	DevEmail    = "owner@example.com"
	DevPassword = "runsten-dev" //nolint:gosec // the simulator's, never a real one
)

// signingKey signs the tokens the simulator issues. It is fixed, so that a token issued
// before a restart of the simulator still reads after it, as a collector keeps it.
var signingKey = []byte("runsten-simulator-polestar-id")

// DevRefreshToken returns a refresh token the simulator accepts, as an exchange would give
// it: development only. Like any refresh token, it no longer refreshes once rotated, until
// the simulator restarts.
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

// flow is a sign-in in progress: the authorization's parameters, until the form is sent.
type flow struct {
	challenge, state string
	at               time.Time
}

// grant is a code issued by the form, until it is exchanged.
type grant struct {
	challenge string
	expires   time.Time
}

// issuer is the simulated Polestar ID's base URL, as the request reached it.
func issuer(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + strings.TrimSuffix(Prefix, "/")
}

func (h *Handler) openIDConfiguration(w http.ResponseWriter, r *http.Request) {
	iss := issuer(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                           iss,
		"authorization_endpoint":           iss + "/as/authorization.oauth2",
		"token_endpoint":                   iss + "/as/token.oauth2",
		"response_types_supported":         []string{"code"},
		"code_challenge_methods_supported": []string{"S256"},
	})
}

// authorize answers GET /as/authorization.oauth2 with the sign-in page: PingFederate's
// form, whose resume path the page names (action: "…"), as the real one does.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case q.Get("client_id") != ClientID:
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	case q.Get("redirect_uri") != RedirectURI:
		// The real one shows an error page and goes nowhere: a redirection it does not know.
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	case q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "":
		http.Error(w, "response_type code with an S256 code_challenge expected", http.StatusBadRequest)
		return
	}
	id := nonce()
	h.mu.Lock()
	h.flows[id] = flow{challenge: q.Get("code_challenge"), state: q.Get("state"), at: h.clk.Now()}
	h.mu.Unlock()
	writeForm(w, resumePath(id), "")
}

func resumePath(id string) string { return Prefix + "as/" + id + "/resume/as/authorization.ping" }

// writeForm writes the sign-in page; message, when set, is the error the real page carries
// (var authMessage = "ERR001"; for a wrong email or password, as seen on the real one).
func writeForm(w http.ResponseWriter, action, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	a := html.EscapeString(action)
	fmt.Fprintf(w, `<!doctype html><title>Polestar ID (simulated)</title>
<script>var authMessage = "%[2]s";
var config = { action: "%[1]s", authMessage: authMessage };</script>
<form method="post" action="%[1]s">
<label>Email <input name="pf.username" type="email"></label>
<label>Password <input name="pf.pass" type="password"></label>
<button>Sign in</button>
</form>
`, a, html.EscapeString(message))
}

// resume answers the form's POST: a code sent to the redirection, or the page again with
// ERR001.
func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("flow")
	h.mu.Lock()
	f, ok := h.flows[id]
	h.mu.Unlock()
	now := h.clk.Now()
	if !ok || now.Sub(f.at) > flowTTL {
		http.Error(w, "the sign-in expired: start again", http.StatusBadRequest)
		return
	}
	if r.PostFormValue("pf.username") != DevEmail || r.PostFormValue("pf.pass") != DevPassword {
		writeForm(w, resumePath(id), "ERR001")
		return
	}
	code := sign("code", nonce())
	h.mu.Lock()
	delete(h.flows, id)
	h.codes[code] = grant{challenge: f.challenge, expires: now.Add(h.behavior.CodeTTL)}
	h.mu.Unlock()
	to := RedirectURI + "?" + url.Values{"code": {code}, "state": {f.state}}.Encode()
	http.Redirect(w, r, to, http.StatusFound)
}

// flowTTL is how long a sign-in page waits for its form.
const flowTTL = 30 * time.Minute

// token answers POST /as/token.oauth2: the authorization code with its PKCE verifier, or a
// refresh token. Each refresh rotates the refresh token, and the one given no longer
// refreshes; using it again revokes nothing (both checked on the real one).
func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("client_id") != ClientID {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "Unknown client.")
		return
	}
	now := h.clk.Now()
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		if r.PostFormValue("redirect_uri") != RedirectURI || !h.useCode(r.PostFormValue("code"), r.PostFormValue("code_verifier"), now) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "Authorization code is invalid or expired.")
			return
		}
	case "refresh_token":
		if !h.useRefreshToken(r.PostFormValue("refresh_token")) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "Refresh token is invalid or expired.")
			return
		}
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "Only authorization_code and refresh_token are simulated.")
		return
	}
	ttl := h.behavior.AccessTokenTTL
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  sign("at", strconv.FormatInt(now.Add(ttl).Unix(), 10), nonce()),
		"refresh_token": h.newRefreshToken(),
		"id_token":      sign("id", nonce()),
		"token_type":    "Bearer",
		"expires_in":    int(ttl.Seconds()),
	})
}

// useCode consumes a code issued by the form, within its lifetime, for the verifier of its
// challenge. An exchange with another verifier leaves it: polestar.com's own attempt, which
// lacks the verifier, did not spend it on the real one (ADR 0042), so that the user may
// paste it afterwards.
func (h *Handler) useCode(code, verifier string, now time.Time) bool {
	sum := sha256.Sum256([]byte(verifier))
	h.mu.Lock()
	defer h.mu.Unlock()
	g, ok := h.codes[code]
	if !ok || !now.Before(g.expires) || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		return false
	}
	delete(h.codes, code)
	return true
}

// newRefreshToken issues a refresh token of the current generation: a revocation starts
// the next one, and refuses the tokens of the previous ones.
func (h *Handler) newRefreshToken() string {
	h.mu.Lock()
	gen := h.generation
	h.mu.Unlock()
	return sign("rt", strconv.Itoa(gen), nonce())
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

// authorized checks the access token of a GraphQL call: signed by the simulator and not
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
