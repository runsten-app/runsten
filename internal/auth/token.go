package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// TokenPrefix starts every access token: it tells them apart from session tokens, and
// lets a secret scanner recognize one.
const TokenPrefix = "rst_"

// MaxTokenName bounds the name of an access token, in characters.
const MaxTokenName = 64

// Errors of the access tokens.
var (
	ErrNoToken          = errors.New("no valid access token")
	ErrUnknownToken     = errors.New("unknown access token")
	ErrInvalidTokenName = fmt.Errorf("the name of an access token must have 1 to %d characters, without control characters", MaxTokenName)
)

// Token is a personal access token: a program reads the API with it on a user's behalf,
// without a session. Its secret is shown once, when it is issued; the store keeps its
// SHA-256.
type Token struct {
	ID         string
	AccountID  string
	UserID     string
	Name       string // chosen by the user, to tell their tokens apart
	CreatedAt  time.Time
	LastUsedAt time.Time // zero: never used; written at most every TouchEvery
	ExpiresAt  time.Time // zero: never expires
}

// Expired reports whether the token no longer authenticates at now.
func (t Token) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && !now.Before(t.ExpiresAt)
}

// IssueToken creates an access token for the user, valid for ttl (zero: without end),
// and returns its secret, to show to the user only.
func (s *Service) IssueToken(ctx context.Context, accountID, userID, name string, ttl time.Duration) (string, Token, error) {
	name = strings.TrimSpace(name)
	if !validTokenName(name) || ttl < 0 {
		return "", Token{}, ErrInvalidTokenName
	}
	raw := make([]byte, tokenLen)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", Token{}, fmt.Errorf("access token: %w", err)
	}
	secret := TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	now := s.clk.Now()
	t := Token{AccountID: accountID, UserID: userID, Name: name, CreatedAt: now}
	if ttl > 0 {
		t.ExpiresAt = now.Add(ttl)
	}
	id, err := s.st.CreateToken(ctx, t, tokenHash(secret))
	if err != nil {
		return "", Token{}, fmt.Errorf("create access token: %w", err)
	}
	t.ID = id
	return secret, t, nil
}

func validTokenName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n > 0 && n <= MaxTokenName && utf8.ValidString(name) && strings.IndexFunc(name, unicode.IsControl) < 0
}

// IsAccessToken reports whether secret has the form of an access token, rather than a
// session token.
func IsAccessToken(secret string) bool { return strings.HasPrefix(secret, TokenPrefix) }

// AuthenticateToken returns the access token of secret, and records its use. It returns
// ErrNoToken if the secret is unknown or its token expired; an expired token stays
// listed, for its user to see why it no longer works.
func (s *Service) AuthenticateToken(ctx context.Context, secret string) (Token, error) {
	rest, ok := strings.CutPrefix(secret, TokenPrefix)
	if !ok || base64.RawURLEncoding.DecodedLen(len(rest)) != tokenLen {
		return Token{}, ErrNoToken
	}
	t, found, err := s.st.TokenByHash(ctx, tokenHash(secret))
	switch {
	case err != nil:
		return Token{}, fmt.Errorf("access token: %w", err)
	case !found:
		return Token{}, ErrNoToken
	}
	now := s.clk.Now()
	if t.Expired(now) {
		return Token{}, ErrNoToken
	}
	if t.LastUsedAt.IsZero() || now.Sub(t.LastUsedAt) >= s.p.TouchEvery {
		if err := s.st.TouchToken(ctx, t.AccountID, t.ID, now); err != nil {
			return Token{}, fmt.Errorf("touch access token: %w", err)
		}
		t.LastUsedAt = now
	}
	return t, nil
}

// Tokens lists the user's access tokens, expired ones included, newest first.
func (s *Service) Tokens(ctx context.Context, accountID, userID string) ([]Token, error) {
	ts, err := s.st.ListTokens(ctx, accountID, userID)
	if err != nil {
		return nil, fmt.Errorf("list access tokens: %w", err)
	}
	return ts, nil
}

// RevokeToken deletes one of the user's access tokens: it no longer authenticates, at
// once. It returns ErrUnknownToken if the user has no such token.
func (s *Service) RevokeToken(ctx context.Context, accountID, userID, tokenID string) error {
	ok, err := s.st.DeleteToken(ctx, accountID, userID, tokenID)
	switch {
	case err != nil:
		return fmt.Errorf("delete access token: %w", err)
	case !ok:
		return ErrUnknownToken
	}
	return nil
}
