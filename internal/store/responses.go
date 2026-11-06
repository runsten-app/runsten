package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/core"
)

// EachResponse implements api.Responses: calls fn with each response stored of the
// vehicle fetched within [from, to), oldest first, as of one snapshot of the database.
// The rows are read as fn takes them, never held: a month of a car read every 15 s
// is hundreds of megabytes. An error of fn stops the read and is returned.
func (s *Store) EachResponse(ctx context.Context, accountID, vehicleID string, from, to time.Time, fn func(api.Response) error) error {
	return s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		// The primary key leads with the vehicle: the period and the order are a scan of
		// its rows and a sort, which PostgreSQL spills to disk past work_mem.
		rows, err := tx.Query(ctx, `
			SELECT source, endpoint, fetched_at, payload FROM snapshots
			WHERE vehicle_id = $1 AND fetched_at >= $2 AND fetched_at < $3
			ORDER BY fetched_at, source, endpoint`, vehicleID, from, to)
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var r api.Response
			if err := rows.Scan(&r.Source, &r.Endpoint, &r.FetchedAt, &r.Payload); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			if err := fn(r); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("rows: %w", err)
		}
		return nil
	})
}

// LatestResponse gives the latest response stored of the vehicle of kind k, its payload
// included; ok is false when none is.
func (s *Store) LatestResponse(ctx context.Context, accountID, vehicleID string, k core.Kind) (r api.Response, ok bool, err error) {
	err = s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT source, endpoint, fetched_at, payload FROM snapshots
			WHERE vehicle_id = $1 AND source = $2 AND endpoint = $3
			ORDER BY fetched_at DESC LIMIT 1`, vehicleID, k.Source, k.Endpoint).
			Scan(&r.Source, &r.Endpoint, &r.FetchedAt, &r.Payload)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("latest response: %w", err)
		}
		ok = true
		return nil
	})
	return r, ok, err
}

// HasResponse tells whether a response of kind k is stored of the vehicle, without
// reading it.
func (s *Store) HasResponse(ctx context.Context, accountID, vehicleID string, k core.Kind) (bool, error) {
	var has bool
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM snapshots WHERE vehicle_id = $1 AND source = $2 AND endpoint = $3)`,
			vehicleID, k.Source, k.Endpoint).Scan(&has)
		if err != nil {
			return fmt.Errorf("has response: %w", err)
		}
		return nil
	})
	return has, err
}
