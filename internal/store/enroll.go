package store

import (
	"context"
	"fmt"
)

// WithAccountLock implements api.AccountLocks: fn runs while the account's enrollment
// lock is held, a transaction-level advisory lock of a connection of its own, released
// when fn returns, or when the connection is lost. fn's own reads and writes go through
// the store as usual: the lock only serializes those who take it, every path that
// checks the account's cap of vehicles and then adds to them, across the instances of
// runsten-api. fn's error is returned as it is.
func (s *Store) WithAccountLock(ctx context.Context, accountID string, fn func(context.Context) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("account lock: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('runsten_enroll:' || $1))", accountID); err != nil {
		return fmt.Errorf("account lock: %w", err)
	}
	return fn(ctx)
}
