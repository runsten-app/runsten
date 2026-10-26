// Package tesla is the client of Tesla's Owner API, the unofficial API of the Tesla app,
// read with the owner's tokens: a refresh token the owner gives, refreshed into access
// tokens, the account's vehicles, their state and their data. It returns raw responses:
// the collector stores them as is, and Normalizer reads them into snapshots.
//
// It never wakes a car up: a logger reads what the car tells while it is awake, and a car
// kept awake loses energy.
package tesla

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The addresses of the real API and of its authentication. Another host serving the same
// paths (the simulator, a proxy, the Fleet API) is given to NewClient instead.
const (
	DefaultAPIURL  = "https://owner-api.teslamotors.com"
	DefaultAuthURL = "https://auth.tesla.com/oauth2/v3"
)

// The OAuth client of the Tesla app, and the scopes of the tokens it is given: the Owner
// API accepts no other client's.
const (
	clientID = "ownerapi"
	scopes   = "openid email offline_access"
)

// vehicleDataEndpoints are the blocks of vehicle_data asked for. location_data is what
// puts the position in drive_state. vehicle_data_combo is left out: an opaque blob.
const vehicleDataEndpoints = "charge_state;climate_state;drive_state;gui_settings;location_data;vehicle_config;vehicle_state"

// maxBody caps the size of a response read (a vehicle_data is about 9 KiB).
const maxBody = 1 << 20

// Endpoint is a kind of response the client reads for a vehicle.
type Endpoint string

// Endpoints.
const (
	// VehicleState is /vehicles/{id}: the vehicle's state (online, asleep, offline),
	// read without waking the car.
	VehicleState Endpoint = "vehicle"
	// VehicleData is /vehicles/{id}/vehicle_data: everything, answered by an awake car
	// only (408 otherwise).
	VehicleData Endpoint = "vehicle_data"
)

// Hosts are the addresses of the API and of its authentication, without a trailing
// slash needed.
type Hosts struct {
	API  string // DefaultAPIURL
	Auth string // DefaultAuthURL: the token endpoint is its /token
}

// Tokens are what a refresh gives.
type Tokens struct {
	Access    string
	ExpiresIn time.Duration
	// Refresh is the refresh token to keep: a new one when the server rotated it, else
	// the one given (RFC 6749 §6). Keep it before using the access token: the one given
	// may no longer refresh.
	Refresh string
}

// Product is a vehicle of the account. ID is Tesla's identifier of the vehicle, which
// the Owner API reads it by, not the VIN.
type Product struct {
	ID  string
	VIN string
}

// Client calls the API. Each method makes one request, and never retries: the caller
// knows what an error means for the connection.
type Client struct {
	api, auth string
	hc        *http.Client
	authHC    *http.Client // nil when hc's transport cannot be held to TLS 1.3
}

// NewClient creates a client. hc must have a timeout; its transport, nil or an
// *http.Transport, is copied for the authentication, held to TLS 1.3 at least: below it,
// the authentication gives a token the Owner API refuses (403, see the Fleet API).
func NewClient(h Hosts, hc *http.Client) *Client {
	c := &Client{api: strings.TrimRight(h.API, "/"), auth: strings.TrimRight(h.Auth, "/"), hc: hc}
	var t *http.Transport
	switch rt := hc.Transport.(type) {
	case nil:
		t = http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert // the standard library's
	case *http.Transport:
		t = rt.Clone()
	default:
		return c
	}
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{} //nolint:gosec // MinVersion set below
	}
	t.TLSClientConfig.MinVersion = tls.VersionTLS13
	auth := *hc
	auth.Transport = t
	c.authHC = &auth
	return c
}

// errAuthTransport is returned by Refresh when the client could not hold the
// authentication to TLS 1.3.
var errAuthTransport = errors.New("refresh: the HTTP client's transport is not an *http.Transport: TLS 1.3 cannot be required")

