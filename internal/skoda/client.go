// Package skoda is the client of the MyŠkoda Public API, the access Škoda opened to its
// owners: a key the owner creates in the MyŠkoda app, bound to the vehicles they ticked,
// sent in the X-API-Key header, with no password and no developer account. It reads one
// endpoint, GET /api/v1/vehicles/{vin}, which returns every part of the vehicle's state
// at once, and returns the raw response: the collector stores it as is, and Normalizer
// reads it into snapshots.
//
// The API has no endpoint listing a key's vehicles: the owner gives the VIN of each car
// with the key. Keys expire, which every successful response tells (X-API-Key-Expires-At),
// and the calls are limited per VIN (20 an hour, "not final": the RateLimit-* headers are
// the authority, a 5xx counts, a 401 or a 403 does not). Read only: the API's charging
// and climate commands are never called.
package skoda

import (
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
const DefaultBaseURL = "https://public.api.connect.skoda-auto.cz"

// maxBody caps the size of a response read (a full response is a few KiB).
const maxBody = 1 << 20

// Parts are the parts of the vehicle's state the collector reads, as the API's include
// parameter names them: info covers the name, the licence plate and the picture. The
// others (fuelStatus, auxiliaryHeating, activeVentilation, chargingProfiles, operations)
// serve neither the trips, the charges nor the state of an electric car.
func Parts() []string {
	return []string{"info", "status", "odometer", "parkingPosition", "airConditioning", "charging"}
}

// RateLimit is what the RateLimit-* headers tell of the VIN's quota. Known is false when
// the response carries none of them.
type RateLimit struct {
	Limit, Remaining int
	// Reset is how long until the quota replenishes, from the response.
	Reset time.Duration
	Known bool
}

// Response is a read of a vehicle: its raw body, the key's expiry, and the VIN's quota.
type Response struct {
	Body []byte
	// ExpiresAt is when the key expires (X-API-Key-Expires-At); zero when not told.
	ExpiresAt time.Time
	Limit     RateLimit
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

// Vehicle reads the vehicle vin with the key, restricted to the parts given (every part
// the vehicle supports when none). A 200 may be partial: the parts that could not be read
// are listed in its errors, which Normalize reads as unknown.
func (c *Client) Vehicle(ctx context.Context, key, vin string, parts ...string) (Response, error) {
	u := c.base + "/api/v1/vehicles/" + url.PathEscape(vin)
	if len(parts) > 0 {
		u += "?" + url.Values{"include": {strings.Join(parts, ",")}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return Response{}, fmt.Errorf("vehicle: %w", err)
	}
	req.Header.Set("X-API-Key", key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		// The request's URL holds the VIN: the logs carry the error.
		var ue *url.Error
		if errors.As(err, &ue) {
			ue.URL = maskVINs(ue.URL)
		}
		return Response{}, fmt.Errorf("vehicle: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Response{}, fmt.Errorf("vehicle: read: %w", err)
	}
	limit := rateLimitOf(resp.Header)
	if resp.StatusCode != http.StatusOK {
		return Response{}, newAPIError(resp, body, limit)
	}
	if !json.Valid(body) {
		return Response{}, fmt.Errorf("vehicle: invalid JSON response")
	}
	return Response{Body: body, ExpiresAt: expiresAt(resp.Header), Limit: limit}, nil
}

// expiresAt reads X-API-Key-Expires-At: an ISO 8601 time (assumption: the docs give no
// example; RFC 3339, and the HTTP date format, are read). Zero when unreadable.
func expiresAt(h http.Header) time.Time {
	v := strings.TrimSpace(h.Get("X-API-Key-Expires-At"))
	if v == "" {
		return time.Time{}
	}
	if at, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return at.UTC()
	}
	if at, err := http.ParseTime(v); err == nil {
		return at.UTC()
	}
	return time.Time{}
}

// rateLimitOf reads the RateLimit-* headers: integers, Reset in seconds (the docs').
func rateLimitOf(h http.Header) RateLimit {
	var l RateLimit
	atoi := func(name string) (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(h.Get(name)))
		return n, err == nil && n >= 0
	}
	limit, okL := atoi("RateLimit-Limit")
	remaining, okR := atoi("RateLimit-Remaining")
	reset, okS := atoi("RateLimit-Reset")
	if !okR {
		return l
	}
	l.Remaining, l.Known = remaining, true
	if okL {
		l.Limit = limit
	}
	if okS {
		l.Reset = time.Duration(reset) * time.Second
	}
	return l
}
