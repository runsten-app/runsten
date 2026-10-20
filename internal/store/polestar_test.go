package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
)

var (
	credA = PolestarCredential{ClientID: "client-a-0123", ClientSecret: "secret-a-4567", AccountID: "account-a-89ab"}
	credB = PolestarCredential{ClientID: "client-b-cdef", ClientSecret: "secret-b-fedc", AccountID: "account-b-ba98"}
)

const polestarVIN = "LPSVSEDE5RL000001"

// targetsOf returns the targets of the account, by vehicle.
func targetsOf(t *testing.T, s *Store, account string) map[string]collector.Target {
	t.Helper()
	targets, err := s.Targets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]collector.Target{}
	for _, tg := range targets {
		if tg.AccountID == account {
			out[tg.VehicleID] = tg
		}
	}
	return out
}

// TestPolestarCredential: an account's Polestar connection beside its Volvo ID, its
// credential sealed and read back, its last four characters in plaintext, refused and
// replaced, and its vehicles no longer shared then listed again.
func TestPolestarCredential(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	conn, err := s.SetPolestarCredential(ctx, a, credA, t0)
	if err != nil {
		t.Fatal(err)
	}
	vp, err := s.AddVehicle(ctx, a, conn, polestarVIN)
	if err != nil {
		t.Fatal(err)
	}

	targets := targetsOf(t, s, a)
	want := collector.Target{AccountID: a, VehicleID: vp, VIN: polestarVIN, ConnectionID: conn, Provider: "polestar", CredentialSetAt: t0}
	if targets[vp] != want || targets[va].Provider != "volvo" || !targets[va].CredentialSetAt.IsZero() {
		t.Fatalf("targets %+v, want %+v", targets, want)
	}
	if c, err := s.PolestarCredential(ctx, a, conn); err != nil || c != credA {
		t.Errorf("credential %+v, %v", c, err)
	}
	if _, err := s.PolestarCredential(ctx, a, targets[va].ConnectionID); err == nil {
		t.Error("a credential read from the Volvo connection")
	}

	// Sealed: neither the secret nor the client ID in plaintext, but its last four.
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var last4 string
	var plain int
	err = db.QueryRow(ctx, `SELECT credential_last4, (position('secret-a'::bytea in credential) > 0)::int +
		(position('client-a'::bytea in credential) > 0)::int FROM connections WHERE id = $1`, conn).Scan(&last4, &plain)
	if err != nil || last4 != "0123" || plain != 0 {
		t.Errorf("last4 %q, plaintext %d, %v", last4, plain, err)
	}

	// Refused: only the credential set at the time given.
	if err := s.SetCredentialRefused(ctx, a, conn, t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if targetsOf(t, s, a)[vp].CredentialRefused {
		t.Error("a credential set since marked refused")
	}
	if err := s.SetCredentialRefused(ctx, a, conn, t0); err != nil {
		t.Fatal(err)
	}
	if !targetsOf(t, s, a)[vp].CredentialRefused || targetsOf(t, s, a)[va].CredentialRefused {
		t.Error("credential not marked refused")
	}
	// A new credential is not known refused, and replaces the connection's.
	t1 := t0.Add(24 * time.Hour)
	if again, err := s.SetPolestarCredential(ctx, a, credB, t1); err != nil || again != conn {
		t.Fatalf("new credential: %s, %v", again, err)
	}
	if tg := targetsOf(t, s, a)[vp]; tg.CredentialRefused || !tg.CredentialSetAt.Equal(t1) {
		t.Errorf("after a new credential: %+v", tg)
	}
	if c, err := s.PolestarCredential(ctx, a, conn); err != nil || c != credB {
		t.Errorf("new credential %+v, %v", c, err)
	}

	// No longer shared, then listed again.
	if err := s.SetUnshared(ctx, a, vp); err != nil {
		t.Fatal(err)
	}
	if !targetsOf(t, s, a)[vp].Unshared || targetsOf(t, s, a)[va].Unshared {
		t.Error("vehicle not marked unshared")
	}
	if _, err := s.AddVehicle(ctx, a, conn, polestarVIN); err != nil {
		t.Fatal(err)
	}
	if targetsOf(t, s, a)[vp].Unshared {
		t.Error("listed again, still unshared")
	}

	// The Volvo connection's application key removed: the Polestar connection stays.
	if err := s.DeleteAPIKey(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, ok := targetsOf(t, s, a)[vp]; !ok {
		t.Error("Polestar connection deleted with the Volvo key")
	}

	// What the collector writes of a Polestar vehicle: its calls and its failures.
	if err := s.SaveCalls(ctx, []collector.Calls{{AccountID: a, API: "polestar", Hour: t0, N: 3}}); err != nil {
		t.Errorf("calls of the Data Portal: %v", err)
	}
	for _, kind := range []string{collector.FailCredentialRefused, collector.FailUnshared, collector.FailNotGranted} {
		f := collector.Failure{At: t0, Endpoint: "battery", Status: 403, Kind: kind}
		if err := s.SaveStatus(ctx, collector.Status{AccountID: a, VehicleID: vp, PassedAt: t0, Mode: "parked", Failure: f}); err != nil {
			t.Errorf("status %s: %v", kind, err)
		}
	}
}

// TestPolestarCredentialChecks: a Polestar connection is its credential alone.
func TestPolestarCredentialChecks(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	conn, err := s.SetPolestarCredential(ctx, a, credA, time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	for name, update := range map[string]string{
		"a token":            "UPDATE connections SET access_token = 'x' WHERE id = $1",
		"an application key": "UPDATE connections SET api_key = 'x', api_key_set_at = now(), api_key_last4 = 'x' WHERE id = $1",
		"no credential":      "UPDATE connections SET credential = NULL, credential_set_at = NULL, credential_last4 = NULL WHERE id = $1",
		"another provider":   "UPDATE connections SET provider = 'tesla' WHERE id = $1",
	} {
		if _, err := db.Exec(ctx, update, conn); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE connections SET credential = 'x', credential_set_at = now(), credential_last4 = 'x'
		WHERE account_id = $1 AND provider = 'volvo'`, a); err == nil {
		t.Error("a credential on a Volvo connection accepted")
	}
}

// TestPolestarCredentialIsolation: an account neither reads, nor refuses, nor replaces
// another's credential, nor marks its vehicles; copied to another account's connection,
// the credential does not open.
func TestPolestarCredentialIsolation(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	connB, err := s.SetPolestarCredential(ctx, b, credB, t0)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := s.AddVehicle(ctx, b, connB, polestarVIN)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := s.PolestarCredential(ctx, a, connB); err == nil {
		t.Errorf("A reads B's credential: %+v", c)
	}
	if err := s.SetCredentialRefused(ctx, a, connB, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUnshared(ctx, a, vb); err != nil {
		t.Fatal(err)
	}
	// A's credential goes into A's connection, never B's.
	connA, err := s.SetPolestarCredential(ctx, a, credA, t0)
	if err != nil || connA == connB {
		t.Fatalf("A's connection %s, %v", connA, err)
	}
	if tg := targetsOf(t, s, b)[vb]; tg.CredentialRefused || tg.Unshared || tg.ConnectionID != connB {
		t.Errorf("B's vehicle after A's writes: %+v", tg)
	}
	if c, err := s.PolestarCredential(ctx, b, connB); err != nil || c != credB {
		t.Errorf("B's credential %+v, %v", c, err)
	}

	db, err := pgx.Connect(ctx, dbURL) // the owner, outside RLS: an attacker with the database
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	if _, err := db.Exec(ctx, `UPDATE connections SET credential = (SELECT credential FROM connections WHERE id = $1)
		WHERE id = $2`, connB, connA); err != nil {
		t.Fatal(err)
	}
	if c, err := s.PolestarCredential(ctx, a, connA); err == nil {
		t.Errorf("B's credential opened in A's account: %+v", c)
	}
	// Sealed with its column too: copied into the application key's, it does not open.
	if _, err := db.Exec(ctx, `UPDATE connections SET api_key = (SELECT credential FROM connections WHERE id = $1),
		api_key_set_at = now(), api_key_last4 = 'x' WHERE account_id = $2 AND provider = 'volvo'`, connB, b); err != nil {
		t.Fatal(err)
	}
	if k, err := s.AccountKey(ctx, b); err == nil {
		t.Errorf("the credential opened as an application key: %q", k.Value)
	}
}