// Refresh exchanges the refresh token for an access token (RFC 6749 §6), with the Tesla
// app's client and scopes.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if c.authHC == nil {
		return Tokens{}, errAuthTransport
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
		"scope":         {scopes},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.auth+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, fmt.Errorf("refresh: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	raw, err := do(c.authHC, req, "token", tokenCall)
	if err != nil {
		return Tokens{}, err
	}
	var resp struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
		TokenType    string  `json:"token_type"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Tokens{}, fmt.Errorf("refresh: %w", err)
	}
	if resp.AccessToken == "" || resp.ExpiresIn <= 0 || !strings.EqualFold(resp.TokenType, "Bearer") {
		return Tokens{}, errors.New("refresh: no bearer token with a lifetime in the response")
	}
	t := Tokens{Access: resp.AccessToken, ExpiresIn: time.Duration(resp.ExpiresIn * float64(time.Second)), Refresh: resp.RefreshToken}
	if t.Refresh == "" {
		t.Refresh = refreshToken
	}
	return t, nil
}

// Products lists the account's vehicles: its other products (a Powerwall, solar
// panels), which have no VIN, are left out.
func (c *Client) Products(ctx context.Context, token string) ([]Product, error) {
	raw, err := c.get(ctx, token, "products", "/api/1/products", listCall)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Response []struct {
			// id is beyond float64's exact integers on some vehicles: read as written,
			// id_s first.
			ID       json.Number `json:"id"`
			IDString string      `json:"id_s"`
			VIN      string      `json:"vin"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("products: %w", err)
	}
	out := []Product{}
	for _, p := range resp.Response {
		id := p.IDString
		if id == "" {
			id = p.ID.String()
		}
		if p.VIN != "" && id != "" {
			out = append(out, Product{ID: id, VIN: p.VIN})
		}
	}
	return out, nil
}

// Vehicle returns the raw response of /vehicles/{id}: the vehicle's state, which does
// not wake the car (see State).
func (c *Client) Vehicle(ctx context.Context, token, id string) ([]byte, error) {
	return c.get(ctx, token, string(VehicleState), "/api/1/vehicles/"+url.PathEscape(id), vehicleCall)
}

// VehicleData returns the response of /vehicles/{id}/vehicle_data, with the position,
// raw but for what plays in the car (see withoutMedia). A car asleep or offline answers
// 408 (KindUnavailable), without being woken.
func (c *Client) VehicleData(ctx context.Context, token, id string) ([]byte, error) {
	q := url.Values{"endpoints": {vehicleDataEndpoints}}
	raw, err := c.get(ctx, token, string(VehicleData), "/api/1/vehicles/"+url.PathEscape(id)+"/vehicle_data?"+q.Encode(), vehicleCall)
	if err != nil {
		return nil, err
	}
	return withoutMedia(raw)
}

// withoutMedia removes vehicle_state.media_info: the title, artist and album playing are
// none of a logger's business, and must not be stored nor exported. The rest is kept as
// written, but for its spacing.
func withoutMedia(raw []byte) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("vehicle_data: %w", err)
	}
	// A response null or not an object, without vehicle_state: nothing plays.
	resp := object(top["response"])
	state := object(resp["vehicle_state"])
	if _, ok := state["media_info"]; !ok {
		return raw, nil
	}
	delete(state, "media_info")
	var err error
	if resp["vehicle_state"], err = json.Marshal(state); err != nil {
		return nil, fmt.Errorf("vehicle_data: %w", err)
	}
	if top["response"], err = json.Marshal(resp); err != nil {
		return nil, fmt.Errorf("vehicle_data: %w", err)
	}
	out, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("vehicle_data: %w", err)
	}
	return out, nil
}

// object returns the members of a JSON object; nil for anything else.
func object(raw json.RawMessage) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func (c *Client) get(ctx context.Context, token, name, path string, k call) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return do(c.hc, req, name, k)
}

// do sends req and returns its body when it is a 200 holding JSON.
func do(hc *http.Client, req *http.Request, name string, k call) ([]byte, error) {
	req.Header.Set("Accept", "application/json")
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

// State is what /vehicles/{id} tells of the car.
type State string

// States. A car asleep has been seen reported offline: the two are not told apart.
const (
	Online  State = "online"
	Asleep  State = "asleep"
	Offline State = "offline"
)

// StateOf reads the state of a response of /vehicles/{id} (or of vehicle_data, which
// carries it too). An unknown word is returned as is.
func StateOf(raw []byte) (State, error) {
	var v struct {
		Response *struct {
			State string `json:"state"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("state: %w", err)
	}
	if v.Response == nil || v.Response.State == "" {
		return "", errors.New("state: none in the response")
	}
	return State(v.Response.State), nil
}
