package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/notify"
)

// PutDevice implements api.Notifications: the device is registered, or updated with its
// new token, language and version, its creation and its Live Activities' token kept. Counted under the account's lock,
// so that two registrations at once cannot both pass api.MaxDevices.
func (s *Store) PutDevice(ctx context.Context, accountID string, d notify.Device) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('runsten_devices:' || $1))", accountID); err != nil {
			return fmt.Errorf("devices lock: %w", err)
		}
		// An API token revoked meanwhile (DeleteToken, under the same lock) registers no
		// device: none would be removed with it. An extension's access (grant:<id>) is the
		// extension's to check.
		if d.AccessID != "" && !strings.HasPrefix(d.AccessID, "grant:") {
			var live bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM api_tokens WHERE id::text = $1)", d.AccessID).Scan(&live); err != nil {
				return fmt.Errorf("access token: %w", err)
			}
			if !live {
				return api.ErrAccessRevoked
			}
		}
		var others int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM devices WHERE id <> $1", d.ID).Scan(&others); err != nil {
			return fmt.Errorf("count devices: %w", err)
		}
		if others >= api.MaxDevices {
			return api.ErrTooManyDevices
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO devices (id, account_id, user_id, platform, token, access_id, locale, app_version, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (account_id, id) DO UPDATE SET user_id = excluded.user_id, platform = excluded.platform,
				token = excluded.token, access_id = excluded.access_id, locale = excluded.locale,
				app_version = excluded.app_version, updated_at = excluded.updated_at`,
			d.ID, accountID, d.UserID, string(d.Platform), d.Token, nullString(d.AccessID), d.Locale, d.AppVersion, d.CreatedAt, d.UpdatedAt)
		if err != nil {
			return fmt.Errorf("put device: %w", err)
		}
		return nil
	})
}

// DeleteDevicesOfAccess removes the devices the access registered (notify.Device.AccessID),
// their Live Activities with them: for an extension whose access is revoked, as
// DeleteToken does for an API token. Their number.
func (s *Store) DeleteDevicesOfAccess(ctx context.Context, accountID, accessID string) (int, error) {
	var n int
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM devices WHERE access_id = $1", accessID)
		n = int(tag.RowsAffected())
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return n, err
}

// DeleteDevice implements api.Notifications.
func (s *Store) DeleteDevice(ctx context.Context, accountID, deviceID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM devices WHERE id = $1", deviceID)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// Devices implements notify.Store: the account's devices, oldest first.
func (s *Store) Devices(ctx context.Context, accountID string) ([]notify.Device, error) {
	var out []notify.Device
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, user_id, platform, token, coalesce(live_activity_token, ''), coalesce(access_id, ''), locale, app_version,
				created_at, updated_at FROM devices
			ORDER BY created_at, id`)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		var d notify.Device
		var platform string
		_, err = pgx.ForEachRow(rows, []any{&d.ID, &d.UserID, &platform, &d.Token, &d.LiveActivityToken, &d.AccessID, &d.Locale, &d.AppVersion, &d.CreatedAt, &d.UpdatedAt}, func() error {
			d.Platform = notify.Platform(platform)
			d.CreatedAt, d.UpdatedAt = d.CreatedAt.UTC(), d.UpdatedAt.UTC()
			out = append(out, d)
			return nil
		})
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return out, err
}

// NotificationPreferences implements api.Notifications and notify.Store.
func (s *Store) NotificationPreferences(ctx context.Context, accountID string) (notify.Preferences, error) {
	p := notify.Preferences{
		Account: map[notify.Kind]bool{}, VehiclesDefault: map[notify.Kind]bool{}, Vehicles: map[string]map[notify.Kind]bool{},
		VehiclesDefaultThreshold: map[notify.Kind]int{}, VehicleThresholds: map[string]map[notify.Kind]int{},
	}
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT vehicle_id, kind, enabled, threshold FROM notification_preferences")
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		var vehicle *string
		var kind string
		var enabled *bool
		var threshold *int16
		_, err = pgx.ForEachRow(rows, []any{&vehicle, &kind, &enabled, &threshold}, func() error {
			k := notify.Kind(kind)
			if vehicle == nil {
				// Both levels in one map each: a kind belongs to one level.
				if enabled != nil {
					p.Account[k] = *enabled
					p.VehiclesDefault[k] = *enabled
				}
				if threshold != nil {
					p.VehiclesDefaultThreshold[k] = int(*threshold)
				}
				return nil
			}
			if enabled != nil {
				if p.Vehicles[*vehicle] == nil {
					p.Vehicles[*vehicle] = map[notify.Kind]bool{}
				}
				p.Vehicles[*vehicle][k] = *enabled
			}
			if threshold != nil {
				if p.VehicleThresholds[*vehicle] == nil {
					p.VehicleThresholds[*vehicle] = map[notify.Kind]int{}
				}
				p.VehicleThresholds[*vehicle][k] = int(*threshold)
			}
			return nil
		})
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return p, err
}

