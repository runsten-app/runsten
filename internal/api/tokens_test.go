package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/auth"
	"runsten/internal/platform/clock"
)

// fakeTokens is an in-memory AccessTokens.
type fakeTokens struct {
	mu     sync.Mutex
	tokens map[string]auth.Token // by secret
	next   int
	clk    clock.Clock
	err    error // returned by every call when set
}

func newFakeTokens(clk clock.Clock) *fakeTokens {
	return &fakeTokens{tokens: map[string]auth.Token{}, clk: clk}
}

func tokenID(n int) string { return fmt.Sprintf("7a0e3c1e-0000-4000-8000-%012d", n) }

func (f *fakeTokens) IssueToken(_ context.Context, accountID, userID, name string, ttl time.Duration) (string, auth.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", auth.Token{}, f.err
	}
	if name = strings.TrimSpace(name); name == "" {
		return "", auth.Token{}, auth.ErrInvalidTokenName
	}
	f.next++
	now := f.clk.Now()
	t := auth.Token{ID: tokenID(f.next), AccountID: accountID, UserID: userID, Name: name, CreatedAt: now}
	if ttl > 0 {
		t.ExpiresAt = now.Add(ttl)
	}
	secret := fmt.Sprintf("%s%043d", auth.TokenPrefix, f.next)
	f.tokens[secret] = t
	return secret, t, nil
}

func (f *fakeTokens) AuthenticateToken(_ context.Context, secret string) (auth.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return auth.Token{}, f.err
	}
	t, ok := f.tokens[secret]
	if !ok || t.Expired(f.clk.Now()) {
		return auth.Token{}, auth.ErrNoToken
	}
	t.LastUsedAt = f.clk.Now()
	f.tokens[secret] = t
	return t, nil
}

func (f *fakeTokens) Tokens(_ context.Context, accountID, userID string) ([]auth.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []auth.Token
	for _, t := range f.tokens {
		if t.AccountID == accountID && t.UserID == userID {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b auth.Token) int { return strings.Compare(b.ID, a.ID) })
	return out, f.err
}

func (f *fakeTokens) RevokeToken(_ context.Context, accountID, userID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	for secret, t := range f.tokens {
		if t.ID == id && t.AccountID == accountID && t.UserID == userID {
			delete(f.tokens, secret)
			return nil
		}
	}
	return auth.ErrUnknownToken
}

// token issues an access token to username directly, and returns its secret and ID.
func (e *env) token(t *testing.T, username string) (string, string) {
	t.Helper()
	secret, tok, err := e.tokens.IssueToken(context.Background(), users[username], "user-"+username, "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	return secret, tok.ID
}

// withBearer sends a request with an Authorization header, and the cookies.
func withBearer(t *testing.T, method, u, authorization, contentType, body string, cookies ...*http.Cookie) (reply, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), method, u, strings.NewReader(body))
	req.Header.Set("Authorization", authorization)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	resp, err := noFollow().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, header: resp.Header, cookies: resp.Cookies()}, string(b)
}

