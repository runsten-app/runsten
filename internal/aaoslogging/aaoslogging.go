// Package aaoslogging reads the export of AAOS Logging, an Android Automotive app that
// records a car's trips and syncs them to its user's Google Drive: the ZIP Google Drive
// gives for the car's folder (AAOSLogging/<car>_<id>), for its Sync or V3 folder, or for
// the Commits and Artifacts folders of V3 chosen together. It
// returns the trips as they are, in the app's units, and knows no business code.
//
// The format is the app's Drive sync V3, documented by its developer's Home Assistant
// integration (github.com/devmcage/aaos-drive, docs/ATTRIBUTES.md): V3/Commits holds the
// generations' manifests, commit-<20-digit sequence>-<generationId>.json, the newest the
// current one; it lists the files of V3/Artifacts with their size and SHA-256, among them
// the day and month archives, ZIPs of YYYY-MM-DD.json day files. Only the newest
// generation is read, every archive it lists checked against it: an export synced
// halfway, or another generation's files, is refused rather than read in part.
package aaoslogging

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Trip is a trip of the export. Its times are the app's, to the millisecond.
type Trip struct {
	ID    string // the app's tripId, stable across generations
	Start time.Time
	End   *time.Time // nil while the trip is unfinished
	// DistanceKm is the app's, from the odometer, the GPS or the speed: not an odometer.
	DistanceKm *float64
	// EnergyKWh is the traction battery's net energy over the trip, regeneration
	// included: negative when it gained. EnergySource tells where it comes from:
	// vehicle_sensor, derived_battery_energy, mixed_vehicle_sensor_and_derived or
	// unknown (or empty).
	EnergyKWh    *float64
	EnergySource string
	// VehicleType is the app's class of the car on this trip: ELECTRIC, HYBRID, FUEL
	// or UNKNOWN; empty in the exports before it.
	VehicleType        string
	StartLat, StartLon *float64 // decimal degrees
	EndLat, EndLon     *float64
	Telemetry          []Sample // as recorded, not sorted
	Route              []Point  // as recorded, not sorted
}

// Sample is a telemetry sample of a trip: only the values Runsten reads of its 79.
type Sample struct {
	At             time.Time
	BatteryPercent *float64 // %, of the usable capacity
	RangeKm        *float64 // the range the car estimates
	OutsideTempC   *float64 // the car's outside temperature, °C
}

// Point is a position of a trip's route.
type Point struct {
	At       time.Time
	Lat, Lon float64 // decimal degrees
}

// Export is the newest generation of a vehicle's export.
type Export struct {
	VehicleID, DatasetID string // the app's identifiers: no VIN
	Generation           int64  // the generation's sequence
	Trips                []Trip // sorted by start
}

// FormatError is an upload that is not AAOS Logging's export, or one incomplete or
// inconsistent.
type FormatError struct{ Reason string }

func (e *FormatError) Error() string { return "not an export of AAOS Logging: " + e.Reason }

func formatErr(format string, a ...any) error { return &FormatError{Reason: fmt.Sprintf(format, a...)} }

// The bounds of what is read, as the developer's integration's: an artifact of at most
// 128 MiB, an archive of at most 10,000 files and 512 MiB once inflated. And an export of
// at most 1 GiB of day files once inflated, all its archives together: its trips are held
// in memory until the last is read, by a process that serves the rest of the API.
const (
	maxManifest      = 16 << 20
	maxArtifact      = 128 << 20
	maxArchiveFiles  = 10_000
	maxArchiveInflat = 512 << 20
)

// maxExportInflat is a variable for the tests alone.
var maxExportInflat uint64 = 1 << 30

