package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

// The endpoints of fakeSource, as the Polestar Data Portal's.
const (
	psBattery      Endpoint = "battery"
	psAvailability Endpoint = "availability"
	psOdometer     Endpoint = "odometer"
	psLocation     Endpoint = "location"
	psTargetSoC    Endpoint = "target-soc"
	psHealth       Endpoint = "health"
)

// fakeSource is a Source whose vehicle drives when its availability answers "driving"
// and charges when its battery answers "charging". Its calls answer what answer says,
// given the login they were made with (from 1).
type fakeSource struct {
	clk      clock.Clock
	daily    int
	ttl      time.Duration
	loginErr error
	answer   func(ep Endpoint, login int, at time.Time) ([]byte, error)
	upload   func(ep Endpoint, raw []byte) (Upload, error) // nil: nothing told
	logins   int
	calls    map[Endpoint][]time.Time
}

func newFakeSource(clk clock.Clock) *fakeSource {
	return &fakeSource{clk: clk, daily: 10_000, ttl: time.Hour, calls: map[Endpoint][]time.Time{}}
}

func (f *fakeSource) Spec() SourceSpec {
	telemetry := func(ep Endpoint, parked Pace) EndpointSpec {
		return EndpointSpec{Endpoint: ep, API: "fake", Parked: parked, Driving: PaceActive, Charging: PaceActive}
	}
	return SourceSpec{
		Name: "fake",
		Endpoints: []EndpointSpec{
			telemetry(psBattery, PaceParked), telemetry(psAvailability, PaceParked),
			telemetry(psOdometer, PaceRare), telemetry(psLocation, PaceRare),
			{Endpoint: psTargetSoC, API: "fake", Parked: PaceRare, Driving: PaceRare, Charging: PaceRare},
			{Endpoint: psHealth, API: "fake", Parked: PaceRare, Driving: PaceRare, Charging: PaceRare, Optional: true},
		},
		Settled: []Endpoint{psOdometer, psLocation},
		Daily:   f.daily,
	}
}

func (f *fakeSource) Login(context.Context, string, string) (Session, time.Duration, error) {
	f.logins++
	if f.loginErr != nil {
		return nil, 0, f.loginErr
	}
	return fakeSession{f, f.logins}, f.ttl, nil
}

func (*fakeSource) Observe(ep Endpoint, raw []byte, a *Activity) error {
	switch ep {
	case psAvailability:
		a.Engine, a.Driving = string(raw), string(raw) == "driving"
	case psBattery:
		a.Charging, a.Charges = string(raw), string(raw) == "charging"
	}
	return nil
}

