package importer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/aximote"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/aximoteapi"
)

var (
	t0     = time.Date(2026, 10, 11, 9, 30, 0, 0, time.UTC)
	cutoff = t0.Add(-time.Hour) // the vehicle's first snapshot
)

const lease = 2 * time.Minute

// memStore keeps one job and what it imported, as the store does: one row per event of a
// source, what starts at or after the cutoff left.
type memStore struct {
	mu       sync.Mutex
	job      Job
	status   string // pending, running, done, failed
	failure  Failure
	notUntil time.Time
	claimed  bool
	counts   [3]int // imported, skipped, invalid
	trips    map[string]ImportedTrip
	charges  map[string]core.Charge
	pages    int
	err      error // returned by SaveImportPage when set
}

func newMemStore() *memStore {
	return &memStore{
		job:    Job{ID: "job-1", AccountID: "acc", VehicleID: "veh", Source: SourceAximote, SourceVehicle: "veh-ex30", Token: aximoteapi.DevToken, Step: StepTrips},
		status: "pending", trips: map[string]ImportedTrip{}, charges: map[string]core.Charge{},
	}
}

func (m *memStore) ClaimImport(_ context.Context, at time.Time, lease time.Duration) (Job, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if (m.status != "pending" && m.status != "running") || at.Before(m.notUntil) ||
		(m.claimed && at.Before(m.job.ClaimedAt.Add(lease))) {
		return Job{}, false, nil
	}
	m.status, m.claimed, m.job.ClaimedAt = "running", true, at
	return m.job, true, nil
}

func (m *memStore) ImportCutoff(context.Context, string, string, time.Time) (time.Time, error) {
	return cutoff, nil
}

func (m *memStore) SaveImportPage(_ context.Context, job Job, page Page) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return Result{}, m.err
	}
	if m.status != "running" || !job.ClaimedAt.Equal(m.job.ClaimedAt) {
		return Result{}, ErrLeaseLost
	}
	res := Result{Cutoff: cutoff}
	for _, t := range page.Trips {
		if !t.Trip.Start.After.Before(cutoff) {
			res.Skipped++
			continue
		}
		m.trips[t.Trip.Import.V.SourceID] = t
		res.Saved++
	}
	for _, c := range page.Charges {
		if !c.Start.After.Before(cutoff) {
			res.Skipped++
			continue
		}
		m.charges[c.Import.V.SourceID] = c
		res.Saved++
	}
	m.pages++
	m.counts[0] += res.Saved
	m.counts[1] += res.Skipped
	m.counts[2] += page.Invalid
	m.job.Step, m.job.Cursor, m.job.ClaimedAt, m.job.Failures = page.Step, page.Cursor, page.At, 0
	if page.Done {
		m.status, m.job.Token = "done", ""
	}
	return res, nil
}

func (m *memStore) RetryImport(_ context.Context, job Job, notBefore time.Time, failures int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !job.ClaimedAt.Equal(m.job.ClaimedAt) {
		return ErrLeaseLost
	}
	m.status, m.claimed, m.notUntil, m.job.Failures = "pending", false, notBefore, failures
	return nil
}

func (m *memStore) FailImport(_ context.Context, job Job, reason Failure, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !job.ClaimedAt.Equal(m.job.ClaimedAt) {
		return ErrLeaseLost
	}
	m.status, m.failure, m.job.Token = "failed", reason, ""
	return nil
}

// failing wraps a source whose calls fail as set.
type failing struct {
	Source
	trips, tracks error
}

func (f *failing) Trips(ctx context.Context, token string, q aximote.Query) (aximote.Page[aximote.Trip], error) {
	if f.trips != nil {
		return aximote.Page[aximote.Trip]{}, f.trips
	}
	return f.Source.Trips(ctx, token, q) //nolint:wrapcheck // a test's
}

func (f *failing) Tracks(ctx context.Context, token string, ids []string) (map[string][]byte, error) {
	if f.tracks != nil {
		return nil, f.tracks
	}
	return f.Source.Tracks(ctx, token, ids) //nolint:wrapcheck // a test's
}

type env struct {
	store  *memStore
	fake   *aximoteapi.Handler
	source *failing
	clk    *clock.Manual
	worker *Worker
	logs   *bytes.Buffer
}

