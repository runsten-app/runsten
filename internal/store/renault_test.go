package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/collector"
)

// TestRenault: an account's My Renault sign-in beside its Volvo ID: sealed and read back,
// shown by the last four characters of the person's id, refused, replaced, and removed,
// the vehicles kept; its calls counted on their own API.
func TestRenault(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	cred := api.RenaultCredential{LoginToken: "renault-login-token", GMID: "gmid", UCID: "ucid", PersonID: "person-0042", Country: "BE"}
	conn, err := s.SetRenaultCredential(ctx, a, cred, t0)
	if err != nil {
		t.Fatal(err)
	}
	vr, err := s.AddVehicle(ctx, a, conn, "VF1AAAAA555777999")
	if err != nil {
		t.Fatal(err)
	}
	targets := targetsOf(t, s, a)
	want := collector.Target{AccountID: a, VehicleID: vr, VIN: "VF1AAAAA555777999", ConnectionID: conn, Provider: "renault", CredentialSetAt: t0}
	if targets[vr] != want || targets[va].Provider != "volvo" {
		t.Fatalf("targets %+v, want %+v", targets, want)
	}
	if got, err := s.RenaultCredential(ctx, a, conn); err != nil || got != cred {
		t.Errorf("credential %+v, %v", got, err)
	}
	if _, err := s.RenaultCredential(ctx, a, targets[va].ConnectionID); err == nil {
		t.Error("a sign-in read from the Volvo connection")
	}
	c, err := s.RenaultConnection(ctx, a)
	if err != nil || c.ID != conn || c.Last4 != "0042" || !c.SetAt.Equal(t0) || !c.RefusedAt.IsZero() {
		t.Errorf("connection %+v, %v", c, err)
	}

	// Refused, then signed in again: the refusal cleared.
	if err := s.SetCredentialRefused(ctx, a, conn, t0); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.RenaultConnection(ctx, a); c.RefusedAt.IsZero() {
		t.Error("not refused")
	}
	t1 := t0.Add(time.Hour)
	cred.LoginToken = "renault-login-token-2"
	if again, err := s.SetRenaultCredential(ctx, a, cred, t1); err != nil || again != conn {
		t.Fatalf("set again: %s, %v", again, err)
	}
	if c, _ := s.RenaultConnection(ctx, a); !c.SetAt.Equal(t1) || !c.RefusedAt.IsZero() {
		t.Errorf("replaced: %+v", c)
	}

	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var plain bool
	if err := db.QueryRow(ctx, `SELECT position('renault-login'::bytea in credential) > 0 FROM connections WHERE id = $1`,
		conn).Scan(&plain); err != nil || plain {
		t.Errorf("the token in plaintext: %v, %v", plain, err)
	}
	// Kamereon's calls are counted on their own API.
	if _, err := db.Exec(ctx, `INSERT INTO api_calls (account_id, api, hour, calls) VALUES ($1, 'renault', '2026-11-13T08:00:00Z', 1)`, a); err != nil {
		t.Errorf("api_calls renault: %v", err)
	}

	// Removed: the connection and its vehicle stay, without a sign-in.
	if err := s.DeleteRenaultCredential(ctx, a); err != nil {
		t.Fatal(err)
	}
	if c, err := s.RenaultConnection(ctx, a); err != nil || c.ID != conn || !c.SetAt.IsZero() {
		t.Errorf("removed: %+v, %v", c, err)
	}
	if targets := targetsOf(t, s, a); targets[vr].VIN == "" {
		t.Error("the removal took the vehicle")
	}
}
