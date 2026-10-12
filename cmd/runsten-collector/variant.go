package main

import (
	"context"
	"errors"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
)

// variants writes each vehicle's variant in effect after its derivation, a copy for the
// readers that have only SQL (store.SetVariantInEffect). The catalog is embedded in the
// binary: the variant in effect changes only with the details, the user's choice or a
// new binary, all of which the vehicle's next poll follows.
type variants struct {
	collector.Deriver
	capacities
	// current returns the vehicle's latest known values (derive.Deriver.Current).
	current func(ctx context.Context, accountID, vehicleID string, at time.Time) (core.Current, error)
	// set writes the copy (store.Store.SetVariantInEffect).
	set func(ctx context.Context, accountID, vehicleID, variantID string) error
	clk clock.Clock
}

// Update implements collector.Deriver: the derivation, then the variant in effect,
// written even when the derivation failed.
func (v variants) Update(ctx context.Context, accountID, vehicleID string) error {
	return errors.Join(v.Deriver.Update(ctx, accountID, vehicleID), v.write(ctx, accountID, vehicleID))
}

func (v variants) write(ctx context.Context, accountID, vehicleID string) error {
	vin, chosen, found, err := v.vehicle(ctx, accountID, vehicleID)
	if err != nil || !found {
		return err
	}
	cur, err := v.current(ctx, accountID, vehicleID, v.clk.Now())
	if err != nil {
		return err //nolint:wrapcheck // the deriver's, which says what failed
	}
	var id string
	if variant, ok := v.effective(chosen, vin, cur.Snapshot); ok {
		id = variant.ID
	}
	return v.set(ctx, accountID, vehicleID, id)
}
