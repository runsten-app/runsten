package store

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/core"
)

// A connection of the demo source (connections.provider demo) has no secret: the vehicle
// is simulated by the instance. The collector only reads a connection whose credential
// was set, and the schema ties the two (connections_credential_check), so the credential
// is a marker: a constant sealed as any other, with "demo" for its last four characters.
// Nothing reads it back.

const providerDemo = "demo"

// demoMarker is the sealed constant, and demoLast4 what the connection shows of it.
const (
	demoMarker = "demo"
	demoLast4  = "demo"
)

// SetDemoConnection implements api.DemoConnections: the account's demo connection, set
// at at, created if it has none; it returns its ID. It is pending, as a refused credential
// is (credential_refused_at at): the collector leaves a connection so marked alone, so
// that it reads nothing of the vehicle before its history is written; DemoReady clears
// the mark.
func (s *Store) SetDemoConnection(ctx context.Context, accountID string, at time.Time) (string, error) {
	var id string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, credential, credential_set_at, credential_last4, credential_refused_at)
			VALUES ($1, $2, $3, $4, $5, $4)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				credential = EXCLUDED.credential, credential_set_at = EXCLUDED.credential_set_at,
				credential_last4 = EXCLUDED.credential_last4, credential_refused_at = EXCLUDED.credential_refused_at,
				updated_at = now()
			RETURNING id`,
			accountID, providerDemo, s.box.Seal([]byte(demoMarker), credentialData(accountID)),
			at.Truncate(time.Microsecond), demoLast4).Scan(&id)
	})
	return id, err
}

// DemoReady implements api.DemoConnections: the history is written, the connection no
// longer pending.
func (s *Store) DemoReady(ctx context.Context, accountID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE connections SET credential_refused_at = NULL, updated_at = now()
			WHERE provider = $1`, providerDemo)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// DemoConnection implements api.DemoConnections: the account's demo connection; a zero
// SetAt: none.
func (s *Store) DemoConnection(ctx context.Context, accountID string) (api.CredentialConnection, error) {
	return s.credentialConnection(ctx, accountID, providerDemo)
}

// DeleteDemoConnection implements api.DemoConnections: the connection and its vehicles
// with their history, which have no other source to come back from. Nothing to delete is
// no error.
func (s *Store) DeleteDemoConnection(ctx context.Context, accountID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			DELETE FROM vehicles WHERE connection_id IN (SELECT id FROM connections WHERE provider = $1)`,
			providerDemo); err != nil {
			return fmt.Errorf("delete demo vehicles: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM connections WHERE provider = $1`, providerDemo); err != nil {
			return fmt.Errorf("delete demo connection: %w", err)
		}
		return nil
	})
}

// HistoricSnapshot is a response of a vehicle's past, to be saved after the fact.
type HistoricSnapshot struct {
	Kind      core.Kind
	FetchedAt time.Time
	Payload   []byte
	Hash      []byte // fingerprint of the values, as the collector's
}

// SaveHistory stores the vehicle's past responses, oldest first, as the collector would
// have saved them one by one: a run of the same values (the same Hash, of one kind) is
// kept as its first snapshot read again until the last of the run (checked_at). One
// already there, at the same time, is left alone. It does not look at what is stored
// after them: it is for a vehicle whose history starts here, and writes the past of one
// kind in order.
func (s *Store) SaveHistory(ctx context.Context, accountID, vehicleID string, snaps []HistoricSnapshot) error {
	if len(snaps) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for i := 0; i < len(snaps); {
		first, last := snaps[i], snaps[i]
		for i++; i < len(snaps) && snaps[i].Kind == first.Kind && bytes.Equal(snaps[i].Hash, first.Hash); i++ {
			last = snaps[i]
		}
		batch.Queue(`
			INSERT INTO snapshots (account_id, vehicle_id, source, endpoint, fetched_at, checked_at, value_hash, payload)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT DO NOTHING`,
			accountID, vehicleID, first.Kind.Source, first.Kind.Endpoint, first.FetchedAt, last.FetchedAt, first.Hash, first.Payload)
	}
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		results := tx.SendBatch(ctx, batch)
		for range batch.Len() {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return fmt.Errorf("insert snapshots: %w", err)
			}
		}
		return results.Close() //nolint:wrapcheck // wrapped by inAccount
	})
}
