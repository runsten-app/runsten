package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/platform/secretbox"
	"runsten/internal/volvo"
)

// testDatabase creates an empty database on the RUNSTEN_TEST_DATABASE_URL server
// (started by make db-up) and returns its URL. Without this variable the test is
// skipped; make test always sets it.
func testDatabase(t *testing.T) string {
	t.Helper()
	admin := os.Getenv("RUNSTEN_TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("RUNSTEN_TEST_DATABASE_URL missing (make db-up)")
	}
	ctx := context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "runsten_test_" + hex.EncodeToString(b)

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		_ = conn.Close(ctx)
	})
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dbURL := testDatabase(t)
	if _, err := Migrate(context.Background(), dbURL); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New(bytes.Repeat([]byte{7}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), dbURL, box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, dbURL
}

// newVehicle creates an account with a connection and a vehicle.
func newVehicle(t *testing.T, s *Store, token, vin string) (account, vehicle string) {
	t.Helper()
	ctx := context.Background()
	account, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := s.SaveConnection(ctx, account, oauth.Credentials{AccessToken: token})
	if err != nil {
		t.Fatal(err)
	}
	vehicle, err = s.AddVehicle(ctx, account, conn, vin)
	if err != nil {
		t.Fatal(err)
	}
	return account, vehicle
}

func TestMigrateIsIdempotent(t *testing.T) {
	dbURL := testDatabase(t)
	ctx := context.Background()
	first, err := Migrate(ctx, dbURL)
	if err != nil || len(first) == 0 {
		t.Fatalf("first migration: %v, %v", first, err)
	}
	again, err := Migrate(ctx, dbURL)
	if err != nil || len(again) != 0 {
		t.Fatalf("second migration: %v, %v", again, err)
	}
}

func TestTargetsAndTokens(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")

	targets, err := s.Targets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %+v", targets)
	}
	want := map[string]collector.Target{
		va: {AccountID: a, VehicleID: va, VIN: "YV1AAAAAAAAAAAAA1"},
		vb: {AccountID: b, VehicleID: vb, VIN: "YV1BBBBBBBBBBBBB1"},
	}
	conns := map[string]string{}
	for _, got := range targets {
		conns[got.AccountID] = got.ConnectionID
		got.ConnectionID = ""
		if got != want[got.VehicleID] {
			t.Errorf("target = %+v, want %+v", got, want[got.VehicleID])
		}
	}
	if c, err := s.Credentials(ctx, a, conns[a]); err != nil || c.AccessToken != "token-a" || c.RefreshToken != "" {
		t.Errorf("credentials A = %+v, %v", c, err)
	}

	// A full grant: every field survives the round trip, and no token is stored in plaintext.
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	full := oauth.Credentials{
		AccessToken: "access-a2", RefreshToken: "refresh-a2",
		ExpiresAt: t0.Add(5 * time.Minute), RefreshedAt: t0, AuthorizedAt: t0.Add(-time.Hour),
	}
	if _, err := s.SaveConnection(ctx, a, full); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Credentials(ctx, a, conns[a]); err != nil || c != full {
		t.Errorf("credentials = %+v, %v; want %+v", c, err, full)
	}
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, secret := range []string{"token-b", "access-a2", "refresh-a2"} {
		var n int
		err := conn.QueryRow(ctx, `SELECT count(*) FROM connections
			WHERE position($1::bytea in access_token) > 0 OR position($1::bytea in coalesce(refresh_token, '')) > 0`,
			[]byte(secret)).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("plaintext %s in database", secret)
		}
	}
}

