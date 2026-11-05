package aaoslogging

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "write testdata/export.zip")

// The export is synthetic, written as the app's Drive sync V3 and trip schema 17 are
// documented (github.com/devmcage/aaos-drive, docs/ATTRIBUTES.md), as the developer's
// own fixtures: a month archive of September, with a README beside its day file, and a
// day archive of October 4, in a car's folder as Google Drive zips it.

const (
	root  = "AAOSLogging/Volvo EX30_veh-1/Sync/"
	start = int64(1791090000000) // 2026-10-04T05:40:00Z
)

func dayDoc(date, dataset string, trips ...map[string]any) []byte {
	return jsonOf(map[string]any{
		"format": "aaos-triplog-day", "formatVersion": 1, "tripSchemaVersion": 17, "date": date,
		"bucketTimeZone": "Europe/Stockholm", "timestampUnit": "unix_ms", "storageUnitSystem": "metric",
		"powerSignConvention": "positive_consumption_negative_regeneration", "datasetId": dataset,
		"carName": "Volvo EX30", "generatedAt": start + 120000, "trips": trips,
	})
}

func trip(id string, at int64) map[string]any {
	sample := func(dt int64, soc, rangeKm, temp any) map[string]any {
		return map[string]any{
			"recordedAt": at + dt, "speedKph": 50.0, "powerKw": 12.5, "batteryPercent": soc, "rangeKm": rangeKm,
			"vehicleOutsideTemperatureC": temp, "tirePressureKpaLeftFront": 250.0, "chargeState": 3,
		}
	}
	return map[string]any{
		"tripId": id, "localId": 1, "startedAt": at, "endedAt": at + 600000, "storageUnitSystem": "metric",
		"distanceKm": 8.4, "energyKwh": 1.35, "energySource": "vehicle_sensor", "averageSpeedKph": 50.0,
		"startLat": 57.7, "startLon": 11.97, "endLat": 57.72, "endLon": 12.0, "weather": nil,
		"weatherSamples": []any{map[string]any{"temperatureC": 9.0, "capturedAt": at}},
		// Out of order, as nothing in the contract sorts them; one sample without the SoC.
		"telemetry": []any{
			sample(300000, 79.0, 300.0, 8.0), sample(0, nil, 310.0, 7.0), sample(1000, 80.0, 309.0, 7.5),
			sample(599000, 78.0, 296.0, 8.5),
		},
		"route": []any{
			map[string]any{"recordedAt": at, "latitude": 57.7, "longitude": 11.97},
			map[string]any{"recordedAt": at + 300000, "latitude": 57.71, "longitude": 11.99},
			map[string]any{"recordedAt": at + 400000, "latitude": nil, "longitude": nil},
			map[string]any{"recordedAt": at + 600000, "latitude": 57.72, "longitude": 12.0},
		},
	}
}