func (*fakeSource) Fingerprint(raw []byte) ([]byte, error) {
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

func (f *fakeSource) Upload(ep Endpoint, raw []byte) (Upload, error) {
	if f.upload == nil {
		return Upload{}, nil
	}
	return f.upload(ep, raw)
}

type fakeSession struct {
	f     *fakeSource
	login int
}

func (s fakeSession) Fetch(_ context.Context, _ string, ep Endpoint) ([]byte, error) {
	now := s.f.clk.Now()
	s.f.calls[ep] = append(s.f.calls[ep], now)
	if s.f.answer != nil {
		return s.f.answer(ep, s.login, now)
	}
	return []byte("parked"), nil
}

// sourceStore is a Store and a SourceStore that keeps every snapshot, of any source.
type sourceStore struct {
	memStore
	refused  map[string]time.Time // credentials refused, by connection: the setAt refused
	unshared []string
}

func (s *sourceStore) SaveSnapshot(_ context.Context, sn Snapshot) (bool, error) {
	s.saved = append(s.saved, sn)
	return true, nil
}

func (s *sourceStore) SetCredentialRefused(_ context.Context, _, connection string, setAt time.Time) error {
	if s.refused == nil {
		s.refused = map[string]time.Time{}
	}
	s.refused[connection] = setAt
	return nil
}

func (s *sourceStore) SetUnshared(_ context.Context, _, vehicle string) error {
	s.unshared = append(s.unshared, vehicle)
	return nil
}

var credentialSetAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func sourceTarget() Target {
	return Target{AccountID: "a", VehicleID: "p", VIN: "LPSVSEDE5RL000001", ConnectionID: "pc", Provider: "fake", CredentialSetAt: credentialSetAt}
}

func newSourced(t *testing.T, targets ...Target) (*Collector, *fakeSource, *sourceStore, *clock.Manual) {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	src := newFakeSource(clk)
	st := &sourceStore{memStore: memStore{targets: targets}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	c.AddSource(src, st)
	return c, src, st, clk
}

func poll(t *testing.T, c *Collector) {
	t.Helper()
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func countIn(calls []time.Time, from, to time.Time) int {
	n := 0
	for _, at := range calls {
		if !at.Before(from) && at.Before(to) {
			n++
		}
	}
	return n
}

// TestSourcePaces: parked, the battery and the availability every parked interval, the
// rest every rare one; driving, the four telemetries every active interval, the target
// level still hourly; the odometer and the position at once when the trip ends.
func TestSourcePaces(t *testing.T) {
	c, src, st, clk := newSourced(t, sourceTarget())
	t0 := clk.Now()
	driveFrom, driveTo := t0.Add(2*time.Hour), t0.Add(2*time.Hour+30*time.Minute)
	src.answer = func(ep Endpoint, _ int, at time.Time) ([]byte, error) {
		if ep == psAvailability && !at.Before(driveFrom) && at.Before(driveTo) {
			return []byte("driving"), nil
		}
		return []byte("parked"), nil
	}
	for clk.Now().Before(t0.Add(4 * time.Hour)) {
		poll(t, c)
		clk.Advance(15 * time.Second)
	}
	parked := func(ep Endpoint) int { return countIn(src.calls[ep], t0, driveFrom) }
	if parked(psBattery) != 12 || parked(psAvailability) != 12 || parked(psOdometer) != 2 || parked(psLocation) != 2 ||
		parked(psTargetSoC) != 2 {
		t.Errorf("parked 2 h: battery %d, availability %d, odometer %d, location %d, target %d calls",
			parked(psBattery), parked(psAvailability), parked(psOdometer), parked(psLocation), parked(psTargetSoC))
	}
	// The drive is seen at the first availability read after it starts, within 10 minutes.
	seen := driveFrom.Add(10 * time.Minute)
	for _, ep := range []Endpoint{psBattery, psAvailability, psOdometer, psLocation} {
		if n := countIn(src.calls[ep], seen.Add(time.Minute), driveTo); n < 18 {
			t.Errorf("driving: %s read %d times in 19 minutes", ep, n)
		}
	}
	if n := countIn(src.calls[psTargetSoC], seen, driveTo.Add(time.Hour)); n > 1 {
		t.Errorf("driving: target read %d times", n)
	}
	// The end is seen at the first availability read after it: the odometer and the
	// position at once, in the same pass.
	var ended time.Time
	for _, at := range src.calls[psAvailability] {
		if !at.Before(driveTo) {
			ended = at
			break
		}
	}
	for _, ep := range []Endpoint{psOdometer, psLocation} {
		if !slices.Contains(src.calls[ep], ended) {
			t.Errorf("%s not read when the trip ended (%v)", ep, ended)
		}
	}
	if len(st.saved) == 0 || st.saved[0].Source != "fake" {
		t.Fatalf("saved %+v", st.saved)
	}
	if src.logins != 4 {
		t.Errorf("%d logins in 4 hours, want 4: one an hour", src.logins)
	}
}

// TestSourceSettles: after a trip, an odometer and a position that the vehicle uploaded
// while driving are read again every active interval, until it uploads new ones or for
// settleFor at most; an upload whose time is not told is taken as new.
func TestSourceSettles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		uploaded time.Duration // after the trip's end, when the vehicle uploads; 0: never, -1: not told
		reads    int           // of each settled endpoint, from the end to an hour later
	}{
		{"uploaded later", 3 * time.Minute, 4},
		{"never uploaded", 0, 11},
		{"time not told", -1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, src, _, clk := newSourced(t, sourceTarget())
			t0 := clk.Now()
			driveFrom, driveTo := t0.Add(time.Hour), t0.Add(time.Hour+30*time.Minute)
			src.answer = func(ep Endpoint, _ int, at time.Time) ([]byte, error) {
				if ep == psAvailability && !at.Before(driveFrom) && at.Before(driveTo) {
					return []byte("driving"), nil
				}
				return []byte("parked"), nil
			}
			src.upload = func(ep Endpoint, _ []byte) (Upload, error) {
				switch {
				case ep != psOdometer && ep != psLocation:
					return Upload{}, nil
				case tc.uploaded < 0:
					return Upload{}, nil
				case tc.uploaded > 0 && !clk.Now().Before(driveTo.Add(tc.uploaded)):
					return Upload{At: driveTo.Add(tc.uploaded)}, nil
				}
				return Upload{At: driveFrom.Add(5 * time.Minute)}, nil // sent on the way
			}
			for clk.Now().Before(driveTo.Add(time.Hour)) {
				poll(t, c)
				clk.Advance(15 * time.Second)
			}
			for _, ep := range []Endpoint{psOdometer, psLocation} {
				if n := countIn(src.calls[ep], driveTo, driveTo.Add(time.Hour)); n != tc.reads {
					t.Errorf("%s read %d times in the hour after the trip, want %d: %v", ep, n, tc.reads, src.calls[ep])
				}
			}
		})
	}
}

