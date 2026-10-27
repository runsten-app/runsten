package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
)

// TestTeslaToken: an account's Tesla connection beside its Volvo ID, its refresh token
// sealed and read back, rotated by the collector, and replaced by the owner; a rotation
// of the token the owner replaced leaves the new one alone.
func TestTeslaToken(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	conn, err := s.SetTeslaToken(ctx, a, "refresh-token-a-1234", "refresh-token-a-1234", t0)
	if err != nil {
		t.Fatal(err)
	}
	vt, err := s.AddVehicle(ctx, a, conn, "LRW3E7EK1RC000001")
	if err != nil {
		t.Fatal(err)
	}
	targets := targetsOf(t, s, a)
	want := collector.Target{AccountID: a, VehicleID: vt, VIN: "LRW3E7EK1RC000001", ConnectionID: conn, Provider: "tesla", CredentialSetAt: t0}
	if targets[vt] != want || targets[va].Provider != "volvo" {
		t.Fatalf("targets %+v, want %+v", targets, want)
	}
	if tok, at, err := s.TeslaToken(ctx, a, conn); err != nil || tok != "refresh-token-a-1234" || !at.Equal(t0) {
		t.Errorf("token %q set at %v, %v", tok, at, err)
	}
	if _, _, err := s.TeslaToken(ctx, a, targets[va].ConnectionID); err == nil {
		t.Error("a token read from the Volvo connection")
	}

	// Rotated: the new token is read, set at the same time, its last four the owner's.
	if err := s.RotateTeslaToken(ctx, a, conn, t0, "refresh-token-a-5678"); err != nil {
		t.Fatal(err)
	}
	if tok, at, err := s.TeslaToken(ctx, a, conn); err != nil || tok != "refresh-token-a-5678" || !at.Equal(t0) {
		t.Errorf("rotated: %q set at %v, %v", tok, at, err)
	}
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var last4 string
	var plain bool
	err = db.QueryRow(ctx, `SELECT credential_last4, position('refresh-token'::bytea in credential) > 0
		FROM connections WHERE id = $1`, conn).Scan(&last4, &plain)
	if err != nil || last4 != "1234" || plain {
		t.Errorf("last4 %q, plaintext %v, %v", last4, plain, err)
	}

	// Refused, then replaced by the owner: no longer refused; a late rotation of the
	// previous token leaves the new one alone.
	if err := s.SetCredentialRefused(ctx, a, conn, t0); err != nil {
		t.Fatal(err)
	}
	if !targetsOf(t, s, a)[vt].CredentialRefused {
		t.Error("not refused")
	}
	t1 := t0.Add(time.Hour)
	if again, err := s.SetTeslaToken(ctx, a, "refresh-token-b-9abc", "given-token-b-7777", t1); err != nil || again != conn {
		t.Fatalf("set again: %s, %v", again, err)
	}
	if err := s.RotateTeslaToken(ctx, a, conn, t0, "refresh-token-a-def0"); err != nil {
		t.Fatal(err)
	}
	if tok, at, err := s.TeslaToken(ctx, a, conn); err != nil || tok != "refresh-token-b-9abc" || !at.Equal(t1) {
		t.Errorf("replaced: %q set at %v, %v", tok, at, err)
	}
	if tg := targetsOf(t, s, a)[vt]; tg.CredentialRefused || !tg.CredentialSetAt.Equal(t1) {
		t.Errorf("replaced: %+v", tg)
	}

	// Shown again by the last four of the token given, never the token.
	if c, err := s.TeslaConnection(ctx, a); err != nil || c.ID != conn || c.Last4 != "7777" || !c.SetAt.Equal(t1) || !c.RefusedAt.IsZero() {
		t.Errorf("connection %+v, %v", c, err)
	}
	// Removed: the vehicle stays, its token missing.
	if err := s.DeleteTeslaToken(ctx, a); err != nil {
		t.Fatal(err)
	}
	if c, err := s.TeslaConnection(ctx, a); err != nil || c.ID != conn || !c.SetAt.IsZero() {
		t.Errorf("deleted: %+v, %v", c, err)
	}
	vs, err := s.Vehicles(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, v := range vs {
		if v.ID == vt {
			kept = true
			if v.Provider != "tesla" || !v.CredentialMissing {
				t.Errorf("vehicle %+v", v)
			}
		}
	}
	if !kept {
		t.Error("the vehicle went with its token")
	}

	// Another account reads none of it.
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if _, _, err := s.TeslaToken(ctx, b, conn); err == nil {
		t.Error("another account read the token")
	}
}

// TestTeslaStatus: the collector's calls of the Owner API and its failure api_closed are
// saved.
func TestTeslaStatus(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	conn, err := s.SetTeslaToken(ctx, a, "refresh-token-a-1234", "refresh-token-a-1234", t0)
	if err != nil {
		t.Fatal(err)
	}
	vt, err := s.AddVehicle(ctx, a, conn, "LRW3E7EK1RC000001")
	if err != nil {
		t.Fatal(err)
	}
	err = s.SaveStatus(ctx, collector.Status{
		AccountID: a, VehicleID: vt, PassedAt: t0, Mode: "parked",
		Failure: collector.Failure{At: t0, Endpoint: "vehicle_data", Status: 403, Kind: collector.FailAPIClosed},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCalls(ctx, []collector.Calls{{AccountID: a, API: "tesla", Hour: t0, N: 3}}); err != nil {
		t.Fatal(err)
	}
}
