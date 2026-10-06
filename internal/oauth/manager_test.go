package oauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

var (
	t0    = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	quiet = slog.New(slog.NewTextHandler(io.Discard, nil))
	ref   = ConnectionRef{AccountID: "a", ConnectionID: "c"}
)

// memStore is an in-memory Store. UpdateCredentials is serialized, like the row lock.
type memStore struct {
	mu        sync.Mutex
	creds     map[ConnectionRef]Credentials
	commitErr error // UpdateCredentials fails after fn, as a failed commit
	updates   int
}

func newMemStore(c Credentials) *memStore {
	return &memStore{creds: map[ConnectionRef]Credentials{ref: c}}
}

func (m *memStore) get() Credentials {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.creds[ref]
}

func (m *memStore) Credentials(_ context.Context, accountID, connectionID string) (Credentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.creds[ConnectionRef{accountID, connectionID}]
	if !ok {
		return c, errors.New("no connection")
	}
	return c, nil
}

func (m *memStore) UpdateCredentials(_ context.Context, accountID, connectionID string, fn func(Credentials) (Credentials, bool)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := ConnectionRef{accountID, connectionID}
	cur, ok := m.creds[r]
	if !ok {
		return errors.New("no connection")
	}
	next, changed := fn(cur)
	if !changed {
		return nil
	}
	if m.commitErr != nil {
		return m.commitErr
	}
	m.updates++
	m.creds[r] = next
	return nil
}

func (m *memStore) StaleConnections(_ context.Context, before time.Time) ([]ConnectionRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ConnectionRef
	for r, c := range m.creds {
		if c.ReauthReason == "" && c.RefreshToken != "" && c.RefreshedAt.Before(before) {
			out = append(out, r)
		}
	}
	return out, nil
}

// provider is a token endpoint with rotation: a refresh token works once.
type provider struct {
	current string   // the only valid refresh token
	err     error    // returned instead of a grant
	noRT    bool     // does not rotate the refresh token
	calls   []string // refresh tokens presented
	n       int
}

type grantLost struct{}

func (grantLost) Error() string        { return "invalid_grant" }
func (grantLost) ReauthRequired() bool { return true }

func (p *provider) Refresh(_ context.Context, rt string) (Grant, error) {
	p.calls = append(p.calls, rt)
	if p.err != nil {
		return Grant{}, p.err
	}
	if rt != p.current {
		return Grant{}, grantLost{}
	}
	p.n++
	g := Grant{AccessToken: "access-" + strconv.Itoa(p.n), ExpiresIn: 5 * time.Minute}
	if !p.noRT {
		p.current = "refresh-" + strconv.Itoa(p.n)
		g.RefreshToken = p.current
	}
	return g, nil
}

// active returns credentials refreshed at t0, with an access token valid 5 minutes.
func active() Credentials {
	return Credentials{
		AccessToken: "access-0", RefreshToken: "refresh-0",
		ExpiresAt: t0.Add(5 * time.Minute), RefreshedAt: t0, AuthorizedAt: t0.Add(-24 * time.Hour),
	}
}

func isReauth(err error) bool { return reauthRequired(err) }

// TestTokenWithoutRefreshTTL: with a development clock, the lifetime of the refresh
// token is left to the provider.
func TestTokenWithoutRefreshTTL(t *testing.T) {
	p := DefaultParams()
	p.RefreshTTL = 0
	clk := clock.NewManual(t0)
	pr := provider{current: "refresh-0"}
	m := NewManager(newMemStore(active()), &pr, clk, p, quiet)
	clk.Advance(30 * 24 * time.Hour)
	if got, err := m.Token(context.Background(), "a", "c"); err != nil || got != "access-1" {
		t.Errorf("token %q, %v: want a refresh asked to the provider", got, err)
	}
}

