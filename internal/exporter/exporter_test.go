package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

var t0 = time.Date(2026, 10, 11, 8, 30, 0, 0, time.UTC)

// memStore keeps one job in memory, as the store does, the archive in parts.
type memStore struct {
	mu     sync.Mutex
	pended bool // a job may run
	// err fails ResetExport; lease makes the write of a part, or SaveExport, lose
	// its claim.
	err   error
	lease bool
	// What the calls did, as the store does.
	status    string // the job's status as the calls left it
	failures  int
	notUntil  time.Time
	saved     int64
	expiresAt time.Time
	parts     [][]byte    // the archive's parts as written
	renewed   []time.Time // each renewal of the claim
	reset     bool        // what an interrupted build wrote was thrown away
	expired   []time.Time // each call of ExpireExports
}

func (m *memStore) ClaimExport(_ context.Context, at time.Time, _ time.Duration) (Job, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.pended || !m.notUntil.IsZero() && m.notUntil.After(at) {
		return Job{}, false, nil
	}
	m.pended = false
	return Job{ID: "job", AccountID: "acc", Failures: m.failures, ClaimedAt: at}, true, nil
}

func (m *memStore) ResetExport(_ context.Context, _ Job, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.reset, m.parts, m.renewed = true, nil, append(m.renewed, at)
	return nil
}

func (m *memStore) WriteExportPart(_ context.Context, _ Job, start int64, data []byte, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lease {
		return ErrLeaseLost
	}
	var written int64
	for _, p := range m.parts {
		written += int64(len(p))
	}
	if start != written {
		return errors.New("a part out of order")
	}
	m.parts, m.renewed = append(m.parts, bytes.Clone(data)), append(m.renewed, at)
	return nil
}

func (m *memStore) SaveExport(_ context.Context, _ Job, _ time.Time, bytes int64, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lease {
		return ErrLeaseLost
	}
	m.saved, m.expiresAt, m.status = bytes, expiresAt, "done"
	return nil
}

func (m *memStore) RetryExport(_ context.Context, _ Job, notBefore time.Time, failures int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pended, m.notUntil, m.failures, m.parts = true, notBefore, failures, nil
	return nil
}

func (m *memStore) FailExport(_ context.Context, _ Job, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status, m.parts = "failed", nil
	return nil
}

func (m *memStore) ExpireExports(_ context.Context, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expired = append(m.expired, at)
	return 0, nil
}

func (m *memStore) archive() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return bytes.Join(m.parts, nil)
}

// memExport writes the account's document, as the store does.
type memExport struct {
	mu   sync.Mutex
	acc  string
	at   time.Time
	doc  string
	err  error
	runs int
}

func (m *memExport) export(ctx context.Context, accountID string, at time.Time, w io.Writer) error {
	m.mu.Lock()
	m.acc, m.at, m.runs = accountID, at, m.runs+1
	m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.err != nil {
		return m.err
	}
	_, err := io.WriteString(w, m.doc)
	return err //nolint:wrapcheck // a test's
}

type env struct {
	store  *memStore
	export *memExport
	clk    *clock.Manual
	worker *Worker
}

