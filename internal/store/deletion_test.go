package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/importer"
	"runsten/internal/oauth"
)

func TestDeleteVehicle(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	a, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := s.SaveConnection(ctx, a, oauth.Credentials{AccessToken: "token-a"})
	if err != nil {
		t.Fatal(err)
	}
	add := func(vin string) string {
		t.Helper()
		v, err := s.AddVehicle(ctx, a, conn, vin)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	va, other := add("YV1AAAAAAAAAAAAA1"), add("YV1AAAAAAAAAAAAA2")
	for _, v := range []struct{ account, vehicle string }{{a, va}, {a, other}, {b, vb}} {
		saveOdometer(t, s, v.account, v.vehicle, t0)
		if err := s.SaveDerivation(ctx, v.account, v.vehicle, time.Time{}, sampleResult(t0)); err != nil {
			t.Fatal(err)
		}
	}
	trip := importedTrip("aximote", "trip-1", t0.Add(-48*time.Hour))
	trip.Track = []byte(`{"type":"LineString","coordinates":[[4.8357,45.764],[4.927,45.7797]]}`)
	if _, err := s.SaveImport(ctx, a, va, []importer.ImportedTrip{trip}, nil); err != nil {
		t.Fatal(err)
	}

	// Every table of the schema with a vehicle_id: the vehicle's rows, within its account.
	rows, err := s.pool.Query(ctx, `
		SELECT table_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name = 'vehicle_id' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(tables) < 5 {
		t.Fatalf("tables %v, %v", tables, err)
	}
	held := func(account, vehicle string) int {
		t.Helper()
		n := 0
		err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
			for _, table := range append(tables, "vehicles") {
				col := "vehicle_id"
				if table == "vehicles" {
					col = "id"
				}
				var c int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE "+col+" = $1", vehicle).Scan(&c); err != nil {
					return err //nolint:wrapcheck // a test
				}
				n += c
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := map[string]int{va: held(a, va), other: held(a, other), vb: held(b, vb)}
	if before[va] < 7 { // the vehicle, a snapshot, trips, a charge, a version, an import and its track
		t.Fatalf("only %d rows of the vehicle to delete", before[va])
	}

	// Another account's vehicle is not found, and stays.
	if err := s.DeleteVehicle(ctx, a, vb); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("another account's vehicle: %v", err)
	}
	if err := s.DeleteVehicle(ctx, a, va); err != nil {
		t.Fatal(err)
	}
	if n := held(a, va); n != 0 {
		t.Errorf("%d rows left of the deleted vehicle", n)
	}
	if n := held(a, other); n != before[other] {
		t.Errorf("the account's other vehicle holds %d rows, want %d", n, before[other])
	}
	if n := held(b, vb); n != before[vb] {
		t.Errorf("another account's vehicle holds %d rows, want %d", n, before[vb])
	}
	// The connection stays: it reads the account's other vehicles.
	var conns int
	if err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM connections").Scan(&conns) //nolint:wrapcheck // a test
	}); err != nil || conns != 1 {
		t.Errorf("%d connections after, %v", conns, err)
	}
	if err := s.DeleteVehicle(ctx, a, va); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("deleted again: %v", err)
	}
	// Listed again, it comes back as a new vehicle, without its history.
	if again := add("YV1AAAAAAAAAAAAA1"); again == va || held(a, again) != 1 {
		t.Errorf("added again as %q (was %q), holding %d rows, want itself only", again, va, held(a, again))
	}
}
