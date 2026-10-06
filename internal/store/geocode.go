package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/core"
	"runsten/internal/geocode"
)

// Addresses implements api.Addresses: the addresses known of the cells, of the account,
// and a request for those not asked for yet, which the geocoding worker then resolves.
// A cell whose geocoder knew no address is left out, as one still unresolved.
func (s *Store) Addresses(ctx context.Context, accountID string, cells []core.GeoCell) (map[core.GeoCell]string, error) {
	out := map[core.GeoCell]string{}
	if len(cells) == 0 {
		return out, nil
	}
	lats, lons := make([]int32, len(cells)), make([]int32, len(cells))
	for i, c := range cells {
		lats[i], lons[i] = c.LatE4, c.LonE4
	}
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO geocoded_positions (account_id, lat_e4, lon_e4, requested_at)
			SELECT $1, lat, lon, now() FROM unnest($2::integer[], $3::integer[]) AS c (lat, lon)
			ON CONFLICT DO NOTHING`, accountID, lats, lons); err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		rows, err := tx.Query(ctx, `
			SELECT g.lat_e4, g.lon_e4, g.address FROM geocoded_positions g
			JOIN unnest($1::integer[], $2::integer[]) AS c (lat, lon) ON (g.lat_e4, g.lon_e4) = (c.lat, c.lon)
			WHERE g.address <> ''`, lats, lons)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		var c core.GeoCell
		var address string
		_, err = pgx.ForEachRow(rows, []any{&c.LatE4, &c.LonE4, &address}, func() error {
			out[c] = address
			return nil
		})
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return nil, fmt.Errorf("addresses: %w", err)
	}
	return out, nil
}

// ClaimGeocoding implements geocode.Store, across the accounts.
func (s *Store) ClaimGeocoding(ctx context.Context, at time.Time, lease time.Duration, maxAttempts int) (geocode.Claim, bool, error) {
	var c geocode.Claim
	err := s.pool.QueryRow(ctx, "SELECT account_id, lat_e4, lon_e4 FROM runsten_claim_geocoding($1, $2, $3)",
		at, lease, maxAttempts).Scan(&c.AccountID, &c.Cell.LatE4, &c.Cell.LonE4)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return c, false, nil
	case err != nil:
		return c, false, fmt.Errorf("claiming a position to geocode: %w", err)
	}
	return c, true, nil
}

// ResolveAddress implements geocode.Store, within the claim's account.
func (s *Store) ResolveAddress(ctx context.Context, c geocode.Claim, address string, at time.Time) error {
	err := s.inAccount(ctx, c.AccountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE geocoded_positions SET address = $3, resolved_at = $4, claimed_at = NULL
			WHERE account_id = runsten_current_account() AND lat_e4 = $1 AND lon_e4 = $2`,
			c.Cell.LatE4, c.Cell.LonE4, address, at)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return fmt.Errorf("resolving an address: %w", err)
	}
	return nil
}