func TestToken(t *testing.T) {
	p := DefaultParams()
	tests := []struct {
		name      string
		creds     func() Credentials
		advance   time.Duration
		provider  provider
		want      string
		reauth    bool   // error means the grant is lost
		failure   bool   // error, grant kept
		calls     int    // refreshes attempted
		storedRT  string // refresh token stored afterwards
		lostSaved bool   // re-authentication persisted
	}{
		{name: "fresh token: no refresh", creds: active, advance: time.Minute, want: "access-0", storedRT: "refresh-0"},
		{
			name: "within the margin: refreshed, with rotation", creds: active, advance: 4 * time.Minute,
			provider: provider{current: "refresh-0"}, want: "access-1", calls: 1, storedRT: "refresh-1",
		},
		{
			name: "expired: refreshed", creds: active, advance: time.Hour,
			provider: provider{current: "refresh-0"}, want: "access-1", calls: 1, storedRT: "refresh-1",
		},
		{
			name: "no rotation: the refresh token is kept", creds: active, advance: time.Hour,
			provider: provider{current: "refresh-0", noRT: true}, want: "access-1", calls: 1, storedRT: "refresh-0",
		},
		{
			name: "unknown expiry: used as is", advance: 30 * 24 * time.Hour,
			creds: func() Credentials { return Credentials{AccessToken: "pasted"} }, want: "pasted",
		},
		{
			name: "expired without refresh token: lost", advance: time.Hour, reauth: true, lostSaved: true,
			creds: func() Credentials { return Credentials{AccessToken: "pasted", ExpiresAt: t0} },
		},
		{
			name: "margin without refresh token: still used", advance: 4 * time.Minute, want: "pasted",
			creds: func() Credentials { return Credentials{AccessToken: "pasted", ExpiresAt: t0.Add(5 * time.Minute)} },
		},
		{
			name: "refresh token unused for 7 days: lost without calling", creds: active, advance: p.RefreshTTL,
			provider: provider{current: "refresh-0"}, reauth: true, lostSaved: true, storedRT: "refresh-0",
		},
		{
			name: "refresh refused: lost", creds: active, advance: time.Hour,
			provider: provider{current: "other"}, reauth: true, calls: 1, lostSaved: true, storedRT: "refresh-0",
		},
		{
			name: "already lost: no call", advance: time.Hour, reauth: true, storedRT: "refresh-0", lostSaved: true,
			creds: func() Credentials {
				c := active()
				c.ReauthReason, c.ReauthAt = "refresh refused", t0
				return c
			},
		},
		{
			name: "provider down, token still valid: current token", creds: active, advance: 4 * time.Minute,
			provider: provider{err: errors.New("503")}, want: "access-0", calls: 1, storedRT: "refresh-0",
		},
		{
			name: "provider down, token expired: failure, grant kept", creds: active, advance: time.Hour,
			provider: provider{err: errors.New("503")}, failure: true, calls: 1, storedRT: "refresh-0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := clock.NewManual(t0)
			st := newMemStore(tt.creds())
			pr := tt.provider
			m := NewManager(st, &pr, clk, p, quiet)
			clk.Advance(tt.advance)

			got, err := m.Token(context.Background(), "a", "c")
			switch {
			case tt.reauth:
				if !isReauth(err) {
					t.Fatalf("err = %v, want re-authentication required", err)
				}
			case tt.failure:
				if err == nil || isReauth(err) {
					t.Fatalf("err = %v, want a failure keeping the grant", err)
				}
			case err != nil || got != tt.want:
				t.Fatalf("Token = %q, %v; want %q", got, err, tt.want)
			}
			if len(pr.calls) != tt.calls {
				t.Errorf("%d refreshes, want %d", len(pr.calls), tt.calls)
			}
			c := st.get()
			if c.RefreshToken != tt.storedRT {
				t.Errorf("stored refresh token %q, want %q", c.RefreshToken, tt.storedRT)
			}
			if (c.ReauthReason != "") != tt.lostSaved {
				t.Errorf("stored re-authentication reason %q", c.ReauthReason)
			}
			if tt.calls == 1 && err == nil && got != "access-0" {
				now := clk.Now()
				if !c.RefreshedAt.Equal(now) || !c.ExpiresAt.Equal(now.Add(5*time.Minute)) ||
					!c.AuthorizedAt.Equal(t0.Add(-24*time.Hour)) || c.AccessToken != got {
					t.Errorf("stored after refresh: %+v", c)
				}
			}
		})
	}
}

