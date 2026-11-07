package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"time"

	"runsten/internal/aaoslogging"
	"runsten/internal/core"
)

// SourceAAOSLogging is AAOS Logging, an Android Automotive app whose Google Drive export
// the user uploads.
const SourceAAOSLogging = "aaos_logging"

// AAOSLogging imports the trips of AAOS Logging's export, the ZIP r of size bytes Google
// Drive gives for the vehicle's folder, with their tracks. The app records no charge:
// none is imported. An upload that is not the export returns an
// *aaoslogging.FormatError, and nothing is written.
func (f *Files) AAOSLogging(ctx context.Context, accountID, vehicleID string, r io.ReaderAt, size int64) (Report, error) {
	exp, err := aaoslogging.Read(r, size)
	if err != nil {
		return Report{}, fmt.Errorf("export: %w", err)
	}
	at := f.clk.Now()
	var out []ImportedTrip
	invalid := 0
	for _, t := range exp.Trips {
		trip, ok := convertAAOSTrip(t, at)
		if !ok {
			invalid++
			continue
		}
		out = append(out, trip)
	}
	res, err := f.store.SaveImport(ctx, accountID, vehicleID, out, nil)
	if err != nil {
		return Report{}, fmt.Errorf("saving: %w", err)
	}
	return Report{Result: res, Invalid: invalid}, nil
}

// aaosEnergySources are the energies AAOS Logging measured or derived from the battery's;
// an unknown one is no figure.
var aaosEnergySources = []string{"vehicle_sensor", "derived_battery_energy", "mixed_vehicle_sensor_and_derived"}

// convertAAOSTrip converts a trip of the export: an unfinished one is left out. Its ends
// are the app's, else its route's; its state of charge and range those of its first and
// last samples telling them, its outside temperature their mean. Its energy is the
// battery's net one, kept only when positive and of a known source: a trip that gained
// energy, downhill, would read as none consumed. The app's distance is no odometer: the
// trip has none. A trip of a hybrid, or of a car the app could not class, keeps neither
// range nor energy: its range is fuel's and the battery's together, and its battery's
// energy over its distance would read as all it consumed, the engine's left out.
func convertAAOSTrip(t aaoslogging.Trip, at time.Time) (ImportedTrip, bool) {
	if t.End == nil || t.End.Before(t.Start) {
		return ImportedTrip{}, false
	}
	route := slices.Clone(t.Route)
	slices.SortStableFunc(route, func(a, b aaoslogging.Point) int { return a.At.Compare(b.At) })
	from, fromOK := position(t.StartLat, t.StartLon)
	to, toOK := position(t.EndLat, t.EndLon)
	if !fromOK || !toOK {
		return ImportedTrip{}, false
	}
	if len(route) > 0 {
		if first := route[0]; !from.OK && inBounds(first.Lat, first.Lon) {
			from = core.Value[core.Position]{V: core.Position{Lat: first.Lat, Lon: first.Lon}, OK: true}
		}
		if last := route[len(route)-1]; !to.OK && inBounds(last.Lat, last.Lon) {
			to = core.Value[core.Position]{V: core.Position{Lat: last.Lat, Lon: last.Lon}, OK: true}
		}
	}
	trip := core.Trip{
		DetectedAt: at.UTC(), Start: exact(t.Start), End: exact(*t.End), Sources: []string{SourceAAOSLogging},
		Import: core.Some(core.Import{Source: SourceAAOSLogging, SourceID: aaosSourceID(t.ID), ImportedAt: at}, at),
		From:   from, To: to,
	}
	var ok bool
	if trip.DistanceKm, ok = nonNegative(t.DistanceKm, at); !ok {
		return ImportedTrip{}, false
	}
	electric := t.VehicleType == "" || t.VehicleType == "ELECTRIC"
	if e := t.EnergyKWh; electric && e != nil && finite(*e) && *e > 0 && slices.Contains(aaosEnergySources, t.EnergySource) {
		trip.EnergyKWh = core.Some(*e, at)
	}
	samples := slices.Clone(t.Telemetry)
	slices.SortStableFunc(samples, func(a, b aaoslogging.Sample) int { return a.At.Compare(b.At) })
	trip.StartSoC, trip.EndSoC = ends(samples, at, func(s aaoslogging.Sample) *float64 { return s.BatteryPercent }, 0, 100)
	if electric {
		trip.StartRangeKm, trip.EndRangeKm = ends(samples, at, func(s aaoslogging.Sample) *float64 { return s.RangeKm }, 0, 5000)
	}
	var sum float64
	var n int
	for _, s := range samples {
		if v := s.OutsideTempC; v != nil && finite(*v) && *v >= -80 && *v <= 80 {
			sum += *v
			n++
		}
	}
	if n > 0 {
		trip.OutsideTempC = core.Some(sum/float64(n), at)
	}
	coords := make([][2]float64, 0, len(route))
	for _, p := range route {
		coords = append(coords, [2]float64{p.Lon, p.Lat})
	}
	return ImportedTrip{Trip: trip, Track: lineString(coords)}, true
}

// ends are the first and the last of the samples' values within [lo, hi]: a value out of
// them is the car's lack of one, not the trip's fault.
func ends(samples []aaoslogging.Sample, at time.Time, value func(aaoslogging.Sample) *float64, lo, hi float64) (first, last core.Value[float64]) {
	for _, s := range samples {
		if v := value(s); v != nil && finite(*v) && *v >= lo && *v <= hi {
			if !first.OK {
				first = core.Some(*v, at)
			}
			last = core.Some(*v, at)
		}
	}
	return first, last
}

// aaosSourceID is the trip's ID: the app's when the store accepts it, else its SHA-256,
// as stable.
func aaosSourceID(id string) string {
	if sourceIDPattern.MatchString(id) {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}
