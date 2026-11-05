package core

import (
	"reflect"
	"testing"
	"time"
)

func TestMapTrips(t *testing.T) {
	at := func(day, hour int) time.Time { return time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC) }
	trip := func(start time.Time, from, to Value[Position], km float64) Trip {
		return Trip{Start: Bounds{After: start, Before: start}, From: from, To: to, DistanceKm: known(km)}
	}
	home := homePlace()
	// Work is no place: the vehicle parks along the same street, a few dozen meters apart,
	// on both sides of a round latitude.
	work1, work2 := Position{Lat: 45.7798, Lon: 4.9270}, Position{Lat: 45.7801, Lon: 4.9274}
	shop := Position{Lat: 45.7501, Lon: 4.8501}
	nearHome := Position{Lat: 45.7642, Lon: 4.8359} // within home's circle
	trips := []Trip{
		trip(at(1, 7), known(lyon), known(work1), 32),
		trip(at(1, 17), known(work2), known(nearHome), 33),
		trip(at(2, 7), known(lyon), known(work2), 32),
		with(trip(at(2, 18), known(work1), known(shop), 9), func(t *Trip) { t.DistanceKm = Value[float64]{} }),
		trip(at(2, 19), known(shop), known(lyon), 4),
		trip(at(3, 9), known(lyon), known(nearHome), 1), // a round trip: no link
		trip(at(3, 12), Value[Position]{}, known(lyon), 5),
		trip(at(1, 6), known(lyon), known(shop), 4), // before the period
		trip(at(4, 0), known(shop), known(lyon), 4), // at its end: after it
	}
	got := MapTrips(trips, []Place{home}, at(1, 7), at(4, 0))
	work := Position{Lat: (work1.Lat + work2.Lat) / 2, Lon: (work1.Lon + work2.Lon) / 2}
	want := TripMap{
		Spots: []MapSpot{
			{Position: lyon, Place: known(home), Departures: 3, Arrivals: 3},
			{Position: work, Departures: 2, Arrivals: 2},
			{Position: shop, Departures: 1, Arrivals: 1},
		},
		Links: []MapLink{
			{A: 0, B: 1, Trips: 3, DistanceKm: 97},
			{A: 0, B: 2, Trips: 1, DistanceKm: 4},
			{A: 1, B: 2, Trips: 1, DistanceUnknown: 1},
		},
		Trips: 6, WithoutPosition: 1,
	}
	if !reflect.DeepEqual(got.Links, want.Links) || got.Trips != want.Trips || got.WithoutPosition != want.WithoutPosition {
		t.Errorf("links = %+v, trips = %d, without = %d", got.Links, got.Trips, got.WithoutPosition)
	}
	if len(got.Spots) != len(want.Spots) {
		t.Fatalf("spots = %+v", got.Spots)
	}
	for i, s := range got.Spots {
		w := want.Spots[i]
		if s.Departures != w.Departures || s.Arrivals != w.Arrivals || s.Place.OK != w.Place.OK || s.Place.V.ID != w.Place.V.ID ||
			distanceM(s.Position, w.Position) > 0.01 {
			t.Errorf("spot %d = %+v, want %+v", i, s, w)
		}
	}
	if m := MapTrips(nil, nil, at(1, 0), at(2, 0)); m.Spots != nil || m.Links != nil || m.Trips != 0 {
		t.Errorf("no trip: %+v", m)
	}
}
