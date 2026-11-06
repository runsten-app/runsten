package core

import (
	"math/rand/v2"
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

// The grid finds the spot a scan of every spot would: the nearest first end within reach,
// the first added between equals, around the antimeridian and near the poles too.
func TestSpotGridNearest(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // reproducible test data
	for _, c := range []Position{{Lat: 45.76, Lon: 4.83}, {Lat: -33.9, Lon: 179.999}, {Lat: 0, Lon: -180}, {Lat: 78.2, Lon: 15.6}, {Lat: 89.99, Lon: 0}} {
		g := newSpotGrid()
		var firsts []Position
		first := func(j int) Position { return firsts[j] }
		for range 3000 {
			// Within about a kilometer of c, some ends on the same spot as others.
			p := Position{Lat: c.Lat + (r.Float64()-0.5)*0.02, Lon: c.Lon + (r.Float64()-0.5)*0.02}
			if p.Lat > 90 {
				p.Lat = 180 - p.Lat
			}
			if p.Lon >= 180 {
				p.Lon -= 360
			}
			if len(firsts) > 0 && r.IntN(4) == 0 {
				p = firsts[r.IntN(len(firsts))]
			}
			want, wantOK := 0, false
			for j, f := range firsts {
				if d := distanceM(p, f); d <= SpotRadiusM && (!wantOK || d < distanceM(p, firsts[want])) {
					want, wantOK = j, true
				}
			}
			got, ok := g.nearest(p, first)
			if ok != wantOK || ok && got != want {
				t.Fatalf("around %v, %v: got %d %v, want %d %v", c, p, got, ok, want, wantOK)
			}
			if !ok {
				g.add(p, len(firsts))
				firsts = append(firsts, p)
			}
		}
	}
}

// A long history of scattered ends costs as much as its ends, not their square: each is
// measured against the spots around it only.
func TestSpotGridScattered(t *testing.T) {
	g := newSpotGrid()
	var firsts []Position
	measured := 0
	first := func(j int) Position { measured++; return firsts[j] }
	for i := range 80_000 {
		p := Position{Lat: 40 + float64(i%400)*0.005, Lon: float64(i/400) * 0.01}
		if _, ok := g.nearest(p, first); ok {
			t.Fatalf("%v gathered", p)
		}
		g.add(p, len(firsts))
		firsts = append(firsts, p)
	}
	if measured > 10*len(firsts) {
		t.Errorf("%d measures for %d spots", measured, len(firsts))
	}
}
