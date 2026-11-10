package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// memAccountExports keeps the accounts' exports in memory, as the store does: one per
// account, its archive held once built.
type memAccountExports struct {
	mu sync.Mutex
	ex map[string]AccountExport // by account, its only export
	// err fails the writes, readErr the reads, as a store the database.
	err, readErr error
}

func (m *memAccountExports) CreateAccountExport(_ context.Context, accountID string, at time.Time) (AccountExport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return AccountExport{}, m.err
	}
	if e := m.ex[accountID]; e.Status == ExportPending || e.Status == ExportRunning {
		return AccountExport{}, ErrExportActive
	}
	if m.ex == nil {
		m.ex = map[string]AccountExport{}
	}
	e := AccountExport{
		ID:     fmt.Sprintf("6f0e2d3e-0000-4000-8000-%012d", len(m.ex)+1),
		Status: ExportPending, CreatedAt: at, NotBefore: at,
	}
	m.ex[accountID] = e
	return e, nil
}

func (m *memAccountExports) LatestAccountExport(_ context.Context, accountID string) (AccountExport, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.ex[accountID]
	if m.readErr != nil {
		return AccountExport{}, false, m.readErr
	}
	return e, ok, nil
}

// memArchives keeps the archive of every export done: a file, or a bucket's link when
// link is set.
type memArchives struct {
	mu      sync.Mutex
	zip     []byte
	link    string
	missing bool // the archive is gone, its export still done
	err     error
	opened  []string // the file names asked for
}

func (m *memArchives) Open(_ context.Context, _, _, filename string) (io.ReadSeekCloser, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opened = append(m.opened, filename)
	switch {
	case m.err != nil:
		return nil, "", m.err
	case m.missing:
		return nil, "", fs.ErrNotExist
	case m.link != "":
		return nil, m.link, nil
	}
	return nopSeekCloser{strings.NewReader(string(m.zip))}, "", nil
}

type nopSeekCloser struct{ io.ReadSeeker }

func (nopSeekCloser) Close() error { return nil }

// done ends the account's export as the worker does: its archive is held.
func (m *memAccountExports) done(accountID string, bytes int64, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.ex[accountID]
	e.Status, e.Bytes, e.EndedAt, e.ExpiresAt = ExportDone, bytes, at, at.Add(7*24*time.Hour)
	m.ex[accountID] = e
}

const exportZip = "PK\x03\x04the account's archive" // opaque bytes: what the worker built

