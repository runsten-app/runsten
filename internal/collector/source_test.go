package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
	local    Endpoint                                      // read without a call (EndpointSpec.Local)
	watch    bool                                          // the battery watched (EndpointSpec.Watch)
	wakes    bool                                          // the battery wakes the availability (EndpointSpec.Wakes)
	logins   int
	calls    map[Endpoint][]time.Time
	vins     []string // the VIN of each call, in order
}

func newFakeSource(clk clock.Clock) *fakeSource {
	return &fakeSource{clk: clk, daily: 10_000, ttl: time.Hour, calls: map[Endpoint][]time.Time{}}
}

func (f *fakeSource) Spec() SourceSpec {
	telemetry := func(ep Endpoint, parked Pace) EndpointSpec {
		return EndpointSpec{Endpoint: ep, API: "fake", Parked: parked, Driving: PaceActive, Charging: PaceActive}
	}
	spec := SourceSpec{
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
	for i := range spec.Endpoints {
		spec.Endpoints[i].Local = spec.Endpoints[i].Endpoint == f.local
		spec.Endpoints[i].Watch = f.watch && spec.Endpoints[i].Endpoint == psBattery
		if f.wakes && spec.Endpoints[i].Endpoint == psBattery {
			spec.Endpoints[i].Wakes = []Endpoint{psAvailability}
		}
	}
	return spec
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

func (s fakeSession) Fetch(_ context.Context, vin string, ep Endpoint) ([]byte, error) {
	now := s.f.clk.Now()
	s.f.calls[ep] = append(s.f.calls[ep], now)
	s.f.vins = append(s.f.vins, vin)
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
	saveErr  error // what SaveSnapshot answers; nil: it saves
}

func (s *sourceStore) SaveSnapshot(_ context.Context, sn Snapshot) (bool, error) {
	if s.saveErr != nil {
		return false, s.saveErr
	}
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

// TestSourceWatch: parked, a watched endpoint is read every active interval while the
// budget is ample; as calls run short, it falls back to its parked pace, and the other
// endpoints are still read at theirs.
func TestSourceWatch(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		daily                  int
		minBattery, maxBattery int // battery calls in 4 hours
	}{
		{"ample", 10_000, 235, 241},
		{"short", 1_000, 25, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
			src := newFakeSource(clk)
			src.watch, src.daily = true, tc.daily
			st := &sourceStore{memStore: memStore{targets: []Target{sourceTarget()}}}
			c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
			c.AddSource(src, st)
			t0 := clk.Now()
			for clk.Now().Before(t0.Add(4 * time.Hour)) {
				poll(t, c)
				clk.Advance(15 * time.Second)
			}
			if n := len(src.calls[psBattery]); n < tc.minBattery || n > tc.maxBattery {
				t.Errorf("battery read %d times in 4 hours, want %d to %d", n, tc.minBattery, tc.maxBattery)
			}
			if n := len(src.calls[psAvailability]); n != 24 {
				t.Errorf("availability read %d times in 4 hours, want 24: every 10 minutes", n)
			}
		})
	}
}

// TestSourceWakes: parked, a new upload of the battery reads the availability in the same
// pass; an upload already seen, or the first one read after a start, does not.
func TestSourceWakes(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	src := newFakeSource(clk)
	src.watch, src.wakes = true, true
	t0 := clk.Now()
	awake := t0.Add(time.Hour) // asleep before: its last upload, read again and again
	src.upload = func(ep Endpoint, _ []byte) (Upload, error) {
		if now := clk.Now(); ep == psBattery && !now.Before(awake) {
			return Upload{At: now.Add(-20 * time.Second)}, nil
		}
		return Upload{At: t0.Add(-3 * time.Hour)}, nil
	}
	st := &sourceStore{memStore: memStore{targets: []Target{sourceTarget()}}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	c.AddSource(src, st)
	for clk.Now().Before(awake.Add(5 * time.Minute)) {
		poll(t, c)
		clk.Advance(15 * time.Second)
	}
	var asleep, woken []time.Time
	for _, at := range src.calls[psAvailability] {
		if at.Before(awake) {
			asleep = append(asleep, at)
		} else {
			woken = append(woken, at)
		}
	}
	if len(asleep) != 6 {
		t.Errorf("availability read %d times asleep in an hour, want 6: every 10 minutes", len(asleep))
	}
	if len(woken) < 5 || woken[0].Sub(awake) > time.Minute {
		t.Errorf("availability read at %v once awake at %v, want with every new battery upload, the first within a minute", woken, awake)
	}
}

// TestSourceWatchFleet: six cars parked on one credential spend more than its budget
// with their batteries watched; the batteries slow down, and every car's availability is
// still read every parked interval.
func TestSourceWatchFleet(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	src := newFakeSource(clk)
	src.watch = true
	var targets []Target
	for i := range 6 {
		tg := sourceTarget()
		tg.VehicleID, tg.VIN = fmt.Sprintf("p%d", i), fmt.Sprintf("LPSVSEDE5RL00000%d", i)
		targets = append(targets, tg)
	}
	st := &sourceStore{memStore: memStore{targets: targets}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	c.AddSource(src, st)
	t0 := clk.Now()
	for clk.Now().Before(t0.Add(12 * time.Hour)) {
		poll(t, c)
		clk.Advance(15 * time.Second)
	}
	// 6 cars × 12 hours, every 10 minutes: 432 availability reads, a minute each: 4,320
	// battery reads.
	if n := len(src.calls[psAvailability]); n < 430 {
		t.Errorf("availability read %d times, want 432: every car every 10 minutes", n)
	}
	if n := len(src.calls[psBattery]); n >= 4_000 || n < 432 {
		t.Errorf("battery read %d times, want fewer than every minute, no fewer than every 10", n)
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
	t.Run("key shared: the connection waits, then asks for a new access", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.answer = func(_ Endpoint, login int, _ time.Time) ([]byte, error) {
			if login == 1 {
				return nil, &SourceError{Kind: KindKeyShared, Status: 403, RetryIn: 15 * time.Minute, Err: errors.New("taken")}
			}
			return []byte("parked"), nil
		}
		t0 := clk.Now()
		poll(t, c)
		s := st.statuses[len(st.statuses)-1]
		if src.logins != 1 || !s.PausedUntil.Equal(t0.Add(15*time.Minute)) || s.Failure.Kind != FailKeyShared {
			t.Fatalf("%d logins, status %+v", src.logins, s)
		}
		clk.Advance(16 * time.Minute)
		poll(t, c)
		s = st.statuses[len(st.statuses)-1]
		if src.logins != 2 || !s.ReadAt.Equal(clk.Now()) || !s.PausedUntil.IsZero() {
			t.Errorf("past the wait: %d logins, status %+v", src.logins, s)
		}
	})
	t.Run("stream held by another client: told, no pause", func(t *testing.T) {
		c, src, st, clk := newSourced(t, sourceTarget())
		src.answer = func(Endpoint, int, time.Time) ([]byte, error) {
			return nil, &SourceError{Kind: KindStreamHeld, Err: errors.New("connection refused: not authorized")}
		}
		poll(t, c)
		clk.Advance(10 * time.Minute)
		poll(t, c)
		s := st.statuses[len(st.statuses)-1]
		if len(src.calls[psBattery]) != 2 || !s.PausedUntil.IsZero() || s.Failure.Kind != FailStreamHeld {
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

// limitedSource is a fakeSource whose provider limits the calls per vehicle.
type limitedSource struct {
	*fakeSource
	held map[string]time.Time
}

func (l limitedSource) HeldUntil(vin string) time.Time { return l.held[vin] }

// TestSourceVehicleLimit: a vehicle its source holds back is not read, without a failure,
// while the connection's other vehicle is; a vehicle's own limit pauses neither the
// connection nor tells the user.
func TestSourceVehicleLimit(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	a, b := sourceTarget(), sourceTarget()
	b.VehicleID, b.VIN = "q", "LPSVSEDE5RL000002"
	src := limitedSource{newFakeSource(clk), map[string]time.Time{a.VIN: clk.Now().Add(30 * time.Minute)}}
	src.answer = func(Endpoint, int, time.Time) ([]byte, error) {
		if src.vins[len(src.vins)-1] == b.VIN {
			return nil, &SourceError{Kind: KindVehicleLimited, Status: 429, RetryIn: time.Hour, Err: errors.New("rate-limit-exceeded")}
		}
		return []byte("parked"), nil
	}
	st := &sourceStore{memStore: memStore{targets: []Target{a, b}}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	c.AddSource(src, st)
	poll(t, c)
	for _, vin := range src.vins {
		if vin == a.VIN {
			t.Fatalf("the vehicle held back was read: %v", src.vins)
		}
	}
	if len(src.vins) == 0 {
		t.Fatal("the other vehicle was not read")
	}
	for _, s := range st.statuses {
		if s.Failure.Kind != "" || !s.PausedUntil.IsZero() {
			t.Errorf("a vehicle's limit told or paused: %+v", s)
		}
	}
	for _, s := range st.statuses {
		if s.VehicleID == a.VehicleID && s.NextAt.Before(clk.Now().Add(30*time.Minute)) {
			t.Errorf("the vehicle held back is told due at %v", s.NextAt)
		}
	}
	clk.Advance(31 * time.Minute)
	src.vins = nil
	poll(t, c)
	if len(src.vins) == 0 || src.vins[0] != a.VIN {
		t.Errorf("past its hold, the vehicle is read: %v", src.vins)
	}
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
	r.budget.sources["fake"] = sourceAPI{providers: []string{"fake"}, bucket: bucketOf(27)}
	r.budget.restore(st.calls, []Target{sourceTarget(), target()}, clk.Now())
	if ok, _ := r.budget.allows(sourceTarget(), "fake", clk.Now()); ok {
		t.Error("the credential's call forgotten after a restart")
	}
	if n := r.budget.tokens[unit{api: "connected-vehicle"}]; n < r.budget.burst-float64(volvo) {
		t.Errorf("Volvo budget %.1f after a restart", n)
	}
	// Without the connection, its calls count on nothing.
	g := newBudget(DefaultQuota(), clk.Now())
	g.addSource("fake", []string{"fake"}, 27)
	g.restore(st.calls, []Target{target()}, clk.Now())
	if _, ok := g.tokens[unit{api: "fake"}]; ok || g.usage["a"] != float64(volvo) {
		t.Errorf("tokens %v, usage %v", g.tokens, g.usage)
	}
}

// TestSourceLocal: an endpoint that reads what the source holds is read at its pace with
// the budget spent, and counts on nothing.
func TestSourceLocal(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	src := newFakeSource(clk)
	src.daily, src.local = 27, psBattery // a burst of one call
	st := &sourceStore{memStore: memStore{targets: []Target{sourceTarget()}}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	c.AddSource(src, st)
	// Five parked intervals, less than the budget takes to give another call.
	for range 5 {
		poll(t, c)
		clk.Advance(DefaultIntervals().Parked)
	}
	if n := len(src.calls[psBattery]); n != 5 {
		t.Errorf("the local endpoint read %d times in 5 parked intervals", n)
	}
	if n := len(src.calls[psAvailability]); n != 1 {
		t.Errorf("%d calls on a burst of one", n)
	}
	fake := 0
	for _, call := range st.calls {
		if call.API == "fake" {
			fake += call.N
		}
	}
	if fake != 1 {
		t.Errorf("%d calls saved, the local reads among them", fake)
	}
}

// TestSourceLocalUnchanged: the source's memory read again, the same byte for byte, is
// not saved, which would confirm its values at that time; a push of the same values
// newly timestamped is, as one of new values; a response the store failed to save is
// saved at its next read.
func TestSourceLocalUnchanged(t *testing.T) {
	const (
		first   = `{"odometer":{"value":100,"timestamp":"06:00"}}`
		stamped = `{"odometer":{"value":100,"timestamp":"06:20"}}`
		rose    = `{"odometer":{"value":102,"timestamp":"06:23"}}`
	)
	for _, tc := range []struct {
		name  string
		reads []string // the stream's response at each parked interval
		fail  int      // the read whose save fails, from 1; 0: none
		want  int      // the responses saved
	}{
		{"the same bytes read again", []string{first, first, first}, 0, 1},
		{"the same value newly timestamped", []string{first, first, stamped}, 0, 2},
		{"a new value", []string{first, rose, rose}, 0, 2},
		{"a save failed, then the same bytes", []string{first, first}, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
			src := newFakeSource(clk)
			src.local = psBattery
			read := 0
			src.answer = func(ep Endpoint, _ int, _ time.Time) ([]byte, error) {
				if ep == psBattery {
					return []byte(tc.reads[read]), nil
				}
				return []byte("parked"), nil
			}
			st := &sourceStore{memStore: memStore{targets: []Target{sourceTarget()}}}
			c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
			c.AddSource(src, st)
			for read = range tc.reads {
				st.saveErr = nil
				if read+1 == tc.fail {
					st.saveErr = errors.New("store down")
				}
				poll(t, c)
				clk.Advance(DefaultIntervals().Parked)
			}
			n := 0
			for _, sn := range st.saved {
				if sn.Endpoint == psBattery {
					n++
				}
			}
			if n != tc.want {
				t.Errorf("%d responses saved, want %d", n, tc.want)
			}
			if vs := c.vehicles[sourceTarget().VehicleID]; tc.fail == 0 && !vs.readAt.Equal(clk.Now().Add(-DefaultIntervals().Parked)) {
				t.Errorf("read at %v: the reads that saved nothing are reads", vs.readAt)
			}
		})
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
