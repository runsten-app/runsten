package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// AccountExports are the accounts' ZIP archives, built by the export worker out of the
// request: GET /account/export streams the same document, but an account that outgrew
// the request (a month of readings alone can be 1 GB) has its export built in the
// background and held until it is downloaded. Every method is restricted to the account.
type AccountExports interface {
	// CreateAccountExport creates the account's export, its only one: the previous
	// one is thrown away, held or failed. ErrExportActive while one is pending or
	// running.
	CreateAccountExport(ctx context.Context, accountID string, at time.Time) (AccountExport, error)
	// LatestAccountExport is the account's export; false when it has none.
	LatestAccountExport(ctx context.Context, accountID string) (AccountExport, bool, error)
	// OpenAccountExport opens the account's archive for reading; ErrNotFound when the
	// export is not held: none was asked for, or it is still being built. The archive
	// is closed with the reader.
	OpenAccountExport(ctx context.Context, accountID string) (io.ReadCloser, error)
}

// ErrExportActive is returned for an export of an account that has one pending or
// running.
var ErrExportActive = errors.New("the account's export is already active")

// AccountExport statuses.
const (
	ExportPending = "pending"
	ExportRunning = "running"
	ExportDone    = "done"
	ExportFailed  = "failed"
)

// AccountExport is how the account's archive stands: the whole of its data (the
// document of GET /account/export) zipped. An account has one export at most; asking
// again replaces the one held.
type AccountExport struct {
	ID        string
	Status    string // ExportPending, ExportRunning, ExportDone or ExportFailed
	Error     string // a kind, when failed
	Bytes     int64  // the archive's size once built
	CreatedAt time.Time
	NotBefore time.Time // waits until then: a failure is retried
	EndedAt   time.Time // zero while active
}

type exportStatusJSON string

// Schema lists the statuses.
func (exportStatusJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema[exportStatusJSON](ExportPending, ExportRunning, ExportDone, ExportFailed)
}

type exportFailureJSON string

// Schema lists the kinds of failure, as the export worker writes them.
func (exportFailureJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema[exportFailureJSON]("internal")
}

// accountExportJSON describes the export.
type accountExportJSON struct {
	ID        string                  `json:"id" format:"uuid"`
	Status    exportStatusJSON        `json:"status" doc:"pending: waiting for the export worker. running: the archive is being built; a restart builds it again. done: held, downloaded with GET /account/export.zip. failed: over; ask for another."`
	Error     null[exportFailureJSON] `json:"error" doc:"Why the export failed. internal: Runsten failed, see its logs. null: not failed."`
	Bytes     int64                   `json:"bytes" minimum:"0" doc:"The archive's size in bytes. 0: not held."`
	CreatedAt time.Time               `json:"created_at" pattern:"Z$"`
	NotBefore time.Time               `json:"not_before" pattern:"Z$" doc:"Not built before: a failure is retried."`
	EndedAt   *time.Time              `json:"ended_at" pattern:"Z$" doc:"null: still active."`
}

func accountExportOf(e AccountExport) accountExportJSON {
	out := accountExportJSON{
		ID: e.ID, Status: exportStatusJSON(e.Status), Bytes: e.Bytes,
		CreatedAt: e.CreatedAt, NotBefore: e.NotBefore, EndedAt: timeOrNil(e.EndedAt),
	}
	if e.Error != "" {
		out.Error = known(exportFailureJSON(e.Error))
	}
	return out
}

type accountExportStateJSON struct {
	Export null[accountExportJSON] `json:"export" doc:"The account's export; null: none yet."`
}

// startAccountExport creates the account's export, built in the background.
func (s *Server) startAccountExport(ctx context.Context, _ *struct{}) (*body[accountExportJSON], error) {
	account := sessionFrom(ctx).AccountID
	e, err := s.AccountExports.CreateAccountExport(ctx, account, s.Clock.Now())
	switch {
	case errors.Is(err, ErrExportActive):
		return nil, apiError(http.StatusConflict, codeExportActive, "an export of the account is already active")
	case err != nil:
		return nil, s.internal("export not created", err)
	}
	s.Log.Info("account export started", "account", account, "job", e.ID)
	return respond(accountExportOf(e)), nil
}