// TestSourceAccess: the access is asked for at the first call and kept until it expires;
// rejected, it is asked for again once, and rejected again, the connection waits a
// parked interval, while the account's Volvo vehicle is read.
func TestSourceAccess(t *testing.T) {
	volvoTarget := target()
	c, src, st, clk := newSourced(t, sourceTarget(), volvoTarget)
	rejectFrom := 0 // the logins from which every call is rejected; 0: none
	src.answer = func(_ Endpoint, login int, _ time.Time) ([]byte, error) {
		if rejectFrom > 0 && login >= rejectFrom {
			return nil, &SourceError{Kind: KindUnauthorized, Status: 401, Err: errors.New("expired")}
		}
		return []byte("parked"), nil
	}
	poll(t, c)
	clk.Advance(10 * time.Minute)
	poll(t, c)
	if src.logins != 1 || len(src.calls[psBattery]) != 2 {
		t.Fatalf("%d logins, %d battery calls; want 1, 2", src.logins, len(src.calls[psBattery]))
	}

	// A rejection of the access kept: asked for again, and the call made again.
	rejectFrom = 1
	src.answer = func(_ Endpoint, login int, _ time.Time) ([]byte, error) {
		if login == 1 {
			return nil, &SourceError{Kind: KindUnauthorized, Status: 401, Err: errors.New("expired")}
		}
		return []byte("parked"), nil
	}
	clk.Advance(10 * time.Minute)
	poll(t, c)
	if src.logins != 2 || len(src.calls[psBattery]) != 4 {
		t.Fatalf("after a rejection: %d logins, %d battery calls; want 2, 4", src.logins, len(src.calls[psBattery]))
	}

	// Rejected again with a new one: the connection waits.
	src.answer = func(_ Endpoint, _ int, _ time.Time) ([]byte, error) {
		return nil, &SourceError{Kind: KindUnauthorized, Status: 401, Err: errors.New("expired")}
	}
	clk.Advance(10 * time.Minute)
	pass := clk.Now()
	poll(t, c)
	calls := len(src.calls[psBattery])
	last := st.statuses[len(st.statuses)-1]
	for _, s := range st.statuses {
		if s.VehicleID == "p" && s.PassedAt.Equal(pass) {
			last = s
		}
	}
	if src.logins != 3 || !last.PausedUntil.Equal(pass.Add(DefaultIntervals().Parked)) || last.Failure.Kind != FailRejected {
		t.Fatalf("rejected again: %d logins, status %+v", src.logins, last)
	}
	clk.Advance(5 * time.Minute)
	volvoCalls := len(st.statuses)
	poll(t, c)
	if len(src.calls[psBattery]) != calls || src.logins != 3 {
		t.Errorf("read while paused: %d battery calls, %d logins", len(src.calls[psBattery]), src.logins)
	}
	if len(st.statuses) == volvoCalls {
		t.Error("no pass over the Volvo vehicle while the Polestar connection waits")
	}
}

