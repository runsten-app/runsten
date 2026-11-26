package ford

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var app = Application{ClientID: "client-1234", ClientSecret: "s3cr3t-value", RedirectURI: "https://runsten.example/auth/ford/callback"}

// service is a token endpoint and an API answering as scripted.
type service struct {
	t      *testing.T
	token  func(w http.ResponseWriter, form url.Values)
	api    func(w http.ResponseWriter, r *http.Request)
	client *Client
}

func newService(t *testing.T) *service {
	t.Helper()
	s := &service{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tenant/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.URL.Query().Get("p") != "B2C_1A_FCON_AUTHORIZE" {
			t.Errorf("the policy was lost from the token URL: %s", r.URL)
		}
		s.token(w, r.PostForm)
	})
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) { s.api(w, r) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s.client = NewClient(Hosts{
		Auth: srv.URL + "/auth/init", Token: srv.URL + "/tenant/oauth2/v2.0/token?p=B2C_1A_FCON_AUTHORIZE", API: srv.URL + "/api/",
	}, srv.Client())
	return s
}

// idToken is an unsigned ID token of the claims given.
func idToken(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(claims)) + ".sig"
}

func TestAuthorizationURL(t *testing.T) {
	c := NewClient(DefaultHosts(), http.DefaultClient)
	raw, err := c.AuthorizationURL(app, "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if u.Host != "api.vehicle.ford.com" || u.Path != "/fcon-public/v1/auth/init" {
		t.Errorf("address %s", raw)
	}
	for k, want := range map[string]string{
		"response_type": "code", "client_id": app.ClientID, "redirect_uri": app.RedirectURI,
		"scope": "openid offline_access", "state": "0123456789abcdef",
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if strings.Contains(raw, app.ClientSecret) {
		t.Error("the secret in the address")
	}
}

// TestExchangeAndRefresh: the code, then the refresh token, with the application's ID
// and secret and its redirect address in the form; the refresh token given kept, the one
// sent when none is given; the sign-in's time read from the ID token.
func TestExchangeAndRefresh(t *testing.T) {
	s := newService(t)
	s.token = func(w http.ResponseWriter, form url.Values) {
		if form.Get("client_id") != app.ClientID || form.Get("client_secret") != app.ClientSecret || form.Get("redirect_uri") != app.RedirectURI {
			t.Errorf("form %v", form)
		}
		switch form.Get("grant_type") {
		case "authorization_code":
			if form.Get("code") != "the-code" {
				t.Errorf("code %q", form.Get("code"))
			}
			_, _ = w.Write([]byte(`{"access_token":"a1","refresh_token":"r1","expires_in":1200,"id_token":"` + idToken(`{"auth_time":1795000000}`) + `"}`))
		case "refresh_token":
			if form.Get("refresh_token") == "r1" {
				_, _ = w.Write([]byte(`{"access_token":"a2","refresh_token":"r2","expires_in":"x"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"a3"}`))
		}
	}
	tok, err := s.client.Exchange(t.Context(), app, "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "a1" || tok.Refresh != "r1" || tok.ExpiresIn != 20*time.Minute || !tok.AuthTime.Equal(time.Unix(1795000000, 0)) {
		t.Errorf("tokens %+v", tok)
	}
	// A lifetime of another type is an unreadable response.
	if _, err := s.client.Refresh(t.Context(), app, "r1"); err == nil {
		t.Error("an unreadable token response accepted")
	}
	tok, err = s.client.Refresh(t.Context(), app, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "a3" || tok.Refresh != "r2" || tok.ExpiresIn != defaultAccessLife || !tok.AuthTime.IsZero() {
		t.Errorf("tokens %+v: the refresh token sent is kept, the lifetime defaulted", tok)
	}
}

// TestTokenErrors: what the token endpoint's refusals mean.
func TestTokenErrors(t *testing.T) {
	for name, c := range map[string]struct {
		status  int
		header  string
		body    string
		kind    Kind
		refused bool
		retry   time.Duration
	}{
		"grant over":       {400, "", `{"error":"invalid_grant","error_description":"AADB2C90080: The provided grant has expired."}`, KindGrantRefused, true, 0},
		"wrong secret":     {401, "", `{"error":"invalid_client","error_description":"AADB2C90081: The specified client_secret does not match."}`, KindClientRefused, true, 0},
		"unknown client":   {400, "", `{"error":"unauthorized_client","error_description":"AADB2C90057"}`, KindClientRefused, true, 0},
		"ford's outage":    {400, "", `{"error":"invalid_grant","error_description":"AADB2C90027: Basic credentials specified for 'RESTAcquireAccessToken' are invalid."}`, KindOther, false, 0},
		"limited":          {429, "300", `Too Many Requests`, KindRateLimited, false, 5 * time.Minute},
		"limited, untold":  {429, "", `{}`, KindRateLimited, false, defaultRateRetry},
		"gateway's 502":    {502, "", `<html>Bad Gateway</html>`, KindOther, false, 0},
		"an error unknown": {400, "", `{"error":"server_error"}`, KindOther, false, 0},
	} {
		s := newService(t)
		s.token = func(w http.ResponseWriter, _ url.Values) {
			if c.header != "" {
				w.Header().Set("Retry-After", c.header)
			}
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}
		_, err := s.client.Refresh(t.Context(), app, "r1")
		var e *APIError
		if !errors.As(err, &e) {
			t.Fatalf("%s: %v", name, err)
		}
		if e.Kind != c.kind || e.CredentialRefused() != c.refused || e.RetryIn != c.retry || e.Status != c.status {
			t.Errorf("%s: %+v", name, e)
		}
		if strings.Contains(err.Error(), app.ClientSecret) || strings.Contains(err.Error(), "r1") {
			t.Errorf("%s: a secret in the error: %v", name, err)
		}
	}
}

// TestFetch: the garage and the telemetry, read with the access; the API's refusals,
// their VINs masked.
func TestFetch(t *testing.T) {
	s := newService(t)
	s.api = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":401,"message":"Unauthorized"}`))
			return
		}
		switch r.URL.Path {
		case "/api/v1/garage":
			_, _ = w.Write([]byte(`{"vin":"WF0TESTPUMA000001"}`))
		case "/api/v1/telemetry":
			_, _ = w.Write([]byte(`{"metrics":{}}`))
		}
	}
	if raw, err := s.client.Garage(t.Context(), "a1"); err != nil || string(raw) != `{"vin":"WF0TESTPUMA000001"}` {
		t.Errorf("garage %s, %v", raw, err)
	}
	if raw, err := s.client.Fetch(t.Context(), "a1", Telemetry); err != nil || string(raw) != `{"metrics":{}}` {
		t.Errorf("telemetry %s, %v", raw, err)
	}
	if _, err := s.client.Fetch(t.Context(), "a1", "wallbox"); err == nil {
		t.Error("an unknown endpoint read")
	}
	var e *APIError
	if _, err := s.client.Telemetry(t.Context(), "old"); !errors.As(err, &e) || e.Kind != KindUnauthorized || e.Op != "telemetry" {
		t.Errorf("an old access: %v", err)
	}
	for name, c := range map[string]struct {
		status int
		header string
		body   string
		kind   Kind
		retry  time.Duration
	}{
		"limited":         {429, "42", `{"errorCode": 429,"errorMessage": "Too Many Requests"}`, KindRateLimited, 42 * time.Second},
		"limited, untold": {429, "", ``, KindRateLimited, defaultRateRetry},
		"forbidden":       {403, "", `{"error":403,"message":"Forbidden for WF0TESTPUMA000001"}`, KindForbidden, 0},
		"not found":       {404, "", `{"error":404,"message":"Not Found"}`, KindNotFound, 0},
		"outage":          {503, "", `Service Unavailable`, KindOther, 0},
	} {
		s.api = func(w http.ResponseWriter, _ *http.Request) {
			if c.header != "" {
				w.Header().Set("Retry-After", c.header)
			}
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}
		_, err := s.client.Telemetry(t.Context(), "a1")
		if !errors.As(err, &e) || e.Kind != c.kind || e.RetryIn != c.retry || e.CredentialRefused() {
			t.Errorf("%s: %v", name, err)
		}
		if strings.Contains(err.Error(), "WF0TESTPUMA000001") {
			t.Errorf("%s: a VIN in the error: %v", name, err)
		}
	}
	s.api = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) }
	if _, err := s.client.Telemetry(t.Context(), "a1"); err == nil {
		t.Error("a response that is not JSON read")
	}
}
