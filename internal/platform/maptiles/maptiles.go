// Package maptiles reads the instance's map tiles from its environment. runsten-web
// writes them into its page and allows their origin in its CSP; runsten-api tells them
// to the API's other clients (GET /instance). Both read the same variables through
// FromEnv, so that the web interface and the API never disagree.
package maptiles

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultAttribution is what every provider of OpenStreetMap's data requires.
const DefaultAttribution = "© OpenStreetMap contributors"

// Tiles are where the maps' tiles come from, and the attribution their provider
// requires, as plain text. The URL is either raster tiles, with {z}, {x} and {y}, as
// https://tile.openstreetmap.org/{z}/{x}/{y}.png, or a PMTiles file of vector tiles
// (Protomaps), its path ending in .pmtiles. The reader's browser or app fetches them
// itself: the host learns the reader's address and the areas they look at.
type Tiles struct {
	URL, Attribution string
}

// FromEnv reads RUNSTEN_WEB_MAP_TILES and RUNSTEN_WEB_MAP_ATTRIBUTION (by default
// DefaultAttribution), and checks them. nil without tiles: the instance has no maps.
func FromEnv(getenv func(string) string) (*Tiles, error) {
	v := getenv("RUNSTEN_WEB_MAP_TILES")
	if v == "" {
		return nil, nil
	}
	t := &Tiles{URL: v, Attribution: getenv("RUNSTEN_WEB_MAP_ATTRIBUTION")}
	if t.Attribution == "" {
		t.Attribution = DefaultAttribution
	}
	if _, err := t.Origin(); err != nil {
		return nil, fmt.Errorf("RUNSTEN_WEB_MAP_TILES: %w", err)
	}
	return t, nil
}

// Vector tells whether the tiles are a PMTiles file.
func (t Tiles) Vector() bool {
	u, err := url.Parse(t.URL)
	return err == nil && strings.HasSuffix(u.Path, ".pmtiles")
}

// Origin checks the URL of the tiles and gives their origin, for a CSP.
func (t Tiles) Origin() (string, error) {
	if !t.Vector() {
		for _, p := range []string{"{z}", "{x}", "{y}"} {
			if !strings.Contains(t.URL, p) {
				return "", fmt.Errorf("map tiles %q: no %s, nor a .pmtiles file", t.URL, p)
			}
		}
	}
	u, err := url.Parse(strings.NewReplacer("{z}", "0", "{x}", "0", "{y}", "0").Replace(t.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(u.Host, "{}") {
		return "", fmt.Errorf("map tiles %q: an http(s) URL with {z}, {x} and {y}, or of a .pmtiles file, expected, its host fixed", t.URL)
	}
	if t.Attribution == "" {
		return "", fmt.Errorf("map tiles %q: an attribution is required", t.URL)
	}
	return u.Scheme + "://" + u.Host, nil
}
