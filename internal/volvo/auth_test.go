package volvo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
)

// authSim starts the simulator (API and Volvo ID) and returns an API client, an
// authorization client registered as cfg's application, and the shared clock.
func authSim(t *testing.T, cfg volvoapi.OAuth) (*Client, *AuthClient, *clock.Manual, *httptest.Server) {
	t.Helper()
	sc := &scenario.Scenario{
		VIN: simVIN, Start: time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC),
		Vehicle: scenario.VehicleConfig{
			Profile: "bev-generic", Model: "EX-SIM", ModelYear: 2026, BatteryKWh: 80,
			ConsumptionKWhPer100km: 18, SoC: 60, TargetSoC: 80, Place: "home",
		},
		Places: map[string]scenario.Place{"home": {Lat: 45.764, Lon: 4.8357}},
		Steps:  []scenario.Step{{Park: &scenario.Park{Duration: time.Hour}}},
	}
	sim, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	srv := httptest.NewServer(volvoapi.NewHandler([]volvoapi.Source{sim}, clk, volvoapi.DefaultLimits(), volvoapi.Faults{}, cfg))
	t.Cleanup(srv.Close)
	auth := NewAuthClient(AuthConfig{
		BaseURL: srv.URL + "/", ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
		RedirectURI: cfg.RedirectURI, Scopes: DefaultScopes(),
	}, srv.Client())
	return NewClient(srv.URL, "key", srv.Client()), auth, clk, srv
}

// authorize plays the user's consent: it follows the authorization URL and returns
// the code from the redirect.
func authorize(ctx context.Context, t *testing.T, srv *httptest.Server, auth *AuthClient, challenge string) string {
	t.Helper()
	hc := *srv.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, auth.AuthorizeURL("state-1", challenge), http.NoBody)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: %d %v", resp.StatusCode, err)
	}
	if loc.Query().Get("state") != "state-1" || !strings.HasPrefix(loc.String(), volvoapi.DefaultOAuth().RedirectURI) {
		t.Fatalf("redirect = %s", loc)
	}
	return loc.Query().Get("code")
}

func reauth(err error) bool {
	var r interface{ ReauthRequired() bool }
	return errors.As(err, &r) && r.ReauthRequired()
}

