// Package aximote is the client of Aximote's public API (v1), from its specification
// (https://api.aximote.com/v3/api-docs/public-v1, read 2026-10-11): the vehicles, trips
// and refuels of the account of a personal access token, and the tracks of its trips.
// It returns the API's values as they are; internal/importer converts them. It makes
// one call per method, never a retry: its caller decides.
package aximote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the address of the real API.
const DefaultBaseURL = "https://api.aximote.com"

// prefix is the path of the public API.
const prefix = "/api/public/v1"

// Caps on what a response may weigh. A page of trips or refuels is small; the tracks of
// a page of trips are as long as the trips. Assumption: no size is documented.
const (
	maxBody   = 4 << 20
	maxTracks = 32 << 20
)

// ErrTooLarge is returned for a response over its cap.
var ErrTooLarge = errors.New("response too large")

// Client calls the API with a user's personal access token, given to each call: the
// client keeps none.
type Client struct {
	base string
	hc   *http.Client
}

// NewClient creates a client. hc must have a timeout.
func NewClient(baseURL string, hc *http.Client) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), hc: hc}
}

// Vehicle is a vehicle of the account. Every field but the ID may be missing.
type Vehicle struct {
	ID                 string   `json:"id"`
	Name               *string  `json:"name"`
	Make               *string  `json:"make"`
	Model              *string  `json:"model"`
	Year               *int     `json:"year"`
	VIN                *string  `json:"vin"`
	BatteryCapacityKWh *float64 `json:"batteryCapacityKwh"`
}

// Point is a place of a trip or a refuel.
type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Country   *string `json:"country"`
	Locality  *string `json:"locality"`
}

// Trip is a trip as Aximote cut it: neither state of charge nor odometer.
type Trip struct {
	ID                string     `json:"id"`
	VehicleID         string     `json:"vehicleId"`
	StartTime         time.Time  `json:"startTime"`
	EndTime           *time.Time `json:"endTime"`
	DistanceKm        *float64   `json:"distanceKm"`
	EnergyConsumedKWh *float64   `json:"energyConsumedKwh"`
	StartLocation     *Point     `json:"startLocation"`
	EndLocation       *Point     `json:"endLocation"`
}

// Refuel types.
const (
	RefuelFuel   = "FUEL"
	RefuelCharge = "CHARGE"
)

// Refuel is a fuel stop or a charge (Type), without its AC or DC type.
type Refuel struct {
	ID               string     `json:"id"`
	VehicleID        string     `json:"vehicleId"`
	Type             string     `json:"type"`
	StartTime        time.Time  `json:"startTime"`
	EndTime          *time.Time `json:"endTime"`
	IsActive         bool       `json:"isActive"`
	StartEnergyKWh   *float64   `json:"startEnergyKwh"`
	EndEnergyKWh     *float64   `json:"endEnergyKwh"`
	EnergyChargedKWh *float64   `json:"energyChargedKwh"`
	AvgChargingKW    *float64   `json:"avgChargingKw"`
	CostAmount       *float64   `json:"costAmount"` // in units of CostCurrency, not minor units
	CostCurrency     *string    `json:"costCurrency"`
	Location         *Point     `json:"location"`
}

// Page is a page of a list; NextCursor is empty after the last one.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// Query selects a page of a vehicle's trips or refuels.
type Query struct {
	VehicleID string
	Until     time.Time // zero: no bound
	Cursor    string    // empty: the first page
	Limit     int       // 0: the API's default
}

// Vehicles lists the vehicles of the token's account.
func (c *Client) Vehicles(ctx context.Context, token string) ([]Vehicle, error) {
	var out []Vehicle
	if err := c.getJSON(ctx, token, "vehicles", prefix+"/vehicles", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Vehicle reads one vehicle of the token's account.
func (c *Client) Vehicle(ctx context.Context, token, id string) (Vehicle, error) {
	var out Vehicle
	err := c.getJSON(ctx, token, "vehicle", prefix+"/vehicles/"+url.PathEscape(id), &out)
	return out, err
}

// Trips reads a page of a vehicle's trips.
func (c *Client) Trips(ctx context.Context, token string, q Query) (Page[Trip], error) {
	return page[Trip](ctx, c, token, "trips", q)
}

// Refuels reads a page of a vehicle's refuels: its fuel stops and its charges.
func (c *Client) Refuels(ctx context.Context, token string, q Query) (Page[Refuel], error) {
	return page[Refuel](ctx, c, token, "refuels", q)
}

func page[T any](ctx context.Context, c *Client, token, name string, q Query) (Page[T], error) {
	v := url.Values{"vehicleId": {q.VehicleID}}
	if !q.Until.IsZero() {
		v.Set("until", q.Until.UTC().Format(time.RFC3339Nano))
	}
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	var resp struct {
		Items      []T     `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := c.getJSON(ctx, token, name, prefix+"/"+name+"?"+v.Encode(), &resp); err != nil {
		return Page[T]{}, err
	}
	p := Page[T]{Items: resp.Items}
	if resp.NextCursor != nil {
		p.NextCursor = *resp.NextCursor
	}
	return p, nil
}

func (c *Client) getJSON(ctx context.Context, token, name, path string, out any) error {
	body, err := c.call(ctx, token, name, http.MethodGet, path, nil, maxBody)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func (c *Client) call(ctx context.Context, token, name, method, path string, body []byte, limit int64) ([]byte, error) {
	var reader io.Reader = http.NoBody
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError(name, resp)
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("%s: read: %w", name, err)
	case int64(len(out)) > limit:
		return nil, fmt.Errorf("%s: %w", name, ErrTooLarge)
	}
	return out, nil
}
