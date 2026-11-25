package main

import (
	"context"

	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/publish"
)

// models tells the MQTT publisher what each vehicle is, from the catalog, for its device
// in Home Assistant: the variant in effect, as the API names it, else the family.
type models capacities

// brands writes the catalog's brands as their makers do.
var brands = map[catalog.Brand]string{
	catalog.Volvo: "Volvo", catalog.Polestar: "Polestar", catalog.Tesla: "Tesla", catalog.BMW: "BMW", catalog.Mini: "MINI",
	catalog.Hyundai: "Hyundai", catalog.Kia: "Kia", catalog.Genesis: "Genesis",
}

// Model implements mqtt.Models.
func (m models) Model(ctx context.Context, accountID, vehicleID string, c core.Current) (publish.Model, error) {
	row, found, err := m.vehicle(ctx, accountID, vehicleID)
	switch {
	case err != nil:
		return publish.Model{}, err
	case !found:
		return publish.Model{}, errNoVehicle
	}
	n := c.Snapshot
	out := publish.Model{}
	if n.Family.OK {
		out.Family = n.Family.V
	}
	if n.ModelYear.OK {
		out.Year = n.ModelYear.V
	}
	// The alias its user gave it in Runsten outranks the name given in the vendor's app.
	if n.Sheet.OK {
		out.Name = n.Sheet.V.Name
	}
	if row.Alias != "" {
		out.Name = row.Alias
	}
	if v, ok := capacities(m).effective(row.Chosen, row.VIN, row.Provider, n); ok {
		out.Variant, out.Brand = v.Name, brands[v.Brand]
	}
	return out, nil
}
