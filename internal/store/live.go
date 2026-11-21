package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/notify"
)

// SetLiveActivityToken implements api.Notifications.
func (s *Store) SetLiveActivityToken(ctx context.Context, accountID, deviceID, token string) error {
	var t *string
	if token != "" {
		t = &token
	}
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE devices SET live_activity_token = $2 WHERE id = $1", deviceID, t)
		if err != nil {
			return fmt.Errorf("set live activity token: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return api.ErrNoDevice
		}
		return nil
	})
}

// PutLiveActivity implements api.Notifications: the device's activity of the vehicle
// takes the charge and its token, unless it holds a later charge already (the charge's
// IDs sort as their times); a token of the same charge keeps what was told.
func (s *Store) PutLiveActivity(ctx context.Context, accountID string, a notify.LiveActivity, now time.Time) error {
	var token *string
	if a.Token != "" {
		token = &a.Token
	}
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var known bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1)", a.DeviceID).Scan(&known); err != nil {
			return fmt.Errorf("device: %w", err)
		}
		if !known {
			return api.ErrNoDevice
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO live_activities (account_id, device_id, vehicle_id, charge_id, token, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (account_id, device_id, vehicle_id) DO UPDATE SET charge_id = excluded.charge_id,
				token = excluded.token, updated_at = excluded.updated_at,
				state = CASE WHEN live_activities.charge_id = excluded.charge_id THEN live_activities.state END,
				pushed_at = CASE WHEN live_activities.charge_id = excluded.charge_id THEN live_activities.pushed_at END
			WHERE live_activities.charge_id <= excluded.charge_id`,
			accountID, a.DeviceID, a.VehicleID, a.ChargeID, token, now)
		if err != nil {
			return fmt.Errorf("put live activity: %w", err)
		}
		return nil
	})
}

// LiveActivities implements notify.LiveStore.
func (s *Store) LiveActivities(ctx context.Context, accountID, vehicleID string) ([]notify.LiveActivity, error) {
	var out []notify.LiveActivity
	err := s.readInAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT a.device_id, d.platform, a.charge_id, coalesce(a.token, ''), a.state, a.pushed_at
			FROM live_activities a JOIN devices d ON d.account_id = a.account_id AND d.id = a.device_id
			WHERE a.vehicle_id = $1 ORDER BY a.device_id`, vehicleID)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		var (
			a        = notify.LiveActivity{VehicleID: vehicleID}
			platform string
			state    []byte
			pushedAt *time.Time
		)
		_, err = pgx.ForEachRow(rows, []any{&a.DeviceID, &platform, &a.ChargeID, &a.Token, &state, &pushedAt}, func() error {
			a.Platform, a.State, a.PushedAt = notify.Platform(platform), notify.ChargeState{}, utcOrZero(pushedAt)
			if state != nil {
				if err := json.Unmarshal(state, &a.State); err != nil {
					return fmt.Errorf("live activity's state: %w", err)
				}
			}
			out = append(out, a)
			return nil
		})
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return out, err
}

// StartLiveActivities implements notify.LiveStore.
func (s *Store) StartLiveActivities(ctx context.Context, accountID, vehicleID, chargeID string, deviceIDs []string, st notify.ChargeState, now time.Time) ([]string, error) {
	state, err := json.Marshal(st)
	if err != nil {
		return nil, fmt.Errorf("live activity's state: %w", err)
	}
	var out []string
	err = s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			INSERT INTO live_activities (account_id, device_id, vehicle_id, charge_id, state, pushed_at, updated_at)
			SELECT $1, d.id, $2, $3, $4, $5, $5 FROM devices d WHERE d.id = ANY($6::uuid[])
			ON CONFLICT (account_id, device_id, vehicle_id) DO NOTHING
			RETURNING device_id`, accountID, vehicleID, chargeID, state, now, deviceIDs)
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	return out, err
}

// MarkLiveActivities implements notify.LiveStore.
func (s *Store) MarkLiveActivities(ctx context.Context, accountID, vehicleID, chargeID string, st notify.ChargeState, now time.Time) error {
	state, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("live activity's state: %w", err)
	}
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE live_activities SET state = $3, pushed_at = $4 WHERE vehicle_id = $1 AND charge_id = $2`,
			vehicleID, chargeID, state, now)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// EndLiveActivities implements notify.LiveStore.
func (s *Store) EndLiveActivities(ctx context.Context, accountID, vehicleID string, chargeIDs []string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM live_activities WHERE vehicle_id = $1 AND charge_id = ANY($2)", vehicleID, chargeIDs)
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}
