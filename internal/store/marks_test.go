package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/core"
	"runsten/internal/importer"
)

// tripsAt are derived trips of an hour, starting at each hour after t0.
func tripsAt(hs ...float64) []core.Trip {
	var out []core.Trip
	for _, h := range hs {
		start := hours(h)
		out = append(out, core.Trip{
			DetectedAt: start.Add(5 * time.Minute), Sources: fromVolvo,
			Start: core.Bounds{After: start, Before: start.Add(5 * time.Minute)},
			End:   core.Bounds{After: start.Add(55 * time.Minute), Before: start.Add(time.Hour)},
		})
	}
	return out
}

// purposes are the purposes of the marks, "-" for none by hand, "" for no mark.
func purposes(marks []core.Value[core.TripMark]) []string {
	out := make([]string, len(marks))
	for i, m := range marks {
		switch {
		case !m.OK:
		case !m.V.Purpose.OK:
			out[i] = "-"
		default:
			out[i] = string(m.V.Purpose.V)
		}
	}
	return out
}

func TestTripMarks(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	trips := tripsAt(0, 2, 4, 6)
	for _, x := range [][2]string{{a, v}, {b, vb}} {
		if err := s.SaveDerivation(ctx, x[0], x[1], time.Time{}, core.Result{Trips: trips}); err != nil {
			t.Fatal(err)
		}
	}
	imported := importedTrip("aximote", "trip-1", t0.Add(-48*time.Hour))
	if _, err := s.SaveImport(ctx, a, v, []importer.ImportedTrip{imported}, nil); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListTrips(ctx, a, v, api.EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	ref := func(tr core.Trip) api.EventRef {
		if tr.Import.OK {
			return api.EventRef{Source: tr.Import.V.Source, SourceID: tr.Import.V.SourceID}
		}
		return api.EventRef{DetectedAt: tr.DetectedAt}
	}
	mark := func(p core.Purpose) core.Value[core.TripMark] {
		m := core.TripMark{}
		if p != "" {
			m.Purpose = core.Value[core.Purpose]{V: p, OK: true}
		}
		return core.Value[core.TripMark]{V: m, OK: true}
	}
	read := func(t *testing.T, account, vehicle string) []string {
		t.Helper()
		got, err := s.TripMarks(ctx, account, vehicle, all)
		if err != nil {
			t.Fatal(err)
		}
		return purposes(got)
	}
	// all is newest first: the trips at 6, 4, 2, 0 h, then the imported one.
	if got := read(t, a, v); !reflect.DeepEqual(got, []string{"", "", "", "", ""}) {
		t.Fatalf("before any mark: %v", got)
	}

	t.Run("set, replace, none, clear", func(t *testing.T) {
		if err := s.SetTripMarks(ctx, a, v, []api.EventRef{ref(all[0]), ref(all[1]), ref(all[4])}, mark(core.PurposeBusiness)); err != nil {
			t.Fatal(err)
		}
		if err := s.SetTripMarks(ctx, a, v, []api.EventRef{ref(all[1])}, mark("")); err != nil {
			t.Fatal(err)
		}
		if got := read(t, a, v); !reflect.DeepEqual(got, []string{"business", "-", "", "", "business"}) {
			t.Errorf("marked: %v", got)
		}
		if err := s.SetTripMarks(ctx, a, v, []api.EventRef{ref(all[1]), ref(all[4])}, core.Value[core.TripMark]{}); err != nil {
			t.Fatal(err)
		}
		if got := read(t, a, v); !reflect.DeepEqual(got, []string{"business", "", "", "", ""}) {
			t.Errorf("cleared: %v", got)
		}
	})

	t.Run("a trip of nowhere writes nothing", func(t *testing.T) {
		refs := []api.EventRef{ref(all[2]), {DetectedAt: hours(50)}}
		if err := s.SetTripMarks(ctx, a, v, refs, mark(core.PurposePersonal)); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
		if err := s.SetTripMarks(ctx, a, v, []api.EventRef{{Source: "aximote", SourceID: "nope"}}, mark(core.PurposePersonal)); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("imported: err = %v, want ErrNotFound", err)
		}
		if got := read(t, a, v); got[2] != "" {
			t.Errorf("written anyway: %v", got)
		}
	})

	t.Run("another account sees and writes none", func(t *testing.T) {
		if got := read(t, b, vb); !reflect.DeepEqual(got, []string{"", "", "", "", ""}) {
			t.Errorf("b reads a's marks: %v", got)
		}
		if err := s.SetTripMarks(ctx, b, v, []api.EventRef{ref(all[2])}, mark(core.PurposeCommute)); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("b marks a's trip: %v", err)
		}
	})

	t.Run("a moved mark", func(t *testing.T) {
		// Marks of trips no longer detected where they were: one overlaps the trip at 4 h
		// alone, two overlap the trip at 2 h, one overlaps two trips.
		insert := func(detected time.Time, after, before float64, p string) {
			t.Helper()
			err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO trip_marks (account_id, vehicle_id, trip_detected_at, window_after, window_before, purpose)
					VALUES ($1, $2, $3, $4, $5, $6)`, a, v, detected, hours(after), hours(before), p)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		insert(hours(100), 4.2, 4.8, "commute")
		insert(hours(101), 2.1, 2.5, "personal")
		insert(hours(102), 2.5, 2.9, "business")
		insert(hours(103), 0.5, 2.5, "business")
		if got := read(t, a, v); !reflect.DeepEqual(got, []string{"business", "commute", "", "", ""}) {
			t.Errorf("moved: %v", got)
		}
		// Marked again, the trip takes its own mark, and the moved one is gone.
		if err := s.SetTripMarks(ctx, a, v, []api.EventRef{ref(all[1])}, core.Value[core.TripMark]{}); err != nil {
			t.Fatal(err)
		}
		if got := read(t, a, v); got[1] != "" {
			t.Errorf("cleared, the moved mark came back: %v", got)
		}
		// The others are kept: never lost.
		if n := count(ctx, t, s, a, "trip_marks"); n != 4 {
			t.Errorf("%d marks left, want 4", n)
		}
	})
}
