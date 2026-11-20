package main

import (
	"context"
	"log/slog"
	"time"

	"runsten/internal/exporter"
	"runsten/internal/importer"
	"runsten/internal/notify"
)

// The workers of runsten-api tell the account's devices of a job that ended, done or
// failed: an import of a vehicle's history, which takes minutes, or the account's
// export. Their stores are wrapped, the workers know nothing of it.

// importsTold is the import worker's store, which tells of each import that ends.
type importsTold struct {
	importer.Store
	notifier *notify.Notifier
	log      *slog.Logger
}

// SaveImportPage implements importer.Store: the job's last page ends it, done.
func (s importsTold) SaveImportPage(ctx context.Context, job importer.Job, page importer.Page) (importer.Result, error) {
	res, err := s.Store.SaveImportPage(ctx, job, page)
	if err == nil && page.Done {
		tell(ctx, s.notifier, s.log, job.AccountID, "import:"+job.ID, page.At, map[string]string{
			"job": "import", "outcome": "done", "vehicle": job.VehicleID, "source": job.Source,
		})
	}
	return res, err //nolint:wrapcheck // the store's, with its context
}

// FailImport implements importer.Store.
func (s importsTold) FailImport(ctx context.Context, job importer.Job, reason importer.Failure, at time.Time) error {
	err := s.Store.FailImport(ctx, job, reason, at)
	if err == nil {
		tell(ctx, s.notifier, s.log, job.AccountID, "import:"+job.ID, at, map[string]string{
			"job": "import", "outcome": "failed", "failure": string(reason), "vehicle": job.VehicleID, "source": job.Source,
		})
	}
	return err //nolint:wrapcheck // the store's, with its context
}

// exportsTold is the export worker's store, which tells of each export that ends.
type exportsTold struct {
	exporter.Store
	notifier *notify.Notifier
	log      *slog.Logger
}

// SaveExport implements exporter.Store.
func (s exportsTold) SaveExport(ctx context.Context, job exporter.Job, at time.Time, bytes int64, expiresAt time.Time) error {
	err := s.Store.SaveExport(ctx, job, at, bytes, expiresAt)
	if err == nil {
		tell(ctx, s.notifier, s.log, job.AccountID, "export:"+job.ID, at, map[string]string{
			"job": "export", "outcome": "done", "expires_at": expiresAt.UTC().Format(time.RFC3339),
		})
	}
	return err //nolint:wrapcheck // the store's, with its context
}

// FailExport implements exporter.Store.
func (s exportsTold) FailExport(ctx context.Context, job exporter.Job, at time.Time) error {
	err := s.Store.FailExport(ctx, job, at)
	if err == nil {
		tell(ctx, s.notifier, s.log, job.AccountID, "export:"+job.ID, at, map[string]string{"job": "export", "outcome": "failed"})
	}
	return err //nolint:wrapcheck // the store's, with its context
}

// tell tells the account of a job that ended; a failure is logged, never the job's.
func tell(ctx context.Context, n *notify.Notifier, log *slog.Logger, accountID, key string, at time.Time, args map[string]string) {
	if !n.Sends() {
		return
	}
	e := notify.Event{Kind: notify.ImportFinished, AccountID: accountID, At: at, Key: key, Args: args}
	if err := n.Notify(context.WithoutCancel(ctx), e); err != nil {
		log.Warn("notification not recorded", "kind", string(e.Kind), "account", accountID, "err", err)
	}
}
