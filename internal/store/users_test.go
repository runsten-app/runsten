package store

import (
	"context"
	"crypto/rand"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/auth"
	"runsten/internal/core"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
)

const password = "correct horse battery staple"

func authService(s *Store, clk clock.Clock) *auth.Service {
	p := auth.DefaultParams()
	p.Hash = auth.HashParams{MemoryKiB: 64, Iterations: 1, Threads: 1, KeyLen: 32} // fast, for tests only
	return auth.New(s, clk, p, rand.Reader)
}

// TestAuthOnPostgres runs the authentication against the real store.
func TestAuthOnPostgres(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	clk := clock.NewManual(t0)
	svc := authService(s, clk)

	if ok, err := s.HasUsers(ctx); err != nil || ok {
		t.Fatalf("users before any: %v, %v", ok, err)
	}
	account, err := s.SingleAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, account, "Admin", password); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.HasUsers(ctx); err != nil || !ok {
		t.Errorf("users after one: %v, %v", ok, err)
	}
	other, _ := s.CreateAccount(ctx)
	for _, tt := range []struct {
		account, name string
		want          error
	}{
		{other, "admin", auth.ErrUsernameTaken},
		{account, "second", auth.ErrAccountHasUser},
	} {
		if _, err := svc.CreateUser(ctx, tt.account, tt.name, password); !errors.Is(err, tt.want) {
			t.Errorf("CreateUser(%s): %v, want %v", tt.name, err, tt.want)
		}
	}
	if _, err := s.CreateUser(ctx, account, "Upper", "x"); err == nil {
		t.Error("a username that is not normalized was stored")
	}

	token, sess, err := svc.Login(ctx, "admin", password, "c")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Authenticate(ctx, token)
	if err != nil || got.ID != sess.ID || got.AccountID != account || got.Username != "admin" ||
		!got.ExpiresAt.Equal(sess.ExpiresAt) || !got.CreatedAt.Equal(t0) {
		t.Fatalf("session %+v, %v", got, err)
	}
	clk.Advance(2 * time.Hour)
	if got, _ := svc.Authenticate(ctx, token); !got.LastSeenAt.Equal(clk.Now()) {
		t.Errorf("activity not recorded: %+v", got)
	}
	var stored []byte
	err = s.inAccount(ctx, account, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT token_hash FROM sessions WHERE id = $1", sess.ID).Scan(&stored)
	})
	if err != nil || len(stored) != 32 || string(stored) == token {
		t.Errorf("stored token: %x, %v", stored, err)
	}

	t.Run("logout", func(t *testing.T) {
		token, sess, _ := svc.Login(ctx, "admin", password, "c")
		if err := svc.Logout(ctx, sess); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrNoSession) {
			t.Errorf("after logout: %v", err)
		}
	})
	t.Run("new password: all sessions end", func(t *testing.T) {
		if err := svc.SetPassword(ctx, "admin", "another long passphrase"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrNoSession) {
			t.Errorf("old session: %v", err)
		}
		if _, _, err := svc.Login(ctx, "admin", "another long passphrase", "c"); err != nil {
			t.Errorf("new password: %v", err)
		}
		if err := s.SetPassword(ctx, account, other, "x"); err == nil {
			t.Error("password set for a missing user")
		}
	})
	t.Run("expired sessions are purged at the next login", func(t *testing.T) {
		clk.Advance(auth.DefaultParams().IdleTTL + time.Minute)
		if _, _, err := svc.Login(ctx, "admin", "another long passphrase", "c"); err != nil {
			t.Fatal(err)
		}
		var n int
		err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&n)
		})
		if err != nil || n != 1 {
			t.Errorf("%d sessions, %v", n, err)
		}
	})
}

