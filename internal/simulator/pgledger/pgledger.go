// Package pgledger keeps the grants of the simulated Volvo ID in PostgreSQL
// (volvoapi.Ledger), so that they outlive the simulator's process. It wants a database
// of its own: its tables are created at Open, without migrations.
package pgledger

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"runsten/internal/simulator/volvoapi"
)

const schema = `
CREATE TABLE IF NOT EXISTS sim_grants (
	id            text PRIMARY KEY,
	authorized_at timestamptz NOT NULL,
	scope         text NOT NULL,
	revoked       boolean NOT NULL DEFAULT false
);
CREATE TABLE IF NOT EXISTS sim_access_tokens (
	token      text PRIMARY KEY,
	grant_id   text NOT NULL REFERENCES sim_grants ON DELETE CASCADE,
	expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS sim_refresh_tokens (
	token     text PRIMARY KEY,
	grant_id  text NOT NULL REFERENCES sim_grants ON DELETE CASCADE,
	issued_at timestamptz NOT NULL,
	used      boolean NOT NULL DEFAULT false
);`

// Ledger is a volvoapi.Ledger on PostgreSQL.
type Ledger struct {
	pool *pgxpool.Pool
}

var _ volvoapi.Ledger = (*Ledger)(nil)

// Open connects to databaseURL and creates the tables missing there.
func Open(ctx context.Context, databaseURL string) (*Ledger, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database connection: %w", err)
	}
	// Without arguments, Exec uses the simple protocol, which allows multiple statements.
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("creating the tables: %w", err)
	}
	return &Ledger{pool: pool}, nil
}

// Close closes the connections.
func (l *Ledger) Close() { l.pool.Close() }

// Load returns everything recorded.
func (l *Ledger) Load(ctx context.Context) (volvoapi.Issued, error) {
	var is volvoapi.Issued
	var err error
	is.Grants, err = collect(ctx, l.pool, "SELECT id, authorized_at, scope, revoked FROM sim_grants",
		func(row pgx.CollectableRow) (g volvoapi.Grant, err error) {
			err = row.Scan(&g.ID, &g.AuthorizedAt, &g.Scope, &g.Revoked)
			return g, err //nolint:wrapcheck // wrapped by collect
		})
	if err != nil {
		return volvoapi.Issued{}, err
	}
	is.Access, err = collect(ctx, l.pool, "SELECT token, grant_id, expires_at FROM sim_access_tokens",
		func(row pgx.CollectableRow) (a volvoapi.AccessToken, err error) {
			err = row.Scan(&a.Token, &a.Grant, &a.Expires)
			return a, err //nolint:wrapcheck // wrapped by collect
		})
	if err != nil {
		return volvoapi.Issued{}, err
	}
	is.Refresh, err = collect(ctx, l.pool, "SELECT token, grant_id, issued_at, used FROM sim_refresh_tokens",
		func(row pgx.CollectableRow) (r volvoapi.RefreshToken, err error) {
			err = row.Scan(&r.Token, &r.Grant, &r.Issued, &r.Used)
			return r, err //nolint:wrapcheck // wrapped by collect
		})
	if err != nil {
		return volvoapi.Issued{}, err
	}
	return is, nil
}

func collect[T any](ctx context.Context, pool *pgxpool.Pool, query string, scan pgx.RowToFunc[T]) ([]T, error) {
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("reading the grants: %w", err)
	}
	out, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("reading the grants: %w", err)
	}
	return out, nil
}

// Issue implements volvoapi.Ledger, in one transaction.
func (l *Ledger) Issue(ctx context.Context, g volvoapi.Grant, used string, access volvoapi.AccessToken, refresh volvoapi.RefreshToken) error {
	err := pgx.BeginFunc(ctx, l.pool, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		b.Queue(`INSERT INTO sim_grants (id, authorized_at, scope, revoked) VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING`, g.ID, g.AuthorizedAt, g.Scope, g.Revoked)
		if used != "" {
			b.Queue("UPDATE sim_refresh_tokens SET used = true WHERE token = $1", used)
		}
		b.Queue("INSERT INTO sim_access_tokens (token, grant_id, expires_at) VALUES ($1, $2, $3)",
			access.Token, access.Grant, access.Expires)
		b.Queue("INSERT INTO sim_refresh_tokens (token, grant_id, issued_at, used) VALUES ($1, $2, $3, $4)",
			refresh.Token, refresh.Grant, refresh.Issued, refresh.Used)
		return tx.SendBatch(ctx, b).Close() //nolint:wrapcheck // wrapped below
	})
	if err != nil {
		return fmt.Errorf("recording the tokens: %w", err)
	}
	return nil
}

// Revoke implements volvoapi.Ledger.
func (l *Ledger) Revoke(ctx context.Context, id string) error {
	if _, err := l.pool.Exec(ctx, "UPDATE sim_grants SET revoked = true WHERE id = $1", id); err != nil {
		return fmt.Errorf("revoking the grant: %w", err)
	}
	return nil
}
