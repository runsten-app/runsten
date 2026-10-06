package collector

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/volvo"
)

// credStore is an in-memory oauth.Store; UpdateCredentials is serialized, like the
// row lock of the real store.
type credStore struct {
	mu    sync.Mutex
	creds map[oauth.ConnectionRef]oauth.Credentials
}

func (s *credStore) Credentials(_ context.Context, accountID, connectionID string) (oauth.Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.creds[oauth.ConnectionRef{AccountID: accountID, ConnectionID: connectionID}]
	if !ok {
		return c, errors.New("no connection")
	}
	return c, nil
}

func (s *credStore) UpdateCredentials(_ context.Context, accountID, connectionID string, fn func(oauth.Credentials) (oauth.Credentials, bool)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := oauth.ConnectionRef{AccountID: accountID, ConnectionID: connectionID}
	if next, changed := fn(s.creds[ref]); changed {
		s.creds[ref] = next
	}
	return nil
}

func (s *credStore) StaleConnections(_ context.Context, before time.Time) ([]oauth.ConnectionRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []oauth.ConnectionRef
	for ref, c := range s.creds {
		if c.ReauthReason == "" && c.RefreshToken != "" && c.RefreshedAt.Before(before) {
			out = append(out, ref)
		}
	}
	return out, nil
}

func (s *credStore) get(ref oauth.ConnectionRef) oauth.Credentials {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds[ref]
}

// authStore is the collector Store of an OAuth connection: the targets carry the
// connection state, as runsten_poll_targets does.
type authStore struct {
	memStore
	creds *credStore
}

func (a *authStore) Targets(ctx context.Context) ([]Target, error) {
	out := make([]Target, len(a.targets))
	for i, t := range a.targets {
		c, err := a.creds.Credentials(ctx, t.AccountID, t.ConnectionID)
		if err != nil {
			return nil, err
		}
		t.ReauthReason = c.ReauthReason
		out[i] = t
	}
	return out, nil
}

// countingRefresher counts the refreshes.
type countingRefresher struct {
	*volvo.AuthClient
	mu sync.Mutex
	n  int
}

func (r *countingRefresher) Refresh(ctx context.Context, rt string) (oauth.Grant, error) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	return r.AuthClient.Refresh(ctx, rt)
}

// rejectionAPI records the calls rejected with a 401.
type rejectionAPI struct {
	API
	clk      clock.Clock
	rejected []time.Time
}

func (r *rejectionAPI) Fetch(ctx context.Context, key, token, vin string, ep volvo.Endpoint) ([]byte, error) {
	raw, err := r.API.Fetch(ctx, key, token, vin, ep)
	var apiErr *volvo.APIError
	if errors.As(err, &apiErr) && apiErr.Kind == volvo.KindUnauthorized {
		r.rejected = append(r.rejected, r.clk.Now())
	}
	return raw, err
}

// connected authorizes the simulated user and returns the stored connection, like
// runsten-api after its callback.
func connected(t *testing.T, e simEnv) (*credStore, oauth.ConnectionRef) {
	t.Helper()
	flows := oauth.NewFlows(e.clk, time.Minute, 1)
	state, challenge, err := flows.Start("a")
	if err != nil {
		t.Fatal(err)
	}
	hc := *e.srv.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, e.auth.AuthorizeURL(state, challenge), http.NoBody)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	verifier, _, err := flows.Finish(loc.Query().Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	at := e.clk.Now()
	g, err := e.auth.Exchange(context.Background(), loc.Query().Get("code"), verifier)
	if err != nil {
		t.Fatal(err)
	}
	ref := oauth.ConnectionRef{AccountID: "a", ConnectionID: "c"}
	return &credStore{creds: map[oauth.ConnectionRef]oauth.Credentials{ref: oauth.NewCredentials(g, at)}}, ref
}

