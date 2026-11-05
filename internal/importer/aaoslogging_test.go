package importer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"runsten/internal/aaoslogging"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
)

func TestConvertAAOSTrip(t *testing.T) {
	start := time.Date(2026, 10, 4, 7, 40, 0, 0, time.FixedZone("CEST", 2*3600))
	end := start.Add(10 * time.Minute)
	f := func(v float64) *float64 { return &v }
	valid := func() aaoslogging.Trip {
		return aaoslogging.Trip{
			ID: "dataset-a-2", Start: start, End: &end, DistanceKm: f(8.4), EnergyKWh: f(1.35), EnergySource: "vehicle_sensor",
			StartLat: f(57.7), StartLon: f(11.97), EndLat: f(57.72), EndLon: f(12),
			Telemetry: []aaoslogging.Sample{
				{At: start.Add(5 * time.Minute), BatteryPercent: f(79), RangeKm: f(300), OutsideTempC: f(8)},
				{At: start, RangeKm: f(310), OutsideTempC: f(7)},
				{At: start.Add(time.Second), BatteryPercent: f(80), RangeKm: f(309), OutsideTempC: f(9)},
				{At: end, BatteryPercent: f(101), RangeKm: f(296), OutsideTempC: f(-300)},
			},
			Route: []aaoslogging.Point{
				{At: end, Lat: 57.72, Lon: 12}, {At: start, Lat: 57.7, Lon: 11.97}, {At: start.Add(time.Minute), Lat: 57.71, Lon: 11.99},
			},
		}
	}
	it, ok := convertAAOSTrip(valid(), t0)
	trip := it.Trip
	if !ok || trip.Start.After != start.UTC() || trip.End.Before != end.UTC() || trip.DetectedAt != t0 ||
		trip.Import.V.Source != SourceAAOSLogging || trip.Import.V.SourceID != "dataset-a-2" ||
		trip.Sources[0] != SourceAAOSLogging || trip.DistanceKm.V != 8.4 || trip.EnergyKWh.V != 1.35 ||
		trip.From.V != (core.Position{Lat: 57.7, Lon: 11.97}) || trip.To.V != (core.Position{Lat: 57.72, Lon: 12}) ||
		trip.StartOdometerKm.OK || trip.Capacity.OK {
		t.Errorf("%+v", trip)
	}
	// The samples sorted; 101 % and -300 °C are no values.
	if trip.StartSoC.V != 80 || trip.EndSoC.V != 79 || trip.StartRangeKm.V != 310 || trip.EndRangeKm.V != 296 ||
		trip.OutsideTempC.V != 8 {
		t.Errorf("SoC %v→%v, range %v→%v, %v °C", trip.StartSoC, trip.EndSoC, trip.StartRangeKm, trip.EndRangeKm, trip.OutsideTempC)
	}
	if want := `{"type":"LineString","coordinates":[[11.97,57.7],[11.99,57.71],[12,57.72]]}`; string(it.Track) != want {
		t.Errorf("track %s", it.Track)
	}

	noEnds := valid()
	noEnds.StartLat, noEnds.StartLon, noEnds.EndLat, noEnds.EndLon = nil, nil, nil, nil
	if it, ok := convertAAOSTrip(noEnds, t0); !ok || it.Trip.From.V != (core.Position{Lat: 57.7, Lon: 11.97}) ||
		it.Trip.To.V != (core.Position{Lat: 57.72, Lon: 12}) {
		t.Errorf("the ends from the route: %+v %+v", it.Trip.From, it.Trip.To)
	}
	bare := aaoslogging.Trip{ID: "x", Start: start, End: &end}
	if it, ok := convertAAOSTrip(bare, t0); !ok || it.Track != nil || it.Trip.From.OK || it.Trip.StartSoC.OK ||
		it.Trip.EnergyKWh.OK || it.Trip.OutsideTempC.OK {
		t.Errorf("without values: %+v", it)
	}
	for name, change := range map[string]func(*aaoslogging.Trip){
		"gained":         func(t *aaoslogging.Trip) { t.EnergyKWh = f(-0.2) },
		"unknown source": func(t *aaoslogging.Trip) { t.EnergySource = "unknown" },
		"no source":      func(t *aaoslogging.Trip) { t.EnergySource = "" },
	} {
		tr := valid()
		change(&tr)
		if it, ok := convertAAOSTrip(tr, t0); !ok || it.Trip.EnergyKWh.OK {
			t.Errorf("%s: %+v", name, it.Trip.EnergyKWh)
		}
	}
	for name, change := range map[string]func(*aaoslogging.Trip){
		"unfinished":  func(t *aaoslogging.Trip) { t.End = nil },
		"ends before": func(t *aaoslogging.Trip) { before := start.Add(-time.Second); t.End = &before },
		"a latitude":  func(t *aaoslogging.Trip) { t.StartLat = f(91) },
		"a distance":  func(t *aaoslogging.Trip) { t.DistanceKm = f(-1) },
	} {
		tr := valid()
		change(&tr)
		if _, ok := convertAAOSTrip(tr, t0); ok {
			t.Errorf("%s: converted", name)
		}
	}
	if id := aaosSourceID("trip/2026:10"); len(id) != 64 || id != aaosSourceID("trip/2026:10") || !sourceIDPattern.MatchString(id) {
		t.Errorf("an ID the store refuses: %q", id)
	}
}

func TestFilesAAOSLogging(t *testing.T) {
	export, err := os.ReadFile("../aaoslogging/testdata/export.zip")
	if err != nil {
		t.Fatal(err)
	}
	s := &saver{}
	files := NewFiles(s, clock.NewManual(t0))
	// Three trips, one unfinished: two saved, of which the saver skips one.
	rep, err := files.AAOSLogging(context.Background(), "acc", "veh", bytes.NewReader(export), int64(len(export)))
	if err != nil || rep.Saved != 1 || rep.Skipped != 1 || rep.Invalid != 1 || rep.Cutoff != cutoff || len(s.trips) != 2 {
		t.Fatalf("%+v, %v", rep, err)
	}
	if s.trips[0].Trip.Import.V.SourceID != "dataset-a-1" || s.trips[1].Track == nil || !s.trips[1].Trip.StartSoC.OK {
		t.Errorf("%+v", s.trips)
	}
	var fe *aaoslogging.FormatError
	if _, err := files.AAOSLogging(context.Background(), "acc", "veh", strings.NewReader("trips"), 5); !errors.As(err, &fe) {
		t.Errorf("not a ZIP: %v", err)
	}
	boom := errors.New("boom")
	s.err = boom
	if _, err := files.AAOSLogging(context.Background(), "acc", "veh", bytes.NewReader(export), int64(len(export))); !errors.Is(err, boom) {
		t.Errorf("the store failing: %v", err)
	}
}
