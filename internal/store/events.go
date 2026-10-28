package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/core"
	"runsten/internal/derive"
)

// DerivationCursor implements derive.Store.
func (s *Store) DerivationCursor(ctx context.Context, accountID, vehicleID string) (core.Cursor, error) {
	var c core.Cursor
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "SELECT settled_at, replay_from FROM derivation_cursors WHERE vehicle_id = $1",
			vehicleID).Scan(&c.Settled, &c.From)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	c.Settled, c.From = c.Settled.UTC(), c.From.UTC()
	return c, err
}

// SnapshotsSince implements derive.Store.
func (s *Store) SnapshotsSince(ctx context.Context, accountID, vehicleID string, from time.Time, kinds []core.Kind) ([]derive.Snapshot, error) {
	sources, endpoints := kindColumns(kinds)
	var out []derive.Snapshot
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT source, endpoint, fetched_at, checked_at, payload FROM snapshots
			WHERE vehicle_id = $1 AND (source, endpoint) IN (SELECT * FROM unnest($3::text[], $4::text[]))
				AND fetched_at > $2
			UNION ALL
			(SELECT DISTINCT ON (source, endpoint) source, endpoint, fetched_at, checked_at, payload FROM snapshots
			 WHERE vehicle_id = $1 AND (source, endpoint) IN (SELECT * FROM unnest($3::text[], $4::text[]))
				AND fetched_at <= $2
			 ORDER BY source, endpoint, fetched_at DESC)
			ORDER BY fetched_at, source, endpoint`, vehicleID, from, sources, endpoints)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		out, err = scanSnapshots(rows)
		return err
	})
	return out, err
}

// SnapshotsBetween implements derive.Store. A response's records follow each other, so
// those read within [from, to] are the ones fetched in (from, to] and the latest fetched
// at or before from, when it was read again at from or later: both halves walk the
// primary key, never the vehicle's whole history.
func (s *Store) SnapshotsBetween(ctx context.Context, accountID, vehicleID string, from, to time.Time, kinds []core.Kind, limit int) ([]derive.Snapshot, error) {
	sources, endpoints := kindColumns(kinds)
	var out []derive.Snapshot
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT source, endpoint, fetched_at, checked_at, payload FROM (
				(SELECT DISTINCT ON (source, endpoint) source, endpoint, fetched_at, checked_at, payload FROM snapshots
				 WHERE vehicle_id = $1 AND (source, endpoint) IN (SELECT * FROM unnest($4::text[], $5::text[]))
					AND fetched_at <= $2
				 ORDER BY source, endpoint, fetched_at DESC)
				UNION ALL
				SELECT source, endpoint, fetched_at, checked_at, payload FROM snapshots
				WHERE vehicle_id = $1 AND (source, endpoint) IN (SELECT * FROM unnest($4::text[], $5::text[]))
					AND fetched_at > $2 AND fetched_at <= $3
			) s
			WHERE checked_at >= $2
			ORDER BY fetched_at, source, endpoint
			LIMIT $6`, vehicleID, from, to, sources, endpoints, limit)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		out, err = scanSnapshots(rows)
		return err
	})
	return out, err
}

// kindColumns splits kinds into their sources and endpoints, for unnest.
func kindColumns(kinds []core.Kind) (sources, endpoints []string) {
	sources, endpoints = make([]string, len(kinds)), make([]string, len(kinds))
	for i, k := range kinds {
		sources[i], endpoints[i] = k.Source, k.Endpoint
	}
	return sources, endpoints
}

func scanSnapshots(rows pgx.Rows) ([]derive.Snapshot, error) {
	var out []derive.Snapshot
	var sn derive.Snapshot
	_, err := pgx.ForEachRow(rows, []any{&sn.Kind.Source, &sn.Kind.Endpoint, &sn.FetchedAt, &sn.CheckedAt, &sn.Payload}, func() error {
		out = append(out, derive.Snapshot{
			Kind: sn.Kind, FetchedAt: sn.FetchedAt.UTC(), CheckedAt: sn.CheckedAt.UTC(),
			Payload: append([]byte(nil), sn.Payload...),
		})
		return nil
	})
	return out, err //nolint:wrapcheck // wrapped by inAccount
}

