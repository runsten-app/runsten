package store

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/importer"
	"runsten/internal/volvo"
)

func imported(source, id string) core.Value[core.Import] {
	return core.Value[core.Import]{V: core.Import{Source: source, SourceID: id}, OK: true}
}

// importedTrip is a trip of the source from start, of 30 minutes.
func importedTrip(source, id string, start time.Time) importer.ImportedTrip {
	return importer.ImportedTrip{Trip: core.Trip{
		Start: core.Bounds{After: start, Before: start}, End: core.Bounds{After: start.Add(30 * time.Minute), Before: start.Add(30 * time.Minute)},
		Import:     imported(source, id),
		DistanceKm: some(21), StartSoC: some(80), EndSoC: some(74), EnergyKWh: some(3.9),
		From: core.Value[core.Position]{V: core.Position{Lat: 45.764, Lon: 4.8357}, OK: true},
	}}
}

// importedCharge is a charge of Aximote from start, of an hour.
func importedCharge(id string, start time.Time) core.Charge {
	const source = "aximote"
	return core.Charge{
		Start: core.Bounds{After: start, Before: start}, End: core.Bounds{After: start.Add(time.Hour), Before: start.Add(time.Hour)},
		Import:   imported(source, id),
		Type:     core.Value[core.ChargeType]{V: core.AC, OK: true},
		StartSoC: some(40), EndSoC: some(80), EnergyPowerKWh: some(26), EnergySoCKWh: some(25.6),
		Capacity:       core.Value[core.Capacity]{V: core.Capacity{KWh: 64, Source: core.CapacityCatalogNet}, OK: true},
		AveragePowerKW: some(10.5),
		PaidCost:       core.Value[core.Amount]{V: core.Amount{Currency: "EUR", Minor: 412}, OK: true},
	}
}

