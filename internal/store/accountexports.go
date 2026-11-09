package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"runsten/internal/api"
	"runsten/internal/exporter"
)

// The accounts' ZIP archives (account_exports): one per account, its row holding the
// archive as a large object until the next export throws it away. The archive is
// written and read on a connection of its own, whose descriptor the writer and the
// reader close when they are done: backup and restore carry it with the database.

// The large object's modes: INV_WRITE with INV_READ to build it, INV_READ to download
// it.
const (
	loWrite = 0x20000 | 0x40000
	loRead  = 0x40000
)

const accountExportColumns = `id, status, coalesce(error, ''), bytes, created_at, not_before, ended_at`

func scanAccountExport(row pgx.Row) (api.AccountExport, error) {
	var e api.AccountExport
	var endedAt *time.Time
	err := row.Scan(&e.ID, &e.Status, &e.Error, &e.Bytes, &e.CreatedAt, &e.NotBefore, &endedAt)
	e.CreatedAt, e.NotBefore = e.CreatedAt.UTC(), e.NotBefore.UTC()
	e.EndedAt = utcOrZero(endedAt)
	return e, err //nolint:wrapcheck // wrapped by the caller
}

// CreateAccountExport implements api.AccountExports: the account holds one export; the
// one it had goes, with its archive.
func (s *Store) CreateAccountExport(ctx context.Context, accountID string, at time.Time) (api.AccountExport, error) {
	at = at.Truncate(time.Microsecond)
	var e api.AccountExport
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var oid *uint32
		var status string
		err := tx.QueryRow(ctx, `SELECT status, oid FROM account_exports WHERE account_id = $1 FOR UPDATE`, accountID).
			Scan(&status, &oid)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// No export yet: no archive to throw away.
		case err != nil:
			return err //nolint:wrapcheck // wrapped by inAccount
		case status == api.ExportPending || status == api.ExportRunning:
			return api.ErrExportActive
		default:
			// The export the account had goes, its archive with it.
			if _, err := tx.Exec(ctx, `SELECT lo_unlink($1)`, *oid); err != nil {
				return fmt.Errorf("throwing the old archive away: %w", err)
			}
		}
		e, err = scanAccountExport(tx.QueryRow(ctx, `
			INSERT INTO account_exports (account_id, created_at, not_before) VALUES ($1, $2, $2)
			ON CONFLICT (account_id) DO UPDATE SET status = 'pending', error = NULL, oid = NULL, bytes = 0,
				created_at = $2, not_before = $2, claimed_at = NULL, failures = 0, ended_at = NULL
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

// ExportFile implements exporter.Store: the archive of an interrupted build is thrown
// away, another one is created and held by the export's row, written on a connection
// released when the file is closed.
func (s *Store) ExportFile(ctx context.Context, job exporter.Job) (io.WriteCloser, error) {
	var oid uint32
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		var old *uint32
		if err := tx.QueryRow(ctx, `SELECT oid FROM account_exports WHERE id = $1`, job.ID).Scan(&old); err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		// An interrupted build's archive goes: the export is built again from its start.
		if old != nil {
			if _, err := tx.Exec(ctx, `SELECT lo_unlink($1)`, *old); err != nil {
				return fmt.Errorf("throwing the interrupted archive away: %w", err)
			}
		}
		return tx.QueryRow(ctx, `SELECT lo_create(0)`).Scan(&oid) //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return nil, fmt.Errorf("export archive: %w", err)
	}
	err = s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE account_exports SET oid = $2
			WHERE id = $1 AND status = 'running' AND claimed_at = $3`, job.ID, oid, job.ClaimedAt)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		if tag.RowsAffected() == 0 {
			return exporter.ErrLeaseLost
		}
		return nil
	})
	if err != nil {
		_, _ = s.pool.Exec(ctx, `SELECT lo_unlink($1)`, oid)
		return nil, fmt.Errorf("export archive: %w", err)
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("archive connection: %w", err)
	}
	tx, fd, err := openExportFile(ctx, conn, oid, loWrite)
	if err != nil {
		conn.Release()
		return nil, err
	}
	return &loWriter{ctx: ctx, conn: conn, tx: tx, fd: fd}, nil
}

// SaveExport implements exporter.Store: the archive is held, its size written with its
// status done.
func (s *Store) SaveExport(ctx context.Context, job exporter.Job, at time.Time, bytes int64) error {
	return s.endExport(ctx, job, false, `UPDATE account_exports SET status = 'done', bytes = $4, ended_at = $3
		WHERE id = $1 AND status = 'running' AND claimed_at = $2`, at.Truncate(time.Microsecond), bytes)
}

// RetryExport implements exporter.Store: whatever the interrupted build wrote goes.
func (s *Store) RetryExport(ctx context.Context, job exporter.Job, notBefore time.Time, failures int) error {
	return s.endExport(ctx, job, true, `UPDATE account_exports SET status = 'pending', claimed_at = NULL, oid = NULL,
		bytes = 0, not_before = $3, failures = $4 WHERE id = $1 AND status = 'running' AND claimed_at = $2`,
		notBefore.Truncate(time.Microsecond), failures)
}

