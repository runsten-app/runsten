// Package derive keeps each vehicle's trips and charges up to date from its stored
// snapshots: incrementally after a collector pass, or rebuilt from the whole history.
//
// Both paths run the same pure detection (core.Derive) and replace the events they
// recompute, in one transaction: running either any number of times never duplicates
// an event.
package derive

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"runsten/internal/core"
	"runsten/internal/volvo"
)

// Snapshot is a stored raw response.
type Snapshot struct {
	Endpoint  volvo.Endpoint
	FetchedAt time.Time
	CheckedAt time.Time
	Payload   []byte
}

// Store reads the snapshots and stores the derived events.
type Store interface {
	// DerivationCursor returns where the vehicle's derivation resumes; zero if it never ran.
	DerivationCursor(ctx context.Context, accountID, vehicleID string) (core.Cursor, error)
	// SnapshotsSince returns the snapshots of endpoints fetched after from, and the latest
	// one of each endpoint fetched at or before it (the one still valid at from).
	SnapshotsSince(ctx context.Context, accountID, vehicleID string, from time.Time, endpoints []volvo.Endpoint) ([]Snapshot, error)
	// SnapshotsBetween returns, oldest first and at most limit of them, the snapshots of
	// endpoints read within [from, to]: fetched in it, or fetched before and read again,
	// unchanged, at from or later.
	SnapshotsBetween(ctx context.Context, accountID, vehicleID string, from, to time.Time, endpoints []volvo.Endpoint, limit int) ([]Snapshot, error)
	// SaveDerivation replaces the events detected after the given time with those of res
	// and records res.Cursor, atomically.
	SaveDerivation(ctx context.Context, accountID, vehicleID string, after time.Time, res core.Result) error
}

// Capacities gives the net battery capacity of each vehicle, for core.CapacityCatalogNet.
// derive does not know where it comes from (the catalog of the variants): the binary
// provides it.
type Capacities interface {
	// NetCapacity returns the vehicle's; nil when none can be known.
	NetCapacity(ctx context.Context, accountID, vehicleID string) (core.NetCapacity, error)
}

// Deriver derives the events of a vehicle.
type Deriver struct {
	st   Store
	p    core.Params
	caps Capacities
	log  *slog.Logger
}

// New creates a Deriver. Without caps, every energy rests on the vendor's capacity: a
// Deriver that only reads the current state needs none.
func New(st Store, p core.Params, caps Capacities, log *slog.Logger) *Deriver {
	return &Deriver{st: st, p: p, caps: caps, log: log}
}

// Update derives the events detected since the vehicle's cursor.
func (d *Deriver) Update(ctx context.Context, accountID, vehicleID string) error {
	c, err := d.st.DerivationCursor(ctx, accountID, vehicleID)
	if err != nil {
		return fmt.Errorf("derivation cursor: %w", err)
	}
	_, err = d.run(ctx, accountID, vehicleID, c)
	return err
}

// Rebuild derives the vehicle's events from its whole history, replacing all of them.
func (d *Deriver) Rebuild(ctx context.Context, accountID, vehicleID string) (core.Result, error) {
	return d.run(ctx, accountID, vehicleID, core.Cursor{})
}

// Current returns the vehicle's latest known values, from the latest snapshot of each
// endpoint fetched at or before at.
func (d *Deriver) Current(ctx context.Context, accountID, vehicleID string, at time.Time) (core.Current, error) {
	records, err := d.records(ctx, accountID, vehicleID, at)
	if err != nil {
		return core.Current{}, err
	}
	// Those fetched after at are returned too: at is in the past.
	records = slices.DeleteFunc(records, func(r core.Record) bool { return r.FetchedAt.After(at) })
	return core.Latest(records), nil
}

// Readings returns, oldest first and at most limit of them, the vehicle's energy states
// (its state of charge, range and charging power) read within [from, to]: the records
// of one endpoint, which follow each other, each valid from its FetchedAt to its
// CheckedAt.
func (d *Deriver) Readings(ctx context.Context, accountID, vehicleID string, from, to time.Time, limit int) ([]core.Record, error) {
	snaps, err := d.st.SnapshotsBetween(ctx, accountID, vehicleID, from, to, []volvo.Endpoint{volvo.EnergyState}, limit)
	if err != nil {
		return nil, fmt.Errorf("snapshots: %w", err)
	}
	return d.normalize(vehicleID, snaps), nil
}

func (d *Deriver) run(ctx context.Context, accountID, vehicleID string, c core.Cursor) (core.Result, error) {
	records, err := d.records(ctx, accountID, vehicleID, c.From)
	if err != nil {
		return core.Result{}, err
	}
	var net core.NetCapacity
	if d.caps != nil {
		if net, err = d.caps.NetCapacity(ctx, accountID, vehicleID); err != nil {
			return core.Result{}, fmt.Errorf("net capacity: %w", err)
		}
	}
	res := core.Derive(records, c, d.p, net)
	if err := d.st.SaveDerivation(ctx, accountID, vehicleID, c.Settled, res); err != nil {
		return core.Result{}, fmt.Errorf("save events: %w", err)
	}
	return res, nil
}

// records returns the normalized snapshots fetched after from, and the latest one of
// each endpoint fetched at or before it.
func (d *Deriver) records(ctx context.Context, accountID, vehicleID string, from time.Time) ([]core.Record, error) {
	snaps, err := d.st.SnapshotsSince(ctx, accountID, vehicleID, from, volvo.NormalizedEndpoints())
	if err != nil {
		return nil, fmt.Errorf("snapshots: %w", err)
	}
	return d.normalize(vehicleID, snaps), nil
}

// normalize reads the stored responses into records.
func (d *Deriver) normalize(vehicleID string, snaps []Snapshot) []core.Record {
	records := make([]core.Record, 0, len(snaps))
	for _, s := range snaps {
		n, err := volvo.Normalize(s.Endpoint, s.Payload)
		if err != nil {
			// Stored responses are valid JSON; an unreadable one is skipped, not fatal.
			d.log.Warn("unreadable snapshot", "vehicle", vehicleID, "endpoint", s.Endpoint, "fetched_at", s.FetchedAt, "err", err)
			continue
		}
		records = append(records, core.Record{FetchedAt: s.FetchedAt, CheckedAt: s.CheckedAt, Snapshot: n})
	}
	return records
}
