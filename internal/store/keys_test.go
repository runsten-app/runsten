package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/oauth"
)

const (
	keyA = "0123456789abcdef0123456789abcdef"
	keyB = "fedcba9876543210fedcba9876543210"
)

// TestAPIKeyLifecycle: a key given before the Volvo ID makes a connection without a
// token, which the grant then fills; the key is read back, never in plaintext in the
// database, told to the collector by its date only, and kept by a new grant. A refusal
// holds for the key refused only; a new key clears it; deleting the key keeps the grant.
func TestAPIKeyLifecycle(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 29, 8, 0, 0, 123456789, time.UTC)
	a, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if k, err := s.AccountKey(ctx, a); err != nil || k != (oauth.APIKey{}) {
		t.Fatalf("no key: %+v, %v", k, err)
	}
	if c, err := s.Connection(ctx, a); err != nil || c.ID != "" {
		t.Fatalf("no connection: %+v, %v", c, err)
	}

	conn, err := s.SetAPIKey(ctx, a, keyA, t0)
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AccountKey(ctx, a)
	setAt := t0.Truncate(time.Microsecond)
	if err != nil || k.Value != keyA || !k.SetAt.Equal(setAt) || k.ConnectionID != conn {
		t.Fatalf("key = %+v, %v", k, err)
	}
	c, err := s.Connection(ctx, a)
	if err != nil || c.ID != conn || c.Connected || c.Key.Last4 != "cdef" || !c.Key.SetAt.Equal(setAt) || !c.Key.RefusedAt.IsZero() {
		t.Fatalf("connection before the Volvo ID = %+v, %v", c, err)
	}

	// The grant fills the same connection, and keeps its key.
	if got, err := s.SaveConnection(ctx, a, oauth.Credentials{AccessToken: "token-a", RefreshToken: "refresh-a"}); err != nil || got != conn {
		t.Fatalf("SaveConnection = %s, %v; want %s", got, err, conn)
	}
	vin := "YV1AAAAAAAAAAAAA1"
	if _, err := s.AddVehicle(ctx, a, conn, vin); err != nil {
		t.Fatal(err)
	}
	if key, err := s.ConnectionKey(ctx, a, conn); err != nil || key != keyA {
		t.Errorf("ConnectionKey = %q, %v", key, err)
	}
	targets, err := s.Targets(ctx)
	if err != nil || len(targets) != 1 || !targets[0].KeySetAt.Equal(setAt) || targets[0].KeyRefused {
		t.Fatalf("targets = %+v, %v", targets, err)
	}
	vs, err := s.Vehicles(ctx, a)
	if err != nil || len(vs) != 1 || !vs[0].OwnKey || vs[0].KeyRefused {
		t.Errorf("vehicles = %+v, %v", vs, err)
	}

	// A refusal of an older key is ignored; of the current one, told everywhere.
	if err := s.SetKeyRefused(ctx, a, conn, setAt.Add(-time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Connection(ctx, a); !c.Key.RefusedAt.IsZero() {
		t.Error("the refusal of an older key marks the current one")
	}
	if err := s.SetKeyRefused(ctx, a, conn, setAt, true); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Connection(ctx, a); c.Key.RefusedAt.IsZero() || !c.Connected {
		t.Errorf("refused: %+v", c)
	}
	if targets, _ := s.Targets(ctx); !targets[0].KeyRefused {
		t.Error("the collector is not told of the refusal")
	}
	if vs, _ := s.Vehicles(ctx, a); !vs[0].KeyRefused {
		t.Error("the vehicle is not told of the refusal")
	}
	if cr, err := s.Credentials(ctx, a, conn); err != nil || cr.AccessToken != "token-a" || cr.ReauthReason != "" {
		t.Errorf("a refused key touched the grant: %+v, %v", cr, err)
	}
	if err := s.SetKeyRefused(ctx, a, conn, setAt, false); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Connection(ctx, a); !c.Key.RefusedAt.IsZero() {
		t.Error("an accepted key stays refused")
	}
	_ = s.SetKeyRefused(ctx, a, conn, setAt, true)
	if _, err := s.SetAPIKey(ctx, a, keyB, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Connection(ctx, a); c.Key.Last4 != "3210" || !c.Key.RefusedAt.IsZero() {
		t.Errorf("a new key: %+v", c)
	}

	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	for _, secret := range []string{keyA, keyB, keyB[len(keyB)-8:]} {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM connections WHERE position($1::bytea in coalesce(api_key, '')) > 0`,
			[]byte(secret)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("plaintext %s in database", secret)
		}
	}
	lastFour := func() *string {
		var s *string
		if err := db.QueryRow(ctx, "SELECT api_key_last4 FROM connections WHERE id = $1", conn).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := lastFour(); s == nil || *s != "3210" {
		t.Errorf("api_key_last4 = %v, want 3210 in plaintext", s)
	}

	// Deleting the key keeps the grant; a connection without a token goes with its key.
	if err := s.DeleteAPIKey(ctx, a); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Connection(ctx, a); c.ID != conn || !c.Connected || !c.Key.SetAt.IsZero() {
		t.Errorf("after DeleteAPIKey: %+v", c)
	}
	if s := lastFour(); s != nil {
		t.Errorf("api_key_last4 = %q after DeleteAPIKey", *s)
	}
	if targets, _ := s.Targets(ctx); !targets[0].KeySetAt.IsZero() {
		t.Error("the collector still reads with the deleted key")
	}
	b, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAPIKey(ctx, b, keyB, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAPIKey(ctx, b); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Connection(ctx, b); c.ID != "" {
		t.Errorf("a connection without token nor key is kept: %+v", c)
	}
}

// TestAPIKeyBoundToAccount: the key is sealed with its account: copied to another
// account's connection, it does not open.
func TestAPIKeyBoundToAccount(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if _, err := s.SetAPIKey(ctx, a, keyA, t0); err != nil {
		t.Fatal(err)
	}
	db, err := pgx.Connect(ctx, dbURL) // the owner, outside RLS: an attacker with the database
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	if _, err := db.Exec(ctx, `UPDATE connections SET api_key = (SELECT api_key FROM connections WHERE account_id = $1),
		api_key_set_at = $3 WHERE account_id = $2`, a, b, t0); err != nil {
		t.Fatal(err)
	}
	if k, err := s.AccountKey(ctx, b); err == nil {
		t.Errorf("A's key opened in B's account: %q", k.Value)
	}
	if _, err := s.Connection(ctx, b); err == nil {
		t.Error("A's key shown in B's account")
	}
	if k, err := s.AccountKey(ctx, a); err != nil || k.Value != keyA {
		t.Errorf("A's own key: %q, %v", k.Value, err)
	}
}

// TestAPIKeyIsolation: an account neither reads, nor sets, nor refuses, nor deletes
// another's key.
func TestAPIKeyIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	connB, err := s.SetAPIKey(ctx, b, keyB, t0)
	if err != nil {
		t.Fatal(err)
	}
	if k, err := s.AccountKey(ctx, a); err != nil || k.Value != "" {
		t.Errorf("A reads a key: %q, %v", k.Value, err)
	}
	if key, err := s.ConnectionKey(ctx, a, connB); err == nil || key != "" {
		t.Errorf("A reads B's connection key: %q, %v", key, err)
	}
	if err := s.SetKeyRefused(ctx, a, connB, t0, true); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAPIKey(ctx, a); err != nil {
		t.Fatal(err)
	}
	// A's key goes into A's connection, never B's.
	if _, err := s.SetAPIKey(ctx, a, keyA, t0); err != nil {
		t.Fatal(err)
	}
	c, err := s.Connection(ctx, b)
	if err != nil || c.ID != connB || c.Key.Last4 != "3210" || !c.Key.RefusedAt.IsZero() {
		t.Errorf("B's connection after A's writes: %+v, %v", c, err)
	}
	if k, err := s.AccountKey(ctx, b); err != nil || k.Value != keyB {
		t.Errorf("B's key: %q, %v", k.Value, err)
	}
}
