package maptiles

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestFromEnv(t *testing.T) {
	const raster = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	for name, tt := range map[string]struct {
		env    map[string]string
		want   *Tiles
		origin string
		vector bool
	}{
		"none":            {env: map[string]string{"RUNSTEN_WEB_MAP_ATTRIBUTION": "© Someone"}},
		"raster":          {env: map[string]string{"RUNSTEN_WEB_MAP_TILES": raster}, want: &Tiles{raster, DefaultAttribution}, origin: "https://tile.openstreetmap.org"},
		"its attribution": {env: map[string]string{"RUNSTEN_WEB_MAP_TILES": raster, "RUNSTEN_WEB_MAP_ATTRIBUTION": "© Someone"}, want: &Tiles{raster, "© Someone"}, origin: "https://tile.openstreetmap.org"},
		"pmtiles": {
			env:  map[string]string{"RUNSTEN_WEB_MAP_TILES": "http://maps.example:8080/europe.pmtiles"},
			want: &Tiles{"http://maps.example:8080/europe.pmtiles", DefaultAttribution}, origin: "http://maps.example:8080", vector: true,
		},
	} {
		got, err := FromEnv(env(tt.env))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if (got == nil) != (tt.want == nil) || got != nil && *got != *tt.want {
			t.Errorf("%s: %+v, want %+v", name, got, tt.want)
		}
		if got == nil {
			continue
		}
		if o, err := got.Origin(); err != nil || o != tt.origin || got.Vector() != tt.vector {
			t.Errorf("%s: origin %q, %v; vector %v", name, o, err, got.Vector())
		}
	}
}

func TestFromEnvRefuses(t *testing.T) {
	for name, url := range map[string]string{
		"pmtiles without scheme": "maps.example/europe.pmtiles",
		"pmtiles in the query":   "https://maps.example/tiles?file=europe.pmtiles",
		"no z":                   "https://tiles.example/{x}/{y}.png",
		"no scheme":              "tiles.example/{z}/{x}/{y}.png",
		"other scheme":           "ftp://tiles.example/{z}/{x}/{y}.png",
		"subdomains":             "https://{s}.tiles.example/{z}/{x}/{y}.png",
	} {
		if got, err := FromEnv(env(map[string]string{"RUNSTEN_WEB_MAP_TILES": url})); err == nil || got != nil ||
			!strings.Contains(err.Error(), "RUNSTEN_WEB_MAP_TILES") {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
	if _, err := (Tiles{URL: "https://tiles.example/{z}/{x}/{y}.png"}).Origin(); err == nil {
		t.Error("no attribution: no error")
	}
}