func TestCredentialsLifecycle(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	account, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	creds := oauth.Credentials{AccessToken: "a1", RefreshToken: "r1", ExpiresAt: t0.Add(5 * time.Minute), RefreshedAt: t0, AuthorizedAt: t0}
	conn, err := s.SaveConnection(ctx, account, creds)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddVehicle(ctx, account, conn, "YV1AAAAAAAAAAAAA1"); err != nil {
		t.Fatal(err)
	}

	t.Run("unchanged: nothing written", func(t *testing.T) {
		err := s.UpdateCredentials(ctx, account, conn, func(c oauth.Credentials) (oauth.Credentials, bool) {
			c.AccessToken = "ignored"
			return c, false
		})
		if c, _ := s.Credentials(ctx, account, conn); err != nil || c.AccessToken != "a1" {
			t.Errorf("access token = %q, %v", c.AccessToken, err)
		}
	})
	t.Run("stale connections", func(t *testing.T) {
		for before, want := range map[time.Time]int{t0: 0, t0.Add(time.Second): 1} {
			refs, err := s.StaleConnections(ctx, before)
			if err != nil || len(refs) != want {
				t.Errorf("before %v: %+v, %v; want %d", before, refs, err, want)
			}
			if want == 1 && refs[0] != (oauth.ConnectionRef{AccountID: account, ConnectionID: conn}) {
				t.Errorf("ref = %+v", refs[0])
			}
		}
	})
	t.Run("re-authentication required, then authorized again", func(t *testing.T) {
		err := s.UpdateCredentials(ctx, account, conn, func(c oauth.Credentials) (oauth.Credentials, bool) {
			c.ReauthReason, c.ReauthAt = "refresh refused", t0.Add(time.Hour)
			return c, true
		})
		if err != nil {
			t.Fatal(err)
		}
		targets, _ := s.Targets(ctx)
		if len(targets) != 1 || targets[0].ReauthReason != "refresh refused" {
			t.Errorf("targets = %+v", targets)
		}
		if refs, _ := s.StaleConnections(ctx, t0.Add(time.Hour)); len(refs) != 0 {
			t.Errorf("a connection requiring re-authentication is kept alive: %+v", refs)
		}
		if _, err := s.SaveConnection(ctx, account, creds); err != nil {
			t.Fatal(err)
		}
		if c, _ := s.Credentials(ctx, account, conn); c.ReauthReason != "" || !c.ReauthAt.IsZero() {
			t.Errorf("re-authentication still required after a new grant: %+v", c)
		}
	})
	t.Run("unknown connection", func(t *testing.T) {
		const unknown = "00000000-0000-0000-0000-000000000000"
		if _, err := s.Credentials(ctx, account, unknown); err == nil {
			t.Error("credentials of an unknown connection")
		}
		called := false
		err := s.UpdateCredentials(ctx, account, unknown, func(c oauth.Credentials) (oauth.Credentials, bool) {
			called = true
			return c, false
		})
		if err == nil || called {
			t.Errorf("update of an unknown connection: %v, fn called: %v", err, called)
		}
	})
}

func TestSingleAccount(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	first, err := s.SingleAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.SingleAccount(ctx)
	if err != nil || again != first {
		t.Fatalf("second call: %q, %v; want %q", again, err, first)
	}
	if _, err := s.CreateAccount(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SingleAccount(ctx); err == nil {
		t.Error("multi-account instance: error expected")
	}
}

func TestSaveSnapshotDeduplicates(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

	snap := func(at time.Time, value int) collector.Snapshot {
		raw := []byte(`{"data":{"odometer":{"timestamp":"` + at.Format(time.RFC3339) + `","unit":"km","value":` + strconv.Itoa(value) + `}}}`)
		h, err := volvo.Fingerprint(raw)
		if err != nil {
			t.Fatal(err)
		}
		return collector.Snapshot{AccountID: a, VehicleID: v, Endpoint: volvo.Odometer, FetchedAt: at, Payload: raw, Hash: h}
	}
	steps := []struct {
		at     time.Time
		value  int
		stored bool
	}{
		{t0, 100, true},
		{t0.Add(time.Minute), 100, false}, // same value, different timestamp
		{t0.Add(2 * time.Minute), 101, true},
		{t0.Add(3 * time.Minute), 100, true}, // back to an earlier value: new row
		{t0.Add(3 * time.Minute), 102, false},
	}
	for i, st := range steps {
		stored, err := s.SaveSnapshot(ctx, snap(st.at, st.value))
		if err != nil || stored != st.stored {
			t.Fatalf("step %d: stored = %v, %v; want %v", i, stored, err, st.stored)
		}
	}

	rows, checkedAt := 0, time.Time{}
	err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM snapshots").Scan(&rows); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT checked_at FROM snapshots WHERE fetched_at = $1", t0).Scan(&checkedAt)
	})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 3 || !checkedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("rows = %d, checked_at = %v", rows, checkedAt)
	}
}

