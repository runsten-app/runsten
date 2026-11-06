package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
)

// TestBMW: an account's BMW connection beside its Volvo ID: sealed and read back, its
// refresh token rotated and its container recorded by the collector (neither undoing the
// other), shown again by the client ID's last four characters, refused, replaced, and
// removed.
func TestBMW(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	given := BMWCredential{ClientID: "client-id-a-1234", GCID: "gcid-a", Refresh: "refresh-token-a-1"}
	conn, err := s.SetBMW(ctx, a, given, t0)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := s.AddVehicle(ctx, a, conn, "WBA00000000000001")
	if err != nil {
		t.Fatal(err)
	}
	targets := targetsOf(t, s, a)
	want := collector.Target{AccountID: a, VehicleID: vb, VIN: "WBA00000000000001", ConnectionID: conn, Provider: "bmw", CredentialSetAt: t0}
	if targets[vb] != want || targets[va].Provider != "volvo" {
		t.Fatalf("targets %+v, want %+v", targets, want)
	}
	if c, at, err := s.BMWCredential(ctx, a, conn); err != nil || c != given || !at.Equal(t0) {
		t.Errorf("credential %+v set at %v, %v", c, at, err)
	}
	if _, _, err := s.BMWCredential(ctx, a, targets[va].ConnectionID); err == nil {
		t.Error("a credential read from the Volvo connection")
	}
	if c, err := s.BMWConnection(ctx, a); err != nil || c.ID != conn || c.Last4 != "1234" || !c.SetAt.Equal(t0) {
		t.Errorf("connection %+v, %v", c, err)
	}

	// Rotated, then the container recorded: each keeps the other.
	if err := s.RotateBMWToken(ctx, a, conn, t0, "refresh-token-a-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBMWContainer(ctx, a, conn, t0, "container-1"); err != nil {
		t.Fatal(err)
	}
	kept := BMWCredential{ClientID: given.ClientID, GCID: given.GCID, Refresh: "refresh-token-a-2", ContainerID: "container-1"}
	if c, _, err := s.BMWCredential(ctx, a, conn); err != nil || c != kept {
		t.Errorf("rotated: %+v, %v", c, err)
	}
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var plain bool
	err = db.QueryRow(ctx, `SELECT position('refresh-token'::bytea in credential) > 0
		OR position('gcid-a'::bytea in credential) > 0 OR position('client-id'::bytea in credential) > 0
		FROM connections WHERE id = $1`, conn).Scan(&plain)
	if err != nil || plain {
		t.Errorf("plaintext %v, %v", plain, err)
	}

	// Connected again: a late rotation or container of the previous one leaves it alone.
	t1 := t0.Add(time.Hour)
	again := BMWCredential{ClientID: "client-id-a-5678", GCID: "gcid-a", Refresh: "refresh-token-b-1"}
	if id, err := s.SetBMW(ctx, a, again, t1); err != nil || id != conn {
		t.Fatalf("set again: %s, %v", id, err)
	}
	if err := s.RotateBMWToken(ctx, a, conn, t0, "refresh-token-a-3"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBMWContainer(ctx, a, conn, t0, "container-2"); err != nil {
		t.Fatal(err)
	}
	if c, at, err := s.BMWCredential(ctx, a, conn); err != nil || c != again || !at.Equal(t1) {
		t.Errorf("a late rotation: %+v set at %v, %v", c, at, err)
	}

	if err := s.SetCredentialRefused(ctx, a, conn, t1); err != nil {
		t.Fatal(err)
	}
	if c, err := s.BMWConnection(ctx, a); err != nil || c.RefusedAt.IsZero() || !targetsOf(t, s, a)[vb].CredentialRefused {
		t.Errorf("refused: %+v, %v", c, err)
	}

	// Removed: the vehicle stays, its credential missing.
	if err := s.DeleteBMW(ctx, a); err != nil {
		t.Fatal(err)
	}
	if c, err := s.BMWConnection(ctx, a); err != nil || c.ID != conn || !c.SetAt.IsZero() {
		t.Errorf("deleted: %+v, %v", c, err)
	}
	vs, err := s.Vehicles(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range vs {
		if v.ID == vb {
			found = true
			if v.Provider != "bmw" || !v.CredentialMissing {
				t.Errorf("vehicle %+v", v)
			}
		}
	}
	if !found {
		t.Error("the vehicle went with its credential")
	}
	if err := s.RotateBMWToken(ctx, a, conn, t1, "refresh-token-c"); err != nil {
		t.Fatal(err)
	}

	// Another account reads none of it.
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if _, err := s.SetBMW(ctx, a, given, t1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BMWCredential(ctx, b, conn); err == nil {
		t.Error("another account read the credential")
	}
	if c, err := s.BMWConnection(ctx, b); err != nil || c.ID != "" {
		t.Errorf("another account's connection: %+v, %v", c, err)
	}
}

// TestBMWCalls: the collector's calls of CarData's API are saved.
func TestBMWCalls(t *testing.T) {
	s, _ := testStore(t)
	t0 := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	if err := s.SaveCalls(context.Background(), []collector.Calls{{AccountID: a, API: "bmw", Hour: t0, N: 3}}); err != nil {
		t.Fatal(err)
	}
}
