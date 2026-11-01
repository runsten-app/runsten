package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
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
