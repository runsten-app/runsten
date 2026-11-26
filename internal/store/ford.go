package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
)

// providerFord is the provider of the FordConnect Query connections.
const providerFord = "ford"

// A Ford connection is the owner's application on Ford's developer portal and the sign-in
// they made with it: one value, sealed as the other credentials
// (connections.credential), bound to the account and to its column. A refresh may give a
// new refresh token, which replaces the one kept (RotateFordToken); the access is never
// stored. When the grant ends is kept beside it (credential_expires_at,
// SetCredentialExpiry).

// FordCredential is what a Ford connection holds.
type FordCredential = api.FordCredential

// fordJSON is the sealed form of a Ford connection.
type fordJSON struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"` //nolint:gosec // to be sealed, never stored in plaintext
	RedirectURI  string `json:"redirectUri"`
	Refresh      string `json:"refresh"`
}

// SetFord sets the account's Ford connection, at at, created if it has none; its grant
// ends at expiresAt (zero: not told). Its last four characters are the client ID's, which
// the owner finds again in the portal. A new credential is not known refused.
func (s *Store) SetFord(ctx context.Context, accountID string, c FordCredential, expiresAt, at time.Time) (string, error) {
	plain, err := json.Marshal(fordJSON(c)) //nolint:gosec // to be sealed, never stored in plaintext
	if err != nil {
		return "", fmt.Errorf("ford: %w", err)
	}
	var id string
	err = s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, credential, credential_set_at, credential_last4, credential_expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				credential = EXCLUDED.credential, credential_set_at = EXCLUDED.credential_set_at,
				credential_last4 = EXCLUDED.credential_last4, credential_expires_at = EXCLUDED.credential_expires_at,
				credential_refused_at = NULL, updated_at = now()
			RETURNING id`,
			accountID, providerFord, s.box.Seal(plain, credentialData(accountID)), at.Truncate(time.Microsecond),
			c.ClientID[max(0, len(c.ClientID)-last4):], nullTime(expiresAt)).Scan(&id)
	})
	return id, err
}

// FordCredential returns the credential of the account's Ford connection, and when the
// owner set it.
func (s *Store) FordCredential(ctx context.Context, accountID, connectionID string) (FordCredential, time.Time, error) {
	var c fordJSON
	var setAt time.Time
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT credential, credential_set_at FROM connections
			WHERE id = $1 AND provider = $2 AND credential IS NOT NULL`, connectionID, providerFord).Scan(&sealed, &setAt)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		c, err = s.openFord(accountID, sealed)
		return err
	})
	return FordCredential(c), setAt.UTC(), err
}

func (s *Store) openFord(accountID string, sealed []byte) (fordJSON, error) {
	var c fordJSON
	plain, err := s.box.Open(sealed, credentialData(accountID))
	if err != nil {
		return c, fmt.Errorf("ford credential for account %s: %w", accountID, err)
	}
	if err := json.Unmarshal(plain, &c); err != nil {
		return c, fmt.Errorf("ford credential for account %s: %w", accountID, err)
	}
	return c, nil
}

// RotateFordToken implements fordsource.Store: the refresh token of the credential set at
// setAt is replaced with token, which a refresh gave, under its row's lock. A credential
// the owner set since is left alone; the time it was set and its last four characters
// stay the owner's.
func (s *Store) RotateFordToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT credential FROM connections
			WHERE id = $1 AND provider = $3 AND credential_set_at = $2 FOR UPDATE`,
			connectionID, setAt, providerFord).Scan(&sealed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		c, err := s.openFord(accountID, sealed)
		if err != nil {
			return err
		}
		c.Refresh = token
		plain, err := json.Marshal(c) //nolint:gosec // to be sealed, never stored in plaintext
		if err != nil {
			return fmt.Errorf("ford: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE connections SET credential = $2, updated_at = now() WHERE id = $1`,
			connectionID, s.box.Seal(plain, credentialData(accountID)))
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// FordConnection returns what the user is shown again of the account's Ford connection,
// never its secret nor its tokens; a zero SetAt: none.
func (s *Store) FordConnection(ctx context.Context, accountID string) (api.CredentialConnection, error) {
	return s.credentialConnection(ctx, accountID, providerFord)
}

// DeleteFord erases the account's Ford credential; the connection is kept with its
// vehicle and its history, no longer read until another.
func (s *Store) DeleteFord(ctx context.Context, accountID string) error {
	return s.deleteCredential(ctx, accountID, providerFord)
}
