package main

import (
	"context"
	"errors"
	"fmt"

	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/pleos"
	"runsten/internal/store"
)

// capacities gives the derivation the net capacity of each vehicle's variant, from the
// catalog (derive.Capacities): derive and core know nothing of the catalog.
type capacities struct {
	catalog *catalog.Catalog
	// vehicle returns the VIN of the account's vehicle, its connection's provider and
	// the variant its user chose (empty: none); found is false when it is gone.
	vehicle func(ctx context.Context, accountID, vehicleID string) (vin, provider, chosen string, found bool, err error)
}

var errNoVehicle = errors.New("no such vehicle")

// vehicleOf reads the VINs, providers and chosen variants from the store.
func vehicleOf(st *store.Store) func(ctx context.Context, accountID, vehicleID string) (string, string, string, bool, error) {
	return func(ctx context.Context, accountID, vehicleID string) (string, string, string, bool, error) {
		v, found, err := st.Vehicle(ctx, accountID, vehicleID)
		if err != nil {
			return "", "", "", false, fmt.Errorf("vehicle: %w", err)
		}
		return v.VIN, v.Provider, v.VariantID, found, nil
	}
}

// NetCapacity implements derive.Capacities. The VIN never changes for a vehicle, and a
// new choice of variant deletes its derivation cursor (store.SetVehicleModel): with the
// details of each snapshot, the capacity depends on the readings alone, as an incremental
// derivation and a rebuild require.
func (c capacities) NetCapacity(ctx context.Context, accountID, vehicleID string) (core.NetCapacity, error) {
	vin, provider, chosen, found, err := c.vehicle(ctx, accountID, vehicleID)
	switch {
	case err != nil:
		return nil, err
	case !found:
		return nil, errNoVehicle
	}
	return func(n core.Snapshot) (float64, bool) {
		v, ok := c.effective(chosen, vin, provider, n)
		if !ok || v.NetKWh == nil {
			return 0, false
		}
		return *v.NetKWh, true
	}, nil
}

// effective is the variant in effect of a vehicle of VIN vin whose user chose chosen
// (empty: none), from the details n: catalog.Effective, the one rule. A car of the Pleos
// Vehicle Data API keeps the brand its provider names, and a family the catalog knows no
// variant of (the consent may spell it otherwise than the catalog) gives way to the
// brand, as a family the details do not read.
func (c capacities) effective(chosen, vin, provider string, n core.Snapshot) (catalog.Variant, bool) {
	r := catalog.Reading{
		Brand: catalog.Brand(n.Brand.V), Family: n.Family.V, ModelYear: n.ModelYear.V, CapacityKWh: n.CapacityKWh.V, VIN: vin,
		Trim: n.Trim.V, Powertrain: catalog.Powertrain(n.Powertrain.V),
	}
	if b, ok := pleos.BrandOf(provider); ok {
		if r.Brand == "" {
			r.Brand = catalog.Brand(b)
		}
		if r.Family != "" && len(c.catalog.Offer(r)) == 0 {
			r.Family = ""
		}
	}
	return c.catalog.Effective(chosen, r)
}
