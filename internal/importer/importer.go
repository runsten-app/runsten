// Package importer imports a vehicle's past trips and charges from another application
// (ADR 0030, ADR 0036): its worker claims the jobs the API created, reads the source page
// by page, converts and checks what it reads, and writes it through its Store, which
// keeps only what starts before the vehicle's first snapshot. Files uploaded by the user
// (Files) are converted and written at once, without a job. It knows neither HTTP nor
// the database: it declares its Store, its Saver and its Source, which the binary wires.
package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"runsten/internal/aximote"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
)

// SourceAximote is the only source so far, in the vocabulary of the snapshots' sources.
const SourceAximote = "aximote"

// ImportedTrip is a trip to import, with its track when the source gives one: a GeoJSON
// LineString.
type ImportedTrip struct {
	Trip  core.Trip
	Track []byte // nil: none given, a track stored before stays
}

// Result tells what a write of imported events did.
type Result struct {
	Saved   int // events written, new or replacing the same event of the same source
	Skipped int // events starting at or after Cutoff
	// Cutoff is the vehicle's first snapshot, of any source, or the time of the import
	// when it has none: only what starts before it is imported.
	Cutoff time.Time
}

// Step is what a job reads: the trips, then the charges.
type Step string

// Steps.
const (
	StepTrips   Step = "trips"
	StepCharges Step = "charges"
)

// Failure is why a job failed: a kind, never the source's message.
type Failure string

// Failures.
const (
	FailTokenRefused Failure = "token_refused" // the source refused the token
	FailSource       Failure = "source_failed" // the source kept failing
	FailInternal     Failure = "internal"      // Runsten's side failed: see the logs
)

// Job is a claimed import.
type Job struct {
	ID, AccountID, VehicleID string
	Source                   string
	SourceVehicle            string // the vehicle at the source
	Token                    string // opened; never logged
	Step                     Step
	Cursor                   string // the source's, of the step's next page; empty: its first
	Failures                 int    // in a row, before this claim
	// ClaimedAt is the claim, renewed by each page: a write of a claim another worker
	// has taken over since is refused (ErrLeaseLost).
	ClaimedAt time.Time
}

// Page is what a job writes after reading a page of its source, at once: the events,
// and where the job stands after them.
type Page struct {
	Trips   []ImportedTrip
	Charges []core.Charge
	Invalid int // events the checks refused, left out
	Step    Step
	Cursor  string // the next page's; empty: the step's first
	Done    bool   // the last page: the job ends, its token erased
	At      time.Time
}

// ErrLeaseLost is returned for a write of a job whose claim is over: another worker
// claimed it since.
var ErrLeaseLost = errors.New("the job's claim is over")

// Store keeps the jobs and writes the imported events.
type Store interface {
	// ClaimImport claims the oldest job of any account that may run and whose previous
	// claim is older than lease; false when there is none.
	ClaimImport(ctx context.Context, at time.Time, lease time.Duration) (Job, bool, error)
	// ImportCutoff is the vehicle's first snapshot, or at when it has none.
	ImportCutoff(ctx context.Context, accountID, vehicleID string, at time.Time) (time.Time, error)
	// SaveImportPage writes a page's events and the job's progress, in one transaction,
	// and renews its claim to the page's At.
	SaveImportPage(ctx context.Context, job Job, page Page) (Result, error)
	// RetryImport releases the job until notBefore, with its failures in a row.
	RetryImport(ctx context.Context, job Job, notBefore time.Time, failures int) error
	// FailImport ends the job, failed: its token is erased.
	FailImport(ctx context.Context, job Job, reason Failure, at time.Time) error
}

// Source is Aximote's public API.
type Source interface {
	Vehicle(ctx context.Context, token, id string) (aximote.Vehicle, error)
	Trips(ctx context.Context, token string, q aximote.Query) (aximote.Page[aximote.Trip], error)
	Refuels(ctx context.Context, token string, q aximote.Query) (aximote.Page[aximote.Refuel], error)
	Tracks(ctx context.Context, token string, tripIDs []string) (map[string][]byte, error)
}

