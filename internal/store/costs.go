package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"runsten/internal/api"
	"runsten/internal/core"
)

// lockPrices serializes, within an account, the writes that give amounts a unit (a
// tariff, an entered cost) with a change of currency, and the creations of places with
// one another. Under READ COMMITTED each statement takes a new snapshot: once the lock
// is granted, the checks of SetCurrency and CreatePlace see every such write committed
// before.
func lockPrices(ctx context.Context, tx pgx.Tx, accountID string) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('runsten_prices:' || $1))", accountID); err != nil {
		return fmt.Errorf("prices lock: %w", err)
	}
	return nil
}

// Currency returns the account's currency; unknown until it is set.
func (s *Store) Currency(ctx context.Context, accountID string) (core.Value[core.Currency], error) {
	var out core.Value[core.Currency]
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		out, err = currency(ctx, tx)
		return err
	})
	return out, err
}

func currency(ctx context.Context, tx pgx.Tx) (core.Value[core.Currency], error) {
	code, err := currencyCode(ctx, tx)
	if err != nil || code == "" {
		return core.Value[core.Currency]{}, err
	}
	c, ok := core.CurrencyOf(code)
	if !ok {
		return core.Value[core.Currency]{}, fmt.Errorf("currency %s is not accepted", code)
	}
	return core.Value[core.Currency]{V: c, OK: true}, nil
}

// currencyCode is the account's currency code, empty if unset.
func currencyCode(ctx context.Context, tx pgx.Tx) (string, error) {
	var code string
	err := tx.QueryRow(ctx, "SELECT currency FROM account_settings").Scan(&code)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("currency: %w", err)
	}
	return code, nil
}