func jsonOf(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func zipOf(files map[string][]byte, order ...string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range order {
		f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			panic(err)
		}
		if _, err := f.Write(files[name]); err != nil {
			panic(err)
		}
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// export builds a car's folder; edit changes the manifest before it is written.
func export(seq int, edit func(m map[string]any)) map[string][]byte {
	files := map[string][]byte{}
	var artifacts []any
	add := func(key, kind, period, ext string, data []byte, trips int) {
		sum := sha256.Sum256(data)
		name := fmt.Sprintf("%s-%s-%s.%s", strings.ReplaceAll(kind, "_", "-"), period, hex.EncodeToString(sum[:8]), ext)
		files[root+"V3/Artifacts/"+name] = data
		mime := "application/json"
		if ext == "zip" {
			mime = "application/zip"
		}
		artifacts = append(artifacts, map[string]any{
			"logicalKey": key, "logicalType": kind, "period": period, "relativePath": "V3/Artifacts/" + name,
			"mimeType": mime, "size": len(data), "sha256": hex.EncodeToString(sum[:]), "tripCount": trips,
		})
	}
	sept := start - 32*24*3600*1000
	add("archive:2026-09", "month_archive", "2026-09", "zip", zipOf(map[string][]byte{
		"days/2026-09-02.json": dayDoc("2026-09-02", "dataset-a", trip("dataset-a-1", sept)),
		"README.md":            []byte("AAOS Logging"),
	}, "README.md", "days/2026-09-02.json"), 1)
	unfinished := trip("dataset-a-3", start+3600000)
	unfinished["endedAt"] = nil
	add("day:2026-10-04", "day_archive", "2026-10-04", "zip", zipOf(map[string][]byte{
		"2026-10-04.json": dayDoc("2026-10-04", "dataset-a", trip("dataset-a-2", start), unfinished),
	}, "2026-10-04.json"), 2)
	add("month:2026-10", "month_index", "2026-10", "json", jsonOf(map[string]any{"format": "aaos-triplog-generation-month"}), 2)
	add("catalog", "catalog", "", "json", jsonOf(map[string]any{"format": "aaos-triplog-generation-catalog"}), 3)
	gen := fmt.Sprintf("g%d-fixture", seq)
	m := map[string]any{
		"format": "aaos-triplog-drive-generation", "formatVersion": 3, "tripSchemaVersion": 17,
		"datasetId": "dataset-a", "vehicleId": "veh-1", "generationId": gen, "generationSequence": seq,
		"generatedAt": start + 120000, "timeZone": "Europe/Stockholm", "totalTripCount": 3,
		"latestTripEndTimestamp": start + 600000, "artifacts": artifacts,
	}
	if edit != nil {
		edit(m)
	}
	files[root+fmt.Sprintf("V3/Commits/commit-%020d-%s.json", seq, gen)] = jsonOf(m)
	return files
}

func folder(files map[string][]byte) []byte {
	return zipOf(files, slices.Sorted(maps.Keys(files))...)
}

func read(b []byte) (*Export, error) { return Read(bytes.NewReader(b), int64(len(b))) }

func TestFixture(t *testing.T) {
	b := folder(export(2, nil))
	if *update {
		if err := os.WriteFile("testdata/export.zip", b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile("testdata/export.zip")
	if err != nil || !bytes.Equal(got, b) {
		t.Fatalf("testdata/export.zip is not the fixture (%v): go test ./internal/aaoslogging -run TestFixture -update", err)
	}
}

func TestRead(t *testing.T) {
	// An older generation beside the newest, as Drive keeps them: only the newest is read.
	files := export(2, nil)
	for name, data := range export(1, func(m map[string]any) { m["totalTripCount"] = 0; m["artifacts"] = []any{} }) {
		if strings.Contains(name, "/Commits/") {
			files[name] = data
		}
	}
	exp, err := read(folder(files))
	if err != nil {
		t.Fatal(err)
	}
	if exp.VehicleID != "veh-1" || exp.DatasetID != "dataset-a" || exp.Generation != 2 || len(exp.Trips) != 3 {
		t.Fatalf("%+v", exp)
	}
	if exp.Trips[0].ID != "dataset-a-1" || exp.Trips[1].ID != "dataset-a-2" || exp.Trips[2].ID != "dataset-a-3" {
		t.Errorf("not sorted by start: %s %s %s", exp.Trips[0].ID, exp.Trips[1].ID, exp.Trips[2].ID)
	}
	tr := exp.Trips[1]
	if !tr.Start.Equal(time.UnixMilli(start)) || tr.End == nil || !tr.End.Equal(time.UnixMilli(start+600000)) ||
		*tr.DistanceKm != 8.4 || *tr.EnergyKWh != 1.35 || tr.EnergySource != "vehicle_sensor" ||
		*tr.StartLat != 57.7 || *tr.EndLon != 12.0 {
		t.Errorf("%+v", tr)
	}
	if len(tr.Telemetry) != 4 || tr.Telemetry[1].BatteryPercent != nil || *tr.Telemetry[1].RangeKm != 310 ||
		*tr.Telemetry[3].OutsideTempC != 8.5 {
		t.Errorf("telemetry %+v", tr.Telemetry)
	}
	if len(tr.Route) != 3 || tr.Route[2].Lat != 57.72 {
		t.Errorf("route %+v", tr.Route)
	}
	if exp.Trips[2].End != nil {
		t.Errorf("the unfinished trip ends at %v", exp.Trips[2].End)
	}
}

// The ZIP of the Sync folder alone, or of V3, is read the same.
func TestReadSubfolder(t *testing.T) {
	for _, prefix := range []string{"Sync/", ""} {
		files := map[string][]byte{}
		for name, data := range export(2, nil) {
			files[prefix+strings.TrimPrefix(name, root)] = data
		}
		if exp, err := read(folder(files)); err != nil || len(exp.Trips) != 3 {
			t.Errorf("%q: %v", prefix, err)
		}
	}
}

func TestReadRefused(t *testing.T) {
	without := func(match string) map[string][]byte {
		files := export(2, nil)
		for name := range files {
			if strings.Contains(name, match) {
				delete(files, name)
			}
		}
		return files
	}
	twoCars := export(2, nil)
	for name, data := range export(2, nil) {
		twoCars[strings.Replace(name, "Volvo EX30_veh-1", "Polestar 2_veh-2", 1)] = data
	}
	tampered := export(2, nil)
	for name, data := range tampered {
		if strings.Contains(name, "day-archive") {
			// The same size, another byte: only the SHA-256 tells.
			data = bytes.Clone(data)
			data[len(data)/2] ^= 1
			tampered[name] = data
		}
	}
	for name, files := range map[string]map[string][]byte{
		"no commit":       without("/Commits/"),
		"archive missing": without("day-archive"),
		"two vehicles":    twoCars,
		"tampered":        tampered,
		"wrong count":     export(2, func(m map[string]any) { m["totalTripCount"] = 4 }),
		"wrong format":    export(2, func(m map[string]any) { m["formatVersion"] = 2 }),
		"wrong name":      export(2, func(m map[string]any) { m["generationId"] = "other" }),
		"bad path": export(2, func(m map[string]any) {
			m["artifacts"].([]any)[0].(map[string]any)["relativePath"] = "../../etc/passwd"
		}),
		"another dataset": export(2, func(m map[string]any) { m["datasetId"] = "dataset-b" }),
	} {
		_, err := read(folder(files))
		var fe *FormatError
		if !errors.As(err, &fe) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := read([]byte("PK not a zip")); err == nil {
		t.Error("not a ZIP: read")
	}
}
