package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
)

// callsKept is how long the counted calls are kept: the budget reads the last 24
// hours, the rest tells what each account spent lately.
const callsKept = 30 * 24 * time.Hour

// SaveCalls implements collector.Store: it adds the calls to those counted, one
// transaction per account, and deletes the account's hours older than callsKept.
func (s *Store) SaveCalls(ctx context.Context, calls []collector.Calls) error {
	byAccount := map[string][]collector.Calls{}
	for _, c := range calls {
		byAccount[c.AccountID] = append(byAccount[c.AccountID], c)
	}
	for account, cs := range byAccount {
		err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
			latest := cs[0].Hour
			for _, c := range cs {
				if _, err := tx.Exec(ctx, `
					INSERT INTO api_calls (account_id, api, hour, calls) VALUES ($1, $2, $3, $4)
					ON CONFLICT (account_id, api, hour) DO UPDATE SET calls = api_calls.calls + EXCLUDED.calls`,
					c.AccountID, c.API, c.Hour, c.N); err != nil {
					return err //nolint:wrapcheck // wrapped by inAccount
				}
				if c.Hour.After(latest) {
					latest = c.Hour
				}
			}
			_, err := tx.Exec(ctx, "DELETE FROM api_calls WHERE hour < $1", latest.Add(-callsKept))
			return err //nolint:wrapcheck // wrapped by inAccount
		})
		if err != nil {
			return fmt.Errorf("save calls: %w", err)
		}
	}
	return nil
}

// Calls implements collector.Store: the calls of every account counted since a time.
func (s *Store) Calls(ctx context.Context, since time.Time) ([]collector.Calls, error) {
	rows, err := s.pool.Query(ctx, "SELECT account_id, api, hour, calls FROM runsten_api_calls($1)", since)
	if err != nil {
		return nil, fmt.Errorf("calls: %w", err)
	}
	calls, err := pgx.CollectRows(rows, pgx.RowToStructByPos[collector.Calls])
	if err != nil {
		return nil, fmt.Errorf("calls: %w", err)
	}
	for i := range calls {
		calls[i].Hour = calls[i].Hour.UTC()
	}
	return calls, nil
}
