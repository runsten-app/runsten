package store

import (
	"context"
	"fmt"
	"time"
)

// LastPass gives the number of vehicles, across the accounts, and the collector's
// latest pass over any of them: zero before the first.
func (s *Store) LastPass(ctx context.Context) (vehicles int, passedAt time.Time, err error) {
	var at *time.Time
	if err := s.pool.QueryRow(ctx, "SELECT vehicles, passed_at FROM runsten_last_pass()").Scan(&vehicles, &at); err != nil {
		return 0, time.Time{}, fmt.Errorf("last pass: %w", err)
	}
	if at != nil {
		passedAt = at.UTC()
	}
	return vehicles, passedAt, nil
}

// Connections gives the connections open to the database server, every database's and
// role's, and how many it accepts. pg_stat_database, which every role reads:
// pg_stat_activity hides the other roles' backends from runsten_app.
func (s *Store) Connections(ctx context.Context) (open, maxConns int, err error) {
	if err := s.pool.QueryRow(ctx,
		"SELECT sum(numbackends)::int, current_setting('max_connections')::int FROM pg_stat_database",
	).Scan(&open, &maxConns); err != nil {
		return 0, 0, fmt.Errorf("connections: %w", err)
	}
	return open, maxConns, nil
}
