package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Release is a published release of Runsten.
type Release struct {
	Version string // X.Y.Z
	URL     string // its page, with its notes
}

// Releases tells whether a release newer than the one running is out, as last read.
type Releases interface {
	Update() (Release, bool)
}

type instanceJSON struct {
	Version     string            `json:"version" doc:"The release the instance runs, as 0.11.0; dev for a build from the sources."`
	UpdateCheck bool              `json:"update_check" doc:"Whether the instance checks for a newer release (RUNSTEN_UPDATE_CHECK): it reads the latest one on GitHub once a day."`
	Update      null[releaseJSON] `json:"update" doc:"A newer release than the one running, when the instance checks and one is out; null otherwise."`
}

type releaseJSON struct {
	Version string `json:"version" doc:"Its version, as 0.12.0."`
	URL     string `json:"url" format:"uri" doc:"Its page, with its notes."`
}

func (s *Server) getInstance(context.Context, *struct{}) (*body[instanceJSON], error) {
	out := instanceJSON{Version: s.Version, UpdateCheck: s.Releases != nil}
	if s.Releases != nil {
		if r, ok := s.Releases.Update(); ok {
			out.Update = known(releaseJSON(r))
		}
	}
	return respond(out), nil
}

func (s *Server) registerInstance(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getInstance", Method: http.MethodGet, Path: "/instance", Tags: []string{"instance"},
		Summary:     "The release the instance runs",
		Description: "The release running, and a newer one when the instance checks for it. The same for every account.",
	}, "The instance's release.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal,
	}), s.getInstance)
}
