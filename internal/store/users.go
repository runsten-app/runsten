package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"runsten/internal/auth"
)

// HasUsers reports whether any account has a user.
func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var ok bool
	if err := s.pool.QueryRow(ctx, "SELECT runsten_has_users()").Scan(&ok); err != nil {
		return false, fmt.Errorf("has users: %w", err)
	}
	return ok, nil
}

// UserByName implements auth.Store.
func (s *Store) UserByName(ctx context.Context, username string) (auth.User, bool, error) {
	var u auth.User
	err := s.pool.QueryRow(ctx, "SELECT id, account_id, username, password_hash FROM runsten_user_by_name($1)", username).
		Scan(&u.ID, &u.AccountID, &u.Username, &u.PasswordHash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return auth.User{}, false, nil
	case err != nil:
		return auth.User{}, false, fmt.Errorf("user by name: %w", err)
	}
	return u, true, nil
}

// CreateUser implements auth.Store.
func (s *Store) CreateUser(ctx context.Context, accountID, username, passwordHash string) (string, error) {
	var id string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "INSERT INTO users (account_id, username, password_hash) VALUES ($1, $2, $3) RETURNING id",
			accountID, username, passwordHash).Scan(&id)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" { // unique_violation
		switch pg.ConstraintName {
		case "users_username_unique":
			return "", auth.ErrUsernameTaken
		case "users_one_per_account":
			return "", auth.ErrAccountHasUser
		}
	}
	return id, err
}

// SetPassword implements auth.Store.
func (s *Store) SetPassword(ctx context.Context, accountID, userID, passwordHash string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE users SET password_hash = $2, password_changed_at = now() WHERE id = $1", userID, passwordHash)
		if err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("user %s not found", userID)
		}
		if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID); err != nil {
			return fmt.Errorf("delete sessions: %w", err)
		}
		return nil
	})
}

// CreateSession implements auth.Store.
func (s *Store) CreateSession(ctx context.Context, sess auth.Session, tokenHash []byte) (string, error) {
	var id string
	err := s.inAccount(ctx, sess.AccountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO sessions (account_id, user_id, token_hash, created_at, last_seen_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			sess.AccountID, sess.UserID, tokenHash, sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt).Scan(&id)
	})
	return id, err
}

// SessionByToken implements auth.Store.
func (s *Store) SessionByToken(ctx context.Context, tokenHash []byte) (auth.Session, bool, error) {
	var sess auth.Session
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, user_id, username, created_at, last_seen_at, expires_at FROM runsten_session($1)`, tokenHash).
		Scan(&sess.ID, &sess.AccountID, &sess.UserID, &sess.Username, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return auth.Session{}, false, nil
	case err != nil:
		return auth.Session{}, false, fmt.Errorf("session: %w", err)
	}
	sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt = sess.CreatedAt.UTC(), sess.LastSeenAt.UTC(), sess.ExpiresAt.UTC()
	return sess, true, nil
}

// TouchSession implements auth.Store.
func (s *Store) TouchSession(ctx context.Context, accountID, sessionID string, seenAt time.Time) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE sessions SET last_seen_at = $2 WHERE id = $1", sessionID, seenAt)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// DeleteSession implements auth.Store.
func (s *Store) DeleteSession(ctx context.Context, accountID, sessionID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM sessions WHERE id = $1", sessionID)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// PurgeSessions implements auth.Store.
func (s *Store) PurgeSessions(ctx context.Context, accountID string, now, idleSince time.Time) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM sessions WHERE expires_at <= $1 OR last_seen_at < $2", now, idleSince)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// CreateToken implements auth.Store.
func (s *Store) CreateToken(ctx context.Context, t auth.Token, tokenHash []byte) (string, error) {
	var id string
	err := s.inAccount(ctx, t.AccountID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO api_tokens (account_id, user_id, name, token_hash, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			t.AccountID, t.UserID, t.Name, tokenHash, t.CreatedAt, nullTime(t.ExpiresAt)).Scan(&id)
	})
	return id, err
}

// TokenByHash implements auth.Store.
func (s *Store) TokenByHash(ctx context.Context, tokenHash []byte) (auth.Token, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, user_id, name, created_at, last_used_at, expires_at FROM runsten_api_token($1)`, tokenHash)
	if err != nil {
		return auth.Token{}, false, fmt.Errorf("access token: %w", err)
	}
	ts, err := scanTokens(rows)
	if err != nil || len(ts) == 0 {
		return auth.Token{}, false, err
	}
	return ts[0], true, nil
}

// TouchToken implements auth.Store.
func (s *Store) TouchToken(ctx context.Context, accountID, tokenID string, usedAt time.Time) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE api_tokens SET last_used_at = $2 WHERE id = $1", tokenID, usedAt)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// ListTokens implements auth.Store.
func (s *Store) ListTokens(ctx context.Context, accountID, userID string) ([]auth.Token, error) {
	var ts []auth.Token
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, account_id, user_id, name, created_at, last_used_at, expires_at FROM api_tokens
			WHERE user_id = $1 ORDER BY created_at DESC, id`, userID)
		if err != nil {
			return fmt.Errorf("list access tokens: %w", err)
		}
		ts, err = scanTokens(rows)
		return err
	})
	return ts, err
}

// DeleteToken implements auth.Store. An ID that is not a UUID is no token's.
func (s *Store) DeleteToken(ctx context.Context, accountID, userID, tokenID string) (bool, error) {
	var deleted bool
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM api_tokens WHERE id::text = $1 AND user_id = $2", tokenID, userID)
		deleted = tag.RowsAffected() == 1
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return deleted, err
}

func scanTokens(rows pgx.Rows) ([]auth.Token, error) {
	var (
		ts               []auth.Token
		t                auth.Token
		lastUsed, expiry *time.Time
	)
	_, err := pgx.ForEachRow(rows, []any{&t.ID, &t.AccountID, &t.UserID, &t.Name, &t.CreatedAt, &lastUsed, &expiry}, func() error {
		t.CreatedAt = t.CreatedAt.UTC()
		t.LastUsedAt, t.ExpiresAt = time.Time{}, time.Time{}
		if lastUsed != nil {
			t.LastUsedAt = lastUsed.UTC()
		}
		if expiry != nil {
			t.ExpiresAt = expiry.UTC()
		}
		ts = append(ts, t)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan access tokens: %w", err)
	}
	return ts, nil
}
