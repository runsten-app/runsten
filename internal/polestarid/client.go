// Package polestarid is the client of the Polestar app's API, read with the owner's
// Polestar ID (ADR 0042): an unofficial API, for the cars the Data Portal does not serve
// (outside the EU/EEA). The Polestar ID is an OpenID provider (PingFederate) that knows
// one redirection for polestar.com's client: Runsten gets the tokens by exchanging a code
// the owner pastes back, or by signing in with the owner's email and password as the app
// does. The refresh token rotates at every refresh. The data is the GraphQL of mystar-v2:
// the account's cars, and their telematics (battery, odometer, service); and two gRPC
// services of the app (grpc.go): the battery with its cable and charging power, and the
// charge limit.
//
// It returns raw responses: the collector stores them as is, and Normalizer reads them
// into snapshots.
package polestarid

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The addresses of the real Polestar ID and API. Another host serving the same paths (the
// simulator) is given to NewClient instead.
const (
	DefaultAuthURL = "https://polestarid.eu.polestar.com"
	DefaultAPIURL  = "https://pc-api.polestar.com/eu-north-1/mystar-v2"
)

// The OAuth client of polestar.com, its only redirection, and the scopes asked for: what
// pypolestar uses, checked on a real Polestar ID (ADR 0042).
const (
	ClientID    = "l3oopkc_10"
	RedirectURI = "https://www.polestar.com/sign-in-callback"
	scopes      = "openid profile email customer:attributes"
)

// maxBody caps the size of a response read.
const maxBody = 1 << 20

// Endpoint is a kind of response the client reads for a car.
type Endpoint string

// Endpoints.
const (
	// Telematics is carTelematicsV2 for one car: its battery, odometer and service.
	Telematics Endpoint = "telematics"
	// Cars is the car's entry of getConsumerCarsV2: its model and model year.
	Cars Endpoint = "cars"
)

// Hosts are the addresses of the Polestar ID, of the GraphQL and of the gRPC services,
// without a trailing slash needed.
type Hosts struct {
	Auth string // DefaultAuthURL: the issuer, under which /as/… are
	API  string // DefaultAPIURL
	// Discovery answers the host of the battery's gRPC (DefaultDiscoveryURL), PCCS serves
	// the charge limit's (DefaultPCCSURL). Empty, their calls fail: the GraphQL reads on.
	Discovery, PCCS string
}

// Tokens are what an exchange, a sign-in or a refresh gives.
type Tokens struct {
	Access    string
	ExpiresIn time.Duration
	// Refresh is the refresh token to keep: the Polestar ID rotates it at every refresh,
	// and the one given then no longer refreshes. Keep it before using the access token.
	Refresh string
}

// Client calls the Polestar ID and the API. Each method makes the requests its step
// needs, and never retries: the caller knows what an error means for the connection.
type Client struct {
	auth, api string
	hc        *http.Client
	grpc      *grpcHosts
}

// NewClient creates a client. hc must have a timeout.
func NewClient(h Hosts, hc *http.Client) *Client {
	return &Client{
		auth: strings.TrimRight(h.Auth, "/"), api: strings.TrimRight(h.API, "/"), hc: hc,
		grpc: &grpcHosts{discovery: h.Discovery, pccs: h.PCCS, hc: h2(hc)},
	}
}

// NewVerifier returns a PKCE code verifier (RFC 7636): 43 characters of 32 random bytes.
func NewVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	return base64.RawURLEncoding.EncodeToString(b)
}

func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c *Client) authorizeParams(challenge, state string) url.Values {
	return url.Values{
		"client_id": {ClientID}, "redirect_uri": {RedirectURI}, "response_type": {"code"},
		"scope": {scopes}, "state": {state}, "code_challenge": {challenge},
		"code_challenge_method": {"S256"}, "response_mode": {"query"},
	}
}

// AuthorizeURL is the sign-in page the owner opens: after it, polestar.com's
// sign-in-callback receives a code for verifier, which the owner pastes back (CodeOf).
func (c *Client) AuthorizeURL(verifier, state string) string {
	return c.AuthorizeURLOf(challenge(verifier), state)
}

// AuthorizeURLOf is AuthorizeURL for the S256 challenge of a verifier kept elsewhere.
func (c *Client) AuthorizeURLOf(challenge, state string) string {
	return c.auth + "/as/authorization.oauth2?" + c.authorizeParams(challenge, state).Encode()
}

// ErrNoCode is CodeOf's error when the address pasted holds no code.
var ErrNoCode = errors.New("no code in the address")

