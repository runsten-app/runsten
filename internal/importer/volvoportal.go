package importer

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/volvoportal"
)

// SourceVolvoDataPortal is the Volvo Data Portal, whose files the user uploads.
const SourceVolvoDataPortal = "volvo_data_portal"

// trackMargin is how far around a trip's bounds its waypoints are looked for: the
// portal records one just before the start, at the same place.
const trackMargin = time.Minute

// Saver writes imported events at once: the store.
type Saver interface {
	SaveImport(ctx context.Context, accountID, vehicleID string, trips []ImportedTrip, charges []core.Charge) (Result, error)
}

// Files imports the files a user uploads, at once, without a job: they are already
// here.
type Files struct {
	store Saver
	clk   clock.Clock
}

// NewFiles creates the import of files.
func NewFiles(store Saver, clk clock.Clock) *Files { return &Files{store: store, clk: clk} }

// Report is what an import of files did.
type Report struct {
	Result
	Invalid int // rows the checks refused, left out
}

// VolvoDataPortal imports the trips' file of the Volvo Data Portal and, when given, its
// waypoints' file, which makes the trips' tracks. A file that is not the portal's
// returns a *volvoportal.FormatError, and nothing is written.
func (f *Files) VolvoDataPortal(ctx context.Context, accountID, vehicleID string, trips, waypoints io.Reader) (Report, error) {
	rows, invalid, err := volvoportal.ReadTrips(trips)
	if err != nil {
		return Report{}, fmt.Errorf("trips: %w", err)
	}
	var points []volvoportal.Waypoint
	if waypoints != nil {
		// A waypoint left out only thins a track: it is not counted.
		if points, _, err = volvoportal.ReadWaypoints(waypoints); err != nil {
			return Report{}, fmt.Errorf("waypoints: %w", err)
		}
	}
	at := f.clk.Now()
	out, refused := convertPortalTrips(rows, points, at)
	res, err := f.store.SaveImport(ctx, accountID, vehicleID, out, nil)
	if err != nil {
		return Report{}, fmt.Errorf("saving: %w", err)
	}
	return Report{Result: res, Invalid: invalid + refused}, nil
}

// convertPortalTrips converts the portal's trips, each with its track when at least two
// of the waypoints fall within it.
func convertPortalTrips(rows []volvoportal.Trip, waypoints []volvoportal.Waypoint, at time.Time) ([]ImportedTrip, int) {
	waypoints = slices.Clone(waypoints)
	slices.SortStableFunc(waypoints, func(a, b volvoportal.Waypoint) int { return a.At.Compare(b.At) })
	var out []ImportedTrip
	invalid := 0
	for _, r := range rows {
		trip, ok := convertPortalTrip(r, at)
		if !ok {
			invalid++
			continue
		}
		out = append(out, ImportedTrip{Trip: trip, Track: track(waypoints, r.Start.Add(-trackMargin), r.End.Add(trackMargin))})
	}
	return out, invalid
}

// convertPortalTrip converts a trip of the portal. Its ID is its start, in milliseconds:
// the portal gives none, and a vehicle starts one trip at a time. Odometers and
// distance are in meters; fuel and regeneration are left out, the portal giving 0.
// An energy of 0 over a distance is the portal's lack of a figure, not a trip.
func convertPortalTrip(r volvoportal.Trip, at time.Time) (core.Trip, bool) {
	if r.End.Before(r.Start) {
		return core.Trip{}, false
	}
	from, fromOK := position(r.StartLat, r.StartLon)
	to, toOK := position(r.EndLat, r.EndLon)
	if !fromOK || !toOK {
		return core.Trip{}, false
	}
	trip := core.Trip{
		DetectedAt: at.UTC(), Start: exact(r.Start), End: exact(r.End), Sources: []string{SourceVolvoDataPortal},
		Import: core.Some(core.Import{
			Source: SourceVolvoDataPortal, SourceID: strconv.FormatInt(r.Start.UnixMilli(), 10), ImportedAt: at,
		}, at),
		From: from, To: to,
	}
	var ok bool
	for _, km := range []struct {
		m *float64
		v *core.Value[float64]
	}{{r.StartOdometerM, &trip.StartOdometerKm}, {r.EndOdometerM, &trip.EndOdometerKm}, {r.DistanceM, &trip.DistanceKm}} {
		if *km.v, ok = nonNegative(kilometers(km.m), at); !ok {
			return core.Trip{}, false
		}
	}
	if trip.EnergyKWh, ok = nonNegative(r.EnergyKWh, at); !ok {
		return core.Trip{}, false
	}
	if trip.EnergyKWh.OK && trip.EnergyKWh.V == 0 && trip.DistanceKm.OK && trip.DistanceKm.V > 0 {
		trip.EnergyKWh = core.Value[float64]{}
	}
	return trip, true
}

func kilometers(m *float64) *float64 {
	if m == nil {
		return nil
	}
	km := *m / 1000
	return &km
}

// position is the point given by both coordinates, absent when either is; false when
// out of bounds.
func position(lat, lon *float64) (core.Value[core.Position], bool) {
	if lat == nil || lon == nil {
		return core.Value[core.Position]{}, true
	}
	if !inBounds(*lat, *lon) {
		return core.Value[core.Position]{}, false
	}
	return core.Value[core.Position]{V: core.Position{Lat: *lat, Lon: *lon}, OK: true}, true
}

func inBounds(lat, lon float64) bool {
	return finite(lat) && finite(lon) && math.Abs(lat) <= 90 && math.Abs(lon) <= 180
}

// track is the GeoJSON LineString of the waypoints between from and to, sorted by
// time, those out of bounds left out; nil with fewer than two. Written by hand: the
// importer knows no JSON, and a LineString is numbers only.
func track(waypoints []volvoportal.Waypoint, from, to time.Time) []byte {
	i, _ := slices.BinarySearchFunc(waypoints, from, func(w volvoportal.Waypoint, t time.Time) int { return w.At.Compare(t) })
	b := []byte(`{"type":"LineString","coordinates":[`)
	n := 0
	for ; i < len(waypoints) && !waypoints[i].At.After(to); i++ {
		w := waypoints[i]
		if !inBounds(w.Lat, w.Lon) {
			continue
		}
		if n > 0 {
			b = append(b, ',')
		}
		b = append(b, '[')
		b = strconv.AppendFloat(b, w.Lon, 'f', -1, 64)
		b = append(b, ',')
		b = strconv.AppendFloat(b, w.Lat, 'f', -1, 64)
		b = append(b, ']')
		n++
	}
	if n < 2 {
		return nil
	}
	return append(b, "]}"...)
}