func TestAccessTokensJSON(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/tokens"

	// Issued with the session, the secret in this response only.
	resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, `{"name":"  Home Assistant ","expiry":"90d"}`, c)
	if resp.status != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.status, body)
	}
	var created createdAccessTokenJSON
	if err := json.Unmarshal([]byte(body), &created); err != nil || !strings.HasPrefix(created.Token, "rst_") ||
		created.Name != "Home Assistant" || created.LastUsedAt != nil || created.ExpiresAt == nil ||
		!created.ExpiresAt.Equal(e.clk.Now().Add(90*24*time.Hour)) {
		t.Fatalf("created %s (%v)", body, err)
	}
	if resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, `{"name":"forever","expiry":"never"}`, c); resp.status != http.StatusCreated ||
		!strings.Contains(body, `"expires_at":null`) {
		t.Errorf("without expiry: %d %s", resp.status, body)
	}
	for _, bad := range []string{`{"name":"   ","expiry":"30d"}`, `{"name":"x","expiry":"1d"}`, `{"name":"x"}`, `{"name":"","expiry":"30d"}`} {
		resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, bad, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, resp.status)
		}
		wantError(t, body, "invalid_body")
	}

	resp, body = do(t, noFollow(), http.MethodPost, u, "text/plain", `{"name":"x","expiry":"30d"}`, c)
	if resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("not JSON: %d", resp.status)
	}
	wantError(t, body, "unsupported_media_type")

	// Listed without the secrets; another user's are not.
	_, body = get(t, noFollow(), u, c)
	if strings.Contains(body, "rst_") || strings.Count(body, `"id"`) != 2 || !strings.Contains(body, `"name":"Home Assistant"`) {
		t.Errorf("list %s", body)
	}
	if _, body := get(t, noFollow(), u, e.session(t, "other")); body != `{"items":[]}`+"\n" {
		t.Errorf("another user's list: %s", body)
	}

	// It reads as the session does, and its use is recorded.
	bearer := "Bearer " + created.Token
	_, viaCookie := get(t, noFollow(), e.api.URL+"/api/v1/vehicles", c)
	resp, viaToken := withBearer(t, http.MethodGet, e.api.URL+"/api/v1/vehicles", bearer, "", "")
	if resp.status != http.StatusOK || viaToken != viaCookie {
		t.Errorf("vehicles with the token: %d %s", resp.status, viaToken)
	}
	if _, body := get(t, noFollow(), u, c); !strings.Contains(body, `"last_used_at":"`) {
		t.Errorf("use not recorded: %s", body)
	}

	// A request with an Authorization header is judged on it alone, not on the cookie.
	for _, authz := range []string{"Bearer rst_unknown", "Basic YWRtaW46cGFzc3dvcmQ=", "Bearer", "rst_" + created.Token} {
		resp, body := withBearer(t, http.MethodGet, e.api.URL+"/api/v1/vehicles", authz, "", "", c)
		if resp.status != http.StatusUnauthorized || resp.header.Get("WWW-Authenticate") != `Bearer error="invalid_token"` {
			t.Errorf("%s: %d %v", authz, resp.status, resp.header)
		}
		wantError(t, body, "unauthorized")
	}
	if resp, _ := withBearer(t, http.MethodGet, e.api.URL+"/api/v1/vehicles", "bearer "+created.Token, "", ""); resp.status != http.StatusOK {
		t.Errorf("lowercase scheme: %d", resp.status)
	}

	// It only reads.
	resp, body = withBearer(t, http.MethodPost, e.api.URL+"/api/v1/places", bearer, jsonType, workBody)
	if resp.status != http.StatusForbidden || resp.header.Get("WWW-Authenticate") != `Bearer error="insufficient_scope"` {
		t.Errorf("write with a token: %d %v", resp.status, resp.header)
	}
	wantError(t, body, "insufficient_scope")

	// Revoked: at once, and only by its user.
	if resp, _ := do(t, noFollow(), http.MethodDelete, u+"/"+created.ID, "", "", e.session(t, "other")); resp.status != http.StatusNotFound {
		t.Errorf("revoked by another user: %d", resp.status)
	}
	if resp, body := do(t, noFollow(), http.MethodDelete, u+"/"+created.ID, "", "", c); resp.status != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", resp.status, body)
	}
	if resp, _ := withBearer(t, http.MethodGet, e.api.URL+"/api/v1/vehicles", bearer, "", ""); resp.status != http.StatusUnauthorized {
		t.Errorf("revoked token: %d", resp.status)
	}
	for _, id := range []string{created.ID, "not-a-uuid"} {
		resp, body := do(t, noFollow(), http.MethodDelete, u+"/"+id, "", "", c)
		if resp.status != http.StatusNotFound {
			t.Errorf("revoke %s: %d", id, resp.status)
		}
		wantError(t, body, "not_found")
	}

	// Expired.
	secret, _ := e.token(t, "admin")
	e.tokens.mu.Lock()
	tok := e.tokens.tokens[secret]
	tok.ExpiresAt = e.clk.Now()
	e.tokens.tokens[secret] = tok
	e.tokens.mu.Unlock()
	if resp, _ := withBearer(t, http.MethodGet, e.api.URL+"/api/v1/vehicles", "Bearer "+secret, "", ""); resp.status != http.StatusUnauthorized {
		t.Errorf("expired token: %d", resp.status)
	}

	// Failures: never a detail.
	live, _ := e.token(t, "admin")
	e.tokens.err = errors.New("boom")
	for _, r := range []struct{ method, url, body string }{
		{http.MethodGet, u, ""}, {http.MethodPost, u, `{"name":"x","expiry":"30d"}`}, {http.MethodDelete, u + "/" + tokenID(1), ""},
	} {
		resp, body := do(t, noFollow(), r.method, r.url, jsonType, r.body, c)
		if resp.status != http.StatusInternalServerError {
			t.Errorf("%s %s failing: %d", r.method, r.url, resp.status)
		}
		wantError(t, body, "internal")
	}
	resp, body = withBearer(t, http.MethodGet, e.api.URL+"/api/v1/vehicles", "Bearer "+live, "", "")
	if resp.status != http.StatusInternalServerError {
		t.Errorf("token check failing: %d", resp.status)
	}
	wantError(t, body, "internal")
}

