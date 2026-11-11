// Package pleos is the client of the Pleos Vehicle Data API, the access Hyundai, Kia
// and Genesis opened to private owners in Europe (the EU Data Act): a token asked for
// with the owner's key, the vehicles it reads, and the status endpoints a logger needs.
// It returns raw responses: the collector stores them as is, and Normalizer reads them
// into snapshots.
//
// One key reads one brand's vehicles: the brand rides in a header on every call, and
// the token, the brand and the VIN belong together. Only one token lives at a time per
// key (observed, not documented): asking for another has the previous refused, which
// the kinds of the APIError tell so the caller can ask again, and so that a source
// reading with the same key as another client (evcc, Home Assistant) knows it.
package pleos

import (
	"bytes"
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

// DefaultBaseURL is the address of the real API.
const DefaultBaseURL = "https://api.pleos.ai"

// maxBody caps the size of a response read (the doc's examples are under 4 KiB).
const maxBody = 1 << 20

// Brand is one of the three the API reads, as its Brand header names it: the brand of
// the key that reads the vehicles. A connection of the collector's goes by the
// provider of the same name, pleos_hyundai and its siblings, one row per brand.
type Brand string

// The brands the API reads.
const (
	Hyundai Brand = "hyundai"
	Kia     Brand = "kia"
	Genesis Brand = "genesis"
)

// Brands lists the brands the API reads.
func Brands() []Brand { return []Brand{Hyundai, Kia, Genesis} }

// Provider is the connection provider of a brand's key: the same name, pleos_ prefixed.
func (b Brand) Provider() string { return "pleos_" + string(b) }

// SourceOf is the source of the readings the brand's key brings: the one source of the
// three brands (pleos), kept at their normalization.
func (b Brand) SourceOf() string { return Source }

// BrandOf returns the brand of a connection provider (pleos_kia → kia).
func BrandOf(provider string) (Brand, bool) {
	name, ok := strings.CutPrefix(provider, "pleos_")
	if !ok {
		return "", false
	}
	for _, b := range Brands() {
		if b == Brand(name) {
			return b, true
		}
	}
	return "", false
}

// Endpoint is a status resource read for a vehicle, named as the API names it.
type Endpoint string

// The status endpoints a logger reads, and the vehicles' list. The doc lists the
// others (freezer, the histories of trips, their data): they serve neither the trips,
// the charges, the state nor the health of a vehicle.
const (
	// Consent lists the key's vehicles. It is no vehicle's state: read by runsten-api
	// when an owner gives a key, and at the source's details pace so a vehicle's model
	// stays known.
	Consent Endpoint = "consent"
	// Batteries tells the charge: the state of charge, the plug, the target, the time
	// left. It has no power, current nor voltage: a charge has the energy of its state
	// of charge only, as a reconstructed one.
	Batteries Endpoint = "batteries"
	// Powertrains tells the range, of the battery alone on an electric.
	Powertrains Endpoint = "powertrains"
	// Driving tells the odometer and, on a car that sleeps, the sleep mode.
	Driving Endpoint = "driving"
	// Locations tells the position, and its coordinate system: only WGS 84 is read.
	Locations Endpoint = "locations"
	// Status tells the doors, the tyres, the warnings and the climate.
	Status Endpoint = "status"
)

// Endpoints lists the status endpoints the collector reads, in its pass order.
func Endpoints() []Endpoint {
	return []Endpoint{Driving, Batteries, Powertrains, Locations, Status}
}

// Credential is the key the owner creates in the Pleos Playground: a client ID and its
// secret, one key per brand. The access token is never stored: it is asked for with
// the key, and kept in memory until it ends.
type Credential struct {
	ClientID, ClientSecret string
}

// AccessToken is a token of POST /v1/auth/personal-token. The response carries no
// expiry: the token (a JWT) tells it in its claims. There is no refresh token.
type AccessToken struct {
	Value string
	// ExpiresAt is when the JWT says the token ends, read without verifying its
	// signature (Pleos does not publish its key, and the signature serves nothing
	// here: the claim gives the lifetime alone). Zero if unreadable.
	ExpiresAt time.Time
}

// ttls is what the JWT's exp claim is read as, whatever its form.
type ttls struct {
	Exp json.Number `json:"exp"`
}

// Client calls the API. Each method makes one request, and never retries: the caller
// knows the quota and what an error means for the connection.
type Client struct {
	base string
	hc   *http.Client
}

// NewClient creates a client of the API at baseURL. hc must have a timeout.
func NewClient(baseURL string, hc *http.Client) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), hc: hc}
}

// ErrUnknownEndpoint is returned, without calling the API, for an endpoint the client
// does not read.
var ErrUnknownEndpoint = errors.New("unknown endpoint")