func TestAuthorizeURL(t *testing.T) {
	auth := NewAuthClient(AuthConfig{
		BaseURL: DefaultAuthURL, ClientID: "id", RedirectURI: "https://runsten.example/auth/volvo/callback",
		Scopes: []string{"openid", "conve:odometer_status"},
	}, http.DefaultClient)
	u, err := url.Parse(auth.AuthorizeURL("s t", "chal"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "volvoid.eu.volvocars.com" || u.Path != "/as/authorization.oauth2" || q.Get("response_type") != "code" ||
		q.Get("client_id") != "id" || q.Get("redirect_uri") != "https://runsten.example/auth/volvo/callback" ||
		q.Get("scope") != "openid conve:odometer_status" || q.Get("state") != "s t" ||
		q.Get("code_challenge") != "chal" || q.Get("code_challenge_method") != "S256" {
		t.Errorf("authorize URL = %s", u)
	}
	if q.Has("client_secret") {
		t.Error("the client secret leaves the backend")
	}
}

// TestAuthorizationAgainstSimulator is the contract test between the client and the
// simulated Volvo ID: exchange, use, expiry, refresh with rotation.
func TestAuthorizationAgainstSimulator(t *testing.T) {
	cfg := volvoapi.DefaultOAuth()
	api, auth, clk, srv := authSim(t, cfg)
	ctx := context.Background()
	verifier := "a-verifier-of-at-least-forty-three-characters-long"

	code := authorize(ctx, t, srv, auth, oauth2.S256ChallengeFromVerifier(verifier))
	g, err := auth.Exchange(ctx, code, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if g.AccessToken == "" || g.RefreshToken == "" || g.ExpiresIn != cfg.AccessTokenTTL {
		t.Fatalf("grant = %+v", g)
	}
	if _, err := api.Vehicles(ctx, "", g.AccessToken); err != nil {
		t.Fatalf("API with the token: %v", err)
	}

	clk.Advance(g.ExpiresIn)
	_, err = api.Vehicles(ctx, "", g.AccessToken)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != KindUnauthorized {
		t.Fatalf("expired token: %v", err)
	}

	next, err := auth.Refresh(ctx, g.RefreshToken)
	if err != nil || next.RefreshToken == g.RefreshToken {
		t.Fatalf("refresh: %+v, %v", next, err)
	}
	if _, err := api.Vehicles(ctx, "", next.AccessToken); err != nil {
		t.Errorf("API with the refreshed token: %v", err)
	}
	if _, err := auth.Refresh(ctx, g.RefreshToken); !reauth(err) {
		t.Errorf("rotated refresh token: %v, want re-authentication", err)
	}
}

func TestAuthErrors(t *testing.T) {
	cfg := volvoapi.DefaultOAuth()
	ctx := context.Background()
	verifier := "a-verifier-of-at-least-forty-three-characters-long"

	t.Run("wrong PKCE verifier", func(t *testing.T) {
		_, auth, _, srv := authSim(t, cfg)
		code := authorize(ctx, t, srv, auth, oauth2.S256ChallengeFromVerifier(verifier))
		_, err := auth.Exchange(ctx, code, "another-verifier-of-at-least-forty-three-chars")
		var ae *AuthError
		if !errors.As(err, &ae) || ae.Code != "invalid_grant" || ae.Status != http.StatusBadRequest {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("wrong client secret: configuration, not re-authentication", func(t *testing.T) {
		_, auth, _, srv := authSim(t, cfg)
		code := authorize(ctx, t, srv, auth, oauth2.S256ChallengeFromVerifier(verifier))
		auth.conf.ClientSecret = "regenerated"
		_, err := auth.Exchange(ctx, code, verifier)
		var ae *AuthError
		if !errors.As(err, &ae) || ae.Code != "invalid_client" || reauth(err) {
			t.Errorf("err = %v", err)
		}
		if strings.Contains(err.Error(), "regenerated") {
			t.Errorf("secret in the error: %v", err)
		}
	})
	t.Run("grant expired", func(t *testing.T) {
		short := cfg
		short.GrantTTL = 12 * time.Hour
		_, auth, clk, srv := authSim(t, short)
		g, err := auth.Exchange(ctx, authorize(ctx, t, srv, auth, oauth2.S256ChallengeFromVerifier(verifier)), verifier)
		if err != nil {
			t.Fatal(err)
		}
		clk.Advance(short.GrantTTL)
		if _, err := auth.Refresh(ctx, g.RefreshToken); !reauth(err) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestTokenResponses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    oauth.Grant
		wantErr bool
		reauth  bool
	}{
		{
			"expires_in as a string", 200, `{"access_token":"a","refresh_token":"r","token_type":"Bearer","expires_in":"299"}`,
			oauth.Grant{AccessToken: "a", RefreshToken: "r", ExpiresIn: 299 * time.Second},
			false, false,
		},
		// Not rotated: the library returns the refresh token that was presented.
		{"no expires_in, no refresh token", 200, `{"access_token":"a"}`, oauth.Grant{AccessToken: "a", RefreshToken: "rt"}, false, false},
		{"no access token", 200, `{"refresh_token":"r"}`, oauth.Grant{}, true, false},
		{"invalid JSON", 200, `<html>`, oauth.Grant{}, true, false},
		{"invalid_grant", 400, `{"error":"invalid_grant","error_description":"expired"}`, oauth.Grant{}, true, true},
		{"server error", 503, `Service Unavailable`, oauth.Grant{}, true, false},
		{"long description", 400, `{"error":"invalid_request","error_description":"` + strings.Repeat("x", 500) + `"}`, oauth.Grant{}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, secret, ok := r.BasicAuth()
				const wantSecret = "s%26cret" //nolint:gosec // test value, escaped as RFC 6749 §2.3.1 requires
				if !ok || id != "id" || secret != wantSecret || r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") != "rt" {
					t.Errorf("request: basic %q %q %v, form %v", id, secret, ok, r.Form)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			auth := NewAuthClient(AuthConfig{BaseURL: srv.URL, ClientID: "id", ClientSecret: "s&cret"}, srv.Client())
			g, err := auth.Refresh(context.Background(), "rt")
			if (err != nil) != tt.wantErr || g != tt.want || reauth(err) != tt.reauth {
				t.Errorf("Refresh = %+v, %v", g, err)
			}
			if err != nil && len(err.Error()) > 300 {
				t.Errorf("error not truncated: %d characters", len(err.Error()))
			}
		})
	}

	t.Run("unreachable", func(t *testing.T) {
		auth := NewAuthClient(AuthConfig{BaseURL: "http://127.0.0.1:1"}, &http.Client{Timeout: time.Second})
		if _, err := auth.Refresh(context.Background(), "rt"); err == nil || reauth(err) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestValidateScopes(t *testing.T) {
	if err := ValidateScopes(DefaultScopes()); err != nil {
		t.Errorf("default scopes: %v", err)
	}
	if err := ValidateScopes([]string{"conve:odometer_status"}); err == nil {
		t.Error("openid missing: error expected")
	}
	if err := ValidateScopes([]string{"openid", "conve:unlock"}); err == nil || !strings.Contains(err.Error(), "conve:unlock") {
		t.Errorf("command scope: %v", err)
	}
}
