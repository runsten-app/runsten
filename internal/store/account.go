package store

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
)

// exportFormat names the export, and its version the layout of its envelope. The
// rows' layout is the schema's, named by its latest migration.
const (
	exportFormat  = "runsten-account"
	exportVersion = 1
)

// exportTable is a table of the export: its rows in a stable order, without the
// columns left out.
type exportTable struct {
	name    string
	orderBy string
	// omit are the columns left out besides account_id, which every row would repeat:
	// secrets, and what only serves the application.
	omit []string
}

// exportTables are the tables of an account's export, in the order they are written.
// Every table of the schema is here or in notExported (TestExportCoversEveryTable).
var exportTables = []exportTable{
	{name: "accounts", orderBy: "id"},
	{name: "users", orderBy: "id", omit: []string{"password_hash"}},
	{name: "sessions", orderBy: "created_at, id", omit: []string{"token_hash"}},
	{name: "api_tokens", orderBy: "created_at, id", omit: []string{"token_hash"}},
	{name: "account_settings", orderBy: "currency"},
	{name: "connections", orderBy: "id", omit: []string{"access_token", "refresh_token", "api_key"}},
	{name: "vehicles", orderBy: "id"},
	{name: "collector_status", orderBy: "vehicle_id"},
	{name: "api_calls", orderBy: "hour, api"},
	{name: "places", orderBy: "created_at, id"},
	{name: "place_tariffs", orderBy: "place_id, valid_from"},
	{name: "trips", orderBy: "vehicle_id, started_after, detected_at"},
	{name: "charges", orderBy: "vehicle_id, started_after, detected_at"},
	{name: "charge_costs", orderBy: "entered_at, id"},
	{name: "geocoded_positions", orderBy: "lat_e4, lon_e4"},
	{name: "mqtt_brokers", orderBy: "account_id", omit: []string{"password"}},
	{name: "mqtt_status", orderBy: "account_id"},
	// value_hash: the deduplication's, a hash of the payload.
	{name: "snapshots", orderBy: "vehicle_id, endpoint, fetched_at", omit: []string{"value_hash"}},
}

// notExported are the tables an export leaves out, and why.
var notExported = map[string]string{
	"derivation_cursors": "where the derivation resumes: rebuilt from the snapshots",
	"schema_migrations":  "the schema's bookkeeping, no account's",
}

// ExportAccount implements api.Accounts: writes to w, as one JSON document, every row
// the account holds, raw snapshots included, then those of an extension's tables
// (more), as of one snapshot of the database. Secrets are left out: tokens, the
// application key, the MQTT broker's password, the password hash and the hashes of the
// session and access tokens. It reads and writes row by row: a failure midway leaves w
// with a truncated document.
func (s *Store) ExportAccount(ctx context.Context, accountID string, at time.Time, more []api.ExportTable, w io.Writer) error {
	tables := slices.Clone(exportTables)
	for _, t := range more {
		tables = append(tables, exportTable{name: t.Name, orderBy: t.OrderBy, omit: t.Omit})
	}
	schema, err := latestMigration()
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	err = s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		// to_jsonb writes times in the session's time zone.
		if _, err := tx.Exec(ctx, "SET LOCAL TimeZone = 'UTC'"); err != nil {
			return fmt.Errorf("time zone: %w", err)
		}
		// Every value of the envelope is ASCII without quotes nor backslashes: Go's
		// quoting is JSON's.
		fmt.Fprintf(bw, `{"format":%q,"version":%d,"schema":%q,"exported_at":%q,"tables":[`,
			exportFormat, exportVersion, schema, at.UTC().Format(time.RFC3339Nano))
		for i, t := range tables {
			if i > 0 {
				_ = bw.WriteByte(',') // sticky: the next write returns it
			}
			if err := exportRows(ctx, tx, bw, t); err != nil {
				return fmt.Errorf("%s: %w", t.name, err)
			}
		}
		_, err := bw.WriteString("]}\n")
		return err //nolint:wrapcheck // wrapped by readInAccount
	})
	if err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	return nil
}

// exportRows writes a table of the export, {"name": …, "rows": […]}, a row at a time.
func exportRows(ctx context.Context, tx pgx.Tx, w *bufio.Writer, t exportTable) error {
	fmt.Fprintf(w, `{"name":%q,"rows":[`, t.name)
	omit := append([]string{"account_id"}, t.omit...)
	if t.name == "accounts" {
		omit = append([]string{}, t.omit...) // its id is the account's; never NULL, which would null every row
	}
	// Names from exportTables or an extension, never from a request.
	rows, err := tx.Query(ctx, "SELECT to_jsonb(t) - $1::text[] FROM "+t.name+" t ORDER BY "+t.orderBy, omit)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var row []byte
		if err := rows.Scan(&row); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if n > 0 {
			_ = w.WriteByte(',') // a write error is sticky: the next Write returns it
		}
		// Stop at the first failed write (the client went away), rather than read on.
		if _, err := w.Write(row); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}
	_, err = w.WriteString("]}")
	return err //nolint:wrapcheck // wrapped by the caller
}

// latestMigration names the schema: its latest migration, without the extension.
func latestMigration() (string, error) {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil || len(names) == 0 {
		return "", fmt.Errorf("migrations: %w", err)
	}
	return strings.TrimSuffix(path.Base(slices.Max(names)), ".sql"), nil
}
