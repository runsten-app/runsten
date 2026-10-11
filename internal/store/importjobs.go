package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"runsten/internal/api"
	"runsten/internal/importer"
)

// The imports' jobs (import_jobs). Their token is sealed as the MQTT password, bound to
// the account and to its column, and erased as soon as the job ends.

// importTokenData is what a job's token is sealed with, beside the account.
func importTokenData(accountID string) []byte { return []byte(accountID + "/import_jobs.token") }

const importJobColumns = `id, source, source_vehicle, status, step, imported, skipped, invalid, cutoff,
	coalesce(error, ''), created_at, not_before, ended_at`

func scanImportJob(row pgx.Row) (api.ImportJob, error) {
	var j api.ImportJob
	var cutoff, endedAt *time.Time
	err := row.Scan(&j.ID, &j.Source, &j.SourceVehicle, &j.Status, &j.Step, &j.Imported, &j.Skipped, &j.Invalid,
		&cutoff, &j.Error, &j.CreatedAt, &j.NotBefore, &endedAt)
	j.Cutoff, j.EndedAt = utcOrZero(cutoff), utcOrZero(endedAt)
	j.CreatedAt, j.NotBefore = j.CreatedAt.UTC(), j.NotBefore.UTC()
	return j, err //nolint:wrapcheck // wrapped by the caller
}

// CreateImportJob implements api.ImportJobs.
func (s *Store) CreateImportJob(ctx context.Context, accountID, vehicleID string, req api.ImportRequest, at time.Time) (api.ImportJob, error) {
	sealed := s.box.Seal([]byte(req.Token), importTokenData(accountID))
	var j api.ImportJob
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		j, err = scanImportJob(tx.QueryRow(ctx, `
			INSERT INTO import_jobs (account_id, vehicle_id, source, source_vehicle, token, created_at, not_before)
			VALUES ($1, $2, $3, $4, $5, $6, $6)
			RETURNING `+importJobColumns,
			accountID, vehicleID, req.Source, req.SourceVehicle, sealed, at.Truncate(time.Microsecond)))
		return err
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "import_jobs_active" {
		return api.ImportJob{}, api.ErrImportActive
	}
	if err != nil {
		return api.ImportJob{}, fmt.Errorf("import job: %w", err)
	}
	return j, nil
}

// LatestImportJob implements api.ImportJobs.
func (s *Store) LatestImportJob(ctx context.Context, accountID, vehicleID string) (api.ImportJob, bool, error) {
	var j api.ImportJob
	found := false
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		j, err = scanImportJob(tx.QueryRow(ctx, `SELECT `+importJobColumns+` FROM import_jobs
			WHERE vehicle_id = $1 ORDER BY created_at DESC, id LIMIT 1`, vehicleID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		return api.ImportJob{}, false, fmt.Errorf("latest import job: %w", err)
	}
	return j, found, nil
}

// ClaimImport implements importer.Store, across the accounts (runsten_claim_import),
// then reads the job within its account.
func (s *Store) ClaimImport(ctx context.Context, at time.Time, lease time.Duration) (importer.Job, bool, error) {
	var j importer.Job
	err := s.pool.QueryRow(ctx, "SELECT account_id, id, claimed_at FROM runsten_claim_import($1, $2)", at, lease).
		Scan(&j.AccountID, &j.ID, &j.ClaimedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return j, false, nil
	case err != nil:
		return j, false, fmt.Errorf("claiming an import: %w", err)
	}
	j.ClaimedAt = j.ClaimedAt.UTC()
	var sealed []byte
	var cursor *string
	err = s.inAccount(ctx, j.AccountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT vehicle_id, source, source_vehicle, token, step, cursor, failures FROM import_jobs WHERE id = $1`, j.ID).
			Scan(&j.VehicleID, &j.Source, &j.SourceVehicle, &sealed, &j.Step, &cursor, &j.Failures)
	})
	if err != nil {
		return j, false, fmt.Errorf("reading an import: %w", err)
	}
	if cursor != nil {
		j.Cursor = *cursor
	}
	token, err := s.box.Open(sealed, importTokenData(j.AccountID))
	if err != nil {
		return j, false, fmt.Errorf("import token: %w", err)
	}
	j.Token = string(token)
	return j, true, nil
}

// ImportCutoff implements importer.Store.
func (s *Store) ImportCutoff(ctx context.Context, accountID, vehicleID string, at time.Time) (time.Time, error) {
	var cutoff time.Time
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT coalesce((SELECT min(fetched_at) FROM snapshots WHERE vehicle_id = $1), $2)`,
			vehicleID, at).Scan(&cutoff)
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("import cutoff: %w", err)
	}
	return cutoff.UTC(), nil
}

// SaveImportPage implements importer.Store: the events as SaveImport writes them, and
// the job's progress, in one transaction, so that a page resumed after a crash is never
// counted twice. A claim another worker has taken over since writes nothing.
func (s *Store) SaveImportPage(ctx context.Context, job importer.Job, page importer.Page) (importer.Result, error) {
	var res importer.Result
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		var err error
		if res, err = saveImport(ctx, tx, job.AccountID, job.VehicleID, page.Trips, page.Charges); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE import_jobs SET step = $3, cursor = nullif($4, ''), imported = imported + $5, skipped = skipped + $6,
				invalid = invalid + $7, cutoff = $8, claimed_at = $9::timestamptz, failures = 0,
				status = CASE WHEN $10::boolean THEN 'done' ELSE status END,
				ended_at = CASE WHEN $10 THEN $9::timestamptz END,
				token = CASE WHEN $10 THEN NULL ELSE token END
			WHERE id = $1 AND status = 'running' AND claimed_at = $2`,
			job.ID, job.ClaimedAt, page.Step, page.Cursor, res.Saved, res.Skipped, page.Invalid, res.Cutoff, page.At, page.Done)
		if err != nil {
			return fmt.Errorf("import progress: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return importer.ErrLeaseLost
		}
		return nil
	})
	return res, err
}

// RetryImport implements importer.Store.
func (s *Store) RetryImport(ctx context.Context, job importer.Job, notBefore time.Time, failures int) error {
	return s.endClaim(ctx, job, `UPDATE import_jobs SET status = 'pending', claimed_at = NULL, not_before = $3, failures = $4
		WHERE id = $1 AND status = 'running' AND claimed_at = $2`, notBefore, failures)
}

// FailImport implements importer.Store: the token goes with the claim.
func (s *Store) FailImport(ctx context.Context, job importer.Job, reason importer.Failure, at time.Time) error {
	return s.endClaim(ctx, job, `UPDATE import_jobs SET status = 'failed', error = $3, ended_at = $4, token = NULL
		WHERE id = $1 AND status = 'running' AND claimed_at = $2`, string(reason), at)
}

func (s *Store) endClaim(ctx context.Context, job importer.Job, sql string, args ...any) error {
	err := s.inAccount(ctx, job.AccountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, append([]any{job.ID, job.ClaimedAt}, args...)...)
		if err == nil && tag.RowsAffected() == 0 {
			return importer.ErrLeaseLost
		}
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return fmt.Errorf("import job: %w", err)
	}
	return nil
}
