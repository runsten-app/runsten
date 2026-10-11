package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Imports are the trips and charges of a vehicle imported from other applications, by
// source (ADR 0030). What writes them is the import's, not the API's.
type Imports interface {
	// Imports returns the vehicle's imports, by source, sorted.
	Imports(ctx context.Context, accountID, vehicleID string) ([]Import, error)
	// DeleteImport deletes every event of the vehicle imported from source, and its
	// tracks; false when there was none.
	DeleteImport(ctx context.Context, accountID, vehicleID, source string) (bool, error)
}

// Import is what a vehicle holds of one source's events.
type Import struct {
	Source         string
	Trips, Charges int
	StartedFrom    time.Time // the earliest start of its events
	StartedTo      time.Time // the latest start of its events
	LastImportedAt time.Time // the latest import of one of them
}

type importJSON struct {
	Source         string    `json:"source" doc:"The application the events were imported from, in the vocabulary of the events' sources." example:"aximote"`
	Trips          int       `json:"trips" minimum:"0" doc:"The trips imported from it."`
	Charges        int       `json:"charges" minimum:"0" doc:"The charges imported from it."`
	StartedFrom    time.Time `json:"started_from" pattern:"Z$" doc:"The earliest start of its events."`
	StartedTo      time.Time `json:"started_to" pattern:"Z$" doc:"The latest start of its events: before the vehicle's first reading."`
	LastImportedAt time.Time `json:"last_imported_at" pattern:"Z$" doc:"When one of them was last imported."`
}

type importListJSON struct {
	Items []importJSON `json:"items" nullable:"false"`
}

type importInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	Source  string `path:"source" doc:"The application the events were imported from." example:"aximote"`
}

func (s *Server) listImports(ctx context.Context, in *vehicleInput) (*body[importListJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	imports, err := s.Imports.Imports(ctx, sessionFrom(ctx).AccountID, v.ID)
	if err != nil {
		return nil, s.internal("imports not read", err)
	}
	out := importListJSON{Items: []importJSON{}}
	for _, i := range imports {
		out.Items = append(out.Items, importJSON(i))
	}
	return respond(out), nil
}

func (s *Server) deleteImport(ctx context.Context, in *importInput) (*struct{}, error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	notFound := apiError(http.StatusNotFound, codeNotFound, "no such import")
	if !sourcePattern.MatchString(in.Source) {
		return nil, notFound
	}
	found, err := s.Imports.DeleteImport(ctx, sessionFrom(ctx).AccountID, v.ID, in.Source)
	switch {
	case err != nil:
		return nil, s.internal("import not deleted", err)
	case !found:
		return nil, notFound
	}
	s.Log.Info("import deleted", "account", sessionFrom(ctx).AccountID, "vehicle", v.ID, "source", in.Source)
	return &struct{}{}, nil
}

// registerImports registers the imports of a vehicle.
func (s *Server) registerImports(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listImports", Method: http.MethodGet, Path: "/vehicles/{vehicle}/imports", Tags: []string{"events"},
		Summary: "The imports of a vehicle",
		Description: "The trips and charges imported from other applications, by source. They are listed with the " +
			"derived events (`imported`), and only events that start before the vehicle's first reading are imported: " +
			"the two never overlap.",
	}, "Every import of the vehicle, by source.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.listImports)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteImport", Method: http.MethodDelete, Path: "/vehicles/{vehicle}/imports/{source}", Tags: []string{"events"},
		Summary:       "Delete an import",
		Description:   "Every trip and charge of the vehicle imported from the source, with their tracks. The derived events stay.",
		DefaultStatus: http.StatusNoContent,
	}, "Deleted.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin,
		http.StatusNotFound:            "`not_found`: no such vehicle in the account, or nothing imported from the source.",
		http.StatusInternalServerError: errInternal,
	}), s.deleteImport)
}