// SaveDerivation implements derive.Store.
func (s *Store) SaveDerivation(ctx context.Context, accountID, vehicleID string, after time.Time, res core.Result) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		for _, table := range []string{"trips", "charges"} {
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE vehicle_id = $1 AND detected_at > $2", vehicleID, after); err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
		}
		for _, t := range res.Trips {
			from, to := latLon(t.From), latLon(t.To)
			kwh, source := capacityColumns(t.Capacity)
			_, err := tx.Exec(ctx, `
				INSERT INTO trips (account_id, vehicle_id, detected_at, reconstructed,
					started_after, started_before, ended_after, ended_before,
					start_odometer_km, end_odometer_km, distance_km, start_soc, end_soc,
					start_range_km, end_range_km, energy_kwh, start_lat, start_lon, end_lat, end_lon,
					trip_meter_km, consumption_kwh_per_100km, capacity_kwh, capacity_source, sources)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)`,
				accountID, vehicleID, t.DetectedAt, t.Reconstructed,
				t.Start.After, t.Start.Before, t.End.After, t.End.Before,
				opt(t.StartOdometerKm), opt(t.EndOdometerKm), opt(t.DistanceKm), opt(t.StartSoC), opt(t.EndSoC),
				opt(t.StartRangeKm), opt(t.EndRangeKm), opt(t.EnergyKWh), from[0], from[1], to[0], to[1],
				opt(t.TripMeterKm), opt(t.ConsumptionKWhPer100km), kwh, source, t.Sources)
			if err != nil {
				return fmt.Errorf("insert trip: %w", err)
			}
		}
		for _, c := range res.Charges {
			pos := latLon(c.Position)
			kwh, source := capacityColumns(c.Capacity)
			span0, span1, spanKWh, gapS, durS := spanColumns(c.Span)
			_, err := tx.Exec(ctx, `
				INSERT INTO charges (account_id, vehicle_id, detected_at, reconstructed,
					started_after, started_before, ended_after, ended_before, charge_type,
					start_soc, end_soc, target_soc, energy_soc_kwh, energy_power_kwh, lat, lon,
					capacity_kwh, capacity_source, span_start_soc, span_end_soc, span_energy_kwh,
					span_max_gap_s, span_duration_s, odometer_km, sources, energy_measured_kwh)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)`,
				accountID, vehicleID, c.DetectedAt, c.Reconstructed,
				c.Start.After, c.Start.Before, c.End.After, c.End.Before, opt(c.Type),
				opt(c.StartSoC), opt(c.EndSoC), opt(c.TargetSoC), opt(c.EnergySoCKWh), opt(c.EnergyPowerKWh), pos[0], pos[1],
				kwh, source, span0, span1, spanKWh, gapS, durS, opt(c.OdometerKm), c.Sources, opt(c.EnergyMeasuredKWh))
			if err != nil {
				return fmt.Errorf("insert charge: %w", err)
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO derivation_cursors (account_id, vehicle_id, settled_at, replay_from) VALUES ($1, $2, $3, $4)
			ON CONFLICT (vehicle_id) DO UPDATE SET settled_at = EXCLUDED.settled_at, replay_from = EXCLUDED.replay_from`,
			accountID, vehicleID, res.Cursor.Settled, res.Cursor.From)
		if err != nil {
			return fmt.Errorf("cursor: %w", err)
		}
		return nil
	})
}

// Trips returns the vehicle's derived trips, oldest first.
func (s *Store) Trips(ctx context.Context, accountID, vehicleID string) ([]core.Trip, error) {
	return s.queryTrips(ctx, accountID, derivedTrips, "vehicle_id = $1 ORDER BY started_after, detected_at", vehicleID)
}

// ListTrips implements api.Reader: derived and imported trips.
func (s *Store) ListTrips(ctx context.Context, accountID, vehicleID string, q api.EventQuery) ([]core.Trip, error) {
	where, args := eventPage(vehicleID, q)
	return s.queryTrips(ctx, accountID, tripEvents, where, args...)
}

// FindTrip implements api.Reader.
func (s *Store) FindTrip(ctx context.Context, accountID, vehicleID string, ref api.EventRef) (core.Trip, bool, error) {
	where, args := eventRef(vehicleID, ref)
	trips, err := s.queryTrips(ctx, accountID, tripEvents, where, args...)
	if err != nil || len(trips) == 0 {
		return core.Trip{}, false, err
	}
	return trips[0], true, nil
}

// TripTrack implements api.Reader.
func (s *Store) TripTrack(ctx context.Context, accountID, vehicleID string, ref api.EventRef) (api.Track, bool, error) {
	var t api.Track
	if ref.Source == "" {
		return t, false, nil
	}
	found := false
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT t.ended_at, k.track -> 'coordinates' FROM imported_trips t JOIN imported_tracks k ON k.trip_id = t.id
			WHERE t.vehicle_id = $1 AND t.source = $2 AND t.source_id = $3`,
			vehicleID, ref.Source, ref.SourceID).Scan(&t.EndedAt, &t.Line)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	t.EndedAt = t.EndedAt.UTC()
	return t, found, err
}

