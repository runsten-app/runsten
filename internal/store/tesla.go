package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// A Tesla connection is the owner's refresh token of Tesla's Owner API, kept as a
// Polestar credential is (connections.credential, see polestar.go): sealed, bound to the
// account and to its column. A refresh may give another in exchange, which replaces it
// (RotateTeslaToken); its access tokens are never stored.

// SetTeslaToken sets the account's Tesla refresh token, at at, in its Tesla connection,
// created if it has none. A new token is not known refused.
func (s *Store) SetTeslaToken(ctx context.Context, accountID, token string, at time.Time) (string, error) {
	var id string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, credential, credential_set_at, credential_last4)
			VALUES ($1, 'tesla', $2, $3, $4)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				credential = EXCLUDED.credential, credential_set_at = EXCLUDED.credential_set_at,
				credential_last4 = EXCLUDED.credential_last4, credential_refused_at = NULL, updated_at = now()
			RETURNING id`, accountID, s.box.Seal([]byte(token), credentialData(accountID)), at.Truncate(time.Microsecond),
			token[max(0, len(token)-last4):]).Scan(&id)
	})
	return id, err
}

// TeslaToken implements teslasource.Store: the refresh token of the account's Tesla
// connection, and when the owner set it.
func (s *Store) TeslaToken(ctx context.Context, accountID, connectionID string) (string, time.Time, error) {
	var token string
	var setAt time.Time
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT credential, credential_set_at FROM connections
			WHERE id = $1 AND provider = 'tesla' AND credential IS NOT NULL`, connectionID).Scan(&sealed, &setAt)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		plain, err := s.box.Open(sealed, credentialData(accountID))
		if err != nil {
			return fmt.Errorf("tesla refresh token for account %s: %w", accountID, err)
		}
		token = string(plain)
		return nil
	})
	return token, setAt.UTC(), err
}

// RotateTeslaToken implements teslasource.Store: the refresh token set at setAt is
// replaced with token, which a refresh gave in exchange. A token the owner set since is
// left alone; the time it was set and its last four characters stay the owner's.
func (s *Store) RotateTeslaToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE connections SET credential = $3, updated_at = now()
			WHERE id = $1 AND provider = 'tesla' AND credential_set_at = $2`,
			connectionID, setAt, s.box.Seal([]byte(token), credentialData(accountID)))
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}
