package api

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	zip          []byte // the archive held for every export done
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

func (m *memAccountExports) OpenAccountExport(_ context.Context, accountID string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readErr != nil {
		return nil, m.readErr
	}
	e, ok := m.ex[accountID]
	if !ok || e.Status != ExportDone {
		return nil, fmt.Errorf("%w: the account's export is not held", ErrNotFound)
	}
	return io.NopCloser(strings.NewReader(string(m.zip))), nil
}

// done ends the account's export as the worker does: its archive is held.
func (m *memAccountExports) done(accountID string, bytes int64, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.ex[accountID]
	e.Status, e.Bytes, e.EndedAt = ExportDone, bytes, at
	m.ex[accountID] = e
}

const exportZip = "PK\x03\x04the account's archive" // opaque bytes: what the worker built

func TestAccountExportJob(t *testing.T) {
	e := newEnv(t, 10)
	e.exports.zip = []byte(exportZip)
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

	// Downloads come as an attachment, of the day's name.
	resp, body = get(t, noFollow(), base+"/export.zip", c)
	if resp.status != http.StatusOK || body != exportZip {
		t.Fatalf("download: %d %q", resp.status, body)
	}
	if ct := resp.header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("download content type: %s", ct)
	}
	if cd := resp.header.Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="runsten-account-`) || !strings.HasSuffix(cd, `.zip"`) {
		t.Errorf("download name: %s", cd)
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
		t.Fatalf("open failed: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)
	e.exports.readErr = nil

	for _, want := range []string{"account export started", "account export downloaded"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs miss %q", want)
		}
	}
}
