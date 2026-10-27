package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/publish"
)

// A Polestar connection is the owner's credential of the Polestar Data Portal
// (connections.credential): one value, sealed as the tokens, bound to the account and to
// its column. Its access token is never stored: the collector asks for one with it.

// PolestarCredential is what the owner creates in the Data Portal: an OAuth client, its
// secret, and the account it reads.
type PolestarCredential = api.PolestarCredential

// credentialJSON is the sealed form of a credential.
type credentialJSON struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	AccountID    string `json:"accountId"`
}

// credentialData is what a credential is sealed with, beside the account.
func credentialData(accountID string) []byte { return []byte(accountID + "/connections.credential") }

// SetPolestarCredential sets the account's Polestar credential, at at, in its Polestar
// connection, created if it has none. A new credential is not known refused.
func (s *Store) SetPolestarCredential(ctx context.Context, accountID string, c PolestarCredential, at time.Time) (string, error) {
	plain, err := json.Marshal(credentialJSON(c)) //nolint:gosec // to be sealed, never stored in plaintext
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
	var c credentialJSON
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
	return PolestarCredential(c), err
}

// PolestarConnection implements api.PolestarCredentials: what the user is shown again of
// the account's credential, never the credential itself; a zero SetAt: none.
func (s *Store) PolestarConnection(ctx context.Context, accountID string) (api.PolestarConnection, error) {
	return s.credentialConnection(ctx, accountID, "polestar")
}

// credentialConnection is what the user is shown again of the credential of the
// account's connection of provider; a zero SetAt: none.
func (s *Store) credentialConnection(ctx context.Context, accountID, provider string) (api.CredentialConnection, error) {
	var c api.CredentialConnection
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var last4 *string
		var setAt, refusedAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT id, credential_last4, credential_set_at, credential_refused_at FROM connections
			WHERE provider = $1`, provider).Scan(&c.ID, &last4, &setAt, &refusedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		if last4 != nil {
			c.Last4 = *last4
		}
		c.SetAt, c.RefusedAt = utcOrZero(setAt), utcOrZero(refusedAt)
		return nil
	})
	return c, err
}

// PublishCredentials implements the publisher's Store: the credential of each vehicle of
// the account's Polestar connection, by vehicle ID; a zero one when the connection has
// none.
func (s *Store) PublishCredentials(ctx context.Context, accountID string) (map[string]publish.Credential, error) {
	creds := map[string]publish.Credential{}
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT v.id, c.credential_set_at, c.credential_refused_at
			FROM vehicles v JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
			WHERE c.provider = 'polestar'`)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		var id string
		var setAt, refusedAt *time.Time
		_, err = pgx.ForEachRow(rows, []any{&id, &setAt, &refusedAt}, func() error {
			var c publish.Credential
			if setAt != nil {
				c.ExpiresAt = setAt.UTC().Add(api.PolestarLifetime)
				c.RefusedAt = utcOrZero(refusedAt)
			}
			creds[id] = c
			return nil
		})
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return creds, err
}

// DeletePolestarCredential implements api.PolestarCredentials: the credential is erased,
// the connection kept with its vehicles and their history, no longer read until another.
func (s *Store) DeletePolestarCredential(ctx context.Context, accountID string) error {
	return s.deleteCredential(ctx, accountID, "polestar")
}

// deleteCredential erases the credential of the account's connection of provider; the
// connection stays, with its vehicles and their history.
func (s *Store) deleteCredential(ctx context.Context, accountID, provider string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE connections SET credential = NULL, credential_set_at = NULL, credential_last4 = NULL,
				credential_refused_at = NULL, updated_at = now()
			WHERE provider = $1 AND credential IS NOT NULL`, provider)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
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