func saveOdometer(t *testing.T, s *Store, account, vehicle string, at time.Time) {
	t.Helper()
	_, err := s.SaveSnapshot(context.Background(), collector.Snapshot{
		AccountID: account, VehicleID: vehicle, Source: volvo.Source, Endpoint: volvo.Odometer, FetchedAt: at,
		Payload: []byte(`{"data":{"odometer":{"value":12400}}}`), Hash: []byte{1},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSaveImport(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	saveOdometer(t, s, a, v, t0)

	before := t0.Add(-48 * time.Hour)
	trip := importedTrip("aximote", "trip-1", before)
	trip.Track = []byte(`{"type":"LineString","coordinates":[[4.8357,45.764],[4.927,45.7797]]}`)
	trips := []importer.ImportedTrip{trip, importedTrip("aximote", "trip-2", t0)} // the second starts at the cutoff
	charges := []core.Charge{importedCharge("charge-1", before.Add(time.Hour)), importedCharge("charge-2", t0.Add(time.Hour))}
	res, err := s.SaveImport(ctx, a, v, trips, charges)
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved != 2 || res.Skipped != 2 || !res.Cutoff.Equal(t0) {
		t.Errorf("result %+v, want 2 saved and 2 skipped at the first snapshot", res)
	}

	got, err := s.ListTrips(ctx, a, v, api.EventQuery{})
	if err != nil || len(got) != 1 {
		t.Fatalf("trips %+v, %v", got, err)
	}
	tr := got[0]
	if !tr.Import.OK || tr.Import.V.Source != "aximote" || tr.Import.V.SourceID != "trip-1" || tr.Reconstructed ||
		!slices.Equal(tr.Sources, []string{"aximote"}) || tr.Start != trip.Trip.Start || tr.End != trip.Trip.End ||
		tr.DistanceKm != some(21) || tr.EnergyKWh != some(3.9) || !tr.From.OK || tr.To.OK || tr.Capacity.OK {
		t.Errorf("trip %+v", tr)
	}
	if !tr.DetectedAt.Equal(tr.Import.V.ImportedAt) || tr.DetectedAt.IsZero() {
		t.Errorf("an imported trip is detected when imported: %v, %v", tr.DetectedAt, tr.Import.V.ImportedAt)
	}
	p, err := s.ListCharges(ctx, a, v, api.EventQuery{})
	if err != nil || len(p.Charges) != 1 {
		t.Fatalf("charges %+v, %v", p.Charges, err)
	}
	c := p.Charges[0]
	want := charges[0]
	if !c.Import.OK || c.EnergyPowerKWh != want.EnergyPowerKWh || c.EnergySoCKWh != want.EnergySoCKWh ||
		c.Capacity != want.Capacity || c.AveragePowerKW != want.AveragePowerKW || c.PaidCost != want.PaidCost ||
		c.Type != want.Type || !slices.Equal(c.Sources, []string{"aximote"}) || c.Span.OK || c.TargetSoC.OK {
		t.Errorf("charge %+v", c)
	}

	t.Run("found by its source and ID", func(t *testing.T) {
		ref := api.EventRef{Source: "aximote", SourceID: "trip-1"}
		if found, ok, err := s.FindTrip(ctx, a, v, ref); err != nil || !ok || !reflect.DeepEqual(found, tr) {
			t.Errorf("trip %+v, %v, %v", found, ok, err)
		}
		if _, ok, err := s.FindTrip(ctx, a, v, api.EventRef{DetectedAt: tr.DetectedAt}); ok || err != nil {
			t.Errorf("an imported trip found by its detection time: %v", err)
		}
		if _, ok, err := s.FindCharge(ctx, a, v, api.EventRef{Source: "aximote", SourceID: "trip-1"}); ok || err != nil {
			t.Errorf("a trip found as a charge: %v", err)
		}
		if found, ok, err := s.FindCharge(ctx, a, v, api.EventRef{Source: "aximote", SourceID: "charge-1"}); err != nil || !ok || !reflect.DeepEqual(found.Charges[0], c) {
			t.Errorf("charge %+v, %v, %v", found, ok, err)
		}
	})

	other, ov := newVehicle(t, s, "token-other", "YV1AAAAAAAAAAAAA2")
	t.Run("its track, apart", func(t *testing.T) {
		ref := api.EventRef{Source: "aximote", SourceID: "trip-1"}
		track, ok, err := s.TripTrack(ctx, a, v, ref)
		want := [][2]float64{{4.8357, 45.764}, {4.927, 45.7797}}
		if err != nil || !ok || !track.EndedAt.Equal(trip.Trip.End.Before) || !reflect.DeepEqual(track.Line, want) {
			t.Errorf("track %+v, %v, %v", track, ok, err)
		}
		for name, read := range map[string]func() (api.Track, bool, error){
			"of a derived trip": func() (api.Track, bool, error) {
				return s.TripTrack(ctx, a, v, api.EventRef{DetectedAt: tr.DetectedAt})
			},
			"of a charge": func() (api.Track, bool, error) {
				return s.TripTrack(ctx, a, v, api.EventRef{Source: "aximote", SourceID: "charge-1"})
			},
			"of another vehicle": func() (api.Track, bool, error) { return s.TripTrack(ctx, other, ov, ref) },
			"of another account": func() (api.Track, bool, error) { return s.TripTrack(ctx, other, v, ref) },
		} {
			if _, ok, err := read(); ok || err != nil {
				t.Errorf("a track %s: %v, %v", name, ok, err)
			}
		}
	})

	t.Run("imported again: replaced, its id and track kept", func(t *testing.T) {
		var id string
		read := func() (string, int) {
			var track int
			err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT t.id, count(k.trip_id) FROM imported_trips t
					LEFT JOIN imported_tracks k ON k.trip_id = t.id GROUP BY t.id`).Scan(&id, &track)
			})
			if err != nil {
				t.Fatal(err)
			}
			return id, track
		}
		first, _ := read()
		again := importedTrip("aximote", "trip-1", before)
		again.Trip.DistanceKm = some(22)
		if res, err := s.SaveImport(ctx, a, v, []importer.ImportedTrip{again}, nil); err != nil || res.Saved != 1 {
			t.Fatalf("%+v, %v", res, err)
		}
		if id, track := read(); id != first || track != 1 {
			t.Errorf("row %s with %d track, want %s with its track", id, track, first)
		}
		if got, _ := s.ListTrips(ctx, a, v, api.EventQuery{}); len(got) != 1 || got[0].DistanceKm != some(22) {
			t.Errorf("trips %+v", got)
		}
	})

	t.Run("without its import, refused", func(t *testing.T) {
		bare := importedTrip("aximote", "x", before)
		bare.Trip.Import = core.Value[core.Import]{}
		if _, err := s.SaveImport(ctx, a, v, []importer.ImportedTrip{bare}, nil); !errors.Is(err, ErrNotImported) {
			t.Errorf("err %v", err)
		}
		if _, err := s.SaveImport(ctx, a, v, nil, []core.Charge{{Start: core.Bounds{After: before}}}); !errors.Is(err, ErrNotImported) {
			t.Errorf("err %v", err)
		}
	})

	t.Run("an invalid ID at the source refused, nothing written", func(t *testing.T) {
		if _, err := s.SaveImport(ctx, a, v, []importer.ImportedTrip{importedTrip("aximote", "a/b", before), importedTrip("aximote", "ok", before)}, nil); err == nil {
			t.Error("an ID with a slash accepted")
		}
		if got, _ := s.ListTrips(ctx, a, v, api.EventQuery{}); len(got) != 1 {
			t.Errorf("trips %+v", got)
		}
	})
}

// A vehicle without a snapshot takes what starts before the import.
func TestSaveImportWithoutSnapshot(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	future := time.Now().Add(time.Hour) //nolint:forbidigo // the database's clock decides
	res, err := s.SaveImport(ctx, a, v, []importer.ImportedTrip{importedTrip("aximote", "1", t0), importedTrip("aximote", "2", future)}, nil)
	if err != nil || res.Saved != 1 || res.Skipped != 1 {
		t.Errorf("%+v, %v", res, err)
	}
}

// The lists take derived and imported events in one order, and so do the statistics'
// periods; two sources may start an event at the same time.
func TestImportedEventsListed(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	if err := s.SaveDerivation(ctx, a, v, time.Time{}, sampleResult(t0)); err != nil {
		t.Fatal(err)
	}
	saveOdometer(t, s, a, v, t0.Add(-time.Hour))
	start := t0.Add(-5 * time.Hour)
	trips := []importer.ImportedTrip{
		importedTrip("aximote", "1", start), importedTrip("volvo_csv", "1", start), importedTrip("aximote", "2", start.Add(time.Hour)),
	}
	if _, err := s.SaveImport(ctx, a, v, trips, []core.Charge{importedCharge("c", start.Add(2*time.Hour))}); err != nil {
		t.Fatal(err)
	}

	var keys []string
	q := api.EventQuery{Limit: 1}
	for {
		page, err := s.ListTrips(ctx, a, v, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		tr := page[0]
		key := "derived"
		if tr.Import.OK {
			key = tr.Import.V.Source + ":" + tr.Import.V.SourceID
		}
		keys = append(keys, key)
		q.After = &api.EventKey{StartedAfter: tr.Start.After, DetectedAt: tr.DetectedAt}
		if tr.Import.OK {
			q.After.Import = key
		}
	}
	// The two of the same start are in the order of their import's key, newest first:
	// their import time may be the same.
	want := []string{"derived", "aximote:2"}
	if !slices.Equal(keys[:2], want) || len(keys) != 4 || !slices.Contains(keys[2:], "aximote:1") || !slices.Contains(keys[2:], "volvo_csv:1") {
		t.Errorf("pages %q", keys)
	}

	p, err := s.PeriodEvents(ctx, a, v, start, t0.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Trips) != 4 || len(p.Charges) != 2 {
		t.Errorf("period: %d trips, %d charges, want 4 and 2", len(p.Trips), len(p.Charges))
	}
	// Before the period: the latest trip and charge, imported ones, open it.
	p, err = s.PeriodEvents(ctx, a, v, start.Add(150*time.Minute), t0.Add(-time.Hour))
	if err != nil || len(p.Trips) != 1 || len(p.Charges) != 1 || p.Trips[0].Import.V.SourceID != "2" {
		t.Errorf("before the period: %+v, %v", p, err)
	}

	imports, err := s.Imports(ctx, a, v)
	if err != nil || len(imports) != 2 {
		t.Fatalf("imports %+v, %v", imports, err)
	}
	if i := imports[0]; i.Source != "aximote" || i.Trips != 2 || i.Charges != 1 || !i.StartedFrom.Equal(start) ||
		!i.StartedTo.Equal(start.Add(2*time.Hour)) || i.LastImportedAt.IsZero() {
		t.Errorf("aximote %+v", i)
	}

	found, err := s.DeleteImport(ctx, a, v, "aximote")
	if err != nil || !found {
		t.Fatalf("delete: %v, %v", found, err)
	}
	if found, err := s.DeleteImport(ctx, a, v, "aximote"); err != nil || found {
		t.Errorf("deleted again: %v, %v", found, err)
	}
	if got, _ := s.ListTrips(ctx, a, v, api.EventQuery{}); len(got) != 2 {
		t.Errorf("trips after the deletion %+v", got)
	}
	if got, _ := s.Imports(ctx, a, v); len(got) != 1 || got[0].Source != "volvo_csv" {
		t.Errorf("imports after the deletion %+v", got)
	}
}

// TestImportIsolation extends TestAccountIsolation to the imported tables.
func TestImportIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	for _, x := range [][2]string{{a, va}, {b, vb}} {
		trip := importedTrip("aximote", "1", t0)
		trip.Track = []byte(`{"type":"LineString","coordinates":[]}`)
		if _, err := s.SaveImport(ctx, x[0], x[1], []importer.ImportedTrip{trip}, []core.Charge{importedCharge("1", t0)}); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"imported_trips", "imported_charges", "imported_tracks"}
	for _, table := range tables {
		var n int
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n)
		})
		if err != nil || n != 1 {
			t.Errorf("account A, %s: %d rows visible, %v; want 1", table, n, err)
		}
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Errorf("no account, %s: %d visible, %v", table, n, err)
		}
		err = s.inAccount(ctx, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE "+table+" SET account_id = $1", b)
			return err
		})
		if err == nil {
			t.Errorf("%s: rows moved to another account", table)
		}
	}
	if trips, err := s.ListTrips(ctx, a, vb, api.EventQuery{}); len(trips) != 0 || err != nil {
		t.Errorf("account A read B's trips: %+v, %v", trips, err)
	}
	if imports, err := s.Imports(ctx, a, vb); len(imports) != 0 || err != nil {
		t.Errorf("account A read B's imports: %+v, %v", imports, err)
	}
	if _, err := s.SaveImport(ctx, a, vb, []importer.ImportedTrip{importedTrip("aximote", "2", t0)}, nil); err == nil {
		t.Error("imported into another account's vehicle")
	}
	if found, err := s.DeleteImport(ctx, a, vb, "aximote"); found || err != nil {
		t.Errorf("deleted another account's import: %v, %v", found, err)
	}
	if imports, _ := s.Imports(ctx, b, vb); len(imports) != 1 || imports[0].Trips != 1 {
		t.Errorf("B's imports %+v", imports)
	}
}
