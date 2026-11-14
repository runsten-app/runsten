package api

import (
	"context"
	"math"
	"time"

	"runsten/internal/core"
)

// pricing is what the costs of a vehicle's charges, and of its trips, depend on.
// charger is the vehicle's onboard charger (chargerOf).
func (s *Server) pricing(p PricedCharges, charger core.Value[float64]) core.Pricing {
	return core.Pricing{Currency: p.Currency, Places: p.Places, OnboardChargerKW: charger, Params: s.CostParams}
}

// tripCosts gives the cost of each of a vehicle's trips: its energy at the price of the
// kWh of the charges of the core.TripPriceWindow before it, whatever period the trips
// were read for. The charges are read apart, with what their costs depend on, entered
// costs attached over the vehicle's whole history. A plug-in hybrid has no cost per
// 100 km, as it has no consumption. l's NoCosts leaves every cost unknown, without a read.
func (s *Server) tripCosts(ctx context.Context, v Vehicle, trips []core.Trip, l AccountLimits) ([]core.Value[core.TripCost], error) {
	if l.NoCosts || len(trips) == 0 {
		return make([]core.Value[core.TripCost], len(trips)), nil
	}
	model, err := s.modelNow(ctx, v)
	if err != nil {
		return nil, err
	}
	return s.tripCostsOf(ctx, v, model, trips, l)
}

