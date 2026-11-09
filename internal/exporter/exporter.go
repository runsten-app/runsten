// Package exporter builds an account's whole history as a ZIP archive, out of the
// request: its worker claims the export the API created (POST /account/export-job),
// writes the account's export (the document of GET /account/export) into the archive
// the store holds for the account, and ends the job done. It knows neither HTTP nor
// the database: it declares its Store, the file the archive is written to, which the
// binary wires.
package exporter

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"runsten/internal/api"
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

// Store keeps the exports and holds their archives.
type Store interface {
	// ClaimExport claims the export that may run and whose previous claim is older
	// than lease; false when there is none.
	ClaimExport(ctx context.Context, at time.Time, lease time.Duration) (Job, bool, error)
	// ExportFile opens the file the archive is written to, throwing away what an
	// interrupted build wrote; ErrLeaseLost when the claim is over. The file is closed
	// when the build ends.
	ExportFile(ctx context.Context, job Job) (io.WriteCloser, error)
	// SaveExport ends the export, done, with the archive's size; ErrLeaseLost when the
	// claim is over.
	SaveExport(ctx context.Context, job Job, at time.Time, bytes int64) error
	// RetryExport releases the export until notBefore, with its failures in a row.
	RetryExport(ctx context.Context, job Job, notBefore time.Time, failures int) error
	// FailExport ends the export, failed: whatever it wrote is thrown away with the
	// next one.
	FailExport(ctx context.Context, job Job, at time.Time) error
}

// Params are the worker's pace and retries.
type Params struct {
	// Lease is how long a claim holds without the archive saved: an export is built
	// again from its start when its worker stopped.
	Lease time.Duration
	// Idle is the wait when no export is to run.
	Idle time.Duration
	// Backoff is the wait after a failure, MaxFailures the failures in a row before an
	// export fails.
	Backoff     time.Duration
	MaxFailures int
}

// DefaultParams are the worker's defaults: an account that outgrew the request takes
// minutes to read, an interrupted build is thrown away and built again, so a claim is
// renewed less often than the import worker's.
func DefaultParams() Params {
	return Params{Lease: 10 * time.Minute, Idle: 5 * time.Second, Backoff: time.Minute, MaxFailures: 3}
}

// Worker builds the exports, one at a time.
type Worker struct {
	store Store
	// accounts writes the account's export; tables are the tables an extension adds,
	// read as they are asked (an extension may add them after the worker starts).
	accounts api.Accounts
	tables   func() []api.ExportTable
	clk      clock.Clock
	params   Params
	log      *slog.Logger
}

// New creates the worker.
func New(store Store, accounts api.Accounts, tables func() []api.ExportTable, clk clock.Clock, params Params, log *slog.Logger) *Worker {
	return &Worker{store: store, accounts: accounts, tables: tables, clk: clk, params: params, log: log}
}

// Once builds one export, if one may run, until it ends or must wait: false when there
// was none. What the export meets is written to it, and is no error of Once.
func (w *Worker) Once(ctx context.Context) (bool, error) {
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

// build writes the account's export into the archive the store holds for it, then ends
// the job done with its size. Every failure leaves the archive half written: the next
// claim throws it away and builds it again.
func (w *Worker) build(ctx context.Context, job Job) error {
	file, err := w.store.ExportFile(ctx, job)
	if err != nil {
		return fmt.Errorf("opening the archive: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			w.log.WarnContext(ctx, "archive not closed", "error", err)
		}
	}()
	at := w.now()
	counted := countWriter{w: file}
	zipFile := zip.NewWriter(&counted)
	entry, err := zipFile.CreateHeader(&zip.FileHeader{
		Name: "runsten-account-" + at.Format(time.DateOnly) + ".json", Method: zip.Deflate,
	})
	if err == nil {
		// The document is the one GET /account/export streams, at the time of the
		// build, of the same database.
		if err = w.accounts.ExportAccount(ctx, job.AccountID, at, w.tables(), entry); err != nil {
			err = fmt.Errorf("writing the export: %w", err)
		}
	}
	if closeErr := zipFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("building the archive: %w", err)
	}
	if err := w.store.SaveExport(ctx, job, at, counted.n); err != nil {
		return fmt.Errorf("saving the export: %w", err)
	}
	return nil
}

// countWriter counts what is written to w: the archive's size.
type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err //nolint:wrapcheck // the file's writer
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
