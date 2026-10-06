// Package geocode gives addresses to the positions of the accounts: its worker claims the
// cells the API asked for, one at a time, asks a reverse geocoder for each, and writes
// the answer. It knows neither HTTP nor the database: it declares its Store and its
// Geocoder, which the binary wires.
package geocode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
)

// Claim is a cell of an account the worker is to resolve.
type Claim struct {
	AccountID string
	Cell      core.GeoCell
}

// Store keeps the cells and their addresses.
type Store interface {
	// ClaimGeocoding claims the oldest unresolved cell of any account whose previous
	// claim is older than lease, attempted fewer than maxAttempts times; false when
	// there is none.
	ClaimGeocoding(ctx context.Context, at time.Time, lease time.Duration, maxAttempts int) (Claim, bool, error)
	// ResolveAddress writes the address of a claimed cell, within its account; empty
	// when the geocoder knows none.
	ResolveAddress(ctx context.Context, c Claim, address string, at time.Time) error
}

// Geocoder gives the address of a position: empty when it knows none, an error when it
// could not answer (the cell is then retried).
type Geocoder interface {
	Reverse(ctx context.Context, p core.Position) (string, error)
}

// Params are the worker's pace and retries.
type Params struct {
	// Interval separates two requests to the geocoder: at most one per interval, the
	// pace most providers ask for (Nominatim's: one per second).
	Interval time.Duration
	// Idle is the wait when no cell is to be resolved.
	Idle time.Duration
	// Lease is how long a claim holds: a failed request is retried after it.
	Lease time.Duration
	// MaxAttempts are the requests of a cell before giving up on it.
	MaxAttempts int
}

// DefaultParams suit Nominatim's usage policy.
func DefaultParams() Params {
	return Params{Interval: time.Second, Idle: 5 * time.Second, Lease: 10 * time.Minute, MaxAttempts: 5}
}

// Worker resolves the cells.
type Worker struct {
	store    Store
	geocoder Geocoder
	clk      clock.Clock
	params   Params
	log      *slog.Logger
}

// New creates the worker.
func New(store Store, geocoder Geocoder, clk clock.Clock, params Params, log *slog.Logger) *Worker {
	return &Worker{store: store, geocoder: geocoder, clk: clk, params: params, log: log}
}

// Once resolves one cell, if one is to be: false when there was none. A geocoder's
// failure leaves the cell to its next claim, and is no error of Once.
func (w *Worker) Once(ctx context.Context) (bool, error) {
	c, ok, err := w.store.ClaimGeocoding(ctx, w.clk.Now(), w.params.Lease, w.params.MaxAttempts)
	if err != nil || !ok {
		return false, err //nolint:wrapcheck // Store error, already has context
	}
	address, err := w.geocoder.Reverse(ctx, c.Cell.Center())
	if err != nil {
		// Neither the position nor the account in the logs: where a user was.
		w.log.WarnContext(ctx, "reverse geocoding failed, retried later", "error", err)
		return true, nil
	}
	if err := w.store.ResolveAddress(ctx, c, address, w.clk.Now()); err != nil {
		return true, fmt.Errorf("saving an address: %w", err)
	}
	return true, nil
}

// Run resolves the cells until ctx is canceled: one per Interval, then every Idle while
// none is to be.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		next := w.params.Interval
		worked, err := w.Once(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			w.log.ErrorContext(ctx, "geocoding", "error", err)
			next = w.params.Idle
		case !worked:
			next = w.params.Idle
		}
		t.Reset(next)
	}
}