// Params are the worker's pace and retries.
type Params struct {
	// PageSize is the events asked per page. Assumption: Aximote documents no maximum.
	PageSize int
	// Lease is how long a claim holds without a page written: a job is resumed after it
	// when its worker stopped.
	Lease time.Duration
	// Idle is the wait when no job is to run.
	Idle time.Duration
	// Backoff is the wait after a failure, MaxFailures the failures in a row before a
	// job fails.
	Backoff     time.Duration
	MaxFailures int
}

// DefaultParams are the worker's defaults.
func DefaultParams() Params {
	return Params{PageSize: 50, Lease: 2 * time.Minute, Idle: 5 * time.Second, Backoff: time.Minute, MaxFailures: 5}
}

// Worker runs the jobs, one at a time.
type Worker struct {
	store  Store
	source Source
	clk    clock.Clock
	params Params
	log    *slog.Logger
}

// New creates the worker.
func New(store Store, source Source, clk clock.Clock, params Params, log *slog.Logger) *Worker {
	return &Worker{store: store, source: source, clk: clk, params: params, log: log}
}

// errSource marks the source's failures, apart from the store's.
var errSource = errors.New("source")

// Once runs one job, if one may run, until it ends or must wait: false when there was
// none. What the job meets is written to it, and is no error of Once.
func (w *Worker) Once(ctx context.Context) (bool, error) {
	job, ok, err := w.store.ClaimImport(ctx, w.now(), w.params.Lease)
	if err != nil || !ok {
		return false, err //nolint:wrapcheck // Store error, already has context
	}
	log := w.log.With("account", job.AccountID, "vehicle", job.VehicleID, "job", job.ID, "source", job.Source)
	err = w.run(ctx, &job, log)
	var apiErr *aximote.APIError
	isAPI := errors.As(err, &apiErr)
	switch {
	case err == nil:
		return true, nil
	case ctx.Err() != nil:
		// Stopped: the job is resumed once its claim is over.
		return true, nil //nolint:nilerr // the job's, not Once's
	case errors.Is(err, ErrLeaseLost):
		log.WarnContext(ctx, "import taken over by another worker")
		return true, nil
	case isAPI && apiErr.TokenRefused():
		log.InfoContext(ctx, "import failed: token refused")
		return true, w.fail(ctx, job, FailTokenRefused)
	case isAPI && apiErr.Kind == aximote.KindRateLimited:
		log.InfoContext(ctx, "import waits for the source's limit", "retry_in", apiErr.RetryIn.String())
		return true, w.retry(ctx, job, w.now().Add(apiErr.RetryIn), job.Failures)
	}
	failures := job.Failures + 1
	if failures < w.params.MaxFailures {
		log.WarnContext(ctx, "import failed, retried later", "error", err, "failures", failures)
		return true, w.retry(ctx, job, w.now().Add(w.params.Backoff), failures)
	}
	reason := FailInternal
	if errors.Is(err, errSource) {
		reason = FailSource
	}
	log.ErrorContext(ctx, "import failed", "error", err, "failure", string(reason))
	return true, w.fail(ctx, job, reason)
}

func (w *Worker) retry(ctx context.Context, job Job, notBefore time.Time, failures int) error {
	if err := w.store.RetryImport(ctx, job, notBefore, failures); err != nil {
		return fmt.Errorf("releasing an import: %w", err)
	}
	return nil
}

func (w *Worker) fail(ctx context.Context, job Job, reason Failure) error {
	if err := w.store.FailImport(ctx, job, reason, w.now()); err != nil {
		return fmt.Errorf("failing an import: %w", err)
	}
	return nil
}

// now is the worker's time, to the microsecond, as the database keeps it: a claim is
// compared to the one stored.
func (w *Worker) now() time.Time { return w.clk.Now().Truncate(time.Microsecond) }

