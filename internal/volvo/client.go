// Package volvo is the Volvo Cars API client (Connected Vehicle v2, Energy v2,
// Location v1). It returns raw responses: the collector stores them as is and only
// reads the few values it needs for polling.
package volvo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// DefaultBaseURL is the address of the real API.
const DefaultBaseURL = "https://api.volvocars.com"

// maxBody caps the size of a response read (observed responses are under 4 KiB).
const maxBody = 1 << 20

// Endpoint identifies a resource polled for a vehicle.
type Endpoint string

// Endpoints polled by the collector.
const (
	Details      Endpoint = "details"
	EngineStatus Endpoint = "engine-status"
	Odometer     Endpoint = "odometer"
	Statistics   Endpoint = "statistics"
	Diagnostics  Endpoint = "diagnostics"
	Brakes       Endpoint = "brakes"
	Engine       Endpoint = "engine"
	Fuel         Endpoint = "fuel"
	Tyres        Endpoint = "tyres"
	Warnings     Endpoint = "warnings"
	Doors        Endpoint = "doors"
	Windows      Endpoint = "windows"
	EnergyState  Endpoint = "energy-state"
	Location     Endpoint = "location"
)

// API names, the units of the Volvo quota (10,000 calls per day and per API).
const (
	APIConnectedVehicle = "connected-vehicle"
	APIEnergy           = "energy"
	APILocation         = "location"
)

// API returns the API the endpoint belongs to.
func (e Endpoint) API() string {
	switch e {
	case EnergyState:
		return APIEnergy
	case Location:
		return APILocation
	default:
		return APIConnectedVehicle
	}
}

func (e Endpoint) path(vin string) string {
	v := url.PathEscape(vin)
	switch e {
	case Details:
		return "/connected-vehicle/v2/vehicles/" + v
	case EnergyState:
		return "/energy/v2/vehicles/" + v + "/state"
	case Location:
		return "/location/v1/vehicles/" + v + "/location"
	default:
		return "/connected-vehicle/v2/vehicles/" + v + "/" + string(e)
	}
}

// Client calls the API with an application key and a user's token. The quota is
// counted on the key's application, not on the OAuth client that issued the token: a
// key of one application is accepted with a token of another (tried on the real API).
type Client struct {
	base   string
	apiKey string // the instance's key; empty: none, each call names its own
	hc     *http.Client
}

// ErrNoKey is returned, without calling the API, when neither the call nor the client
// has an application key.
var ErrNoKey = errors.New("no application key (vcc-api-key)")

// NewClient creates a client. apiKey is the instance's application key, used by the
// calls that name none; empty: every call must name its own. hc must have a timeout.
func NewClient(baseURL, apiKey string, hc *http.Client) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), apiKey: apiKey, hc: hc}
}

// HasKey reports whether the client has the instance's application key.
func (c *Client) HasKey() bool { return c.apiKey != "" }

// Vehicles lists the VINs accessible with token, with the application key key (empty:
// the instance's).
func (c *Client) Vehicles(ctx context.Context, key, token string) ([]string, error) {
	body, err := c.get(ctx, key, token, "vehicles", "/connected-vehicle/v2/vehicles")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []struct {
			VIN string `json:"vin"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("list vehicles: %w", err)
	}
	vins := make([]string, 0, len(resp.Data))
	for _, d := range resp.Data {
		vins = append(vins, d.VIN)
	}
	return vins, nil
}

// Fetch returns the raw response of the endpoint for vehicle vin, with the application
// key key (empty: the instance's).
func (c *Client) Fetch(ctx context.Context, key, token, vin string, ep Endpoint) ([]byte, error) {
	return c.get(ctx, key, token, string(ep), ep.path(vin))
}

func (c *Client) get(ctx context.Context, key, token, name, path string) ([]byte, error) {
	if key == "" {
		key = c.apiKey
	}
	if key == "" {
		return nil, fmt.Errorf("%s: %w", name, ErrNoKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("vcc-api-key", key)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError(name, resp, body)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%s: invalid JSON response", name)
	}
	return body, nil
}
