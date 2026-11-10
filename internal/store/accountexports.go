package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/exporter"
)

// The accounts' ZIP archives (account_exports): one per account, its archive held in
// parts (account_export_parts) until it expires or the next export replaces it. Each
// part is written and read in a transaction of its own: neither the build nor a
// download holds a connection or a snapshot for its length.

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
// one it had goes, with its archive, and the new one has an id of its own.
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
			// The export the account had goes, its parts with it.
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

// ResetExport implements exporter.Store: the parts an interrupted build wrote go, the
// export is built again from its start.
func (s *Store) ResetExport(ctx context.Context, job exporter.Job, at time.Time) error {
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		if err := renew(ctx, tx, job, at, 0); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM account_export_parts WHERE export_id = $1`, job.ID)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return fmt.Errorf("resetting an export: %w", err)
	}
	return nil
}

// WriteExportPart implements exporter.Store: the part, and the claim renewed with the
// bytes written so far, in one transaction.
func (s *Store) WriteExportPart(ctx context.Context, job exporter.Job, start int64, data []byte, at time.Time) error {
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		if err := renew(ctx, tx, job, at, start+int64(len(data))); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO account_export_parts (account_id, export_id, start, data)
			VALUES ($1, $2, $3, $4)`, job.AccountID, job.ID, start, data)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return fmt.Errorf("writing a part of an export: %w", err)
	}
	return nil
}

// SaveExport implements exporter.Store: the archive is held until expiresAt, its size
// written with its status done.
func (s *Store) SaveExport(ctx context.Context, job exporter.Job, at time.Time, bytes int64, expiresAt time.Time) error {
	return s.endExport(ctx, job, false, `UPDATE account_exports SET status = 'done', bytes = $4, ended_at = $3, expires_at = $5
		WHERE id = $1 AND status = 'running' AND claimed_at = $2`,
		at.Truncate(time.Microsecond), bytes, expiresAt.Truncate(time.Microsecond))
}

// RetryExport implements exporter.Store: whatever the interrupted build wrote goes.
func (s *Store) RetryExport(ctx context.Context, job exporter.Job, notBefore time.Time, failures int) error {
	return s.endExport(ctx, job, true, `UPDATE account_exports SET status = 'pending', claimed_at = NULL, renewed_at = NULL,
		bytes = 0, not_before = $3, failures = $4 WHERE id = $1 AND status = 'running' AND claimed_at = $2`,
		notBefore.Truncate(time.Microsecond), failures)
}

// FailExport implements exporter.Store: whatever the failed build wrote goes.
func (s *Store) FailExport(ctx context.Context, job exporter.Job, at time.Time) error {
	return s.endExport(ctx, job, true, `UPDATE account_exports SET status = 'failed', error = 'internal',
		bytes = 0, ended_at = $3 WHERE id = $1 AND status = 'running' AND claimed_at = $2`, at.Truncate(time.Microsecond))
}

// endExport ends the export's claim, throwing its parts away with it when throwAway
// says so (a retry builds it again, a failure has none; a done export holds them).
func (s *Store) endExport(ctx context.Context, job exporter.Job, throwAway bool, sql string, args ...any) error {
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, append([]any{job.ID, job.ClaimedAt}, args...)...)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		if tag.RowsAffected() == 0 {
			return exporter.ErrLeaseLost
		}
		if throwAway {
			_, err = tx.Exec(ctx, `DELETE FROM account_export_parts WHERE export_id = $1`, job.ID)
		}
		return err //nolint:wrapcheck // wrapped by inAccount
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

// OpenAccountExport implements api.AccountExports: the account's held archive, read
// part by part, each in a transaction of its own.
func (s *Store) OpenAccountExport(ctx context.Context, accountID string) (api.AccountExport, io.ReadSeeker, error) {
	e, found, err := s.LatestAccountExport(ctx, accountID)
	switch {
	case err != nil:
		return api.AccountExport{}, nil, fmt.Errorf("opening an account export: %w", err)
	case !found || e.Status != api.ExportDone:
		return api.AccountExport{}, nil, fmt.Errorf("%w: the account's export is not held", api.ErrNotFound)
	}
	return e, &partReader{ctx: ctx, s: s, accountID: accountID, exportID: e.ID, size: e.Bytes}, nil
}

// errArchiveGone is returned by a read of an archive thrown away since it was opened:
// expired, or replaced by the account's next export.
var errArchiveGone = errors.New("the archive is no longer held")

// partReader reads an archive held in parts, from any offset: each part is read in a
// transaction of its own, then served from memory.
type partReader struct {
	ctx                  context.Context
	s                    *Store
	accountID, exportID  string
	size, pos, partStart int64
	part                 []byte
}

func (r *partReader) Read(p []byte) (int, error) {
	if r.pos >= r.size {
		return 0, io.EOF
	}
	if r.pos < r.partStart || r.pos >= r.partStart+int64(len(r.part)) {
		if err := r.load(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.part[r.pos-r.partStart:])
	r.pos += int64(n)
	return n, nil
}

// load reads the part that holds pos.
func (r *partReader) load() error {
	var start int64
	var data []byte
	err := r.s.readInAccount(r.ctx, r.accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.ctx, `SELECT start, data FROM account_export_parts
			WHERE export_id = $1 AND start <= $2 ORDER BY start DESC LIMIT 1`, r.exportID, r.pos).Scan(&start, &data) //nolint:wrapcheck // wrapped by readInAccount
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return errArchiveGone
	case err != nil:
		return fmt.Errorf("reading the archive: %w", err)
	case r.pos >= start+int64(len(data)):
		return errArchiveGone
	}
	r.partStart, r.part = start, data
	return nil
}

func (r *partReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.pos
	case io.SeekEnd:
		offset += r.size
	default:
		return r.pos, errors.New("seek: invalid whence")
	}
	if offset < 0 {
		return r.pos, errors.New("seek: negative position")
	}
	r.pos = offset
	return r.pos, nil
}