// newEnv runs a worker on the simulator's Aximote, whose history ends a month before
// t0: pages of two events.
func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{store: newMemStore(), clk: clock.NewManual(t0), logs: &bytes.Buffer{}}
	e.fake = aximoteapi.NewHandler(aximoteapi.DevToken, aximoteapi.Default(t0, "YV1SMLT0000ER0001"))
	srv := httptest.NewServer(e.fake)
	t.Cleanup(srv.Close)
	e.source = &failing{Source: aximote.NewClient(srv.URL, srv.Client())}
	params := DefaultParams()
	params.PageSize = 2
	e.worker = New(e.store, e.source, e.clk, params, slog.New(slog.NewTextHandler(e.logs, nil)))
	return e
}

func (e *env) once(t *testing.T) bool {
	t.Helper()
	worked, err := e.worker.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return worked
}

func TestImport(t *testing.T) {
	e := newEnv(t)
	if !e.once(t) {
		t.Fatal("no job run")
	}
	s := e.store
	if s.status != "done" || s.job.Token != "" || s.counts != [3]int{3, 0, 2} || s.pages != 4 {
		t.Fatalf("status %s, counts %v, %d pages", s.status, s.counts, s.pages)
	}
	if e.once(t) {
		t.Error("a job done ran again")
	}

	// Only what starts before the cutoff is asked for.
	for _, call := range e.fake.Calls() {
		if strings.HasPrefix(call, "GET /api/public/v1/trips") || strings.HasPrefix(call, "GET /api/public/v1/refuels") {
			if !strings.Contains(call, "until=2026-10-11T08%3A30%3A00Z") || !strings.Contains(call, "limit=2") {
				t.Errorf("call %s", call)
			}
		}
	}

	trip, ok := s.trips["trip-1001"]
	start := time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC)
	if !ok || trip.Trip.Start != (core.Bounds{After: start, Before: start}) || trip.Trip.End.After != start.Add(35*time.Minute) ||
		trip.Trip.DistanceKm.V != 24.5 || trip.Trip.EnergyKWh.V != 5.1 || trip.Trip.StartSoC.OK || trip.Trip.StartOdometerKm.OK ||
		trip.Trip.Import.V != (core.Import{Source: "aximote", SourceID: "trip-1001", ImportedAt: t0, StartAddress: "Lyon, France", EndAddress: "Villeurbanne, France"}) ||
		!slices.Equal(trip.Trip.Sources, []string{"aximote"}) || trip.Trip.From.V != (core.Position{Lat: 45.764, Lon: 4.8357}) ||
		!strings.HasPrefix(string(trip.Track), `{"type":"LineString","coordinates":[[4.8357,45.764],`) {
		t.Errorf("trip %+v, track %s", trip.Trip, trip.Track)
	}
	if other := s.trips["trip-1002"]; other.Track != nil || other.Trip.Import.V.SourceID != "trip-1002" {
		t.Errorf("trip without a track %+v", other)
	}
	if _, ok := s.trips["trip-2001"]; ok || len(s.trips) != 2 {
		t.Errorf("trips %v", s.trips)
	}

	c, ok := s.charges["charge-501"]
	if !ok || len(s.charges) != 1 {
		t.Fatalf("charges %v", s.charges)
	}
	near := func(v core.Value[float64], want float64) bool { return v.OK && math.Abs(v.V-want) < 1e-9 }
	if c.Type.OK || !near(c.StartSoC, 65) || !near(c.EndSoC, 90) || !near(c.EnergySoCKWh, 16) || !near(c.EnergyPowerKWh, 16.8) ||
		!near(c.AveragePowerKW, 8.4) || c.Capacity.V != (core.Capacity{KWh: 64, Source: core.CapacityAPI}) ||
		c.PaidCost.V != (core.Amount{Currency: "EUR", Minor: 437}) || c.Import.V.StartAddress != "Lyon, France" ||
		c.Start.After != time.Date(2026, 9, 8, 19, 0, 0, 0, time.UTC) || c.Start.After != c.Start.Before {
		t.Errorf("charge %+v", c)
	}
	if strings.Contains(e.logs.String(), aximoteapi.DevToken) || !strings.Contains(e.logs.String(), "import done") {
		t.Errorf("logs:\n%s", e.logs)
	}
}

