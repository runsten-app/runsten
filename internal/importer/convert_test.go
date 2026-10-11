package importer

import (
	"strings"
	"testing"
	"time"

	"runsten/internal/aximote"
	"runsten/internal/core"
)

func TestConvertTrip(t *testing.T) {
	start := time.Date(2026, 9, 8, 8, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	end := start.Add(30 * time.Minute)
	before := start.Add(-time.Minute)
	f := func(v float64) *float64 { return &v }
	s := func(v string) *string { return &v }
	valid := func() aximote.Trip {
		return aximote.Trip{
			ID: "t-1", StartTime: start, EndTime: &end, DistanceKm: f(12), EnergyConsumedKWh: f(-0.4),
			StartLocation: &aximote.Point{Latitude: 45, Longitude: 4, Locality: s(" Lyon "), Country: s("")},
			EndLocation:   &aximote.Point{Latitude: 45.1, Longitude: 4.1},
		}
	}
	trip, ok := convertTrip(valid(), "aximote", t0)
	if !ok || trip.Start.After.Location() != time.UTC || trip.Import.V.StartAddress != "Lyon" || trip.Import.V.EndAddress != "" ||
		trip.EnergyKWh.V != -0.4 || trip.DetectedAt != t0 {
		t.Errorf("%+v", trip)
	}
	if bare, ok := convertTrip(aximote.Trip{ID: "t", StartTime: start, EndTime: &end}, "aximote", t0); !ok ||
		bare.DistanceKm.OK || bare.EnergyKWh.OK || bare.From.OK || bare.To.OK {
		t.Errorf("without values: %+v", bare)
	}
	for name, change := range map[string]func(*aximote.Trip){
		"no end":        func(t *aximote.Trip) { t.EndTime = nil },
		"ends before":   func(t *aximote.Trip) { t.EndTime = &before },
		"an ID":         func(t *aximote.Trip) { t.ID = "a/b" },
		"an empty ID":   func(t *aximote.Trip) { t.ID = "" },
		"a distance":    func(t *aximote.Trip) { t.DistanceKm = f(-1) },
		"a latitude":    func(t *aximote.Trip) { t.StartLocation.Latitude = 91 },
		"a longitude":   func(t *aximote.Trip) { t.EndLocation.Longitude = -181 },
		"an address":    func(t *aximote.Trip) { t.StartLocation.Locality = s(strings.Repeat("é", 501)) },
		"invalid UTF-8": func(t *aximote.Trip) { t.EndLocation.Locality = s("\xff") },
	} {
		trip := valid()
		change(&trip)
		if _, ok := convertTrip(trip, "aximote", t0); ok {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestConvertCharge(t *testing.T) {
	start := time.Date(2026, 9, 8, 19, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	f := func(v float64) *float64 { return &v }
	s := func(v string) *string { return &v }
	valid := func() aximote.Refuel {
		return aximote.Refuel{
			ID: "c-1", Type: aximote.RefuelCharge, StartTime: start, EndTime: &end,
			StartEnergyKWh: f(20), EndEnergyKWh: f(60), EnergyChargedKWh: f(42), AvgChargingKW: f(21),
			CostAmount: f(1500), CostCurrency: s("ISK"), Location: &aximote.Point{Latitude: 64.1, Longitude: -21.9},
		}
	}
	capacity := f(80)
	c, ok := convertCharge(valid(), "aximote", capacity, t0)
	if !ok || c.StartSoC.V != 25 || c.EndSoC.V != 75 || c.EnergySoCKWh.V != 40 || c.PaidCost.V != (core.Amount{Currency: "ISK", Minor: 1500}) ||
		c.Type.OK || c.Position.V != (core.Position{Lat: 64.1, Lon: -21.9}) {
		t.Errorf("%+v", c)
	}

	// Without the capacity, or one of the energies: no state of charge, nor its energy.
	for name, change := range map[string]func(*aximote.Refuel, **float64){
		"no capacity":   func(_ *aximote.Refuel, c **float64) { *c = nil },
		"zero capacity": func(_ *aximote.Refuel, c **float64) { *c = f(0) },
		"no end energy": func(r *aximote.Refuel, _ **float64) { r.EndEnergyKWh = nil },
	} {
		r, capacity := valid(), f(80)
		change(&r, &capacity)
		c, ok := convertCharge(r, "aximote", capacity, t0)
		if !ok || c.StartSoC.OK || c.EndSoC.OK || c.EnergySoCKWh.OK || c.Capacity.OK || !c.EnergyPowerKWh.OK {
			t.Errorf("%s: %+v", name, c)
		}
	}

	// A currency Runsten does not know the digits of: no cost, the charge kept.
	r := valid()
	r.CostCurrency = s("JPY")
	if c, ok := convertCharge(r, "aximote", capacity, t0); !ok || c.PaidCost.OK {
		t.Errorf("JPY: %+v", c)
	}
	r = valid()
	r.CostCurrency, r.CostAmount = s("EUR"), f(4.365)
	if c, _ := convertCharge(r, "aximote", capacity, t0); c.PaidCost.V.Minor != 437 {
		t.Errorf("rounded: %+v", c.PaidCost)
	}

	before := start.Add(-time.Second)
	for name, change := range map[string]func(*aximote.Refuel){
		"in progress":  func(r *aximote.Refuel) { r.IsActive = true },
		"no end":       func(r *aximote.Refuel) { r.EndTime = nil },
		"ends before":  func(r *aximote.Refuel) { r.EndTime = &before },
		"another type": func(r *aximote.Refuel) { r.Type = "HYDROGEN" },
		"an ID":        func(r *aximote.Refuel) { r.ID = "c:1" },
		"an energy":    func(r *aximote.Refuel) { r.EnergyChargedKWh = f(-1) },
		"a power":      func(r *aximote.Refuel) { r.AvgChargingKW = f(-1) },
		"a cost":       func(r *aximote.Refuel) { r.CostAmount = f(-1) },
		"a SoC over":   func(r *aximote.Refuel) { r.EndEnergyKWh = f(81) },
		"a SoC under":  func(r *aximote.Refuel) { r.StartEnergyKWh = f(-1) },
		"a position":   func(r *aximote.Refuel) { r.Location.Latitude = -90.5 },
		"an address":   func(r *aximote.Refuel) { r.Location.Country = s(strings.Repeat("x", 501)) },
	} {
		r := valid()
		change(&r)
		if _, ok := convertCharge(r, "aximote", capacity, t0); ok {
			t.Errorf("%s accepted", name)
		}
	}

	charges, invalid := convertCharges([]aximote.Refuel{
		valid(), {ID: "f", Type: aximote.RefuelFuel, StartTime: start, EndTime: &end}, {ID: "x", Type: aximote.RefuelCharge, IsActive: true},
	}, "aximote", capacity, t0)
	if len(charges) != 1 || invalid != 1 {
		t.Errorf("%d charges, %d invalid: the fuel stop is not counted", len(charges), invalid)
	}
}
