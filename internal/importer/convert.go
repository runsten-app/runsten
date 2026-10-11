package importer

import (
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"runsten/internal/aximote"
	"runsten/internal/core"
)

// Aximote's events as Runsten's (ADR 0036 §3). Its times are exact: an event's bounds are
// each one instant. Neither state of charge nor odometer on a trip; no AC or DC type on a
// charge, never inferred from its power. An event the checks refuse is left out and
// counted; a fuel stop is left out, uncounted.

// sourceIDPattern is the store's: an event's ID is source:source_id in the API.
var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)

// maxAddress is the store's bound of an address, in characters.
const maxAddress = 500

func convertTrips(items []aximote.Trip, source string, at time.Time) ([]ImportedTrip, int) {
	var out []ImportedTrip
	invalid := 0
	for _, t := range items {
		trip, ok := convertTrip(t, source, at)
		if !ok {
			invalid++
			continue
		}
		out = append(out, ImportedTrip{Trip: trip})
	}
	return out, invalid
}

func convertTrip(t aximote.Trip, source string, at time.Time) (core.Trip, bool) {
	if t.EndTime == nil || !sourceIDPattern.MatchString(t.ID) || t.EndTime.Before(t.StartTime) {
		return core.Trip{}, false
	}
	from, fromOK := point(t.StartLocation)
	to, toOK := point(t.EndLocation)
	if !fromOK || !toOK {
		return core.Trip{}, false
	}
	start, end := exact(t.StartTime), exact(*t.EndTime)
	trip := core.Trip{
		DetectedAt: at.UTC(), Start: start, End: end, Sources: []string{source},
		Import: core.Some(core.Import{
			Source: source, SourceID: t.ID, ImportedAt: at,
			StartAddress: address(t.StartLocation), EndAddress: address(t.EndLocation),
		}, at),
		From: from, To: to,
	}
	var ok bool
	if trip.DistanceKm, ok = nonNegative(t.DistanceKm, at); !ok {
		return core.Trip{}, false
	}
	if t.EnergyConsumedKWh != nil {
		if !finite(*t.EnergyConsumedKWh) {
			return core.Trip{}, false
		}
		trip.EnergyKWh = core.Some(*t.EnergyConsumedKWh, at)
	}
	if !fits(trip.Import.V.StartAddress) || !fits(trip.Import.V.EndAddress) {
		return core.Trip{}, false
	}
	return trip, true
}

func convertCharges(items []aximote.Refuel, source string, capacityKWh *float64, at time.Time) ([]core.Charge, int) {
	var out []core.Charge
	invalid := 0
	for _, r := range items {
		if r.Type == aximote.RefuelFuel {
			continue
		}
		c, ok := convertCharge(r, source, capacityKWh, at)
		if !ok {
			invalid++
			continue
		}
		out = append(out, c)
	}
	return out, invalid
}

func convertCharge(r aximote.Refuel, source string, capacityKWh *float64, at time.Time) (core.Charge, bool) {
	if r.Type != aximote.RefuelCharge || r.IsActive || r.EndTime == nil || !sourceIDPattern.MatchString(r.ID) ||
		r.EndTime.Before(r.StartTime) {
		return core.Charge{}, false
	}
	pos, ok := point(r.Location)
	if !ok {
		return core.Charge{}, false
	}
	c := core.Charge{
		DetectedAt: at.UTC(), Start: exact(r.StartTime), End: exact(*r.EndTime), Sources: []string{source},
		Import:   core.Some(core.Import{Source: source, SourceID: r.ID, ImportedAt: at, StartAddress: address(r.Location)}, at),
		Position: pos,
	}
	if !fits(c.Import.V.StartAddress) {
		return core.Charge{}, false
	}
	if c.EnergyPowerKWh, ok = nonNegative(r.EnergyChargedKWh, at); !ok {
		return core.Charge{}, false
	}
	if c.AveragePowerKW, ok = nonNegative(r.AvgChargingKW, at); !ok {
		return core.Charge{}, false
	}
	// The state of charge: the battery's energy over the vehicle's capacity, at both ends
	// (an assumption until tried on a real account: ADR 0036, B1).
	if r.StartEnergyKWh != nil && r.EndEnergyKWh != nil && capacityKWh != nil && finite(*capacityKWh) && *capacityKWh > 0 {
		capacity, from, to := *capacityKWh, *r.StartEnergyKWh, *r.EndEnergyKWh
		startSoC, endSoC := from/capacity*100, to/capacity*100
		if !finite(startSoC) || !finite(endSoC) || startSoC < 0 || startSoC > 100 || endSoC < 0 || endSoC > 100 {
			return core.Charge{}, false
		}
		c.StartSoC, c.EndSoC = core.Some(startSoC, at), core.Some(endSoC, at)
		c.EnergySoCKWh = core.Some(to-from, at)
		c.Capacity = core.Some(core.Capacity{KWh: capacity, Source: core.CapacityAPI}, at)
	}
	if r.CostAmount != nil && r.CostCurrency != nil {
		if !finite(*r.CostAmount) || *r.CostAmount < 0 {
			return core.Charge{}, false
		}
		// Only a currency whose minor digits are known: Runsten's.
		if cur, known := core.CurrencyOf(*r.CostCurrency); known {
			minor := math.Round(*r.CostAmount * math.Pow10(cur.MinorDigits))
			c.PaidCost = core.Some(core.Amount{Currency: cur.Code, Minor: int64(minor)}, at)
		}
	}
	return c, true
}

func exact(t time.Time) core.Bounds { return core.Bounds{After: t.UTC(), Before: t.UTC()} }

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// nonNegative is v when given, false when it is out of bounds.
func nonNegative(v *float64, at time.Time) (core.Value[float64], bool) {
	if v == nil {
		return core.Value[float64]{}, true
	}
	if !finite(*v) || *v < 0 {
		return core.Value[float64]{}, false
	}
	return core.Some(*v, at), true
}

// point is p's position when given, false when it is out of bounds.
func point(p *aximote.Point) (core.Value[core.Position], bool) {
	if p == nil {
		return core.Value[core.Position]{}, true
	}
	if !finite(p.Latitude) || !finite(p.Longitude) || math.Abs(p.Latitude) > 90 || math.Abs(p.Longitude) > 180 {
		return core.Value[core.Position]{}, false
	}
	return core.Value[core.Position]{V: core.Position{Lat: p.Latitude, Lon: p.Longitude}, OK: true}, true
}

// address is the source's name of a place: its locality and its country, or empty.
func address(p *aximote.Point) string {
	if p == nil {
		return ""
	}
	var parts []string
	for _, s := range []*string{p.Locality, p.Country} {
		if s != nil && strings.TrimSpace(*s) != "" {
			parts = append(parts, strings.TrimSpace(*s))
		}
	}
	return strings.Join(parts, ", ")
}

func fits(s string) bool { return utf8.ValidString(s) && utf8.RuneCountInString(s) <= maxAddress }