// run reads the job's source from its step and cursor, page by page, until its end.
func (w *Worker) run(ctx context.Context, job *Job, log *slog.Logger) error {
	if job.Source != SourceAximote {
		return fmt.Errorf("unknown source %q", job.Source)
	}
	cutoff, err := w.store.ImportCutoff(ctx, job.AccountID, job.VehicleID, w.now())
	if err != nil {
		return fmt.Errorf("cutoff: %w", err)
	}
	vehicle, err := w.source.Vehicle(ctx, job.Token, job.SourceVehicle)
	if err != nil {
		return fmt.Errorf("%w: vehicle: %w", errSource, err)
	}
	// Only what starts before the cutoff is asked for: nothing is read that would be
	// left. The store reads it again at each page, and decides.
	q := aximote.Query{VehicleID: job.SourceVehicle, Until: cutoff, Cursor: job.Cursor, Limit: w.params.PageSize}
	for {
		page := Page{Step: job.Step}
		switch job.Step {
		case StepTrips:
			p, err := w.source.Trips(ctx, job.Token, q)
			if err != nil {
				return fmt.Errorf("%w: trips: %w", errSource, err)
			}
			page.Trips, page.Invalid = convertTrips(p.Items, job.Source, w.clk.Now())
			if err := w.addTracks(ctx, job.Token, page.Trips, log); err != nil {
				return err
			}
			if page.Cursor = p.NextCursor; page.Cursor == "" {
				page.Step = StepCharges
			}
		case StepCharges:
			p, err := w.source.Refuels(ctx, job.Token, q)
			if err != nil {
				return fmt.Errorf("%w: charges: %w", errSource, err)
			}
			page.Charges, page.Invalid = convertCharges(p.Items, job.Source, vehicle.BatteryCapacityKWh, w.clk.Now())
			page.Cursor = p.NextCursor
			page.Done = page.Cursor == ""
		default:
			return fmt.Errorf("unknown step %q", job.Step)
		}
		page.At = w.now()
		if _, err := w.store.SaveImportPage(ctx, *job, page); err != nil {
			return fmt.Errorf("saving a page: %w", err)
		}
		job.Step, job.Cursor, job.ClaimedAt, job.Failures = page.Step, page.Cursor, page.At, 0
		// The same bound for every page: a cursor is the query's.
		q.Cursor = page.Cursor
		if page.Done {
			log.InfoContext(ctx, "import done")
			return nil
		}
	}
}

// addTracks gives the trips their tracks, from one export of the source. A refused
// token or the source's limit stop the job; any other failure, a format not understood
// included, leaves the trips without their track.
func (w *Worker) addTracks(ctx context.Context, token string, trips []ImportedTrip, log *slog.Logger) error {
	if len(trips) == 0 {
		return nil
	}
	ids := make([]string, len(trips))
	for i, t := range trips {
		ids[i] = t.Trip.Import.V.SourceID
	}
	tracks, err := w.source.Tracks(ctx, token, ids)
	var apiErr *aximote.APIError
	switch {
	case errors.As(err, &apiErr) && (apiErr.TokenRefused() || apiErr.Kind == aximote.KindRateLimited):
		return fmt.Errorf("%w: tracks: %w", errSource, err)
	case err != nil && ctx.Err() != nil:
		return ctx.Err() //nolint:wrapcheck // the context's own
	case err != nil:
		log.WarnContext(ctx, "tracks not imported", "error", err)
		return nil
	}
	for i := range trips {
		trips[i].Track = tracks[trips[i].Trip.Import.V.SourceID]
	}
	return nil
}

// Run runs the jobs until ctx is canceled, then waits Idle while none is to run.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		next := time.Duration(0)
		worked, err := w.Once(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			w.log.ErrorContext(ctx, "import", "error", err)
			next = w.params.Idle
		case !worked:
			next = w.params.Idle
		}
		t.Reset(next)
	}
}