func newEnv(t *testing.T, params Params) *env {
	t.Helper()
	e := &env{store: &memStore{}, export: &memExport{doc: `{"format":"runsten-account"}`}, clk: clock.NewManual(t0)}
	e.worker = New(e.store, e.export.export, e.clk, params, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

// unzip reads the archive's only entry.
func unzip(t *testing.T, archive []byte) (string, string) {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("not an archive: %v", err)
	}
	if len(r.File) != 1 {
		t.Fatalf("entries %+v", r.File)
	}
	f, err := r.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return r.File[0].Name, string(body)
}

func TestExport(t *testing.T) {
	e := newEnv(t, DefaultParams())
	e.store.pended = true
	if !e.once(t) {
		t.Fatal("no job run")
	}
	s := e.store
	if s.status != "done" || !s.reset {
		t.Fatalf("status %s, reset %t", s.status, s.reset)
	}
	if e.once(t) {
		t.Error("a job done ran again")
	}
	// The archive is the account's document, zipped and named after its day.
	archive := s.archive()
	if len(archive) == 0 || s.saved != int64(len(archive)) {
		t.Errorf("archive %d bytes, saved %d", len(archive), s.saved)
	}
	if name, body := unzip(t, archive); name != "runsten-account-2026-10-11.json" || body != e.export.doc {
		t.Errorf("entry %s: %q", name, body)
	}
	// The export is the account's, at the build's time; the archive is held a week.
	if e.export.acc != "acc" || !e.export.at.Equal(e.clk.Now()) {
		t.Errorf("account %s at %s", e.export.acc, e.export.at)
	}
	if !s.expiresAt.Equal(e.clk.Now().Add(7 * 24 * time.Hour)) {
		t.Errorf("expires at %s", s.expiresAt)
	}
}

// The archive is written in parts of PartSize, each renewing the claim: a build longer
// than the lease keeps it.
func TestExportParts(t *testing.T) {
	params := DefaultParams()
	params.PartSize = 64
	e := newEnv(t, params)
	// Random bytes: the document does not compress below several parts.
	random := make([]byte, 1500)
	_, _ = rand.Read(random)
	e.export.doc = hex.EncodeToString(random)
	e.store.pended = true
	e.once(t)
	s := e.store
	if s.status != "done" || len(s.parts) < 3 {
		t.Fatalf("status %s, %d parts", s.status, len(s.parts))
	}
	for i, p := range s.parts[:len(s.parts)-1] {
		if len(p) != params.PartSize {
			t.Errorf("part %d: %d bytes", i, len(p))
		}
	}
	if len(s.renewed) != len(s.parts)+1 {
		t.Errorf("%d renewals for %d parts", len(s.renewed), len(s.parts))
	}
	if _, body := unzip(t, s.archive()); body != e.export.doc {
		t.Error("the parts are not the archive")
	}
}

// Failures are retried after Backoff, MaxFailures times in a row: the build starts
// again from its own start.
func TestExportFailures(t *testing.T) {
	e := newEnv(t, DefaultParams())
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
}

// A document that fails halfway fails the build: nothing is saved.
func TestExportDocumentFails(t *testing.T) {
	e := newEnv(t, DefaultParams())
	e.store.pended = true
	e.export.err = errors.New("query failed")
	e.once(t)
	if s := e.store; s.status != "" || s.saved != 0 || s.failures != 1 {
		t.Errorf("status %q, saved %d, failures %d", s.status, s.saved, s.failures)
	}
}

// A claim another worker took over is left alone: no retry, no failure.
func TestExportLeaseLost(t *testing.T) {
	e := newEnv(t, DefaultParams())
	e.store.pended = true
	e.store.lease = true
	e.once(t)
	if s := e.store; s.status != "" || s.saved != 0 || s.failures != 0 || e.once(t) {
		t.Errorf("taken over: status %q, saved %d, failures %d", s.status, s.saved, s.failures)
	}
}

// Stopped, the build is left where it is: built again once its claim is over.
func TestExportStopped(t *testing.T) {
	e := newEnv(t, DefaultParams())
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

// The archives past their time are thrown away at most every Expire.
func TestExportExpires(t *testing.T) {
	e := newEnv(t, DefaultParams())
	e.once(t)
	e.clk.Advance(30 * time.Second)
	e.once(t)
	if len(e.store.expired) != 1 || !e.store.expired[0].Equal(t0) {
		t.Fatalf("expired %v", e.store.expired)
	}
	e.clk.Advance(30 * time.Second)
	e.once(t)
	if len(e.store.expired) != 2 {
		t.Errorf("expired %v", e.store.expired)
	}
}

// Run stops with its context.
func TestRunStops(t *testing.T) {
	e := newEnv(t, DefaultParams())
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
