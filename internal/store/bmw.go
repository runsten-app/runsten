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

// providerBMW is the provider of the BMW CarData connections.
const providerBMW = "bmw"

// A BMW connection (ADR 0045) is the owner's client ID of CarData and what its device flow
// gave: one value, sealed as the Polestar ID's (connections.credential, see polestar.go).
// BMW gives a new refresh token at every refresh, which replaces the one kept
// (RotateBMWToken); the access and ID tokens are never stored. The container is created by
// the collector at its first read and recorded in the same value (SetBMWContainer).

// BMWCredential is what a BMW connection holds.
type BMWCredential = api.BMWCredential

// bmwJSON is the sealed form of a BMW connection.
type bmwJSON struct {
	ClientID    string `json:"clientId"`
	GCID        string `json:"gcid"`
	Refresh     string `json:"refresh"`
	ContainerID string `json:"containerId,omitempty"`
}

// SetBMW implements api.BMWCredentials: it sets the account's BMW connection, at at, created if it has none. Its last four
// characters are the client ID's, which the owner finds again in the portal. A new
// credential is not known refused.
func (s *Store) SetBMW(ctx context.Context, accountID string, c BMWCredential, at time.Time) (string, error) {
	plain, err := json.Marshal(bmwJSON(c)) //nolint:gosec // to be sealed, never stored in plaintext
	if err != nil {
		return "", fmt.Errorf("bmw: %w", err)
	}
	var id string
	err = s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, credential, credential_set_at, credential_last4)
			VALUES ($1, $5, $2, $3, $4)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				credential = EXCLUDED.credential, credential_set_at = EXCLUDED.credential_set_at,
				credential_last4 = EXCLUDED.credential_last4, credential_refused_at = NULL, updated_at = now()
			RETURNING id`, accountID, s.box.Seal(plain, credentialData(accountID)), at.Truncate(time.Microsecond),
			c.ClientID[max(0, len(c.ClientID)-last4):], providerBMW).Scan(&id)
	})
	return id, err
}

// BMWCredential returns the credential of the account's BMW connection, and when the
// owner set it.
func (s *Store) BMWCredential(ctx context.Context, accountID, connectionID string) (BMWCredential, time.Time, error) {
	var c bmwJSON
	var setAt time.Time
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT credential, credential_set_at FROM connections
			WHERE id = $1 AND provider = $2 AND credential IS NOT NULL`, connectionID, providerBMW).Scan(&sealed, &setAt)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		c, err = s.openBMW(accountID, sealed)
		return err
	})
	return BMWCredential(c), setAt.UTC(), err
}

func (s *Store) openBMW(accountID string, sealed []byte) (bmwJSON, error) {
	var c bmwJSON
	plain, err := s.box.Open(sealed, credentialData(accountID))
	if err != nil {
		return c, fmt.Errorf("bmw credential for account %s: %w", accountID, err)
	}
	if err := json.Unmarshal(plain, &c); err != nil {
		return c, fmt.Errorf("bmw credential for account %s: %w", accountID, err)
	}
	return c, nil
}

// RotateBMWToken implements bmwsource.Store: the refresh token of the credential set at
// setAt is replaced with token, which a refresh gave. A credential the owner set since is
// left alone; the time it was set and its last four characters stay the owner's.
func (s *Store) RotateBMWToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error {
	return s.updateBMW(ctx, accountID, connectionID, setAt, func(c *bmwJSON) { c.Refresh = token })
}

// SetBMWContainer implements bmwsource.Store: the container of the credential set at
// setAt, which the collector created; one the owner set since is left alone.
func (s *Store) SetBMWContainer(ctx context.Context, accountID, connectionID string, setAt time.Time, containerID string) error {
	return s.updateBMW(ctx, accountID, connectionID, setAt, func(c *bmwJSON) { c.ContainerID = containerID })
}

// updateBMW changes the credential set at setAt under its row's lock, so that a rotation
// and the container's record never undo each other.
func (s *Store) updateBMW(ctx context.Context, accountID, connectionID string, setAt time.Time, change func(*bmwJSON)) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT credential FROM connections
			WHERE id = $1 AND provider = $3 AND credential_set_at = $2 FOR UPDATE`,
			connectionID, setAt, providerBMW).Scan(&sealed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		c, err := s.openBMW(accountID, sealed)
		if err != nil {
			return err
		}
		change(&c)
		plain, err := json.Marshal(c) //nolint:gosec // to be sealed, never stored in plaintext
		if err != nil {
			return fmt.Errorf("bmw: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE connections SET credential = $2, updated_at = now() WHERE id = $1`,
			connectionID, s.box.Seal(plain, credentialData(accountID)))
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// BMWConnection implements api.BMWCredentials: what the user is shown again of the account's BMW connection, never its
// tokens; a zero SetAt: none.
func (s *Store) BMWConnection(ctx context.Context, accountID string) (api.CredentialConnection, error) {
	return s.credentialConnection(ctx, accountID, providerBMW)
}

// DeleteBMW implements api.BMWCredentials: it erases the account's BMW credential; the connection is kept with its vehicles
// and their history, no longer read until another.
func (s *Store) DeleteBMW(ctx context.Context, accountID string) error {
	return s.deleteCredential(ctx, accountID, providerBMW)
}