// Charges returns the vehicle's derived charges, oldest first.
func (s *Store) Charges(ctx context.Context, accountID, vehicleID string) ([]core.Charge, error) {
	return s.queryCharges(ctx, accountID, "vehicle_id = $1 ORDER BY started_after, detected_at", vehicleID)
}

// ListCharges implements api.Reader: a page of charges, derived and imported, with what
// their costs depend on, from one snapshot.
func (s *Store) ListCharges(ctx context.Context, accountID, vehicleID string, q api.EventQuery) (api.PricedCharges, error) {
	where, args := eventPage(vehicleID, q)
	var out api.PricedCharges
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		charges, err := scanChargesOf(ctx, tx, chargeEvents, where, args...)
		if err != nil {
			return err
		}
		out, err = priced(ctx, tx, vehicleID, charges)
		return err
	})
	return out, err
}

// FindCharge implements api.Reader, like ListCharges.
func (s *Store) FindCharge(ctx context.Context, accountID, vehicleID string, ref api.EventRef) (api.PricedCharges, bool, error) {
	where, args := eventRef(vehicleID, ref)
	var out api.PricedCharges
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		charges, err := scanChargesOf(ctx, tx, chargeEvents, where, args...)
		if err != nil || len(charges) == 0 {
			return err
		}
		out, err = priced(ctx, tx, vehicleID, charges)
		return err
	})
	return out, len(out.Charges) > 0, err
}

// eventRef returns the WHERE clause of an event: a derived one by its detection time, an
// imported one by its source and its ID there.
func eventRef(vehicleID string, ref api.EventRef) (string, []any) {
	if ref.Source != "" {
		return "vehicle_id = $1 AND import_key = $2", []any{vehicleID, importKey(ref.Source, ref.SourceID)}
	}
	return "vehicle_id = $1 AND detected_at = $2 AND import_key = ''", []any{vehicleID, ref.DetectedAt}
}

// importKey is what tells the imported events apart, as the API's IDs do.
func importKey(source, sourceID string) string { return source + ":" + sourceID }

// periodEvents keeps the events that started in [$2, $3), and the latest one that
// started before $2: it opens the first parked interval of the period.
func periodEvents(relation string) string {
	return `vehicle_id = $1 AND started_after < $3 AND (started_after >= $2 OR (started_after, detected_at, import_key) = (
		SELECT started_after, detected_at, import_key FROM ` + relation + ` WHERE vehicle_id = $1 AND started_after < $2
		ORDER BY started_after DESC, detected_at DESC, import_key DESC LIMIT 1))
	ORDER BY started_after, detected_at, import_key`
}

// PeriodEvents implements api.Reader, derived and imported events. The events, the
// costs' and the orphans are read from one snapshot, so that a derivation saved
// meanwhile cannot show half of its events, nor an entered cost both orphaned and taken.
func (s *Store) PeriodEvents(ctx context.Context, accountID, vehicleID string, from, to time.Time) (api.Period, error) {
	var out api.Period
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		if out.Trips, err = scanTrips(ctx, tx, tripEvents, periodEvents(tripEvents), vehicleID, from, to); err != nil {
			return err
		}
		charges, err := scanChargesOf(ctx, tx, chargeEvents, periodEvents(chargeEvents), vehicleID, from, to)
		if err != nil {
			return err
		}
		if out.PricedCharges, err = priced(ctx, tx, vehicleID, charges); err != nil {
			return err
		}
		rows, err := scanEntered(ctx, tx, "vehicle_id = $1 ORDER BY window_after DESC, charge_detected_at DESC, id", vehicleID)
		if err != nil {
			return err
		}
		orphans, err := orphansOf(ctx, tx, rows)
		out.Orphans = costsOf(orphans)
		return err
	})
	return out, err
}

