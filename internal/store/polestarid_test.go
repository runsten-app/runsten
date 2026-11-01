package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/collector"
)

// TestPolestarID: an account's Polestar ID beside its Volvo ID, in the token mode then
// the password one: sealed and read back, its refresh token rotated by the collector
// (the email and password kept), shown again by its mode and the last four characters of
// the token the connection gave, refused, replaced, and removed with the password.
func TestPolestarID(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	conn, err := s.SetPolestarID(ctx, a, api.PolestarIDCredential{Refresh: "refresh-token-a-1234"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	vp, err := s.AddVehicle(ctx, a, conn, "LPSYCZRE5RL000014")
	if err != nil {
		t.Fatal(err)
	}
	targets := targetsOf(t, s, a)
	want := collector.Target{AccountID: a, VehicleID: vp, VIN: "LPSYCZRE5RL000014", ConnectionID: conn, Provider: "polestar_id", CredentialSetAt: t0}
	if targets[vp] != want || targets[va].Provider != "volvo" {
		t.Fatalf("targets %+v, want %+v", targets, want)
	}
	if c, at, err := s.PolestarIDCredential(ctx, a, conn); err != nil || c != (api.PolestarIDCredential{Refresh: "refresh-token-a-1234"}) || !at.Equal(t0) {
		t.Errorf("credential %+v set at %v, %v", c, at, err)
	}
	if _, _, err := s.PolestarIDCredential(ctx, a, targets[va].ConnectionID); err == nil {
		t.Error("a credential read from the Volvo connection")
	}
	if c, err := s.PolestarIDConnection(ctx, a); err != nil || c.ID != conn || c.Last4 != "1234" || c.Password || !c.SetAt.Equal(t0) {
		t.Errorf("connection %+v, %v", c, err)
	}

	// The password mode: the email and password sealed with the token; rotated, they stay.
	t1 := t0.Add(time.Hour)
	given := api.PolestarIDCredential{Refresh: "refresh-token-b-5678", Email: "owner@example.com", Password: "secret-password"}
	if again, err := s.SetPolestarID(ctx, a, given, t1); err != nil || again != conn {
		t.Fatalf("set again: %s, %v", again, err)
	}
	if err := s.RotatePolestarIDToken(ctx, a, conn, t1, "refresh-token-b-9abc"); err != nil {
		t.Fatal(err)
	}
	rotated := given
	rotated.Refresh = "refresh-token-b-9abc"
	if c, at, err := s.PolestarIDCredential(ctx, a, conn); err != nil || c != rotated || !at.Equal(t1) {
		t.Errorf("rotated: %+v set at %v, %v", c, at, err)
	}
	// A late rotation of the previous connection leaves this one alone.
	if err := s.RotatePolestarIDToken(ctx, a, conn, t0, "refresh-token-a-def0"); err != nil {
		t.Fatal(err)
	}
	if c, _, err := s.PolestarIDCredential(ctx, a, conn); err != nil || c != rotated {
		t.Errorf("a late rotation: %+v, %v", c, err)
	}
	if c, err := s.PolestarIDConnection(ctx, a); err != nil || c.Last4 != "5678" || !c.Password || !c.SetAt.Equal(t1) {
		t.Errorf("password mode: %+v, %v", c, err)
	}
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var mode string
	var plain bool
	err = db.QueryRow(ctx, `SELECT credential_mode, position('secret-password'::bytea in credential) > 0
		OR position('owner@example'::bytea in credential) > 0 OR position('refresh-token'::bytea in credential) > 0
		FROM connections WHERE id = $1`, conn).Scan(&mode, &plain)
	if err != nil || mode != "password" || plain {
		t.Errorf("mode %q, plaintext %v, %v", mode, plain, err)
	}

	if err := s.SetCredentialRefused(ctx, a, conn, t1); err != nil {
		t.Fatal(err)
	}
	if c, err := s.PolestarIDConnection(ctx, a); err != nil || c.RefusedAt.IsZero() || !targetsOf(t, s, a)[vp].CredentialRefused {
		t.Errorf("refused: %+v, %v", c, err)
	}

	// Removed: the vehicle stays, its credential missing, the mode gone with it.
	if err := s.DeletePolestarID(ctx, a); err != nil {
		t.Fatal(err)
	}
	if c, err := s.PolestarIDConnection(ctx, a); err != nil || c.ID != conn || !c.SetAt.IsZero() || c.Password {
		t.Errorf("deleted: %+v, %v", c, err)
	}
	vs, err := s.Vehicles(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, v := range vs {
		if v.ID == vp {
			kept = true
			if v.Provider != "polestar_id" || !v.CredentialMissing {
				t.Errorf("vehicle %+v", v)
			}
		}
	}
	if !kept {
		t.Error("the vehicle went with its credential")
	}
	// A rotation after the removal stores nothing.
	if err := s.RotatePolestarIDToken(ctx, a, conn, t1, "refresh-token-c-0000"); err != nil {
		t.Fatal(err)
	}

	// Another account reads none of it.
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if _, err := s.SetPolestarID(ctx, a, given, t1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PolestarIDCredential(ctx, b, conn); err == nil {
		t.Error("another account read the credential")
	}
	if c, err := s.PolestarIDConnection(ctx, b); err != nil || c.ID != "" {
		t.Errorf("another account's connection: %+v, %v", c, err)
	}
}

// TestPolestarIDCalls: the collector's calls of the Polestar app's API are saved.
func TestPolestarIDCalls(t *testing.T) {
	s, _ := testStore(t)
	t0 := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	if err := s.SaveCalls(context.Background(), []collector.Calls{{AccountID: a, API: "polestar_id", Hour: t0, N: 3}}); err != nil {
		t.Fatal(err)
	}
}