// SetNotificationPreferences implements api.Notifications, in one transaction. A row
// holds a kind's choice of on or off and its threshold, each apart: setting one leaves the
// other, and the row goes once it holds neither.
func (s *Store) SetNotificationPreferences(ctx context.Context, accountID string, c api.NotificationChoices) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		// set writes one column of the row of the kind (the account's, the vehicles' default
		// or a vehicle's): the value, or NULL, which deletes the row when other, the other
		// column, is NULL too. Both are constants.
		set := func(vehicle *string, k notify.Kind, column, other string, value any, none bool) error {
			if none {
				_, err := tx.Exec(ctx, `
					DELETE FROM notification_preferences
					WHERE vehicle_id IS NOT DISTINCT FROM $1 AND kind = $2 AND `+other+` IS NULL`, vehicle, string(k))
				if err != nil {
					return err //nolint:wrapcheck // wrapped by inAccount
				}
				_, err = tx.Exec(ctx, `
					UPDATE notification_preferences SET `+column+` = NULL
					WHERE vehicle_id IS NOT DISTINCT FROM $1 AND kind = $2`, vehicle, string(k))
				return err //nolint:wrapcheck // wrapped by inAccount
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO notification_preferences (account_id, vehicle_id, kind, `+column+`) VALUES ($1, $2, $3, $4)
				ON CONFLICT (account_id, vehicle_id, kind) DO UPDATE SET `+column+` = excluded.`+column,
				accountID, vehicle, string(k), value)
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		enabled := func(vehicle *string, k notify.Kind, on *bool) error {
			if on == nil {
				return set(vehicle, k, "enabled", "threshold", nil, true)
			}
			return set(vehicle, k, "enabled", "threshold", *on, false)
		}
		threshold := func(vehicle *string, k notify.Kind, v *int) error {
			if v == nil {
				return set(vehicle, k, "threshold", "enabled", nil, true)
			}
			return set(vehicle, k, "threshold", "enabled", int16(*v), false) //nolint:gosec // bounded by the kind's range, at most 100
		}
		for _, level := range []map[notify.Kind]bool{c.Account, c.VehiclesDefault} {
			for k, on := range level {
				if err := enabled(nil, k, &on); err != nil {
					return err
				}
			}
		}
		for k, v := range c.VehiclesDefaultThreshold {
			if err := threshold(nil, k, v); err != nil {
				return err
			}
		}
		for vehicle, kinds := range c.Vehicles {
			for k, on := range kinds {
				if err := enabled(&vehicle, k, on); err != nil {
					return err
				}
			}
		}
		for vehicle, kinds := range c.VehicleThresholds {
			for k, v := range kinds {
				if err := threshold(&vehicle, k, v); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// PurgeDeliveries deletes the deliveries recorded before before, across the accounts
// (runsten_purge_notification_deliveries): their number.
func (s *Store) PurgeDeliveries(ctx context.Context, before time.Time) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT runsten_purge_notification_deliveries($1)", before).Scan(&n); err != nil {
		return 0, fmt.Errorf("purging the notifications' deliveries: %w", err)
	}
	return n, nil
}

// RecordDelivery implements notify.Store, in one transaction under the vehicle's lock:
// two deliveries recorded at once cannot both pass perVehicle.
func (s *Store) RecordDelivery(ctx context.Context, e notify.Event, now time.Time, perVehicle int) (string, bool, error) {
	args, err := json.Marshal(e.Args)
	if err != nil {
		return "", false, fmt.Errorf("delivery's arguments: %w", err)
	}
	if e.Args == nil {
		args = []byte("{}")
	}
	var vehicle *string
	if e.VehicleID != "" {
		vehicle = &e.VehicleID
	}
	var (
		id   string
		send = true
	)
	err = s.inAccount(ctx, e.AccountID, func(tx pgx.Tx) error {
		if vehicle != nil {
			if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('runsten_deliveries:' || $1))", *vehicle); err != nil {
				return fmt.Errorf("deliveries lock: %w", err)
			}
			var sent int
			err := tx.QueryRow(ctx, `
				SELECT count(*) FROM notification_deliveries
				WHERE vehicle_id = $1 AND NOT held_back AND created_at > $2`, *vehicle, now.Add(-time.Hour)).Scan(&sent)
			if err != nil {
				return fmt.Errorf("count deliveries: %w", err)
			}
			send = sent < perVehicle
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO notification_deliveries (account_id, vehicle_id, kind, key, happened_at, args, created_at, held_back)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (account_id, kind, vehicle_id, key) DO NOTHING RETURNING id`,
			e.AccountID, vehicle, string(e.Kind), e.Key, nullTime(e.At), args, now, !send).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			send = false // recorded before
			return nil
		}
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil {
		return "", false, err
	}
	return id, send, nil
}

// NotifyConnections returns the account's connections as the credential's notifications
// read them: a credential's expiry (its own, else the provider's lifetime after it was
// given, as PublishCredentials), its refusal, and a lost grant.
func (s *Store) NotifyConnections(ctx context.Context, accountID string) ([]notify.Connection, error) {
	var out []notify.Connection
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, provider, credential_set_at, credential_expires_at, credential_refused_at, reauth_at FROM connections
			ORDER BY id`)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		var id, provider string
		var setAt, expiresAt, refusedAt, reauthAt *time.Time
		_, err = pgx.ForEachRow(rows, []any{&id, &provider, &setAt, &expiresAt, &refusedAt, &reauthAt}, func() error {
			c := notify.Connection{ID: id, Provider: provider, ExpiresAt: utcOrZero(expiresAt), RefusedAt: utcOrZero(refusedAt), ReauthAt: utcOrZero(reauthAt)}
			if c.ExpiresAt.IsZero() && setAt != nil {
				switch provider {
				case "polestar":
					c.ExpiresAt = setAt.UTC().Add(api.PolestarLifetime)
				case "pleos_hyundai", "pleos_kia", "pleos_genesis":
					c.ExpiresAt = setAt.UTC().Add(api.PleosLifetime)
				}
			}
			out = append(out, c)
			return nil
		})
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return out, err
}