// eventPage returns the WHERE clause, order and limit of a page of events, newest first;
// without a limit, every event.
func eventPage(vehicleID string, q api.EventQuery) (string, []any) {
	where, args := "vehicle_id = $1", []any{vehicleID}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if !q.From.IsZero() {
		where += " AND ended_before > " + arg(q.From)
	}
	if !q.To.IsZero() {
		where += " AND started_after < " + arg(q.To)
	}
	if k := q.After; k != nil {
		where += " AND (started_after, detected_at, import_key) < (" + arg(k.StartedAfter) + ", " + arg(k.DetectedAt) + ", " + arg(k.Import) + ")"
	}
	// Not in the return statement: the order between arg's append and reading args
	// there is unspecified.
	where += " ORDER BY started_after DESC, detected_at DESC, import_key DESC"
	if q.Limit > 0 {
		where += " LIMIT " + arg(q.Limit)
	}
	return where, args
}

// The trips as one relation, named trips. An imported trip's bounds are its exact times,
// its detected_at the time of its import, and its import_key (empty for a derived one)
// tells it apart: two sources may start a trip at the same time.
const (
	derivedTripColumns = `vehicle_id, detected_at, '' AS import_key, reconstructed,
		started_after, started_before, ended_after, ended_before,
		start_odometer_km, end_odometer_km, distance_km, start_soc, end_soc,
		start_range_km, end_range_km, energy_kwh, start_lat, start_lon, end_lat, end_lon,
		trip_meter_km, consumption_kwh_per_100km, capacity_kwh, capacity_source, sources,
		NULL::text AS source, NULL::text AS source_id, NULL::text AS start_address, NULL::text AS end_address
		FROM trips`
	importedTripColumns = `vehicle_id, imported_at, source || ':' || source_id, false,
		started_at, started_at, ended_at, ended_at,
		start_odometer_km, end_odometer_km, distance_km, start_soc, end_soc,
		NULL, NULL, energy_kwh, start_lat, start_lon, end_lat, end_lon,
		NULL, NULL, capacity_kwh, capacity_source, ARRAY[source],
		source, source_id, start_address, end_address
		FROM imported_trips`
	derivedTrips = `(SELECT ` + derivedTripColumns + `) trips`
	tripEvents   = `(SELECT ` + derivedTripColumns + ` UNION ALL SELECT ` + importedTripColumns + `) trips`
)

// The charges, as the trips. An imported charge's energy is a measured flow, its
// energy_power_kwh.
const (
	derivedChargeColumns = `vehicle_id, detected_at, '' AS import_key, reconstructed,
		started_after, started_before, ended_after, ended_before,
		charge_type, start_soc, end_soc, target_soc, energy_soc_kwh, energy_power_kwh, lat, lon,
		capacity_kwh, capacity_source, span_start_soc, span_end_soc, span_energy_kwh,
		span_max_gap_s, span_duration_s, odometer_km, sources, energy_measured_kwh,
		NULL::text AS source, NULL::text AS source_id, NULL::text AS address,
		NULL::double precision AS average_power_kw, NULL::bigint AS cost_minor, NULL::text AS cost_currency
		FROM charges`
	importedChargeColumns = `vehicle_id, imported_at, source || ':' || source_id, false,
		started_at, started_at, ended_at, ended_at,
		charge_type, start_soc, end_soc, NULL, energy_soc_kwh, energy_kwh, lat, lon,
		capacity_kwh, capacity_source, NULL, NULL, NULL,
		NULL, NULL, NULL, ARRAY[source], NULL,
		source, source_id, address, average_power_kw, cost_minor, cost_currency
		FROM imported_charges`
	derivedCharges = `(SELECT ` + derivedChargeColumns + `) charges`
	chargeEvents   = `(SELECT ` + derivedChargeColumns + ` UNION ALL SELECT ` + importedChargeColumns + `) charges`
)

func (s *Store) queryTrips(ctx context.Context, accountID, relation, where string, args ...any) ([]core.Trip, error) {
	var out []core.Trip
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		out, err = scanTrips(ctx, tx, relation, where, args...)
		return err
	})
	return out, err
}

