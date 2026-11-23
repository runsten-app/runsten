package store

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/notify"
)

// device is a device of the user, registered at t0.
func device(user, id string) notify.Device {
	return notify.Device{
		ID: id, UserID: user, Platform: notify.APNs, Token: "apns-" + id, Locale: "fr-FR", AppVersion: "1.0.0",
		CreatedAt: t0, UpdatedAt: t0,
	}
}

func TestDevices(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	ua, err := s.CreateUser(ctx, a, "alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	ub, err := s.CreateUser(ctx, b, "bob", "hash")
	if err != nil {
		t.Fatal(err)
	}
	const phone = "6f1c2a8e-0d5b-4c3a-9e7f-1a2b3c4d5e6f"
	if err := s.PutDevice(ctx, a, device(ua, phone)); err != nil {
		t.Fatal(err)
	}
	// The same installation signed in to another account is a device of each.
	if err := s.PutDevice(ctx, b, device(ub, phone)); err != nil {
		t.Fatal(err)
	}
	// Registered again: a new token and version, its creation kept.
	again := device(ua, phone)
	again.Token, again.AppVersion, again.Platform = "apns-new", "1.1.0", notify.FCM
	again.CreatedAt, again.UpdatedAt = t0.Add(time.Hour), t0.Add(time.Hour)
	if err := s.PutDevice(ctx, a, again); err != nil {
		t.Fatal(err)
	}
	got, err := s.Devices(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	want := again
	want.CreatedAt = t0
	if len(got) != 1 || got[0] != want {
		t.Errorf("devices %+v, want %+v", got, want)
	}

	t.Run("at most MaxDevices", func(t *testing.T) {
		for i := 1; i < api.MaxDevices; i++ {
			if err := s.PutDevice(ctx, a, device(ua, fmt.Sprintf("00000000-0000-4000-8000-%012d", i))); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.PutDevice(ctx, a, device(ua, "00000000-0000-4000-8000-999999999999")); !errors.Is(err, api.ErrTooManyDevices) {
			t.Errorf("one too many: %v", err)
		}
		// One already registered is still updated.
		if err := s.PutDevice(ctx, a, device(ua, phone)); err != nil {
			t.Errorf("updated at the limit: %v", err)
		}
	})

	t.Run("removed", func(t *testing.T) {
		if err := s.DeleteDevice(ctx, a, phone); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteDevice(ctx, a, "00000000-0000-4000-8000-999999999999"); err != nil {
			t.Errorf("an unknown device: %v", err)
		}
		got, _ := s.Devices(ctx, a)
		if len(got) != api.MaxDevices-1 {
			t.Errorf("%d devices left", len(got))
		}
		// Another account's is untouched.
		if got, _ := s.Devices(ctx, b); len(got) != 1 || got[0].UserID != ub {
			t.Errorf("account B: %+v", got)
		}
	})

	t.Run("deleted with the user", func(t *testing.T) {
		err := s.inAccount(ctx, b, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "DELETE FROM users WHERE id = $1", ub)
			return err //nolint:wrapcheck // a test
		})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Devices(ctx, b); len(got) != 0 {
			t.Errorf("left %+v", got)
		}
	})
}

func TestNotificationPreferences(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	on, off := true, false
	err := s.SetNotificationPreferences(ctx, a, api.NotificationChoices{
		Account:         map[notify.Kind]bool{notify.AccessGranted: false},
		VehiclesDefault: map[notify.Kind]bool{notify.TripEnded: true, notify.ChargeFinished: false},
		Vehicles:        map[string]map[notify.Kind]*bool{v: {notify.ChargeFinished: &on, notify.TripEnded: &off}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.NotificationPreferences(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	spec := func(k notify.Kind) notify.Spec {
		sp, _ := notify.NewRegistry().Spec(k)
		return sp
	}
	for _, c := range []struct {
		kind    notify.Kind
		vehicle string
		want    bool
	}{
		{notify.AccessGranted, "", false},
		{notify.CredentialRefused, "", true},
		{notify.ChargeFinished, "", false},
		{notify.ChargeFinished, v, true},
		{notify.TripEnded, "", true},
		{notify.TripEnded, v, false},
	} {
		if got := p.Enabled(spec(c.kind), c.vehicle); got != c.want {
			t.Errorf("%s of %q: %v", c.kind, c.vehicle, got)
		}
	}
	// Changed, then back to the default for the vehicle: the others stay.
	err = s.SetNotificationPreferences(ctx, a, api.NotificationChoices{
		Account:  map[notify.Kind]bool{notify.AccessGranted: true},
		Vehicles: map[string]map[notify.Kind]*bool{v: {notify.TripEnded: nil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = s.NotificationPreferences(ctx, a)
	if !p.Enabled(spec(notify.AccessGranted), "") || !p.Enabled(spec(notify.TripEnded), v) || !p.Enabled(spec(notify.ChargeFinished), v) ||
		!maps.Equal(p.Vehicles[v], map[notify.Kind]bool{notify.ChargeFinished: true}) {
		t.Errorf("after the change: %+v", p)
	}
	// Account B sees none of them, and cannot name A's vehicle.
	if pb, err := s.NotificationPreferences(ctx, b); err != nil || len(pb.Account)+len(pb.VehiclesDefault)+len(pb.Vehicles) != 0 {
		t.Errorf("account B: %+v, %v", pb, err)
	}
	if err := s.SetNotificationPreferences(ctx, b, api.NotificationChoices{Vehicles: map[string]map[notify.Kind]*bool{v: {notify.TripEnded: &on}}}); err == nil {
		t.Error("account B set a preference of A's vehicle")
	}
	// Deleted with the vehicle.
	if err := s.DeleteVehicle(ctx, a, v); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.NotificationPreferences(ctx, a); len(p.Vehicles) != 0 || len(p.Account) == 0 {
		t.Errorf("after the vehicle's deletion: %+v", p)
	}
}

func TestRecordDelivery(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	record := func(account, vehicle string, k notify.Kind, key string, now time.Time) (string, bool) {
		t.Helper()
		id, send, err := s.RecordDelivery(ctx, notify.Event{
			Kind: k, AccountID: account, VehicleID: vehicle, Key: key, At: now, Args: map[string]string{"end_soc": "80"},
		}, now, 2)
		if err != nil {
			t.Fatal(err)
		}
		if send && id == "" {
			t.Error("sent without an ID")
		}
		return id, send
	}
	if _, send := record(a, "", notify.CredentialRefused, "c@1", t0); !send {
		t.Error("first account's event held")
	}
	if _, send := record(a, "", notify.CredentialRefused, "c@1", t0); send {
		t.Error("the same key sent twice")
	}
	// The same key in another account, or of another kind, is another delivery.
	if _, send := record(b, "", notify.CredentialRefused, "c@1", t0); !send {
		t.Error("another account's held")
	}
	if _, send := record(a, "", notify.CredentialExpiring, "c@1", t0); !send {
		t.Error("another kind held")
	}
	// Two a vehicle and an hour: the third is held back, and never sent.
	for i, want := range []bool{true, true, false} {
		if _, send := record(a, v, notify.ChargeFinished, fmt.Sprint(i), t0.Add(time.Duration(i)*time.Minute)); send != want {
			t.Errorf("vehicle's #%d: sent %v", i, send)
		}
	}
	if _, send := record(a, v, notify.ChargeFinished, "2", t0.Add(2*time.Hour)); send {
		t.Error("a delivery held back sent later")
	}
	if _, send := record(a, v, notify.ChargeFinished, "3", t0.Add(2*time.Hour)); !send {
		t.Error("an hour later held")
	}
	if _, send := record(b, vb, notify.ChargeFinished, "0", t0); !send {
		t.Error("another vehicle held")
	}
	var args map[string]string
	var held bool
	err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT args, held_back FROM notification_deliveries WHERE key = '2'").Scan(&args, &held)
	})
	if err != nil || args["end_soc"] != "80" || !held {
		t.Errorf("recorded %v, held %v, %v", args, held, err)
	}
	// Each account sees its own deliveries only.
	for account, want := range map[string]int{a: 6, b: 2} {
		var n int
		err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM notification_deliveries").Scan(&n)
		})
		if err != nil || n != want {
			t.Errorf("account %s: %d deliveries, want %d, %v", account, n, want, err)
		}
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM notification_deliveries").Scan(&n); err != nil || n != 0 {
		t.Errorf("with no account set: %d visible, %v", n, err)
	}

	// The deliveries recorded before an instant are purged, across the accounts: a key
	// purged notifies again.
	if n, err := s.PurgeDeliveries(ctx, t0.Add(90*time.Minute)); err != nil || n != 7 {
		t.Errorf("purged %d, %v; want 7", n, err)
	}
	if _, send := record(a, "", notify.CredentialRefused, "c@1", t0.Add(3*time.Hour)); !send {
		t.Error("a purged key held")
	}
	if _, send := record(a, v, notify.ChargeFinished, "3", t0.Add(3*time.Hour)); send {
		t.Error("a key kept sent twice")
	}
}

func TestNotifyConnections(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	conns, err := s.NotifyConnections(ctx, a)
	if err != nil || len(conns) != 1 || conns[0].Provider != "volvo" || !conns[0].ReauthAt.IsZero() || !conns[0].ExpiresAt.IsZero() {
		t.Fatalf("a Volvo ID: %+v, %v", conns, err)
	}
	lost := t0.Add(-time.Hour)
	err = s.inAccount(ctx, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE connections SET reauth_at = $1, reauth_reason = 'refresh_refused'", lost)
		return err //nolint:wrapcheck // a test
	})
	if err != nil {
		t.Fatal(err)
	}
	if conns, err = s.NotifyConnections(ctx, a); err != nil || len(conns) != 1 || !conns[0].ReauthAt.Equal(lost) {
		t.Errorf("a grant lost: %+v, %v", conns, err)
	}
	if conns, err := s.NotifyConnections(ctx, "00000000-0000-4000-8000-000000000000"); err != nil || len(conns) != 0 {
		t.Errorf("another account: %+v, %v", conns, err)
	}
}

// TestNotificationThresholds: a threshold is kept apart from the choice of on or off, each
// set without touching the other, and a row goes once it holds neither.
func TestNotificationThresholds(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	ptr := func(n int) *int { return &n }
	off := false
	set := func(c api.NotificationChoices) notify.Preferences {
		t.Helper()
		if err := s.SetNotificationPreferences(ctx, a, c); err != nil {
			t.Fatal(err)
		}
		p, err := s.NotificationPreferences(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	low, _ := notify.NewRegistry().Spec(notify.BatteryLow)
	// A threshold alone: no choice, the kind keeps its default.
	p := set(api.NotificationChoices{
		VehiclesDefaultThreshold: map[notify.Kind]*int{notify.BatteryLow: ptr(30)},
		VehicleThresholds:        map[string]map[notify.Kind]*int{v: {notify.BatteryLow: ptr(10)}},
	})
	if !p.Enabled(low, v) || p.Threshold(low, v) != 10 || p.DefaultThreshold(low) != 30 || len(p.VehiclesDefault)+len(p.Vehicles[v]) != 0 {
		t.Errorf("thresholds alone: %+v", p)
	}
	// A choice beside it, then the vehicle's threshold reset: its choice stays.
	p = set(api.NotificationChoices{Vehicles: map[string]map[notify.Kind]*bool{v: {notify.BatteryLow: &off}}})
	if p.Enabled(low, v) || p.Threshold(low, v) != 10 {
		t.Errorf("a choice beside the threshold: %+v", p)
	}
	p = set(api.NotificationChoices{VehicleThresholds: map[string]map[notify.Kind]*int{v: {notify.BatteryLow: nil}}})
	if p.Enabled(low, v) || p.Threshold(low, v) != 30 || len(p.VehicleThresholds[v]) != 0 {
		t.Errorf("threshold reset: %+v", p)
	}
	// The choice back to the default and the default's threshold reset: no row is left.
	p = set(api.NotificationChoices{
		Vehicles:                 map[string]map[notify.Kind]*bool{v: {notify.BatteryLow: nil}},
		VehiclesDefaultThreshold: map[notify.Kind]*int{notify.BatteryLow: nil},
	})
	if !p.Enabled(low, v) || p.Threshold(low, v) != 20 {
		t.Errorf("all reset: %+v", p)
	}
	var rows int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM notification_preferences").Scan(&rows); err != nil || rows != 0 {
		t.Errorf("rows left: %d, %v", rows, err)
	}
	// The database refuses a row with neither.
	if _, err := s.pool.Exec(ctx, "INSERT INTO notification_preferences (account_id, kind) VALUES ($1, 'battery_low')", a); err == nil {
		t.Error("a row with neither a choice nor a threshold")
	}
}
