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
)

// Snapshot is a stored raw response.
type Snapshot struct {
	Kind      core.Kind
	FetchedAt time.Time
	CheckedAt time.Time
	Payload   []byte
}

// Store reads the snapshots and stores the derived events.
type Store interface {
	// DerivationCursor returns where the vehicle's derivation resumes; zero if it never ran.
	DerivationCursor(ctx context.Context, accountID, vehicleID string) (core.Cursor, error)
	// SnapshotsSince returns the snapshots of the kinds fetched after from, and the latest
	// one of each kind fetched at or before it (the one still valid at from).
	SnapshotsSince(ctx context.Context, accountID, vehicleID string, from time.Time, kinds []core.Kind) ([]Snapshot, error)
	// SnapshotsBetween returns, oldest first and at most limit of them, the snapshots of
	// the kinds read within [from, to]: fetched in it, or fetched before and read again,
	// unchanged, at from or later.
	SnapshotsBetween(ctx context.Context, accountID, vehicleID string, from, to time.Time, kinds []core.Kind, limit int) ([]Snapshot, error)
	// SaveDerivation replaces the events detected after the given time with those of res
	// and records res.Cursor, atomically.
	SaveDerivation(ctx context.Context, accountID, vehicleID string, after time.Time, res core.Result) error
}

// Normalizer reads the stored responses of the sources it knows into snapshots. derive
// does not know their formats: the binary provides one per source (Sources).
type Normalizer interface {
	// Kinds returns the kinds of response the derivation reads.
	Kinds() []core.Kind
	// EnergyKinds returns those holding the energy state: the state of charge, the range
	// and the charging power.
	EnergyKinds() []core.Kind
	// Normalize reads a stored response of kind k.
	Normalize(k core.Kind, raw []byte) (core.Snapshot, error)
	// HealthKinds returns the kinds of response holding the vehicle's health: its tyres,
	// its service and its warnings, which the derivation does not read.
	HealthKinds() []core.Kind
	// NormalizeHealth reads a stored response of kind k into a health reading.
	NormalizeHealth(k core.Kind, raw []byte) (core.Health, error)
}

// Sources returns a Normalizer of the responses of every source norms knows, each read
// by the one whose kinds hold its source (core.Kind.Source): a vehicle read through
// several sources, or another than Volvo, is derived from all its readings. A source
// two of them know is read by the first.
func Sources(norms ...Normalizer) Normalizer {
	s := sources{norms: norms, bySource: map[string]int{}}
	for i, n := range norms {
		for _, k := range n.Kinds() {
			if _, ok := s.bySource[k.Source]; !ok {
				s.bySource[k.Source] = i
			}
		}
	}
	return s
}

type sources struct {
	norms    []Normalizer
	bySource map[string]int // the index of the one that reads it, by source
}

func (s sources) Kinds() []core.Kind { return s.kinds(Normalizer.Kinds) }

func (s sources) EnergyKinds() []core.Kind { return s.kinds(Normalizer.EnergyKinds) }

func (s sources) HealthKinds() []core.Kind { return s.kinds(Normalizer.HealthKinds) }

func (s sources) kinds(of func(Normalizer) []core.Kind) []core.Kind {
	var out []core.Kind
	for i, n := range s.norms {
		for _, k := range of(n) {
			if s.bySource[k.Source] == i {
				out = append(out, k)
			}
		}
	}
	return out
}

func (s sources) Normalize(k core.Kind, raw []byte) (core.Snapshot, error) {
	i, ok := s.bySource[k.Source]
	if !ok {
		return core.Snapshot{}, fmt.Errorf("normalize: unknown source %q", k.Source)
	}
	return s.norms[i].Normalize(k, raw) //nolint:wrapcheck // the source's, which names it
}

func (s sources) NormalizeHealth(k core.Kind, raw []byte) (core.Health, error) {
	i, ok := s.bySource[k.Source]
	if !ok {
		return core.Health{}, fmt.Errorf("normalize health: unknown source %q", k.Source)
	}
	return s.norms[i].NormalizeHealth(k, raw) //nolint:wrapcheck // the source's, which names it
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
	norm Normalizer
	p    core.Params
	caps Capacities
	log  *slog.Logger
}