func scanTrips(ctx context.Context, tx pgx.Tx, relation, where string, args ...any) ([]core.Trip, error) {
	rows, err := tx.Query(ctx, `
		SELECT detected_at, reconstructed, started_after, started_before, ended_after, ended_before,
			start_odometer_km, end_odometer_km, distance_km, start_soc, end_soc,
			start_range_km, end_range_km, energy_kwh, start_lat, start_lon, end_lat, end_lon,
			trip_meter_km, consumption_kwh_per_100km, capacity_kwh, capacity_source, sources,
			source, source_id, start_address, end_address
		FROM `+relation+` WHERE `+where, args...)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by inAccount
	}
	var out []core.Trip
	var t core.Trip
	var odo0, odo1, dist, soc0, soc1, rng0, rng1, kwh, lat0, lon0, lat1, lon1, meter, cons, capKWh *float64
	var capSource, source, sourceID, addr0, addr1 *string
	var sources []string
	_, err = pgx.ForEachRow(rows, []any{
		&t.DetectedAt, &t.Reconstructed, &t.Start.After, &t.Start.Before, &t.End.After, &t.End.Before,
		&odo0, &odo1, &dist, &soc0, &soc1, &rng0, &rng1, &kwh, &lat0, &lon0, &lat1, &lon1, &meter, &cons, &capKWh, &capSource,
		&sources, &source, &sourceID, &addr0, &addr1,
	}, func() error {
		out = append(out, core.Trip{
			DetectedAt: t.DetectedAt.UTC(), Reconstructed: t.Reconstructed,
			Start:           core.Bounds{After: t.Start.After.UTC(), Before: t.Start.Before.UTC()},
			End:             core.Bounds{After: t.End.After.UTC(), Before: t.End.Before.UTC()},
			Sources:         slices.Clone(sources),
			Import:          importOf(source, sourceID, t.DetectedAt, addr0, addr1),
			StartOdometerKm: val(odo0), EndOdometerKm: val(odo1), DistanceKm: val(dist),
			StartSoC: val(soc0), EndSoC: val(soc1), StartRangeKm: val(rng0), EndRangeKm: val(rng1),
			EnergyKWh: val(kwh), From: position(lat0, lon0), To: position(lat1, lon1),
			TripMeterKm: val(meter), ConsumptionKWhPer100km: val(cons), Capacity: capacity(capKWh, capSource),
		})
		return nil
	})
	return out, err //nolint:wrapcheck // wrapped by inAccount
}

// importOf is what an imported event keeps of its import; nothing for a derived one.
func importOf(source, sourceID *string, importedAt time.Time, addr0, addr1 *string) core.Value[core.Import] {
	if source == nil || sourceID == nil {
		return core.Value[core.Import]{}
	}
	imp := core.Import{Source: *source, SourceID: *sourceID, ImportedAt: importedAt.UTC()}
	if addr0 != nil {
		imp.StartAddress = *addr0
	}
	if addr1 != nil {
		imp.EndAddress = *addr1
	}
	return core.Value[core.Import]{V: imp, OK: true}
}

func (s *Store) queryCharges(ctx context.Context, accountID, where string, args ...any) ([]core.Charge, error) {
	var out []core.Charge
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		out, err = scanCharges(ctx, tx, where, args...)
		return err
	})
	return out, err
}

// scanCharges reads derived charges: those an entered cost may go to.
func scanCharges(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]core.Charge, error) {
	return scanChargesOf(ctx, tx, derivedCharges, where, args...)
}

