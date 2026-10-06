package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAccessTokens(t *testing.T) {
	ctx := context.Background()
	s, st, clk := newService(ctx, t)
	u, _, _ := s.st.UserByName(ctx, "admin")

	secret, tok, err := s.IssueToken(ctx, u.AccountID, u.ID, "  Claude Code  ", 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, TokenPrefix) || !IsAccessToken(secret) || len(secret) != len(TokenPrefix)+43 {
		t.Errorf("secret %q", secret)
	}
	if tok.Name != "Claude Code" || !tok.ExpiresAt.Equal(t0.Add(90*24*time.Hour)) || !tok.LastUsedAt.IsZero() {
		t.Errorf("token %+v", tok)
	}
	for _, stored := range st.tokens {
		if strings.Contains(string(stored.hash), secret) || len(stored.hash) != 32 {
			t.Errorf("stored hash %x", stored.hash)
		}
	}

	// The first use is recorded, the next ones once per TouchEvery.
	got, err := s.AuthenticateToken(ctx, secret)
	if err != nil || got.ID != tok.ID || got.AccountID != "acc" || got.UserID != u.ID || !got.LastUsedAt.Equal(t0) {
		t.Fatalf("authenticate: %+v, %v", got, err)
	}
	clk.Advance(time.Minute)
	if got, _ := s.AuthenticateToken(ctx, secret); !got.LastUsedAt.Equal(t0) {
		t.Errorf("touched again after a minute: %v", got.LastUsedAt)
	}
	clk.Advance(s.p.TouchEvery)
	if got, _ := s.AuthenticateToken(ctx, secret); !got.LastUsedAt.Equal(clk.Now()) {
		t.Errorf("not touched after TouchEvery: %v", got.LastUsedAt)
	}

	// Neither a session token, nor a malformed or unknown secret.
	session, _, _ := s.Open(ctx, u)
	for _, bad := range []string{"", session, TokenPrefix, TokenPrefix + "x", secret[:len(secret)-1] + "A", "Bearer " + secret} {
		if _, err := s.AuthenticateToken(ctx, bad); !errors.Is(err, ErrNoToken) {
			t.Errorf("AuthenticateToken(%q): %v", bad, err)
		}
	}
	if _, err := s.Authenticate(ctx, secret); !errors.Is(err, ErrNoSession) {
		t.Errorf("a token as a session: %v", err)
	}

	// Expired: refused, still listed.
	clk.Advance(90 * 24 * time.Hour)
	if _, err := s.AuthenticateToken(ctx, secret); !errors.Is(err, ErrNoToken) {
		t.Errorf("expired: %v", err)
	}
	forever, _, err := s.IssueToken(ctx, u.AccountID, u.ID, "Home Assistant", 0)
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(10 * 365 * 24 * time.Hour)
	if _, err := s.AuthenticateToken(ctx, forever); err != nil {
		t.Errorf("without expiry: %v", err)
	}
	list, err := s.Tokens(ctx, u.AccountID, u.ID)
	if err != nil || len(list) != 2 || list[0].Name != "Home Assistant" || !list[0].ExpiresAt.IsZero() || !list[1].Expired(clk.Now()) {
		t.Errorf("tokens %+v, %v", list, err)
	}

	// Revoked: at once, and only by its user.
	if err := s.RevokeToken(ctx, "other", u.ID, list[0].ID); !errors.Is(err, ErrUnknownToken) {
		t.Errorf("revoke from another account: %v", err)
	}
	if err := s.RevokeToken(ctx, u.AccountID, u.ID, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateToken(ctx, forever); !errors.Is(err, ErrNoToken) {
		t.Errorf("revoked: %v", err)
	}
	if err := s.RevokeToken(ctx, u.AccountID, u.ID, list[0].ID); !errors.Is(err, ErrUnknownToken) {
		t.Errorf("revoked twice: %v", err)
	}
}

func TestTokenNames(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newService(ctx, t)
	for _, tt := range []struct {
		name string
		ttl  time.Duration
		ok   bool
	}{
		{"a", 0, true},
		{strings.Repeat("é", MaxTokenName), 0, true},
		{strings.Repeat("é", MaxTokenName+1), 0, false},
		{"   ", 0, false},
		{"tab\there", 0, false},
		{"bad \xff", 0, false},
		{"negative", -time.Hour, false},
	} {
		_, _, err := s.IssueToken(ctx, "acc", "1", tt.name, tt.ttl)
		if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidTokenName)) {
			t.Errorf("IssueToken(%q, %v): %v", tt.name, tt.ttl, err)
		}
	}
}

func TestTokenStoreErrors(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newService(ctx, t)
	secret, tok, err := s.IssueToken(ctx, "acc", "1", "script", 0)
	if err != nil {
		t.Fatal(err)
	}
	st.err = errors.New("database down")
	if _, _, err := s.IssueToken(ctx, "acc", "1", "script", 0); !errors.Is(err, st.err) {
		t.Errorf("issue: %v", err)
	}
	if _, err := s.AuthenticateToken(ctx, secret); !errors.Is(err, st.err) || errors.Is(err, ErrNoToken) {
		t.Errorf("authenticate: %v", err)
	}
	if _, err := s.Tokens(ctx, "acc", "1"); !errors.Is(err, st.err) {
		t.Errorf("list: %v", err)
	}
	if err := s.RevokeToken(ctx, "acc", "1", tok.ID); !errors.Is(err, st.err) {
		t.Errorf("revoke: %v", err)
	}
}
