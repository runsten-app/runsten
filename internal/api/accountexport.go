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
// background and held a few days, to be downloaded. Every method is restricted to the
// account.
type AccountExports interface {
	// CreateAccountExport creates the account's export, its only one: the previous
	// one is thrown away, held or failed. ErrExportActive while one is pending or
	// running.
	CreateAccountExport(ctx context.Context, accountID string, at time.Time) (AccountExport, error)
	// LatestAccountExport is the account's export; false when it has none.
	LatestAccountExport(ctx context.Context, accountID string) (AccountExport, bool, error)
	// OpenAccountExport opens the account's held archive, to be read from any offset;
	// ErrNotFound when the export is not held: none was asked for, it is still being
	// built, or it expired. A read fails once the archive is no longer held.
	OpenAccountExport(ctx context.Context, accountID string) (AccountExport, io.ReadSeeker, error)
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
// again replaces the one held, under another id.
type AccountExport struct {
	ID        string
	Status    string // ExportPending, ExportRunning, ExportDone or ExportFailed
	Error     string // a kind, when failed
	Bytes     int64  // the archive's bytes written so far: its size once built
	CreatedAt time.Time
	NotBefore time.Time // waits until then: a failure is retried
	EndedAt   time.Time // zero while active
	ExpiresAt time.Time // when the archive held goes; zero unless done
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
	Bytes     int64                   `json:"bytes" minimum:"0" doc:"The archive's bytes: written so far while running, its size once done. 0: none yet, or failed."`
	CreatedAt time.Time               `json:"created_at" pattern:"Z$"`
	NotBefore time.Time               `json:"not_before" pattern:"Z$" doc:"Not built before: a failure is retried."`
	EndedAt   *time.Time              `json:"ended_at" pattern:"Z$" doc:"null: still active."`
	ExpiresAt *time.Time              `json:"expires_at" pattern:"Z$" doc:"When the archive held is thrown away; the export is then gone. null: not done."`
}

func accountExportOf(e AccountExport) accountExportJSON {
	out := accountExportJSON{
		ID: e.ID, Status: exportStatusJSON(e.Status), Bytes: e.Bytes,
		CreatedAt: e.CreatedAt, NotBefore: e.NotBefore, EndedAt: timeOrNil(e.EndedAt), ExpiresAt: timeOrNil(e.ExpiresAt),
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
		out.Export = known(accountExportOf(e))
	}
	return respond(out), nil
}

// downloadAccountExport serves the account's archive, a range of it when the request
// asks for one (http.ServeContent): a download cut short resumes where it stopped, as
// long as the archive is the same one (If-Range, its ETag the export's id). Its status
// goes with its first bytes: a failure before them is an error of the contract, one
// after them cuts the archive short.
func (s *Server) downloadAccountExport(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
	account := sessionFrom(ctx).AccountID
	e, archive, err := s.AccountExports.OpenAccountExport(ctx, account)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, apiError(http.StatusNotFound, codeNotFound, "no export is held: ask for one first")
	case err != nil:
		return nil, s.internal("export not opened", err)
	}
	name := `attachment; filename="runsten-account-` + e.EndedAt.UTC().Format(time.DateOnly) + `.zip"`
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		r, w := unwrap(hctx)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", name)
		w.Header().Set("ETag", `"`+e.ID+`"`)
		// The download is best effort: a client that goes away stops it, the archive
		// stays as it is, to be resumed.
		read := &readErr{ReadSeeker: archive}
		http.ServeContent(w, r, "", e.EndedAt, read)
		if read.err != nil {
			s.Log.Error("account export cut short", "account", account, "err", read.err)
			return
		}
		s.Log.Info("account export downloaded", "account", account, "range", r.Header.Get("Range"))
	}}, nil
}

// readErr keeps the first error of its reads but the end of the archive:
// http.ServeContent says nothing of a read that fails once it has answered.
type readErr struct {
	io.ReadSeeker
	err error
}

func (r *readErr) Read(p []byte) (int, error) {
	n, err := r.ReadSeeker.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && r.err == nil {
		r.err = err
	}
	return n, err //nolint:wrapcheck // the archive's reader
}

// registerAccountExport registers the account's ZIP archive, built by the export worker.
func (s *Server) registerAccountExport(api huma.API) {
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "startAccountExport", Method: http.MethodPost, Path: "/account/export-job", Tags: []string{"account"},
		Summary: "Build the account's data as a ZIP archive",
		Description: "Creates the account's export, which runs in the background: the whole of its data (the " +
			"document of `GET /account/export`), zipped, built however long it takes however big it is. Its " +
			"progress is read with `GET /account/export-job`; once done, the archive is held until `expires_at`, " +
			"or until another is asked for, downloaded with `GET /account/export.zip`. Open to every account, " +
			"whatever its offer.",
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
		Description: "The archive the export worker built: the whole of the account's data zipped. " +
			"`GET /account/export-job` tells when it is held. A `Range` request (`Accept-Ranges: bytes`) gets " +
			"that part of it, `206`, so that a download cut short resumes: with `If-Range` set to the `ETag`, " +
			"the export's id, a range of an archive replaced since gets the whole new one instead.",
	}, "The archive, as an attachment (Content-Disposition), named after the day it was built.", map[int]string{
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
		"ETag": {
			Description: "The export's id, quoted: the archive's, for If-Range.", Required: true,
			Schema: &huma.Schema{Type: huma.TypeString},
		},
	}
	o.Responses["206"] = &huma.Response{
		Description: "The range of the archive the `Range` header asked for, as Content-Range says.",
		Content: map[string]*huma.MediaType{
			"application/zip": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
		},
		Headers: map[string]*huma.Param{
			"Content-Range": {
				Description: "The range sent, such as bytes 1048576-2097151/52428800.", Required: true,
				Schema: &huma.Schema{Type: huma.TypeString},
			},
		},
	}
	huma.Register(api, o, s.downloadAccountExport)
}
