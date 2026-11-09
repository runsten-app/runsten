package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"runsten/internal/api"
	"runsten/internal/platform/clock"
)

var t0 = time.Date(2026, 10, 11, 8, 30, 0, 0, time.UTC)

// memStore keeps one job in memory, as the store does, writing the archive to a
// buffer.
type memStore struct {
	mu     sync.Mutex
	pended bool // a job may run
	// errs fail the calls; lease makes SaveExport lose its claim.
	err   error // ExportFile's
	lease bool
	// What the calls did, as the store does.
	called   []string
	status   string // the job's status as the calls left it
	failures int
	notUntil time.Time
	saved    int64
	archive  bytes.Buffer // what the worker wrote
	throwOld bool         // the archive of an interrupted build was thrown away
}

func (m *memStore) ClaimExport(_ context.Context, at time.Time, _ time.Duration) (Job, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.pended || !m.notUntil.IsZero() && m.notUntil.After(at) {
		return Job{}, false, nil
	}
	m.pended = false
	m.called = append(m.called, "claim")
	return Job{ID: "job", AccountID: "acc", Failures: m.failures, ClaimedAt: at}, true, nil
}

func (m *memStore) ExportFile(_ context.Context, _ Job) (io.WriteCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	m.throwOld = true
	return nopCloser{&m.archive}, nil
}

func (m *memStore) SaveExport(_ context.Context, _ Job, _ time.Time, bytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lease {
		m.called = append(m.called, "lease")
		return ErrLeaseLost
	}
	m.saved, m.status = bytes, "done"
	return nil
}

func (m *memStore) RetryExport(_ context.Context, _ Job, notBefore time.Time, failures int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pended, m.notUntil, m.failures = true, notBefore, failures
	return nil
}

func (m *memStore) FailExport(_ context.Context, _ Job, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = "failed"
	return nil
}

type nopCloser struct{ w io.Writer }

func (c nopCloser) Write(p []byte) (int, error) { return c.w.Write(p) }
func (nopCloser) Close() error                  { return nil }

// memAccounts writes the account's document, as the store does.
type memAccounts struct {
	mu     sync.Mutex
	acc    string
	at     time.Time
	more   []api.ExportTable
	err    error
	writes int
}

func (m *memAccounts) ExportAccount(ctx context.Context, accountID string, at time.Time, more []api.ExportTable, w io.Writer) error {
	m.mu.Lock()
	m.acc, m.at, m.more, m.writes = accountID, at, more, m.writes+1
	m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.err != nil {
		return m.err
	}
	_, err := io.WriteString(w, `{"format":"runsten-account"}`)
	return err //nolint:wrapcheck // a test's
}

type env struct {
	store    *memStore
	accounts *memAccounts
	clk      *clock.Manual
	worker   *Worker
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{store: &memStore{}, accounts: &memAccounts{}, clk: clock.NewManual(t0)}
	e.worker = New(e.store, e.accounts, func() []api.ExportTable {
		return []api.ExportTable{{Name: "ext.notes", OrderBy: "id"}}
	}, e.clk, DefaultParams(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return e
}

func (e *env) once(t *testing.T) bool {
	t.Helper()
	worked, err := e.worker.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return worked
}

func TestExport(t *testing.T) {
	e := newEnv(t)
	e.store.pended = true
	if !e.once(t) {
		t.Fatal("no job run")
	}
	s := e.store
	if s.status != "done" {
		t.Fatalf("status %s", s.status)
	}
	if e.once(t) {
		t.Error("a job done ran again")
	}
	// The archive is the account's document, zipped and named after its day.
	if s.archive.Len() == 0 || s.saved != int64(s.archive.Len()) {
		t.Errorf("archive %d bytes, saved %d", s.archive.Len(), s.saved)
	}
	r, err := zip.NewReader(bytes.NewReader(s.archive.Bytes()), int64(s.archive.Len()))
	if err != nil {
		t.Fatalf("not an archive: %v", err)
	}
	if len(r.File) != 1 || r.File[0].Name != "runsten-account-2026-10-11.json" {
		t.Fatalf("entries %+v", r.File)
	}
	f, err := r.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if string(body) != `{"format":"runsten-account"}` {
		t.Errorf("entry %q", body)
	}
	// The export is the API's document: the account, at the build's time, with the
	// tables an extension adds.
	if e.accounts.acc != "acc" || !e.accounts.at.Equal(e.clk.Now()) ||
		len(e.accounts.more) != 1 || e.accounts.more[0].Name != "ext.notes" {
		t.Errorf("account %s at %s of %v", e.accounts.acc, e.accounts.at, e.accounts.more)
	}
	// A build of everything falls short of the claim's lease.
	if params := DefaultParams(); params.Lease < 10*time.Minute {
		t.Errorf("lease %s, want at least 10 minutes", params.Lease)
	}
	// An export asked for again builds another archive.
	e.store.pended = true
	if !e.once(t) {
		t.Fatal("no job run")
	}
	if e.store.status != "done" || !e.store.throwOld {
		t.Error("the interrupted build's archive is kept")
	}
}

// Failures are retried after Backoff, MaxFailures times in a row: the build starts
// again from its own start.
func TestExportFailures(t *testing.T) {
	e := newEnv(t)
	e.store.pended = true
	e.store.err = errors.New("database down")
	for i := 1; i < DefaultParams().MaxFailures; i++ {
		e.once(t)
		s := e.store
		if s.status != "" || s.failures != i || !s.notUntil.Equal(e.clk.Now().Add(time.Minute)) {
			t.Fatalf("failure %d: status %q, failures %d, not before %s", i, s.status, s.failures, s.notUntil)
		}
		if e.once(t) {
			t.Fatalf("ran before its backoff")
		}
		e.clk.Advance(time.Minute)
	}
	e.once(t)
	if e.store.status != "failed" {
		t.Errorf("status %s", e.store.status)
	}
	e.store.err = nil
	e.clk.Advance(time.Minute)
}

// A claim another worker took over is left alone: the build is thrown away, no retry,
// no failure.
func TestExportLeaseLost(t *testing.T) {
	e := newEnv(t)
	e.store.pended = true
	e.store.lease = true
	e.once(t)
	if s := e.store; s.status != "" || s.saved != 0 || e.once(t) {
		t.Errorf("taken over: status %q, saved %d", s.status, s.saved)
	}
}

// Stopped, the build is left where it is: built again once its claim is over.
func TestExportStopped(t *testing.T) {
	e := newEnv(t)
	e.store.pended = true
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if worked, err := e.worker.Once(ctx); err != nil || !worked {
		t.Fatalf("stopped: worked %t, err %v", worked, err)
	}
	if e.store.status != "" || e.once(t) {
		t.Errorf("stopped: status %q", e.store.status)
	}
}

// Run stops with its context.
func TestRunStops(t *testing.T) {
	e := newEnv(t)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.worker.Run(ctx)
		close(done)
	}()
	stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the worker still runs")
	}
}
