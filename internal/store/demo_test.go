package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/core"
)

// TestDemoConnection: an account's demo connection beside its Volvo ID: the collector
// reads it (a credential set), the marker is sealed, and it is deleted with its vehicle
// and its history, the account's other vehicle untouched.
func TestDemoConnection(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	a, volvoVehicle := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	if c, err := s.DemoConnection(ctx, a); err != nil || c.ID != "" || !c.SetAt.IsZero() {
		t.Fatalf("a connection before any: %+v, %v", c, err)
	}
	conn, err := s.SetDemoConnection(ctx, a, t0)
	if err != nil {
		t.Fatal(err)
	}
	vehicle, err := s.AddVehicle(ctx, a, conn, "YV12ZEL000DEM0001")
	if err != nil {
		t.Fatal(err)
	}
	targets := targetsOf(t, s, a)
	if got := targets[vehicle]; got.Provider != "demo" || got.ConnectionID != conn || !got.CredentialSetAt.Equal(t0) || got.CredentialRefused {
		t.Fatalf("target %+v", got)
	}
	c, err := s.DemoConnection(ctx, a)
	if err != nil || c.ID != conn || c.Last4 != "demo" || !c.SetAt.Equal(t0) {
		t.Errorf("connection %+v, %v", c, err)
	}
	if again, err := s.SetDemoConnection(ctx, a, t0.Add(time.Hour)); err != nil || again != conn {
		t.Errorf("set again: %s, %v", again, err)
	}

	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var sealed bool
	if err := db.QueryRow(ctx, `SELECT credential <> 'demo'::bytea FROM connections WHERE id = $1`, conn).Scan(&sealed); err != nil || !sealed {
		t.Errorf("the marker in plaintext: %v, %v", sealed, err)
	}

	// A snapshot of each vehicle; the demo's goes with it.
	for _, v := range []string{vehicle, volvoVehicle} {
		if err := s.SaveHistory(ctx, a, v, []HistoricSnapshot{{
			Kind: core.Kind{Source: "demo", Endpoint: "vehicle"}, FetchedAt: t0, Payload: []byte(`{}`), Hash: []byte("h"),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteDemoConnection(ctx, a); err != nil {
		t.Fatal(err)
	}
	if c, err := s.DemoConnection(ctx, a); err != nil || c.ID != "" {
		t.Errorf("after the deletion: %+v, %v", c, err)
	}
	targets = targetsOf(t, s, a)
	if _, ok := targets[vehicle]; ok || targets[volvoVehicle].Provider != "volvo" {
		t.Errorf("after the deletion: %+v", targets)
	}
	var left int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM snapshots WHERE vehicle_id = $1`, vehicle).Scan(&left); err != nil || left != 0 {
		t.Errorf("%d snapshots left of the demo vehicle, %v", left, err)
	}
	// Nothing to delete is no error, and a new connection starts again.
	if err := s.DeleteDemoConnection(ctx, a); err != nil {
		t.Errorf("deleted twice: %v", err)
	}
	if again, err := s.SetDemoConnection(ctx, a, t0); err != nil || again == conn {
		t.Errorf("set after the deletion: %s, %v", again, err)
	}
}

// TestSaveHistory: a past written afterwards is what the collector would have saved one by
// one: a run of the same values one snapshot, read again until its last, and a second
// write of it changes nothing.
func TestSaveHistory(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	a, v := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	kind := core.Kind{Source: "demo", Endpoint: "vehicle"}
	at := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
	hist := []HistoricSnapshot{
		{Kind: kind, FetchedAt: at(0), Payload: []byte(`{"soc":80}`), Hash: []byte("a")},
		{Kind: kind, FetchedAt: at(10), Payload: []byte(`{"soc":80}`), Hash: []byte("a")},
		{Kind: kind, FetchedAt: at(20), Payload: []byte(`{"soc":80}`), Hash: []byte("a")},
		{Kind: kind, FetchedAt: at(21), Payload: []byte(`{"soc":79}`), Hash: []byte("b")},
		{Kind: kind, FetchedAt: at(22), Payload: []byte(`{"soc":80}`), Hash: []byte("a")},
	}
	if err := s.SaveHistory(ctx, a, v, nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.SaveHistory(ctx, a, v, hist); err != nil {
			t.Fatal(err)
		}
	}
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	rows, err := db.Query(ctx, `SELECT fetched_at, checked_at, payload->>'soc' FROM snapshots WHERE vehicle_id = $1 ORDER BY fetched_at`, v)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		fetched, checked time.Time
		soc              string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.fetched, &r.checked, &r.soc); err != nil {
			t.Fatal(err)
		}
		got = append(got, row{r.fetched.UTC(), r.checked.UTC(), r.soc})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []row{{at(0), at(20), "80"}, {at(21), at(21), "79"}, {at(22), at(22), "80"}}
	if len(got) != len(want) {
		t.Fatalf("snapshots %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("snapshot %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}
