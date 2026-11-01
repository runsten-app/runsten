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

// A Polestar ID connection (ADR 0042) is the owner's refresh token of the Polestar ID,
// and in the password mode their email and password: one value, sealed as the Data
// Portal's credential (connections.credential, see polestar.go). The Polestar ID gives a
// new refresh token at every refresh, which replaces the one kept
// (RotatePolestarIDToken); its access tokens are never stored.

// polestarIDJSON is the sealed form of a Polestar ID connection.
type polestarIDJSON struct {
	Refresh  string `json:"refresh"`
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"` //nolint:gosec // to be sealed, never stored in plaintext
}

// The connection modes, in plaintext beside the sealed value.
const (
	polestarIDToken    = "token"
	polestarIDPassword = "password"
)

// SetPolestarID implements api.PolestarIDCredentials: it sets the account's Polestar ID,
// at at, in its Polestar ID connection, created if it has none. Its mode is the password
// one when the email is given. A new credential is not known refused.
func (s *Store) SetPolestarID(ctx context.Context, accountID string, c api.PolestarIDCredential, at time.Time) (string, error) {
	plain, err := json.Marshal(polestarIDJSON(c)) //nolint:gosec // to be sealed, never stored in plaintext
	if err != nil {
		return "", fmt.Errorf("polestar id: %w", err)
	}
	mode := polestarIDToken
	if c.Email != "" {
		mode = polestarIDPassword
	}
	var id string
	err = s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO connections (account_id, provider, credential, credential_set_at, credential_last4, credential_mode)
			VALUES ($1, 'polestar_id', $2, $3, $4, $5)
			ON CONFLICT (account_id, provider) DO UPDATE SET
				credential = EXCLUDED.credential, credential_set_at = EXCLUDED.credential_set_at,
				credential_last4 = EXCLUDED.credential_last4, credential_mode = EXCLUDED.credential_mode,
				credential_refused_at = NULL, updated_at = now()
			RETURNING id`, accountID, s.box.Seal(plain, credentialData(accountID)), at.Truncate(time.Microsecond),
			c.Refresh[max(0, len(c.Refresh)-last4):], mode).Scan(&id)
	})
	return id, err
}

// PolestarIDCredential returns the credential of the account's Polestar ID connection,
// and when the owner set it.
func (s *Store) PolestarIDCredential(ctx context.Context, accountID, connectionID string) (api.PolestarIDCredential, time.Time, error) {
	var c polestarIDJSON
	var setAt time.Time
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT credential, credential_set_at FROM connections
			WHERE id = $1 AND provider = 'polestar_id' AND credential IS NOT NULL`, connectionID).Scan(&sealed, &setAt)
		if err != nil {
			return fmt.Errorf("connection: %w", err)
		}
		c, err = s.openPolestarID(accountID, sealed)
		return err
	})
	return api.PolestarIDCredential(c), setAt.UTC(), err
}

func (s *Store) openPolestarID(accountID string, sealed []byte) (polestarIDJSON, error) {
	var c polestarIDJSON
	plain, err := s.box.Open(sealed, credentialData(accountID))
	if err != nil {
		return c, fmt.Errorf("polestar id credential for account %s: %w", accountID, err)
	}
	if err := json.Unmarshal(plain, &c); err != nil {
		return c, fmt.Errorf("polestar id credential for account %s: %w", accountID, err)
	}
	return c, nil
}

// RotatePolestarIDToken implements polestaridsource.Store: the refresh token of the
// credential set at setAt is replaced with token, which a refresh or a sign-in gave; the
// email and password stay. A credential the owner set since is left alone; the time it was
// set and its last four characters stay the owner's.
func (s *Store) RotatePolestarIDToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT credential FROM connections
			WHERE id = $1 AND provider = 'polestar_id' AND credential_set_at = $2 FOR UPDATE`,
			connectionID, setAt).Scan(&sealed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		c, err := s.openPolestarID(accountID, sealed)
		if err != nil {
			return err
		}
		c.Refresh = token
		plain, err := json.Marshal(c) //nolint:gosec // to be sealed, never stored in plaintext
		if err != nil {
			return fmt.Errorf("polestar id: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE connections SET credential = $2, updated_at = now() WHERE id = $1`,
			connectionID, s.box.Seal(plain, credentialData(accountID)))
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// PolestarIDConnection implements api.PolestarIDCredentials: what the user is shown again
// of the account's Polestar ID, never the token, the email nor the password; a zero SetAt:
// none.
func (s *Store) PolestarIDConnection(ctx context.Context, accountID string) (api.PolestarIDConnection, error) {
	c, err := s.credentialConnection(ctx, accountID, "polestar_id")
	if err != nil || c.SetAt.IsZero() {
		return api.PolestarIDConnection{CredentialConnection: c}, err
	}
	var mode string
	err = s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT credential_mode FROM connections WHERE id = $1", c.ID).Scan(&mode) //nolint:wrapcheck // wrapped by inAccount
	})
	return api.PolestarIDConnection{CredentialConnection: c, Password: mode == polestarIDPassword}, err
}

// DeletePolestarID implements api.PolestarIDCredentials: the credential is erased, the
// password with it; the connection is kept with its vehicles and their history, no longer
// read until another.
func (s *Store) DeletePolestarID(ctx context.Context, accountID string) error {
	return s.deleteCredential(ctx, accountID, "polestar_id")
}
