package core

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// SpotDegrees is the side of the squares, in degrees, that gather the ends of the trips
// outside every place: about 330 m of latitude, enough for the spots a vehicle parks at
// around the same address, day after day, to be one.
const SpotDegrees = 0.003

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

// MapSpot gathers the ends of trips: a place's (its circle), or else a square of
// SpotDegrees.
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
	type spotKey struct {
		place    string
		lat, lon int64
	}
	type gather struct {
		MapSpot
		sumLat, sumLon float64
		n              int
	}
	var out TripMap
	keys := map[spotKey]int{}
	var spots []*gather
	spotOf := func(p Position) int {
		k := spotKey{lat: int64(math.Floor(p.Lat / SpotDegrees)), lon: int64(math.Floor(p.Lon / SpotDegrees))}
		place, at := PlaceAt(p, places)
		if at {
			k = spotKey{place: place.ID}
		}
		i, ok := keys[k]
		if !ok {
			i = len(spots)
			keys[k] = i
			g := &gather{MapSpot: MapSpot{First: p}}
			if at {
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