func scanChargesOf(ctx context.Context, tx pgx.Tx, relation, where string, args ...any) ([]core.Charge, error) {
	rows, err := tx.Query(ctx, `
		SELECT detected_at, reconstructed, started_after, started_before, ended_after, ended_before,
			charge_type, start_soc, end_soc, target_soc, energy_soc_kwh, energy_power_kwh, lat, lon,
			capacity_kwh, capacity_source, span_start_soc, span_end_soc, span_energy_kwh,
			span_max_gap_s, span_duration_s, odometer_km, sources, energy_measured_kwh,
			source, source_id, address, average_power_kw, cost_minor, cost_currency
		FROM `+relation+` WHERE `+where, args...)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by inAccount
	}
	var out []core.Charge
	var c core.Charge
	var typ, capSource, source, sourceID, addr, currency *string
	var soc0, soc1, target, esoc, epow, emeas, lat, lon, capKWh, span0, span1, spanKWh, gapS, durS, odo, power *float64
	var costMinor *int64
	var sources []string
	_, err = pgx.ForEachRow(rows, []any{
		&c.DetectedAt, &c.Reconstructed, &c.Start.After, &c.Start.Before, &c.End.After, &c.End.Before,
		&typ, &soc0, &soc1, &target, &esoc, &epow, &lat, &lon, &capKWh, &capSource,
		&span0, &span1, &spanKWh, &gapS, &durS, &odo, &sources, &emeas,
		&source, &sourceID, &addr, &power, &costMinor, &currency,
	}, func() error {
		ch := core.Charge{
			DetectedAt: c.DetectedAt.UTC(), Reconstructed: c.Reconstructed,
			Start:    core.Bounds{After: c.Start.After.UTC(), Before: c.Start.Before.UTC()},
			End:      core.Bounds{After: c.End.After.UTC(), Before: c.End.Before.UTC()},
			Sources:  slices.Clone(sources),
			Import:   importOf(source, sourceID, c.DetectedAt, addr, nil),
			StartSoC: val(soc0), EndSoC: val(soc1), TargetSoC: val(target),
			EnergySoCKWh: val(esoc), EnergyPowerKWh: val(epow), EnergyMeasuredKWh: val(emeas),
			Position: position(lat, lon),
			Span:     span(span0, span1, spanKWh, gapS, durS), OdometerKm: val(odo),
			Capacity: capacity(capKWh, capSource), AveragePowerKW: val(power),
		}
		if typ != nil {
			ch.Type = core.Value[core.ChargeType]{V: core.ChargeType(*typ), OK: true}
		}
		if costMinor != nil && currency != nil {
			ch.PaidCost = core.Value[core.Amount]{V: core.Amount{Currency: *currency, Minor: *costMinor}, OK: true}
		}
		out = append(out, ch)
		return nil
	})
	return out, err //nolint:wrapcheck // wrapped by inAccount
}

// opt maps an absent value to NULL.
func opt[T any](v core.Value[T]) *T {
	if !v.OK {
		return nil
	}
	return &v.V
}

func val(p *float64) core.Value[float64] {
	if p == nil {
		return core.Value[float64]{}
	}
	return core.Value[float64]{V: *p, OK: true}
}

func latLon(p core.Value[core.Position]) [2]*float64 {
	if !p.OK {
		return [2]*float64{}
	}
	return [2]*float64{&p.V.Lat, &p.V.Lon}
}

func position(lat, lon *float64) core.Value[core.Position] {
	if lat == nil || lon == nil {
		return core.Value[core.Position]{}
	}
	return core.Value[core.Position]{V: core.Position{Lat: *lat, Lon: *lon}, OK: true}
}

// capacityColumns maps a capacity to capacity_kwh and capacity_source, both NULL when
// it is unknown.
func capacityColumns(c core.Value[core.Capacity]) (*float64, *string) {
	if !c.OK {
		return nil, nil
	}
	src := string(c.V.Source)
	return &c.V.KWh, &src
}

// spanColumns maps the retained span to its five columns, all NULL when there is none.
func spanColumns(s core.Value[core.PowerSpan]) (start, end, kwh, gap, dur *float64) {
	if !s.OK {
		return nil, nil, nil, nil, nil
	}
	secs, durS := s.V.MaxGap.Seconds(), s.V.Duration.Seconds()
	return &s.V.StartSoC, &s.V.EndSoC, &s.V.EnergyKWh, &secs, &durS
}

// span maps the five span columns back, none when they are all NULL.
func span(start, end, kwh, gap, dur *float64) core.Value[core.PowerSpan] {
	if start == nil || end == nil || kwh == nil || gap == nil || dur == nil {
		return core.Value[core.PowerSpan]{}
	}
	return core.Value[core.PowerSpan]{V: core.PowerSpan{
		StartSoC: *start, EndSoC: *end, EnergyKWh: *kwh,
		MaxGap:   time.Duration(*gap * float64(time.Second)),
		Duration: time.Duration(*dur * float64(time.Second)),
	}, OK: true}
}

func capacity(kwh *float64, source *string) core.Value[core.Capacity] {
	if kwh == nil || source == nil {
		return core.Value[core.Capacity]{}
	}
	return core.Value[core.Capacity]{V: core.Capacity{KWh: *kwh, Source: core.CapacitySource(*source)}, OK: true}
}
