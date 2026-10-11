package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// ImportJobs are the imports of a vehicle's history from another application, run by
// the import worker (ADR 0036): the API creates them and reads how they stand.
type ImportJobs interface {
	// CreateImportJob creates a job, its token sealed; ErrImportActive when the vehicle
	// has one pending or running.
	CreateImportJob(ctx context.Context, accountID, vehicleID string, req ImportRequest, at time.Time) (ImportJob, error)
	// LatestImportJob is the vehicle's latest job; false when it has none.
	LatestImportJob(ctx context.Context, accountID, vehicleID string) (ImportJob, bool, error)
}

// ErrImportActive is returned for a job of a vehicle that has one pending or running.
var ErrImportActive = errors.New("an import of the vehicle is already active")

// ImportRequest is what a job imports: a vehicle of a source, read with a token.
type ImportRequest struct {
	Source, SourceVehicle, Token string
}

// Import job statuses.
const (
	ImportPending = "pending"
	ImportRunning = "running"
	ImportDone    = "done"
	ImportFailed  = "failed"
)

// ImportJob is how a job stands: never its token.
type ImportJob struct {
	ID, Source, SourceVehicle  string
	Status                     string // ImportPending, ImportRunning, ImportDone or ImportFailed
	Step                       string // trips, then charges
	Imported, Skipped, Invalid int
	Cutoff                     time.Time // zero before the first page
	Error                      string    // a kind, when failed
	CreatedAt                  time.Time
	NotBefore                  time.Time // waits until then: the source's limit, or a retry
	EndedAt                    time.Time // zero while active
}

// AximoteVehicles lists the vehicles of an Aximote account, with its personal access
// token. An error whose TokenRefused method reports true is Aximote's refusal of the
// token.
type AximoteVehicles interface {
	AximoteVehicles(ctx context.Context, token string) ([]AximoteVehicle, error)
}

// AximoteVehicle is a vehicle of an Aximote account; every field but the ID may be
// empty.
type AximoteVehicle struct {
	ID, Name, Make, Model, VIN string
	Year                       int
}

type importStatusJSON string

// Schema lists the statuses.
func (importStatusJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema[importStatusJSON](ImportPending, ImportRunning, ImportDone, ImportFailed)
}

type importStepJSON string

// Schema lists the steps.
func (importStepJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema[importStepJSON]("trips", "charges")
}

type importFailureJSON string

// Schema lists the kinds of failure, as the import worker writes them.
func (importFailureJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema[importFailureJSON]("token_refused", "source_failed", "internal")
}

type importJobJSON struct {
	ID            string                  `json:"id" format:"uuid"`
	Source        string                  `json:"source" doc:"The application the history is imported from." example:"aximote"`
	SourceVehicle string                  `json:"source_vehicle" doc:"The vehicle there."`
	Status        importStatusJSON        `json:"status" doc:"pending: waiting for the import worker, or for the application's limit (not_before). running: being read, page by page; a restart resumes it. done, failed: over, its token erased."`
	Step          importStepJSON          `json:"step" doc:"What is being read: the trips, then the charges."`
	Imported      int                     `json:"imported" minimum:"0" doc:"Events written so far, new or replacing those of an earlier import."`
	Skipped       int                     `json:"skipped" minimum:"0" doc:"Events left because they start at or after the cutoff."`
	Invalid       int                     `json:"invalid" minimum:"0" doc:"Events left because they are unfinished or out of bounds (a trip without an end, a charge in progress). The application's fuel stops are left without being counted."`
	Cutoff        *time.Time              `json:"cutoff" pattern:"Z$" doc:"The vehicle's first reading: only what starts before it is imported. null: no page read yet."`
	Error         null[importFailureJSON] `json:"error" doc:"Why the import failed. token_refused: the application refused the token (revoked, or mistyped). source_failed: the application kept failing. internal: Runsten failed, see its logs. null: not failed."`
	CreatedAt     time.Time               `json:"created_at" pattern:"Z$"`
	NotBefore     time.Time               `json:"not_before" pattern:"Z$" doc:"Not read again before: the application asked to wait, or a failure is retried."`
	EndedAt       *time.Time              `json:"ended_at" pattern:"Z$" doc:"null: still active."`
}

func importJobOf(j ImportJob) importJobJSON {
	out := importJobJSON{
		ID: j.ID, Source: j.Source, SourceVehicle: j.SourceVehicle, Status: importStatusJSON(j.Status),
		Step: importStepJSON(j.Step), Imported: j.Imported, Skipped: j.Skipped, Invalid: j.Invalid,
		Cutoff: timeOrNil(j.Cutoff), CreatedAt: j.CreatedAt, NotBefore: j.NotBefore, EndedAt: timeOrNil(j.EndedAt),
	}
	if j.Error != "" {
		out.Error = known(importFailureJSON(j.Error))
	}
	return out
}

type importJobStateJSON struct {
	Job null[importJobJSON] `json:"job" doc:"The vehicle's latest import; null: none yet."`
}

// The token's pattern is Aximote's personal access token: axm_pat_ then 26 base62
// characters, as its specification states.
type aximoteTokenJSON struct {
	Token string `json:"token" pattern:"^axm_pat_[0-9A-Za-z]{26}$" doc:"A personal access token of Aximote (axm_pat_…), created in its app (Pro). Only sent to Aximote, never returned."`
}

type aximoteVehiclesInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	Body    aximoteTokenJSON
}