// TestSourceRefusals: what the collector does of each refusal of the provider.
func TestSourceRefusals(t *testing.T) {
	refusal := func(k ErrorKind, status int) error {
		return &SourceError{Kind: k, Status: status, Err: errors.New("refused")}
	}
	t.Run("credential refused: marked, never asked again", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.loginErr = refusal(KindCredentialRefused, 401)
		poll(t, c)
		clk.Advance(time.Hour)
		poll(t, c)
		s := st.statuses[len(st.statuses)-1]
		if src.logins != 1 || !st.refused["pc"].Equal(credentialSetAt) || !s.NextAt.IsZero() ||
			s.Failure.Kind != FailCredentialRefused {
			t.Errorf("%d logins, refused %v, status %+v", src.logins, st.refused, s)
		}
		// A new credential is read.
		src.loginErr = nil
		st.targets[0].CredentialSetAt = credentialSetAt.Add(time.Hour)
		poll(t, c)
		if src.logins != 2 || len(src.calls[psBattery]) != 1 {
			t.Errorf("new credential: %d logins, %d calls", src.logins, len(src.calls[psBattery]))
		}
	})
	t.Run("credential refused by the store: not polled", func(t *testing.T) {
		tg := sourceTarget()
		tg.CredentialRefused = true
		c, src, st, _ := newSourced(t, tg)
		poll(t, c)
		if src.logins != 0 || !st.statuses[0].NextAt.IsZero() {
			t.Errorf("%d logins, status %+v", src.logins, st.statuses[0])
		}
	})
	t.Run("vehicle unshared: marked, no longer polled", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.answer = func(Endpoint, int, time.Time) ([]byte, error) { return nil, refusal(KindUnshared, 403) }
		poll(t, c)
		clk.Advance(time.Hour)
		poll(t, c)
		s := st.statuses[len(st.statuses)-1]
		if len(src.calls[psBattery]) != 1 || len(src.calls[psAvailability]) != 0 || !slices.Equal(st.unshared, []string{"p"}) ||
			!s.NextAt.IsZero() || s.Failure != (Failure{At: s.Failure.At, Endpoint: psBattery, Status: 403, Kind: FailUnshared}) {
			t.Errorf("calls %v, unshared %v, status %+v", src.calls, st.unshared, s)
		}
	})
	t.Run("endpoint not granted: left out a day, told", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.answer = func(ep Endpoint, _ int, _ time.Time) ([]byte, error) {
			if ep == psLocation {
				return nil, refusal(KindNotGranted, 403)
			}
			return []byte("parked"), nil
		}
		for range 24 * 6 {
			poll(t, c)
			clk.Advance(10 * time.Minute)
		}
		s := st.statuses[len(st.statuses)-1]
		if len(src.calls[psLocation]) != 1 || len(src.calls[psOdometer]) != 24 || s.Failure.Kind != FailNotGranted {
			t.Errorf("location %d, odometer %d calls in a day; status %+v", len(src.calls[psLocation]), len(src.calls[psOdometer]), s)
		}
	})
	t.Run("optional endpoint not granted: left out a day, not told", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.answer = func(ep Endpoint, _ int, _ time.Time) ([]byte, error) {
			if ep == psHealth {
				return nil, refusal(KindNotGranted, 403)
			}
			return []byte("parked"), nil
		}
		for range 24 * 6 {
			poll(t, c)
			clk.Advance(10 * time.Minute)
		}
		s := st.statuses[len(st.statuses)-1]
		if len(src.calls[psHealth]) != 1 || len(src.calls[psOdometer]) != 24 || s.Failure.Kind != "" {
			t.Errorf("health %d, odometer %d calls in a day; status %+v", len(src.calls[psHealth]), len(src.calls[psOdometer]), s)
		}
	})
	t.Run("endpoint not available: left out an hour, not told", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.answer = func(ep Endpoint, _ int, _ time.Time) ([]byte, error) {
			if ep == psBattery {
				return nil, refusal(KindNotAvailable, 404)
			}
			return []byte("parked"), nil
		}
		for range 6 {
			poll(t, c)
			clk.Advance(10 * time.Minute)
		}
		poll(t, c)
		s := st.statuses[len(st.statuses)-1]
		if len(src.calls[psBattery]) != 2 || len(src.calls[psAvailability]) != 7 || s.Failure != (Failure{}) {
			t.Errorf("battery %d, availability %d calls in an hour; status %+v", len(src.calls[psBattery]),
				len(src.calls[psAvailability]), s)
		}
	})
	t.Run("rate limited: the connection waits", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.answer = func(Endpoint, int, time.Time) ([]byte, error) {
			return nil, &SourceError{Kind: KindRateLimited, Status: 429, RetryIn: 2 * time.Minute, Err: errors.New("slow down")}
		}
		t0 := clk.Now()
		poll(t, c)
		clk.Advance(time.Minute)
		poll(t, c)
		s := st.statuses[len(st.statuses)-1]
		if len(src.calls[psBattery]) != 1 || !s.PausedUntil.Equal(t0.Add(2*time.Minute)) || s.Failure.Kind != FailRateLimited {
			t.Errorf("%d calls, status %+v", len(src.calls[psBattery]), s)
		}
	})
	t.Run("no access: asked for again after a while", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.loginErr = errors.New("dial tcp: connection refused")
		poll(t, c)
		clk.Advance(15 * time.Second)
		poll(t, c)
		s := st.statuses[len(st.statuses)-1]
		if src.logins != 1 || s.Failure.Kind != FailToken || s.Failure.Endpoint != "" {
			t.Errorf("%d logins, status %+v", src.logins, s)
		}
		clk.Advance(loginRetry)
		src.loginErr = nil
		poll(t, c)
		if src.logins != 2 || len(src.calls[psBattery]) != 1 {
			t.Errorf("after the delay: %d logins, %d calls", src.logins, len(src.calls[psBattery]))
		}
	})
	t.Run("no credential: not polled until one is set", func(t *testing.T) {
		tg := sourceTarget()
		tg.CredentialSetAt = time.Time{}
		c, src, st, _ := newSourced(t, tg)
		poll(t, c)
		if src.logins != 0 || !st.statuses[0].NextAt.IsZero() {
			t.Errorf("%d logins, status %+v", src.logins, st.statuses[0])
		}
		st.targets[0].CredentialSetAt = credentialSetAt
		poll(t, c)
		if src.logins != 1 || len(src.calls[psBattery]) != 1 {
			t.Errorf("credential set: %d logins, %d calls", src.logins, len(src.calls[psBattery]))
		}
	})
	t.Run("a provider no source reads: not polled", func(t *testing.T) {
		tg := sourceTarget()
		tg.Provider = "other"
		c, src, st, _ := newSourced(t, tg)
		poll(t, c)
		if src.logins != 0 || !st.statuses[0].NextAt.IsZero() {
			t.Errorf("%d logins, status %+v", src.logins, st.statuses[0])
		}
	})
}