// FailExport implements exporter.Store: whatever the failed build wrote goes.
func (s *Store) FailExport(ctx context.Context, job exporter.Job, at time.Time) error {
	return s.endExport(ctx, job, true, `UPDATE account_exports SET status = 'failed', error = 'internal', oid = NULL,
		bytes = 0, ended_at = $3 WHERE id = $1 AND status = 'running' AND claimed_at = $2`, at.Truncate(time.Microsecond))
}

// endExport ends the export's claim, throwing its archive away with it when throwAway
// says so (a retry and a failure build it again; a done export holds it).
func (s *Store) endExport(ctx context.Context, job exporter.Job, throwAway bool, sql string, args ...any) error {
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		if throwAway {
			var oid *uint32
			if err := tx.QueryRow(ctx, `SELECT oid FROM account_exports WHERE id = $1`, job.ID).Scan(&oid); err != nil {
				return err //nolint:wrapcheck // wrapped by inAccount
			}
			if oid != nil {
				if _, err := tx.Exec(ctx, `SELECT lo_unlink($1)`, *oid); err != nil {
					return err //nolint:wrapcheck // wrapped by inAccount
				}
			}
		}
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

// OpenAccountExport implements api.AccountExports: the account's held archive, read on
// a connection released when the reader is closed.
func (s *Store) OpenAccountExport(ctx context.Context, accountID string) (io.ReadCloser, error) {
	var status string
	var oid *uint32
	var bytes int64
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT status, oid, bytes FROM account_exports WHERE account_id = $1`, accountID).
			Scan(&status, &oid, &bytes)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil //nolint:nilerr // told of below
		default:
			return err //nolint:wrapcheck // wrapped by readInAccount
		}
	})
	switch {
	case err != nil:
		return nil, fmt.Errorf("opening an account export: %w", err)
	case status != api.ExportDone:
		return nil, fmt.Errorf("%w: the account's export is not held", api.ErrNotFound)
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("archive connection: %w", err)
	}
	tx, fd, err := openExportFile(ctx, conn, *oid, loRead)
	if err != nil {
		conn.Release()
		return nil, err
	}
	return &loReader{ctx: ctx, conn: conn, tx: tx, fd: fd, left: bytes}, nil
}

// openExportFile opens the archive in a transaction of its own, that holds the large
// object's descriptor: PostgreSQL closes it at its end. The connection c is released
// when the archive is done, by the writer's or the reader's Close.
func openExportFile(ctx context.Context, c *pgxpool.Conn, oid uint32, mode int32) (pgx.Tx, int32, error) {
	// The descriptor is only valid within the transaction: one is held for as long
	// as the archive is written or read, on the one connection c.
	tx, err := c.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("archive transaction: %w", err)
	}
	var fd int32
	if err := tx.QueryRow(ctx, `SELECT lo_open($1, $2)`, oid, mode).Scan(&fd); err != nil {
		_ = tx.Rollback(ctx)
		return nil, 0, fmt.Errorf("opening the archive: %w", err)
	}
	return tx, fd, nil
}

// loWriter writes to a large object's descriptor, held by a transaction of one
// connection.
type loWriter struct {
	ctx  context.Context
	conn *pgxpool.Conn
	tx   pgx.Tx
	fd   int32
}

func (w *loWriter) Write(p []byte) (int, error) {
	var n int
	if err := w.tx.QueryRow(w.ctx, `SELECT lowrite($1, $2)`, w.fd, p).Scan(&n); err != nil {
		return n, fmt.Errorf("writing the archive: %w", err)
	}
	if n < len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

// Close releases the descriptor, its transaction and its connection; the archive
// stays, however far the build went.
func (w *loWriter) Close() error {
	_, err := w.tx.Exec(w.ctx, `SELECT lo_close($1)`, w.fd)
	if err == nil {
		err = w.tx.Commit(w.ctx)
	}
	if err != nil {
		_ = w.tx.Rollback(w.ctx)
	}
	w.conn.Release()
	if err != nil {
		return fmt.Errorf("closing the archive: %w", err)
	}
	return nil
}

// loReader streams a large object's descriptor, held by a transaction of one
// connection, to its end.
type loReader struct {
	ctx  context.Context
	conn *pgxpool.Conn
	tx   pgx.Tx
	fd   int32
	left int64 // the archive's bytes, as the export's row says
	buf  []byte
}

func (r *loReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.left == 0 {
			return 0, io.EOF
		}
		var b []byte
		if err := r.tx.QueryRow(r.ctx, `SELECT loread($1, $2)`, r.fd, min(int64(1<<20), r.left)).Scan(&b); err != nil {
			return 0, fmt.Errorf("reading the archive: %w", err)
		}
		if len(b) == 0 {
			return 0, io.ErrUnexpectedEOF
		}
		r.buf, r.left = b, r.left-int64(len(b))
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// Close releases the descriptor, its transaction and its connection; the archive
// stays, however far the download went.
func (r *loReader) Close() error {
	_, err := r.tx.Exec(r.ctx, `SELECT lo_close($1)`, r.fd)
	if err == nil {
		err = r.tx.Commit(r.ctx)
	}
	if err != nil {
		_ = r.tx.Rollback(r.ctx)
	}
	r.conn.Release()
	if err != nil {
		return fmt.Errorf("closing the archive: %w", err)
	}
	return nil
}