// tripCostsOf is tripCosts for the vehicle's model in effect, read already.
func (s *Server) tripCostsOf(ctx context.Context, v Vehicle, model vehicleModel, trips []core.Trip, l AccountLimits) ([]core.Value[core.TripCost], error) {
	out := make([]core.Value[core.TripCost], len(trips))
	if l.NoCosts || len(trips) == 0 {
		return out, nil
	}
	first, last := trips[0].Start.After, trips[0].Start.After
	for _, t := range trips {
		first, last = minTime(first, t.Start.After), maxTime(last, t.Start.After)
	}
	// Every charge that started in the windows: the lists keep those that end after
	// from, and TripCosts the ones that started in each trip's window.
	q := EventQuery{From: first.Add(-core.TripPriceWindow - time.Second), To: last}
	// The charges the account's history hides price nothing: it reads no more than it sees.
	if l.HistoryFrom.After(q.From) {
		q.From = l.HistoryFrom
	}
	if !q.From.Before(q.To) {
		return out, nil
	}
	p, err := s.Reader.ListCharges(ctx, sessionFrom(ctx).AccountID, v.ID, q)
	if err != nil {
		return nil, err //nolint:wrapcheck // reported by the caller, never shown
	}
	out = s.pricing(p, model.ChargerKW).TripCosts(trips, p.Charges, s.costs(p, model.ChargerKW))
	if model.Powertrain.V == core.PlugInHybrid {
		for i := range out {
			out[i].V.Per100km = core.Value[core.MinorRange]{}
		}
	}
	return out, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// pricePerKWhJSON bounds a price per kWh, in major units of the currency.
type pricePerKWhJSON struct {
	Min float64 `json:"min" minimum:"0" doc:"The lowest price of a kWh, in major units of the currency (euros), rounded down to 5 decimals."`
	Max float64 `json:"max" minimum:"0" doc:"The highest price of a kWh, rounded up to 5 decimals."`
}

// priceDigits are the decimals of a price per kWh, as a tariff's.
const priceDigits = 5

// pricePerKWhOf rounds the bounds as round does an amount's: lo down and hi up, one
// price to the nearest.
func pricePerKWhOf(lo, hi float64) pricePerKWhJSON {
	scale := math.Pow10(priceDigits)
	if hi-lo < 1e-12 {
		p := math.Round(lo*scale) / scale
		return pricePerKWhJSON{Min: p, Max: p}
	}
	return pricePerKWhJSON{Min: math.Floor(lo*scale+1e-9) / scale, Max: math.Ceil(hi*scale-1e-9) / scale}
}

type priceShareJSON struct {
	Place       null[placeRefJSON]   `json:"place" doc:"The place of these charges (charges.place); null for the charges at no place."`
	Type        null[chargeTypeJSON] `json:"type" doc:"Outside every place, the type of these charges: ac (those of unknown type included, as their efficiency assumes) or dc. null at a place."`
	EnergyKWh   float64              `json:"energy_kwh" exclusiveMinimum:"0" doc:"The energy these charges added to the battery."`
	PricePerKWh pricePerKWhJSON      `json:"price_per_kwh" doc:"What these charges cost over the energy they added."`
}

type kwhPriceJSON struct {
	WindowStart time.Time        `json:"window_start" pattern:"Z$" doc:"The start of the window of the charges: 30 days before the trip's start.after, which ends it."`
	PricePerKWh pricePerKWhJSON  `json:"price_per_kwh" doc:"The average price of the kWh that reached the battery over the window: the costs of its charges over the energy they added, both on the battery's side."`
	EnergyKWh   float64          `json:"energy_kwh" exclusiveMinimum:"0" doc:"The energy the charges added to the battery: energy_measured_kwh, else energy_soc_kwh (energy_power_kwh for a rise of the state of charge below 2 points), as their costs bill."`
	Charges     int              `json:"charges" minimum:"1" doc:"The charges of the window the price rests on: those with both a cost (charges.cost, entered and imported ones included) and an energy."`
	LeftOut     int              `json:"left_out" minimum:"0" doc:"The charges of the window left out, their cost or their energy unknown."`
	Shares      []priceShareJSON `json:"shares" nullable:"false" minItems:"1" doc:"Where energy_kwh was charged: by place, in the order of the places, then outside every place on AC and on DC; none empty."`
}

type tripCostJSON struct {
	Currency currencyCodeJSON  `json:"currency" doc:"The account's currency, the unit of the _minor fields."`
	MinMinor int64             `json:"min_minor" minimum:"0" doc:"The lowest amount the trip's energy may have cost, in minor units of the currency: energy_kwh at the lowest price per kWh, rounded down. 0 for a trip that gained energy."`
	MaxMinor int64             `json:"max_minor" minimum:"0" doc:"The highest amount, at the highest price per kWh, rounded up. Equal to min_minor when the charges' costs were exact."`
	Per100km null[costSumJSON] `json:"per_100km" doc:"The cost over the trip's distance, for 100 km, in minor units: the lowest rounded down, the highest up. null without a positive distance, and for a plug-in hybrid (the vehicle's model.powertrain), whose trips' energy is the battery's only."`
	Price    kwhPriceJSON      `json:"price" doc:"The price per kWh the cost rests on, and the charges behind it."`
}

func tripCostOf(c core.TripCost) tripCostJSON {
	p := c.Price
	shares := make([]priceShareJSON, len(p.Shares))
	for i, s := range p.Shares {
		shares[i] = priceShareJSON{
			Place: nullOf(s.Place, placeRefOf), EnergyKWh: s.EnergyKWh,
			PricePerKWh: pricePerKWhOf(s.Min/s.EnergyKWh, s.Max/s.EnergyKWh),
		}
		if !s.Place.OK {
			shares[i].Type = known(chargeTypeJSON(s.Type))
		}
	}
	return tripCostJSON{
		Currency: currencyCodeJSON(c.Currency.Code), MinMinor: c.Min, MaxMinor: c.Max,
		Per100km: nullOf(c.Per100km, func(r core.MinorRange) costSumJSON { return costSumJSON{MinMinor: r.Min, MaxMinor: r.Max} }),
		Price: kwhPriceJSON{
			WindowStart: p.From.UTC(), PricePerKWh: pricePerKWhOf(p.Min, p.Max), EnergyKWh: p.EnergyKWh,
			Charges: p.Charges, LeftOut: p.LeftOut, Shares: shares,
		},
	}
}
