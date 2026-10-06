package oauth

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"runsten/internal/platform/clock"
)

// Flow errors.
var (
	ErrUnknownState = errors.New("unknown, expired or already used state")
	ErrTooManyFlows = errors.New("too many pending authorizations")
)

// Flows keeps the pending authorization flows in memory: each state maps to its PKCE
// verifier and to its owner, the account that started it. A state is single use and expires after a delay; the number of pending
// flows is capped, so that repeated starts cannot exhaust memory. A restart forgets
// the pending flows: the user starts again.
type Flows struct {
	clk clock.Clock
	ttl time.Duration
	max int

	mu      sync.Mutex
	pending map[string]pendingFlow
}

type pendingFlow struct {
	verifier string
	owner    string
	expires  time.Time
}

// NewFlows creates a flow store whose states expire after ttl, with at most max
// pending flows.
func NewFlows(clk clock.Clock, ttl time.Duration, maxPending int) *Flows {
	return &Flows{clk: clk, ttl: ttl, max: maxPending, pending: map[string]pendingFlow{}}
}

// Start creates a flow for owner and returns its state (128 random bits) and PKCE
// challenge (S256, RFC 7636).
func (f *Flows) Start(owner string) (state, challenge string, err error) {
	now := f.clk.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	for s, p := range f.pending {
		if !now.Before(p.expires) {
			delete(f.pending, s)
		}
	}
	if len(f.pending) >= f.max {
		return "", "", ErrTooManyFlows
	}
	state, verifier := rand.Text(), oauth2.GenerateVerifier()
	f.pending[state] = pendingFlow{verifier: verifier, owner: owner, expires: now.Add(f.ttl)}
	return state, oauth2.S256ChallengeFromVerifier(verifier), nil
}

// Finish consumes the flow of state and returns its PKCE verifier and owner. The state
// can only be used once, even if what follows fails.
func (f *Flows) Finish(state string) (verifier, owner string, err error) {
	now := f.clk.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pending[state]
	delete(f.pending, state)
	if !ok || !now.Before(p.expires) {
		return "", "", ErrUnknownState
	}
	return p.verifier, p.owner, nil
}

// SameState compares two states in constant time.
func SameState(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
