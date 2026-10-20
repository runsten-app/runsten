package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// A Polestar connection is the owner's credential of the Polestar Data Portal
// (connections.credential): one value, sealed as the tokens, bound to the account and to
// its column. Its access token is never stored: the collector asks for one with it.

// PolestarCredential is what the owner creates in the Data Portal: an OAuth client, its
// secret, and the account it reads.
type PolestarCredential struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	AccountID    string `json:"accountId"`
}

// credentialData is what a credential is sealed with, beside the account.
func credentialData(accountID string) []byte { return []byte(accountID + "/connections.credential") }

// SetPolestarCredential sets the account's Polestar credential, at at, in its Polestar
// connection, created if it has none. A new credential is not known refused.
func (s *Store) SetPolestarCredential(ctx context.Context, accountID string, c PolestarCredential, at time.Time) (string, error) {
	plain, err := json.Marshal(c) //nolint:gosec // to be sealed, never stored in plaintext
	if err != nil {
		return "", fmt.Errorf("polestar credential: %w", err)
	}
	var id string
	err = s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, credential, credential_set_at, credential_last4)
			VALUES ($1, 'polestar', $2, $3, $4)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				credential = EXCLUDED.credential, credential_set_at = EXCLUDED.credential_set_at,
				credential_last4 = EXCLUDED.credential_last4, credential_refused_at = NULL, updated_at = now()
			RETURNING id`, accountID, s.box.Seal(plain, credentialData(accountID)), at.Truncate(time.Microsecond),
			c.ClientID[max(0, len(c.ClientID)-last4):]).Scan(&id)
	})
	return id, err
}

// PolestarCredential returns the credential of the account's Polestar connection.
func (s *Store) PolestarCredential(ctx context.Context, accountID, connectionID string) (PolestarCredential, error) {
	var c PolestarCredential
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, "SELECT credential FROM connections WHERE id = $1 AND provider = 'polestar'", connectionID).
			Scan(&sealed)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		plain, err := s.box.Open(sealed, credentialData(accountID))
		if err != nil {
			return fmt.Errorf("polestar credential for account %s: %w", accountID, err)
		}
		return json.Unmarshal(plain, &c) //nolint:wrapcheck // wrapped by inAccount
	})
	return c, err
}

// SetCredentialRefused implements collector.SourceStore: the provider refused the
// connection's credential set at setAt. A credential set since is left alone, and the
// first refusal's time is kept.
func (s *Store) SetCredentialRefused(ctx context.Context, accountID, connectionID string, setAt time.Time) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE connections SET credential_refused_at = coalesce(credential_refused_at, now())
			WHERE id = $1 AND credential_set_at = $2`, connectionID, setAt)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// SetUnshared implements collector.SourceStore: the vehicle is no longer shared with its
// connection's credential. The first refusal's time is kept.
func (s *Store) SetUnshared(ctx context.Context, accountID, vehicleID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE vehicles SET unshared_at = coalesce(unshared_at, now()) WHERE id = $1`, vehicleID)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}