// TestAccountIsolation checks RLS: an account can neither see nor modify another
// account's data, even without a filter in the query.
func TestAccountIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	raw := []byte(`{"data":{}}`)
	h, _ := volvo.Fingerprint(raw)
	now := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	for _, sn := range []collector.Snapshot{
		{AccountID: a, VehicleID: va, Endpoint: volvo.Odometer, FetchedAt: now, Payload: raw, Hash: h},
		{AccountID: b, VehicleID: vb, Endpoint: volvo.Odometer, FetchedAt: now, Payload: raw, Hash: h},
	} {
		if _, err := s.SaveSnapshot(ctx, sn); err != nil {
			t.Fatal(err)
		}
	}

	count := func(account, table string) int {
		t.Helper()
		var n int
		err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, table := range []string{"accounts", "connections", "vehicles", "snapshots"} {
		if got := count(a, table); got != 1 {
			t.Errorf("account A, %s: %d rows visible, want 1", table, got)
		}
	}

	t.Run("with no account set, nothing is visible", func(t *testing.T) {
		var n int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM snapshots").Scan(&n); err != nil || n != 0 {
			t.Errorf("visible snapshots: %d, %v", n, err)
		}
	})
	t.Run("write into another account refused", func(t *testing.T) {
		_, err := s.SaveSnapshot(ctx, collector.Snapshot{
			AccountID: a, VehicleID: vb, Endpoint: volvo.Doors, FetchedAt: now, Payload: raw, Hash: h,
		})
		if err == nil {
			t.Error("snapshot written to another account's vehicle")
		}
		err = s.inAccount(ctx, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO snapshots (account_id, vehicle_id, endpoint, fetched_at, checked_at, value_hash, payload) VALUES ($1, $2, 'doors', now(), now(), '', '{}')", b, vb)
			return err
		})
		if err == nil {
			t.Error("insert with another account's account_id accepted")
		}
	})
	t.Run("modifying another account has no effect", func(t *testing.T) {
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, "DELETE FROM vehicles WHERE id = $1", vb)
			if err == nil && tag.RowsAffected() != 0 {
				t.Error("another account's vehicle deleted")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if count(b, "vehicles") != 1 {
			t.Error("vehicle B gone")
		}
	})
	t.Run("credentials of another account unreachable", func(t *testing.T) {
		targets, err := s.Targets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var connB string
		for _, tg := range targets {
			if tg.AccountID == b {
				connB = tg.ConnectionID
			}
		}
		if c, err := s.Credentials(ctx, a, connB); err == nil {
			t.Errorf("account A read B's credentials: %+v", c)
		}
		called := false
		err = s.UpdateCredentials(ctx, a, connB, func(oauth.Credentials) (oauth.Credentials, bool) {
			called = true
			return oauth.Credentials{AccessToken: "stolen"}, true
		})
		if err == nil || called {
			t.Errorf("account A updated B's credentials: %v, fn called: %v", err, called)
		}
		if c, err := s.Credentials(ctx, b, connB); err != nil || c.AccessToken != "token-b" {
			t.Errorf("credentials B = %+v, %v", c, err)
		}
	})
	t.Run("no RLS bypass", func(t *testing.T) {
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SET LOCAL row_security = off")
			if err != nil {
				return err
			}
			var n int
			return tx.QueryRow(ctx, "SELECT count(*) FROM snapshots").Scan(&n)
		})
		if err == nil {
			t.Error("row_security = off accepted for the application role")
		}
	})
}

func TestOpenErrors(t *testing.T) {
	box, _ := secretbox.New(bytes.Repeat([]byte{7}, secretbox.KeySize))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Open(ctx, "::", box); err == nil {
		t.Error("invalid URL accepted")
	}
	if _, err := Open(ctx, "postgres://nobody@127.0.0.1:1/x?connect_timeout=1", box); err == nil {
		t.Error("unreachable database: error expected")
	}
	if _, err := Migrate(ctx, "postgres://nobody@127.0.0.1:1/x?connect_timeout=1"); err == nil {
		t.Error("migration on unreachable database: error expected")
	}
}

// rotatingProvider is a token endpoint with refresh token rotation: a refresh token
// works once. It is slow, so that concurrent refreshers overlap.
type rotatingProvider struct {
	mu      sync.Mutex
	current string
	calls   int
	n       int
}

func (p *rotatingProvider) Refresh(_ context.Context, refreshToken string) (oauth.Grant, error) {
	time.Sleep(200 * time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if refreshToken != p.current {
		return oauth.Grant{}, &oauth.ReauthError{Reason: "invalid_grant: refresh token already used"}
	}
	p.n++
	p.current = "refresh-" + strconv.Itoa(p.n)
	return oauth.Grant{AccessToken: "access-" + strconv.Itoa(p.n), RefreshToken: p.current, ExpiresIn: 5 * time.Minute}, nil
}

// TestConcurrentRefresh: two collectors (two managers, two connections from the pool)
// need a token at the same time. The row lock makes a single one refresh; the other
// one waits and uses the refreshed token. Without the lock, the second refresh would
// present a rotated refresh token and lose the grant.
func TestConcurrentRefresh(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	account, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := s.SaveConnection(ctx, account, oauth.Credentials{
		AccessToken: "access-0", RefreshToken: "refresh-0", ExpiresAt: t0, RefreshedAt: t0.Add(-time.Hour), AuthorizedAt: t0.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &rotatingProvider{current: "refresh-0"}
	clk := clock.NewManual(t0)

	const refreshers = 4
	tokens := make([]string, refreshers)
	errs := make([]error, refreshers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range refreshers {
		m := oauth.NewManager(s, provider, clk, oauth.DefaultParams(), quietLog)
		wg.Go(func() {
			<-start
			tokens[i], errs[i] = m.Token(ctx, account, conn)
		})
	}
	close(start)
	wg.Wait()

	for i := range refreshers {
		if errs[i] != nil || tokens[i] != "access-1" {
			t.Errorf("refresher %d: %q, %v; want access-1", i, tokens[i], errs[i])
		}
	}
	if provider.calls != 1 {
		t.Errorf("%d refreshes, want 1", provider.calls)
	}
	c, err := s.Credentials(ctx, account, conn)
	if err != nil || c.RefreshToken != "refresh-1" || c.ReauthReason != "" {
		t.Errorf("stored credentials = %+v, %v", c, err)
	}
}
