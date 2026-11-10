package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

var t0 = time.Date(2026, 10, 11, 8, 30, 0, 0, time.UTC)

// memStore keeps one job in memory, as the store does.
type memStore struct {
	mu     sync.Mutex
	pended bool // a job may run
	// err fails StartExport; lease makes a renewal, or SaveExport, lose its claim.
	err   error
	lease bool
	// What the calls did, as the store does.
	status    string // the job's status as the calls left it
	failures  int
	notUntil  time.Time
	saved     int64
	expiresAt time.Time
	renewed   []int64     // the bytes of each renewal of the claim
	expired   []time.Time // each call of ExpireExports
	held      map[string]bool
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

func (m *memStore) StartExport(_ context.Context, _ Job, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.renewed = append(m.renewed, 0)
	return nil
}

func (m *memStore) RenewExport(_ context.Context, _ Job, _ time.Time, bytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lease {
		return ErrLeaseLost
	}
	m.renewed = append(m.renewed, bytes)
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
	m.pended, m.notUntil, m.failures = true, notBefore, failures
	return nil
}

func (m *memStore) FailExport(_ context.Context, _ Job, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = "failed"
	return nil
}

func (m *memStore) ExpireExports(_ context.Context, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expired = append(m.expired, at)
	return 0, nil
}

func (m *memStore) HeldExports(context.Context) (map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.held, nil
}

// memArchives keeps the archives in memory, as a directory does: complete once closed,
// given up when the build's context is over.
type memArchives struct {
	mu       sync.Mutex
	archives map[string][]byte // complete, by export
	accounts map[string]string // every archive's account, a build's under way too
	givenUp  int
	removed  []string
	// listErr fails List, stuck the archives Remove cannot remove.
	listErr error
	stuck   map[string]bool
}

func newMemArchives() *memArchives {
	return &memArchives{archives: map[string][]byte{}, accounts: map[string]string{}}
}

type memFile struct {
	ctx context.Context
	m   *memArchives
	id  string
	buf bytes.Buffer
}

func (f *memFile) Write(p []byte) (int, error) { return f.buf.Write(p) }

func (f *memFile) Close() error {
	f.m.mu.Lock()
	defer f.m.mu.Unlock()
	if err := f.ctx.Err(); err != nil {
		f.m.givenUp++
		return err
	}
	f.m.archives[f.id] = f.buf.Bytes()
	return nil
}

func (m *memArchives) Create(ctx context.Context, accountID, exportID string) (io.WriteCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accounts[exportID] = accountID
	return &memFile{ctx: ctx, m: m, id: exportID}, nil
}

func (m *memArchives) List(context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	return maps.Clone(m.accounts), nil
}

func (m *memArchives) Remove(_ context.Context, _, exportID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stuck[exportID] {
		return errors.New("permission denied")
	}
	delete(m.archives, exportID)
	delete(m.accounts, exportID)
	m.removed = append(m.removed, exportID)
	return nil
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
	store    *memStore
	archives *memArchives
	export   *memExport
	clk      *clock.Manual
	worker   *Worker
}

func newEnv(t *testing.T, params Params) *env {
	t.Helper()
	e := &env{
		store: &memStore{held: map[string]bool{"job": true}}, archives: newMemArchives(),
		export: &memExport{doc: `{"format":"runsten-account"}`}, clk: clock.NewManual(t0),
	}
	e.worker = New(e.store, e.archives, e.export.export, e.clk, params, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	if s.status != "done" {
		t.Fatalf("status %s", s.status)
	}
	if e.once(t) {
		t.Error("a job done ran again")
	}
	// The archive is the account's document, zipped and named after its day.
	archive := e.archives.archives["job"]
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

// The claim is renewed every RenewEvery bytes written, and once complete: a build
// longer than the lease keeps it.
func TestExportRenews(t *testing.T) {
	params := DefaultParams()
	params.RenewEvery = 100
	e := newEnv(t, params)
	r := &renewWriter{ctx: context.Background(), w: e.worker, job: Job{ID: "job"}, file: io.Discard}
	for _, n := range []int{60, 60, 30, 150, 10} {
		if _, err := r.Write(make([]byte, n)); err != nil {
			t.Fatal(err)
		}
	}
	if want := []int64{120, 300}; !slices.Equal(e.store.renewed, want) || r.n != 310 {
		t.Errorf("renewed %v after %d bytes, want %v", e.store.renewed, r.n, want)
	}
	// A claim lost stops the build.
	e.store.lease = true
	if _, err := r.Write(make([]byte, 100)); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("a claim lost: %v", err)
	}

	// A build renews it as it starts and as it completes, with the archive's size.
	e = newEnv(t, DefaultParams())
	e.store.pended = true
	e.once(t)
	size := int64(len(e.archives.archives["job"]))
	if s := e.store; s.status != "done" || s.saved != size || !slices.Equal(s.renewed, []int64{0, size}) {
		t.Errorf("status %s, saved %d of %d, renewed %v", s.status, s.saved, size, s.renewed)
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
	if len(e.archives.archives) != 0 || e.archives.givenUp != 1 {
		t.Errorf("the archive is kept: %d given up", e.archives.givenUp)
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
	if len(e.archives.archives) != 0 {
		t.Error("the archive of a claim taken over is kept")
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

// The sweep, at most every Sweep, ends the exports past their time and throws away the
// archives no export holds.
func TestExportSweeps(t *testing.T) {
	e := newEnv(t, DefaultParams())
	e.archives.archives["job"], e.archives.accounts["job"] = []byte("PK held"), "acc"
	e.archives.archives["old"], e.archives.accounts["old"] = []byte("PK replaced"), "acc"
	e.once(t)
	if len(e.store.expired) != 1 || !e.store.expired[0].Equal(t0) {
		t.Fatalf("expired %v", e.store.expired)
	}
	if _, ok := e.archives.archives["job"]; !ok || len(e.archives.removed) != 1 || e.archives.removed[0] != "old" {
		t.Errorf("removed %v", e.archives.removed)
	}
	e.clk.Advance(30 * time.Second)
	e.once(t)
	if len(e.store.expired) != 1 {
		t.Errorf("swept again within a minute: %v", e.store.expired)
	}
	e.clk.Advance(30 * time.Second)
	e.once(t)
	if len(e.store.expired) != 2 {
		t.Errorf("expired %v", e.store.expired)
	}
}

// The sweep is best effort: an archive that cannot be removed leaves the others to go,
// and a sweep that fails holds no export back.
func TestExportSweepFails(t *testing.T) {
	e := newEnv(t, DefaultParams())
	e.archives.stuck = map[string]bool{"stuck": true}
	e.archives.accounts["stuck"], e.archives.accounts["old"] = "acc", "acc"
	e.store.pended = true
	if !e.once(t) || e.store.status != "done" {
		t.Fatalf("not built past an archive stuck: %s", e.store.status)
	}
	if !slices.Equal(e.archives.removed, []string{"old"}) {
		t.Errorf("removed %v", e.archives.removed)
	}

	e = newEnv(t, DefaultParams())
	e.archives.listErr = errors.New("bucket unreachable")
	e.store.pended = true
	if !e.once(t) || e.store.status != "done" {
		t.Errorf("not built past a sweep failed: %s", e.store.status)
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
