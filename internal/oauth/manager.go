package oauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"runsten/internal/platform/clock"
)

// Manager provides valid access tokens, refreshing them when needed. It is safe for
// concurrent use, and several managers (several processes) may share a store: the
// store lock makes each refresh exclusive, and a manager re-reads the credentials
// under that lock before refreshing.
type Manager struct {
	st  Store
	ref Refresher
	clk clock.Clock
	p   Params
	log *slog.Logger

	mu      sync.Mutex
	retryAt map[ConnectionRef]time.Time // after a failed refresh
	lost    map[ConnectionRef]bool      // loss of the grant logged
	warned  map[ConnectionRef]bool      // end of the grant announced
}

// NewManager creates a token manager.
func NewManager(st Store, ref Refresher, clk clock.Clock, p Params, log *slog.Logger) *Manager {
	return &Manager{
		st: st, ref: ref, clk: clk, p: p, log: log,
		retryAt: map[ConnectionRef]time.Time{}, lost: map[ConnectionRef]bool{}, warned: map[ConnectionRef]bool{},
	}
}

// Token returns an access token for the connection, refreshed first if it expires
// within the margin. An error implementing ReauthRequired() means that the user must
// authorize again.
func (m *Manager) Token(ctx context.Context, accountID, connectionID string) (string, error) {
	c, err := m.st.Credentials(ctx, accountID, connectionID)
	if err != nil {
		return "", fmt.Errorf("credentials: %w", err)
	}
	if c.ReauthReason != "" {
		return "", &ReauthError{Reason: c.ReauthReason}
	}
	if !m.expiring(c, m.clk.Now()) {
		return c.AccessToken, nil
	}
	return m.refresh(ctx, ConnectionRef{accountID, connectionID}, false, func(c Credentials) bool {
		return m.expiring(c, m.clk.Now())
	})
}

// Refresh is called after the provider rejected the access token rejected: it
// refreshes the connection once, unless another refresher already replaced that
// token, and returns the new access token.
func (m *Manager) Refresh(ctx context.Context, accountID, connectionID, rejected string) (string, error) {
	return m.refresh(ctx, ConnectionRef{accountID, connectionID}, true, func(c Credentials) bool {
		return c.AccessToken == rejected
	})
}

// KeepAlive refreshes the connections whose refresh token is older than
// Params.KeepAlive, so that it does not lapse while nothing calls the API. A
// connection that loses its grant is not an error here: it is marked and logged.
func (m *Manager) KeepAlive(ctx context.Context) error {
	before := m.clk.Now().Add(-m.p.KeepAlive)
	refs, err := m.st.StaleConnections(ctx, before)
	if err != nil {
		return fmt.Errorf("stale connections: %w", err)
	}
	var errs []error
	for _, ref := range refs {
		_, err := m.refresh(ctx, ref, false, func(c Credentials) bool {
			return !c.RefreshedAt.IsZero() && c.RefreshedAt.Before(before)
		})
		if err != nil && !reauthRequired(err) {
			errs = append(errs, fmt.Errorf("keep-alive of connection %s: %w", ref.ConnectionID, err))
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) expiring(c Credentials, now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt.Add(-m.p.Margin))
}

func expired(c Credentials, now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)
}

