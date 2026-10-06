package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/collector"
)

// SaveStatus implements collector.Store. A zero ReadAt or Failure keeps the one saved
// before: a restarted collector knows neither.
func (s *Store) SaveStatus(ctx context.Context, st collector.Status) error {
	f := st.Failure
	var endpoint *string
	var status *int
	if !f.At.IsZero() {
		e := string(f.Endpoint)
		endpoint = &e
		if f.Status != 0 {
			status = &f.Status
		}
	}
	quota := make(map[string]time.Time, len(st.Quota))
	for api, until := range st.Quota {
		quota[api] = until.UTC()
	}
	return s.inAccount(ctx, st.AccountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO collector_status (account_id, vehicle_id, passed_at, mode, read_at, next_at, paused_until, quota,
				failed_at, fail_endpoint, fail_status, fail_kind)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (vehicle_id) DO UPDATE SET
				passed_at = EXCLUDED.passed_at, mode = EXCLUDED.mode,
				read_at = coalesce(EXCLUDED.read_at, collector_status.read_at),
				next_at = EXCLUDED.next_at, paused_until = EXCLUDED.paused_until, quota = EXCLUDED.quota,
				failed_at = coalesce(EXCLUDED.failed_at, collector_status.failed_at),
				fail_endpoint = CASE WHEN EXCLUDED.failed_at IS NULL THEN collector_status.fail_endpoint ELSE EXCLUDED.fail_endpoint END,
				fail_status = CASE WHEN EXCLUDED.failed_at IS NULL THEN collector_status.fail_status ELSE EXCLUDED.fail_status END,
				fail_kind = CASE WHEN EXCLUDED.failed_at IS NULL THEN collector_status.fail_kind ELSE EXCLUDED.fail_kind END`,
			st.AccountID, st.VehicleID, st.PassedAt, st.Mode, nullTime(st.ReadAt), nullTime(st.NextAt),
			nullTime(st.PausedUntil), quota, nullTime(f.At), endpoint, status, nullString(f.Kind))
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// statusColumns are read by scanStatus, in this order, from the collector_status s.
const statusColumns = `s.passed_at, s.mode, s.read_at, s.next_at, s.paused_until, s.quota,
	s.failed_at, s.fail_endpoint, s.fail_status, s.fail_kind`

// statusScan holds the columns of a collector_status, which may be missing (LEFT JOIN).
type statusScan struct {
	passed, read, next, paused, failed *time.Time
	mode, endpoint, kind               *string
	status                             *int
	quota                              map[string]time.Time
}

func (s *statusScan) dest() []any {
	return []any{&s.passed, &s.mode, &s.read, &s.next, &s.paused, &s.quota, &s.failed, &s.endpoint, &s.status, &s.kind}
}

// collection is nil when the collector never wrote the vehicle's status.
func (s *statusScan) collection() *api.Collection {
	if s.passed == nil {
		return nil
	}
	c := &api.Collection{
		PassedAt: s.passed.UTC(), Mode: *s.mode, ReadAt: utcOrZero(s.read), NextAt: utcOrZero(s.next),
		PausedUntil: utcOrZero(s.paused), Quota: map[string]time.Time{},
	}
	for api, until := range s.quota {
		c.Quota[api] = until.UTC()
	}
	if s.failed != nil {
		c.Failure = api.CollectionFailure{At: s.failed.UTC(), Endpoint: *s.endpoint, Kind: *s.kind}
		if s.status != nil {
			c.Failure.Status = *s.status
		}
	}
	return c
}