// TestSourceQuota: a Source's calls count on its connection's credential, at its own
// daily quota, apart from the Volvo API's; a restart replays them onto the credential.
func TestSourceQuota(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	src := newFakeSource(clk)
	src.daily = 27 // a burst of one call
	st := &sourceStore{memStore: memStore{targets: []Target{sourceTarget(), target()}}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	c.AddSource(src, st)
	poll(t, c)
	if n := len(src.calls[psBattery]) + len(src.calls[psAvailability]); n != 1 {
		t.Errorf("%d calls on a burst of one", n)
	}
	if u := c.budget.unitOf(sourceTarget(), "fake"); u != (unit{key: "pc", api: "fake"}) {
		t.Errorf("unit %+v", u)
	}
	var fake, volvo int
	for _, call := range st.calls {
		if call.API == "fake" {
			fake += call.N
		} else {
			volvo += call.N
		}
	}
	if fake != 1 || volvo != len(endpoints()) {
		t.Fatalf("calls saved: %d of the source, %d of Volvo", fake, volvo)
	}

	// Restarted: the call counts on the credential again, and the Volvo budget is whole.
	r := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	r.AddSource(newFakeSource(clk), st)
	r.budget.sources["fake"] = sourceAPI{provider: "fake", bucket: bucketOf(27)}
	r.budget.restore(st.calls, []Target{sourceTarget(), target()}, clk.Now())
	if ok, _ := r.budget.allows(sourceTarget(), "fake", clk.Now()); ok {
		t.Error("the credential's call forgotten after a restart")
	}
	if n := r.budget.tokens[unit{api: "connected-vehicle"}]; n < r.budget.burst-float64(volvo) {
		t.Errorf("Volvo budget %.1f after a restart", n)
	}
	// Without the connection, its calls count on nothing.
	g := newBudget(DefaultQuota(), clk.Now())
	g.addSource("fake", "fake", 27)
	g.restore(st.calls, []Target{target()}, clk.Now())
	if _, ok := g.tokens[unit{api: "fake"}]; ok || g.usage["a"] != float64(volvo) {
		t.Errorf("tokens %v, usage %v", g.tokens, g.usage)
	}
}

// TestSourceLogs: what measures a provider on real vehicles is logged, never the VIN: each
// new upload with its age and the time since the previous, the fields when they change,
// an access rejected with its age, a rate limit with the calls of the last minute.
func TestSourceLogs(t *testing.T) {
	c, src, _, clk := newSourced(t, sourceTarget())
	var logs bytes.Buffer
	c.log = slog.New(slog.NewJSONHandler(&logs, nil))
	t0 := clk.Now()
	fields := []string{"a"}
	src.upload = func(ep Endpoint, _ []byte) (Upload, error) {
		if ep != psBattery {
			return Upload{}, nil
		}
		// The car uploads every 20 minutes, its cloud receiving each a second later.
		at := t0.Add(clk.Now().Sub(t0).Truncate(20 * time.Minute)).Add(-time.Minute)
		return Upload{At: at, ReceivedAt: at.Add(time.Second), ModelYear: 2024, Fields: fields}, nil
	}
	for range 4 { // battery read at 0, 10, 20 and 30 minutes
		poll(t, c)
		clk.Advance(10 * time.Minute)
	}
	if n := strings.Count(logs.String(), `"msg":"vehicle upload"`); n != 2 {
		t.Errorf("%d uploads logged, want 2:\n%s", n, &logs)
	}
	for _, want := range []string{`"age":"1m0s"`, `"received_after":"1s"`, `"since_previous":"20m0s"`, `"mode":"parked"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("no %s in the logs:\n%s", want, &logs)
		}
	}
	if n := strings.Count(logs.String(), `"fields":"a"`); n != 1 {
		t.Errorf("the battery's fields logged %d times, want once", n)
	}
	fields = []string{"a", "b"}
	poll(t, c)
	if !strings.Contains(logs.String(), `"model_year":2024,"fields":"a,b"`) {
		t.Errorf("new fields not logged:\n%s", &logs)
	}

	src.answer = func(_ Endpoint, login int, _ time.Time) ([]byte, error) {
		if login == 1 {
			return nil, &SourceError{Kind: KindUnauthorized, Status: 401, Err: errors.New("expired")}
		}
		return []byte("parked"), nil
	}
	clk.Advance(10 * time.Minute)
	poll(t, c)
	if !strings.Contains(logs.String(), `"msg":"access rejected: asked for again"`) || !strings.Contains(logs.String(), `"access_age":"50m0s"`) {
		t.Errorf("access rejected not logged with its age:\n%s", &logs)
	}

	src.answer = func(Endpoint, int, time.Time) ([]byte, error) {
		return nil, &SourceError{Kind: KindRateLimited, Status: 429, RetryIn: time.Minute, Err: errors.New("Too Many Requests [Retry-After=60]")}
	}
	clk.Advance(10 * time.Minute)
	poll(t, c)
	if !strings.Contains(logs.String(), `"calls_last_minute":1,"budget_left":`) || !strings.Contains(logs.String(), `Retry-After=60`) {
		t.Errorf("rate limit not logged with the calls:\n%s", &logs)
	}
	if strings.Contains(logs.String(), sourceTarget().VIN) {
		t.Errorf("the VIN is in the logs:\n%s", &logs)
	}
}

// countingTokens is a TokenSource that counts its calls.
type countingTokens struct{ calls int }

func (c *countingTokens) Token(context.Context, string, string) (string, error) {
	c.calls++
	return "token", nil
}

func (c *countingTokens) Refresh(context.Context, string, string, string) (string, error) {
	c.calls++
	return "token", nil
}

func (c *countingTokens) KeepAlive(context.Context) error {
	c.calls++
	return nil
}

// TestWithoutVolvo: on an instance without a Volvo application, a Volvo ID's vehicle is
// not polled and no token is asked for or refreshed; a Polestar's is read as usual.
func TestWithoutVolvo(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	src := newFakeSource(clk)
	st := &sourceStore{memStore: memStore{targets: []Target{target(), sourceTarget()}}}
	api, tokens := &scriptedAPI{}, &countingTokens{}
	c := New(api, st, tokens, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	c.AddSource(src, st)
	c.WithoutVolvo()
	for range 3 {
		poll(t, c)
		clk.Advance(time.Minute)
	}
	if len(api.calls) != 0 || tokens.calls != 0 {
		t.Errorf("Volvo called: API %v, tokens %d", api.calls, tokens.calls)
	}
	if len(src.calls[psBattery]) == 0 {
		t.Error("the Polestar was not read")
	}
	for _, s := range st.statuses {
		if s.VehicleID == "v" && !s.NextAt.IsZero() {
			t.Errorf("the Volvo is due at %s", s.NextAt)
		}
	}
}