// CodeOf reads the code and the state of the sign-in-callback's address the owner pasted,
// or of the code alone.
func CodeOf(pasted string) (code, state string, err error) {
	pasted = strings.TrimSpace(pasted)
	if !strings.ContainsAny(pasted, "?=/") {
		if pasted == "" {
			return "", "", ErrNoCode
		}
		return pasted, "", nil
	}
	u, err := url.Parse(pasted)
	if err != nil {
		return "", "", ErrNoCode
	}
	q := u.Query()
	if q.Get("code") == "" {
		return "", "", ErrNoCode
	}
	return q.Get("code"), q.Get("state"), nil
}

// Exchange exchanges a code for the tokens, with the verifier its authorization was asked
// with.
func (c *Client) Exchange(ctx context.Context, code, verifier string) (Tokens, error) {
	return c.token(ctx, c.hc, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {ClientID}, "code": {code},
		"redirect_uri": {RedirectURI}, "code_verifier": {verifier},
	}, "")
}

// Refresh exchanges the refresh token for an access token and the next refresh token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	return c.token(ctx, c.hc, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {ClientID}, "refresh_token": {refreshToken},
	}, refreshToken)
}

func (c *Client) token(ctx context.Context, hc *http.Client, form url.Values, given string) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.auth+"/as/token.oauth2", strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, fmt.Errorf("token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	raw, err := do(hc, req, "token", tokenCall)
	if err != nil {
		return Tokens{}, err
	}
	var resp struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Tokens{}, fmt.Errorf("token: %w", err)
	}
	if resp.AccessToken == "" || resp.ExpiresIn <= 0 {
		return Tokens{}, errors.New("token: no access token with a lifetime in the response")
	}
	t := Tokens{Access: resp.AccessToken, ExpiresIn: time.Duration(resp.ExpiresIn * float64(time.Second)), Refresh: resp.RefreshToken}
	if t.Refresh == "" {
		t.Refresh = given
	}
	if t.Refresh == "" {
		return Tokens{}, errors.New("token: no refresh token in the response")
	}
	return t, nil
}

// resumeRe finds the form's resume path in the sign-in page (PingFederate's).
var resumeRe = regexp.MustCompile(`(?:url|action):\s*"([^"]+)"`)

// SignIn signs in with the owner's email and password as the app does: the sign-in page,
// its form, the confirmation of the terms when the Polestar ID asks for it, then the
// exchange of the code. A wrong email or password is a KindSignInRefused.
func (c *Client) SignIn(ctx context.Context, email, password string) (Tokens, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return Tokens{}, fmt.Errorf("sign-in: %w", err)
	}
	// Its own cookies, and no redirection followed: the code is read from the Location.
	hc := *c.hc
	hc.Jar = jar
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	verifier := NewVerifier()
	params := c.authorizeParams(challenge(verifier), NewVerifier())
	page, err := c.send(ctx, &hc, http.MethodGet, c.auth+"/as/authorization.oauth2?"+params.Encode(), nil)
	if err != nil {
		return Tokens{}, err
	}
	m := resumeRe.FindSubmatch(page.body)
	if page.status != http.StatusOK || m == nil {
		return Tokens{}, &APIError{Endpoint: "sign-in", Status: page.status, Message: "no sign-in form in the page"}
	}
	resume, err := url.Parse(c.auth)
	if err != nil {
		return Tokens{}, fmt.Errorf("sign-in: %w", err)
	}
	if resume, err = resume.Parse(string(m[1])); err != nil {
		return Tokens{}, fmt.Errorf("sign-in: %w", err)
	}
	resume.RawQuery = params.Encode()

	sent, err := c.send(ctx, &hc, http.MethodPost, resume.String(), url.Values{"pf.username": {email}, "pf.pass": {password}})
	if err != nil {
		return Tokens{}, err
	}
	loc, err := sent.redirection()
	if err != nil {
		return Tokens{}, err
	}
	// Terms to accept: the app confirms them for the user, and so does pypolestar.
	if loc.Query().Get("code") == "" && loc.Query().Get("uid") != "" {
		if sent, err = c.send(ctx, &hc, http.MethodPost, resume.String(), url.Values{"pf.submit": {"true"}, "subject": {loc.Query().Get("uid")}}); err != nil {
			return Tokens{}, err
		}
		if loc, err = sent.redirection(); err != nil {
			return Tokens{}, err
		}
	}
	code := loc.Query().Get("code")
	if code == "" {
		return Tokens{}, &APIError{Endpoint: "sign-in", Status: sent.status, Message: "no code in the redirection"}
	}
	return c.token(ctx, &hc, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {ClientID}, "code": {code},
		"redirect_uri": {RedirectURI}, "code_verifier": {verifier},
	}, "")
}

