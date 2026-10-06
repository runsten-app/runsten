package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"runsten/internal/oauth"
)

// Application keys of the tests: the simulator accepts goodKey and otherKey, refuses
// badKey.
const (
	goodKey  = "0123456789abcdef0123456789abcdef"
	otherKey = "fedcba9876543210fedcba9876543210"
	badKey   = "badbadbadbadbadbadbadbadbadbad99"
)

func keyBody(key string) string { return `{"key":"` + key + `"}` }

// TestAPIKey: on an instance without a key of its own, the account's key is given,
// replaced and removed; it is never returned, only its last four characters and when it
// was given, and never logged.
func TestAPIKey(t *testing.T) {
	e := newEnvKeys(t, 10, "", nil)
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"

	resp, body := get(t, noFollow(), u, c)
	if resp.status != http.StatusOK || body != `{"connected":false,"client_id":"runsten-dev","instance_key":null,"api_key":null}`+"\n" {
		t.Errorf("no connection: %d %s", resp.status, body)
	}
	resp, body = do(t, noFollow(), http.MethodPut, u+"/api-key", jsonType, keyBody(goodKey), c)
	if resp.status != http.StatusOK {
		t.Fatalf("PUT: %d %s", resp.status, body)
	}
	golden(t, "connection.json", body)
	if strings.Contains(body, goodKey[:8]) {
		t.Errorf("the key is returned: %s", body)
	}
	if e.store.key.Value != goodKey || !e.store.key.SetAt.Equal(e.clk.Now()) {
		t.Errorf("stored key: %+v", e.store.key)
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}
	if resp, body := do(t, noFollow(), http.MethodDelete, u+"/api-key", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	if _, body := get(t, noFollow(), u, c); !strings.HasSuffix(body, `"api_key":null}`+"\n") || e.store.key.Value != "" {
		t.Errorf("after DELETE: %s", body)
	}
	if strings.Contains(logs.String(), goodKey) {
		t.Errorf("the key is logged:\n%s", logs)
	}

	for _, tt := range []struct{ name, body string }{
		{"too short", keyBody("0123456789")},
		{"too long", keyBody(strings.Repeat("a", 129))},
		{"a space", keyBody("0123456789abcdef 0123456789abcde")},
		{"no key", `{}`},
		{"unknown field", `{"key":"` + goodKey + `","primary":true}`},
	} {
		resp, body := do(t, noFollow(), http.MethodPut, u+"/api-key", jsonType, tt.body, c)
		if resp.status != http.StatusBadRequest || strings.Contains(body, "0123456789") {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		wantError(t, body, codeInvalidBody)
	}
	resp, body = do(t, noFollow(), http.MethodPut, u+"/api-key", "text/plain", keyBody(goodKey), c)
	if resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d", resp.status)
	}
	wantError(t, body, codeUnsupportedMediaType)

	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "", ""}, {http.MethodPut, "/api-key", keyBody(goodKey)}, {http.MethodDelete, "/api-key", ""},
	} {
		resp, body := do(t, noFollow(), req.method, u+req.path, jsonType, req.body)
		if resp.status != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d", req.method, resp.status)
		}
		wantError(t, body, codeUnauthorized)
	}
	if e.store.key.Value != "" {
		t.Error("a key stored without a session, or an invalid one")
	}

	e.store.keysErr = errors.New("boom")
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "", ""}, {http.MethodPut, "/api-key", keyBody(goodKey)}, {http.MethodDelete, "/api-key", ""},
	} {
		resp, body := do(t, noFollow(), req.method, u+req.path, jsonType, req.body, c)
		if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
			t.Errorf("%s, store down: %d %s", req.method, resp.status, body)
		}
		wantError(t, body, codeInternal)
	}
}

// TestInstanceKeyOnly: an instance with a key of its own reads every vehicle with it: an
// account may not give one, and one left from before is not used, not even to list the
// vehicles at the connection.
func TestInstanceKeyOnly(t *testing.T) {
	const instance = "zq9xw7v5k3-0123456789abcd"
	e := newEnvKeys(t, 10, instance, []string{instance})
	b := e.signedIn(t)
	u := e.api.URL + apiPrefix + "/connection"
	// The page tells which application the instance uses, never its key.
	if _, body := get(t, b, u); !strings.Contains(body, `"client_id":"runsten-dev","instance_key":{"last4":"abcd"}`) ||
		strings.Contains(body, instance[:8]) {
		t.Errorf("connection: %s", body)
	}
	resp, body := do(t, b, http.MethodPut, u+"/api-key", jsonType, keyBody(goodKey))
	if resp.status != http.StatusConflict || e.store.key.Value != "" {
		t.Errorf("PUT with the instance's key: %d %s", resp.status, body)
	}
	wantError(t, body, codeInstanceKey)

	e.store.key = oauth.APIKey{Value: badKey, SetAt: e.clk.Now()} // left from before
	resp, body = get(t, b, e.api.URL+"/auth/volvo/start")
	if resp.status != http.StatusOK || len(e.store.vehicles) != 1 || e.store.refused {
		t.Errorf("connection with a key left from before: %d %s; vehicles %v", resp.status, body, e.store.vehicles)
	}
	if resp, _ := do(t, b, http.MethodDelete, u+"/api-key", "", ""); resp.status != http.StatusNoContent || e.store.key.Value != "" {
		t.Errorf("DELETE of a key left from before: %d", resp.status)
	}
}

