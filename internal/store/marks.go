package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/core"
)

// movedMarkAs selects, as alias, the marks of vehicle $1 whose derived trip is no longer
// detected at their trip_detected_at (a change of the detection moved it) and whose
// window overlaps one trip of the vehicle only: that trip is theirs. A mark whose window
// overlaps several, or none, is kept and takes none: it is never lost.
func movedMarkAs(alias string) string {
	return strings.NewReplacer("m.", alias+".").Replace(`m.vehicle_id = $1 AND m.trip_detected_at IS NOT NULL
	AND NOT EXISTS (SELECT FROM trips d WHERE d.vehicle_id = $1 AND d.detected_at = m.trip_detected_at)
	AND (SELECT count(*) FROM trips d WHERE d.vehicle_id = $1
		AND d.started_after < m.window_before AND m.window_after < d.ended_before) = 1`)
}

var movedMark = movedMarkAs("m")

// TripMarks implements api.TripMarks: marks[i] is the mark trips[i], the vehicle's,
// takes. A derived trip takes the mark set at its detection time, else a moved one
// (movedMark) that overlaps it alone among the moved ones.
func (s *Store) TripMarks(ctx context.Context, accountID, vehicleID string, trips []core.Trip) ([]core.Value[core.TripMark], error) {
	out := make([]core.Value[core.TripMark], len(trips))
	if len(trips) == 0 {
		return out, nil
	}
	idx := make([]int32, len(trips))
	detected := make([]*time.Time, len(trips))
	after := make([]*time.Time, len(trips))
	before := make([]*time.Time, len(trips))
	source := make([]*string, len(trips))
	sourceID := make([]*string, len(trips))
	for i, t := range trips {
		idx[i] = int32(i) //nolint:gosec // a page of trips, far below 2^31
		if t.Import.OK {
			source[i], sourceID[i] = &t.Import.V.Source, &t.Import.V.SourceID
			continue
		}
		detected[i], after[i], before[i] = &t.DetectedAt, &t.Start.After, &t.End.Before
	}
	moved := make([]int, len(trips)) // the moved marks overlapping each trip
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH t (i, detected_at, a, b, source, source_id) AS (
				SELECT * FROM unnest($2::int[], $3::timestamptz[], $4::timestamptz[], $5::timestamptz[], $6::text[], $7::text[]))
			SELECT t.i, m.purpose, true FROM t JOIN trip_marks m ON m.vehicle_id = $1
				AND (m.trip_detected_at = t.detected_at OR (m.source = t.source AND m.source_id = t.source_id))
			UNION ALL
			SELECT t.i, m.purpose, false FROM t JOIN trip_marks m ON t.detected_at IS NOT NULL
				AND m.window_after < t.b AND t.a < m.window_before AND `+movedMark+`
			WHERE NOT EXISTS (SELECT FROM trip_marks x WHERE x.vehicle_id = $1 AND x.trip_detected_at = t.detected_at)`,
			vehicleID, idx, detected, after, before, source, sourceID)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		var i int
		var purpose *string
		var exact bool
		_, err = pgx.ForEachRow(rows, []any{&i, &purpose, &exact}, func() error {
			if !exact {
				moved[i]++
			}
			out[i] = core.Value[core.TripMark]{V: core.TripMark{Purpose: purposeOf(purpose)}, OK: true}
			return nil
		})
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return nil, fmt.Errorf("trip marks: %w", err)
	}
	// Two moved marks overlapping one trip: neither is known to be its.
	for i, n := range moved {
		if n > 1 {
			out[i] = core.Value[core.TripMark]{}
		}
	}
	return out, nil
}

func purposeOf(p *string) core.Value[core.Purpose] {
	if p == nil {
		return core.Value[core.Purpose]{}
	}
	return core.Value[core.Purpose]{V: core.Purpose(*p), OK: true}
}

// SetTripMarks implements api.TripMarks: it marks the vehicle's trips by hand, or, mark
// absent, deletes their marks, giving them back to the places' rules. All or none:
// api.ErrNotFound if a ref names no trip of the vehicle.
func (s *Store) SetTripMarks(ctx context.Context, accountID, vehicleID string, refs []api.EventRef, mark core.Value[core.TripMark]) error {
	var purpose *string
	if mark.OK && mark.V.Purpose.OK {
		p := string(mark.V.Purpose.V)
		purpose = &p
	}
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		for _, ref := range refs {
			if err := setTripMark(ctx, tx, accountID, vehicleID, ref, mark.OK, purpose); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, api.ErrNotFound) {
		return fmt.Errorf("set trip marks: %w", err)
	}
	return err
}

func setTripMark(ctx context.Context, tx pgx.Tx, accountID, vehicleID string, ref api.EventRef, set bool, purpose *string) error {
	if ref.Source != "" {
		var found bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT FROM imported_trips WHERE vehicle_id = $1 AND source = $2 AND source_id = $3)`,
			vehicleID, ref.Source, ref.SourceID).Scan(&found)
		switch {
		case err != nil:
			return fmt.Errorf("imported trip: %w", err)
		case !found:
			return api.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM trip_marks WHERE vehicle_id = $1 AND source = $2 AND source_id = $3`,
			vehicleID, ref.Source, ref.SourceID); err != nil {
			return fmt.Errorf("delete trip mark: %w", err)
		}
		if !set {
			return nil
		}
		// Upserted: another write of the same trip may have inserted since the delete.
		_, err = tx.Exec(ctx, `INSERT INTO trip_marks (account_id, vehicle_id, source, source_id, purpose) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (vehicle_id, source, source_id) WHERE source IS NOT NULL
			DO UPDATE SET purpose = EXCLUDED.purpose, marked_at = now()`,
			accountID, vehicleID, ref.Source, ref.SourceID, purpose)
		if err != nil {
			return fmt.Errorf("insert trip mark: %w", err)
		}
		return nil
	}
	var after, before time.Time
	err := tx.QueryRow(ctx, `SELECT started_after, ended_before FROM trips WHERE vehicle_id = $1 AND detected_at = $2`,
		vehicleID, ref.DetectedAt).Scan(&after, &before)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return api.ErrNotFound
	case err != nil:
		return fmt.Errorf("trip: %w", err)
	}
	// The trip's mark, and the moved one it takes, as TripMarks does: else that one would
	// come back. Two moved marks overlapping the trip are neither its: both are kept.
	_, err = tx.Exec(ctx, `DELETE FROM trip_marks m WHERE m.vehicle_id = $1 AND (m.trip_detected_at = $2
		OR (m.window_after < $4 AND $3 < m.window_before AND `+movedMark+`
			AND NOT EXISTS (SELECT FROM trip_marks o WHERE o.id <> m.id
				AND o.window_after < $4 AND $3 < o.window_before AND `+movedMarkAs("o")+`)))`,
		vehicleID, ref.DetectedAt, after, before)
	if err != nil {
		return fmt.Errorf("delete trip mark: %w", err)
	}
	if !set {
		return nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO trip_marks (account_id, vehicle_id, trip_detected_at, window_after, window_before, purpose)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (vehicle_id, trip_detected_at) WHERE trip_detected_at IS NOT NULL
		DO UPDATE SET window_after = EXCLUDED.window_after, window_before = EXCLUDED.window_before,
			purpose = EXCLUDED.purpose, marked_at = now()`, accountID, vehicleID, ref.DetectedAt, after, before, purpose)
	if err != nil {
		return fmt.Errorf("insert trip mark: %w", err)
	}
	return nil
}