var (
	commitName   = regexp.MustCompile(`^commit-(\d{20})-([A-Za-z0-9_-]+)\.json$`)
	artifactPath = regexp.MustCompile(`^V3/Artifacts/[A-Za-z0-9_.-]+\.(json|zip)$`)
	dayName      = regexp.MustCompile(`^(?:days/)?(\d{4}-\d{2}-\d{2})\.json$`)
	digest       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	periods      = map[string]*regexp.Regexp{
		"day_archive": regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`), "month_archive": regexp.MustCompile(`^\d{4}-\d{2}$`),
	}
)

// Read reads the export in the ZIP r, of size bytes: Google Drive's download of the
// vehicle's folder. A ZIP of several vehicles' folders is refused: which one is this
// vehicle, Runsten cannot tell.
func Read(r io.ReaderAt, size int64) (*Export, error) {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, formatErr("not a ZIP file")
	}
	files := make(map[string]*zip.File, len(z.File))
	type commit struct {
		seq  int64
		root string
		f    *zip.File
	}
	var newest []commit
	roots := map[string]bool{}
	for _, f := range z.File {
		name := strings.TrimPrefix(f.Name, "./")
		files[name] = f
		dir, base := path.Split(name)
		// root is the V3 folder, whose name is not in the ZIP when its Commits and
		// Artifacts are downloaded alone.
		root, ok := strings.CutSuffix(dir, "Commits/")
		if !ok || (root != "" && !strings.HasSuffix(root, "/")) {
			continue
		}
		m := commitName.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		roots[root] = true
		seq, _ := strconv.ParseInt(m[1], 10, 64)
		switch {
		case len(newest) == 0 || seq > newest[0].seq:
			newest = []commit{{seq, root, f}}
		case seq == newest[0].seq:
			newest = append(newest, commit{seq, root, f})
		}
	}
	switch {
	case len(roots) > 1:
		return nil, formatErr("it holds the folders of %d vehicles: download one vehicle's folder", len(roots))
	case len(newest) == 0:
		return nil, formatErr("no Commits/commit-….json: download the vehicle's folder, synced with AAOS Logging's Drive sync V3")
	case len(newest) > 1:
		return nil, formatErr("two commits of the same sequence")
	}
	head := newest[0]
	raw, err := readMember(head.f, maxManifest)
	if err != nil {
		return nil, err
	}
	man, err := parseManifest(raw, path.Base(head.f.Name))
	if err != nil {
		return nil, err
	}
	exp := &Export{VehicleID: man.VehicleID, DatasetID: man.DatasetID, Generation: man.GenerationSequence}
	seen := map[string]bool{}
	left := maxExportInflat
	for _, a := range man.Artifacts {
		if a.LogicalType != "day_archive" && a.LogicalType != "month_archive" {
			continue
		}
		f, ok := files[head.root+strings.TrimPrefix(a.RelativePath, "V3/")]
		if !ok {
			return nil, formatErr("%s is missing: download the vehicle's whole folder once AAOS Logging has synced it", a.RelativePath)
		}
		data, err := readMember(f, maxArtifact)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != a.Size {
			return nil, formatErr("%s: %d bytes, the commit says %d", a.RelativePath, len(data), a.Size)
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != a.SHA256 {
			return nil, formatErr("%s: its SHA-256 is not the commit's", a.RelativePath)
		}
		trips, err := readArchive(data, a, man.DatasetID, &left)
		if err != nil {
			return nil, err
		}
		for _, t := range trips {
			if seen[t.ID] {
				return nil, formatErr("trip %q twice", t.ID)
			}
			seen[t.ID] = true
		}
		exp.Trips = append(exp.Trips, trips...)
	}
	if int64(len(exp.Trips)) != man.TotalTripCount {
		return nil, formatErr("%d trips, the commit says %d", len(exp.Trips), man.TotalTripCount)
	}
	sortTrips(exp.Trips)
	return exp, nil
}

type manifest struct {
	Format             string     `json:"format"`
	FormatVersion      int        `json:"formatVersion"`
	DatasetID          string     `json:"datasetId"`
	VehicleID          string     `json:"vehicleId"`
	GenerationID       string     `json:"generationId"`
	GenerationSequence int64      `json:"generationSequence"`
	TotalTripCount     int64      `json:"totalTripCount"`
	Artifacts          []artifact `json:"artifacts"`
}

type artifact struct {
	LogicalKey   string `json:"logicalKey"`
	LogicalType  string `json:"logicalType"`
	Period       string `json:"period"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	TripCount    int64  `json:"tripCount"`
}

func parseManifest(raw []byte, name string) (manifest, error) {
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, formatErr("%s: %v", name, err)
	}
	match := commitName.FindStringSubmatch(name)
	if m.Format != "aaos-triplog-drive-generation" || m.FormatVersion != 3 {
		return m, formatErr("%s: not a commit of the Drive sync V3", name)
	}
	if seq, _ := strconv.ParseInt(match[1], 10, 64); seq != m.GenerationSequence || match[2] != m.GenerationID {
		return m, formatErr("%s: its name is not its generation's", name)
	}
	if m.DatasetID == "" || m.VehicleID == "" || m.TotalTripCount < 0 {
		return m, formatErr("%s: no dataset, vehicle or trip count", name)
	}
	var trips int64
	paths := map[string]bool{}
	for _, a := range m.Artifacts {
		if !artifactPath.MatchString(a.RelativePath) || paths[a.RelativePath] {
			return m, formatErr("%s: an artifact's path %q", name, a.RelativePath)
		}
		paths[a.RelativePath] = true
		if !digest.MatchString(a.SHA256) || a.Size < 0 || a.Size > maxArtifact || a.TripCount < 0 {
			return m, formatErr("%s: artifact %s", name, a.RelativePath)
		}
		if p, ok := periods[a.LogicalType]; ok {
			if !p.MatchString(a.Period) {
				return m, formatErr("%s: artifact %s's period %q", name, a.RelativePath, a.Period)
			}
			trips += a.TripCount
		}
	}
	if trips != m.TotalTripCount {
		return m, formatErr("%s: its archives hold %d trips, it says %d", name, trips, m.TotalTripCount)
	}
	return m, nil
}

