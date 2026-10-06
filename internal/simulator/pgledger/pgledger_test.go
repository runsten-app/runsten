package pgledger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/simulator/volvoapi"
)

// testDatabase creates an empty database on the RUNSTEN_TEST_DATABASE_URL server and
// returns its URL. Without this variable the test is skipped.
func testDatabase(t *testing.T) string {
	t.Helper()
	admin := os.Getenv("RUNSTEN_TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("RUNSTEN_TEST_DATABASE_URL missing (task db-up)")
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

func open(t *testing.T, dbURL string) *Ledger {
	t.Helper()
	l, err := Open(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l
}

func TestLedger(t *testing.T) {
	dbURL := testDatabase(t)
	ctx := t.Context()
	l := open(t, dbURL)
	t0 := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

	g := volvoapi.Grant{ID: "g1", AuthorizedAt: t0, Scope: "openid conve:odometer_status"}
	a1 := volvoapi.AccessToken{Token: "a1", Grant: "g1", Expires: t0.Add(30 * time.Minute)}
	r1 := volvoapi.RefreshToken{Token: "r1", Grant: "g1", Issued: t0}
	if err := l.Issue(ctx, g, "", a1, r1); err != nil {
		t.Fatal(err)
	}
	a2 := volvoapi.AccessToken{Token: "a2", Grant: "g1", Expires: t0.Add(time.Hour)}
	r2 := volvoapi.RefreshToken{Token: "r2", Grant: "g1", Issued: t0.Add(30 * time.Minute)}
	if err := l.Issue(ctx, g, "r1", a2, r2); err != nil {
		t.Fatal(err)
	}
	if err := l.Revoke(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	if err := l.Issue(ctx, g, "", a1, volvoapi.RefreshToken{Token: "r3", Grant: "g1", Issued: t0}); err == nil {
		t.Error("access token issued twice: no error")
	}

	// Another simulator, on the same database, reads it all back.
	got, err := open(t, dbURL).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r1.Used = true
	g.Revoked = true
	want := volvoapi.Issued{
		Grants:  []volvoapi.Grant{g},
		Access:  []volvoapi.AccessToken{a1, a2},
		Refresh: []volvoapi.RefreshToken{r1, r2},
	}
	for _, is := range []*volvoapi.Issued{&got, &want} {
		for i := range is.Grants {
			is.Grants[i].AuthorizedAt = is.Grants[i].AuthorizedAt.UTC()
		}
		for i := range is.Access {
			is.Access[i].Expires = is.Access[i].Expires.UTC()
		}
		for i := range is.Refresh {
			is.Refresh[i].Issued = is.Refresh[i].Issued.UTC()
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load = %+v\nwant %+v", got, want)
	}
}
