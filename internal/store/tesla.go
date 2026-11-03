package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
)

// The providers of the Tesla connections: the Owner API's, and the Fleet API's.
const (
	providerTesla      = "tesla"
	providerTeslaFleet = "tesla_fleet"
)

// A Tesla connection is the owner's refresh token of Tesla's Owner API, kept as a
// Polestar credential is (connections.credential, see polestar.go): sealed, bound to the
// account and to its column. A refresh may give another in exchange, which replaces it
// (RotateTeslaToken); its access tokens are never stored.

// SetTeslaToken implements api.TeslaTokens: it sets the account's Tesla refresh token, at
// at, in its Tesla connection, created if it has none. given is the token the owner gave,
// whose last four characters are kept to tell it apart: token may be the one a refresh
// gave for it. A new token is not known refused.
func (s *Store) SetTeslaToken(ctx context.Context, accountID, token, given string, at time.Time) (string, error) {
	return s.setToken(ctx, providerTesla, accountID, token, given, at)
}

// setToken sets the account's refresh token of provider, at at, in its connection of
// provider, created if it has none; its last four characters are given's.
func (s *Store) setToken(ctx context.Context, provider, accountID, token, given string, at time.Time) (string, error) {
	var id string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, credential, credential_set_at, credential_last4)
			VALUES ($1, $5, $2, $3, $4)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				credential = EXCLUDED.credential, credential_set_at = EXCLUDED.credential_set_at,
				credential_last4 = EXCLUDED.credential_last4, credential_refused_at = NULL, updated_at = now()
			RETURNING id`, accountID, s.box.Seal([]byte(token), credentialData(accountID)), at.Truncate(time.Microsecond),
			given[max(0, len(given)-last4):], provider).Scan(&id)
	})
	return id, err
}

// TeslaToken implements teslasource.Store: the refresh token of the account's Tesla
// connection, and when the owner set it.
func (s *Store) TeslaToken(ctx context.Context, accountID, connectionID string) (string, time.Time, error) {
	return s.token(ctx, providerTesla, accountID, connectionID)
}

// token is the refresh token of the account's connection of provider, and when it was
// set.
func (s *Store) token(ctx context.Context, provider, accountID, connectionID string) (string, time.Time, error) {
	var token string
	var setAt time.Time
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT credential, credential_set_at FROM connections
			WHERE id = $1 AND provider = $2 AND credential IS NOT NULL`, connectionID, provider).Scan(&sealed, &setAt)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		plain, err := s.box.Open(sealed, credentialData(accountID))
		if err != nil {
			return fmt.Errorf("%s refresh token for account %s: %w", provider, accountID, err)
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
	return s.rotateToken(ctx, providerTesla, accountID, connectionID, setAt, token)
}

// rotateToken replaces the refresh token of the account's connection of provider set at
// setAt with token; one set since is left alone.
func (s *Store) rotateToken(ctx context.Context, provider, accountID, connectionID string, setAt time.Time, token string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE connections SET credential = $3, updated_at = now()
			WHERE id = $1 AND provider = $4 AND credential_set_at = $2`,
			connectionID, setAt, s.box.Seal([]byte(token), credentialData(accountID)), provider)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// TeslaConnection implements api.TeslaTokens: what the user is shown again of the
// account's refresh token, never the token itself; a zero SetAt: none.
func (s *Store) TeslaConnection(ctx context.Context, accountID string) (api.TeslaConnection, error) {
	return s.credentialConnection(ctx, accountID, providerTesla)
}

// DeleteTeslaToken implements api.TeslaTokens: the refresh token is erased, the
// connection kept with its vehicles and their history, no longer read until another.
func (s *Store) DeleteTeslaToken(ctx context.Context, accountID string) error {
	return s.deleteCredential(ctx, accountID, providerTesla)
}

// A Fleet API connection (tesla_fleet) is the owner's refresh token of the
// application the instance's operator registered at Tesla, given by the owner's sign-in
// rather than pasted. It is kept as the Owner API's, one per account beside it; the
// core reads none of its vehicles itself, an extension's source does, through these.

// SetTeslaFleetToken sets the account's Fleet API refresh token, at at, in its Fleet API
// connection, created if it has none, and returns the connection. A new token is not
// known refused.
func (s *Store) SetTeslaFleetToken(ctx context.Context, accountID, token string, at time.Time) (string, error) {
	return s.setToken(ctx, providerTeslaFleet, accountID, token, token, at)
}

// TeslaFleetToken returns the refresh token of the account's Fleet API connection, and
// when it was set.
func (s *Store) TeslaFleetToken(ctx context.Context, accountID, connectionID string) (string, time.Time, error) {
	return s.token(ctx, providerTeslaFleet, accountID, connectionID)
}

// RotateTeslaFleetToken replaces the Fleet API refresh token set at setAt with token,
// which a refresh gave in exchange. A token set since is left alone.
func (s *Store) RotateTeslaFleetToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error {
	return s.rotateToken(ctx, providerTeslaFleet, accountID, connectionID, setAt, token)
}

// TeslaFleetConnection returns what the user is shown again of the account's Fleet API
// refresh token, never the token itself; a zero SetAt: none.
func (s *Store) TeslaFleetConnection(ctx context.Context, accountID string) (api.CredentialConnection, error) {
	return s.credentialConnection(ctx, accountID, providerTeslaFleet)
}

// DeleteTeslaFleetToken erases the Fleet API refresh token: the connection is kept with
// its vehicles and their history, no longer read until the owner signs in again.
func (s *Store) DeleteTeslaFleetToken(ctx context.Context, accountID string) error {
	return s.deleteCredential(ctx, accountID, providerTeslaFleet)
}
