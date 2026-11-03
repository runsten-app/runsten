package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/oauth"
	"runsten/internal/platform/secretbox"
	"runsten/internal/store"
)

const fixture = "../../internal/teslamate/testdata/teslamate-v4.4.0.bck"

func TestImportTeslaMateErrors(t *testing.T) {
	db := env(map[string]string{"RUNSTEN_DATABASE_URL": "postgres://127.0.0.1:1/none?connect_timeout=1"})
	for _, tt := range []struct {
		name, want string
		args       []string
		getenv     func(string) string
	}{
		{"no VIN", "usage", nil, db},
		{"two files", "usage", []string{"--vin", "V", "a", "b"}, db},
		{"unknown flag", "usage", []string{"--vim", "V"}, db},
		{"no database", "RUNSTEN_DATABASE_URL", []string{"--vin", "V"}, env(nil)},
		{"no file", "backup", []string{"--vin", "V", "/nonexistent/teslamate.bck"}, db},
		{"database unreachable", "migrations", []string{"--vin", "V", fixture}, db},
	} {
		err := importTeslaMate(tt.args, tt.getenv, strings.NewReader(""), io.Discard)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
	if err := run([]string{"import", "teslamate"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("run: %v", err)
	}
}

// testDatabase creates an empty database on the RUNSTEN_TEST_DATABASE_URL server, as the
// store's tests do; skipped without it.
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

func TestImportTeslaMate(t *testing.T) {
	dbURL := testDatabase(t)
	ctx := context.Background()
	if _, err := store.Migrate(ctx, dbURL); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New(bytes.Repeat([]byte{7}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, dbURL, box)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	account, err := st.SingleAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tesla, err := st.SetTeslaToken(ctx, account, "refresh", "refresh", time.Now()) //nolint:forbidigo // any time
	if err != nil {
		t.Fatal(err)
	}
	vehicle, err := st.AddVehicle(ctx, account, tesla, "SYNTHETIC0000VIN1")
	if err != nil {
		t.Fatal(err)
	}
	volvo, err := st.SaveConnection(ctx, account, oauth.Credentials{AccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddVehicle(ctx, account, volvo, "YV1AAAAAAAAAAAAA1"); err != nil {
		t.Fatal(err)
	}
	getenv := env(map[string]string{"RUNSTEN_DATABASE_URL": dbURL})

	for _, tt := range []struct {
		name, want string
		args       []string
	}{
		{"a Volvo", "not read as a Tesla", []string{"--vin", "YV1AAAAAAAAAAAAA1", fixture}},
		{"a VIN of no vehicle", "no vehicle", []string{"--vin", "SYNTHETIC0000VIN2", fixture}},
		{"no such user", `no user "nobody"`, []string{"--vin", "SYNTHETIC0000VIN1", "--user", "nobody", fixture}},
		{"no such car", "only SYNTHETIC0000VIN1, SYNTHETIC0000VIN2", nil},
	} {
		args := tt.args
		stdin := io.Reader(strings.NewReader(""))
		if args == nil {
			// A vehicle whose VIN the backup lacks: the backup's VINs are named.
			other, err := st.AddVehicle(ctx, account, tesla, "SYNTHETIC0000VIN3")
			if err != nil || other == "" {
				t.Fatal(err)
			}
			args = []string{"--vin", "SYNTHETIC0000VIN3", fixture}
		}
		if err := importTeslaMate(args, getenv, stdin, io.Discard); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}

	// From stdin, as piped from TeslaMate's database.
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out bytes.Buffer
	if err := importTeslaMate([]string{"--vin", "synthetic0000vin1", "--currency", "EUR"}, getenv, f, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"saved":2`) || !strings.Contains(out.String(), `"invalid":1`) {
		t.Errorf("report %s", out.String())
	}
	trips, err := st.ListTrips(ctx, account, vehicle, api.EventQuery{})
	if err != nil || len(trips) != 1 || trips[0].Import.V.Source != "teslamate" {
		t.Errorf("trips %+v, %v", trips, err)
	}
	p, err := st.ListCharges(ctx, account, vehicle, api.EventQuery{})
	if err != nil || len(p.Charges) != 1 || p.Charges[0].PaidCost.V.Minor != 602 || !p.Charges[0].EndUsableSoC.OK {
		t.Errorf("charges %+v, %v", p.Charges, err)
	}
	if err := importTeslaMate([]string{"--vin", "SYNTHETIC0000VIN1", "--currency", "XYZ", fixture}, getenv,
		nil, io.Discard); err == nil || !strings.Contains(err.Error(), "unknown currency") {
		t.Errorf("unknown currency: %v", err)
	}
}
