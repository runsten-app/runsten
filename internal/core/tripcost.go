package core

import (
	"math"
	"time"
)

// TripPriceWindow is how far before a trip the charges that price its kWh go: a trip
// keeps its cost whatever period it is read in.
const TripPriceWindow = 30 * 24 * time.Hour

// MinorRange is an amount known by bounds, in minor units: every amount the bounds allow
// lies in [Min, Max].
type MinorRange struct {
	Min, Max int64
}

// PriceShare is the part of a KWhPrice's energy charged at one place, or elsewhere on one
// type of charger, with what it cost.
type PriceShare struct {
	// Place holds the charges PlaceOf puts there; without it, those at no place of Type.
	Place Value[Place]
	// Type is AC or DC, outside every place. Assumption: a charge of unknown type (a
	// reconstructed one) charged on AC, as its efficiency assumes.
	Type      ChargeType
	EnergyKWh float64
	// Min and Max are what the share's charges cost, in major units of the currency.
	Min, Max float64
}

// KWhPrice is the average price of the kWh that reached the battery over a window: the
// cost of the charges that started in it, over the energy they added (chargeEnergy),
// both on the battery's side. Charges without a cost, or without an energy, stay out.
type KWhPrice struct {
	Currency Currency
	From, To time.Time
	// Min and Max bound the price of a kWh, in major units of the currency: the charges'
	// costs are ranges.
	Min, Max  float64
	EnergyKWh float64
	// Charges are the charges it rests on; LeftOut those of the window it could not take,
	// their cost or their energy unknown.
	Charges, LeftOut int
	// Shares split EnergyKWh by where it was charged: the places in the order of the
	// Pricing's, then AC and DC elsewhere; none empty.
	Shares []PriceShare
}

// TripCost is what a trip's energy cost, at the price of the kWh of the charges of the
// TripPriceWindow before it.
type TripCost struct {
	Currency Currency
	// Min and Max are in minor units, rounded as a charge's cost (round).
	Min, Max int64
	// Per100km is the cost over the trip's distance, unknown without a positive one.
	Per100km Value[MinorRange]
	Price    KWhPrice
}

// KWhPrice is the price of the kWh over the charges that started in [from, to); costs[i]
// is the cost of charges[i], as ChargeCost gives it. Unknown without a currency, and
// when no charge of the window has both a cost and a positive energy: never zero.
func (pr Pricing) KWhPrice(charges []Charge, costs []Value[Cost], from, to time.Time) Value[KWhPrice] {
	if !pr.Currency.OK {
		return Value[KWhPrice]{}
	}
	p := KWhPrice{Currency: pr.Currency.V, From: from, To: to}
	scale := math.Pow10(pr.Currency.V.MinorDigits)
	shares := make([]PriceShare, len(pr.Places)+2) // the places, then AC and DC elsewhere
	for i := range pr.Places {
		shares[i].Place = known(pr.Places[i])
	}
	shares[len(pr.Places)].Type, shares[len(pr.Places)+1].Type = AC, DC
	for i, c := range charges {
		if !in(c.Start.After, from, to) {
			continue
		}
		var cost Value[Cost]
		if i < len(costs) {
			cost = costs[i]
		}
		energy := pr.chargeEnergy(c)
		if !cost.OK || !energy.OK || energy.V <= 0 {
			p.LeftOut++
			continue
		}
		p.Charges++
		share := &shares[len(pr.Places)]
		switch j := placeIndex(c, pr.Places); {
		case j >= 0:
			share = &shares[j]
		case isDC(c):
			share = &shares[len(pr.Places)+1]
		}
		lo, hi := float64(cost.V.Min)/scale, float64(cost.V.Max)/scale
		share.EnergyKWh += energy.V
		share.Min += lo
		share.Max += hi
		p.EnergyKWh += energy.V
		p.Min += lo
		p.Max += hi
	}
	if p.Charges == 0 {
		return Value[KWhPrice]{}
	}
	p.Min /= p.EnergyKWh
	p.Max /= p.EnergyKWh
	for _, s := range shares {
		if s.EnergyKWh > 0 {
			p.Shares = append(p.Shares, s)
		}
	}
	return known(p)
}

// TripCosts gives the cost of each trip: its energy at the KWhPrice of the charges that
// started in the TripPriceWindow before its Start.After, a trip that gained energy
// costing nothing. costs[i] is the cost of charges[i]. Unknown when the price is, or
// the trip's energy.
func (pr Pricing) TripCosts(trips []Trip, charges []Charge, costs []Value[Cost]) []Value[TripCost] {
	out := make([]Value[TripCost], len(trips))
	for i, t := range trips {
		if !t.EnergyKWh.OK {
			continue
		}
		at := t.Start.After
		price := pr.KWhPrice(charges, costs, at.Add(-TripPriceWindow), at)
		if !price.OK {
			continue
		}
		kwh := max(0, t.EnergyKWh.V)
		lo, hi := kwh*price.V.Min, kwh*price.V.Max
		c := TripCost{Currency: price.V.Currency, Price: price.V}
		c.Min, c.Max = round(lo, hi, c.Currency.MinorDigits)
		if d := t.DistanceKm; d.OK && d.V > 0 {
			var r MinorRange
			r.Min, r.Max = round(lo*100/d.V, hi*100/d.V, c.Currency.MinorDigits)
			c.Per100km = known(r)
		}
		out[i] = known(c)
	}
	return out
}