// SetCurrency sets the account's currency. Changing it while a tariff or an entered cost
// exists fails with api.ErrCurrencyInUse: their amounts would change unit. Setting the
// first one does not: amounts entered before had no unit yet.
func (s *Store) SetCurrency(ctx context.Context, accountID string, c core.Currency) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		if err := lockPrices(ctx, tx, accountID); err != nil {
			return err
		}
		code, err := currencyCode(ctx, tx)
		if err != nil {
			return err
		}
		if code != "" && code != c.Code {
			var inUse bool
			err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT FROM place_tariffs) OR EXISTS (SELECT FROM charge_costs)").Scan(&inUse)
			if err != nil {
				return fmt.Errorf("currency in use: %w", err)
			}
			if inUse {
				return api.ErrCurrencyInUse
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO account_settings (account_id, currency) VALUES ($1, $2)
			ON CONFLICT (account_id) DO UPDATE SET currency = EXCLUDED.currency`, accountID, c.Code)
		if err != nil {
			return fmt.Errorf("set currency: %w", err)
		}
		return nil
	})
}

// windowJSON is a price window in place_tariffs.windows (format in migration 0005).
type windowJSON struct {
	Days        []time.Weekday `json:"days"`
	From        int            `json:"from"`
	To          int            `json:"to"`
	Months      []time.Month   `json:"months"`
	PricePerKWh float64        `json:"price_per_kwh"`
}

func windowsJSON(ws []core.PriceWindow) []windowJSON {
	out := make([]windowJSON, len(ws))
	for i, w := range ws {
		// Never null: an empty list means every day or month.
		out[i] = windowJSON{
			Days: append([]time.Weekday{}, w.Days...), From: w.From, To: w.To,
			Months: append([]time.Month{}, w.Months...), PricePerKWh: w.PricePerKWh,
		}
	}
	return out
}

func priceWindows(ws []windowJSON) []core.PriceWindow {
	var out []core.PriceWindow
	for _, w := range ws {
		out = append(out, core.PriceWindow{
			Days: nilIfEmpty(w.Days), From: w.From, To: w.To,
			Months: nilIfEmpty(w.Months), PricePerKWh: w.PricePerKWh,
		})
	}
	return out
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// Places returns the account's places with their tariff, in the order they were
// created: the stable order core.PlaceOf decides by.
func (s *Store) Places(ctx context.Context, accountID string) ([]core.Place, error) {
	var out []core.Place
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		out, err = queryPlaces(ctx, tx, "TRUE")
		return err
	})
	return out, err
}

// Place returns one of the account's places.
func (s *Store) Place(ctx context.Context, accountID, placeID string) (core.Place, bool, error) {
	var out []core.Place
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		out, err = queryPlaces(ctx, tx, "id = $1", placeID)
		return err
	})
	if err != nil || len(out) == 0 {
		return core.Place{}, false, err
	}
	return out[0], true, nil
}

// Pricing reads what the costs of the account's charges depend on, in one transaction:
// its currency and its places. p holds the assumptions; the onboard charger's power is
// unknown for now.
func (s *Store) Pricing(ctx context.Context, accountID string, p core.CostParams) (core.Pricing, error) {
	out := core.Pricing{Params: p}
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		if out.Currency, err = currency(ctx, tx); err != nil {
			return err
		}
		out.Places, err = queryPlaces(ctx, tx, "TRUE")
		return err
	})
	return out, err
}

func queryPlaces(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]core.Place, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, name, lat, lon, radius_m, time_zone, without_position, max_power_kw, efficiency
		FROM places WHERE `+where+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("places: %w", err)
	}
	var out []core.Place
	var p core.Place
	var zone string
	var power, eff *float64
	_, err = pgx.ForEachRow(rows, []any{
		&p.ID, &p.Name, &p.Position.Lat, &p.Position.Lon, &p.RadiusM, &zone, &p.WithoutPosition, &power, &eff,
	}, func() error {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			// The ID only: a place's name may tell where its owner lives.
			return fmt.Errorf("time zone of place %s: %w", p.ID, err)
		}
		out = append(out, core.Place{
			ID: p.ID, Name: p.Name, Position: p.Position, RadiusM: p.RadiusM, Location: loc,
			WithoutPosition: p.WithoutPosition, MaxPowerKW: val(power), Efficiency: val(eff),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("places: %w", err)
	}
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]string, len(out))
	for i, pl := range out {
		ids[i] = pl.ID
	}
	// ::float8: numeric(10,5) to the nearest double, as ParseFloat of its text would.
	rows, err = tx.Query(ctx, `
		SELECT place_id, valid_from, price_per_kwh::float8, windows FROM place_tariffs
		WHERE place_id = ANY($1::uuid[]) ORDER BY place_id, valid_from`, ids)
	if err != nil {
		return nil, fmt.Errorf("tariffs: %w", err)
	}
	var placeID string
	var from time.Time
	var price float64
	var windows []windowJSON
	_, err = pgx.ForEachRow(rows, []any{&placeID, &from, &price, &windows}, func() error {
		i := slices.IndexFunc(out, func(pl core.Place) bool { return pl.ID == placeID })
		out[i].Tariff = append(out[i].Tariff, core.TariffVersion{
			ValidFrom:   core.Date{Year: from.Year(), Month: from.Month(), Day: from.Day()},
			PricePerKWh: price, Windows: priceWindows(windows),
		})
		windows = nil
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("tariffs: %w", err)
	}
	return out, nil
}

// placeError maps the refusal of a second place without a position to its error.
func placeError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "places_one_without_position" {
		return api.ErrWithoutPositionTaken
	}
	return fmt.Errorf("place: %w", err)
}

func zoneName(loc *time.Location) string {
	if loc == nil {
		return "UTC"
	}
	return loc.String()
}

