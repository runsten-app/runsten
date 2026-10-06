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

// Match recognizes the variant of a vehicle from its family and model year, the
// batteryCapacityKWH of its details and its VIN:
//
//  1. the variants of the family whose years hold modelYear;
//  2. those that have already been seen reporting capacityKWh (api_kwh), or else those
//     whose gross capacity is within grossTolerance of it;
//  3. the VIN's motor code (positions 4-5) rules out the variants that list other codes,
//     when a candidate lists it.
//
// An unknown model year (≤ 0) skips step 1's year filter. An unknown capacity (≤ 0, NaN)
// skips step 2: an absent field or 0.0 tells nothing of the battery (0.0 is what a
// combustion car returns, and the caller keeps those out by their fuel type).
func (c *Catalog) Match(family string, modelYear int, capacityKWh float64, vin string) Result {
	var fits []Variant
	for _, v := range c.variants {
		if v.Family == family && (modelYear <= 0 || v.Years.Contains(modelYear)) {
			fits = append(fits, v)
		}
	}
	fits = byMotorCode(byCapacity(fits, capacityKWh), vin)
	if len(fits) == 0 {
		return Result{Candidates: c.Family(family)}
	}
	return Result{Candidates: fits, Recognized: len(fits) == 1}
}

// Reading is what the vendor API says of a vehicle: its latest details and its VIN. The
// zero value of a field is unknown.
type Reading struct {
	Family      string
	ModelYear   int     // ≤ 0: unknown
	CapacityKWh float64 // batteryCapacityKWH; ≤ 0: unknown
	VIN         string
	// BatteryElectric is true when the vehicle is known to run on its battery alone:
	// false for a hybrid, a combustion car, and an unknown fuel type.
	BatteryElectric bool
}

// Recognize returns the variant of a vehicle when exactly one fits (Match). Only a
// battery electric vehicle is recognized: a hybrid's capacity may lie near an electric
// variant's, and Runsten does not derive its charges correctly. It is the one rule of
// recognition, for the API's reads and for the derivation alike.
func (c *Catalog) Recognize(r Reading) (Variant, bool) {
	if r.Family == "" || !r.BatteryElectric {
		return Variant{}, false
	}
	m := c.Match(r.Family, r.ModelYear, r.CapacityKWh, r.VIN)
	if !m.Recognized {
		return Variant{}, false
	}
	return m.Candidates[0], true
}

// Chosen returns the variant the user chose while the choice holds: one the catalog
// still has, of the family read, for a battery electric vehicle. A choice that no longer
// holds is ignored, never an error: the recognition applies again.
func (c *Catalog) Chosen(id string, r Reading) (Variant, bool) {
	if r.Family == "" || !r.BatteryElectric {
		return Variant{}, false
	}
	v, ok := c.Variant(id)
	if !ok || v.Family != r.Family {
		return Variant{}, false
	}
	return v, true
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
	code := vin[3:5]
	lists := func(v Variant) bool { return slices.Contains(v.MotorCodes, code) }
	if !slices.ContainsFunc(vs, lists) {
		return vs
	}
	return slices.DeleteFunc(vs, func(v Variant) bool { return len(v.MotorCodes) > 0 && !lists(v) })
}

// seen reports whether batteryCapacityKWH has already been seen at kwh for the variant.
func (v Variant) seen(kwh float64) bool {
	return slices.ContainsFunc(v.APIKWh, func(a APIValue) bool { return math.Abs(a.KWh-kwh) <= apiTolerance })
}
