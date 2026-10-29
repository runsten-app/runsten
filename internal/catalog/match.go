package catalog

import (
	"math"
	"slices"
	"strings"
)

const (
	// grossTolerance is how far batteryCapacityKWH may be from a variant's gross capacity
	// (78.012 for 78, 81.608 for 82), while staying short of the net one (79 for 82).
	grossTolerance = 1.5
	// apiTolerance compares batteryCapacityKWH with the values already seen. They have at
	// most three decimals: a hundredth absorbs float noise and a rounding to two, far below
	// any gap between two real values.
	apiTolerance = 0.01
)

// Result is what Match makes of a vehicle.
type Result struct {
	// Candidates are the variants that fit, or, when none does, the whole family (every
	// year), from which the user chooses; empty for an unknown family.
	Candidates []Variant
	// Recognized is true when exactly one variant fits: Candidates[0].
	Recognized bool
}

// Match recognizes the variant of an electric vehicle from its family and model year,
// the batteryCapacityKWH of its details and its VIN:
//
//  1. the variants of the family whose years hold modelYear;
//  2. those that have already been seen reporting capacityKWh (api_kwh), or else those
//     whose gross capacity is within grossTolerance of it;
//  3. the VIN's motor code (positions 6-7) rules out the variants that list other codes,
//     when a candidate lists it.
//
// An unknown model year (≤ 0) skips step 1's year filter. An unknown capacity (≤ 0, NaN)
// skips step 2: an absent field or 0.0 tells nothing of the battery (0.0 is what a
// combustion car returns, and the caller keeps those out by their fuel type).
func (c *Catalog) Match(family string, modelYear int, capacityKWh float64, vin string) Result {
	return c.MatchReading(Reading{Family: family, ModelYear: modelYear, CapacityKWh: capacityKWh, VIN: vin, Powertrain: Electric})
}

// MatchBrand is Match among the electric variants of a brand, every family, for a vehicle
// whose source tells its maker but not its family (a Polestar of the Data Portal): when
// none fits, the candidates are the whole brand.
func (c *Catalog) MatchBrand(brand Brand, modelYear int, capacityKWh float64, vin string) Result {
	return c.MatchReading(Reading{Brand: brand, ModelYear: modelYear, CapacityKWh: capacityKWh, VIN: vin, Powertrain: Electric})
}

// MatchReading is Match, or MatchBrand for a reading without a family, among the variants
// of the reading's powertrain (Offer), with the reading's trim too (byTrim): what
// Recognize rests on.
func (c *Catalog) MatchReading(r Reading) Result {
	all := c.Offer(r)
	var fits []Variant
	for _, v := range all {
		if r.ModelYear <= 0 || v.Years.Contains(r.ModelYear) {
			fits = append(fits, v)
		}
	}
	fits = byTrim(byMotorCode(byCapacity(fits, r.CapacityKWh), r.VIN), r.Trim)
	if len(fits) == 0 {
		return Result{Candidates: all}
	}
	return Result{Candidates: fits, Recognized: len(fits) == 1}
}

// Offer lists the variants a vehicle may be, every year, in the order of the file: those
// of its powertrain, of its family, else of its brand. Empty for a reading Offered refuses.
func (c *Catalog) Offer(r Reading) []Variant {
	if !r.Offered() {
		return nil
	}
	var vs []Variant
	for _, v := range c.variants {
		if v.Powertrain == r.Powertrain && (v.Family == r.Family || r.Family == "" && v.Brand == r.Brand) {
			vs = append(vs, v)
		}
	}
	return vs
}

// Reading is what the vendor API says of a vehicle: its latest details and its VIN. The
// zero value of a field is unknown.
type Reading struct {
	// Brand stands in for the family when the source tells the maker alone: the variants
	// of the brand are then the candidates, and the one chosen sets the family.
	Brand       Brand
	Family      string
	ModelYear   int     // ≤ 0: unknown
	CapacityKWh float64 // batteryCapacityKWH; ≤ 0: unknown
	VIN         string
	Trim        string // the vendor's badge (Tesla's trim_badging); empty: unknown
	// Powertrain is core.Snapshot's: Electric or PlugInHybrid are matched among the
	// variants of theirs; empty (unknown) or any other (a combustion car) never is.
	Powertrain Powertrain
}

