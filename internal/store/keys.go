package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/oauth"
)

// The application key of a connection (connections.api_key) is encrypted as the
// tokens, bound to the account. It may be set before the Volvo ID is connected: the
// connection row then has no token.

// last4 is how much of a key the user is shown again: enough to tell two apart. It is
// also kept in plaintext (api_key_last4), for readers without the sealing key.
const last4 = 4

// AccountKey implements api.Keys: the account's own key, zero if it has none.
func (s *Store) AccountKey(ctx context.Context, accountID string) (oauth.APIKey, error) {
	var k oauth.APIKey
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		var setAt *time.Time
		err := tx.QueryRow(ctx, "SELECT id, api_key, api_key_set_at FROM connections WHERE provider = 'volvo'").Scan(&k.ConnectionID, &sealed, &setAt)
		if errors.Is(err, pgx.ErrNoRows) || sealed == nil {
			k = oauth.APIKey{}
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		if k.Value, err = s.openKey(accountID, sealed); err != nil {
			return err
		}
		k.SetAt = setAt.UTC()
		return nil
	})
	return k, err
}

// ConnectionKey implements collector.Store: the connection's own key, empty if it has
// none.
func (s *Store) ConnectionKey(ctx context.Context, accountID, connectionID string) (string, error) {
	var key string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		if err := tx.QueryRow(ctx, "SELECT api_key FROM connections WHERE id = $1", connectionID).Scan(&sealed); err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		if sealed == nil {
			return nil
		}
		var err error
		key, err = s.openKey(accountID, sealed)
		return err
	})
	return key, err
}

// SetKeyRefused implements oauth.Enrollment and collector.Store: whether the key set at
// setAt was refused. A key set since, or none, is left alone.
func (s *Store) SetKeyRefused(ctx context.Context, accountID, connectionID string, setAt time.Time, refused bool) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE connections SET api_key_refused_at = CASE WHEN $3::boolean THEN coalesce(api_key_refused_at, now()) END
			WHERE id = $1 AND api_key_set_at = $2`, connectionID, setAt, refused)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// SetAPIKey implements api.Keys: the account's own key, set at at, in its connection,
// created without a token if the Volvo ID is not connected yet. A new key is not known
// refused.
func (s *Store) SetAPIKey(ctx context.Context, accountID, key string, at time.Time) (string, error) {
	var id string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, api_key, api_key_set_at, api_key_last4) VALUES ($1, 'volvo', $2, $3, $4)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				api_key = EXCLUDED.api_key, api_key_set_at = EXCLUDED.api_key_set_at, api_key_last4 = EXCLUDED.api_key_last4,
				api_key_refused_at = NULL, updated_at = now()
			RETURNING id`, accountID, s.box.Seal([]byte(key), []byte(accountID)), at.Truncate(time.Microsecond),
			key[max(0, len(key)-last4):]).Scan(&id)
	})
	return id, err
}

// DeleteAPIKey implements api.Keys: the instance's key applies again. A connection
// without a token keeps nothing: it is deleted.
func (s *Store) DeleteAPIKey(ctx context.Context, accountID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM connections WHERE access_token IS NULL"); err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		_, err := tx.Exec(ctx, `
			UPDATE connections SET api_key = NULL, api_key_set_at = NULL, api_key_last4 = NULL, api_key_refused_at = NULL,
				updated_at = now()
			WHERE api_key IS NOT NULL`)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// Connection implements api.Keys: the account's connection, zero if it has none.
func (s *Store) Connection(ctx context.Context, accountID string) (api.Connection, error) {
	var c api.Connection
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		var setAt, refusedAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT id, access_token IS NOT NULL, coalesce(reauth_reason, ''), api_key, api_key_set_at, api_key_refused_at
			FROM connections WHERE provider = 'volvo'`).Scan(&c.ID, &c.Connected, &c.ReauthReason, &sealed, &setAt, &refusedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		if sealed == nil {
			return nil
		}
		key, err := s.openKey(accountID, sealed)
		if err != nil {
			return err
		}
		c.Key = api.KeyInfo{Last4: key[max(0, len(key)-last4):], SetAt: setAt.UTC(), RefusedAt: utcOrZero(refusedAt)}
		return nil
	})
	return c, err
}

func (s *Store) openKey(accountID string, sealed []byte) (string, error) {
	plain, err := s.box.Open(sealed, []byte(accountID))
	if err != nil {
		return "", fmt.Errorf("application key for account %s: %w", accountID, err)
	}
	return string(plain), nil
}
