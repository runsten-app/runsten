package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"runsten/internal/platform/clock"
)

// tokenLen is the size of a session token: 256 random bits.
const tokenLen = 32

// ThrottledError means that login attempts are blocked for a while.
type ThrottledError struct {
	RetryIn time.Duration
}

func (e *ThrottledError) Error() string {
	return "too many failed logins: retry in " + e.RetryIn.Round(time.Second).String()
}

// Service creates users, logs them in and authenticates their sessions.
type Service struct {
	st       Store
	clk      clock.Clock
	p        Params
	random   io.Reader
	throttle *throttle
	hashing  chan struct{}
	// decoy is hashed when the username is unknown, so that the response time does not
	// tell whether a user exists.
	decoy func() (string, error)
}

// New creates the service. random provides the salts and the session tokens:
// crypto/rand.Reader outside tests.
func New(st Store, clk clock.Clock, p Params, random io.Reader) *Service {
	s := &Service{
		st: st, clk: clk, p: p, random: random,
		throttle: newThrottle(p.MaxFailures, p.FailureWindow),
		hashing:  make(chan struct{}, max(p.MaxHashing, 1)),
	}
	s.decoy = sync.OnceValues(func() (string, error) { return HashPassword("decoy password", p.Hash, random) })
	return s
}

// CreateUser adds a user to the account.
func (s *Service) CreateUser(ctx context.Context, accountID, username, password string) (string, error) {
	name, err := NormalizeUsername(username)
	if err != nil {
		return "", err
	}
	hash, err := s.hash(ctx, password)
	if err != nil {
		return "", err
	}
	id, err := s.st.CreateUser(ctx, accountID, name, hash)
	if err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// SetPassword replaces a user's password and ends all their sessions.
func (s *Service) SetPassword(ctx context.Context, username, password string) error {
	name, err := NormalizeUsername(username)
	if err != nil {
		return err
	}
	u, ok, err := s.st.UserByName(ctx, name)
	switch {
	case err != nil:
		return fmt.Errorf("user: %w", err)
	case !ok:
		return ErrUnknownUser
	}
	hash, err := s.hash(ctx, password)
	if err != nil {
		return err
	}
	if err := s.st.SetPassword(ctx, u.AccountID, u.ID, hash); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return nil
}

// hash validates a new password and hashes it.
func (s *Service) hash(ctx context.Context, password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return HashPassword(password, s.p.Hash, s.random)
}

func (s *Service) acquire(ctx context.Context) (func(), error) {
	select {
	case s.hashing <- struct{}{}:
		return func() { <-s.hashing }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting to hash: %w", ctx.Err())
	}
}

// Login checks a username and password and opens a session. client identifies where
// the attempt comes from (the peer address), for throttling. It returns the session
// token, to hand to the browser only.
//
// Errors: ErrInvalidCredentials, or *ThrottledError after too many failures for this
// username or from this client.
func (s *Service) Login(ctx context.Context, username, password, client string) (string, Session, error) {
	now := s.clk.Now()
	name, nameErr := NormalizeUsername(username)
	keys := []string{"user:" + name, "client:" + client}
	if wait := s.throttle.blocked(now, keys...); wait > 0 {
		return "", Session{}, &ThrottledError{RetryIn: wait}
	}
	u, found := User{}, false
	if nameErr == nil {
		var err error
		if u, found, err = s.st.UserByName(ctx, name); err != nil {
			return "", Session{}, fmt.Errorf("user: %w", err)
		}
	}
	ok, err := s.verify(ctx, u, found, password)
	if err != nil {
		return "", Session{}, err
	}
	if !ok {
		s.throttle.fail(now, keys...)
		return "", Session{}, ErrInvalidCredentials
	}
	s.throttle.reset(keys...)
	return s.Open(ctx, u)
}

// Open opens a session for u, a user authenticated by Login or by another means (the
// hosted offer's identity provider), and returns its token.
func (s *Service) Open(ctx context.Context, u User) (string, Session, error) {
	now := s.clk.Now()
	if err := s.st.PurgeSessions(ctx, u.AccountID, now, now.Add(-s.p.IdleTTL)); err != nil {
		return "", Session{}, fmt.Errorf("purge sessions: %w", err)
	}
	raw := make([]byte, tokenLen)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", Session{}, fmt.Errorf("session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sess := Session{
		AccountID: u.AccountID, UserID: u.ID, Username: u.Username,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.p.SessionTTL),
	}
	id, err := s.st.CreateSession(ctx, sess, tokenHash(token))
	if err != nil {
		return "", Session{}, fmt.Errorf("create session: %w", err)
	}
	sess.ID = id
	return token, sess, nil
}

// verify checks the password, hashing a decoy when the user does not exist.
func (s *Service) verify(ctx context.Context, u User, found bool, password string) (bool, error) {
	if len(password) > maxPasswordBytes {
		return false, nil
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	if !found {
		decoy, err := s.decoy()
		if err != nil {
			return false, err
		}
		_, _ = verifyPassword(decoy, password)
		return false, nil
	}
	ok, err := verifyPassword(u.PasswordHash, password)
	if err != nil {
		return false, fmt.Errorf("password hash of user %s: %w", u.ID, err)
	}
	return ok, nil
}

// Authenticate returns the session of token, and records the activity. It returns
// ErrNoSession if the token is unknown or its session expired.
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if base64.RawURLEncoding.DecodedLen(len(token)) != tokenLen {
		return Session{}, ErrNoSession
	}
	sess, ok, err := s.st.SessionByToken(ctx, tokenHash(token))
	switch {
	case err != nil:
		return Session{}, fmt.Errorf("session: %w", err)
	case !ok:
		return Session{}, ErrNoSession
	}
	now := s.clk.Now()
	if !now.Before(sess.ExpiresAt) || !now.Before(sess.LastSeenAt.Add(s.p.IdleTTL)) {
		if err := s.st.DeleteSession(ctx, sess.AccountID, sess.ID); err != nil {
			return Session{}, fmt.Errorf("delete expired session: %w", err)
		}
		return Session{}, ErrNoSession
	}
	if now.Sub(sess.LastSeenAt) >= s.p.TouchEvery {
		if err := s.st.TouchSession(ctx, sess.AccountID, sess.ID, now); err != nil {
			return Session{}, fmt.Errorf("touch session: %w", err)
		}
		sess.LastSeenAt = now
	}
	return sess, nil
}

// Logout ends a session.
func (s *Service) Logout(ctx context.Context, sess Session) error {
	if err := s.st.DeleteSession(ctx, sess.AccountID, sess.ID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// IsThrottled reports whether err is a *ThrottledError, and returns it.
func IsThrottled(err error) (*ThrottledError, bool) {
	var t *ThrottledError
	ok := errors.As(err, &t)
	return t, ok
}

// tokenHash is what the store keeps of a token: 256 random bits need no salt nor slow
// hash, and a stolen table does not give usable tokens.
func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