// refresh refreshes the connection under the store lock if due, re-evaluated on the
// locked credentials, still says so. forced means that the current access token was
// rejected: it cannot be returned as a fallback.
func (m *Manager) refresh(ctx context.Context, ref ConnectionRef, forced bool, due func(Credentials) bool) (string, error) {
	var (
		token     string
		result    error
		refreshed bool
		stored    Credentials
	)
	// fallback returns the current access token if it can still be used.
	fallback := func(c Credentials, now time.Time, err error) (Credentials, bool) {
		if !forced && !expired(c, now) {
			token = c.AccessToken
		} else {
			result = err
		}
		return c, false
	}
	lose := func(c Credentials, now time.Time, reason string) (Credentials, bool) {
		c.ReauthReason, c.ReauthAt = reason, now
		result = &ReauthError{Reason: reason}
		return c, true
	}

	err := m.st.UpdateCredentials(ctx, ref.AccountID, ref.ConnectionID, func(c Credentials) (Credentials, bool) {
		now := m.clk.Now()
		switch {
		case c.ReauthReason != "":
			result = &ReauthError{Reason: c.ReauthReason}
			return c, false
		case !due(c): // another refresher got there first
			token = c.AccessToken
			return c, false
		case c.RefreshToken == "":
			if !forced && !expired(c, now) {
				token = c.AccessToken
				return c, false
			}
			return lose(c, now, "access token rejected or expired, and no refresh token")
		case m.p.RefreshTTL > 0 && !c.RefreshedAt.IsZero() && !now.Before(c.RefreshedAt.Add(m.p.RefreshTTL)):
			// Documented lifetime: no need to ask the provider.
			return lose(c, now, "refresh token unused for "+m.p.RefreshTTL.String())
		}
		if at := m.nextAttempt(ref); now.Before(at) {
			return fallback(c, now, fmt.Errorf("token refresh failed recently, next attempt at %s", at.Format(time.RFC3339)))
		}

		g, err := m.ref.Refresh(ctx, c.RefreshToken)
		if err != nil {
			if reauthRequired(err) {
				return lose(c, now, "refresh refused: "+err.Error())
			}
			m.setNextAttempt(ref, now.Add(m.p.RetryDelay))
			m.log.Warn("token refresh failed", "account", ref.AccountID, "connection", ref.ConnectionID,
				"retry_in", m.p.RetryDelay.String(), "err", err)
			return fallback(c, now, fmt.Errorf("refresh: %w", err))
		}
		next := c
		next.AccessToken = g.AccessToken
		// Assumption: without a new refresh token (no rotation), the current one stays
		// valid and its 7-day period restarts from this use.
		if g.RefreshToken != "" {
			next.RefreshToken = g.RefreshToken
		}
		next.RefreshedAt = now
		next.ExpiresAt = time.Time{}
		if g.ExpiresIn > 0 {
			next.ExpiresAt = now.Add(g.ExpiresIn) // counted from the request: conservative
		}
		token, refreshed, stored = next.AccessToken, true, next
		return next, true
	})
	if err != nil {
		if refreshed {
			// The provider has rotated the refresh token but the new one is not stored:
			// it must not be used, and the connection will likely need re-authentication.
			m.log.Error("refreshed tokens could not be stored", "account", ref.AccountID,
				"connection", ref.ConnectionID, "err", err)
		}
		return "", fmt.Errorf("storing credentials: %w", err)
	}
	var reauth *ReauthError
	if errors.As(result, &reauth) && reauth.Reason != "" {
		m.logReauth(ref, reauth.Reason)
	}
	if result != nil {
		return "", result
	}
	if refreshed {
		m.setNextAttempt(ref, time.Time{})
		m.log.Debug("token refreshed", "account", ref.AccountID, "connection", ref.ConnectionID,
			"expires_at", stored.ExpiresAt)
		m.warnGrantEnd(ref, stored)
	}
	return token, nil
}

func (m *Manager) nextAttempt(ref ConnectionRef) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.retryAt[ref]
}

func (m *Manager) setNextAttempt(ref ConnectionRef, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if at.IsZero() {
		delete(m.retryAt, ref)
	} else {
		m.retryAt[ref] = at
	}
}

// logReauth logs the loss of a grant once per connection and process: the collector
// then skips the connection instead of retrying.
func (m *Manager) logReauth(ref ConnectionRef, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lost[ref] {
		return
	}
	m.lost[ref] = true
	m.log.Error("re-authentication required", "account", ref.AccountID, "connection", ref.ConnectionID, "reason", reason)
}

// warnGrantEnd announces, once per connection and process, that the grant ends soon.
func (m *Manager) warnGrantEnd(ref ConnectionRef, c Credentials) {
	if c.AuthorizedAt.IsZero() || m.p.GrantTTL <= 0 {
		return
	}
	end := c.AuthorizedAt.Add(m.p.GrantTTL)
	if m.clk.Now().Before(end.Add(-m.p.GrantWarning)) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.warned[ref] {
		return
	}
	m.warned[ref] = true
	m.log.Warn("grant ends soon: authorize again before it expires", "account", ref.AccountID,
		"connection", ref.ConnectionID, "grant_ends_at", end)
}

func reauthRequired(err error) bool {
	var r interface{ ReauthRequired() bool }
	return errors.As(err, &r) && r.ReauthRequired()
}