// Token asks for an access token with the key, for the brand the key reads. The API
// gives no expiry: the JWT's claim tells when it ends, and another token asked for with
// the same key ends this one before its time.
func (c *Client) Token(ctx context.Context, brand Brand, cred Credential) (AccessToken, error) {
	body, err := json.Marshal(map[string]string{"clientId": cred.ClientID, "clientSecret": cred.ClientSecret})
	if err != nil {
		return AccessToken{}, fmt.Errorf("token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/auth/personal-token", bytes.NewReader(body))
	if err != nil {
		return AccessToken{}, fmt.Errorf("token: %w", err)
	}
	req.Header.Set("Brand", string(brand))
	raw, err := c.do(req, "token", tokenCall)
	if err != nil {
		return AccessToken{}, err
	}
	var resp struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return AccessToken{}, fmt.Errorf("token: %w", err)
	}
	if resp.Data.AccessToken == "" {
		return AccessToken{}, errors.New("token: no access token in the response")
	}
	return AccessToken{Value: resp.Data.AccessToken, ExpiresAt: expOf(resp.Data.AccessToken)}, nil
}

// Vehicles lists the VINs the key reads, those of its brand. A list is fixed when the
// key is issued: vehicles registered later need a new key, and one of another brand is
// not read with this key.
func (c *Client) Vehicles(ctx context.Context, tok string, brand Brand) ([]string, error) {
	raw, err := c.Consent(ctx, tok, brand)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Vehicles []struct {
				VIN string `json:"vin"`
			} `json:"vehicles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("list vehicles: %w", err)
	}
	if resp.Data.Vehicles == nil {
		resp.Data.Vehicles = []struct {
			VIN string `json:"vin"`
		}{} // none: an empty list is told too
	}
	vins := make([]string, 0, len(resp.Data.Vehicles))
	for _, v := range resp.Data.Vehicles {
		// An entry without a VIN names no vehicle to read.
		if vinOf(v.VIN) {
			vins = append(vins, v.VIN)
		}
	}
	return vins, nil
}

// Consent returns the raw response of the key's vehicles, with every vehicle's entry
// (vin, nickname, name, type, sellname): the caller — the collector's Source, which a
// snapshot of its own is kept for each vehicle — keeps the entry of the vehicle it
// reads, and runsten-api lists the VINs of the whole.
func (c *Client) Consent(ctx context.Context, tok string, brand Brand) ([]byte, error) {
	return c.get(ctx, tok, brand, "/v1/vehicles/consent", "consent")
}

// VehicleOfConsent returns consent's response with the entry of vehicle vin alone:
// the response lists every vehicle the key reads, and each vehicle's own snapshot is
// kept from its own entry (the normalizer reads no vehicle's identity).
func VehicleOfConsent(raw []byte, vin string) ([]byte, error) {
	var v struct {
		Data struct {
			Vehicles []map[string]any `json:"vehicles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("vehicles: %w", err)
	}
	kept := make([]map[string]any, 0, 1)
	for _, m := range v.Data.Vehicles {
		if s, _ := m["vin"].(string); s == vin {
			kept = append(kept, m)
		}
	}
	v.Data.Vehicles = kept
	out, err := json.Marshal(map[string]any{"data": v.Data})
	if err != nil {
		return nil, fmt.Errorf("vehicles: %w", err)
	}
	return out, nil
}

// Fetch returns the raw response of the endpoint for vehicle vin.
func (c *Client) Fetch(ctx context.Context, tok string, brand Brand, vin string, ep Endpoint) ([]byte, error) {
	if ep == Consent {
		return nil, fmt.Errorf("%s: %w", ep, ErrUnknownEndpoint)
	}
	return c.get(ctx, tok, brand, "/v1/vehicles/"+url.PathEscape(vin)+"/"+string(ep), string(ep))
}

func (c *Client) get(ctx context.Context, tok string, brand Brand, path, name string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Brand", string(brand))
	return c.do(req, name, dataCall)
}

// do sends req and returns its body when it is a 200 holding JSON.
func (c *Client) do(req *http.Request, name string, k call) ([]byte, error) {
	req.Header.Set("Accept", "application/json")
	if req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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

// expOf reads when a token ends, in its JWT's claims, without verifying the signature
// (it is read for the lifetime alone). Zero when there is none.
func expOf(tok string) time.Time {
	_, rest, _ := strings.Cut(tok, ".")
	claims, _, _ := strings.Cut(rest, ".")
	raw, err := base64.RawURLEncoding.DecodeString(claims)
	if err != nil {
		return time.Time{}
	}
	var e ttls
	if json.Unmarshal(raw, &e) != nil {
		return time.Time{}
	}
	sec, err := e.Exp.Float64()
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(sec), 0).UTC()
}
