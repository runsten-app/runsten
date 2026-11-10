package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/exporter"
)

// The accounts' ZIP archives (account_exports): one export per account, its status,
// its claim and its expiry. The archive itself is kept out of the database, by the
// export worker's archive store.

const accountExportColumns = `id, status, coalesce(error, ''), bytes, created_at, not_before, ended_at, expires_at`

func scanAccountExport(row pgx.Row) (api.AccountExport, error) {
	var e api.AccountExport
	var endedAt, expiresAt *time.Time
	err := row.Scan(&e.ID, &e.Status, &e.Error, &e.Bytes, &e.CreatedAt, &e.NotBefore, &endedAt, &expiresAt)
	e.CreatedAt, e.NotBefore = e.CreatedAt.UTC(), e.NotBefore.UTC()
	e.EndedAt, e.ExpiresAt = utcOrZero(endedAt), utcOrZero(expiresAt)
	return e, err //nolint:wrapcheck // wrapped by the caller
}

// CreateAccountExport implements api.AccountExports: the account holds one export; the
// one it had goes (its archive is then no export's), and the new one has an id of its
// own.
func (s *Store) CreateAccountExport(ctx context.Context, accountID string, at time.Time) (api.AccountExport, error) {
	at = at.Truncate(time.Microsecond)
	var e api.AccountExport
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM account_exports WHERE account_id = $1 FOR UPDATE`, accountID).Scan(&status)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// No export yet: no archive to throw away.
		case err != nil:
			return err //nolint:wrapcheck // wrapped by inAccount
		case status == api.ExportPending || status == api.ExportRunning:
			return api.ErrExportActive
		default:
			// The export the account had goes.
			if _, err := tx.Exec(ctx, `DELETE FROM account_exports WHERE account_id = $1`, accountID); err != nil {
				return fmt.Errorf("throwing the old archive away: %w", err)
			}
		}
		e, err = scanAccountExport(tx.QueryRow(ctx, `
			INSERT INTO account_exports (account_id, created_at, not_before) VALUES ($1, $2, $2)
			RETURNING `+accountExportColumns, accountID, at))
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return api.AccountExport{}, fmt.Errorf("account export: %w", err)
	}
	return e, nil
}

// LatestAccountExport implements api.AccountExports.
func (s *Store) LatestAccountExport(ctx context.Context, accountID string) (api.AccountExport, bool, error) {
	var e api.AccountExport
	found := false
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		e, err = scanAccountExport(tx.QueryRow(ctx, `SELECT `+accountExportColumns+` FROM account_exports
			WHERE account_id = $1`, accountID))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil //nolint:nilerr // told of by found
		default:
			found = err == nil
			return err //nolint:wrapcheck // wrapped by readInAccount
		}
	})
	if err != nil {
		return api.AccountExport{}, false, fmt.Errorf("latest account export: %w", err)
	}
	return e, found, nil
}

// ClaimExport implements exporter.Store, across the accounts
// (runsten_claim_account_export), then reads the export within its account.
func (s *Store) ClaimExport(ctx context.Context, at time.Time, lease time.Duration) (exporter.Job, bool, error) {
	var j exporter.Job
	err := s.pool.QueryRow(ctx, "SELECT account_id, id, claimed_at FROM runsten_claim_account_export($1, $2)", at, lease).
		Scan(&j.AccountID, &j.ID, &j.ClaimedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return j, false, nil
	case err != nil:
		return j, false, fmt.Errorf("claiming an export: %w", err)
	}
	j.ClaimedAt = j.ClaimedAt.UTC()
	err = s.readInAccount(ctx, j.AccountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT failures FROM account_exports WHERE id = $1`, j.ID).Scan(&j.Failures) //nolint:wrapcheck // wrapped by readInAccount
	})
	if err != nil {
		return j, false, fmt.Errorf("reading an export: %w", err)
	}
	return j, true, nil
}

// renew renews the job's claim at at, the bytes written so far set to bytes, within
// tx: exporter.ErrLeaseLost when the claim is over.
func renew(ctx context.Context, tx pgx.Tx, job exporter.Job, at time.Time, bytes int64) error {
	tag, err := tx.Exec(ctx, `UPDATE account_exports SET renewed_at = $3, bytes = $4
		WHERE id = $1 AND status = 'running' AND claimed_at = $2`, job.ID, job.ClaimedAt, at.Truncate(time.Microsecond), bytes)
	if err != nil {
		return err //nolint:wrapcheck // wrapped by inAccount
	}
	if tag.RowsAffected() == 0 {
		return exporter.ErrLeaseLost
	}
	return nil
}

// StartExport implements exporter.Store: the build starts, nothing written yet.
func (s *Store) StartExport(ctx context.Context, job exporter.Job, at time.Time) error {
	return s.RenewExport(ctx, job, at, 0)
}

// RenewExport implements exporter.Store: the claim renewed, with the bytes written so
// far.
func (s *Store) RenewExport(ctx context.Context, job exporter.Job, at time.Time, bytes int64) error {
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		return renew(ctx, tx, job, at, bytes)
	})
	if err != nil {
		return fmt.Errorf("renewing an export: %w", err)
	}
	return nil
}

// SaveExport implements exporter.Store: the archive is held until expiresAt, its size
// written with its status done.
func (s *Store) SaveExport(ctx context.Context, job exporter.Job, at time.Time, bytes int64, expiresAt time.Time) error {
	return s.endExport(ctx, job, `UPDATE account_exports SET status = 'done', bytes = $4, ended_at = $3, expires_at = $5
		WHERE id = $1 AND status = 'running' AND claimed_at = $2`,
		at.Truncate(time.Microsecond), bytes, expiresAt.Truncate(time.Microsecond))
}

// RetryExport implements exporter.Store.
func (s *Store) RetryExport(ctx context.Context, job exporter.Job, notBefore time.Time, failures int) error {
	return s.endExport(ctx, job, `UPDATE account_exports SET status = 'pending', claimed_at = NULL, renewed_at = NULL,
		bytes = 0, not_before = $3, failures = $4 WHERE id = $1 AND status = 'running' AND claimed_at = $2`,
		notBefore.Truncate(time.Microsecond), failures)
}

// FailExport implements exporter.Store: a failed export holds no archive.
func (s *Store) FailExport(ctx context.Context, job exporter.Job, at time.Time) error {
	return s.endExport(ctx, job, `UPDATE account_exports SET status = 'failed', error = 'internal',
		bytes = 0, ended_at = $3 WHERE id = $1 AND status = 'running' AND claimed_at = $2`, at.Truncate(time.Microsecond))
}

// endExport ends the export's claim with sql.
func (s *Store) endExport(ctx context.Context, job exporter.Job, sql string, args ...any) error {
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, append([]any{job.ID, job.ClaimedAt}, args...)...)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		if tag.RowsAffected() == 0 {
			return exporter.ErrLeaseLost
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ending an export: %w", err)
	}
	return nil
}

// ExpireExports implements exporter.Store, across the accounts
// (runsten_expire_account_exports).
func (s *Store) ExpireExports(ctx context.Context, at time.Time) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT runsten_expire_account_exports($1)", at).Scan(&n); err != nil {
		return 0, fmt.Errorf("expiring the exports: %w", err)
	}
	return n, nil
}

// HeldExports implements exporter.Store, across the accounts
// (runsten_account_exports_held).
func (s *Store) HeldExports(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, "SELECT runsten_account_exports_held()")
	if err != nil {
		return nil, fmt.Errorf("exports held: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("exports held: %w", err)
	}
	held := make(map[string]bool, len(ids))
	for _, id := range ids {
		held[id] = true
	}
	return held, nil
}