// TestPersistBeforeUse: when the rotated tokens cannot be stored, the new access token
// is not returned: a token in use must always be recoverable from the store.
func TestPersistBeforeUse(t *testing.T) {
	clk := clock.NewManual(t0)
	st := newMemStore(active())
	st.commitErr = errors.New("database unavailable")
	pr := &provider{current: "refresh-0"}
	m := NewManager(st, pr, clk, DefaultParams(), quiet)
	clk.Advance(time.Hour)

	got, err := m.Token(context.Background(), "a", "c")
	if err == nil || got != "" {
		t.Fatalf("Token = %q, %v; want an error and no token", got, err)
	}
	if len(pr.calls) != 1 || st.get().AccessToken != "access-0" {
		t.Errorf("calls %v, stored %+v", pr.calls, st.get())
	}
}

func TestBackoffAfterFailure(t *testing.T) {
	clk := clock.NewManual(t0)
	st := newMemStore(active())
	pr := &provider{err: errors.New("503")}
	p := DefaultParams()
	m := NewManager(st, pr, clk, p, quiet)
	clk.Advance(4 * time.Minute) // within the margin

	for range 3 {
		if got, err := m.Token(context.Background(), "a", "c"); err != nil || got != "access-0" {
			t.Fatalf("Token = %q, %v", got, err)
		}
	}
	if len(pr.calls) != 1 {
		t.Fatalf("%d refreshes during the retry delay, want 1", len(pr.calls))
	}
	clk.Advance(p.RetryDelay)
	pr.err, pr.current = nil, "refresh-0"
	if got, err := m.Token(context.Background(), "a", "c"); err != nil || got != "access-1" {
		t.Fatalf("after the delay: %q, %v", got, err)
	}
	if len(pr.calls) != 2 {
		t.Errorf("%d refreshes, want 2", len(pr.calls))
	}
}

