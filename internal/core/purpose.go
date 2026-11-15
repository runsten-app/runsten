package core

import "time"

// Purpose is what a trip was made for, as the user declares it: for a mileage allowance,
// a company car's private use, an expense report. Commute stands apart from Business: each
// country counts the way to work its own way, as business in some, apart in others.
type Purpose string

// Purposes.
const (
	PurposeBusiness Purpose = "business"
	PurposeCommute  Purpose = "commute"
	PurposePersonal Purpose = "personal"
)

// Purposes are every purpose, in the order they are offered.
var Purposes = []Purpose{PurposeBusiness, PurposeCommute, PurposePersonal}

// PurposeScope tells which trips a place's rule marks.
type PurposeScope string

// Scopes of a place's rule.
const (
	// ScopeEnd marks the trips that end at the place: the way there, not the way back.
	ScopeEnd PurposeScope = "end"
	// ScopeEither marks the trips that start or end at the place.
	ScopeEither PurposeScope = "either"
)

// PurposeRule is a place's rule: the trips of its scope have its purpose, unless marked
// by hand.
type PurposeRule struct {
	Purpose Purpose
	Scope   PurposeScope
}

// PurposeOrigin tells where a trip's purpose comes from.
type PurposeOrigin string

// Origins of a trip's purpose.
const (
	PurposeByHand PurposeOrigin = "hand"
	PurposeByRule PurposeOrigin = "rule"
)

// TripMark is a trip's mark set by hand: its purpose, or none, which no rule overrides.
type TripMark struct {
	Purpose Value[Purpose]
}

// TripPurpose is a trip's purpose and where it comes from. Marked by hand, Purpose may be
// absent: the user said the trip has none, which no rule overrides.
type TripPurpose struct {
	Purpose Value[Purpose]
	Origin  PurposeOrigin
	// Place is the place whose rule gave the purpose; absent by hand.
	Place Value[Place]
}

// PurposeOf is the purpose of a trip: the one marked by hand (marked, even without a
// purpose), else the rule of the place it ends at, or starts at when that place's rule
// takes both ends. Two rules that disagree give none: a trip from a place marked Business
// to one marked Personal is left for the user to tell. Absent: the trip has no purpose.
func PurposeOf(t Trip, mark Value[TripMark], places []Place) Value[TripPurpose] {
	if mark.OK {
		return known(TripPurpose{Purpose: mark.V.Purpose, Origin: PurposeByHand})
	}
	var rules []Place
	if t.To.OK {
		if p, ok := PlaceAt(t.To.V, places); ok && p.Rule.OK {
			rules = append(rules, p)
		}
	}
	if t.From.OK {
		if p, ok := PlaceAt(t.From.V, places); ok && p.Rule.OK && p.Rule.V.Scope == ScopeEither {
			rules = append(rules, p)
		}
	}
	if len(rules) == 0 {
		return Value[TripPurpose]{}
	}
	for _, p := range rules[1:] {
		if p.Rule.V.Purpose != rules[0].Rule.V.Purpose {
			return Value[TripPurpose]{}
		}
	}
	// The place the trip ends at names the rule, when both ends agree.
	return known(TripPurpose{Purpose: known(rules[0].Rule.V.Purpose), Origin: PurposeByRule, Place: known(rules[0])})
}

// PurposeTotals are the trips of one purpose: how many, how far, what they cost.
type PurposeTotals struct {
	Count      int
	DistanceKm float64 // of the known distances
	// Cost sums the known costs; CostUnknown counts the trips left out of it.
	Cost        MinorRange
	CostUnknown int
}

// TripsByPurpose are the trips of a period by purpose; None are those without one.
type TripsByPurpose struct {
	Business, Commute, Personal, None PurposeTotals
}

// SumByPurpose sums the trips that started in [from, to) by purpose: purposes[i] and
// costs[i] are trips[i]'s.
func SumByPurpose(trips []Trip, purposes []Value[TripPurpose], costs []Value[TripCost], from, to time.Time) TripsByPurpose {
	var out TripsByPurpose
	for i, t := range trips {
		if !in(t.Start.After, from, to) {
			continue
		}
		total := &out.None
		if p := purposes[i]; p.OK && p.V.Purpose.OK {
			switch p.V.Purpose.V {
			case PurposeBusiness:
				total = &out.Business
			case PurposeCommute:
				total = &out.Commute
			case PurposePersonal:
				total = &out.Personal
			}
		}
		total.Count++
		if t.DistanceKm.OK {
			total.DistanceKm += t.DistanceKm.V
		}
		if c := costs[i]; c.OK {
			total.Cost.Min += c.V.Min
			total.Cost.Max += c.V.Max
		} else {
			total.CostUnknown++
		}
	}
	return out
}