func (s *Server) getAccountExport(ctx context.Context, _ *struct{}) (*body[accountExportStateJSON], error) {
	e, found, err := s.AccountExports.LatestAccountExport(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, s.internal("export not read", err)
	}
	out := accountExportStateJSON{}
	if found {
		e.CreatedAt, e.NotBefore = e.CreatedAt.UTC(), e.NotBefore.UTC()
		out.Export = known(accountExportOf(e))
	}
	return respond(out), nil
}

// downloadAccountExport streams the account's archive. Its status goes with its first
// bytes: a failure before them is an error of the contract, one after them cuts the
// archive short.
func (s *Server) downloadAccountExport(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
	account := sessionFrom(ctx).AccountID
	r, err := s.AccountExports.OpenAccountExport(ctx, account)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, apiError(http.StatusNotFound, codeNotFound, "no export is held: ask for one first")
	case err != nil:
		return nil, s.internal("export not opened", err)
	}
	name := `attachment; filename="runsten-account-` + s.Clock.Now().UTC().Format(time.DateOnly) + `.zip"`
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		hctx.SetHeader("Content-Type", "application/zip")
		hctx.SetHeader("Content-Disposition", name)
		hctx.SetStatus(http.StatusOK)
		// The download is best effort: a client that goes away stops it, the archive
		// stays as it is.
		_, err = io.Copy(hctx.BodyWriter(), r) //nolint:errcheck // the response's writer
		_ = r.Close()                          //nolint:errcheck // the reader's own cleanup
		if err != nil {
			s.Log.Error("account export cut short", "err", err)
			return
		}
		s.Log.Info("account export downloaded", "account", account)
	}}, nil
}

// registerAccountExport registers the account's ZIP archive, built by the export worker.
func (s *Server) registerAccountExport(api huma.API) {
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "startAccountExport", Method: http.MethodPost, Path: "/account/export-job", Tags: []string{"account"},
		Summary: "Build the account's data as a ZIP archive",
		Description: "Creates the account's export, which runs in the background: the whole of its data (the " +
			"document of `GET /account/export`), zipped, built however long it takes however big it is. Its " +
			"progress is read with `GET /account/export-job`; once done, the archive is held until it is asked " +
			"for again, downloaded with `GET /account/export.zip`. Open to every account, whatever its offer.",
		DefaultStatus: http.StatusAccepted,
	}, "The export, pending.", map[int]string{
		http.StatusUnauthorized:        errUnauthorized,
		http.StatusForbidden:           errCrossOrigin + " " + errSessionOnly,
		http.StatusConflict:            "`export_active`: an export of the account is pending or running.",
		http.StatusInternalServerError: errInternal,
	}), s.startAccountExport)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getAccountExport", Method: http.MethodGet, Path: "/account/export-job", Tags: []string{"account"},
		Summary:     "The account's export",
		Description: "How it stands: pending, running, or over; held once built, downloaded with `GET /account/export.zip`.",
	}, "The account's export; null: none yet.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal,
	}), s.getAccountExport)
	o := s.operation(huma.Operation{
		OperationID: "downloadAccountExport", Method: http.MethodGet, Path: "/account/export.zip", Tags: []string{"account"},
		Summary: "Download the account's data as a ZIP archive",
		Description: "The archive the export worker built: the whole of the account's data zipped, written as it " +
			"is read. `GET /account/export-job` tells when it is held.",
	}, "The archive, as an attachment (Content-Disposition), named after the day.", map[int]string{
		http.StatusUnauthorized:        errUnauthorized,
		http.StatusNotFound:            "`not_found`: no export is held, none asked for or one still being built.",
		http.StatusInternalServerError: errInternal,
	})
	o.Responses["200"].Content = map[string]*huma.MediaType{
		"application/zip": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
	}
	o.Responses["200"].Headers = map[string]*huma.Param{
		"Content-Disposition": {
			Description: "attachment, with a file name such as runsten-account-2026-10-01.zip.", Required: true,
			Schema: &huma.Schema{Type: huma.TypeString},
		},
	}
	huma.Register(api, o, s.downloadAccountExport)
}
