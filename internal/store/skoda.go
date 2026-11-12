package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
)

// A connection of the MyŠkoda Public API is the owner's key (connections.credential, the
// provider skoda): one value, sealed as the other credentials, bound to the account and
// to its column, and when it expires (credential_expires_at), as the API tells. The
// vehicles it reads are the connection's, their VINs given by the owner with the key.

const providerSkoda = "skoda"

// SetSkodaCredential implements api.SkodaKeys: the key set at at, expiring at expiresAt
// (zero: not told), in the account's Škoda connection, created if it has none. A new key
// is not known refused.
func (s *Store) SetSkodaCredential(ctx context.Context, accountID, key string, expiresAt, at time.Time) (string, error) {
	plain, err := json.Marshal(skodaJSON{APIKey: key}) //nolint:gosec // to be sealed, never stored in plaintext
	if err != nil {
		return "", fmt.Errorf("skoda credential: %w", err)
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
			accountID, providerSkoda, s.box.Seal(plain, credentialData(accountID)),
			at.Truncate(time.Microsecond), key[max(0, len(key)-last4):], nullTime(expiresAt)).Scan(&id)
	})
	return id, err
}

// SkodaCredential returns the key of the account's Škoda connection, and when it was set.
func (s *Store) SkodaCredential(ctx context.Context, accountID, connectionID string) (string, time.Time, error) {
	var (
		c      skodaJSON
		setAt  time.Time
		sealed []byte
	)
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`SELECT credential, credential_set_at FROM connections WHERE id = $1 AND provider = $2 AND credential IS NOT NULL`,
			connectionID, providerSkoda).Scan(&sealed, &setAt)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		plain, err := s.box.Open(sealed, credentialData(accountID))
		if err != nil {
			return fmt.Errorf("skoda credential for account %s: %w", accountID, err)
		}
		return json.Unmarshal(plain, &c) //nolint:wrapcheck // wrapped by inAccount
	})
	return c.APIKey, setAt.UTC(), err
}

// SetCredentialExpiry implements skodasource.Store: the connection's key, set at setAt,
// expires at expiresAt. A key set since is left alone, and an expiry never moves back: a
// response older than the one that told a later expiry does not undo it.
func (s *Store) SetCredentialExpiry(ctx context.Context, accountID, connectionID string, setAt, expiresAt time.Time) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE connections SET credential_expires_at = $3, updated_at = now()
			WHERE id = $1 AND credential_set_at = $2
				AND (credential_expires_at IS NULL OR credential_expires_at < $3)`,
			connectionID, setAt, expiresAt.UTC())
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// SkodaConnection implements api.SkodaKeys: what the user is shown again of the
// account's key; a zero SetAt: none.
func (s *Store) SkodaConnection(ctx context.Context, accountID string) (api.CredentialConnection, error) {
	return s.credentialConnection(ctx, accountID, providerSkoda)
}

// DeleteSkodaCredential implements api.SkodaKeys: the key is erased, the connection kept
// with its vehicles and their history, no longer read until another.
func (s *Store) DeleteSkodaCredential(ctx context.Context, accountID string) error {
	return s.deleteCredential(ctx, accountID, providerSkoda)
}

// skodaJSON is the sealed form of a Škoda key.
type skodaJSON struct {
	APIKey string `json:"apiKey"`
}
