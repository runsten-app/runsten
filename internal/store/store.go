// Package store is the PostgreSQL adapter. Every access to an account's data goes
// through a transaction restricted to that account by Row-Level Security.
package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"runsten/internal/api"
	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/secretbox"
)

// appRole is the RLS-restricted role the application runs as.
const appRole = "runsten_app"

// Store accesses the database under the application role.
type Store struct {
	pool *pgxpool.Pool
	box  *secretbox.Box
}

// Open opens a pool whose connections all switch to the application role.
// Migrations must already have been applied (Migrate). box encrypts the tokens; it may
// be nil for a command that never reads nor writes them (user management).
func Open(ctx context.Context, databaseURL string, box *secretbox.Box) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database url: %w", err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE "+appRole)
		return err //nolint:wrapcheck // wrapped by pgxpool
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database connection: %w", err)
	}
	return &Store{pool: pool, box: box}, nil
}

// Close closes the pool.
func (s *Store) Close() { s.pool.Close() }

// inAccount runs fn in a transaction restricted to the account accountID.
func (s *Store) inAccount(ctx context.Context, accountID string, fn func(pgx.Tx) error) error {
	return s.inAccountTx(ctx, pgx.TxOptions{}, accountID, fn)
}

// readInAccount runs fn in a read-only transaction restricted to the account, whose
// statements all see the same snapshot: under READ COMMITTED each one would take its
// own, and a derivation committed between two would show half of its events.
func (s *Store) readInAccount(ctx context.Context, accountID string, fn func(pgx.Tx) error) error {
	return s.inAccountTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, accountID, fn)
}

func (s *Store) inAccountTx(ctx context.Context, opts pgx.TxOptions, accountID string, fn func(pgx.Tx) error) error {
	err := pgx.BeginTxFunc(ctx, s.pool, opts, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('runsten.account_id', $1, true)", accountID); err != nil {
			return fmt.Errorf("setting account: %w", err)
		}
		return fn(tx)
	})
	if err != nil {
		return fmt.Errorf("transaction: %w", err)
	}
	return nil
}

func (s *Store) scalar(ctx context.Context, sql string) (string, error) {
	var v string
	if err := s.pool.QueryRow(ctx, sql).Scan(&v); err != nil {
		return "", fmt.Errorf("%s: %w", sql, err)
	}
	return v, nil
}

// CreateAccount creates an account.
func (s *Store) CreateAccount(ctx context.Context) (string, error) {
	return s.scalar(ctx, "SELECT runsten_create_account()")
}

// SingleAccount returns the instance's single account, creating it if needed. It
// fails on a multi-account instance.
func (s *Store) SingleAccount(ctx context.Context) (string, error) {
	return s.scalar(ctx, "SELECT runsten_single_account()")
}

