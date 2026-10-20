// Package polestar is the client of the Polestar Data Portal's M2M API (EU Data Act):
// a token for the owner's credential, the VINs it reads, and the domains a logger needs.
// It returns raw responses: the collector stores them as is, and Normalizer reads them
// into snapshots.
package polestar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the address of the real API.
const DefaultBaseURL = "https://pc-api.polestar.com/eu-north-1/data-portal/m2m"

// maxBody caps the size of a response read (observed responses are under 4 KiB).
const maxBody = 1 << 20

// Domain is a resource read for a vehicle, named as the API names it.
type Domain string

// Domains read: the others (doors, health, climatisation, timers) serve neither trips
// nor charges, and would only spend the quota.
const (
	Battery      Domain = "battery"
	Odometer     Domain = "odometer"
	Location     Domain = "location"
	Availability Domain = "availability"
	TargetSoC    Domain = "target-soc"
)

// Domains returns every domain the client reads.
func Domains() []Domain { return []Domain{Battery, Availability, Odometer, Location, TargetSoC} }

// section is the part of /v1/vehicles/{vin}/ the domain is served under; empty for a
// domain the client does not read.
func (d Domain) section() string {
	switch d {
	case Battery, Odometer, Location, Availability:
		return "telemetry"
	case TargetSoC:
		return "charging"
	}
	return ""
}

// Credential is what the owner creates in the Data Portal: an OAuth client, and the
// account it reads, whose ID goes in the x-client-id header of every call.
type Credential struct {
	ClientID, ClientSecret, AccountID string
}

// AccessToken is a token of POST /token. There is no refresh token: a new one is asked
// with the credential.
type AccessToken struct {
	Value     string
	ExpiresIn time.Duration
}

// Client calls the API. Each method makes one request, and never retries: the caller
// knows the quota and what an error means for the connection.
type Client struct {
	base string
	hc   *http.Client
}

// NewClient creates a client. hc must have a timeout.
func NewClient(baseURL string, hc *http.Client) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), hc: hc}
}

// Token asks for an access token with the credential's client ID and secret. The API
// takes them in a JSON body, without grant_type: it is not RFC 6749's client
// credentials grant to the letter. Without a scope, the token reads every domain the
// credential was granted.
func (c *Client) Token(ctx context.Context, cred Credential) (AccessToken, error) {
	body, err := json.Marshal(map[string]string{"clientId": cred.ClientID, "clientSecret": cred.ClientSecret})
	if err != nil {
		return AccessToken{}, fmt.Errorf("token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/token", bytes.NewReader(body))
	if err != nil {
		return AccessToken{}, fmt.Errorf("token: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	raw, err := c.do(req, "token", tokenCall)
	if err != nil {
		return AccessToken{}, err
	}
	var resp struct {
		AccessToken string  `json:"accessToken"`
		ExpiresIn   float64 `json:"expiresIn"`
		TokenType   string  `json:"tokenType"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return AccessToken{}, fmt.Errorf("token: %w", err)
	}
	if resp.AccessToken == "" || resp.ExpiresIn <= 0 || !strings.EqualFold(resp.TokenType, "Bearer") {
		return AccessToken{}, errors.New("token: no bearer token with a lifetime in the response")
	}
	return AccessToken{Value: resp.AccessToken, ExpiresIn: time.Duration(resp.ExpiresIn * float64(time.Second))}, nil
}

// Vehicles lists the VINs the credential reads, with its account ID. The API gives the
// VIN alone: no model, year nor name.
func (c *Client) Vehicles(ctx context.Context, token, accountID string) ([]string, error) {
	raw, err := c.get(ctx, token, accountID, "vehicles", "/v1/vehicles", listCall)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("list vehicles: %w", err)
	}
	if resp.Data == nil {
		resp.Data = []string{} // protobuf's JSON leaves an empty list out
	}
	return resp.Data, nil
}

// ErrUnknownDomain is returned, without calling the API, for a domain the client does
// not read.
var ErrUnknownDomain = errors.New("unknown domain")

// Fetch returns the raw response of the domain for vehicle vin.
func (c *Client) Fetch(ctx context.Context, token, accountID, vin string, d Domain) ([]byte, error) {
	section := d.section()
	if section == "" {
		return nil, fmt.Errorf("%s: %w", d, ErrUnknownDomain)
	}
	return c.get(ctx, token, accountID, string(d), "/v1/vehicles/"+url.PathEscape(vin)+"/"+section+"/"+string(d), domainCall)
}

func (c *Client) get(ctx context.Context, token, accountID, name, path string, k call) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-client-id", accountID)
	return c.do(req, name, k)
}

// do sends req and returns its body when it is a 200 holding JSON.
func (c *Client) do(req *http.Request, name string, k call) ([]byte, error) {
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
		return nil, newAPIError(name, k, resp, body)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%s: invalid JSON response", name)
	}
	return body, nil
}
