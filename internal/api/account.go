package api

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Accounts exports an account's data, whole, whatever its offer. Every method is
// restricted to the account.
type Accounts interface {
	// ExportAccount writes every row of the account as one JSON document (exportJSON),
	// secrets left out, as of one snapshot, and then the rows of the tables of more. A
	// failure midway leaves w truncated.
	ExportAccount(ctx context.Context, accountID string, at time.Time, more []ExportTable, w io.Writer) error
}

// ExportTable is a table an extension adds to the export, after the core's: one of its
// own schema (as "cloud.subscriptions"), with an account_id under the same row-level
// security. Its rows are written in OrderBy's order, without account_id nor the
// columns of Omit: its secrets, and what only serves the application. Its names come
// from the extension, never from a request.
type ExportTable struct {
	Name, OrderBy string
	Omit          []string
}

// exportJSON describes the export: the body is written by Accounts, never built from
// these types.
type exportJSON struct {
	Format     string            `json:"format" enum:"runsten-account" doc:"Names the document."`
	Version    int               `json:"version" enum:"1" doc:"The layout of this envelope."`
	Schema     string            `json:"schema" minLength:"1" doc:"The latest migration of the database (as 0013_api_key_last4): the layout of the rows."`
	ExportedAt time.Time         `json:"exported_at" pattern:"Z$" doc:"When the export was read, from one snapshot of the database."`
	Tables     []exportTableJSON `json:"tables" nullable:"false" doc:"Every table that holds the account's data, in a fixed order."`
}

type exportTableJSON struct {
	Name string          `json:"name" minLength:"1" doc:"The table, as accounts, vehicles, snapshots, trips or charges; another schema's is qualified by its name, as an extension of the instance adds them."`
	Rows []exportRowJSON `json:"rows" nullable:"false" doc:"Its rows, in a stable order."`
}

// exportRowJSON is a row of a table: the database's own layout.
type exportRowJSON struct{}

// Schema leaves the row open: its columns are the table's, named by the export's schema.
func (exportRowJSON) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Description: "A row: its columns, by name, as PostgreSQL writes them in JSON (times in UTC), " +
		"without account_id. Left out: the tokens and the application key of the connections, the password hash, " +
		"the hashes of the session tokens and of the snapshots."}
}

// exportAccount streams the account's export. The status is written with the first
// byte: a failure before it is a 500 of the contract, one after it truncates the
// document, which is then no valid JSON.
func (s *Server) exportAccount(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
	account := sessionFrom(ctx).AccountID
	at := s.Clock.Now()
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		w := &startWriter{ctx: hctx, start: func(c huma.Context) {
			c.SetHeader("Content-Type", "application/json")
			c.SetHeader("Content-Disposition", `attachment; filename="runsten-account-`+at.UTC().Format(time.DateOnly)+`.json"`)
			c.SetStatus(http.StatusOK)
		}}
		err := s.Accounts.ExportAccount(ctx, account, at, s.ExportTables, w)
		switch {
		case err != nil && !w.started:
			_, rw := unwrap(hctx)
			writeError(rw, http.StatusInternalServerError, codeInternal, "internal error, see the logs")
			s.Log.Error("account not exported", "err", err)
		case err != nil:
			s.Log.Error("account export cut short", "err", err)
		default:
			s.Log.Info("account exported", "account", account)
		}
	}}, nil
}

// startWriter writes the response's status and headers before its first byte.
type startWriter struct {
	ctx     huma.Context
	start   func(huma.Context)
	started bool
}

func (w *startWriter) Write(p []byte) (int, error) {
	if !w.started {
		w.started = true
		w.start(w.ctx)
	}
	return w.ctx.BodyWriter().Write(p) //nolint:wrapcheck // the response's writer
}

func (s *Server) registerAccount(api huma.API) {
	export := s.operation(huma.Operation{
		OperationID: "exportAccount", Method: http.MethodGet, Path: "/account/export", Tags: []string{"account"},
		Summary: "Export all the account's data",
		Description: "Every row the account holds, raw snapshots included, as one JSON document, read from one snapshot of " +
			"the database and written as it is read: a failure midway cuts it short, and it is then no valid JSON. " +
			"Secrets are left out. Open to every account, whatever its offer.",
	}, "The export, as an attachment (Content-Disposition), named after the day.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal,
	})
	exportSchema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[exportJSON](), true, "Export")
	export.Responses["200"].Content = map[string]*huma.MediaType{"application/json": {Schema: exportSchema}}
	export.Responses["200"].Headers = map[string]*huma.Param{
		"Content-Disposition": {
			Description: "attachment, with a file name such as runsten-account-2026-10-01.json.", Required: true,
			Schema: &huma.Schema{Type: huma.TypeString},
		},
	}
	huma.Register(api, export, s.exportAccount)
}
