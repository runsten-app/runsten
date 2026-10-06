// Package auth authenticates the users of runsten-api: local accounts with a password
// hashed with argon2id, and server-side sessions identified by a random token that
// the browser keeps in a cookie.
//
// A user belongs to one account; a session carries that account, and every read is
// then restricted to it by the store. The package knows neither HTTP nor the database:
// it declares its Store, and api turns sessions into cookies.
package auth

import (
	"context"
	"errors"
	"time"
)

// Errors returned to the caller. Login never tells an unknown user from a wrong
// password.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrNoSession          = errors.New("no valid session")
	ErrUsernameTaken      = errors.New("username already taken")
	ErrAccountHasUser     = errors.New("the account already has a user")
	ErrUnknownUser        = errors.New("unknown user")
)

// User is a stored user.
type User struct {
	ID           string
	AccountID    string
	Username     string
	PasswordHash string // PHC string, see HashPassword
}

// Session is an authenticated session. Its token is only known to the browser: the
// store keeps its SHA-256.
type Session struct {
	ID         string
	AccountID  string
	UserID     string
	Username   string
	CreatedAt  time.Time
	LastSeenAt time.Time // last authenticated request, written at most every TouchEvery
	ExpiresAt  time.Time // absolute end, whatever the activity
	// TokenID is set when an access token, not a session, authenticated the request:
	// the other fields but AccountID and UserID are then zero.
	TokenID string
}

// Store keeps the users and the sessions.
type Store interface {
	// UserByName finds a user across all accounts: the account is not known before
	// the login.
	UserByName(ctx context.Context, username string) (User, bool, error)
	// CreateUser adds a user to the account. It returns ErrUsernameTaken, or
	// ErrAccountHasUser: an account has a single user for now.
	CreateUser(ctx context.Context, accountID, username, passwordHash string) (string, error)
	// SetPassword replaces the user's password hash and deletes all their sessions.
	SetPassword(ctx context.Context, accountID, userID, passwordHash string) error

	// CreateSession stores a session under the SHA-256 of its token and returns its ID.
	CreateSession(ctx context.Context, s Session, tokenHash []byte) (string, error)
	// SessionByToken finds a session, of any account, by the SHA-256 of its token.
	SessionByToken(ctx context.Context, tokenHash []byte) (Session, bool, error)
	// TouchSession records activity on the session.
	TouchSession(ctx context.Context, accountID, sessionID string, seenAt time.Time) error
	DeleteSession(ctx context.Context, accountID, sessionID string) error
	// PurgeSessions deletes the account's sessions that expired at now, or were idle
	// since before idleSince.
	PurgeSessions(ctx context.Context, accountID string, now, idleSince time.Time) error

	// CreateToken stores an access token under the SHA-256 of its secret and returns its
	// ID.
	CreateToken(ctx context.Context, t Token, tokenHash []byte) (string, error)
	// TokenByHash finds an access token, of any account, by the SHA-256 of its secret.
	TokenByHash(ctx context.Context, tokenHash []byte) (Token, bool, error)
	// TouchToken records the use of an access token.
	TouchToken(ctx context.Context, accountID, tokenID string, usedAt time.Time) error
	// ListTokens returns the user's access tokens, newest first.
	ListTokens(ctx context.Context, accountID, userID string) ([]Token, error)
	// DeleteToken deletes one of the user's access tokens; false: there was none.
	DeleteToken(ctx context.Context, accountID, userID, tokenID string) (bool, error)
}

// Params tunes authentication. The defaults follow the OWASP cheat sheets (password
// storage, session management) for a single-user, self-hosted instance.
type Params struct {
	// SessionTTL is the absolute lifetime of a session: the user logs in again after it.
	SessionTTL time.Duration
	// IdleTTL ends a session unused for this long.
	IdleTTL time.Duration
	// TouchEvery bounds the writes of a session's LastSeenAt, and of an access token's
	// LastUsedAt: once per period, not per request.
	TouchEvery time.Duration

	// MaxFailures failed logins within FailureWindow, for a username or from a client
	// address, block further attempts from either until the window ends.
	MaxFailures   int
	FailureWindow time.Duration

	// Hash is the cost of new password hashes. A stored hash keeps its own cost.
	Hash HashParams
	// MaxHashing bounds the concurrent hash computations: each one takes
	// Hash.MemoryKiB of memory.
	MaxHashing int
}

// DefaultParams returns the default authentication parameters.
func DefaultParams() Params {
	const day = 24 * time.Hour
	return Params{
		SessionTTL:    30 * day,
		IdleTTL:       7 * day,
		TouchEvery:    time.Hour,
		MaxFailures:   10,
		FailureWindow: 15 * time.Minute,
		Hash:          DefaultHashParams(),
		MaxHashing:    2,
	}
}
