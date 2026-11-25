package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
)

// TestFord: an account's Ford connection beside its Volvo ID: the application and the
// refresh token sealed and read back with when they were set, the refresh token replaced
// by the one a refresh gives (never over a credential set since), the grant's end kept,
// refused, replaced, and removed, the vehicle kept.
func TestFord(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
	ends := t0.Add(365 * 24 * time.Hour)
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	cred := FordCredential{
		ClientID: "ford-client-abcd", ClientSecret: "ford-secret-value", RedirectURI: "https://runsten.example/auth/ford/callback",
		Refresh: "ford-refresh-1",
	}
	conn, err := s.SetFord(ctx, a, cred, ends, t0)
	if err != nil {
		t.Fatal(err)
	}
	vf, err := s.AddVehicle(ctx, a, conn, "WF0TESTPUMA000001")
	if err != nil {
		t.Fatal(err)
	}
	targets := targetsOf(t, s, a)
	want := collector.Target{AccountID: a, VehicleID: vf, VIN: "WF0TESTPUMA000001", ConnectionID: conn, Provider: "ford", CredentialSetAt: t0}
	if targets[vf] != want || targets[va].Provider != "volvo" {
		t.Fatalf("targets %+v, want %+v", targets, want)
	}
	if got, at, err := s.FordCredential(ctx, a, conn); err != nil || got != cred || !at.Equal(t0) {
		t.Errorf("credential %+v set at %v, %v", got, at, err)
	}
	if _, _, err := s.FordCredential(ctx, a, targets[va].ConnectionID); err == nil {
		t.Error("a Ford credential read from the Volvo connection")
	}
	if c, err := s.FordConnection(ctx, a); err != nil || c.ID != conn || c.Last4 != "abcd" || !c.SetAt.Equal(t0) || !c.ExpiresAt.Equal(ends) || !c.RefusedAt.IsZero() {
		t.Errorf("connection %+v, %v", c, err)
	}

	// The refresh token a refresh gave replaces the one kept; not over a credential set
	// at another time.
	if err := s.RotateFordToken(ctx, a, conn, t0, "ford-refresh-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.RotateFordToken(ctx, a, conn, t0.Add(-time.Hour), "ford-refresh-stale"); err != nil {
		t.Fatal(err)
	}
	got, at, err := s.FordCredential(ctx, a, conn)
	if err != nil || got.Refresh != "ford-refresh-2" || got.ClientSecret != cred.ClientSecret || !at.Equal(t0) {
		t.Errorf("after a rotation: %+v set at %v, %v", got, at, err)
	}
	if c, _ := s.FordConnection(ctx, a); c.Last4 != "abcd" || !c.ExpiresAt.Equal(ends) {
		t.Errorf("the rotation changed what the owner is shown: %+v", c)
	}

	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var plain bool
	if err := db.QueryRow(ctx, `
		SELECT position('ford-secret'::bytea in credential) > 0 OR position('ford-refresh'::bytea in credential) > 0
		FROM connections WHERE id = $1`, conn).Scan(&plain); err != nil || plain {
		t.Errorf("the secret or the refresh token in plaintext: %v, %v", plain, err)
	}

	// Refused, then connected again: the refusal cleared, the new grant's end kept.
	if err := s.SetCredentialRefused(ctx, a, conn, t0); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.FordConnection(ctx, a); c.RefusedAt.IsZero() {
		t.Error("not refused")
	}
	t1 := t0.Add(time.Hour)
	cred.ClientID, cred.Refresh = "ford-client-wxyz", "ford-refresh-3"
	if again, err := s.SetFord(ctx, a, cred, time.Time{}, t1); err != nil || again != conn {
		t.Fatalf("set again: %s, %v", again, err)
	}
	if c, _ := s.FordConnection(ctx, a); c.Last4 != "wxyz" || !c.SetAt.Equal(t1) || !c.ExpiresAt.IsZero() || !c.RefusedAt.IsZero() {
		t.Errorf("replaced: %+v", c)
	}
	if err := s.SetCredentialExpiry(ctx, a, conn, t1, ends.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.FordConnection(ctx, a); !c.ExpiresAt.Equal(ends.Add(time.Hour)) {
		t.Errorf("the grant's end told by a refresh: %+v", c)
	}

	// Removed: the connection and its vehicle stay, without a credential.
	if err := s.DeleteFord(ctx, a); err != nil {
		t.Fatal(err)
	}
	if c, err := s.FordConnection(ctx, a); err != nil || c.ID != conn || !c.SetAt.IsZero() || !c.ExpiresAt.IsZero() {
		t.Errorf("removed: %+v, %v", c, err)
	}
	if targets := targetsOf(t, s, a); targets[vf].VIN == "" {
		t.Error("the removal took the vehicle")
	}
}