// wrongPasswordRe recognizes the sign-in page sent back for a wrong email or password: the
// real one writes `var authMessage = "ERR001";` (seen on 2026-11-01; pypolestar looks for
// `authMessage: "ERR001"`, which it does not write).
var wrongPasswordRe = regexp.MustCompile(`authMessage\s*[:=]\s*"ERR001"`)

// page is a response of the sign-in, read.
type page struct {
	status   int
	location string
	body     []byte
}

// redirection reads the form's answer: a redirection, or the page again with ERR001 for
// a wrong email or password.
func (p page) redirection() (*url.URL, error) {
	if p.status != http.StatusFound && p.status != http.StatusSeeOther {
		e := &APIError{Endpoint: "sign-in", Status: p.status, Message: "the sign-in did not redirect"}
		if wrongPasswordRe.Match(p.body) {
			e.Kind, e.Message = KindSignInRefused, "wrong email or password (ERR001)"
		}
		return nil, e
	}
	u, err := url.Parse(p.location)
	if err != nil {
		return nil, fmt.Errorf("sign-in: redirection: %w", err)
	}
	return u, nil
}

func (c *Client) send(ctx context.Context, hc *http.Client, method, target string, form url.Values) (page, error) {
	var body io.Reader = http.NoBody
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return page{}, fmt.Errorf("sign-in: %w", err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return page{}, fmt.Errorf("sign-in: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return page{}, fmt.Errorf("sign-in: read: %w", err)
	}
	return page{status: resp.StatusCode, location: resp.Header.Get("Location"), body: b}, nil
}

// The GraphQL queries: pypolestar's, the fields Runsten reads.
const (
	carsQuery       = `query GetConsumerCarsV2 { getConsumerCarsV2 { vin internalVehicleIdentifier modelYear modelName pno34 structureWeek } }`
	telematicsQuery = `query CarTelematicsV2($vins: [String!]!) { carTelematicsV2(vins: $vins) {
  health { vin brakeFluidLevelWarning daysToService distanceToServiceKm engineCoolantLevelWarning oilLevelWarning serviceWarning timestamp { seconds nanos } }
  battery { vin chargingStatusV2 batteryChargeLevelPercentage estimatedChargingTimeToFullMinutes estimatedDistanceToEmptyKm timestamp { seconds nanos } }
  odometer { vin odometerMeters timestamp { seconds nanos } } } }`
)

// Car is a car of the account.
type Car struct {
	VIN string
	// Raw is the car's entry of getConsumerCarsV2, as a response of Cars of its own:
	// {"data": {"getConsumerCarsV2": [<the car>]}}.
	Raw []byte
}

// Cars lists the account's cars.
func (c *Client) Cars(ctx context.Context, token string) ([]Car, error) {
	raw, err := c.query(ctx, token, string(Cars), carsQuery, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Cars []json.RawMessage `json:"getConsumerCarsV2"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("cars: %w", err)
	}
	out := []Car{}
	for _, car := range resp.Data.Cars {
		var v struct {
			VIN string `json:"vin"`
		}
		if json.Unmarshal(car, &v) != nil || v.VIN == "" {
			continue
		}
		one, err := json.Marshal(map[string]any{"data": map[string]any{"getConsumerCarsV2": []json.RawMessage{car}}})
		if err != nil {
			return nil, fmt.Errorf("cars: %w", err)
		}
		out = append(out, Car{VIN: v.VIN, Raw: one})
	}
	return out, nil
}

// Telematics returns the raw response of carTelematicsV2 for the car vin.
func (c *Client) Telematics(ctx context.Context, token, vin string) ([]byte, error) {
	return c.query(ctx, token, string(Telematics), telematicsQuery, map[string]any{"vins": []string{vin}})
}

func (c *Client) query(ctx context.Context, token, name, q string, variables map[string]any) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"query": q, "variables": variables})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api+"/", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	raw, err := do(c.hc, req, name, graphqlCall)
	if err != nil {
		return nil, err
	}
	// A GraphQL error comes in a 200, without data.
	var v struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(v.Errors) > 0 && (len(v.Data) == 0 || string(v.Data) == "null") {
		return nil, graphqlError(name, http.StatusOK, v.Errors[0].Message)
	}
	return raw, nil
}

// do sends req and returns its body when it is a 200 holding JSON.
func do(hc *http.Client, req *http.Request, name string, k call) ([]byte, error) {
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError(name, k, resp, body)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%s: invalid JSON response", name)
	}
	return body, nil
}
