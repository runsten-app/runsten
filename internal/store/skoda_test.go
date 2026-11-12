package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
)

// TestSkoda: an account's MyŠkoda key beside its Volvo ID: sealed and read back with when
// it was set, its expiry kept as the API tells it (never moved back, nor written over a
// key set since), refused, replaced, and removed with its expiry, the vehicles kept.
func TestSkoda(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	expires := time.Date(2027, 11, 1, 8, 0, 0, 0, time.UTC)
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	conn, err := s.SetSkodaCredential(ctx, a, "skoda-key-a-1234", expires, t0)
	if err != nil {
		t.Fatal(err)
	}
	vs, err := s.AddVehicle(ctx, a, conn, "TMBJC9NY0RF000001")
	if err != nil {
		t.Fatal(err)
	}
	targets := targetsOf(t, s, a)
	want := collector.Target{AccountID: a, VehicleID: vs, VIN: "TMBJC9NY0RF000001", ConnectionID: conn, Provider: "skoda", CredentialSetAt: t0}
	if targets[vs] != want || targets[va].Provider != "volvo" {
		t.Fatalf("targets %+v, want %+v", targets, want)
	}
	if key, at, err := s.SkodaCredential(ctx, a, conn); err != nil || key != "skoda-key-a-1234" || !at.Equal(t0) {
		t.Errorf("credential %q set at %v, %v", key, at, err)
	}
	if _, _, err := s.SkodaCredential(ctx, a, targets[va].ConnectionID); err == nil {
		t.Error("a key read from the Volvo connection")
	}
	connection := func() (last4 string, setAt, expiresAt, refusedAt time.Time) {
		t.Helper()
		c, err := s.SkodaConnection(ctx, a)
		if err != nil || c.ID != conn {
			t.Fatalf("connection %+v, %v", c, err)
		}
		return c.Last4, c.SetAt, c.ExpiresAt, c.RefusedAt
	}
	if last4, setAt, exp, refused := connection(); last4 != "1234" || !setAt.Equal(t0) || !exp.Equal(expires) || !refused.IsZero() {
		t.Errorf("connection: %s %v %v %v", last4, setAt, exp, refused)
	}

	// The expiry moves forward, never back, and only for the key set then.
	later := expires.Add(30 * 24 * time.Hour)
	for _, e := range []struct {
		setAt, at time.Time
	}{{t0, later}, {t0, expires}, {t0.Add(-time.Hour), later.Add(time.Hour)}} {
		if err := s.SetCredentialExpiry(ctx, a, conn, e.setAt, e.at); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, exp, _ := connection(); !exp.Equal(later) {
		t.Errorf("expiry %v, want %v", exp, later)
	}

	// Refused, then replaced: the refusal cleared, the new expiry the one given.
	if err := s.SetCredentialRefused(ctx, a, conn, t0); err != nil {
		t.Fatal(err)
	}
	if _, _, _, refused := connection(); refused.IsZero() {
		t.Error("not refused")
	}
	t1 := t0.Add(time.Hour)
	if again, err := s.SetSkodaCredential(ctx, a, "skoda-key-b-5678", time.Time{}, t1); err != nil || again != conn {
		t.Fatalf("set again: %s, %v", again, err)
	}
	if last4, setAt, exp, refused := connection(); last4 != "5678" || !setAt.Equal(t1) || !exp.IsZero() || !refused.IsZero() {
		t.Errorf("replaced: %s %v %v %v", last4, setAt, exp, refused)
	}

	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var plain bool
	if err := db.QueryRow(ctx, `SELECT position('skoda-key'::bytea in credential) > 0 FROM connections WHERE id = $1`,
		conn).Scan(&plain); err != nil || plain {
		t.Errorf("the key in plaintext: %v, %v", plain, err)
	}

	// Removed: the connection and its vehicle stay, without a key nor an expiry.
	if err := s.SetCredentialExpiry(ctx, a, conn, t1, later); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSkodaCredential(ctx, a); err != nil {
		t.Fatal(err)
	}
	if c, err := s.SkodaConnection(ctx, a); err != nil || c.ID != conn || !c.SetAt.IsZero() || !c.ExpiresAt.IsZero() {
		t.Errorf("removed: %+v, %v", c, err)
	}
	if targets := targetsOf(t, s, a); targets[vs].VIN == "" {
		t.Error("the removal took the vehicle")
	}
}
