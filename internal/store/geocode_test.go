package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/core"
	"runsten/internal/geocode"
)

// TestGeocoding follows the cells of two accounts: asked for by a read, claimed across
// the accounts, oldest first, claimed again once their lease is over, given up after
// the attempts, resolved within their account and never seen by the other.
func TestGeocoding(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	home, work := core.GeoCell{LatE4: 457640, LonE4: 48357}, core.GeoCell{LatE4: 457797, LonE4: 49270}
	at := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	const lease, attempts = 10 * time.Minute, 2

	addresses := func(account string, cells ...core.GeoCell) map[core.GeoCell]string {
		t.Helper()
		got, err := s.Addresses(ctx, account, cells)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	claim := func(at time.Time) (geocode.Claim, bool) {
		t.Helper()
		c, ok, err := s.ClaimGeocoding(ctx, at, lease, attempts)
		if err != nil {
			t.Fatal(err)
		}
		return c, ok
	}

	if got := addresses(a); len(got) != 0 {
		t.Errorf("no cells: %v", got)
	}
	// A asks first, B then; asking again changes nothing.
	if got := addresses(a, home, work); len(got) != 0 {
		t.Errorf("unknown yet: %v", got)
	}
	addresses(b, home)
	addresses(a, home)

	c1, ok := claim(at)
	if !ok || c1 != (geocode.Claim{AccountID: a, Cell: home}) {
		t.Fatalf("first claim %+v, %v: A's home, the oldest", c1, ok)
	}
	c2, _ := claim(at)
	c3, _ := claim(at)
	aWork, bHome := geocode.Claim{AccountID: a, Cell: work}, geocode.Claim{AccountID: b, Cell: home}
	if got := []geocode.Claim{c2, c3}; !reflect.DeepEqual(got, []geocode.Claim{aWork, bHome}) &&
		!reflect.DeepEqual(got, []geocode.Claim{bHome, aWork}) {
		t.Errorf("claims %+v: A's work and B's home", got)
	}
	if _, ok := claim(at.Add(lease - time.Second)); ok {
		t.Error("claimed within the lease")
	}

	if err := s.ResolveAddress(ctx, c1, "Rue de la République, Lyon", at); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveAddress(ctx, geocode.Claim{AccountID: b, Cell: home}, "", at); err != nil {
		t.Fatal(err)
	}
	if got := addresses(a, home, work); !reflect.DeepEqual(got, map[core.GeoCell]string{home: "Rue de la République, Lyon"}) {
		t.Errorf("A's addresses %v", got)
	}
	if got := addresses(b, home); len(got) != 0 {
		t.Errorf("B's addresses %v: its home has none, A's is not its own", got)
	}

	// A's work, unresolved, is claimed again after its lease, once: then given up.
	if c, ok := claim(at.Add(lease)); !ok || c != (geocode.Claim{AccountID: a, Cell: work}) {
		t.Errorf("after the lease: %+v, %v", c, ok)
	}
	if c, ok := claim(at.Add(3 * lease)); ok {
		t.Errorf("claimed after %d attempts: %+v", attempts, c)
	}

	// Isolation: each account sees its own cells.
	count := func(account string) int {
		t.Helper()
		var n int
		if err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM geocoded_positions").Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(a) != 2 || count(b) != 1 {
		t.Errorf("visible cells: A %d, B %d", count(a), count(b))
	}
}
