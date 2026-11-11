package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
)

// A connection of the Pleos Vehicle Data API is the owner's key of one brand, one of
// hyundai, kia and genesis (connections.credential, the provider pleos_<brand>): one
// value, sealed as the other credentials, bound to the account and to its column. Its
// access token is never stored: the collector asks for one with the key.

// providerPleos is the provider of a brand's key.
func providerPleos(brand string) string { return "pleos_" + brand }

// SetPleosCredential implements api.PleosKeys: the key set at at, in the account's
// connection of the brand, created if it has none. A new key is not known refused.
func (s *Store) SetPleosCredential(ctx context.Context, accountID, brand string, c api.PleosCredential, at time.Time) (string, error) {
	plain, err := json.Marshal(pleosJSON{ClientID: c.ClientID, ClientSecret: c.ClientSecret}) //nolint:gosec // to be sealed, never stored in plaintext
	if err != nil {
		return "", fmt.Errorf("pleos credential: %w", err)
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
			accountID, providerPleos(brand), s.box.Seal(plain, credentialData(accountID)),
			at.Truncate(time.Microsecond), c.ClientID[max(0, len(c.ClientID)-last4):]).Scan(&id)
	})
	return id, err
}

// PleosCredential returns the key of the account's Pleos connection, and the brand its
// provider reads: the collector's Source takes it in every call's header.
func (s *Store) PleosCredential(ctx context.Context, accountID, connectionID string) (api.PleosCredential, string, error) {
	var (
		c        pleosJSON
		provider string
		sealed   []byte
	)
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`SELECT credential, provider FROM connections WHERE id = $1 AND provider LIKE 'pleos_%'`,
			connectionID).Scan(&sealed, &provider)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		plain, err := s.box.Open(sealed, credentialData(accountID))
		if err != nil {
			return fmt.Errorf("pleos credential for account %s: %w", accountID, err)
		}
		return json.Unmarshal(plain, &c) //nolint:wrapcheck // wrapped by inAccount
	})
	return api.PleosCredential{ClientID: c.ClientID, ClientSecret: c.ClientSecret}, provider, err
}

// PleosConnection implements api.PleosKeys: what the user is shown again of the
// account's key of the brand; a zero SetAt: none.
func (s *Store) PleosConnection(ctx context.Context, accountID, brand string) (api.CredentialConnection, error) {
	return s.credentialConnection(ctx, accountID, providerPleos(brand))
}

// DeletePleosCredential implements api.PleosKeys: the brand's key is erased, the
// connection kept with its vehicles and their history, no longer read until another.
func (s *Store) DeletePleosCredential(ctx context.Context, accountID, brand string) error {
	return s.deleteCredential(ctx, accountID, providerPleos(brand))
}

// pleosJSON is the sealed form of a Pleos key.
type pleosJSON struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}
