// Package exporter builds an account's whole history as a ZIP archive, out of the
// request: its worker claims the export the API created (POST /account/export-job),
// writes the account's export (the document of GET /account/export) into an archive of
// its Archives, out of the database, renewing its claim as it goes, and ends the job
// done, the archive held a few days. It throws away the archives no export holds
// anymore. It knows neither HTTP, the database nor where archives are kept: it declares
// its Store and its Archives, and is given the function that writes the account's
// document, which the binary wires.
package exporter

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"runsten/internal/platform/clock"
)

// ErrLeaseLost is returned for a write of a job whose claim is over: another worker
// claimed it since.
var ErrLeaseLost = errors.New("the job's claim is over")

// Job is a claimed export.
type Job struct {
	ID, AccountID string
	Failures      int // in a row, before this claim
	// ClaimedAt is the claim: a write of a claim another worker has taken over since
	// is refused (ErrLeaseLost).
	ClaimedAt time.Time
}

// Store keeps the exports: their status, their claim and their expiry.
type Store interface {
	// ClaimExport claims the export that may run and whose previous claim has gone
	// lease without a sign of life; false when there is none.
	ClaimExport(ctx context.Context, at time.Time, lease time.Duration) (Job, bool, error)
	// StartExport starts the build, nothing written yet, and RenewExport renews its
	// claim with the bytes written so far; ErrLeaseLost when the claim is over.
	StartExport(ctx context.Context, job Job, at time.Time) error
	RenewExport(ctx context.Context, job Job, at time.Time, bytes int64) error
	// SaveExport ends the export, done, with the archive's size, held until expiresAt;
	// ErrLeaseLost when the claim is over.
	SaveExport(ctx context.Context, job Job, at time.Time, bytes int64, expiresAt time.Time) error
	// RetryExport releases the export until notBefore, with its failures in a row.
	RetryExport(ctx context.Context, job Job, notBefore time.Time, failures int) error
	// FailExport ends the export, failed: it holds no archive.
	FailExport(ctx context.Context, job Job, at time.Time) error
	// ExpireExports ends the exports held past at, across the accounts: their number.
	ExpireExports(ctx context.Context, at time.Time) (int, error)
	// HeldExports are the ids of the exports that may hold an archive, across the
	// accounts: being built, or done.
	HeldExports(ctx context.Context) (map[string]bool, error)
}

// Archives keeps the archives, out of the database, named by their account and export.
type Archives interface {
	// Create starts the export's archive: Close makes it the export's, unless ctx is
	// over, which gives it up.
	Create(ctx context.Context, accountID, exportID string) (io.WriteCloser, error)
	// List lists the exports that have an archive, or what a build of it left: their
	// account by their id.
	List(ctx context.Context) (map[string]string, error)
	// Remove removes the export's archive, and what a build of it left.
	Remove(ctx context.Context, accountID, exportID string) error
}

// ExportFunc writes the account's export document at the time at to w: the one GET
// /account/export streams, with the tables an extension adds.
type ExportFunc func(ctx context.Context, accountID string, at time.Time, w io.Writer) error

// Params are the worker's pace, retries and how long an archive is held.
type Params struct {
	// Lease is how long a claim holds without a part written: an export is built again
	// from its start when its worker stopped.
	Lease time.Duration
	// Idle is the wait when no export is to run.
	Idle time.Duration
	// Backoff is the wait after a failure, MaxFailures the failures in a row before an
	// export fails.
	Backoff     time.Duration
	MaxFailures int
	// RenewEvery is how many bytes of the archive are written between two renewals of
	// the claim.
	RenewEvery int64
	// Keep is how long a built archive is held; Sweep how often the archives past their
	// time, or no export's, are thrown away.
	Keep  time.Duration
	Sweep time.Duration
}

// DefaultParams are the worker's defaults: every 4 MiB written renews the claim, so a
// lease of 5 minutes covers the reading of a table before its first rows; an archive
// is held a week, the time to download it.
func DefaultParams() Params {
	return Params{
		Lease: 5 * time.Minute, Idle: 5 * time.Second, Backoff: time.Minute, MaxFailures: 3,
		RenewEvery: 4 << 20, Keep: 7 * 24 * time.Hour, Sweep: time.Minute,
	}
}

// Worker builds the exports, one at a time.
type Worker struct {
	store    Store
	archives Archives
	export   ExportFunc
	clk      clock.Clock
	params   Params
	log      *slog.Logger
	// swept is when the archives were last swept.
	swept time.Time
}

// New creates the worker.
func New(store Store, archives Archives, export ExportFunc, clk clock.Clock, params Params, log *slog.Logger) *Worker {
	return &Worker{store: store, archives: archives, export: export, clk: clk, params: params, log: log}
}

// Once sweeps the archives, at most every Sweep, then builds one export, if one may
// run, until it ends or must wait: false when there was none. What the export meets is
// written to it, and is no error of Once.
func (w *Worker) Once(ctx context.Context) (bool, error) {
	// The sweep is best effort: a failure of it holds no export back.
	if err := w.sweep(ctx); err != nil && ctx.Err() == nil {
		w.log.ErrorContext(ctx, "account exports not swept", "error", err)
	}
	job, ok, err := w.store.ClaimExport(ctx, w.now(), w.params.Lease)
	if err != nil || !ok {
		return false, err //nolint:wrapcheck // Store error, already has context
	}
	log := w.log.With("account", job.AccountID, "job", job.ID)
	err = w.build(ctx, job)
	switch {
	case err == nil:
		log.InfoContext(ctx, "account export built")
		return true, nil
	case ctx.Err() != nil:
		// Stopped: the export is built again once its claim is over.
		return true, nil //nolint:nilerr // the job's, not Once's
	case errors.Is(err, ErrLeaseLost):
		log.WarnContext(ctx, "account export taken over by another worker")
		return true, nil
	}
	failures := job.Failures + 1
	if failures < w.params.MaxFailures {
		log.WarnContext(ctx, "account export failed, retried later", "error", err, "failures", failures)
		return true, w.retry(ctx, job, w.now().Add(w.params.Backoff), failures)
	}
	log.ErrorContext(ctx, "account export failed", "error", err)
	return true, w.fail(ctx, job)
}

