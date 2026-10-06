package store

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/auth"
	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/volvo"
)

// TestExportCoversEveryTable checks the export against the migrated schema: every table
// is exported or left out with a reason, each column it omits exists, and every bytea
// column of an exported table, where the secrets are, is omitted.
func TestExportCoversEveryTable(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
		SELECT table_name, column_name, data_type FROM information_schema.columns
		WHERE table_schema = current_schema() ORDER BY table_name, ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]map[string]string{}
	for rows.Next() {
		var table, column, typ string
		if err := rows.Scan(&table, &column, &typ); err != nil {
			t.Fatal(err)
		}
		if columns[table] == nil {
			columns[table] = map[string]string{}
		}
		columns[table][column] = typ
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// schema_migrations is not granted to runsten_app: read as the owner below.
	if len(columns) < 10 {
		t.Fatalf("only %d tables seen", len(columns))
	}

	exported := map[string]bool{}
	for _, et := range exportTables {
		if exported[et.name] {
			t.Errorf("%s exported twice", et.name)
		}
		exported[et.name] = true
		cols, ok := columns[et.name]
		if !ok {
			t.Errorf("%s: no such table", et.name)
			continue
		}
		for _, c := range et.omit {
			if _, ok := cols[c]; !ok {
				t.Errorf("%s: omits %s, no such column", et.name, c)
			}
		}
		for c, typ := range cols {
			if typ == "bytea" && !slices.Contains(et.omit, c) {
				t.Errorf("%s.%s: a bytea column exported", et.name, c)
			}
		}
	}
	for table := range columns {
		if _, left := notExported[table]; !exported[table] && !left {
			t.Errorf("%s: neither exported nor in notExported", table)
		}
		if _, left := notExported[table]; exported[table] && left {
			t.Errorf("%s: both exported and in notExported", table)
		}
	}
}

