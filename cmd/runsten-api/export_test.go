package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"runsten/internal/oauth"
	"runsten/internal/platform/secretbox"
	"runsten/internal/store"
)

func TestExportCommandErrors(t *testing.T) {
	db := env(map[string]string{"RUNSTEN_DATABASE_URL": "postgres://127.0.0.1:1/none?connect_timeout=1"})
	for _, tt := range []struct {
		name, want string
		args       []string
		getenv     func(string) string
	}{
		{"a user and an account", "usage", []string{"--user", "u", "--account", "a"}, db},
		{"two files", "usage", []string{"a", "b"}, db},
		{"unknown flag", "usage", []string{"--users", "u"}, db},
		{"no database", "RUNSTEN_DATABASE_URL", nil, env(nil)},
		{"database unreachable", "migrations", nil, db},
	} {
		err := exportCommand(tt.args, tt.getenv, io.Discard)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
	if err := run([]string{"export"}); err == nil { // no RUNSTEN_DATABASE_URL in the test's environment
		t.Errorf("run: %v", err)
	}
}

// TestExportCommand takes a copy of an account whole: the archive is the one the
// interface holds, of the same document again.
func TestExportCommand(t *testing.T) {
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
	volvo, err := st.SaveConnection(ctx, account, oauth.Credentials{AccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddVehicle(ctx, account, volvo, "YV1AAAAAAAAAAAAA1"); err != nil {
		t.Fatal(err)
	}
	getenv := env(map[string]string{"RUNSTEN_DATABASE_URL": dbURL})

	// The default name, in the working directory; the existing file is replaced.
	dir := t.TempDir()
	t.Chdir(dir)
	var out bytes.Buffer
	if err = exportCommand(nil, getenv, &out); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(out.String(), `"account exported"`) {
		t.Errorf("report %s", out.String())
	}
	if err = exportCommand(nil, getenv, &out); err != nil {
		t.Fatal(err) // the same name again, replaced
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "runsten-account-") ||
		!strings.HasSuffix(entries[0].Name(), ".zip") {
		t.Fatalf("entries %v, %v", entries, err)
	}
	archive := filepath.Join(dir, entries[0].Name())

	// The archive holds the account's document, of the day's name.
	r, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if len(r.File) != 1 || strings.TrimSuffix(r.File[0].Name, ".json") != strings.TrimSuffix(entries[0].Name(), ".zip") {
		t.Fatalf("entries %v", r.File)
	}
	f, err := r.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	document, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if !bytes.Contains(document, []byte(`"format":"runsten-account"`)) ||
		!bytes.Contains(document, []byte(`"YV1AAAAAAAAAAAAA1"`)) ||
		!bytes.Contains(document, []byte(account)) {
		t.Errorf("document %s…", document[:min(len(document), 200)])
	}

	// The account by its id: the hosted offer's back office, across every account.
	if err = exportCommand([]string{"--account", account, filepath.Join(dir, "other.zip")}, getenv, &out); err != nil {
		t.Fatal(err)
	}
	other, err := zip.OpenReader(filepath.Join(dir, "other.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	// Neither a user nor an account of no one.
	for _, tt := range []struct {
		name, want string
		args       []string
	}{
		{"no such user", `no user "nobody"`, []string{"--user", "nobody"}},
		{"no such account", "no account", []string{"--account", "6f0c1d2e-0000-4000-8000-000000000001"}},
	} {
		if err := exportCommand(tt.args, getenv, io.Discard); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
}
