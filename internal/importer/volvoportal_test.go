package importer

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/volvoportal"
)

func TestConvertPortalTrip(t *testing.T) {
	start := time.Date(2025, 10, 13, 9, 17, 26, 495e6, time.FixedZone("CEST", 2*3600))
	end := start.Add(36 * time.Minute)
	f := func(v float64) *float64 { return &v }
	valid := func() volvoportal.Trip {
		return volvoportal.Trip{
			Start: start, End: end, StartLat: f(35.3), StartLon: f(23.4), EndLat: f(35.4), EndLon: f(23.5),
			StartOdometerM: f(5094000), EndOdometerM: f(5106000), DistanceM: f(12000),
			FuelL: f(0), EnergyKWh: f(1.824), RegenerationKWh: f(0),
		}
	}
	trip, ok := convertPortalTrip(valid(), t0)
	if !ok || trip.Start.After != start.UTC() || trip.Start.Before != start.UTC() || trip.End.After != end.UTC() ||
		trip.Start.After.Location() != time.UTC || trip.DetectedAt != t0 ||
		trip.Import.V.Source != SourceVolvoDataPortal || trip.Import.V.SourceID != "1760339846495" ||
		trip.Sources[0] != SourceVolvoDataPortal || trip.StartOdometerKm.V != 5094 || trip.EndOdometerKm.V != 5106 ||
		trip.DistanceKm.V != 12 || trip.EnergyKWh.V != 1.824 || trip.From.V != (core.Position{Lat: 35.3, Lon: 23.4}) ||
		trip.To.V != (core.Position{Lat: 35.4, Lon: 23.5}) {
		t.Errorf("%+v", trip)
	}
	if bare, ok := convertPortalTrip(volvoportal.Trip{Start: start, End: start}, t0); !ok ||
		bare.DistanceKm.OK || bare.EnergyKWh.OK || bare.From.OK || bare.To.OK || bare.StartOdometerKm.OK {
		t.Errorf("without values: %+v", bare)
	}
	noEnergy := valid()
	noEnergy.EnergyKWh = f(0)
	if trip, ok := convertPortalTrip(noEnergy, t0); !ok || trip.EnergyKWh.OK {
		t.Errorf("0 kWh over 12 km: %+v", trip.EnergyKWh)
	}
	standing := valid()
	standing.EnergyKWh, standing.DistanceM = f(0), f(0)
	if trip, ok := convertPortalTrip(standing, t0); !ok || !trip.EnergyKWh.OK {
		t.Errorf("0 kWh over 0 km: %+v", trip.EnergyKWh)
	}
	halfPosition := valid()
	halfPosition.EndLon = nil
	if trip, ok := convertPortalTrip(halfPosition, t0); !ok || trip.To.OK || !trip.From.OK {
		t.Errorf("a latitude alone: %+v", trip)
	}
	for name, change := range map[string]func(*volvoportal.Trip){
		"ends before":  func(t *volvoportal.Trip) { t.End = start.Add(-time.Second) },
		"a latitude":   func(t *volvoportal.Trip) { t.StartLat = f(91) },
		"a longitude":  func(t *volvoportal.Trip) { t.EndLon = f(-181) },
		"a distance":   func(t *volvoportal.Trip) { t.DistanceM = f(-1) },
		"an odometer":  func(t *volvoportal.Trip) { t.StartOdometerM = f(-1) },
		"an energy":    func(t *volvoportal.Trip) { t.EnergyKWh = f(-0.1) },
		"not a number": func(t *volvoportal.Trip) { t.EndOdometerM = f(math.Inf(1)) },
	} {
		trip := valid()
		change(&trip)
		if _, ok := convertPortalTrip(trip, t0); ok {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestPortalTracks(t *testing.T) {
	start := time.Date(2025, 10, 13, 7, 17, 26, 0, time.UTC)
	f := func(v float64) *float64 { return &v }
	w := func(d time.Duration, lat, lon float64) volvoportal.Waypoint {
		return volvoportal.Waypoint{At: start.Add(d), Lat: lat, Lon: lon}
	}
	rows := []volvoportal.Trip{
		{Start: start, End: start.Add(3 * time.Minute)},
		{Start: start.Add(time.Hour), End: start.Add(time.Hour + time.Minute), StartLat: f(1), StartLon: f(2)},
		{Start: start.Add(2 * time.Hour), End: start.Add(time.Hour)}, // invalid
	}
	// Unsorted; one before the margin, one in it, one out of bounds, one after the end.
	waypoints := []volvoportal.Waypoint{
		w(2*time.Minute, 35.3, 23.5), w(-23*time.Second, 35.3, 23.4), w(-2*time.Minute, 0, 0),
		w(time.Minute, 35.3, 23.45), w(90*time.Second, 99, 23), w(5*time.Minute, 1, 1),
		w(time.Hour, 1, 2), // the second trip's only one
	}
	trips, invalid := convertPortalTrips(rows, waypoints, t0)
	if invalid != 1 || len(trips) != 2 {
		t.Fatalf("%d trips, %d invalid", len(trips), invalid)
	}
	want := `{"type":"LineString","coordinates":[[23.4,35.3],[23.45,35.3],[23.5,35.3]]}`
	if string(trips[0].Track) != want {
		t.Errorf("track %s", trips[0].Track)
	}
	if trips[1].Track != nil {
		t.Errorf("a single waypoint: %s", trips[1].Track)
	}
	if waypoints[0].At != start.Add(2*time.Minute) {
		t.Error("the caller's waypoints were sorted")
	}
}

// saver records what is saved, the cutoff left to its caller's test.
type saver struct {
	trips []ImportedTrip
	err   error
}

func (s *saver) SaveImport(_ context.Context, accountID, vehicleID string, trips []ImportedTrip, charges []core.Charge) (Result, error) {
	if s.err != nil {
		return Result{}, s.err
	}
	if accountID != "acc" || vehicleID != "veh" || charges != nil {
		return Result{}, errors.New("unexpected call")
	}
	s.trips = trips
	return Result{Saved: len(trips) - 1, Skipped: 1, Cutoff: cutoff}, nil
}

func TestFilesVolvoDataPortal(t *testing.T) {
	trips := "Start time,End time,Distance (meters)\n" +
		"2025-10-13T07:17:26Z,2025-10-13T07:53:02Z,12000\n" +
		"2025-10-13T08:17:26Z,2025-10-13T08:53:02Z,-1\n" + // refused by the conversion
		"2025-10-14T07:17:26Z,2025-10-14T07:53:02Z,1000\n" +
		"yesterday,2025-10-14T07:53:02Z,1\n" // refused by the reader
	waypoints := "Timestamp,Latitude,Longitude\n" +
		"2025-10-13T07:18:00Z,35.3,23.4\n2025-10-13T07:19:00Z,35.4,23.5\nnow,1,2\n"
	s := &saver{}
	files := NewFiles(s, clock.NewManual(t0))
	ctx := context.Background()
	rep, err := files.VolvoDataPortal(ctx, "acc", "veh", portalFiles(waypoints, trips))
	if err != nil || rep.Saved != 1 || rep.Skipped != 1 || rep.Invalid != 2 || rep.Cutoff != cutoff || len(s.trips) != 2 {
		t.Fatalf("%+v, %v", rep, err)
	}
	if s.trips[0].Track == nil || s.trips[1].Track != nil || s.trips[0].Trip.DetectedAt != t0 {
		t.Errorf("%+v", s.trips)
	}

	if _, err := files.VolvoDataPortal(ctx, "acc", "veh", portalFiles(trips)); err != nil || s.trips[0].Track != nil {
		t.Errorf("without waypoints: %v, %s", err, s.trips[0].Track)
	}
	// The same trips twice, as two of the portal's chunks may hold them, are written once;
	// the rows the reader refused are counted in each.
	if rep, err := files.VolvoDataPortal(ctx, "acc", "veh", portalFiles(trips, trips)); err != nil ||
		len(s.trips) != 2 || rep.Invalid != 3 {
		t.Errorf("twice: %+v, %d trips, %v", rep, len(s.trips), err)
	}
	var fe *volvoportal.FormatError
	if _, err := files.VolvoDataPortal(ctx, "acc", "veh", portalFiles(waypoints)); !errors.As(err, &fe) {
		t.Errorf("waypoints alone: %v", err)
	}
	boom := errors.New("boom")
	s.err = boom
	if _, err := files.VolvoDataPortal(ctx, "acc", "veh", portalFiles(trips)); !errors.Is(err, boom) {
		t.Errorf("the store failing: %v", err)
	}
}

func portalFiles(texts ...string) []volvoportal.File {
	files := make([]volvoportal.File, 0, len(texts))
	for i, text := range texts {
		files = append(files, volvoportal.File{Name: strconv.Itoa(i) + ".csv", R: strings.NewReader(text), Size: int64(len(text))})
	}
	return files
}

// TestFilesPortalAnswer imports the portal's made-up answer, as downloaded: every trip
// gets its track, a trip's waypoints being in a ZIP of another name.
func TestFilesPortalAnswer(t *testing.T) {
	paths, err := filepath.Glob("../volvoportal/testdata/portal/*")
	if err != nil || len(paths) == 0 {
		t.Fatalf("%v %v", paths, err)
	}
	var files []volvoportal.File
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // a fixture's name
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, volvoportal.File{Name: filepath.Base(p), R: bytes.NewReader(data), Size: int64(len(data))})
	}
	s := &saver{}
	rep, err := NewFiles(s, clock.NewManual(t0)).VolvoDataPortal(context.Background(), "acc", "veh", files)
	if err != nil || rep.Invalid != 0 || len(s.trips) != 3 {
		t.Fatalf("%+v, %d trips, %v", rep, len(s.trips), err)
	}
	for _, trip := range s.trips {
		if n := strings.Count(string(trip.Track), "],["); n < 4 {
			t.Errorf("trip of %v: %d points, %s", trip.Trip.Start.After, n+1, trip.Track)
		}
	}
}
