package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/oauth"
)

// credentialColumns are read by scanCredentials, in this order.
const credentialColumns = `access_token, refresh_token, expires_at, refreshed_at, authorized_at,
	reauth_at, coalesce(reauth_reason, '')`

// SaveConnection stores (or replaces) the account's Volvo connection, tokens
// encrypted. A new authorization clears any re-authentication requirement; the
// connection's application key stays.
func (s *Store) SaveConnection(ctx context.Context, accountID string, c oauth.Credentials) (string, error) {
	var id string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, access_token, refresh_token, expires_at, refreshed_at, authorized_at)
			VALUES ($1, 'volvo', $2, $3, $4, $5, $6)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				access_token = EXCLUDED.access_token, refresh_token = EXCLUDED.refresh_token,
				expires_at = EXCLUDED.expires_at, refreshed_at = EXCLUDED.refreshed_at,
				authorized_at = EXCLUDED.authorized_at, reauth_at = NULL, reauth_reason = NULL, updated_at = now()
			RETURNING id`,
			accountID, s.seal(accountID, c.AccessToken), s.seal(accountID, c.RefreshToken),
			nullTime(c.ExpiresAt), nullTime(c.RefreshedAt), nullTime(c.AuthorizedAt)).Scan(&id)
	})
	return id, err
}

// Credentials implements oauth.Store.
func (s *Store) Credentials(ctx context.Context, accountID, connectionID string) (oauth.Credentials, error) {
	var c oauth.Credentials
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		c, err = s.scanCredentials(accountID, tx.QueryRow(ctx,
			"SELECT "+credentialColumns+" FROM connections WHERE id = $1", connectionID))
		return err
	})
	return c, err
}

// UpdateCredentials implements oauth.Store. The row lock (FOR UPDATE) serializes the
// refreshers of a connection, across processes: a second refresher waits, then reads
// the credentials the first one stored.
func (s *Store) UpdateCredentials(ctx context.Context, accountID, connectionID string, fn func(oauth.Credentials) (oauth.Credentials, bool)) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		cur, err := s.scanCredentials(accountID, tx.QueryRow(ctx,
			"SELECT "+credentialColumns+" FROM connections WHERE id = $1 FOR UPDATE", connectionID))
		if err != nil {
			return err
		}
		next, changed := fn(cur)
		if !changed {
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE connections SET access_token = $2, refresh_token = $3, expires_at = $4, refreshed_at = $5,
				authorized_at = $6, reauth_at = $7, reauth_reason = $8, updated_at = now()
			WHERE id = $1`,
			connectionID, s.seal(accountID, next.AccessToken), s.seal(accountID, next.RefreshToken),
			nullTime(next.ExpiresAt), nullTime(next.RefreshedAt), nullTime(next.AuthorizedAt),
			nullTime(next.ReauthAt), nullString(next.ReauthReason))
		if err != nil {
			return fmt.Errorf("update credentials: %w", err)
		}
		return nil
	})
}

// StaleConnections implements oauth.Store.
func (s *Store) StaleConnections(ctx context.Context, before time.Time) ([]oauth.ConnectionRef, error) {
	rows, err := s.pool.Query(ctx, "SELECT account_id, connection_id FROM runsten_stale_connections($1)", before)
	if err != nil {
		return nil, fmt.Errorf("stale connections: %w", err)
	}
	refs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[oauth.ConnectionRef])
	if err != nil {
		return nil, fmt.Errorf("stale connections: %w", err)
	}
	return refs, nil
}

func (s *Store) scanCredentials(accountID string, row pgx.Row) (oauth.Credentials, error) {
	var (
		c                                        oauth.Credentials
		access, refresh                          []byte
		expires, refreshed, authorized, reauthAt *time.Time
	)
	if err := row.Scan(&access, &refresh, &expires, &refreshed, &authorized, &reauthAt, &c.ReauthReason); err != nil {
		return c, fmt.Errorf("connection: %w", err)
	}
	if access != nil { // NULL: an application key set before the Volvo ID was connected
		plain, err := s.box.Open(access, []byte(accountID))
		if err != nil {
			return c, fmt.Errorf("access token for account %s: %w", accountID, err)
		}
		c.AccessToken = string(plain)
	}
	if refresh != nil {
		plain, err := s.box.Open(refresh, []byte(accountID))
		if err != nil {
			return c, fmt.Errorf("refresh token for account %s: %w", accountID, err)
		}
		c.RefreshToken = string(plain)
	}
	for dst, src := range map[*time.Time]*time.Time{
		&c.ExpiresAt: expires, &c.RefreshedAt: refreshed, &c.AuthorizedAt: authorized, &c.ReauthAt: reauthAt,
	} {
		if src != nil {
			*dst = src.UTC()
		}
	}
	return c, nil
}

// seal encrypts a token bound to its account; an empty token is stored as NULL.
func (s *Store) seal(accountID, token string) []byte {
	if token == "" {
		return nil
	}
	return s.box.Seal([]byte(token), []byte(accountID))
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func nullString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