// New creates a Deriver reading the snapshots norm knows. Without caps, every energy
// rests on the vendor's capacity: a Deriver that only reads the current state needs none.
func New(st Store, norm Normalizer, p core.Params, caps Capacities, log *slog.Logger) *Deriver {
	return &Deriver{st: st, norm: norm, p: p, caps: caps, log: log}
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

// InProgress returns the charge still open at the vehicle's latest reading, if any: the
// derivation replayed from the vehicle's cursor, as Update would, nothing saved. net is
// the vehicle's net capacity, as Capacities would give it; nil when it is unknown.
func (d *Deriver) InProgress(ctx context.Context, accountID, vehicleID string, net core.NetCapacity) (core.Value[core.ChargeInProgress], error) {
	c, err := d.st.DerivationCursor(ctx, accountID, vehicleID)
	if err != nil {
		return core.Value[core.ChargeInProgress]{}, fmt.Errorf("derivation cursor: %w", err)
	}
	records, err := d.records(ctx, accountID, vehicleID, c.From)
	if err != nil {
		return core.Value[core.ChargeInProgress]{}, err
	}
	return core.Derive(records, c, d.p, net).InProgress, nil
}

// Readings returns, oldest first and at most limit of them, the vehicle's energy states
// (its state of charge, range and charging power) read within [from, to]: the records
// of each kind follow each other, each valid from its FetchedAt to its CheckedAt.
func (d *Deriver) Readings(ctx context.Context, accountID, vehicleID string, from, to time.Time, limit int) ([]core.Record, error) {
	snaps, err := d.st.SnapshotsBetween(ctx, accountID, vehicleID, from, to, d.norm.EnergyKinds(), limit)
	if err != nil {
		return nil, fmt.Errorf("snapshots: %w", err)
	}
	return d.normalize(vehicleID, snaps), nil
}

// Health returns the vehicle's latest known health, from the latest response of each
// of its kinds fetched at or before at.
func (d *Deriver) Health(ctx context.Context, accountID, vehicleID string, at time.Time) (core.CurrentHealth, error) {
	snaps, err := d.st.SnapshotsSince(ctx, accountID, vehicleID, at, d.norm.HealthKinds())
	if err != nil {
		return core.CurrentHealth{}, fmt.Errorf("snapshots: %w", err)
	}
	// Those fetched after at are returned too: at is in the past.
	snaps = slices.DeleteFunc(snaps, func(s Snapshot) bool { return s.FetchedAt.After(at) })
	return core.LatestHealth(d.normalizeHealth(vehicleID, snaps)), nil
}

// HealthReadings returns, oldest first and at most limit of them, the vehicle's health
// read within [from, to], each record valid from its FetchedAt to its CheckedAt.
func (d *Deriver) HealthReadings(ctx context.Context, accountID, vehicleID string, from, to time.Time, limit int) ([]core.HealthRecord, error) {
	snaps, err := d.st.SnapshotsBetween(ctx, accountID, vehicleID, from, to, d.norm.HealthKinds(), limit)
	if err != nil {
		return nil, fmt.Errorf("snapshots: %w", err)
	}
	return d.normalizeHealth(vehicleID, snaps), nil
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
// each kind fetched at or before it.
func (d *Deriver) records(ctx context.Context, accountID, vehicleID string, from time.Time) ([]core.Record, error) {
	snaps, err := d.st.SnapshotsSince(ctx, accountID, vehicleID, from, d.norm.Kinds())
	if err != nil {
		return nil, fmt.Errorf("snapshots: %w", err)
	}
	return d.normalize(vehicleID, snaps), nil
}

// normalize reads the stored responses into records.
func (d *Deriver) normalize(vehicleID string, snaps []Snapshot) []core.Record {
	records := make([]core.Record, 0, len(snaps))
	for _, s := range snaps {
		n, err := d.norm.Normalize(s.Kind, s.Payload)
		if err != nil {
			// Stored responses are valid JSON; an unreadable one is skipped, not fatal.
			d.log.Warn("unreadable snapshot", "vehicle", vehicleID, "source", s.Kind.Source, "endpoint", s.Kind.Endpoint,
				"fetched_at", s.FetchedAt, "err", err)
			continue
		}
		records = append(records, core.Record{Source: s.Kind.Source, FetchedAt: s.FetchedAt, CheckedAt: s.CheckedAt, Snapshot: n})
	}
	return records
}

// normalizeHealth reads the stored responses into health records.
func (d *Deriver) normalizeHealth(vehicleID string, snaps []Snapshot) []core.HealthRecord {
	records := make([]core.HealthRecord, 0, len(snaps))
	for _, s := range snaps {
		h, err := d.norm.NormalizeHealth(s.Kind, s.Payload)
		if err != nil {
			d.log.Warn("unreadable snapshot", "vehicle", vehicleID, "source", s.Kind.Source, "endpoint", s.Kind.Endpoint,
				"fetched_at", s.FetchedAt, "err", err)
			continue
		}
		records = append(records, core.HealthRecord{Source: s.Kind.Source, FetchedAt: s.FetchedAt, CheckedAt: s.CheckedAt, Health: h})
	}
	return records
}
