package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/core"
)

// ImportedTrip is a trip to import, with its track when the source gives one: a GeoJSON
// LineString.
type ImportedTrip struct {
	Trip  core.Trip
	Track []byte // nil: none given, a track stored before stays
}

// ImportResult tells what an import wrote.
type ImportResult struct {
	Saved   int // events written, new or replacing the same event of the same source
	Skipped int // events starting at or after Cutoff
	// Cutoff is the vehicle's first snapshot, of any source, or the time of the import
	// when it has none: only what starts before it is imported.
	Cutoff time.Time
}

// ErrNotImported is returned for an event without its import: its source and its ID
// there.
var ErrNotImported = errors.New("an imported event needs its source and its ID there")

// SaveImport writes the trips and charges imported from another application for the
// vehicle, in one transaction (ADR 0030). Each event names its import (core.Import):
// an event of the same source and ID replaces the stored one, which keeps its id and
// its track. Only the events that start before the vehicle's first snapshot are
// written, so that none overlaps a derived one; the others are counted as skipped.
// Times are each event's Start.After and End.Before, exact.
//
// A snapshot written meanwhile by the collector is not waited for: it reads the
// present, and an import the past.
func (s *Store) SaveImport(ctx context.Context, accountID, vehicleID string, trips []ImportedTrip, charges []core.Charge) (ImportResult, error) {
	var res ImportResult
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT coalesce((SELECT min(fetched_at) FROM snapshots WHERE vehicle_id = $1), now())`,
			vehicleID).Scan(&res.Cutoff)
		if err != nil {
			return fmt.Errorf("cutoff: %w", err)
		}
		res.Cutoff = res.Cutoff.UTC()
		for _, it := range trips {
			t := it.Trip
			if !t.Import.OK {
				return ErrNotImported
			}
			if !t.Start.After.Before(res.Cutoff) {
				res.Skipped++
				continue
			}
			from, to := latLon(t.From), latLon(t.To)
			kwh, source := capacityColumns(t.Capacity)
			imp := t.Import.V
			var id string
			err := tx.QueryRow(ctx, `
				INSERT INTO imported_trips (account_id, vehicle_id, source, source_id, started_at, ended_at,
					distance_km, start_odometer_km, end_odometer_km, start_soc, end_soc, energy_kwh,
					capacity_kwh, capacity_source, start_lat, start_lon, end_lat, end_lon, start_address, end_address)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
				ON CONFLICT (vehicle_id, source, source_id) DO UPDATE SET imported_at = now(),
					started_at = EXCLUDED.started_at, ended_at = EXCLUDED.ended_at,
					distance_km = EXCLUDED.distance_km, start_odometer_km = EXCLUDED.start_odometer_km,
					end_odometer_km = EXCLUDED.end_odometer_km, start_soc = EXCLUDED.start_soc, end_soc = EXCLUDED.end_soc,
					energy_kwh = EXCLUDED.energy_kwh, capacity_kwh = EXCLUDED.capacity_kwh,
					capacity_source = EXCLUDED.capacity_source, start_lat = EXCLUDED.start_lat,
					start_lon = EXCLUDED.start_lon, end_lat = EXCLUDED.end_lat, end_lon = EXCLUDED.end_lon,
					start_address = EXCLUDED.start_address, end_address = EXCLUDED.end_address
				RETURNING id`,
				accountID, vehicleID, imp.Source, imp.SourceID, t.Start.After, t.End.Before,
				opt(t.DistanceKm), opt(t.StartOdometerKm), opt(t.EndOdometerKm), opt(t.StartSoC), opt(t.EndSoC),
				opt(t.EnergyKWh), kwh, source, from[0], from[1], to[0], to[1],
				nonEmpty(imp.StartAddress), nonEmpty(imp.EndAddress)).Scan(&id)
			if err != nil {
				return fmt.Errorf("imported trip: %w", err)
			}
			if it.Track != nil {
				_, err := tx.Exec(ctx, `
					INSERT INTO imported_tracks (account_id, trip_id, track) VALUES ($1, $2, $3)
					ON CONFLICT (trip_id) DO UPDATE SET track = EXCLUDED.track`, accountID, id, it.Track)
				if err != nil {
					return fmt.Errorf("imported track: %w", err)
				}
			}
			res.Saved++
		}
		for _, c := range charges {
			if !c.Import.OK {
				return ErrNotImported
			}
			if !c.Start.After.Before(res.Cutoff) {
				res.Skipped++
				continue
			}
			pos := latLon(c.Position)
			kwh, source := capacityColumns(c.Capacity)
			imp := c.Import.V
			var costMinor *int64
			var currency *string
			if p := c.PaidCost; p.OK {
				costMinor, currency = &p.V.Minor, &p.V.Currency
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO imported_charges (account_id, vehicle_id, source, source_id, started_at, ended_at,
					charge_type, start_soc, end_soc, energy_kwh, energy_soc_kwh, capacity_kwh, capacity_source,
					average_power_kw, cost_minor, cost_currency, lat, lon, address)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
				ON CONFLICT (vehicle_id, source, source_id) DO UPDATE SET imported_at = now(),
					started_at = EXCLUDED.started_at, ended_at = EXCLUDED.ended_at, charge_type = EXCLUDED.charge_type,
					start_soc = EXCLUDED.start_soc, end_soc = EXCLUDED.end_soc, energy_kwh = EXCLUDED.energy_kwh,
					energy_soc_kwh = EXCLUDED.energy_soc_kwh, capacity_kwh = EXCLUDED.capacity_kwh,
					capacity_source = EXCLUDED.capacity_source, average_power_kw = EXCLUDED.average_power_kw,
					cost_minor = EXCLUDED.cost_minor, cost_currency = EXCLUDED.cost_currency,
					lat = EXCLUDED.lat, lon = EXCLUDED.lon, address = EXCLUDED.address`,
				accountID, vehicleID, imp.Source, imp.SourceID, c.Start.After, c.End.Before,
				opt(c.Type), opt(c.StartSoC), opt(c.EndSoC), opt(c.EnergyPowerKWh), opt(c.EnergySoCKWh), kwh, source,
				opt(c.AveragePowerKW), costMinor, currency, pos[0], pos[1], nonEmpty(imp.StartAddress))
			if err != nil {
				return fmt.Errorf("imported charge: %w", err)
			}
			res.Saved++
		}
		return nil
	})
	return res, err
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Imports implements api.Imports.
func (s *Store) Imports(ctx context.Context, accountID, vehicleID string) ([]api.Import, error) {
	var out []api.Import
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT source, count(*) FILTER (WHERE trip), count(*) FILTER (WHERE NOT trip),
				min(started_at), max(started_at), max(imported_at)
			FROM (SELECT source, started_at, imported_at, true AS trip FROM imported_trips WHERE vehicle_id = $1
				UNION ALL
				SELECT source, started_at, imported_at, false FROM imported_charges WHERE vehicle_id = $1) i
			GROUP BY source ORDER BY source`, vehicleID)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by readInAccount
		}
		var i api.Import
		_, err = pgx.ForEachRow(rows, []any{&i.Source, &i.Trips, &i.Charges, &i.StartedFrom, &i.StartedTo, &i.LastImportedAt}, func() error {
			i.StartedFrom, i.StartedTo, i.LastImportedAt = i.StartedFrom.UTC(), i.StartedTo.UTC(), i.LastImportedAt.UTC()
			out = append(out, i)
			return nil
		})
		return err //nolint:wrapcheck // wrapped by readInAccount
	})
	return out, err
}

// DeleteImport implements api.Imports. The tracks go with their trips.
func (s *Store) DeleteImport(ctx context.Context, accountID, vehicleID, source string) (bool, error) {
	var n int64
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		for _, table := range []string{"imported_trips", "imported_charges"} {
			tag, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE vehicle_id = $1 AND source = $2", vehicleID, source)
			if err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
			n += tag.RowsAffected()
		}
		return nil
	})
	return n > 0, err
}
