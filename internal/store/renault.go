package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
)

// A connection of the My Renault app's API (ADR 0050) is the owner's sign-in
// (connections.credential, the provider renault): the login token Gigya gave, the device
// it was given to, the person it signs in and the account's country, sealed as one value
// bound to the account and to its column. The password is never stored. The vehicles it
// reads are the connection's, those the account listed when the owner signed in.

const providerRenault = "renault"

// renaultJSON is the sealed form of a Renault sign-in.
type renaultJSON struct {
	LoginToken string `json:"loginToken"` //nolint:gosec // to be sealed, never stored in plaintext
	GMID       string `json:"gmid"`
	UCID       string `json:"ucid"`
	PersonID   string `json:"personId"`
	Country    string `json:"country"`
}

// SetRenaultCredential implements api.RenaultCredentials: the sign-in set at at, in the
// account's Renault connection, created if it has none. A new sign-in is not known
// refused.
func (s *Store) SetRenaultCredential(ctx context.Context, accountID string, c api.RenaultCredential, at time.Time) (string, error) {
	plain, err := json.Marshal(renaultJSON(c)) //nolint:gosec // to be sealed, never stored in plaintext
	if err != nil {
		return "", fmt.Errorf("renault credential: %w", err)
	}
	var id string
	err = s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, credential, credential_set_at, credential_last4)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				credential = EXCLUDED.credential, credential_set_at = EXCLUDED.credential_set_at,
				credential_last4 = EXCLUDED.credential_last4, credential_refused_at = NULL, updated_at = now()
			RETURNING id`,
			accountID, providerRenault, s.box.Seal(plain, credentialData(accountID)),
			at.Truncate(time.Microsecond), c.PersonID[max(0, len(c.PersonID)-last4):]).Scan(&id)
	})
	return id, err
}

// RenaultCredential returns the sign-in of the account's Renault connection.
func (s *Store) RenaultCredential(ctx context.Context, accountID, connectionID string) (api.RenaultCredential, error) {
	var c renaultJSON
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx,
			`SELECT credential FROM connections WHERE id = $1 AND provider = $2 AND credential IS NOT NULL`,
			connectionID, providerRenault).Scan(&sealed)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		plain, err := s.box.Open(sealed, credentialData(accountID))
		if err != nil {
			return fmt.Errorf("renault credential for account %s: %w", accountID, err)
		}
		return json.Unmarshal(plain, &c) //nolint:wrapcheck // wrapped by inAccount
	})
	return api.RenaultCredential(c), err
}

// RenaultConnection implements api.RenaultCredentials: what the user is shown again of
// the account's sign-in, never the token; a zero SetAt: none.
func (s *Store) RenaultConnection(ctx context.Context, accountID string) (api.CredentialConnection, error) {
	return s.credentialConnection(ctx, accountID, providerRenault)
}

// DeleteRenaultCredential implements api.RenaultCredentials: the sign-in is erased, the
// connection kept with its vehicles and their history, no longer read until another.
func (s *Store) DeleteRenaultCredential(ctx context.Context, accountID string) error {
	return s.deleteCredential(ctx, accountID, providerRenault)
}