// day is a day file of an archive: the trips that start that day in the export's time
// zone.
type day struct {
	Format              string    `json:"format"`
	FormatVersion       int       `json:"formatVersion"`
	Date                string    `json:"date"`
	TimestampUnit       string    `json:"timestampUnit"`
	StorageUnitSystem   string    `json:"storageUnitSystem"`
	PowerSignConvention string    `json:"powerSignConvention"`
	DatasetID           string    `json:"datasetId"`
	Trips               []rawTrip `json:"trips"`
}

type rawTrip struct {
	TripID            string   `json:"tripId"`
	StartedAt         int64    `json:"startedAt"`
	EndedAt           *int64   `json:"endedAt"`
	StorageUnitSystem string   `json:"storageUnitSystem"`
	DistanceKm        *float64 `json:"distanceKm"`
	EnergyKwh         *float64 `json:"energyKwh"`
	EnergySource      *string  `json:"energySource"`
	VehicleType       *string  `json:"vehicleType"`
	StartLat          *float64 `json:"startLat"`
	StartLon          *float64 `json:"startLon"`
	EndLat            *float64 `json:"endLat"`
	EndLon            *float64 `json:"endLon"`
	Telemetry         []struct {
		RecordedAt                 int64    `json:"recordedAt"`
		BatteryPercent             *float64 `json:"batteryPercent"`
		RangeKm                    *float64 `json:"rangeKm"`
		VehicleOutsideTemperatureC *float64 `json:"vehicleOutsideTemperatureC"`
	} `json:"telemetry"`
	Route []struct {
		RecordedAt int64    `json:"recordedAt"`
		Latitude   *float64 `json:"latitude"`
		Longitude  *float64 `json:"longitude"`
	} `json:"route"`
}