// TestTokenLifecycle plays three days with 299-second access tokens and a 60-hour
// grant. The collector refreshes on its own, never presents an expired token, then
// stops polling once the grant ends and reports it.
func TestTokenLifecycle(t *testing.T) {
	e := startSim(t, "token-lifecycle.yaml", nil)
	creds, ref := connected(t, e)
	refresher := &countingRefresher{AuthClient: e.auth}
	tokens := oauth.NewManager(creds, refresher, e.clk, oauth.DefaultParams(), quiet)
	api := &rejectionAPI{API: &countingAPI{API: e.client, clk: e.clk}, clk: e.clk}
	st := &authStore{memStore: memStore{targets: []Target{{AccountID: "a", VehicleID: "v", VIN: e.sc.VIN, ConnectionID: "c"}}}, creds: creds}
	rec := &recorder{}
	c := New(api, st, tokens, e.clk, DefaultIntervals(), DefaultQuota(), quiet, rec, nil)

	grantEnd := e.sc.Start.Add(e.sc.API.GrantTTL)
	end := e.sc.Start.Truncate(24 * time.Hour).Add(2*24*time.Hour + 21*time.Hour)
	for e.clk.Now().Before(end) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		e.clk.Advance(30 * time.Second)
	}

	counted := api.API.(*countingAPI)
	var afterEnd, total int
	for _, calls := range counted.calls {
		for _, at := range calls {
			total++
			// Once the grant is over, the current access token lives at most 299 s.
			if at.After(grantEnd.Add(e.sc.API.AccessTokenTTL)) {
				afterEnd++
			}
		}
	}
	t.Logf("%d calls, %d refreshes, %d rejected", total, refresher.n, len(api.rejected))
	if refresher.n < 100 {
		t.Errorf("%d refreshes over 60 hours of 299-second tokens", refresher.n)
	}
	for _, at := range api.rejected {
		if at.Before(grantEnd) {
			t.Errorf("token rejected at %v, before the end of the grant", at)
		}
	}
	if afterEnd != 0 {
		t.Errorf("%d calls after the end of the grant", afterEnd)
	}
	if got := creds.get(ref); got.ReauthReason == "" || got.ReauthAt.Before(grantEnd) {
		t.Errorf("connection after the grant: %+v", got)
	}
	if last := rec.states[len(rec.states)-1]; last.ReauthReason == "" {
		t.Errorf("observer not told: %+v", last)
	}
	var trips int
	for _, s := range st.saved {
		if s.Endpoint == volvo.Location {
			trips++
		}
	}
	if trips < 5 {
		t.Errorf("%d locations stored: the trips before the end of the grant were not followed", trips)
	}
}

// TestKeepAliveDuringQuota: every API quota runs out during the day, so no call needs
// a token until midnight. With 4-hour refresh tokens, only the keep-alive preserves
// the grant; without it, the refresh token lapses.
func TestKeepAliveDuringQuota(t *testing.T) {
	for _, tt := range []struct {
		name      string
		keepAlive time.Duration
		lost      bool
	}{
		{"with keep-alive: the grant survives", time.Hour, false},
		{"without keep-alive: the refresh token lapses", 48 * time.Hour, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := startSim(t, "quota-exhausted.yaml", func(sc *scenario.Scenario) {
				quota := 10
				sc.API.DailyQuota, sc.API.AccessTokenTTL, sc.API.RefreshTokenTTL = &quota, 299*time.Second, 4*time.Hour
			})
			creds, ref := connected(t, e)
			p := oauth.DefaultParams()
			p.KeepAlive, p.RefreshTTL = tt.keepAlive, 4*time.Hour
			tokens := oauth.NewManager(creds, e.auth, e.clk, p, quiet)
			api := &countingAPI{API: e.client, clk: e.clk}
			st := &authStore{memStore: memStore{targets: []Target{{AccountID: "a", VehicleID: "v", VIN: e.sc.VIN, ConnectionID: "c"}}}, creds: creds}
			c := New(api, st, tokens, e.clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)

			midnight := e.sc.Start.Truncate(24 * time.Hour).Add(24 * time.Hour)
			for e.clk.Now().Before(midnight.Add(2 * time.Hour)) {
				if err := c.PollOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				e.clk.Advance(time.Minute)
			}

			var lastBefore time.Time
			var after int
			for _, calls := range api.calls {
				for _, at := range calls {
					if at.Before(midnight) && at.After(lastBefore) {
						lastBefore = at
					}
					if !at.Before(midnight) {
						after++
					}
				}
			}
			t.Logf("last call before midnight at %v, %d calls after", lastBefore, after)
			if midnight.Sub(lastBefore) < 6*time.Hour {
				t.Fatalf("quotas exhausted too late (%v): the keep-alive is not exercised", lastBefore)
			}
			got := creds.get(ref)
			if lost := got.ReauthReason != ""; lost != tt.lost {
				t.Errorf("connection: %+v", got)
			}
			if resumed := after > 0; resumed == tt.lost {
				t.Errorf("%d calls after the quota reset", after)
			}
		})
	}
}

// fakeTokens is a scripted TokenSource.
type fakeTokens struct {
	token, renewed           string
	tokenErr, refreshErr     error
	keepAliveErr             error
	tokens, refreshes, kicks int
}

func (f *fakeTokens) Token(context.Context, string, string) (string, error) {
	f.tokens++
	return f.token, f.tokenErr
}

func (f *fakeTokens) Refresh(_ context.Context, _, _, rejected string) (string, error) {
	f.refreshes++
	if rejected != f.token {
		return "", errors.New("unexpected rejected token")
	}
	return f.renewed, f.refreshErr
}

