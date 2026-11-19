package main

import (
	"context"
	"fmt"
	"time"

	"runsten/internal/core"
	"runsten/internal/demo"
	"runsten/internal/store"
)

// demoVehicle is the demo vehicle for the API (api.DemoVehicle): its readings of the
// weeks before connection, written to the store as the collector would have saved them,
// then its trips and charges derived from them from scratch.
type demoVehicle struct {
	week    *demo.Demo
	store   historyStore
	deriver rebuilder
}

// historyStore saves a vehicle's past responses.
type historyStore interface {
	SaveHistory(ctx context.Context, accountID, vehicleID string, snaps []store.HistoricSnapshot) error
}

// rebuilder derives a vehicle's events from its whole history, on the net capacity given:
// runsten-api's deriver has no catalog of its own to know it.
type rebuilder interface {
	RebuildWith(ctx context.Context, accountID, vehicleID string, net core.NetCapacity) (core.Result, error)
}

// VIN implements api.DemoVehicle.
func (demoVehicle) VIN() string { return demo.VIN }

// Backfill implements api.DemoVehicle. The collector may read the vehicle meanwhile, its
// first reading at to or after: the history ends at to, and a snapshot already stored at a
// time of it is left alone.
func (d demoVehicle) Backfill(ctx context.Context, accountID, vehicleID string, to time.Time, net core.NetCapacity) error {
	samples, err := d.week.History(to.Add(-demo.HistoryDays*24*time.Hour), to)
	if err != nil {
		return fmt.Errorf("demo history: %w", err)
	}
	kind := core.Kind{Source: demo.Source, Endpoint: string(demo.Vehicle)}
	snaps := make([]store.HistoricSnapshot, len(samples))
	for i, s := range samples {
		snaps[i] = store.HistoricSnapshot{Kind: kind, FetchedAt: s.At, Payload: s.Payload, Hash: s.Hash}
	}
	if err := d.store.SaveHistory(ctx, accountID, vehicleID, snaps); err != nil {
		return fmt.Errorf("demo history: %w", err)
	}
	if _, err := d.deriver.RebuildWith(ctx, accountID, vehicleID, net); err != nil {
		return fmt.Errorf("demo history derived: %w", err)
	}
	return nil
}
