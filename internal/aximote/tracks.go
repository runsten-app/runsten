package aximote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
)

// ErrUnexpectedFormat is returned for an export of tracks in another format than the
// one expected.
var ErrUnexpectedFormat = errors.New("unexpected format")

// Tracks exports the tracks of trips, by trip ID: each a GeoJSON LineString of
// longitude and latitude. A trip without a track is missing.
//
// The format of the export is not described (ADR 0036, B3). Assumption: a GeoJSON
// FeatureCollection whose features name their trip (properties tripId or id, or the
// feature's id). Anything else is ErrUnexpectedFormat; a feature not understood is
// left out.
func (c *Client) Tracks(ctx context.Context, token string, tripIDs []string) (map[string][]byte, error) {
	body, err := json.Marshal(struct {
		TripIDs []string `json:"tripIds"`
	}{tripIDs})
	if err != nil {
		return nil, fmt.Errorf("tracks: %w", err)
	}
	body, err = c.call(ctx, token, "tracks", http.MethodPost, prefix+"/trips/export/geojson", body, maxTracks)
	if err != nil {
		return nil, err
	}
	tracks := parseTracks(body)
	if tracks == nil {
		return nil, fmt.Errorf("tracks: %w", ErrUnexpectedFormat)
	}
	return tracks, nil
}

// parseTracks reads the tracks of an export: a GeoJSON FeatureCollection whose features
// name their trip (properties tripId or id, or the feature's id), each a LineString of
// longitude and latitude. The format is not described (ADR 0036, B3): nil when it is
// not this one; a feature not understood is left out.
func parseTracks(body []byte) map[string][]byte {
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			ID         any `json:"id"`
			Properties struct {
				TripID any `json:"tripId"`
				ID     any `json:"id"`
			} `json:"properties"`
			Geometry *struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if json.Unmarshal(body, &fc) != nil || fc.Type != "FeatureCollection" {
		return nil
	}
	out := map[string][]byte{}
	for _, f := range fc.Features {
		id := ""
		for _, v := range []any{f.Properties.TripID, f.Properties.ID, f.ID} {
			if s, ok := v.(string); ok && s != "" {
				id = s
				break
			}
		}
		g := f.Geometry
		if id == "" || g == nil || g.Type != "LineString" {
			continue
		}
		var coordinates [][]float64
		if json.Unmarshal(g.Coordinates, &coordinates) != nil || len(coordinates) < 2 {
			continue
		}
		line := make([][2]float64, 0, len(coordinates))
		for _, c := range coordinates {
			if len(c) < 2 || math.Abs(c[0]) > 180 || math.Abs(c[1]) > 90 {
				line = nil
				break
			}
			line = append(line, [2]float64{c[0], c[1]})
		}
		if line == nil {
			continue
		}
		track, err := json.Marshal(struct {
			Type        string       `json:"type"`
			Coordinates [][2]float64 `json:"coordinates"`
		}{"LineString", line})
		if err == nil {
			out[id] = track
		}
	}
	return out
}
