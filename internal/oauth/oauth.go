// Package oauth manages the OAuth connections to a vehicle provider: the pending
// authorization flows (state, PKCE), the enrollment of a new grant, and the lifecycle
// of its tokens (refresh before expiry, rotation, re-authentication).
//
// It knows neither HTTP, the database nor a vendor: it declares the interfaces it
// needs. The Volvo client implements Refresher, the store implements Store.
package oauth

import (
	"context"
	"time"
)

// Credentials are the stored tokens of a connection and what is known about them.
// Zero times are unknown: a token pasted by hand has no refresh token, no expiry and
// no grant start.
type Credentials struct {
	AccessToken  string
	RefreshToken string    // empty: the connection cannot be refreshed
	ExpiresAt    time.Time // access token expiry
	RefreshedAt  time.Time // issue time of RefreshToken
	AuthorizedAt time.Time // start of the grant (user consent)
	// ReauthReason is set once the grant is lost: the user must authorize again. The
	// connection is then no longer used.
	ReauthReason string
	ReauthAt     time.Time
}

// Grant is a token endpoint response.
type Grant struct {
	AccessToken  string
	RefreshToken string        // empty if the provider did not rotate it
	ExpiresIn    time.Duration // zero: not announced
}

// NewCredentials returns the credentials of a grant obtained at at, from an
// authorization code: the grant starts at at.
func NewCredentials(g Grant, at time.Time) Credentials {
	c := Credentials{AccessToken: g.AccessToken, RefreshToken: g.RefreshToken, AuthorizedAt: at}
	if g.RefreshToken != "" {
		c.RefreshedAt = at
	}
	if g.ExpiresIn > 0 {
		c.ExpiresAt = at.Add(g.ExpiresIn)
	}
	return c
}

// ConnectionRef identifies a connection.
type ConnectionRef struct {
	AccountID    string
	ConnectionID string
}

// Store keeps the credentials. Implementations encrypt the tokens at rest.
type Store interface {
	// Credentials returns the connection's credentials.
	Credentials(ctx context.Context, accountID, connectionID string) (Credentials, error)
	// UpdateCredentials locks the connection, passes its current credentials to fn and,
	// if fn reports a change, stores the credentials it returns, in the same
	// transaction. The lock is held while fn runs, so that a connection has a single
	// refresher at a time: with rotation, two concurrent refreshes would invalidate
	// each other. It returns an error if the change could not be committed.
	UpdateCredentials(ctx context.Context, accountID, connectionID string, fn func(Credentials) (Credentials, bool)) error
	// StaleConnections lists the refreshable connections, of all accounts, whose refresh
	// token was issued before before and that do not require re-authentication.
	StaleConnections(ctx context.Context, before time.Time) ([]ConnectionRef, error)
}

// Refresher exchanges a refresh token for a new grant. An error that means the grant
// is gone (expired, revoked, already used) implements ReauthRequired() bool.
type Refresher interface {
	Refresh(ctx context.Context, refreshToken string) (Grant, error)
}

// Params tunes the token lifecycle. The provider lifetimes come from the Volvo
// documentation; the rest are Runsten choices.
type Params struct {
	// Margin: the access token is refreshed this long before it expires, so that it
	// does not expire during a polling pass.
	Margin time.Duration
	// KeepAlive: a connection whose refresh token is older is refreshed even if no call
	// needed a token (account paused, quota exhausted). Well below RefreshTTL, so that
	// the collector may be down for a few days without losing the grant.
	KeepAlive time.Duration
	// RefreshTTL: a refresh token must be used within this delay (7 days, documented).
	// An older one is considered lost without calling the provider. Zero: the provider
	// alone decides (a development clock, not the time scale of the stored refreshes).
	RefreshTTL time.Duration
	// GrantTTL: maximum duration of a grant, after which the user must authorize again
	// (6 months, documented). Assumption: counted from the authorization, and 6 months
	// taken as 180 days. It only drives the warning below: the end of the grant itself
	// is detected by the provider refusing the refresh.
	GrantTTL time.Duration
	// GrantWarning: the logs warn this long before the estimated end of the grant.
	GrantWarning time.Duration
	// RetryDelay: after a refresh failure that does not lose the grant (network,
	// provider error), the provider is not called again before this delay.
	RetryDelay time.Duration
}

// DefaultParams returns the default lifecycle parameters.
func DefaultParams() Params {
	const day = 24 * time.Hour
	return Params{
		Margin:       2 * time.Minute,
		KeepAlive:    day,
		RefreshTTL:   7 * day,
		GrantTTL:     180 * day,
		GrantWarning: 14 * day,
		RetryDelay:   time.Minute,
	}
}

// ReauthError means that the connection lost its grant: only a new authorization by
// the user can restore it.
type ReauthError struct {
	Reason string
}

func (e *ReauthError) Error() string { return "re-authentication required: " + e.Reason }

// ReauthRequired is always true: consumers test for this method rather than the type.
func (*ReauthError) ReauthRequired() bool { return true }
