package archive

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"testing"
)

const (
	account = "6f0e2d3e-0000-4000-8000-000000000001"
	export  = "6f0e2d3e-0000-4000-8000-0000000000e1"
	other   = "6f0e2d3e-0000-4000-8000-0000000000e2"
)

// store is what the export worker and the API ask of both kinds.
type store interface {
	Create(ctx context.Context, accountID, exportID string) (io.WriteCloser, error)
	Open(ctx context.Context, accountID, exportID, filename string) (io.ReadSeekCloser, string, error)
	Remove(ctx context.Context, accountID, exportID string) error
	List(ctx context.Context) (map[string]string, error)
}

// write writes body as the export's archive; canceled, the build gives it up.
func write(t *testing.T, s store, exportID, body string, giveUp bool) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := s.Create(ctx, account, exportID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatal(err)
	}
	if giveUp {
		cancel()
	}
	return w.Close() //nolint:wrapcheck // a test's
}

// read reads the export's archive, from its file or its link.
func read(t *testing.T, s store) (string, error) {
	t.Helper()
	f, link, err := s.Open(context.Background(), account, export, "runsten-account-2026-10-11.zip")
	if err != nil {
		return "", err //nolint:wrapcheck // a test's
	}
	if f != nil {
		defer f.Close()
		if _, err := f.Seek(3, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(f)
		return string(b), err //nolint:wrapcheck // a test's
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, link, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=3-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename=runsten-account-2026-10-11.zip` {
		t.Errorf("link's name: %q", cd)
	}
	b, err := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("link: %d %s", resp.StatusCode, b)
	}
	return string(b), err //nolint:wrapcheck // a test's
}

// testStore runs the contract both kinds keep.
func testStore(t *testing.T, s store) {
	t.Helper()
	ctx := context.Background()
	if _, err := read(t, s); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("before any: %v", err)
	}
	// Given up, an archive is not the export's.
	if err := write(t, s, export, "PK half", true); err == nil {
		t.Error("an archive given up closed without error")
	}
	if _, err := read(t, s); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("given up: %v", err)
	}
	// Complete, it is read from any offset.
	body := "PK\x03\x04" + strings.Repeat("the account's archive ", 1000)
	if err := write(t, s, export, body, false); err != nil {
		t.Fatal(err)
	}
	if got, err := read(t, s); err != nil || got != body[3:] {
		t.Fatalf("read: %d bytes, %v", len(got), err)
	}
	if err := write(t, s, other, "PK other", false); err != nil {
		t.Fatal(err)
	}
	listed, err := s.List(ctx)
	if err != nil || len(listed) != 2 || listed[export] != account || listed[other] != account {
		t.Fatalf("listed %v, %v", listed, err)
	}
	// Removed, it is gone; removed again, no error.
	for range 2 {
		if err := s.Remove(ctx, account, export); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := read(t, s); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("removed: %v", err)
	}
	if listed, _ := s.List(ctx); len(listed) != 1 || listed[other] != account {
		t.Errorf("listed after the removal %v", listed)
	}
	// Only UUIDs name an archive.
	if _, err := s.Create(ctx, "../etc", export); !errors.Is(err, errID) {
		t.Errorf("a path for an account: %v", err)
	}
	if err := s.Remove(ctx, account, "*"); !errors.Is(err, errID) {
		t.Errorf("a pattern for an export: %v", err)
	}
}

func TestDir(t *testing.T) {
	d, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testStore(t, d)
}

// What a build stopped mid-way left is listed, and removed with its export.
func TestDirLeftovers(t *testing.T) {
	root := t.TempDir()
	d, err := NewDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create(context.Background(), account, export); err != nil { // never closed: a worker killed
		t.Fatal(err)
	}
	if listed, _ := d.List(context.Background()); listed[export] != account {
		t.Fatalf("a leftover not listed: %v", listed)
	}
	if err := d.Remove(context.Background(), account, export); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("left: %v", entries)
	}
}

// The S3 store runs against a bucket of RUNSTEN_TEST_S3_ENDPOINT (MinIO: its root
// user, RUNSTEN_TEST_S3_ACCESS_KEY and _SECRET_KEY, its bucket RUNSTEN_TEST_S3_BUCKET).
func TestS3(t *testing.T) {
	endpoint := os.Getenv("RUNSTEN_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("RUNSTEN_TEST_S3_ENDPOINT missing")
	}
	s, err := NewS3(S3Config{
		Endpoint: endpoint, Bucket: os.Getenv("RUNSTEN_TEST_S3_BUCKET"), Region: "us-east-1",
		AccessKey: os.Getenv("RUNSTEN_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("RUNSTEN_TEST_S3_SECRET_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for id, acc := range must(s.List(context.Background())) {
			_ = s.Remove(context.Background(), acc, id)
		}
	})
	testStore(t, s)
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestNewS3Refuses(t *testing.T) {
	for _, cfg := range []S3Config{{Endpoint: "s3.fr-par.scw.cloud", Bucket: "b"}, {Endpoint: "https://s3.fr-par.scw.cloud"}} {
		if _, err := NewS3(cfg); err == nil {
			t.Errorf("%+v accepted", cfg)
		}
	}
}
