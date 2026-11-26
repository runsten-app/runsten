// Package ford is the client of FordConnect Query, the read-only API Ford opened on its
// developer portal: the owner creates an application there (a client ID, a secret, a
// redirect address), signs in to Ford with it, chooses a car and consents. It builds the
// address of that sign-in, exchanges its code, refreshes the access, and reads the two
// endpoints the collector needs: the garage (the car the grant reads) and the telemetry
// (its whole state, each metric with its own send time). It returns the raw responses:
// the collector stores them as they are, and Normalizer reads them into snapshots.
//
// A grant reads one car, the one chosen on Ford's consent page: neither endpoint takes a
// VIN. The telemetry is the copy Ford's cloud keeps of what the car sent; reading it does
// not wake the car. Read only: the API has no command.
//
// The specification is Ford's (FCON Query service 1.0.0, behind the portal's sign-in);
// the sign-in's addresses and what the token endpoint answers are those evcc and Home
// Assistant's FordConnect Query integration use. Nothing here is checked on a real car
// yet: what is assumed says so.
package ford

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The addresses of the real service. The token endpoint is the Azure AD B2C tenant's,
// behind Ford's gateway, its policy in the query.
const (
	DefaultAuthURL  = "https://api.vehicle.ford.com/fcon-public/v1/auth/init"
	DefaultTokenURL = "https://api.vehicle.ford.com/dah2vb2cprod.onmicrosoft.com/oauth2/v2.0/token?p=B2C_1A_FCON_AUTHORIZE" //nolint:gosec // an address, no credential
	DefaultAPIURL   = "https://api.vehicle.ford.com/fcon-query"
)

// maxBody caps the size of a response read (a telemetry of every metric is some tens of
// KiB).
const maxBody = 4 << 20

// scope is asked of the sign-in: the ID token, and a refresh token.
const scope = "openid offline_access"

// GrantLife is how long a sign-in reads before Ford asks for another: a year from the
// owner's consent (Ford's answer to a user of another client, to check).
const GrantLife = 365 * 24 * time.Hour

// MaxState is the longest state the sign-in carries back whole (Home Assistant's
// integration: 16 characters; a longer one came back "invalid state").
const MaxState = 16

// Hosts are the addresses of the service: Auth the page the owner is sent to, Token the
// token endpoint, API the base of the endpoints.
type Hosts struct {
	Auth, Token, API string
}

// DefaultHosts returns the real service's.
func DefaultHosts() Hosts {
	return Hosts{Auth: DefaultAuthURL, Token: DefaultTokenURL, API: DefaultAPIURL}
}

// Application is the owner's application on Ford's developer portal.
type Application struct {
	ClientID, ClientSecret string
	// RedirectURI is the one of its redirect addresses the grant was asked with: the
	// token endpoint is given it again at every exchange and refresh.
	RedirectURI string
}

// Tokens is what the token endpoint gives. It never rides in a log.
type Tokens struct {
	Access string
	// Refresh is the refresh token to use next: the one given, or the one sent when the
	// response gives none.
	Refresh   string
	ExpiresIn time.Duration
	// AuthTime is when the owner signed in and consented (the ID token's auth_time);
	// zero when the response tells none. Ford asks for a new sign-in a year after it.
	AuthTime time.Time
}

// Client calls Ford. Each method makes one request, and never retries: the caller knows
// what an error means for the connection.
type Client struct {
	hosts Hosts
	hc    *http.Client
}

// NewClient creates a client of the service at hosts. hc must have a timeout.
func NewClient(hosts Hosts, hc *http.Client) *Client {
	hosts.API = strings.TrimRight(hosts.API, "/")
	return &Client{hosts: hosts, hc: hc}
}

// AuthorizationURL returns the address the owner opens to sign in to Ford, choose the car
// and consent: Ford sends them back to the application's redirect address with a code and
// state. No PKCE: the application has a secret, and Ford's page does not ask for it.
func (c *Client) AuthorizationURL(app Application, state string) (string, error) {
	u, err := url.Parse(c.hosts.Auth)
	if err != nil {
		return "", fmt.Errorf("authorization url: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", app.ClientID)
	q.Set("redirect_uri", app.RedirectURI)
	q.Set("scope", scope)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Exchange exchanges the code of a sign-in for the tokens.
func (c *Client) Exchange(ctx context.Context, app Application, code string) (Tokens, error) {
	return c.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code}}, "")
}

// Refresh asks for a new access with the refresh token. The response may give another
// refresh token, to keep in place of the one sent (assumption: Azure AD B2C rotates them
// by default; whether Ford's policy does is not known).
func (c *Client) Refresh(ctx context.Context, app Application, refresh string) (Tokens, error) {
	return c.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}, refresh)
}

// token posts a grant to the token endpoint, the application's ID and secret in the
// form, as Ford's policy reads them.
func (c *Client) token(ctx context.Context, app Application, form url.Values, sent string) (Tokens, error) {
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	form.Set("redirect_uri", app.RedirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.hosts.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, fmt.Errorf("token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return Tokens{}, fmt.Errorf("token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Tokens{}, fmt.Errorf("token: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Tokens{}, newTokenError(resp, body)
	}
	var t struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		ID        string `json:"id_token"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return Tokens{}, errors.New("token: invalid JSON response")
	}
	if t.Access == "" {
		return Tokens{}, errors.New("token: no access token in the response")
	}
	out := Tokens{Access: t.Access, Refresh: t.Refresh, ExpiresIn: time.Duration(t.ExpiresIn) * time.Second, AuthTime: authTime(t.ID)}
	if out.Refresh == "" {
		out.Refresh = sent
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = defaultAccessLife
	}
	return out, nil
}

// defaultAccessLife is taken for an access whose lifetime the response does not tell:
// the 20 minutes other clients report.
const defaultAccessLife = 20 * time.Minute

// authTime reads auth_time, in Unix seconds, from an ID token's claims. The token comes
// from the token endpoint over TLS, and the claim only dates a reminder: its signature
// is not checked. Zero when unreadable.
func authTime(idToken string) time.Time {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		AuthTime json.Number `json:"auth_time"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return time.Time{}
	}
	sec, err := claims.AuthTime.Int64()
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// Garage reads the car the grant covers: its VIN, its model and its year.
func (c *Client) Garage(ctx context.Context, access string) ([]byte, error) {
	return c.get(ctx, access, Garage)
}

// Telemetry reads the car's state: every metric Ford's cloud holds, each with the time
// the car sent it.
func (c *Client) Telemetry(ctx context.Context, access string) ([]byte, error) {
	return c.get(ctx, access, Telemetry)
}

// Fetch reads the endpoint ep.
func (c *Client) Fetch(ctx context.Context, access string, ep Endpoint) ([]byte, error) {
	switch ep {
	case Garage, Telemetry:
		return c.get(ctx, access, ep)
	}
	return nil, fmt.Errorf("ford: unknown endpoint %q", ep)
}

func (c *Client) get(ctx context.Context, access string, ep Endpoint) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.hosts.API+"/v1/"+string(ep), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ep, err)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ep, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", ep, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError(ep, resp, body)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%s: invalid JSON response", ep)
	}
	return body, nil
}