// readArchive reads the trips of a day or month archive, in memory: a member's name is
// never a path on disk. left is what the export may still inflate; it takes what the
// archive does.
func readArchive(data []byte, a artifact, datasetID string, left *uint64) ([]Trip, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, formatErr("%s: not a ZIP file", a.RelativePath)
	}
	if len(z.File) > maxArchiveFiles {
		return nil, formatErr("%s: more than %d files", a.RelativePath, maxArchiveFiles)
	}
	var inflated uint64
	var trips []Trip
	names := map[string]bool{}
	for _, f := range z.File {
		if names[f.Name] {
			return nil, formatErr("%s: %s twice", a.RelativePath, f.Name)
		}
		names[f.Name] = true
		// Only the day files hold trips; an archive also has its README and summaries.
		m := dayName.FindStringSubmatch(f.Name)
		if m == nil {
			continue
		}
		if inflated += f.UncompressedSize64; inflated > maxArchiveInflat {
			return nil, formatErr("%s: more than %d MiB once inflated", a.RelativePath, maxArchiveInflat>>20)
		}
		if f.UncompressedSize64 > *left {
			return nil, formatErr("more than %d MiB of trips once inflated", maxExportInflat>>20)
		}
		*left -= f.UncompressedSize64
		raw, err := readMember(f, maxArtifact)
		if err != nil {
			return nil, err
		}
		var d day
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, formatErr("%s/%s: %v", a.RelativePath, f.Name, err)
		}
		where := a.RelativePath + "/" + f.Name
		switch {
		case d.Format != "aaos-triplog-day" || d.FormatVersion != 1:
			return nil, formatErr("%s: not a day file of format 1", where)
		case d.DatasetID != datasetID:
			return nil, formatErr("%s: another dataset's", where)
		case d.TimestampUnit != "unix_ms" || d.StorageUnitSystem != "metric" ||
			d.PowerSignConvention != "positive_consumption_negative_regeneration":
			return nil, formatErr("%s: units other than unix_ms, metric and positive consumption", where)
		case d.Date != m[1] || (a.LogicalType == "day_archive" && d.Date != a.Period) ||
			(a.LogicalType == "month_archive" && d.Date[:7] != a.Period):
			return nil, formatErr("%s: its date is not its archive's", where)
		}
		for _, rt := range d.Trips {
			t, err := convert(rt)
			if err != nil {
				return nil, formatErr("%s: %v", where, err)
			}
			trips = append(trips, t)
		}
	}
	if int64(len(trips)) != a.TripCount {
		return nil, formatErr("%s: %d trips, the commit says %d", a.RelativePath, len(trips), a.TripCount)
	}
	return trips, nil
}

func convert(rt rawTrip) (Trip, error) {
	switch {
	case rt.TripID == "":
		return Trip{}, errors.New("a trip without its tripId")
	case rt.StorageUnitSystem != "metric":
		return Trip{}, fmt.Errorf("trip %q: units %q", rt.TripID, rt.StorageUnitSystem)
	case rt.StartedAt <= 0 || (rt.EndedAt != nil && *rt.EndedAt < rt.StartedAt):
		return Trip{}, fmt.Errorf("trip %q: its times", rt.TripID)
	}
	t := Trip{
		ID: rt.TripID, Start: time.UnixMilli(rt.StartedAt).UTC(), DistanceKm: rt.DistanceKm, EnergyKWh: rt.EnergyKwh,
		StartLat: rt.StartLat, StartLon: rt.StartLon, EndLat: rt.EndLat, EndLon: rt.EndLon,
	}
	if rt.EndedAt != nil {
		end := time.UnixMilli(*rt.EndedAt).UTC()
		t.End = &end
	}
	if rt.EnergySource != nil {
		t.EnergySource = *rt.EnergySource
	}
	if rt.VehicleType != nil {
		t.VehicleType = *rt.VehicleType
	}
	for _, s := range rt.Telemetry {
		if s.RecordedAt <= 0 {
			return Trip{}, fmt.Errorf("trip %q: a sample's time", rt.TripID)
		}
		t.Telemetry = append(t.Telemetry, Sample{
			At: time.UnixMilli(s.RecordedAt).UTC(), BatteryPercent: s.BatteryPercent, RangeKm: s.RangeKm,
			OutsideTempC: s.VehicleOutsideTemperatureC,
		})
	}
	for _, p := range rt.Route {
		if p.RecordedAt <= 0 {
			return Trip{}, fmt.Errorf("trip %q: a route point's time", rt.TripID)
		}
		// A point without both coordinates is no position: left out.
		if p.Latitude != nil && p.Longitude != nil {
			t.Route = append(t.Route, Point{At: time.UnixMilli(p.RecordedAt).UTC(), Lat: *p.Latitude, Lon: *p.Longitude})
		}
	}
	return t, nil
}

// sortTrips sorts the trips by start, then ID.
func sortTrips(trips []Trip) {
	slices.SortFunc(trips, func(a, b Trip) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// readMember reads a file of a ZIP, at most limit bytes once inflated.
func readMember(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) { //nolint:gosec // limit is one of the positive bounds above
		return nil, formatErr("%s: larger than %d MiB", f.Name, limit>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, formatErr("%s: %v", f.Name, err)
	}
	defer rc.Close()
	// The upload is already on disk: a failure to read it is its own, a corrupt ZIP.
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	switch {
	case err != nil:
		return nil, formatErr("%s: %v", f.Name, err)
	case int64(len(data)) > limit:
		return nil, formatErr("%s: larger than %d MiB", f.Name, limit>>20)
	}
	return data, nil
}
