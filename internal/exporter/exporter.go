// Package exporter builds an account's whole history as a ZIP archive, out of the
// request: its worker claims the export the API created (POST /account/export-job),
// writes the account's export (the document of GET /account/export) into the archive,
// part by part, each part renewing its claim, and ends the job done, the archive held
// a few days. It knows neither HTTP nor the database: it declares its Store, and is
// given the function that writes the account's document, which the binary wires.
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

// Store keeps the exports and their archives, in parts.
type Store interface {
	// ClaimExport claims the export that may run and whose previous claim has gone
	// lease without a sign of life; false when there is none.
	ClaimExport(ctx context.Context, at time.Time, lease time.Duration) (Job, bool, error)
	// ResetExport throws away what an interrupted build wrote, and renews the claim;
	// ErrLeaseLost when it is over.
	ResetExport(ctx context.Context, job Job, at time.Time) error
	// WriteExportPart adds the archive's part that starts at start, and renews the
	// claim; ErrLeaseLost when it is over.
	WriteExportPart(ctx context.Context, job Job, start int64, data []byte, at time.Time) error
	// SaveExport ends the export, done, with the archive's size, held until expiresAt;
	// ErrLeaseLost when the claim is over.
	SaveExport(ctx context.Context, job Job, at time.Time, bytes int64, expiresAt time.Time) error
	// RetryExport throws away what the build wrote and releases the export until
	// notBefore, with its failures in a row.
	RetryExport(ctx context.Context, job Job, notBefore time.Time, failures int) error
	// FailExport throws away what the build wrote and ends the export, failed.
	FailExport(ctx context.Context, job Job, at time.Time) error
	// ExpireExports throws away the archives held past at, across the accounts: their
	// number.
	ExpireExports(ctx context.Context, at time.Time) (int, error)
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
	// PartSize is the size of the archive's parts: a part is one write of the store.
	PartSize int
	// Keep is how long a built archive is held; Expire how often the ones past their
	// time are thrown away.
	Keep   time.Duration
	Expire time.Duration
}

// DefaultParams are the worker's defaults: every part (1 MiB) renews the claim, so a
// lease of 5 minutes covers the reading of a table before its first rows; an archive
// is held a week, the time to download it.
func DefaultParams() Params {
	return Params{
		Lease: 5 * time.Minute, Idle: 5 * time.Second, Backoff: time.Minute, MaxFailures: 3,
		PartSize: 1 << 20, Keep: 7 * 24 * time.Hour, Expire: time.Minute,
	}
}

// Worker builds the exports, one at a time.
type Worker struct {
	store  Store
	export ExportFunc
	clk    clock.Clock
	params Params
	log    *slog.Logger
	// expired is when the archives past their time were last thrown away.
	expired time.Time
}

// New creates the worker.
func New(store Store, export ExportFunc, clk clock.Clock, params Params, log *slog.Logger) *Worker {
	return &Worker{store: store, export: export, clk: clk, params: params, log: log}
}

// Once throws away the archives past their time, at most every Expire, then builds one
// export, if one may run, until it ends or must wait: false when there was none. What
// the export meets is written to it, and is no error of Once.
func (w *Worker) Once(ctx context.Context) (bool, error) {
	if err := w.expire(ctx); err != nil {
		return false, err
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

func (w *Worker) expire(ctx context.Context) error {
	now := w.now()
	if !w.expired.IsZero() && now.Sub(w.expired) < w.params.Expire {
		return nil
	}
	n, err := w.store.ExpireExports(ctx, now)
	if err != nil {
		return fmt.Errorf("expiring the exports: %w", err)
	}
	w.expired = now
	if n > 0 {
		w.log.InfoContext(ctx, "account exports expired", "exports", n)
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

// build writes the account's export into the archive, part by part, then ends the job
// done with its size. Every failure leaves the archive half written: the next claim
// throws it away and builds it again.
func (w *Worker) build(ctx context.Context, job Job) error {
	if err := w.store.ResetExport(ctx, job, w.now()); err != nil {
		return fmt.Errorf("opening the archive: %w", err)
	}
	at := w.now()
	parts := &partWriter{ctx: ctx, w: w, job: job, buf: make([]byte, 0, w.params.PartSize)}
	zipFile := zip.NewWriter(parts)
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
		err = parts.flush()
	}
	if err != nil {
		return fmt.Errorf("building the archive: %w", err)
	}
	if err := w.store.SaveExport(ctx, job, w.now(), parts.start, w.now().Add(w.params.Keep)); err != nil {
		return fmt.Errorf("saving the export: %w", err)
	}
	return nil
}

// partWriter cuts what is written into parts of the store, PartSize each but the last:
// each write of a part renews the job's claim.
type partWriter struct {
	ctx   context.Context
	w     *Worker
	job   Job
	buf   []byte
	start int64 // the bytes of the parts written: where the next one starts
}

func (p *partWriter) Write(b []byte) (int, error) {
	n := 0
	for len(b) > 0 {
		k := min(len(b), cap(p.buf)-len(p.buf))
		p.buf, b, n = append(p.buf, b[:k]...), b[k:], n+k
		if len(p.buf) == cap(p.buf) {
			if err := p.flush(); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// flush writes the part held, if any.
func (p *partWriter) flush() error {
	if len(p.buf) == 0 {
		return nil
	}
	if err := p.w.store.WriteExportPart(p.ctx, p.job, p.start, p.buf, p.w.now()); err != nil {
		return fmt.Errorf("writing a part of the archive: %w", err)
	}
	p.start += int64(len(p.buf))
	p.buf = p.buf[:0]
	return nil
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