// Offered reports whether the catalog has variants to offer for the reading: a family
// read, or a brand when the source tells the maker alone, of a powertrain the catalog
// has.
func (r Reading) Offered() bool {
	return (r.Family != "" || r.Brand != "") && (r.Powertrain == Electric || r.Powertrain == PlugInHybrid)
}

// Recognize returns the variant of a vehicle when exactly one fits (MatchReading), among
// the variants of its powertrain: a hybrid's capacity may lie near an electric variant's,
// and the derivation tells their charges apart. It is the one rule of recognition, for
// the API's reads and for the derivation alike.
func (c *Catalog) Recognize(r Reading) (Variant, bool) {
	m := c.MatchReading(r)
	if !m.Recognized {
		return Variant{}, false
	}
	return m.Candidates[0], true
}

// Chosen returns the variant the user chose while the choice holds: one the catalog
// still has (Offer), of the powertrain and family read (of the brand read, when the
// family is unknown: the choice then sets it). A choice that no longer holds is ignored,
// never an error: the recognition applies again.
func (c *Catalog) Chosen(id string, r Reading) (Variant, bool) {
	for _, v := range c.Offer(r) {
		if v.ID == id {
			return v, true
		}
	}
	return Variant{}, false
}

// Effective returns the variant a vehicle is taken to be: the chosen one while it holds,
// else the recognized one. The one rule, for the API's reads and for the derivation.
func (c *Catalog) Effective(chosenID string, r Reading) (Variant, bool) {
	if v, ok := c.Chosen(chosenID, r); ok {
		return v, true
	}
	return c.Recognize(r)
}

// byCapacity prefers the values already seen to the gross capacity: they explain readings
// the capacity does not (66.0 on an EX30 of 69 kWh), and outrank a gross capacity that
// merely lies near.
func byCapacity(vs []Variant, kwh float64) []Variant {
	if kwh <= 0 || math.IsNaN(kwh) {
		return vs
	}
	var seen, near []Variant
	for _, v := range vs {
		switch {
		case v.seen(kwh):
			seen = append(seen, v)
		case math.Abs(v.GrossKWh-kwh) <= grossTolerance:
			near = append(near, v)
		}
	}
	if len(seen) > 0 {
		return seen
	}
	return near
}

// byMotorCode breaks ties with the VIN. The code tables are incomplete: a code no
// candidate lists rules nothing out, and a variant with no code is kept.
func byMotorCode(vs []Variant, vin string) []Variant {
	vin = strings.ToUpper(strings.TrimSpace(vin))
	if len(vin) != 17 {
		return vs
	}
	code := vin[5:7]
	lists := func(v Variant) bool { return slices.Contains(v.MotorCodes, code) }
	if !slices.ContainsFunc(vs, lists) {
		return vs
	}
	return slices.DeleteFunc(vs, func(v Variant) bool { return len(v.MotorCodes) > 0 && !lists(v) })
}

// byTrim breaks ties with the vendor's badge, as byMotorCode with the VIN: a badge no
// candidate lists rules nothing out, and a variant that lists none is kept.
func byTrim(vs []Variant, trim string) []Variant {
	trim = strings.ToLower(strings.TrimSpace(trim))
	if trim == "" {
		return vs
	}
	lists := func(v Variant) bool { return slices.Contains(v.Trims, trim) }
	if !slices.ContainsFunc(vs, lists) {
		return vs
	}
	return slices.DeleteFunc(vs, func(v Variant) bool { return len(v.Trims) > 0 && !lists(v) })
}

// seen reports whether batteryCapacityKWH has already been seen at kwh for the variant.
func (v Variant) seen(kwh float64) bool {
	return slices.ContainsFunc(v.APIKWh, func(a APIValue) bool { return math.Abs(a.KWh-kwh) <= apiTolerance })
}
