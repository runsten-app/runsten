package volvoportal

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// testdata/trips.csv and waypoints.csv are the samples an owner published, their dates
// made consistent (they had edited some), one header broken over two lines as it was
// shown. testdata/portal holds a made-up answer of the portal, in its exact format: one
// ZIP per chunk holding a data.csv (zipped as the portal does), rows out of order, a
// trip whose waypoints are in a ZIP of another name, metadata.json and final.zip.

func fromDisk(t *testing.T, paths ...string) []File {
	t.Helper()
	var files []File
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // a fixture's name
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, File{Name: filepath.Base(p), R: bytes.NewReader(data), Size: int64(len(data))})
	}
	return files
}

func fromText(name, text string) File {
	return File{Name: name, R: strings.NewReader(text), Size: int64(len(text))}
}

// zipped is a ZIP of the members, name then contents.
func zipped(t *testing.T, members ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for ; len(members) >= 2; members = members[2:] {
		w, err := z.Create(members[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(members[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func fromZip(t *testing.T, name string, members ...string) File {
	t.Helper()
	data := zipped(t, members...)
	return File{Name: name, R: bytes.NewReader(data), Size: int64(len(data))}
}

func TestReadCSVs(t *testing.T) {
	// The waypoints first: the files are told apart by their header.
	ex, err := ReadFiles(fromDisk(t, "testdata/waypoints.csv", "testdata/trips.csv"))
	if err != nil || ex.Invalid != 0 || len(ex.Trips) != 3 || len(ex.Waypoints) != 3 {
		t.Fatalf("%d trips, %d waypoints, %d invalid, %v", len(ex.Trips), len(ex.Waypoints), ex.Invalid, err)
	}
	first := ex.Trips[0]
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
	last := ex.Waypoints[2]
	if !last.At.Equal(time.Date(2025, 10, 13, 7, 18, 27, 813e6, time.UTC)) || last.Lat != 35.34759426666667 ||
		last.Lon != 23.454243411111111 || last.OdometerM == nil || *last.OdometerM != 5094000 ||
		last.SpeedKmh == nil || *last.SpeedKmh != 11 {
		t.Errorf("%+v", last)
	}
}

func TestReadPortalAnswer(t *testing.T) {
	paths, err := filepath.Glob("testdata/portal/*")
	if err != nil || len(paths) != 6 {
		t.Fatalf("%v %v", paths, err)
	}
	ex, err := ReadFiles(fromDisk(t, paths...))
	if err != nil || ex.Invalid != 0 || len(ex.Trips) != 3 || len(ex.Waypoints) != 17 {
		t.Fatalf("%d trips, %d waypoints, %d invalid, %v", len(ex.Trips), len(ex.Waypoints), ex.Invalid, err)
	}
	// The same files zipped together, as an archiver of the downloads would.
	var members []string
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // a fixture's name
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, "Downloads/"+filepath.Base(p), string(data))
	}
	members = append(members, "__MACOSX/Downloads/._data.csv", "\x00\x05\x16\x07", "Downloads/", "")
	all, err := ReadFiles([]File{fromZip(t, "Archive.zip", members...)})
	if err != nil || len(all.Trips) != 3 || len(all.Waypoints) != 17 {
		t.Errorf("zipped together: %d trips, %d waypoints, %v", len(all.Trips), len(all.Waypoints), err)
	}
}

func TestReadTolerates(t *testing.T) {
	// Reordered, in another case, a byte order mark, columns missing or added, cells
	// empty or spaced, CRLF line ends.
	ex, err := ReadFiles([]File{fromText("trips.csv", "\ufeffEND TIME, start  time ,Distance (meters),Extra\r\n"+
		"2025-10-13T07:53:02Z,2025-10-13T07:17:26+02:00, ,x\r\n"+
		"2025-10-13T08:53:02Z,2025-10-13T08:17:26Z\n")})
	if err != nil || ex.Invalid != 0 || len(ex.Trips) != 2 {
		t.Fatalf("%d trips, %d invalid, %v", len(ex.Trips), ex.Invalid, err)
	}
	if trip := ex.Trips[0]; trip.DistanceM != nil || trip.StartLat != nil || trip.EnergyKWh != nil ||
		!trip.Start.Equal(time.Date(2025, 10, 13, 5, 17, 26, 0, time.UTC)) {
		t.Errorf("%+v", trip)
	}
}

func TestReadInvalidRows(t *testing.T) {
	ex, err := ReadFiles([]File{
		fromText("trips.csv", "Start time,End time,Distance (meters)\n"+
			"13/10/2025 07:17,2025-10-13T07:53:02Z,1\n"+ // a time not RFC 3339
			"2025-10-13T07:17:26Z,,1\n"+ // no end
			"2025-10-13T07:17:26Z,2025-10-13T07:53:02Z,12 km\n"+ // not a number
			"2025-10-13T07:17:26Z,2025-10-13T07:53:02Z,12000\n"),
		fromText("waypoints.csv", "Timestamp,Latitude,Longitude,Speed (kph)\n"+
			"2025-10-13T07:17:26Z,,23.4,0\n"+ // no latitude
			"2025-10-13T07:17:26Z,35.3,23.4,fast\n"+
			"2025-10-13T07:17:26Z,35.3,23.4,\n"),
	})
	// The waypoints left out are not counted: they only thin a track.
	if err != nil || ex.Invalid != 3 || len(ex.Trips) != 1 || len(ex.Waypoints) != 1 ||
		ex.Waypoints[0].SpeedKmh != nil || ex.Waypoints[0].OdometerM != nil {
		t.Errorf("%+v, %v", ex, err)
	}
}

func TestReadFormatErrors(t *testing.T) {
	trips := "Start time,End time\n2025-10-13T07:17:26Z,2025-10-13T07:53:02Z\n"
	for name, c := range map[string]struct {
		files []File
		want  string
	}{
		"nothing":        {nil, "no trips' file"},
		"empty":          {[]File{fromText("data.csv", "")}, "data.csv: empty"},
		"another file":   {[]File{fromText("x.csv", trips), fromText("app.csv", "Category,Started\nx,y\n")}, "app.csv: neither"},
		"waypoints only": {[]File{fromText("w.csv", "Timestamp,Latitude,Longitude\n2025-10-13T07:17:26Z,35,23\n")}, "no trips' file"},
		"not CSV":        {[]File{fromText("t.csv", "Start time,End time\n\"2025-10-13T07:17:26Z,2025\"x\n")}, "t.csv: "},
		"no end column":  {[]File{fromText("t.csv", "Start time\n2025-10-13T07:17:26Z\n")}, "neither"},
		"semicolons":     {[]File{fromText("t.csv", "Start time;End time\n")}, "neither"},
		"only JSON":      {[]File{fromText("metadata.json", "{}"), fromZip(t, "final.zip", "x.json", "[]")}, "no trips' file"},
		"a broken ZIP":   {[]File{fromText("a.zip", "PK\x03\x04 not a zip")}, "a.zip: zip"},
		"a bad member": {
			[]File{fromZip(t, "a.zip", "data.csv", "Category\nx\n")}, "a.zip/data.csv: neither",
		},
		"ZIPs too deep": {
			// A ZIP in a ZIP in a ZIP is not read: no trips.
			[]File{fromZip(t, "a.zip", "b.zip", string(zipped(t, "c.zip", string(zipped(t, "data.csv", trips)))))}, "no trips' file",
		},
	} {
		_, err := ReadFiles(c.files)
		var fe *FormatError
		if !errors.As(err, &fe) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	failing := File{Name: "t.csv", R: readerAt{iotest.ErrReader(errors.New("boom"))}, Size: 10}
	var fe *FormatError
	if _, err := ReadFiles([]File{failing}); err == nil || errors.As(err, &fe) {
		t.Errorf("a failing reader: %v", err)
	}
}

type readerAt struct {
	r interface{ Read([]byte) (int, error) }
}

func (r readerAt) ReadAt(p []byte, _ int64) (int, error) { return r.r.Read(p) }

func TestReadInflatedBound(t *testing.T) {
	defer func(n int64) { maxInflated = n }(maxInflated)
	maxInflated = 1 << 10
	big := "Timestamp,Latitude,Longitude\n" + strings.Repeat("2025-10-13T07:17:26Z,35,23\n", 100)
	trips := "Start time,End time\n2025-10-13T07:17:26Z,2025-10-13T07:53:02Z\n"
	for name, files := range map[string][]File{
		"one member": {fromZip(t, "a.zip", "data.csv", big)},
		"together":   {fromZip(t, "a.zip", "data.csv", big[:700]), fromZip(t, "b.zip", "data.csv", big[:700])},
		"nested":     {fromZip(t, "all.zip", "b.zip", string(zipped(t, "data.csv", big)))},
	} {
		_, err := ReadFiles(append(files, fromText("t.csv", trips)))
		if err == nil || !strings.Contains(err.Error(), "once inflated") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ReadFiles([]File{fromZip(t, "a.zip", "data.csv", trips)}); err != nil {
		t.Errorf("within the bound: %v", err)
	}
}

func TestReadMaxRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("Start time,End time\n")
	for range MaxRows + 1 {
		b.WriteString("x,y\n")
	}
	_, err := ReadFiles([]File{fromText("t.csv", b.String())})
	var fe *FormatError
	if !errors.As(err, &fe) {
		t.Errorf("%v", err)
	}
}