// TestAccessTokenOnEveryOperation: a token reads every GET but the session's and the
// tokens', is refused everything else, and is rate limited.
func TestAccessTokenOnEveryOperation(t *testing.T) {
	e := newEnv(t, 10)
	path := strings.NewReplacer("{vehicle}", car, "{id}", "2026-09-28T07:01:00Z", "{place}", placeID(1), "{token}", tokenID(1))
	sessionOnly := []string{"GET /session", "DELETE /session", "GET /tokens", "POST /tokens", "DELETE /tokens/{token}", "GET /mqtt"}
	for _, op := range specOperations(t) {
		if op == "POST /session" {
			continue
		}
		method, p, _ := strings.Cut(op, " ")
		u := e.api.URL + apiPrefix + path.Replace(p)
		secret, id := e.token(t, "admin")
		if method != http.MethodGet || slices.Contains(sessionOnly, op) {
			resp, body := withBearer(t, method, u, "Bearer "+secret, jsonType, "{}")
			if resp.status != http.StatusForbidden || resp.header.Get("WWW-Authenticate") != `Bearer error="insufficient_scope"` {
				t.Errorf("%s with a token: %d", op, resp.status)
			}
			wantError(t, body, "insufficient_scope")
			continue
		}
		for range tokenBurst {
			e.server.tokenLimiter.take(id, e.clk.Now())
		}
		resp, body := withBearer(t, method, u, "Bearer "+secret, "", "")
		if resp.status != http.StatusTooManyRequests || resp.header.Get("Retry-After") != "1" {
			t.Errorf("%s past the rate: %d %v", op, resp.status, resp.header)
		}
		wantError(t, body, "rate_limited")
	}
}

func TestServeAs(t *testing.T) {
	e, c := readerEnv(t)
	serve := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", jsonType)
		e.server.ServeAs(rec, r, account, "user-admin")
		return rec
	}
	_, want := get(t, noFollow(), e.api.URL+"/api/v1/vehicles", c)
	if rec := serve(http.MethodGet, "/api/v1/vehicles", ""); rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("read as the account: %d %s", rec.Code, rec.Body)
	}
	for _, r := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/places"}, {http.MethodGet, "/api/v1/session"}, {http.MethodGet, "/api/v1/tokens"},
	} {
		if rec := serve(r.method, r.path, workBody); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as the account: %d", r.method, r.path, rec.Code)
		}
	}
	// A request from the network cannot claim a principal.
	if resp, _ := get(t, noFollow(), e.api.URL+"/api/v1/vehicles"); resp.status != http.StatusUnauthorized {
		t.Errorf("without credentials: %d", resp.status)
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(3, 2)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		if wait := l.take("a", now); wait != 0 {
			t.Fatalf("request %d: wait %v", i, wait)
		}
	}
	if wait := l.take("a", now); wait != 500*time.Millisecond {
		t.Errorf("past the burst: wait %v", wait)
	}
	if wait := l.take("b", now); wait != 0 {
		t.Errorf("another key: wait %v", wait)
	}
	if wait := l.take("a", now.Add(500*time.Millisecond)); wait != 0 {
		t.Errorf("after the wait: %v", wait)
	}
	if wait := l.take("a", now.Add(time.Hour)); wait != 0 || l.buckets["a"].left != 2 {
		t.Errorf("refilled up to the burst only: %v, %v left", wait, l.buckets["a"].left)
	}
}