// AddVehicle attaches the VIN to the connection and returns the vehicle ID.
func (s *Store) AddVehicle(ctx context.Context, accountID, connectionID, vin string) (string, error) {
	var id string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO vehicles (account_id, connection_id, vin) VALUES ($1, $2, $3)
			ON CONFLICT (account_id, vin) DO UPDATE SET connection_id = EXCLUDED.connection_id
			RETURNING id`, accountID, connectionID, vin).Scan(&id)
	})
	return id, err
}

// Targets returns the vehicles of all accounts, with their connection, whether it
// requires re-authentication, and whether it has its own application key. Tokens and
// keys are read separately (Credentials, ConnectionKey).
func (s *Store) Targets(ctx context.Context) ([]collector.Target, error) {
	rows, err := s.pool.Query(ctx, `SELECT account_id, vehicle_id, vin, connection_id, coalesce(reauth_reason, ''),
		api_key_set_at, api_key_refused FROM runsten_poll_targets()`)
	if err != nil {
		return nil, fmt.Errorf("vehicles to poll: %w", err)
	}
	var out []collector.Target
	var t collector.Target
	var keySetAt *time.Time
	_, err = pgx.ForEachRow(rows, []any{&t.AccountID, &t.VehicleID, &t.VIN, &t.ConnectionID, &t.ReauthReason, &keySetAt, &t.KeyRefused}, func() error {
		t.KeySetAt = utcOrZero(keySetAt)
		out = append(out, t)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("vehicles to poll: %w", err)
	}
	return out, nil
}

// SaveSnapshot implements collector.Store.
func (s *Store) SaveSnapshot(ctx context.Context, snap collector.Snapshot) (bool, error) {
	stored := false
	err := s.inAccount(ctx, snap.AccountID, func(tx pgx.Tx) error {
		var lastHash []byte
		var lastAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT value_hash, fetched_at FROM snapshots
			WHERE vehicle_id = $1 AND endpoint = $2
			ORDER BY fetched_at DESC LIMIT 1`, snap.VehicleID, string(snap.Endpoint)).Scan(&lastHash, &lastAt)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("latest snapshot: %w", err)
		case bytes.Equal(lastHash, snap.Hash):
			_, err := tx.Exec(ctx, `
				UPDATE snapshots SET checked_at = $4
				WHERE vehicle_id = $1 AND endpoint = $2 AND fetched_at = $3`,
				snap.VehicleID, string(snap.Endpoint), lastAt, snap.FetchedAt)
			if err != nil {
				return fmt.Errorf("check: %w", err)
			}
			return nil
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO snapshots (account_id, vehicle_id, endpoint, fetched_at, checked_at, value_hash, payload)
			VALUES ($1, $2, $3, $4, $4, $5, $6)
			ON CONFLICT DO NOTHING`,
			snap.AccountID, snap.VehicleID, string(snap.Endpoint), snap.FetchedAt, snap.Hash, snap.Payload)
		if err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		stored = tag.RowsAffected() == 1
		return nil
	})
	return stored, err
}

// Vehicles implements api.Reader.
func (s *Store) Vehicles(ctx context.Context, accountID string) ([]api.Vehicle, error) {
	return s.queryVehicles(ctx, accountID, "ORDER BY v.created_at, v.vin")
}

// Vehicle implements api.Reader.
func (s *Store) Vehicle(ctx context.Context, accountID, vehicleID string) (api.Vehicle, bool, error) {
	vs, err := s.queryVehicles(ctx, accountID, "WHERE v.id = $1", vehicleID)
	if err != nil || len(vs) == 0 {
		return api.Vehicle{}, false, err
	}
	return vs[0], true, nil
}

func (s *Store) queryVehicles(ctx context.Context, accountID, clause string, args ...any) ([]api.Vehicle, error) {
	var out []api.Vehicle
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT v.id, v.vin, c.authorized_at, c.refreshed_at, c.reauth_at, coalesce(c.reauth_reason, ''),
				c.api_key IS NOT NULL, c.api_key_refused_at IS NOT NULL, coalesce(v.variant_id, ''), v.ac_max_kw, `+statusColumns+`
			FROM vehicles v JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
			LEFT JOIN collector_status s ON s.vehicle_id = v.id `+clause, args...)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		var v api.Vehicle
		var authorized, refreshed, reauth *time.Time
		var acMaxKW *float64
		var st statusScan
		dest := append([]any{
			&v.ID, &v.VIN, &authorized, &refreshed, &reauth, &v.ReauthReason, &v.OwnKey, &v.KeyRefused,
			&v.VariantID, &acMaxKW,
		}, st.dest()...)
		_, err = pgx.ForEachRow(rows, dest, func() error {
			v.AuthorizedAt, v.RefreshedAt, v.ReauthAt = utcOrZero(authorized), utcOrZero(refreshed), utcOrZero(reauth)
			v.ACMaxKW = core.Value[float64]{}
			if acMaxKW != nil {
				v.ACMaxKW = core.Value[float64]{V: *acMaxKW, OK: true}
			}
			v.Collection = st.collection()
			out = append(out, v)
			return nil
		})
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return out, err
}

// SetVehicleModel implements api.VehicleModels: the variant the user chose (empty: none)
// and the onboard charger they stated. It checks nothing of the catalog: the API does.
func (s *Store) SetVehicleModel(ctx context.Context, accountID, vehicleID, variantID string, acMaxKW core.Value[float64]) error {
	var variant *string
	if variantID != "" {
		variant = &variantID
	}
	var charger *float64
	if acMaxKW.OK {
		charger = &acMaxKW.V
	}
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var changed bool
		err := tx.QueryRow(ctx, `
			UPDATE vehicles v SET variant_id = $2, ac_max_kw = $3
			FROM (SELECT id, variant_id FROM vehicles WHERE id = $1 FOR UPDATE) old
			WHERE v.id = old.id
			RETURNING old.variant_id IS DISTINCT FROM $2`, vehicleID, variant, charger).Scan(&changed)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("vehicle model: %w", err)
		}
		// The energies rest on the variant's net capacity: without its cursor, the next
		// pass of the collector derives the vehicle's whole history again. The charger
		// needs nothing: the costs are computed on reading.
		if changed {
			if _, err := tx.Exec(ctx, "DELETE FROM derivation_cursors WHERE vehicle_id = $1", vehicleID); err != nil {
				return fmt.Errorf("derivation cursor: %w", err)
			}
		}
		return nil
	})
}

func utcOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}
