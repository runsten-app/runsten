package main

import (
	"context"
	"errors"
	"fmt"

	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/store"
)

// capacities gives the derivation the net capacity of each vehicle's variant, from the
// catalog (derive.Capacities): derive and core know nothing of the catalog.
type capacities struct {
	catalog *catalog.Catalog
	// vehicle returns the VIN of the account's vehicle and the variant its user chose
	// (empty: none); found is false when it is gone.
	vehicle func(ctx context.Context, accountID, vehicleID string) (vin, chosen string, found bool, err error)
}

var errNoVehicle = errors.New("no such vehicle")

// vehicleOf reads the VINs and the chosen variants from the store.
func vehicleOf(st *store.Store) func(ctx context.Context, accountID, vehicleID string) (string, string, bool, error) {
	return func(ctx context.Context, accountID, vehicleID string) (string, string, bool, error) {
		v, found, err := st.Vehicle(ctx, accountID, vehicleID)
		if err != nil {
			return "", "", false, fmt.Errorf("vehicle: %w", err)
		}
		return v.VIN, v.VariantID, found, nil
	}
}

// NetCapacity implements derive.Capacities. The VIN never changes for a vehicle, and a
// new choice of variant deletes its derivation cursor (store.SetVehicleModel): with the
// details of each snapshot, the capacity depends on the readings alone, as an incremental
// derivation and a rebuild require.
func (c capacities) NetCapacity(ctx context.Context, accountID, vehicleID string) (core.NetCapacity, error) {
	vin, chosen, found, err := c.vehicle(ctx, accountID, vehicleID)
	switch {
	case err != nil:
		return nil, err
	case !found:
		return nil, errNoVehicle
	}
	return func(n core.Snapshot) (float64, bool) {
		v, ok := c.catalog.Effective(chosen, catalog.Reading{
			Family: n.Family.V, ModelYear: n.ModelYear.V, CapacityKWh: n.CapacityKWh.V, VIN: vin,
			BatteryElectric: n.BatteryElectric.OK && n.BatteryElectric.V,
		})
		if !ok || v.NetKWh == nil {
			return 0, false
		}
		return *v.NetKWh, true
	}, nil
}