// CreatePlace creates a place with its tariff, and returns its ID; p.ID is ignored.
// api.ErrTooManyPlaces when the account has api.MaxPlaces already.
func (s *Store) CreatePlace(ctx context.Context, accountID string, p core.Place) (string, error) {
	var id string
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		// Counted under the lock: two creations at once cannot both pass at MaxPlaces - 1.
		if err := lockPrices(ctx, tx, accountID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM places").Scan(&n); err != nil {
			return fmt.Errorf("count places: %w", err)
		}
		if n >= api.MaxPlaces {
			return api.ErrTooManyPlaces
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO places (account_id, name, lat, lon, radius_m, time_zone, without_position, max_power_kw, efficiency)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			accountID, p.Name, p.Position.Lat, p.Position.Lon, p.RadiusM, zoneName(p.Location), p.WithoutPosition,
			opt(p.MaxPowerKW), opt(p.Efficiency)).Scan(&id)
		if err != nil {
			return placeError(err)
		}
		return saveTariff(ctx, tx, accountID, id, p.Tariff)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// ReplacePlace replaces the place p.ID and all the versions of its tariff, atomically;
// api.ErrNotFound if the account has no such place. It keeps its rank among the places.
func (s *Store) ReplacePlace(ctx context.Context, accountID string, p core.Place) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE places SET name = $2, lat = $3, lon = $4, radius_m = $5, time_zone = $6,
				without_position = $7, max_power_kw = $8, efficiency = $9
			WHERE id = $1`,
			p.ID, p.Name, p.Position.Lat, p.Position.Lon, p.RadiusM, zoneName(p.Location), p.WithoutPosition,
			opt(p.MaxPowerKW), opt(p.Efficiency))
		switch {
		case err != nil:
			return placeError(err)
		case tag.RowsAffected() == 0:
			return api.ErrNotFound
		}
		if _, err := tx.Exec(ctx, "DELETE FROM place_tariffs WHERE place_id = $1", p.ID); err != nil {
			return fmt.Errorf("delete tariff: %w", err)
		}
		return saveTariff(ctx, tx, accountID, p.ID, p.Tariff)
	})
}

func saveTariff(ctx context.Context, tx pgx.Tx, accountID, placeID string, versions []core.TariffVersion) error {
	if len(versions) == 0 {
		return nil
	}
	if err := lockPrices(ctx, tx, accountID); err != nil {
		return err
	}
	for _, v := range versions {
		_, err := tx.Exec(ctx, `
			INSERT INTO place_tariffs (account_id, place_id, valid_from, price_per_kwh, windows)
			VALUES ($1, $2, $3, $4, $5)`,
			accountID, placeID, time.Date(v.ValidFrom.Year, v.ValidFrom.Month, v.ValidFrom.Day, 0, 0, 0, 0, time.UTC),
			v.PricePerKWh, windowsJSON(v.Windows))
		if err != nil {
			return fmt.Errorf("insert tariff: %w", err)
		}
	}
	return nil
}

// DeletePlace deletes a place and its tariff; api.ErrNotFound if the account has no
// such place.
func (s *Store) DeletePlace(ctx context.Context, accountID, placeID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM places WHERE id = $1", placeID)
		switch {
		case err != nil:
			return fmt.Errorf("delete place: %w", err)
		case tag.RowsAffected() == 0:
			return api.ErrNotFound
		}
		return nil
	})
}

func scanEntered(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]api.EnteredCost, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, vehicle_id, charge_detected_at, window_after, window_before, amount_minor, energy_kwh, note, entered_at
		FROM charge_costs WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("entered costs: %w", err)
	}
	var out []api.EnteredCost
	var e api.EnteredCost
	var kwh *float64
	_, err = pgx.ForEachRow(rows, []any{
		&e.ID, &e.VehicleID, &e.ChargeDetectedAt, &e.WindowAfter, &e.WindowBefore, &e.AmountMinor, &kwh, &e.Note, &e.EnteredAt,
	}, func() error {
		out = append(out, api.EnteredCost{
			ID: e.ID, VehicleID: e.VehicleID, EnteredAt: e.EnteredAt.UTC(),
			EnteredCost: core.EnteredCost{
				ChargeDetectedAt: e.ChargeDetectedAt.UTC(), WindowAfter: e.WindowAfter.UTC(), WindowBefore: e.WindowBefore.UTC(),
				AmountMinor: e.AmountMinor, EnergyKWh: val(kwh), Note: e.Note,
			},
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("entered costs: %w", err)
	}
	return out, nil
}

func costsOf(rows []api.EnteredCost) []core.EnteredCost {
	out := make([]core.EnteredCost, len(rows))
	for i, r := range rows {
		out[i] = r.EnteredCost
	}
	return out
}

// EnteredCosts returns the vehicle's entered costs, attached or not, oldest first.
func (s *Store) EnteredCosts(ctx context.Context, accountID, vehicleID string) ([]core.EnteredCost, error) {
	var rows []api.EnteredCost
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		rows, err = scanEntered(ctx, tx, "vehicle_id = $1 ORDER BY window_after, charge_detected_at", vehicleID)
		return err
	})
	return costsOf(rows), err
}

// EnteredCostsFor returns what attaching the entered costs to some of the vehicle's
// charges (a page of the list, a period) needs, for core.AttachCosts to attach them as it
// would over the whole history: the charges given, first and in their order, then the
// others that decide, and the entered costs of them all. The orphans core.AttachCosts
// finds among them are not those of the history: OrphanCosts gives these.
//
// The given charges alone would not do. A cost whose charge is elsewhere would go to
// one of them by overlap; one overlapping them and a charge elsewhere would be taken
// instead of orphaned; one overlapping them and a charge elsewhere already taken would
// be orphaned instead of taken. So, besides the given charges: the costs they may take
// (at their detection time, or overlapping them); the charges these costs may go to (at
// their detection time, or overlapping their window); and the costs at the detection
// time of those, which take them first. A cost further away takes none of the given
// charges, and changes nothing for those that do.
func (s *Store) EnteredCostsFor(ctx context.Context, accountID, vehicleID string, charges []core.Charge) ([]core.Charge, []core.EnteredCost, error) {
	var all []core.Charge
	var rows []api.EnteredCost
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var err error
		all, rows, err = costsFor(ctx, tx, vehicleID, charges)
		return err
	})
	return all, costsOf(rows), err
}

// mayTake selects the entered costs of vehicle $1 that the charges detected at $2, with
// windows from $3 to $4, may take, as k.
const mayTake = `k.vehicle_id = $1 AND (k.charge_detected_at = ANY($2) OR EXISTS (
	SELECT FROM unnest($3::timestamptz[], $4::timestamptz[]) g (a, b)
	WHERE g.a < k.window_before AND k.window_after < g.b))`

// overlapsCharge tells whether the entered cost k may go to the row of charges.
const overlapsCharge = `(k.charge_detected_at = charges.detected_at OR
	(charges.started_after < k.window_before AND k.window_after < charges.ended_before))`

// priced reads what the costs of the vehicle's charges depend on, besides the charges.
func priced(ctx context.Context, tx pgx.Tx, vehicleID string, charges []core.Charge) (api.PricedCharges, error) {
	out := api.PricedCharges{Charges: charges}
	var err error
	if out.Currency, err = currency(ctx, tx); err != nil {
		return out, err
	}
	if out.Places, err = queryPlaces(ctx, tx, "TRUE"); err != nil {
		return out, err
	}
	all, rows, err := costsFor(ctx, tx, vehicleID, charges)
	if err != nil {
		return out, err
	}
	out.Deciding, out.Entered = all[len(charges):], costsOf(rows)
	return out, nil
}

// ChargeHistory implements api.Reader: every charge of the account, by vehicle, with all
// its entered costs, from one snapshot.
func (s *Store) ChargeHistory(ctx context.Context, accountID string) ([]api.PricedCharges, error) {
	var out []api.PricedCharges
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		cur, err := currency(ctx, tx)
		if err != nil {
			return err
		}
		places, err := queryPlaces(ctx, tx, "TRUE")
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT id FROM vehicles ORDER BY created_at, vin")
		if err != nil {
			return fmt.Errorf("vehicles: %w", err)
		}
		vehicles, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("vehicles: %w", err)
		}
		for _, v := range vehicles {
			charges, err := scanCharges(ctx, tx, "vehicle_id = $1 ORDER BY started_after, detected_at", v)
			if err != nil {
				return err
			}
			entered, err := scanEntered(ctx, tx, "vehicle_id = $1 ORDER BY window_after, charge_detected_at", v)
			if err != nil {
				return err
			}
			out = append(out, api.PricedCharges{Charges: charges, Entered: costsOf(entered), Currency: cur, Places: places})
		}
		return nil
	})
	return out, err
}

func costsFor(ctx context.Context, tx pgx.Tx, vehicleID string, charges []core.Charge) ([]core.Charge, []api.EnteredCost, error) {
	detected := make([]time.Time, len(charges))
	after, before := make([]time.Time, len(charges)), make([]time.Time, len(charges))
	for i, c := range charges {
		detected[i], after[i], before[i] = c.DetectedAt, c.Start.After, c.End.Before
	}
	others, err := scanCharges(ctx, tx, `vehicle_id = $1 AND NOT detected_at = ANY($2) AND EXISTS (
		SELECT FROM charge_costs k WHERE `+mayTake+` AND `+overlapsCharge+`)
		ORDER BY started_after, detected_at`, vehicleID, detected, after, before)
	if err != nil {
		return nil, nil, fmt.Errorf("charges of the entered costs: %w", err)
	}
	all := append(slices.Clip(charges), others...)
	for _, c := range others {
		detected = append(detected, c.DetectedAt)
	}
	rows, err := scanEntered(ctx, tx, `id IN (SELECT k.id FROM charge_costs k WHERE `+mayTake+`)
		ORDER BY window_after, charge_detected_at`, vehicleID, detected, after, before)
	return all, rows, err
}

// OrphanCosts returns the account's entered costs that no charge takes, newest first.
func (s *Store) OrphanCosts(ctx context.Context, accountID string) ([]api.EnteredCost, error) {
	var out []api.EnteredCost
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := scanEntered(ctx, tx, "TRUE ORDER BY window_after DESC, charge_detected_at DESC, id")
		if err != nil {
			return err
		}
		out, err = orphansOf(ctx, tx, rows)
		return err
	})
	return out, err
}

// orphansOf keeps, of rows (every entered cost of their vehicles), those that no charge
// takes, in their order.
func orphansOf(ctx context.Context, tx pgx.Tx, rows []api.EnteredCost) ([]api.EnteredCost, error) {
	orphaned := map[string]bool{} // by vehicle and detection time, unique
	key := func(vehicleID string, at time.Time) string { return vehicleID + "/" + at.Format(time.RFC3339Nano) }
	var vehicles []string
	for _, r := range rows {
		if !slices.Contains(vehicles, r.VehicleID) {
			vehicles = append(vehicles, r.VehicleID)
		}
	}
	for _, v := range vehicles {
		// Only the charges some cost may go to decide.
		charges, err := scanCharges(ctx, tx, `vehicle_id = $1 AND EXISTS (
			SELECT FROM charge_costs k WHERE k.vehicle_id = $1 AND `+overlapsCharge+`)
			ORDER BY started_after, detected_at`, v)
		if err != nil {
			return nil, fmt.Errorf("charges of the entered costs: %w", err)
		}
		var costs []core.EnteredCost
		for _, r := range rows {
			if r.VehicleID == v {
				costs = append(costs, r.EnteredCost)
			}
		}
		_, orphans := core.AttachCosts(charges, costs)
		for _, o := range orphans {
			orphaned[key(v, o.ChargeDetectedAt)] = true
		}
	}
	var out []api.EnteredCost
	for _, r := range rows {
		if orphaned[key(r.VehicleID, r.ChargeDetectedAt)] {
			out = append(out, r)
		}
	}
	return out, nil
}

func findCharge(ctx context.Context, tx pgx.Tx, vehicleID string, detectedAt time.Time) (core.Charge, bool, error) {
	charges, err := scanCharges(ctx, tx, "vehicle_id = $1 AND detected_at = $2", vehicleID, detectedAt)
	if err != nil {
		return core.Charge{}, false, fmt.Errorf("charge: %w", err)
	}
	if len(charges) == 0 {
		return core.Charge{}, false, nil
	}
	return charges[0], true, nil
}

// attachedTo returns the entered cost the charge takes, at its detection time or by
// overlap.
func attachedTo(ctx context.Context, tx pgx.Tx, vehicleID string, c core.Charge) (api.EnteredCost, bool, error) {
	all, rows, err := costsFor(ctx, tx, vehicleID, []core.Charge{c})
	if err != nil {
		return api.EnteredCost{}, false, err
	}
	attached, _ := core.AttachCosts(all, costsOf(rows))
	if !attached[0].OK {
		return api.EnteredCost{}, false, nil
	}
	i := slices.IndexFunc(rows, func(r api.EnteredCost) bool { return r.ChargeDetectedAt.Equal(attached[0].V.ChargeDetectedAt) })
	return rows[i], true, nil
}

// SetChargeCost enters the cost of the vehicle's charge detected at e.ChargeDetectedAt,
// with the charge's window as it is now; api.ErrNotFound without such a charge. It
// replaces the cost the charge takes, even one attached by overlap: that one moves to
// the charge's detection time, instead of being orphaned by the new one.
func (s *Store) SetChargeCost(ctx context.Context, accountID, vehicleID string, e core.EnteredCost) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		if err := lockPrices(ctx, tx, accountID); err != nil {
			return err
		}
		c, ok, err := findCharge(ctx, tx, vehicleID, e.ChargeDetectedAt)
		switch {
		case err != nil:
			return err
		case !ok:
			return api.ErrNotFound
		}
		cur, attached, err := attachedTo(ctx, tx, vehicleID, c)
		if err != nil {
			return err
		}
		if attached {
			_, err = tx.Exec(ctx, `
				UPDATE charge_costs SET charge_detected_at = $2, window_after = $3, window_before = $4,
					amount_minor = $5, energy_kwh = $6, note = $7, entered_at = now()
				WHERE id = $1`,
				cur.ID, c.DetectedAt, c.Start.After, c.End.Before, e.AmountMinor, opt(e.EnergyKWh), e.Note)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO charge_costs (account_id, vehicle_id, charge_detected_at, window_after, window_before,
					amount_minor, energy_kwh, note)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				accountID, vehicleID, c.DetectedAt, c.Start.After, c.End.Before, e.AmountMinor, opt(e.EnergyKWh), e.Note)
		}
		if err != nil {
			return fmt.Errorf("save entered cost: %w", err)
		}
		return nil
	})
}

// DeleteChargeCost deletes the entered cost the vehicle's charge detected at detectedAt
// takes; api.ErrNotFound without such a charge, or if it takes none.
func (s *Store) DeleteChargeCost(ctx context.Context, accountID, vehicleID string, detectedAt time.Time) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		c, ok, err := findCharge(ctx, tx, vehicleID, detectedAt)
		switch {
		case err != nil:
			return err
		case !ok:
			return api.ErrNotFound
		}
		cur, attached, err := attachedTo(ctx, tx, vehicleID, c)
		switch {
		case err != nil:
			return err
		case !attached:
			return api.ErrNotFound
		}
		if _, err := tx.Exec(ctx, "DELETE FROM charge_costs WHERE id = $1", cur.ID); err != nil {
			return fmt.Errorf("delete entered cost: %w", err)
		}
		return nil
	})
}

// DeleteEnteredCost deletes an entered cost by its ID, an orphaned one most often;
// api.ErrNotFound if the account has no such cost.
func (s *Store) DeleteEnteredCost(ctx context.Context, accountID, costID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM charge_costs WHERE id = $1", costID)
		switch {
		case err != nil:
			return fmt.Errorf("delete entered cost: %w", err)
		case tag.RowsAffected() == 0:
			return api.ErrNotFound
		}
		return nil
	})
}

// AttachEnteredCost attaches an entered cost, an orphaned one most often, to the charge
// the user chose: it takes the charge's detection time and window. api.ErrNotFound
// without such a cost or charge in the account; api.ErrChargeHasCost if the charge takes
// another cost already.
func (s *Store) AttachEnteredCost(ctx context.Context, accountID, costID, vehicleID string, detectedAt time.Time) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		costs, err := scanEntered(ctx, tx, "id = $1 FOR UPDATE", costID)
		switch {
		case err != nil:
			return err
		case len(costs) == 0:
			return api.ErrNotFound
		}
		c, ok, err := findCharge(ctx, tx, vehicleID, detectedAt)
		switch {
		case err != nil:
			return err
		case !ok:
			return api.ErrNotFound
		}
		cur, attached, err := attachedTo(ctx, tx, vehicleID, c)
		switch {
		case err != nil:
			return err
		case attached && cur.ID == costID && cur.ChargeDetectedAt.Equal(c.DetectedAt):
			return nil
		case attached && cur.ID != costID:
			return api.ErrChargeHasCost
		}
		_, err = tx.Exec(ctx, `
			UPDATE charge_costs SET vehicle_id = $2, charge_detected_at = $3, window_after = $4, window_before = $5
			WHERE id = $1`, costID, vehicleID, c.DetectedAt, c.Start.After, c.End.Before)
		if err != nil {
			return fmt.Errorf("attach entered cost: %w", err)
		}
		return nil
	})
}