// accountData fills an account with a row of most tables.
func accountData(t *testing.T, s *Store, account, vehicle, user string, t0 time.Time) {
	t.Helper()
	ctx := context.Background()
	raw := []byte(`{"data":{"odometer":{"value":1234,"unit":"km"}}}`)
	h, _ := volvo.Fingerprint(raw)
	if _, err := s.SaveSnapshot(ctx, collector.Snapshot{
		AccountID: account, VehicleID: vehicle, Endpoint: volvo.Odometer, FetchedAt: t0, Payload: raw, Hash: h,
	}); err != nil {
		t.Fatal(err)
	}
	userID, err := s.CreateUser(ctx, account, user, "$argon2id$secret-hash-"+user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSession(ctx, auth.Session{
		AccountID: account, UserID: userID, CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0.Add(time.Hour),
	}, bytes.Repeat([]byte{user[0]}, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAPIKey(ctx, account, keyA, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePlace(ctx, account, core.Place{Name: "Home of " + user, RadiusM: 50}); err != nil {
		t.Fatal(err)
	}
}

type exported struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	Schema     string `json:"schema"`
	ExportedAt string `json:"exported_at"`
	Tables     []struct {
		Name string           `json:"name"`
		Rows []map[string]any `json:"rows"`
	} `json:"tables"`
}

func TestExportAccount(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	accountData(t, s, a, va, "alice", t0)
	accountData(t, s, b, vb, "bob", t0)

	var buf bytes.Buffer
	if err := s.ExportAccount(ctx, a, t0.Add(time.Hour), nil, &buf); err != nil {
		t.Fatal(err)
	}
	var e exported
	if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if e.Format != "runsten-account" || e.Version != 1 || e.ExportedAt != "2026-10-01T09:00:00Z" {
		t.Errorf("envelope = %q %d %q", e.Format, e.Version, e.ExportedAt)
	}
	if latest, _ := latestMigration(); e.Schema != latest || !strings.HasPrefix(e.Schema, "00") {
		t.Errorf("schema = %q, want %q", e.Schema, latest)
	}
	var names []string
	rows := map[string][]map[string]any{}
	for _, tb := range e.Tables {
		names = append(names, tb.Name)
		rows[tb.Name] = tb.Rows
	}
	var want []string
	for _, et := range exportTables {
		want = append(want, et.name)
	}
	if !slices.Equal(names, want) {
		t.Errorf("tables = %v, want %v", names, want)
	}
	for table, n := range map[string]int{
		"accounts": 1, "users": 1, "sessions": 1, "connections": 1, "vehicles": 1,
		"snapshots": 1, "places": 1, "trips": 0,
	} {
		if len(rows[table]) != n {
			t.Errorf("%s: %d rows, want %d", table, len(rows[table]), n)
		}
	}
	if len(rows["accounts"]) == 1 && rows["accounts"][0]["id"] != a {
		t.Errorf("account = %v", rows["accounts"][0])
	}
	if v := rows["vehicles"]; len(v) == 1 && v[0]["vin"] != "YV1AAAAAAAAAAAAA1" {
		t.Errorf("vehicle = %v", v[0])
	}
	if sn := rows["snapshots"]; len(sn) == 1 {
		payload, _ := json.Marshal(sn[0]["payload"])
		if !strings.Contains(string(payload), `"value":1234`) || sn[0]["fetched_at"] != "2026-10-01T08:00:00+00:00" {
			t.Errorf("snapshot = %v", sn[0])
		}
	}
	if c := rows["connections"]; len(c) == 1 && c[0]["api_key_last4"] != "cdef" {
		t.Errorf("connection = %v", c[0])
	}
	// Neither secrets, nor account_id, nor another account's data.
	for table, rs := range rows {
		for _, r := range rs {
			for _, k := range []string{"account_id", "password_hash", "token_hash", "access_token", "refresh_token", "api_key", "value_hash"} {
				if _, ok := r[k]; ok {
					t.Errorf("%s: %s exported", table, k)
				}
			}
		}
	}
	for _, leak := range []string{"secret-hash", "bob", "YV1BBBBBBBBBBBBB1", b, vb, keyA, "token-a"} {
		if strings.Contains(buf.String(), leak) {
			t.Errorf("the export holds %q", leak)
		}
	}
}

// TestExportExtensionTables: an extension's tables come after the core's, within the
// account by their row-level security, without account_id nor what they omit.
func TestExportExtensionTables(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	owner, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(ctx) }()
	if _, err := owner.Exec(ctx, `CREATE SCHEMA ext;
		GRANT USAGE ON SCHEMA ext TO runsten_app;
		CREATE TABLE ext.notes (account_id uuid NOT NULL REFERENCES accounts, note text NOT NULL, secret text NOT NULL);
		ALTER TABLE ext.notes ENABLE ROW LEVEL SECURITY;
		CREATE POLICY account_isolation ON ext.notes TO runsten_app USING (account_id = runsten_current_account());
		GRANT SELECT ON ext.notes TO runsten_app`); err != nil {
		t.Fatal(err)
	}
	for _, n := range [][]any{{a, "a2", "s"}, {a, "a1", "s"}, {b, "b1", "s"}} {
		if _, err := owner.Exec(ctx, "INSERT INTO ext.notes VALUES ($1, $2, $3)", n...); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	more := []api.ExportTable{{Name: "ext.notes", OrderBy: "note", Omit: []string{"secret"}}}
	if err := s.ExportAccount(ctx, a, time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC), more, &buf); err != nil {
		t.Fatal(err)
	}
	var e exported
	if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if len(e.Tables) != len(exportTables)+1 {
		t.Fatalf("%d tables", len(e.Tables))
	}
	last, _ := json.Marshal(e.Tables[len(e.Tables)-1])
	if want := `{"name":"ext.notes","rows":[{"note":"a1"},{"note":"a2"}]}`; string(last) != want {
		t.Errorf("extension's table = %s, want %s", last, want)
	}
}