// TestUserIsolation: users and sessions follow the account isolation of the other
// tables. Only the two lookup functions read across accounts, one row at a time.
func TestUserIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	svc := authService(s, clock.NewManual(t0))
	a, _ := s.CreateAccount(ctx)
	b, _ := s.CreateAccount(ctx)
	for acc, name := range map[string]string{a: "alice", b: "bob"} {
		if _, err := svc.CreateUser(ctx, acc, name, password); err != nil {
			t.Fatal(err)
		}
	}
	tokenB, sessB, err := svc.Login(ctx, "bob", password, "c")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(ctx, "alice", password, "c"); err != nil {
		t.Fatal(err)
	}
	bob, _, _ := s.UserByName(ctx, "bob")
	alice, _, _ := s.UserByName(ctx, "alice")
	secretB, tokB, err := svc.IssueToken(ctx, b, bob.ID, "bob's script", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.IssueToken(ctx, a, alice.ID, "alice's script", 0); err != nil {
		t.Fatal(err)
	}
	tables := []string{"users", "sessions", "api_tokens"}
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
	for _, table := range tables {
		if got := count(a, table); got != 1 {
			t.Errorf("account A, %s: %d rows visible, want 1", table, got)
		}
	}
	t.Run("with no account set, nothing is visible", func(t *testing.T) {
		for _, table := range tables {
			var n int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("%s: %d visible, %v", table, n, err)
			}
		}
	})
	t.Run("a token leads to its own account only", func(t *testing.T) {
		got, err := svc.Authenticate(ctx, tokenB)
		if err != nil || got.AccountID != b || got.Username != "bob" {
			t.Errorf("session B = %+v, %v", got, err)
		}
		tok, err := svc.AuthenticateToken(ctx, secretB)
		if err != nil || tok.AccountID != b || tok.UserID != bob.ID {
			t.Errorf("access token B = %+v, %v", tok, err)
		}
	})
	t.Run("another account's user cannot be changed", func(t *testing.T) {
		if err := s.SetPassword(ctx, a, bob.ID, "stolen"); err == nil {
			t.Error("password of another account's user changed")
		}
		if _, err := s.CreateSession(ctx, auth.Session{
			AccountID: a, UserID: bob.ID, CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0.Add(time.Hour),
		}, make([]byte, 32)); err == nil {
			t.Error("session opened in account A for B's user")
		}
		if _, err := s.CreateToken(ctx, auth.Token{AccountID: a, UserID: bob.ID, Name: "x", CreatedAt: t0}, make([]byte, 32)); err == nil {
			t.Error("access token issued in account A for B's user")
		}
		for _, table := range tables {
			err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, "UPDATE "+table+" SET account_id = $1", b)
				return err
			})
			if err == nil {
				t.Errorf("%s: rows moved to another account", table)
			}
		}
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO users (account_id, username, password_hash) VALUES ($1, 'mallory', 'x')", b)
			return err
		})
		if err == nil {
			t.Error("user created in another account")
		}
	})
	t.Run("another account's sessions cannot be ended or touched", func(t *testing.T) {
		if err := s.DeleteSession(ctx, a, sessB.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.TouchSession(ctx, a, sessB.ID, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := s.PurgeSessions(ctx, a, t0.Add(1000*time.Hour), t0.Add(1000*time.Hour)); err != nil {
			t.Fatal(err)
		}
		got, err := svc.Authenticate(ctx, tokenB)
		if err != nil || !got.LastSeenAt.Equal(t0) {
			t.Errorf("session B = %+v, %v", got, err)
		}
	})
	t.Run("another account's access tokens cannot be listed, revoked or touched", func(t *testing.T) {
		if ts, err := s.ListTokens(ctx, a, bob.ID); err != nil || len(ts) != 0 {
			t.Errorf("B's tokens listed from A: %+v, %v", ts, err)
		}
		if ok, err := s.DeleteToken(ctx, a, bob.ID, tokB.ID); err != nil || ok {
			t.Errorf("B's token revoked from A: %v, %v", ok, err)
		}
		if ok, err := s.DeleteToken(ctx, b, alice.ID, tokB.ID); err != nil || ok {
			t.Errorf("B's token revoked by another user: %v, %v", ok, err)
		}
		if err := s.TouchToken(ctx, a, tokB.ID, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		ts, err := s.ListTokens(ctx, b, bob.ID)
		if err != nil || len(ts) != 1 || !ts[0].LastUsedAt.Equal(t0) {
			t.Errorf("B's tokens = %+v, %v", ts, err)
		}
	})
	t.Run("deleting from another account has no effect", func(t *testing.T) {
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			for _, table := range []string{"api_tokens", "sessions", "users"} {
				if _, err := tx.Exec(ctx, "DELETE FROM "+table); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range tables {
			if count(b, table) != 1 {
				t.Errorf("%s of account B gone", table)
			}
		}
	})
}

// TestAccessTokensOnPostgres runs the access tokens against the real store.
func TestAccessTokensOnPostgres(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	clk := clock.NewManual(t0)
	svc := authService(s, clk)
	account, _ := s.SingleAccount(ctx)
	if _, err := svc.CreateUser(ctx, account, "admin", password); err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.UserByName(ctx, "admin")

	forever, _, err := svc.IssueToken(ctx, account, u.ID, "Home Assistant", 0)
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Minute)
	month, issued, err := svc.IssueToken(ctx, account, u.ID, "Claude Code", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.AuthenticateToken(ctx, month)
	if err != nil || got != (auth.Token{
		ID: issued.ID, AccountID: account, UserID: u.ID, Name: "Claude Code",
		CreatedAt: t0.Add(time.Minute), LastUsedAt: t0.Add(time.Minute), ExpiresAt: t0.Add(time.Minute + 30*24*time.Hour),
	}) {
		t.Errorf("token = %+v, %v", got, err)
	}
	ts, err := svc.Tokens(ctx, account, u.ID)
	if err != nil || len(ts) != 2 || ts[0].Name != "Claude Code" || ts[1].Name != "Home Assistant" ||
		!ts[1].LastUsedAt.IsZero() || !ts[1].ExpiresAt.IsZero() {
		t.Errorf("tokens = %+v, %v", ts, err)
	}
	if err := svc.RevokeToken(ctx, account, u.ID, "not-a-uuid"); !errors.Is(err, auth.ErrUnknownToken) {
		t.Errorf("revoke a malformed ID: %v", err)
	}
	if err := svc.RevokeToken(ctx, account, u.ID, issued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, month); !errors.Is(err, auth.ErrNoToken) {
		t.Errorf("revoked token: %v", err)
	}

	// A new password leaves the tokens; the user's deletion takes them.
	if err := svc.SetPassword(ctx, "admin", "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, forever); err != nil {
		t.Errorf("after a new password: %v", err)
	}
	err = s.inAccount(ctx, account, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM users")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, forever); !errors.Is(err, auth.ErrNoToken) {
		t.Errorf("after the user's deletion: %v", err)
	}
}

// TestReads: the API reads, restricted to the account.
func TestReads(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")

	vs, err := s.Vehicles(ctx, a)
	if err != nil || len(vs) != 1 || vs[0].ID != va || vs[0].VIN != "YV1AAAAAAAAAAAAA1" || vs[0].ReauthReason != "" {
		t.Fatalf("vehicles = %+v, %v", vs, err)
	}
	conn := connectionOf(t, s, b)
	err = s.UpdateCredentials(ctx, b, conn, func(c oauth.Credentials) (oauth.Credentials, bool) {
		c.AuthorizedAt, c.ReauthAt, c.ReauthReason = t0, t0.Add(time.Hour), "grant ended"
		return c, true
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Vehicle(ctx, b, vb); err != nil || !ok || !reflect.DeepEqual(v, api.Vehicle{
		ID: vb, VIN: "YV1BBBBBBBBBBBBB1", AuthorizedAt: t0, ReauthAt: t0.Add(time.Hour), ReauthReason: "grant ended",
	}) {
		t.Errorf("vehicle B = %+v, %v, %v", v, ok, err)
	}
	if v, ok, err := s.Vehicle(ctx, a, vb); err != nil || ok {
		t.Errorf("account A reads vehicle B: %+v, %v", v, err)
	}

	// Three days of the sample: trips at t0, t0+1d, t0+2d; charges an hour later.
	for i := range 3 {
		day := t0.Add(time.Duration(i) * 24 * time.Hour)
		if err := s.SaveDerivation(ctx, a, va, day.Add(-time.Minute), sampleResult(day)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveDerivation(ctx, b, vb, time.Time{}, sampleResult(t0)); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Trips(ctx, a, va)
	if len(all) != 3 {
		t.Fatalf("%d trips", len(all))
	}
	detected := func(trips []core.Trip) []time.Time {
		var out []time.Time
		for _, tr := range trips {
			out = append(out, tr.DetectedAt)
		}
		return out
	}
	day := func(i int) time.Time { return t0.Add(time.Duration(i) * 24 * time.Hour) }
	last := all[2]
	for _, tt := range []struct {
		name string
		q    api.EventQuery
		want []time.Time
	}{
		{"newest first", api.EventQuery{Limit: 10}, []time.Time{day(2), day(1), day(0)}},
		{"limit", api.EventQuery{Limit: 2}, []time.Time{day(2), day(1)}},
		{"no limit: every one", api.EventQuery{}, []time.Time{day(2), day(1), day(0)}},
		{"after a key", api.EventQuery{Limit: 10, After: &api.EventKey{StartedAfter: last.Start.After, DetectedAt: last.DetectedAt}}, []time.Time{day(1), day(0)}},
		{"from: ended before it, out", api.EventQuery{Limit: 10, From: day(1).Add(41 * time.Minute)}, []time.Time{day(2)}},
		{"from: may still run, in", api.EventQuery{Limit: 10, From: day(1).Add(40 * time.Minute)}, []time.Time{day(2), day(1)}},
		{"to: started after it, out", api.EventQuery{Limit: 10, To: day(1).Add(-10 * time.Minute)}, []time.Time{day(0)}},
		{"period", api.EventQuery{Limit: 10, From: day(1), To: day(1).Add(time.Hour)}, []time.Time{day(1)}},
	} {
		trips, err := s.ListTrips(ctx, a, va, tt.q)
		if err != nil || !reflect.DeepEqual(detected(trips), tt.want) {
			t.Errorf("%s: %v, %v; want %v", tt.name, detected(trips), err, tt.want)
		}
	}
	if trips, err := s.ListTrips(ctx, a, vb, api.EventQuery{Limit: 10}); err != nil || len(trips) != 0 {
		t.Errorf("account A lists B's trips: %v, %v", trips, err)
	}
	charges, err := s.ListCharges(ctx, a, va, api.EventQuery{Limit: 1, To: day(1).Add(2 * time.Hour)})
	if err != nil || len(charges.Charges) != 1 || !charges.Charges[0].DetectedAt.Equal(day(1).Add(time.Hour)) {
		t.Errorf("charges = %+v, %v", charges, err)
	}

	want := sampleResult(day(1))
	if tr, ok, err := s.FindTrip(ctx, a, va, day(1)); err != nil || !ok || !reflect.DeepEqual(tr, want.Trips[0]) {
		t.Errorf("trip = %+v, %v, %v", tr, ok, err)
	}
	if c, ok, err := s.FindCharge(ctx, a, va, day(1).Add(time.Hour)); err != nil || !ok || !reflect.DeepEqual(c.Charges, want.Charges[:1]) {
		t.Errorf("charge = %+v, %v, %v", c, ok, err)
	}
	for _, at := range []time.Time{day(1).Add(time.Microsecond), day(5)} {
		if _, ok, err := s.FindTrip(ctx, a, va, at); ok || err != nil {
			t.Errorf("trip at %s: %v, %v", at, ok, err)
		}
		if _, ok, err := s.FindCharge(ctx, a, va, at); ok || err != nil {
			t.Errorf("charge at %s: %v, %v", at, ok, err)
		}
	}
	if _, ok, err := s.FindTrip(ctx, a, vb, t0); ok || err != nil {
		t.Errorf("account A finds B's trip: %v, %v", ok, err)
	}
}

// connectionOf returns the connection of the account's single vehicle.
func connectionOf(t *testing.T, s *Store, account string) string {
	t.Helper()
	targets, err := s.Targets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range targets {
		if tg.AccountID == account {
			return tg.ConnectionID
		}
	}
	t.Fatalf("no connection for %s", account)
	return ""
}
