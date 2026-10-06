package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies pending migrations in file name order, within a single
// transaction. It connects as the databaseURL user, who becomes the owner of the
// tables.
func Migrate(ctx context.Context, databaseURL string) (applied []string, err error) {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database connection: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	sort.Strings(names)

	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		// Lock: two instances starting together do not migrate at the same time.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('runsten_migrate'))"); err != nil {
			return fmt.Errorf("lock: %w", err)
		}
		if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return fmt.Errorf("schema_migrations: %w", err)
		}
		for _, name := range names {
			var done bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT FROM schema_migrations WHERE name = $1)", name).Scan(&done); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if done {
				continue
			}
			sql, err := migrations.ReadFile(name)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			// Without arguments, Exec uses the simple protocol, which allows multiple statements.
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", name); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			applied = append(applied, name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("migration: %w", err)
	}
	return applied, nil
}