// A 429 puts the job back until Retry-After; it resumes at its cursor, nothing read
// twice.
func TestImportWaitsForTheLimit(t *testing.T) {
	e := newEnv(t)
	// The vehicle, the first page of trips and its tracks, then the second page: limited.
	e.fake.LimitAfter(3, 90*time.Second)
	e.once(t)
	s := e.store
	// Newest first: the trip still driving, left, and the evening's.
	if s.status != "pending" || s.job.Step != StepTrips || s.job.Cursor != "2" || s.counts != [3]int{1, 0, 1} || s.job.Failures != 0 ||
		!s.notUntil.Equal(t0.Add(90*time.Second)) {
		t.Fatalf("after the limit: %s %+v %v", s.status, s.job, s.counts)
	}
	if e.once(t) {
		t.Error("ran before Retry-After")
	}
	e.clk.Advance(90 * time.Second)
	e.once(t)
	if s.status != "done" || s.counts != [3]int{3, 0, 2} {
		t.Errorf("resumed: %s %v", s.status, s.counts)
	}
	trips := 0
	for _, c := range e.fake.Calls() {
		if strings.Contains(c, "/trips?") && !strings.Contains(c, "cursor=") {
			trips++
		}
	}
	if trips != 1 {
		t.Errorf("the first page read %d times", trips)
	}
}

func TestImportTokenRefused(t *testing.T) {
	e := newEnv(t)
	e.fake.Revoke()
	e.once(t)
	if s := e.store; s.status != "failed" || s.failure != FailTokenRefused || s.job.Token != "" {
		t.Errorf("%s %s", s.status, s.failure)
	}

	// While exporting the tracks too.
	e = newEnv(t)
	e.source.tracks = &aximote.APIError{Call: "tracks", Status: 403, Kind: aximote.KindTokenRefused}
	e.once(t)
	if s := e.store; s.status != "failed" || s.failure != FailTokenRefused || s.counts[0] != 0 {
		t.Errorf("tracks: %s %s %v", s.status, s.failure, s.counts)
	}
}

// Failures are retried after Backoff, MaxFailures times in a row.
func TestImportFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*env)
		want Failure
	}{
		{"the source", func(e *env) { e.source.trips = &aximote.APIError{Call: "trips", Status: 503} }, FailSource},
		{"the store", func(e *env) { e.store.err = errors.New("database down") }, FailInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			tc.set(e)
			for i := 1; i < DefaultParams().MaxFailures; i++ {
				e.once(t)
				if s := e.store; s.status != "pending" || s.job.Failures != i || !s.notUntil.Equal(e.clk.Now().Add(time.Minute)) {
					t.Fatalf("failure %d: %s %+v", i, s.status, s.job)
				}
				e.clk.Advance(time.Minute)
			}
			e.once(t)
			if s := e.store; s.status != "failed" || s.failure != tc.want {
				t.Errorf("%s %s", s.status, s.failure)
			}
		})
	}

	t.Run("a success between them starts again", func(t *testing.T) {
		e := newEnv(t)
		e.store.job.Failures = 4
		e.once(t)
		if s := e.store; s.status != "done" || s.job.Failures != 0 {
			t.Errorf("%s %+v", s.status, s.job)
		}
	})
}

// Tracks the worker cannot read leave the trips without them.
func TestImportWithoutTracks(t *testing.T) {
	e := newEnv(t)
	e.source.tracks = &aximote.APIError{Call: "tracks", Status: 404}
	e.once(t)
	if s := e.store; s.status != "done" || s.counts[0] != 3 || s.trips["trip-1001"].Track != nil {
		t.Errorf("%s %v", s.status, s.counts)
	}
	if !strings.Contains(e.logs.String(), "tracks not imported") {
		t.Errorf("logs:\n%s", e.logs)
	}
}

func TestImportLeaseLost(t *testing.T) {
	e := newEnv(t)
	e.source.trips = &aximote.APIError{Call: "trips", Status: 503}
	job, _, _ := e.store.ClaimImport(context.Background(), t0, lease)
	// Another worker took the job over: the first one's failure writes nothing.
	e.clk.Advance(lease)
	e.once(t)
	if err := e.store.RetryImport(context.Background(), job, t0, 1); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("stale retry: %v", err)
	}
	e.store.job.Source = "volvo_csv"
	e.store.status, e.store.notUntil = "pending", time.Time{}
	e.source.trips = nil
	e.clk.Advance(time.Hour)
	e.once(t)
	if e.store.job.Failures != 2 {
		t.Errorf("an unknown source: %+v", e.store.job)
	}
}

func TestRunStops(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.worker.Run(ctx)
		close(done)
	}()
	for {
		e.store.mu.Lock()
		status := e.store.status
		e.store.mu.Unlock()
		if status == "done" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
}
