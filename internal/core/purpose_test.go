package core

import (
	"reflect"
	"testing"
	"time"
)

func TestPurposeOf(t *testing.T) {
	home := Place{ID: "home", Name: "Home", Position: lyon, RadiusM: 100}
	officeAt := Position{Lat: 45.75, Lon: 4.85}
	shopAt := Position{Lat: 45.78, Lon: 4.80}
	rule := func(p Purpose, s PurposeScope) Value[PurposeRule] { return known(PurposeRule{Purpose: p, Scope: s}) }
	office := Place{ID: "office", Name: "Office", Position: officeAt, RadiusM: 100, Rule: rule(PurposeCommute, ScopeEnd)}
	client := Place{ID: "client", Name: "Client", Position: Position{Lat: 45.70, Lon: 4.90}, RadiusM: 100, Rule: rule(PurposeBusiness, ScopeEither)}
	shop := Place{ID: "shop", Name: "Shop", Position: shopAt, RadiusM: 100, Rule: rule(PurposePersonal, ScopeEither)}
	places := []Place{home, office, client, shop}
	trip := func(from, to Value[Position]) Trip { return Trip{From: from, To: to} }
	byRule := func(p Place) Value[TripPurpose] {
		return known(TripPurpose{Purpose: known(p.Rule.V.Purpose), Origin: PurposeByRule, Place: known(p)})
	}
	elsewhere := known(Position{Lat: 48.85, Lon: 2.35})

	for _, tt := range []struct {
		name string
		trip Trip
		mark Value[TripMark]
		want Value[TripPurpose]
	}{
		{name: "no rule, no mark", trip: trip(known(lyon), elsewhere)},
		{name: "ends at a place of scope end", trip: trip(known(lyon), known(officeAt)), want: byRule(office)},
		{name: "starts at a place of scope end", trip: trip(known(officeAt), known(lyon))},
		{name: "starts at a place of scope either", trip: trip(known(client.Position), known(lyon)), want: byRule(client)},
		{name: "ends at a place of scope either", trip: trip(elsewhere, known(client.Position)), want: byRule(client)},
		{name: "two rules that disagree", trip: trip(known(shopAt), known(client.Position))},
		{
			name: "two rules that agree: the end's names it",
			trip: Trip{From: known(shopAt), To: known(Position{Lat: 45.7801, Lon: 4.8001})},
			want: byRule(shop),
		},
		{name: "positions unknown", trip: trip(Value[Position]{}, Value[Position]{})},
		{
			name: "marked by hand wins over the rule",
			trip: trip(known(lyon), known(officeAt)), mark: known(TripMark{Purpose: known(PurposeBusiness)}),
			want: known(TripPurpose{Purpose: known(PurposeBusiness), Origin: PurposeByHand}),
		},
		{
			name: "marked by hand as none wins over the rule",
			trip: trip(known(lyon), known(officeAt)), mark: known(TripMark{}),
			want: known(TripPurpose{Origin: PurposeByHand}),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := PurposeOf(tt.trip, tt.mark, places); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PurposeOf = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSumByPurpose(t *testing.T) {
	from := utc("2026-10-01T00:00:00Z")
	to := from.Add(48 * time.Hour)
	trip := func(h int, km Value[float64]) Trip {
		at := from.Add(time.Duration(h) * time.Hour)
		return Trip{Start: Bounds{at, at}, End: Bounds{at.Add(time.Hour), at.Add(time.Hour)}, DistanceKm: km}
	}
	purpose := func(p Purpose) Value[TripPurpose] {
		return known(TripPurpose{Purpose: known(p), Origin: PurposeByHand})
	}
	cost := func(lo, hi int64) Value[TripCost] { return known(TripCost{Min: lo, Max: hi}) }
	trips := []Trip{trip(-2, val(5)), trip(1, val(30)), trip(3, val(98)), trip(5, Value[float64]{}), trip(7, val(12)), trip(9, val(4))}
	purposes := []Value[TripPurpose]{
		purpose(PurposeBusiness), purpose(PurposeCommute), purpose(PurposeBusiness), purpose(PurposeBusiness),
		known(TripPurpose{Origin: PurposeByHand}),
		{},
	}
	costs := []Value[TripCost]{cost(9, 9), cost(140, 144), cost(450, 460), {}, cost(50, 50), cost(20, 21)}
	want := TripsByPurpose{
		Business: PurposeTotals{Count: 2, DistanceKm: 98, Cost: MinorRange{450, 460}, CostUnknown: 1},
		Commute:  PurposeTotals{Count: 1, DistanceKm: 30, Cost: MinorRange{140, 144}},
		None:     PurposeTotals{Count: 2, DistanceKm: 16, Cost: MinorRange{70, 71}},
	}
	if got := SumByPurpose(trips, purposes, costs, from, to); got != want {
		t.Errorf("SumByPurpose = %+v, want %+v", got, want)
	}
}