func (f *fakeTokens) KeepAlive(context.Context) error {
	f.kicks++
	return f.keepAliveErr
}

// authAPI rejects some tokens with a 401, and some endpoints with a quota 403.
type authAPI struct {
	rejected map[string]bool
	quota    map[volvo.Endpoint]bool
	calls    map[volvo.Endpoint]int
}

func (a *authAPI) Fetch(_ context.Context, _, token, _ string, ep volvo.Endpoint) ([]byte, error) {
	if a.calls == nil {
		a.calls = map[volvo.Endpoint]int{}
	}
	a.calls[ep]++
	switch {
	case a.rejected[token]:
		return nil, &volvo.APIError{Endpoint: string(ep), Status: 401, Kind: volvo.KindUnauthorized, Message: "expired"}
	case a.quota[ep]:
		return nil, &volvo.APIError{Endpoint: string(ep), Status: 403, Kind: volvo.KindQuota, RetryIn: time.Hour, Message: "Out of call volume quota"}
	}
	return []byte(`{}`), nil
}

func TestTokenHandling(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	lost := &oauth.ReauthError{Reason: "refresh refused: invalid_grant"}
	tests := []struct {
		name      string
		tokens    fakeTokens
		api       authAPI
		target    Target
		refreshes int
		calls     int // endpoints called at least once
		engine    int // engine-status calls
		paused    bool
		reauth    bool
	}{
		{
			name:   "rejected token: renewed once, the pass goes on",
			tokens: fakeTokens{token: "old", renewed: "new"}, api: authAPI{rejected: map[string]bool{"old": true}},
			refreshes: 1, calls: len(endpoints()), engine: 2,
		},
		{
			name:   "rejected again after the refresh: account paused, no loop",
			tokens: fakeTokens{token: "old", renewed: "new"}, api: authAPI{rejected: map[string]bool{"old": true, "new": true}},
			refreshes: 1, calls: 1, engine: 2, paused: true,
		},
		{
			name:   "refresh refused: the connection is no longer polled",
			tokens: fakeTokens{token: "old", refreshErr: lost}, api: authAPI{rejected: map[string]bool{"old": true}},
			refreshes: 1, calls: 1, engine: 1, reauth: true,
		},
		{
			// Only Connected Vehicle is suspended: Energy and Location are still called.
			name:   "quota 403 is not an authentication error",
			tokens: fakeTokens{token: "t"}, api: authAPI{quota: map[volvo.Endpoint]bool{volvo.EngineStatus: true}},
			calls: 3, engine: 1,
		},
		{
			name:   "no token (provider down): nothing called this pass",
			tokens: fakeTokens{tokenErr: errors.New("token endpoint: HTTP 503")},
		},
		{
			name:   "lost grant reported by Token: nothing called",
			tokens: fakeTokens{tokenErr: lost}, reauth: true,
		},
		{
			name:   "connection marked in the store: no token asked",
			tokens: fakeTokens{token: "t"}, target: Target{ReauthReason: "refresh refused"}, reauth: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := target()
			target.ReauthReason = tt.target.ReauthReason
			rec := &recorder{}
			c := New(&tt.api, &memStore{targets: []Target{target}}, &tt.tokens, clock.NewManual(t0), DefaultIntervals(), DefaultQuota(), quiet, rec, nil)
			if err := c.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if tt.tokens.refreshes != tt.refreshes {
				t.Errorf("%d refreshes, want %d", tt.tokens.refreshes, tt.refreshes)
			}
			if len(tt.api.calls) != tt.calls || tt.api.calls[volvo.EngineStatus] != tt.engine {
				t.Errorf("calls %v, want %d endpoints and %d engine-status", tt.api.calls, tt.calls, tt.engine)
			}
			state := rec.states[len(rec.states)-1]
			if paused := !state.PausedUntil.IsZero(); paused != tt.paused {
				t.Errorf("paused until %v", state.PausedUntil)
			}
			if reauth := state.ReauthReason != ""; reauth != tt.reauth {
				t.Errorf("re-authentication reason %q", state.ReauthReason)
			}
			if target.ReauthReason != "" && tt.tokens.tokens != 0 {
				t.Error("token asked for a connection requiring re-authentication")
			}
		})
	}
}

func TestKeepAliveFailureDoesNotStopPolling(t *testing.T) {
	tokens := &fakeTokens{token: "t", keepAliveErr: errors.New("database unavailable")}
	api := &authAPI{}
	c := New(api, &memStore{targets: []Target{target()}}, tokens, clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)),
		DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tokens.kicks != 1 || len(api.calls) != len(endpoints()) {
		t.Errorf("keep-alive %d, calls %v", tokens.kicks, api.calls)
	}
}
