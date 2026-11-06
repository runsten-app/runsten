package core

import (
	"cmp"
	"math"
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
	// DistanceUnknown counts the trips whose distance is not known.
	DistanceUnknown int
}

// MapTrips draws the map of the trips that started in [from, to).
func MapTrips(trips []Trip, places []Place, from, to time.Time) TripMap {
	type gather struct {
		MapSpot
		sumLat float64
		sumLon float64 // from First.Lon
		n      int
	}
	var out TripMap
	var spots []*gather
	byPlace := map[string]int{}
	free := newSpotGrid()
	spotOf := func(p Position) int {
		place, at := PlaceAt(p, places)
		var i int
		var ok bool
		if at {
			i, ok = byPlace[place.ID]
		} else {
			// The nearest spot outside every place within reach, measured from its first
			// end: a spot never creeps along a street.
			i, ok = free.nearest(p, func(j int) Position { return spots[j].First })
		}
		if !ok {
			i = len(spots)
			g := &gather{MapSpot: MapSpot{First: p}}
			if at {
				byPlace[place.ID] = i
				g.Position, g.Place = place.Position, Value[Place]{V: place, OK: true}
			} else {
				free.add(p, i)
			}
			spots = append(spots, g)
		}
		g := spots[i]
		// The longitudes are summed from the first end's, so that a spot across the
		// antimeridian stays there, never moved round the world to its mean.
		g.sumLat, g.sumLon, g.n = g.sumLat+p.Lat, g.sumLon+wrapLon(p.Lon-g.First.Lon), g.n+1
		if !g.Place.OK {
			g.Position = Position{Lat: g.sumLat / float64(g.n), Lon: wrapLon(g.First.Lon + g.sumLon/float64(g.n))}
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
		} else {
			l.DistanceUnknown++
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
		out.Links = append(out.Links, MapLink{
			A: min(a, b), B: max(a, b), Trips: l.Trips, DistanceKm: l.DistanceKm, DistanceUnknown: l.DistanceUnknown,
		})
	}
	slices.SortFunc(out.Links, func(x, y MapLink) int {
		return cmp.Or(cmp.Compare(y.Trips, x.Trips), cmp.Compare(x.A, y.A), cmp.Compare(x.B, y.B))
	})
	return out
}

// spotGrid holds the spots outside every place by the cell of their first end, cells
// SpotRadiusM high: an end is measured against the spots of the cells around it only, so
// that a long history of scattered ends costs as much as its ends, not their square.
type spotGrid struct {
	cells map[[2]int][]int
	all   []int // every spot, for the poles, where the cells around are most of a row
	deg   float64
	nLon  int
}

func newSpotGrid() *spotGrid {
	deg := SpotRadiusM / (earthRadiusM * math.Pi / 180)
	return &spotGrid{cells: map[[2]int][]int{}, deg: deg, nLon: int(math.Ceil(360 / deg))}
}

// cell is p's row and column; the columns wrap around the antimeridian.
func (g *spotGrid) cell(p Position) (int, int) {
	return int(math.Floor(p.Lat / g.deg)), g.wrap(int(math.Floor((p.Lon + 180) / g.deg)))
}

func (g *spotGrid) wrap(x int) int { return ((x % g.nLon) + g.nLon) % g.nLon }

func (g *spotGrid) add(first Position, i int) {
	y, x := g.cell(first)
	g.cells[[2]int{y, x}] = append(g.cells[[2]int{y, x}], i)
	g.all = append(g.all, i)
}

// nearest is the spot whose first end is the nearest to p within SpotRadiusM, the first
// added between equals. Within the radius, an end is at most one row away from it, and as
// many columns as a degree of longitude shrinks at the farther latitude, one more for
// the rounding.
func (g *spotGrid) nearest(p Position, first func(int) Position) (int, bool) {
	best, bestD, ok := 0, 0.0, false
	try := func(j int) {
		if d := distanceM(p, first(j)); d <= SpotRadiusM && (!ok || d < bestD || d == bestD && j < best) {
			best, bestD, ok = j, d, true
		}
	}
	shrink := math.Cos((math.Abs(p.Lat) + g.deg) * math.Pi / 180)
	k := 0
	if shrink > 0.01 {
		k = int(math.Ceil(1/shrink)) + 1
	}
	if k == 0 || 2*k+1 >= g.nLon {
		for _, j := range g.all {
			try(j)
		}
		return best, ok
	}
	y, x := g.cell(p)
	for dy := -1; dy <= 1; dy++ {
		for dx := -k; dx <= k; dx++ {
			for _, j := range g.cells[[2]int{y + dy, g.wrap(x + dx)}] {
				try(j)
			}
		}
	}
	return best, ok
}

// wrapLon brings a longitude, or a difference of two, into [-180, 180); one there already
// is kept as is, never rounded through the shift.
func wrapLon(lon float64) float64 {
	if lon >= -180 && lon < 180 {
		return lon
	}
	return math.Mod(math.Mod(lon+180, 360)+360, 360) - 180
}
