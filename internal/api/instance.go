package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Release is a published release of Runsten.
type Release struct {
	Version string // X.Y.Z
	URL     string // its page, with its notes
}

// Releases tells what the last read of the latest release found: when it succeeded
// (zero: not yet), and a release newer than the one running, if any.
type Releases interface {
	Latest() (checkedAt time.Time, update Release, newer bool)
}

// MapTiles are where the instance's maps take their tiles from: raster tiles, a URL
// with {z}, {x} and {y}, or a PMTiles file of vector tiles, its path ending in .pmtiles;
// and the attribution their provider requires, as plain text.
type MapTiles struct {
	URL, Attribution string
}

type instanceJSON struct {
	Version     string             `json:"version" doc:"The release the instance runs, as 0.11.0; dev for a build from the sources."`
	UpdateCheck bool               `json:"update_check" doc:"Whether the instance checks for a newer release (RUNSTEN_UPDATE_CHECK): it reads the latest one on GitHub once a day."`
	CheckedAt   *time.Time         `json:"checked_at" pattern:"Z$" doc:"When the latest release was last read; null: the instance does not check, or no read succeeded yet."`
	Update      null[releaseJSON]  `json:"update" doc:"A newer release than the one running, as last read; null: none, or not known."`
	MapTiles    null[mapTilesJSON] `json:"map_tiles" doc:"The map tiles the instance's web interface draws with (RUNSTEN_WEB_MAP_TILES), for a client that draws maps too; null: the instance has no maps, and a client draws none either. The reader's device fetches the tiles from their provider, which sees its address and the areas shown."`
}

type mapTilesJSON struct {
	URL         string `json:"url" format:"uri-template" doc:"Raster tiles, a URL with {z}, {x} and {y}, as https://tile.openstreetmap.org/{z}/{x}/{y}.png; or a PMTiles file of vector tiles (Protomaps), its path ending in .pmtiles, read by range requests."`
	Attribution string `json:"attribution" doc:"The attribution the tiles' provider requires, as plain text, shown with each map."`
}

type releaseJSON struct {
	Version string `json:"version" doc:"Its version, as 0.12.0."`
	URL     string `json:"url" format:"uri" doc:"Its page, with its notes."`
}

func (s *Server) getInstance(context.Context, *struct{}) (*body[instanceJSON], error) {
	out := instanceJSON{Version: s.Version, UpdateCheck: s.Releases != nil}
	if m := s.Map; m != nil {
		out.MapTiles = known(mapTilesJSON{URL: m.URL, Attribution: m.Attribution})
	}
	if s.Releases != nil {
		at, r, newer := s.Releases.Latest()
		if !at.IsZero() {
			at = at.UTC()
			out.CheckedAt = &at
		}
		if newer {
			out.Update = known(releaseJSON(r))
		}
	}
	return respond(out), nil
}

func (s *Server) registerInstance(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getInstance", Method: http.MethodGet, Path: "/instance", Tags: []string{"instance"},
		Summary:     "The instance's release and maps",
		Description: "The release running, a newer one when the instance checks for it, and the map tiles it draws with. The same for every account.",
	}, "The instance's release and maps.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal,
	}), s.getInstance)
}