// TestNoInstanceKey: without the instance's key, a Volvo ID is not connected before
// the account gives its own; the vehicles are then listed with it, and read with it.
func TestNoInstanceKey(t *testing.T) {
	e := newEnvKeys(t, 10, "", []string{goodKey})
	b := e.signedIn(t)
	resp, body := get(t, b, e.api.URL+"/auth/volvo/start")
	if resp.status != http.StatusConflict || !strings.Contains(body, "Volvo key required") {
		t.Fatalf("start without a key: %d %s", resp.status, body)
	}
	if _, body := get(t, b, e.api.URL+apiPrefix+"/connection"); !strings.Contains(body, `"instance_key":null`) {
		t.Errorf("connection: %s", body)
	}
	if resp, body := do(t, b, http.MethodPut, e.api.URL+apiPrefix+"/connection/api-key", jsonType, keyBody(goodKey)); resp.status != http.StatusOK {
		t.Fatalf("PUT before the Volvo ID: %d %s", resp.status, body)
	}
	resp, body = get(t, b, e.api.URL+"/auth/volvo/start")
	if resp.status != http.StatusOK || !strings.Contains(body, "Volvo ID connected") || len(e.store.vehicles) != 1 {
		t.Fatalf("flow with the account's key: %d %s; vehicles %v", resp.status, body, e.store.vehicles)
	}
}

// TestKeyRefused: a key Volvo refuses at the callback keeps the grant and records no
// vehicle; a corrected key lists them, without a new consent. A key refused when given
// is not stored.
func TestKeyRefused(t *testing.T) {
	e := newEnvKeys(t, 10, "", []string{goodKey, otherKey})
	b := e.signedIn(t)
	u := e.api.URL + apiPrefix + "/connection"
	if resp, _ := do(t, b, http.MethodPut, u+"/api-key", jsonType, keyBody(badKey)); resp.status != http.StatusOK {
		t.Fatal("a key given before the Volvo ID is not checked: it should be stored")
	}
	resp, body := get(t, b, e.api.URL+"/auth/volvo/start")
	if resp.status != http.StatusOK || !strings.Contains(body, "Volvo key refused") {
		t.Fatalf("callback with a refused key: %d %s", resp.status, body)
	}
	if e.store.count() != 1 || len(e.store.vehicles) != 0 || !e.store.refused {
		t.Fatalf("connections %d, vehicles %v, refused %v: want the grant kept, no vehicle, the key refused",
			e.store.count(), e.store.vehicles, e.store.refused)
	}
	if _, body := get(t, b, u); !strings.Contains(body, `"connected":true`) || strings.Contains(body, `"refused_at":null`) {
		t.Errorf("connection: %s", body)
	}

	resp, body = do(t, b, http.MethodPut, u+"/api-key", jsonType, keyBody(badKey))
	if resp.status != http.StatusBadRequest || e.store.count() != 1 {
		t.Errorf("a refused key, with the Volvo ID: %d %s", resp.status, body)
	}
	wantError(t, body, codeAPIKeyRefused)

	resp, body = do(t, b, http.MethodPut, u+"/api-key", jsonType, keyBody(goodKey))
	if resp.status != http.StatusOK || !strings.Contains(body, `"refused_at":null`) || len(e.store.vehicles) != 1 {
		t.Errorf("a corrected key: %d %s; vehicles %v", resp.status, body, e.store.vehicles)
	}
	if e.store.count() != 1 {
		t.Errorf("%d connections saved: the grant was asked again", e.store.count())
	}
}

// TestKeyTooManyVehicles: a key that lists more vehicles than the cap is not stored.
func TestKeyTooManyVehicles(t *testing.T) {
	e := newEnvKeys(t, 10, "", []string{goodKey, otherKey})
	b := e.signedIn(t)
	u := e.api.URL + apiPrefix + "/connection"
	if resp, _ := do(t, b, http.MethodPut, u+"/api-key", jsonType, keyBody(goodKey)); resp.status != http.StatusOK {
		t.Fatal("key not stored")
	}
	if resp, body := get(t, b, e.api.URL+"/auth/volvo/start"); resp.status != http.StatusOK || !strings.Contains(body, "Volvo ID connected") {
		t.Fatalf("connect: %d %s", resp.status, body)
	}
	e.server.Vehicles, e.server.MaxVehicles = twoVehicles{}, 1
	resp, body := do(t, b, http.MethodPut, u+"/api-key", jsonType, keyBody(otherKey))
	if resp.status != http.StatusConflict || e.store.key.Value != goodKey || len(e.store.vehicles) != 1 {
		t.Errorf("%d %s; key %q, vehicles %v", resp.status, body, e.store.key.Value, e.store.vehicles)
	}
	wantError(t, body, codeTooManyVehicles)
}
