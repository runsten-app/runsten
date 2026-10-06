package core

import "math"

// GeoCell is a square of 10⁻⁴ degrees (about 11 m of latitude) holding positions that
// share an address: the positions a geocoder is asked for, and the key of its answers.
// A cell is not a place: it tells no more than where the vehicle stood, give or take the
// precision of its positioning.
type GeoCell struct {
	LatE4, LonE4 int32 // the position in 10⁻⁴ degrees, rounded to the nearest
}

// CellOf is the cell holding a position.
func CellOf(p Position) GeoCell {
	return GeoCell{LatE4: int32(math.Round(p.Lat * 1e4)), LonE4: int32(math.Round(p.Lon * 1e4))}
}

// Center is the position a geocoder is asked for the cell.
func (c GeoCell) Center() Position {
	return Position{Lat: float64(c.LatE4) / 1e4, Lon: float64(c.LonE4) / 1e4}
}
