package volvo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"runsten/internal/oauth"
)

// DefaultAuthURL is the address of Volvo ID, the authorization server.
const DefaultAuthURL = "https://volvoid.eu.volvocars.com"

// Volvo ID endpoints (documented).
const (
	authorizePath = "/as/authorization.oauth2"
	tokenPath     = "/as/token.oauth2" //nolint:gosec // endpoint path, not a credential
)

// DefaultScopes are the read scopes of the endpoints the collector polls. Runsten
// never requests a command scope. location:read is reportedly subject to a manual
// review by Volvo (community source): an application without it must set its own
// list.
func DefaultScopes() []string {
	return []string{
		"openid",
		"conve:vehicle_relation", "conve:engine_status", "conve:odometer_status", "conve:trip_statistics",
		"conve:diagnostics_workshop", "conve:brake_status", "conve:diagnostics_engine_status", "conve:fuel_status",
		"conve:tyre_status", "conve:warnings",
		"conve:doors_status", "conve:lock_status", "conve:windows_status",
		"energy:state:read", "energy:capability:read",
		"location:read",
	}
}

// CommandScopes are the scopes that allow acting on the vehicle. Runsten is a logger:
// they are refused in the configuration.
func CommandScopes() []string {
	return []string{
		"conve:lock", "conve:unlock", "conve:engine_start_stop", "conve:honk_flash",
		"conve:climatization_start_stop",
	}
}

// ValidateScopes checks that scopes are read scopes, with openid.
func ValidateScopes(scopes []string) error {
	if !slices.Contains(scopes, "openid") {
		return errors.New("scope openid is required")
	}
	var errs []error
	for _, s := range scopes {
		if slices.Contains(CommandScopes(), s) {
			errs = append(errs, fmt.Errorf("scope %s allows commands: Runsten only reads", s))
		}
	}
	return errors.Join(errs...)
}

// reauthCodes are the token endpoint error codes that mean the grant is gone
// (refresh token expired, revoked or already used, code invalid).
//
// Assumption: Volvo ID follows RFC 6749 §5.2 and answers invalid_grant. Its actual
// response to an expired or reused refresh token has not been observed.
func reauthCodes() []string { return []string{"invalid_grant"} }

// AuthConfig is an application registered on the Volvo developer portal.
type AuthConfig struct {
	BaseURL      string // Volvo ID address (DefaultAuthURL, or the simulator)
	ClientID     string
	ClientSecret string // never leaves the backend
	RedirectURI  string // as registered with the application
	Scopes       []string
}

// AuthClient implements the OAuth 2.0 Authorization Code flow with PKCE of Volvo ID,
// on golang.org/x/oauth2. Only what is specific to Volvo is here: the endpoints, the
// scopes and which errors mean that the grant is gone.
type AuthClient struct {
	conf *oauth2.Config
	hc   *http.Client
}

// NewAuthClient creates a client. hc must have a timeout.
func NewAuthClient(cfg AuthConfig, hc *http.Client) *AuthClient {
	base := strings.TrimRight(cfg.BaseURL, "/")
	return &AuthClient{
		conf: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURI,
			Scopes:       cfg.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  base + authorizePath,
				TokenURL: base + tokenPath,
				// Documented: Basic base64(client_id:client_secret). Set explicitly, so
				// that the library never retries with the secret in the body.
				AuthStyle: oauth2.AuthStyleInHeader,
			},
		},
		hc: hc,
	}
}

// AuthorizeURL returns the address where the user authorizes the application. The
// challenge is computed by the caller, which keeps the verifier.
func (c *AuthClient) AuthorizeURL(state, challenge string) string {
	return c.conf.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"), // assumption: S256 is supported (standard method)
	)
}

// Exchange exchanges an authorization code and its PKCE verifier for tokens.
func (c *AuthClient) Exchange(ctx context.Context, code, verifier string) (oauth.Grant, error) {
	tok, err := c.conf.Exchange(c.context(ctx), code, oauth2.VerifierOption(verifier))
	return grant("code exchange", tok, err)
}

// Refresh exchanges a refresh token for new tokens. With rotation, the refresh token
// passed is invalid afterwards. If the response has no new refresh token, the one
// passed is returned.
func (c *AuthClient) Refresh(ctx context.Context, refreshToken string) (oauth.Grant, error) {
	// Without an access token, the token source refreshes on the first call.
	tok, err := c.conf.TokenSource(c.context(ctx), &oauth2.Token{RefreshToken: refreshToken}).Token()
	return grant("token refresh", tok, err)
}

func (c *AuthClient) context(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, c.hc)
}

// AuthError is an error response of the token endpoint. It never contains a token.
type AuthError struct {
	Op          string
	Status      int
	Code        string // RFC 6749 error code, empty if the body is not an OAuth error
	Description string
}

func (e *AuthError) Error() string {
	msg := fmt.Sprintf("%s: HTTP %d", e.Op, e.Status)
	if e.Code != "" {
		msg += ": " + e.Code
	}
	if e.Description != "" {
		msg += ": " + e.Description
	}
	return msg
}

// ReauthRequired reports whether the grant is gone: only a new authorization by the
// user can restore the connection. A rejected client (wrong secret) is not one: it is
// a configuration error of the instance.
func (e *AuthError) ReauthRequired() bool { return slices.Contains(reauthCodes(), e.Code) }

// grant converts the library's result. Its errors carry the raw response body: only
// the status, the code and a truncated description are kept.
func grant(op string, tok *oauth2.Token, err error) (oauth.Grant, error) {
	var re *oauth2.RetrieveError
	switch {
	case errors.As(err, &re):
		e := &AuthError{Op: op, Code: re.ErrorCode, Description: re.ErrorDescription}
		if re.Response != nil {
			e.Status = re.Response.StatusCode
		}
		const maxLen = 200
		if len(e.Description) > maxLen {
			e.Description = e.Description[:maxLen]
		}
		return oauth.Grant{}, e
	case err != nil:
		return oauth.Grant{}, fmt.Errorf("%s: %w", op, err)
	}
	g := oauth.Grant{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken}
	// The access token lifetime is not documented (1799 s then 299 s observed): read it
	// from each response. The library's Expiry uses the system time, not the injected
	// clock, so it is ignored. Without expires_in, the token is renewed after a 401 only.
	if tok.ExpiresIn > 0 {
		g.ExpiresIn = time.Duration(tok.ExpiresIn) * time.Second
	}
	return g, nil
}