func TestAccountExportJob(t *testing.T) {
	e := newEnv(t, 10)
	e.archives.zip = []byte(exportZip)
	c := e.session(t, "admin")
	logs := logged(e)
	base := e.api.URL + "/api/v1/account"

	// None yet, nothing held.
	resp, body := get(t, noFollow(), base+"/export-job", c)
	if resp.status != http.StatusOK || strings.TrimSpace(body) != `{"export":null}` {
		t.Fatalf("before any export: %d %s", resp.status, body)
	}
	if resp, body = get(t, noFollow(), base+"/export.zip", c); resp.status != http.StatusNotFound {
		t.Fatalf("nothing held: %d %s", resp.status, body)
	}
	wantError(t, body, codeNotFound)

	// Started: the export is read back as it was created.
	resp, body = do(t, noFollow(), http.MethodPost, base+"/export-job", jsonType, "", c)
	if resp.status != http.StatusAccepted {
		t.Fatalf("start: %d %s", resp.status, body)
	}
	if _, read := get(t, noFollow(), base+"/export-job", c); read != `{"export":`+strings.TrimSpace(body)+"}\n" {
		t.Errorf("read back: %s, created: %s", read, body)
	}

	// One export at a time.
	if resp, body = do(t, noFollow(), http.MethodPost, base+"/export-job", jsonType, "", c); resp.status != http.StatusConflict {
		t.Fatalf("start again: %d %s", resp.status, body)
	}
	wantError(t, body, codeExportActive)

	// The worker built the archive: it is held, its size read with its status.
	at := e.clk.Now().UTC().Truncate(time.Microsecond)
	e.exports.done(account, int64(len(exportZip)), at)
	_, read := get(t, noFollow(), base+"/export-job", c)
	for _, want := range []string{`"status":"done"`, fmt.Sprintf(`"bytes":%d`, len(exportZip))} {
		if !strings.Contains(read, want) {
			t.Errorf("done: %s misses %s", read, want)
		}
	}

	if !strings.Contains(read, `"expires_at":"`+at.Add(7*24*time.Hour).Format(time.RFC3339Nano)+`"`) {
		t.Errorf("done: %s misses its expiry", read)
	}

	// Downloads come as an attachment, named after the day of the build.
	resp, body = get(t, noFollow(), base+"/export.zip", c)
	if resp.status != http.StatusOK || body != exportZip {
		t.Fatalf("download: %d %q", resp.status, body)
	}
	if ct := resp.header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("download content type: %s", ct)
	}
	if cd, want := resp.header.Get("Content-Disposition"), `attachment; filename="runsten-account-`+at.Format(time.DateOnly)+`.zip"`; cd != want {
		t.Errorf("download name: %s, want %s", cd, want)
	}
	etag := resp.header.Get("ETag")
	if etag == "" || resp.header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("download: ETag %q, Accept-Ranges %q", etag, resp.header.Get("Accept-Ranges"))
	}

	// A download cut short resumes where it stopped, of the same archive.
	resumed := func(rangeHeader, ifRange string) (reply, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/export.zip", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(c)
		req.Header.Set("Range", rangeHeader)
		if ifRange != "" {
			req.Header.Set("If-Range", ifRange)
		}
		r, err := noFollow().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		return reply{status: r.StatusCode, header: r.Header}, string(b)
	}
	if r, b := resumed("bytes=4-", etag); r.status != http.StatusPartialContent || b != exportZip[4:] ||
		r.header.Get("Content-Range") != fmt.Sprintf("bytes 4-%d/%d", len(exportZip)-1, len(exportZip)) {
		t.Errorf("resumed: %d %q, %s", r.status, b, r.header.Get("Content-Range"))
	}
	// Of another archive (another ETag), the whole of the one held.
	if r, b := resumed("bytes=4-", `"another"`); r.status != http.StatusOK || b != exportZip {
		t.Errorf("resumed of another archive: %d %q", r.status, b)
	}

	// Another account's export is not this one's.
	if resp, body = get(t, noFollow(), base+"/export.zip", e.session(t, "other")); resp.status != http.StatusNotFound {
		t.Fatalf("another account: %d %s", resp.status, body)
	}

	// Asking again replaces the export held by the account.
	if resp, body = do(t, noFollow(), http.MethodPost, base+"/export-job", jsonType, "", c); resp.status != http.StatusAccepted {
		t.Fatalf("start again once done: %d %s", resp.status, body)
	}
	if _, read = get(t, noFollow(), base+"/export-job", c); !strings.Contains(read, `"status":"pending"`) {
		t.Errorf("a new export: %s", read)
	}

	// A failure of the store is an internal error, without detail.
	e.exports.err = errors.New("boom")
	if resp, body = do(t, noFollow(), http.MethodPost, base+"/export-job", jsonType, "", c); resp.status != http.StatusInternalServerError {
		t.Fatalf("create failed: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)
	e.exports.err = nil
	e.exports.readErr = errors.New("boom")
	if resp, body = get(t, noFollow(), base+"/export-job", c); resp.status != http.StatusInternalServerError {
		t.Fatalf("read failed: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)
	if resp, body = get(t, noFollow(), base+"/export.zip", c); resp.status != http.StatusInternalServerError {
		t.Fatalf("read failed: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)
	e.exports.readErr = nil

	// The export done again: its archive may be gone, or the archives may fail.
	e.exports.done(account, int64(len(exportZip)), at)
	e.archives.missing = true
	if resp, body = get(t, noFollow(), base+"/export.zip", c); resp.status != http.StatusNotFound {
		t.Fatalf("archive gone: %d %s", resp.status, body)
	}
	e.archives.missing, e.archives.err = false, errors.New("disk full")
	if resp, body = get(t, noFollow(), base+"/export.zip", c); resp.status != http.StatusInternalServerError {
		t.Fatalf("archives failed: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)
	e.archives.err = nil

	// Archives kept in a bucket: the client is sent to its link, the file named after
	// the build's day.
	e.archives.link = "https://bucket.example/exports/archive.zip?X-Amz-Signature=abc"
	resp, _ = get(t, noFollow(), base+"/export.zip", c)
	if resp.status != http.StatusFound || resp.header.Get("Location") != e.archives.link || resp.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("bucket: %d %s", resp.status, resp.header.Get("Location"))
	}
	if name := e.archives.opened[len(e.archives.opened)-1]; name != "runsten-account-"+at.Format(time.DateOnly)+".zip" {
		t.Errorf("the link's name: %s", name)
	}

	for _, want := range []string{"account export started", "account export downloaded"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs miss %q", want)
		}
	}
}
