package volvoportal

import (
	"errors"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// The fixtures are the samples an owner published, their dates made consistent (they
// had edited some), one header broken over two lines as it was shown, CRLF in the trips'.

func TestReadTrips(t *testing.T) {
	f, err := os.Open("testdata/trips.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	trips, invalid, err := ReadTrips(f)
	if err != nil || invalid != 0 || len(trips) != 3 {
		t.Fatalf("%d trips, %d invalid, %v", len(trips), invalid, err)
	}
	first := trips[0]
	if !first.Start.Equal(time.Date(2025, 10, 13, 7, 17, 26, 495e6, time.UTC)) ||
		!first.End.Equal(time.Date(2025, 10, 13, 7, 53, 2, 677e6, time.UTC)) {
		t.Errorf("times %v %v", first.Start, first.End)
	}
	for name, got := range map[string]struct {
		v    *float64
		want float64
	}{
		"start latitude": {first.StartLat, 35.34558666666667}, "end longitude": {first.EndLon, 23.45352472222222},
		"start odometer": {first.StartOdometerM, 5094000}, "end odometer": {first.EndOdometerM, 5106000},
		"distance": {first.DistanceM, 12000}, "fuel": {first.FuelL, 0}, "energy": {first.EnergyKWh, 1.824},
		"regeneration": {first.RegenerationKWh, 0},
	} {
		if got.v == nil || *got.v != got.want {
			t.Errorf("%s: %v, want %v", name, got.v, got.want)
		}
	}
}

func TestReadWaypoints(t *testing.T) {
	f, err := os.Open("testdata/waypoints.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ws, invalid, err := ReadWaypoints(f)
	if err != nil || invalid != 0 || len(ws) != 3 {
		t.Fatalf("%d waypoints, %d invalid, %v", len(ws), invalid, err)
	}
	last := ws[2]
	if !last.At.Equal(time.Date(2025, 10, 13, 7, 18, 27, 813e6, time.UTC)) || last.Lat != 35.34759426666667 ||
		last.Lon != 23.454243411111111 || last.OdometerM == nil || *last.OdometerM != 5094000 ||
		last.SpeedKmh == nil || *last.SpeedKmh != 11 {
		t.Errorf("%+v", last)
	}
}

func TestReadTolerates(t *testing.T) {
	// Reordered, in another case, a byte order mark, columns missing or added, cells
	// empty or spaced.
	trips, invalid, err := ReadTrips(strings.NewReader("\ufeffEND TIME, start  time ,Distance (meters),Extra\n" +
		"2025-10-13T07:53:02Z,2025-10-13T07:17:26+02:00, ,x\n" +
		"2025-10-13T08:53:02Z,2025-10-13T08:17:26Z\n"))
	if err != nil || invalid != 0 || len(trips) != 2 {
		t.Fatalf("%d trips, %d invalid, %v", len(trips), invalid, err)
	}
	if trips[0].DistanceM != nil || trips[0].StartLat != nil || trips[0].EnergyKWh != nil ||
		!trips[0].Start.Equal(time.Date(2025, 10, 13, 5, 17, 26, 0, time.UTC)) {
		t.Errorf("%+v", trips[0])
	}
}

func TestReadInvalidRows(t *testing.T) {
	trips, invalid, err := ReadTrips(strings.NewReader("Start time,End time,Distance (meters)\n" +
		"13/10/2025 07:17,2025-10-13T07:53:02Z,1\n" + // a time not RFC 3339
		"2025-10-13T07:17:26Z,,1\n" + // no end
		"2025-10-13T07:17:26Z,2025-10-13T07:53:02Z,12 km\n" + // not a number
		"2025-10-13T07:17:26Z,2025-10-13T07:53:02Z,12000\n"))
	if err != nil || invalid != 3 || len(trips) != 1 {
		t.Errorf("%d trips, %d invalid, %v", len(trips), invalid, err)
	}
	ws, invalid, err := ReadWaypoints(strings.NewReader("Timestamp,Latitude,Longitude,Speed (kph)\n" +
		"2025-10-13T07:17:26Z,,23.4,0\n" + // no latitude
		"2025-10-13T07:17:26Z,35.3,23.4,fast\n" +
		"2025-10-13T07:17:26Z,35.3,23.4,\n"))
	if err != nil || invalid != 2 || len(ws) != 1 || ws[0].SpeedKmh != nil || ws[0].OdometerM != nil {
		t.Errorf("%+v, %d invalid, %v", ws, invalid, err)
	}
}

func TestReadFormatErrors(t *testing.T) {
	for name, file := range map[string]string{
		"empty":            "",
		"another file":     "Category,Started,Stopped\nx,y,z\n",
		"waypoints":        "Timestamp,Latitude,Longitude\n2025-10-13T07:17:26Z,35,23\n",
		"not CSV":          "Start time,End time\n\"2025-10-13T07:17:26Z,2025\"x\n",
		"no end column":    "Start time\n2025-10-13T07:17:26Z\n",
		"semicolons (app)": "Start time;End time\n",
	} {
		_, _, err := ReadTrips(strings.NewReader(file))
		var fe *FormatError
		if !errors.As(err, &fe) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, _, err := ReadWaypoints(strings.NewReader("Start time,End time\n"))
	var fe *FormatError
	if !errors.As(err, &fe) || !strings.Contains(err.Error(), `"timestamp"`) {
		t.Errorf("trips as waypoints: %v", err)
	}
	if _, _, err := ReadTrips(iotest.ErrReader(errors.New("boom"))); err == nil || errors.As(err, &fe) {
		t.Errorf("a failing reader: %v", err)
	}
}

func TestReadMaxRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("Timestamp,Latitude,Longitude\n")
	for range MaxRows + 1 {
		b.WriteString("x,1,2\n")
	}
	_, _, err := ReadWaypoints(strings.NewReader(b.String()))
	var fe *FormatError
	if !errors.As(err, &fe) {
		t.Errorf("%v", err)
	}
}
