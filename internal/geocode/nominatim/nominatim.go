// Package nominatim is a reverse geocoder speaking Nominatim's API
// (https://nominatim.org/release-docs/latest/api/Reverse/), which its hosted
// providers speak too (LocationIQ, a Nominatim of one's own). It implements
// geocode.Geocoder.
package nominatim

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

	"runsten/internal/core"
)

// UserAgent identifies Runsten, as Nominatim's usage policy requires.
const UserAgent = "Runsten (+https://github.com/runsten-app/runsten)"

// maxBody bounds a response: an address is a few kilobytes.
const maxBody = 1 << 20

// Client asks a Nominatim for addresses.
type Client struct {
	base *url.URL
	http *http.Client
}

// New checks the URL of the service: http(s), its query kept for every request (a
// provider's key).
func New(base string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("geocoder URL %q: an http(s) URL expected", base)
	}
	return &Client{base: u, http: hc}, nil
}

// response is what Runsten reads of Nominatim's jsonv2 answer.
type response struct {
	Error       string            `json:"error"`
	DisplayName string            `json:"display_name"`
	Address     map[string]string `json:"address"`
}

// Reverse implements geocode.Geocoder: the street and the town of the position, empty
// when Nominatim knows none (at sea). Errors never tell the position.
func (c *Client) Reverse(ctx context.Context, p core.Position) (string, error) {
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/reverse"
	q := u.Query()
	q.Set("format", "jsonv2")
	q.Set("lat", strconv.FormatFloat(p.Lat, 'f', -1, 64))
	q.Set("lon", strconv.FormatFloat(p.Lon, 'f', -1, 64))
	q.Set("zoom", "17") // major and minor streets
	q.Set("addressdetails", "1")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return "", errors.New("reverse geocoding: building the request")
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// A *url.Error holds the URL, hence the position: only its cause goes on.
		if ue := (*url.Error)(nil); errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("reverse geocoding: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reverse geocoding: status %d", resp.StatusCode)
	}
	var r response
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&r); err != nil {
		return "", fmt.Errorf("reverse geocoding: reading the response: %w", err)
	}
	if r.Error != "" {
		return "", nil // "Unable to geocode": nothing there
	}
	return format(r), nil
}

// format is the street and the town: no house number, which a position read within
// meters cannot tell, and whose place before or after the street depends on the country.
func format(r response) string {
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(r.Address[k]); v != "" {
				return v
			}
		}
		return ""
	}
	parts := []string{}
	for _, v := range []string{
		first("road", "pedestrian", "footway", "square", "place", "neighbourhood", "quarter", "suburb", "hamlet"),
		first("city", "town", "village", "municipality", "county"),
	} {
		if v != "" && (len(parts) == 0 || parts[len(parts)-1] != v) {
			parts = append(parts, v)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	// Without address details: the first two parts of the full name.
	names := strings.Split(r.DisplayName, ",")
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	return strings.Join(names[:min(2, len(names))], ", ")
}
