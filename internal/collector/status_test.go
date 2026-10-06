package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/volvo"
)

// TestStatusWrites checks when the status is written: on the first pass, on a change,
// and statusEvery after the last write while nothing changes.
func TestStatusWrites(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	iv := DefaultIntervals()
	clk := clock.NewManual(t0)
	st := &memStore{targets: []Target{target()}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, iv, DefaultQuota(), quiet, nil, nil)
	pass := func(after time.Duration) {
		t.Helper()
		clk.Advance(after)
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	pass(0)
	if len(st.statuses) != 1 {
		t.Fatalf("first pass: %d statuses", len(st.statuses))
	}
	first := st.statuses[0]
	if first.AccountID != "a" || first.VehicleID != "v" || !first.PassedAt.Equal(t0) || first.Mode != "parked" ||
		!first.ReadAt.Equal(t0) || !first.NextAt.Equal(t0.Add(iv.Parked)) || !first.PausedUntil.IsZero() ||
		len(first.Quota) != 0 || first.Failure != (Failure{}) {
		t.Errorf("first status: %+v", first)
	}

	pass(10 * time.Second)
	pass(30 * time.Second)
	if len(st.statuses) != 1 {
		t.Errorf("unchanged, within statusEvery: %d statuses", len(st.statuses))
	}
	pass(20 * time.Second) // statusEvery after the first write
	if len(st.statuses) != 2 || !st.statuses[1].PassedAt.Equal(t0.Add(statusEvery)) {
		t.Errorf("unchanged, after statusEvery: %+v", st.statuses)
	}
	pass(iv.Parked - statusEvery) // engine and energy are due: read again
	if len(st.statuses) != 3 || !st.statuses[2].ReadAt.Equal(t0.Add(iv.Parked)) {
		t.Errorf("after a reading: %+v", st.statuses)
	}

	t.Run("a failed write is tried again at the next pass", func(t *testing.T) {
		st.statusErr = errors.New("database unavailable")
		pass(iv.Parked)
		st.statusErr = nil
		n := len(st.statuses)
		pass(10 * time.Second)
		if len(st.statuses) != n+1 {
			t.Errorf("%d statuses, want %d", len(st.statuses), n+1)
		}
	})
}

// TestStatus checks what the status tells of the failures, the blocks and the pauses.
func TestStatus(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	iv := DefaultIntervals()
	lost := &oauth.ReauthError{Reason: "refresh refused: invalid_grant"}
	tests := []struct {
		name   string
		errs   map[volvo.Endpoint]error
		tokens TokenSource
		target Target
		want   func(t *testing.T, s Status)
	}{
		{
			name: "quota exhausted: the API is blocked, the next call is another's",
			errs: map[volvo.Endpoint]error{
				volvo.EnergyState: &volvo.APIError{Status: 403, Kind: volvo.KindQuota, RetryIn: 2 * time.Hour},
			},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if len(s.Quota) != 1 || !s.Quota[volvo.APIEnergy].Equal(t0.Add(2*time.Hour)) {
					t.Errorf("quota: %+v", s.Quota)
				}
				if s.Failure != (Failure{At: t0, Endpoint: volvo.EnergyState, Status: 403, Kind: FailQuota}) {
					t.Errorf("failure: %+v", s.Failure)
				}
				if !s.NextAt.Equal(t0.Add(iv.Parked)) || !s.ReadAt.Equal(t0) {
					t.Errorf("next %s, read %s", s.NextAt, s.ReadAt)
				}
			},
		},
		{
			name: "rate limited: the account is paused, nothing is due before",
			errs: map[volvo.Endpoint]error{
				volvo.EngineStatus: &volvo.APIError{Status: 429, Kind: volvo.KindRateLimited, RetryIn: 30 * time.Minute},
			},
			want: func(t *testing.T, s Status) {
				t.Helper()
				until := t0.Add(30 * time.Minute)
				if !s.PausedUntil.Equal(until) || !s.NextAt.Equal(until) || !s.ReadAt.IsZero() {
					t.Errorf("paused %s, next %s, read %s", s.PausedUntil, s.NextAt, s.ReadAt)
				}
				if s.Failure.Kind != FailRateLimited || s.Failure.Status != 429 {
					t.Errorf("failure: %+v", s.Failure)
				}
			},
		},
		{
			name: "no response",
			errs: map[volvo.Endpoint]error{volvo.Doors: errors.New("dial tcp: connection refused")},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if s.Failure != (Failure{At: t0, Endpoint: volvo.Doors, Kind: FailUnavailable}) {
					t.Errorf("failure: %+v", s.Failure)
				}
			},
		},
		{
			name: "a server error",
			errs: map[volvo.Endpoint]error{volvo.Doors: &volvo.APIError{Status: 503}},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if s.Failure.Kind != FailUnavailable || s.Failure.Status != 503 {
					t.Errorf("failure: %+v", s.Failure)
				}
			},
		},
		{
			name: "a vehicle not found",
			errs: map[volvo.Endpoint]error{volvo.Doors: &volvo.APIError{Status: 404, Kind: volvo.KindNotFound}},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if s.Failure.Kind != FailNotFound {
					t.Errorf("failure: %+v", s.Failure)
				}
			},
		},
		{
			name: "another client error",
			errs: map[volvo.Endpoint]error{volvo.Doors: &volvo.APIError{Status: 400}},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if s.Failure.Kind != FailOther || s.Failure.Status != 400 {
					t.Errorf("failure: %+v", s.Failure)
				}
			},
		},
		{
			name:   "a token rejected after a refresh",
			errs:   map[volvo.Endpoint]error{volvo.EngineStatus: &volvo.APIError{Status: 401, Kind: volvo.KindUnauthorized}},
			tokens: &fakeTokens{token: "t", renewed: "t2"},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if s.Failure.Kind != FailRejected || !s.PausedUntil.Equal(t0.Add(iv.Parked)) {
					t.Errorf("failure %+v, paused %s", s.Failure, s.PausedUntil)
				}
			},
		},
		{
			name:   "no access token, the grant held",
			tokens: &fakeTokens{tokenErr: errors.New("volvo id unreachable")},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if s.Failure != (Failure{At: t0, Kind: FailToken}) || !s.NextAt.Equal(t0) {
					t.Errorf("failure %+v, next %s", s.Failure, s.NextAt)
				}
			},
		},
		{
			name:   "the grant lost at the token: nothing is due",
			tokens: &fakeTokens{tokenErr: lost},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if !s.NextAt.IsZero() || s.Failure != (Failure{}) {
					t.Errorf("next %s, failure %+v", s.NextAt, s.Failure)
				}
			},
		},
		{
			name:   "the grant already lost: nothing is due",
			target: Target{AccountID: "a", VehicleID: "v", VIN: simVIN, ConnectionID: "c", ReauthReason: "lost"},
			want: func(t *testing.T, s Status) {
				t.Helper()
				if !s.NextAt.IsZero() || !s.ReadAt.IsZero() || s.Mode != "parked" {
					t.Errorf("%+v", s)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, tg := tt.tokens, tt.target
			if tokens == nil {
				tokens = fixedToken{}
			}
			if tg == (Target{}) {
				tg = target()
			}
			st := &memStore{targets: []Target{tg}}
			c := New(&scriptedAPI{errs: tt.errs}, st, tokens, clock.NewManual(t0), iv, DefaultQuota(), quiet, nil, nil)
			if err := c.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(st.statuses) != 1 {
				t.Fatalf("%d statuses", len(st.statuses))
			}
			tt.want(t, st.statuses[0])
		})
	}

	t.Run("a block that ended is left out", func(t *testing.T) {
		clk := clock.NewManual(t0)
		api := &scriptedAPI{errs: map[volvo.Endpoint]error{
			volvo.Location: &volvo.APIError{Status: 403, Kind: volvo.KindQuota, RetryIn: time.Minute},
		}}
		st := &memStore{targets: []Target{target()}}
		c := New(api, st, fixedToken{}, clk, iv, DefaultQuota(), quiet, nil, nil)
		_ = c.PollOnce(context.Background())
		clk.Advance(2 * time.Minute)
		_ = c.PollOnce(context.Background())
		last := st.statuses[len(st.statuses)-1]
		if len(st.statuses) != 2 || len(last.Quota) != 0 {
			t.Errorf("statuses: %+v", st.statuses)
		}
		// The failure stays: it is the latest.
		if last.Failure.Kind != FailQuota {
			t.Errorf("failure: %+v", last.Failure)
		}
	})

	t.Run("a vehicle gone is forgotten", func(t *testing.T) {
		st := &memStore{targets: []Target{target()}}
		c := New(&scriptedAPI{}, st, fixedToken{}, clock.NewManual(t0), iv, DefaultQuota(), quiet, nil, nil)
		_ = c.PollOnce(context.Background())
		st.targets = nil
		_ = c.PollOnce(context.Background())
		if len(c.written) != 0 {
			t.Errorf("written: %+v", c.written)
		}
	})
}
