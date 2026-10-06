package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// memAccounts is an in-memory Accounts.
type memAccounts struct {
	mu sync.Mutex
	// exportErr fails the export, after its first bytes when exportWrites.
	exportErr    error
	exportWrites bool
	exportedAt   time.Time
	more         []ExportTable
}

func newMemAccounts() *memAccounts { return &memAccounts{} }

const exportBody = `{"format":"runsten-account","version":1,"schema":"0013_api_key_last4",` +
	`"exported_at":"2026-09-28T05:00:00Z","tables":[{"name":"accounts","rows":[{"id":"acc"}]},` +
	`{"name":"snapshots","rows":[{"payload":{"data":{}},"fetched_at":"2026-09-28T05:00:00+00:00"}]}]}`

func (m *memAccounts) ExportAccount(_ context.Context, accountID string, at time.Time, more []ExportTable, w io.Writer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exportedAt, m.more = at, more
	if m.exportErr != nil {
		if m.exportWrites {
			_, _ = io.WriteString(w, exportBody[:20])
		}
		return m.exportErr
	}
	_, err := io.WriteString(w, strings.Replace(exportBody, `"acc"`, `"`+accountID+`"`, 1))
	return err
}

func accountEnv(t *testing.T) (*env, *memAccounts) {
	t.Helper()
	e := newEnv(t, 10)
	m := newMemAccounts()
	e.server.Accounts = m
	return e, m
}

func TestExportAccount(t *testing.T) {
	e, m := accountEnv(t)
	c := e.session(t, "admin")
	u := e.api.URL + "/api/v1/account/export"

	resp, body := get(t, noFollow(), u, c)
	if resp.status != http.StatusOK || resp.header.Get("Content-Type") != "application/json" ||
		resp.header.Get("Content-Disposition") != `attachment; filename="runsten-account-2026-09-28.json"` ||
		resp.header.Get("Cache-Control") != "no-store" || !json.Valid([]byte(body)) || !strings.Contains(body, `"id":"acc"`) {
		t.Errorf("export: %d %v %s", resp.status, resp.header, body)
	}
	if !m.exportedAt.Equal(e.clk.Now()) || m.more != nil {
		t.Errorf("exported as of %v, with %v", m.exportedAt, m.more)
	}
	e.server.ExportTables = []ExportTable{{Name: "ext.notes", OrderBy: "id"}}
	if get(t, noFollow(), u, c); len(m.more) != 1 || m.more[0].Name != "ext.notes" {
		t.Errorf("an extension's tables: %v", m.more)
	}
	if _, body := get(t, noFollow(), u, e.session(t, "other")); !strings.Contains(body, `"id":"other"`) {
		t.Errorf("another account's export: %s", body)
	}

	m.exportErr = errors.New("boom")
	resp, body = get(t, noFollow(), u, c)
	if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") || resp.header.Get("Content-Disposition") != "" {
		t.Errorf("failure before any byte: %d %v %s", resp.status, resp.header, body)
	}
	wantError(t, body, codeInternal)

	if resp, _ := get(t, noFollow(), u); resp.status != http.StatusUnauthorized {
		t.Errorf("without a session: %d", resp.status)
	}
}

// TestExportCutShort: a failure after the first bytes cannot change the status: the
// document is cut short. The contract's check is left out: the body is no JSON.
func TestExportCutShort(t *testing.T) {
	e, m := accountEnv(t)
	m.exportErr, m.exportWrites = errors.New("boom"), true
	srv := e.server
	tok, _, _ := e.sessions.Login(t.Context(), "admin", password, "test")
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/account/export", http.NoBody)
	req.AddCookie(reqCookie("runsten_session", tok))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != exportBody[:20] {
		t.Errorf("cut short: %d %q", rec.Code, rec.Body.String())
	}
}