// sweep ends the exports past their time, then throws away the archives no export
// holds: replaced, expired, failed, or of an account deleted. The archives are listed
// before the exports are read: one built meanwhile has its export already.
func (w *Worker) sweep(ctx context.Context) error {
	now := w.now()
	if !w.swept.IsZero() && now.Sub(w.swept) < w.params.Sweep {
		return nil
	}
	// Tried again a Sweep later, whether this one fails or not.
	w.swept = now
	n, err := w.store.ExpireExports(ctx, now)
	if err != nil {
		return fmt.Errorf("expiring the exports: %w", err)
	}
	if n > 0 {
		w.log.InfoContext(ctx, "account exports expired", "exports", n)
	}
	archives, err := w.archives.List(ctx)
	if err != nil {
		return fmt.Errorf("listing the archives: %w", err)
	}
	held, err := w.store.HeldExports(ctx)
	if err != nil {
		return fmt.Errorf("reading the exports held: %w", err)
	}
	for exportID, accountID := range archives {
		if held[exportID] {
			continue
		}
		if err := w.archives.Remove(ctx, accountID, exportID); err != nil {
			// One archive that stays leaves the others to go.
			w.log.ErrorContext(ctx, "account export archive not removed", "account", accountID, "job", exportID, "error", err)
			continue
		}
		w.log.InfoContext(ctx, "account export archive removed", "account", accountID, "job", exportID)
	}
	return nil
}

func (w *Worker) retry(ctx context.Context, job Job, notBefore time.Time, failures int) error {
	if err := w.store.RetryExport(ctx, job, notBefore, failures); err != nil {
		return fmt.Errorf("releasing an export: %w", err)
	}
	return nil
}

func (w *Worker) fail(ctx context.Context, job Job) error {
	if err := w.store.FailExport(ctx, job, w.now()); err != nil {
		return fmt.Errorf("failing an export: %w", err)
	}
	return nil
}

// now is the worker's time, to the microsecond, as the database keeps it: a claim is
// compared to the one stored.
func (w *Worker) now() time.Time { return w.clk.Now().Truncate(time.Microsecond) }

// build writes the account's export into a new archive, renewing the claim as it goes,
// then ends the job done with its size. A failure gives the archive up.
func (w *Worker) build(ctx context.Context, job Job) error {
	if err := w.store.StartExport(ctx, job, w.now()); err != nil {
		return fmt.Errorf("starting the export: %w", err)
	}
	// Canceled, the archive is given up: only a complete one is the export's.
	buildCtx, giveUp := context.WithCancel(ctx)
	defer giveUp()
	file, err := w.archives.Create(buildCtx, job.AccountID, job.ID)
	if err != nil {
		return fmt.Errorf("opening the archive: %w", err)
	}
	at := w.now()
	counted := &renewWriter{ctx: ctx, w: w, job: job, file: file}
	zipFile := zip.NewWriter(counted)
	entry, err := zipFile.CreateHeader(&zip.FileHeader{
		Name: "runsten-account-" + at.Format(time.DateOnly) + ".json", Method: zip.Deflate, Modified: at,
	})
	if err == nil {
		// The document is the one GET /account/export streams, at the time of the
		// build, of the same database.
		if err = w.export(ctx, job.AccountID, at, entry); err != nil {
			err = fmt.Errorf("writing the export: %w", err)
		}
	}
	if closeErr := zipFile.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		// The claim is still this worker's as the archive becomes the export's.
		err = w.store.RenewExport(ctx, job, w.now(), counted.n)
	}
	if err != nil {
		giveUp()
		_ = file.Close() // given up: what was written goes
		return fmt.Errorf("building the archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing the archive: %w", err)
	}
	if err := w.store.SaveExport(ctx, job, w.now(), counted.n, w.now().Add(w.params.Keep)); err != nil {
		return fmt.Errorf("saving the export: %w", err)
	}
	return nil
}

// renewWriter writes to the archive, counting its bytes, and renews the job's claim
// every RenewEvery of them.
type renewWriter struct {
	ctx     context.Context
	w       *Worker
	job     Job
	file    io.Writer
	n, last int64 // the bytes written, and at the last renewal
}

func (r *renewWriter) Write(p []byte) (int, error) {
	n, err := r.file.Write(p)
	r.n += int64(n)
	if err != nil {
		return n, err //nolint:wrapcheck // the archive's writer
	}
	if r.n-r.last >= r.w.params.RenewEvery {
		if err := r.w.store.RenewExport(r.ctx, r.job, r.w.now(), r.n); err != nil {
			return n, fmt.Errorf("renewing the claim: %w", err)
		}
		r.last = r.n
	}
	return n, nil
}

// Run builds the exports until ctx is canceled, then waits Idle while none is to run.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		next := time.Duration(0)
		worked, err := w.Once(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			w.log.ErrorContext(ctx, "export", "error", err)
			next = w.params.Idle
		case !worked:
			next = w.params.Idle
		}
		t.Reset(next)
	}
}
