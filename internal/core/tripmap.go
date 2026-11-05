package core

import (
	"cmp"
	"slices"
	"time"
)

// SpotRadiusM is how far from the first end a spot gathered the others may be, outside
// every place: enough for the spots a vehicle parks at around the same address, day after
// day, to be one.
const SpotRadiusM = 250

// TripMap is where a vehicle went over a period, drawn from the ends of its trips: the
// spots it left and reached, and the links between them, each the trips that went one way
// or the other. A trip read by Runsten has its two ends only, never its path: a link is a
// straight line, and the same journey repeated is one link, counted again.
type TripMap struct {
	Spots []MapSpot // the most visited first
	Links []MapLink // the most driven first; a trip back to its spot is none
	// Trips counts the trips of the map, both ends known; WithoutPosition the others.
	Trips, WithoutPosition int
}

// MapSpot gathers the ends of trips: a place's (its circle), or else those within
// SpotRadiusM of the first one it gathered.
type MapSpot struct {
	Position Position // the place's, else the mean of the ends it gathers
	// First is the first end the spot gathered: one a trip had, whose address the list
	// of the trips already asked for.
	First                Position
	Place                Value[Place]
	Departures, Arrivals int
}

// MapLink is the trips between two spots, A before B in Spots, either way.
type MapLink struct {
	A, B       int
	Trips      int
	DistanceKm float64 // the sum of the known distances
}

// MapTrips draws the map of the trips that started in [from, to).
func MapTrips(trips []Trip, places []Place, from, to time.Time) TripMap {
	type gather struct {
		MapSpot
		sumLat, sumLon float64
		n              int
	}
	var out TripMap
	var spots []*gather
	byPlace := map[string]int{}
	spotOf := func(p Position) int {
		place, at := PlaceAt(p, places)
		i, ok := -1, false
		if at {
			i, ok = byPlace[place.ID]
		} else {
			// The nearest spot outside every place within reach, measured from its first
			// end: a spot never creeps along a street.
			best := 0.0
			for j, g := range spots {
				if d := distanceM(p, g.First); !g.Place.OK && d <= SpotRadiusM && (!ok || d < best) {
					i, ok, best = j, true, d
				}
			}
		}
		if !ok {
			i = len(spots)
			g := &gather{MapSpot: MapSpot{First: p}}
			if at {
				byPlace[place.ID] = i
				g.Position, g.Place = place.Position, Value[Place]{V: place, OK: true}
			}
			spots = append(spots, g)
		}
		g := spots[i]
		g.sumLat, g.sumLon, g.n = g.sumLat+p.Lat, g.sumLon+p.Lon, g.n+1
		if !g.Place.OK {
			g.Position = Position{Lat: g.sumLat / float64(g.n), Lon: g.sumLon / float64(g.n)}
		}
		return i
	}
	// Oldest first, whatever the order given: the same trips gather the same spots.
	trips = slices.Clone(trips)
	slices.SortStableFunc(trips, func(x, y Trip) int {
		return cmp.Or(x.Start.After.Compare(y.Start.After), x.DetectedAt.Compare(y.DetectedAt))
	})
	type linkKey struct{ a, b int }
	links := map[linkKey]*MapLink{}
	for _, t := range trips {
		if t.Start.After.Before(from) || !t.Start.After.Before(to) {
			continue
		}
		if !t.From.OK || !t.To.OK {
			out.WithoutPosition++
			continue
		}
		out.Trips++
		a, b := spotOf(t.From.V), spotOf(t.To.V)
		spots[a].Departures++
		spots[b].Arrivals++
		if a == b {
			continue
		}
		k := linkKey{min(a, b), max(a, b)}
		l := links[k]
		if l == nil {
			l = &MapLink{A: k.a, B: k.b}
			links[k] = l
		}
		l.Trips++
		if t.DistanceKm.OK {
			l.DistanceKm += t.DistanceKm.V
		}
	}
	// The spots by visits, the first seen first between equals; the links follow them.
	order := make([]int, len(spots))
	for i := range order {
		order[i] = i
	}
	visits := func(i int) int { return spots[i].Departures + spots[i].Arrivals }
	slices.SortStableFunc(order, func(i, j int) int { return cmp.Compare(visits(j), visits(i)) })
	rank := make([]int, len(spots))
	for r, i := range order {
		rank[i] = r
		out.Spots = append(out.Spots, spots[i].MapSpot)
	}
	for _, l := range links {
		a, b := rank[l.A], rank[l.B]
		out.Links = append(out.Links, MapLink{A: min(a, b), B: max(a, b), Trips: l.Trips, DistanceKm: l.DistanceKm})
	}
	slices.SortFunc(out.Links, func(x, y MapLink) int {
		return cmp.Or(cmp.Compare(y.Trips, x.Trips), cmp.Compare(x.A, y.A), cmp.Compare(x.B, y.B))
	})
	return out
}