func TestRefreshAfterRejection(t *testing.T) {
	tests := []struct {
		name     string
		rejected string
		want     string
		calls    int
	}{
		{"the current token was rejected: refreshed", "access-0", "access-1", 1},
		{"another refresher already replaced it: no call", "access-old", "access-0", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newMemStore(active())
			pr := &provider{current: "refresh-0"}
			m := NewManager(st, pr, clock.NewManual(t0), DefaultParams(), quiet)
			got, err := m.Refresh(context.Background(), "a", "c", tt.rejected)
			if err != nil || got != tt.want || len(pr.calls) != tt.calls {
				t.Errorf("Refresh = %q, %v, %d calls; want %q, %d", got, err, len(pr.calls), tt.want, tt.calls)
			}
		})
	}

	t.Run("rejected, provider down: failure even if not expired", func(t *testing.T) {
		st := newMemStore(active())
		m := NewManager(st, &provider{err: errors.New("503")}, clock.NewManual(t0), DefaultParams(), quiet)
		if got, err := m.Refresh(context.Background(), "a", "c", "access-0"); err == nil || isReauth(err) {
			t.Errorf("Refresh = %q, %v", got, err)
		}
	})
	t.Run("rejected without refresh token: lost", func(t *testing.T) {
		st := newMemStore(Credentials{AccessToken: "pasted"})
		m := NewManager(st, &provider{}, clock.NewManual(t0), DefaultParams(), quiet)
		if _, err := m.Refresh(context.Background(), "a", "c", "pasted"); !isReauth(err) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestKeepAlive(t *testing.T) {
	p := DefaultParams()
	idle := ConnectionRef{AccountID: "a", ConnectionID: "idle"}
	lost := ConnectionRef{AccountID: "b", ConnectionID: "lost"}
	recent := ConnectionRef{AccountID: "c", ConnectionID: "recent"}
	old := active()
	old.RefreshedAt = t0.Add(-p.KeepAlive - time.Minute)
	st := &memStore{creds: map[ConnectionRef]Credentials{idle: old, recent: active()}}
	withOther := old
	withOther.RefreshToken = "refresh-lost"
	st.creds[lost] = withOther

	pr := &provider{current: "refresh-0"}
	m := NewManager(st, pr, clock.NewManual(t0), p, quiet)
	if err := m.KeepAlive(context.Background()); err != nil {
		t.Fatalf("a lost grant is not a keep-alive error: %v", err)
	}
	if len(pr.calls) != 2 {
		t.Errorf("refreshes: %v, want the two idle connections only", pr.calls)
	}
	if c := st.creds[idle]; c.RefreshToken != "refresh-1" || !c.RefreshedAt.Equal(t0) {
		t.Errorf("idle connection: %+v", c)
	}
	if c := st.creds[lost]; c.ReauthReason == "" {
		t.Errorf("refused connection not marked: %+v", c)
	}
	if c := st.creds[recent]; c.RefreshToken != "refresh-0" {
		t.Errorf("recent connection refreshed: %+v", c)
	}

	// A provider failure is reported once the access token is expired too (before, the
	// manager only logs it: the token is still usable).
	old.ExpiresAt = t0
	st.creds[idle] = old
	m = NewManager(st, &provider{err: errors.New("503")}, clock.NewManual(t0), p, quiet)
	if err := m.KeepAlive(context.Background()); err == nil {
		t.Error("provider failure not reported")
	}
}

func TestLogs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	p := DefaultParams()
	c := active()
	c.AuthorizedAt = t0.Add(-p.GrantTTL + p.GrantWarning/2) // grant ends in a week
	st := newMemStore(c)
	pr := &provider{current: "refresh-0"}
	clk := clock.NewManual(t0)
	m := NewManager(st, pr, clk, p, log)
	for range 3 {
		clk.Advance(time.Hour)
		if _, err := m.Token(context.Background(), "a", "c"); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(buf.String(), "grant ends soon"); n != 1 {
		t.Errorf("grant end announced %d times, want 1:\n%s", n, buf.String())
	}

	pr.current = "revoked"
	for range 3 {
		clk.Advance(time.Hour)
		_, _ = m.Token(context.Background(), "a", "c")
	}
	if n := strings.Count(buf.String(), "re-authentication required"); n != 1 {
		t.Errorf("loss logged %d times, want 1:\n%s", n, buf.String())
	}
	for _, secret := range []string{"access-", "refresh-"} {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("token in the logs:\n%s", buf.String())
		}
	}
}

func TestStoreErrors(t *testing.T) {
	m := NewManager(&memStore{creds: map[ConnectionRef]Credentials{}}, &provider{}, clock.NewManual(t0), DefaultParams(), quiet)
	if _, err := m.Token(context.Background(), "a", "c"); err == nil {
		t.Error("unknown connection: error expected")
	}
	if _, err := m.Refresh(context.Background(), "a", "c", "x"); err == nil {
		t.Error("unknown connection: error expected")
	}
	if err := NewManager(&failingStale{}, &provider{}, clock.NewManual(t0), DefaultParams(), quiet).KeepAlive(context.Background()); err == nil {
		t.Error("store failure not reported")
	}
}

type failingStale struct{ memStore }

func (*failingStale) StaleConnections(context.Context, time.Time) ([]ConnectionRef, error) {
	return nil, errors.New("database unavailable")
}

func TestNewCredentials(t *testing.T) {
	c := NewCredentials(Grant{AccessToken: "a", RefreshToken: "r", ExpiresIn: time.Minute}, t0)
	want := Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: t0.Add(time.Minute), RefreshedAt: t0, AuthorizedAt: t0}
	if c != want {
		t.Errorf("credentials = %+v", c)
	}
	if c := NewCredentials(Grant{AccessToken: "a"}, t0); !c.ExpiresAt.IsZero() || !c.RefreshedAt.IsZero() {
		t.Errorf("without expiry nor refresh token: %+v", c)
	}
}