type aximoteVehicleJSON struct {
	ID         string  `json:"id" doc:"Its ID at Aximote, to import its history."`
	Name       *string `json:"name" doc:"Its name in Aximote; null: none."`
	Make       *string `json:"make"`
	Model      *string `json:"model"`
	Year       *int    `json:"year"`
	VINMatches bool    `json:"vin_matches" doc:"Aximote gives it the VIN of the vehicle: its history is this vehicle's. Never guessed otherwise."`
}

type aximoteVehicleListJSON struct {
	Items []aximoteVehicleJSON `json:"items" nullable:"false"`
}

type importSourceJSON string

// Schema lists the sources an import reads.
func (importSourceJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema[importSourceJSON]("aximote")
}

type importStartJSON struct {
	Source  importSourceJSON `json:"source" doc:"The application to import from: aximote, the only one so far."`
	Token   string           `json:"token" pattern:"^axm_pat_[0-9A-Za-z]{26}$" doc:"A personal access token of Aximote, kept encrypted until the import ends, then erased."`
	Vehicle string           `json:"vehicle" pattern:"^[A-Za-z0-9._~-]{1,128}$" doc:"The vehicle at Aximote whose history is imported: one of POST /vehicles/{vehicle}/aximote-vehicles."`
}

type importStartInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	Body    importStartJSON
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Server) aximoteVehicles(ctx context.Context, in *aximoteVehiclesInput) (*body[aximoteVehicleListJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	vehicles, err := s.Aximote.AximoteVehicles(ctx, in.Body.Token)
	var r interface{ TokenRefused() bool }
	switch {
	case errors.As(err, &r) && r.TokenRefused():
		return nil, apiError(http.StatusBadRequest, codeTokenRefused, "Aximote refused this token")
	case err != nil:
		s.Log.Warn("Aximote's vehicles not listed", "err", err)
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "Aximote did not answer, try again later")
	}
	out := aximoteVehicleListJSON{Items: []aximoteVehicleJSON{}}
	for _, a := range vehicles {
		j := aximoteVehicleJSON{
			ID: a.ID, Name: nonEmpty(a.Name), Make: nonEmpty(a.Make), Model: nonEmpty(a.Model),
			VINMatches: a.VIN != "" && strings.EqualFold(a.VIN, v.VIN),
		}
		if a.Year > 0 {
			j.Year = &a.Year
		}
		out.Items = append(out.Items, j)
	}
	return respond(out), nil
}

func (s *Server) startImport(ctx context.Context, in *importStartInput) (*body[importJobJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	account := sessionFrom(ctx).AccountID
	j, err := s.ImportJobs.CreateImportJob(ctx, account, v.ID,
		ImportRequest{Source: string(in.Body.Source), SourceVehicle: in.Body.Vehicle, Token: in.Body.Token}, s.Clock.Now())
	switch {
	case errors.Is(err, ErrImportActive):
		return nil, apiError(http.StatusConflict, codeImportActive, "an import of this vehicle is already active")
	case err != nil:
		return nil, s.internal("import not created", err)
	}
	s.Log.Info("import started", "account", account, "vehicle", v.ID, "source", j.Source, "job", j.ID)
	return respond(importJobOf(j)), nil
}

func (s *Server) getImportJob(ctx context.Context, in *vehicleInput) (*body[importJobStateJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	j, found, err := s.ImportJobs.LatestImportJob(ctx, sessionFrom(ctx).AccountID, v.ID)
	if err != nil {
		return nil, s.internal("import job not read", err)
	}
	out := importJobStateJSON{}
	if found {
		out.Job = known(importJobOf(j))
	}
	return respond(out), nil
}

// registerImportJobs registers the imports from Aximote and their jobs.
func (s *Server) registerImportJobs(api huma.API) {
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "listAximoteVehicles", Method: http.MethodPost, Path: "/vehicles/{vehicle}/aximote-vehicles",
		Tags: []string{"events"}, Summary: "The vehicles of an Aximote account",
		Description: "Asks Aximote for the vehicles its token gives access to, and tells the one with the VIN of the " +
			"vehicle. The token goes in the body, never in the URL; it is not kept.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "Aximote's vehicles, in its order.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `token_refused`: Aximote refused the token.",
		http.StatusForbidden:  errCrossOrigin + " " + errSessionOnly,
		http.StatusNotFound:   errVehicle,
		http.StatusBadGateway: "`source_unavailable`: Aximote did not answer, or asked to wait.",
	})), s.aximoteVehicles)
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "startImport", Method: http.MethodPost, Path: "/vehicles/{vehicle}/import-job",
		Tags: []string{"events"}, Summary: "Import a vehicle's history from another application",
		Description: "Creates the import, which runs in the background: its trips, then its charges, page by page, " +
			"only what starts before the vehicle's first reading. Its progress and its report are read with " +
			"`GET /vehicles/{vehicle}/import-job`. An event imported before is replaced. The token is kept encrypted " +
			"until the import ends, then erased.",
		DefaultStatus: http.StatusAccepted,
		Middlewares:   huma.Middlewares{s.requireJSON},
	}, "The import, pending.", writeErrors(map[int]string{
		http.StatusForbidden: errCrossOrigin + " " + errSessionOnly,
		http.StatusNotFound:  errVehicle,
		http.StatusConflict:  "`import_active`: an import of the vehicle is pending or running.",
	})), s.startImport)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getImportJob", Method: http.MethodGet, Path: "/vehicles/{vehicle}/import-job", Tags: []string{"events"},
		Summary:     "The latest import of a vehicle",
		Description: "How it stands: pending, running, or over with its report. Never its token.",
	}, "The latest import.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getImportJob)
}
