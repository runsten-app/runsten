package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/api"
	"runsten/internal/auth"
	"runsten/internal/notify"
)

// TestLiveActivities: the token to start them with, kept when the device registers
// again; an activity started once per device, its token registered, a later charge
// taking its place and an earlier one not; what was told marked; forgotten once ended,
// and with the device. Another account sees none of them.
func TestLiveActivities(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, car := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	ua, err := s.CreateUser(ctx, a, "alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	const phone, tablet = "6f1c2a8e-0d5b-4c3a-9e7f-1a2b3c4d5e6f", "6f1c2a8e-0d5b-4c3a-9e7f-1a2b3c4d5e70"
	const charge, later = "2026-09-28T18:30:00.000000Z", "2026-09-29T07:00:00.000000Z"
	if err := s.SetLiveActivityToken(ctx, a, phone, "start"); !errors.Is(err, api.ErrNoDevice) {
		t.Errorf("start token before the device: %v", err)
	}
	if err := s.PutLiveActivity(ctx, a, notify.LiveActivity{DeviceID: phone, VehicleID: car, ChargeID: charge, Token: "t"}, t0); !errors.Is(err, api.ErrNoDevice) {
		t.Errorf("activity before the device: %v", err)
	}
	for _, id := range []string{phone, tablet} {
		if err := s.PutDevice(ctx, a, device(ua, id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetLiveActivityToken(ctx, a, phone, "start"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutDevice(ctx, a, device(ua, phone)); err != nil {
		t.Fatal(err)
	}
	devices, err := s.Devices(ctx, a)
	if err != nil || len(devices) != 2 || devices[0].LiveActivityToken != "start" || devices[1].LiveActivityToken != "" {
		t.Fatalf("devices %+v %v", devices, err)
	}
	if err := s.SetLiveActivityToken(ctx, b, phone, "x"); !errors.Is(err, api.ErrNoDevice) {
		t.Errorf("another account's device: %v", err)
	}

	soc := 50.0
	state := notify.ChargeState{Status: notify.LiveCharging, SoCPct: &soc, ReadAt: t0.Unix()}
	started, err := s.StartLiveActivities(ctx, a, car, charge, []string{phone}, state, t0)
	if err != nil || len(started) != 1 || started[0] != phone {
		t.Fatalf("started %v %v", started, err)
	}
	if started, err := s.StartLiveActivities(ctx, a, car, charge, []string{phone}, state, t0); err != nil || len(started) != 0 {
		t.Errorf("started twice: %v %v", started, err)
	}
	// The app registers its token: what was told is kept, the charge being the same.
	if err := s.PutLiveActivity(ctx, a, notify.LiveActivity{DeviceID: phone, VehicleID: car, ChargeID: charge, Token: "act1"}, t0); err != nil {
		t.Fatal(err)
	}
	// The tablet started one of its own.
	if err := s.PutLiveActivity(ctx, a, notify.LiveActivity{DeviceID: tablet, VehicleID: car, ChargeID: charge, Token: "act2"}, t0); err != nil {
		t.Fatal(err)
	}
	acts, err := s.LiveActivities(ctx, a, car)
	if err != nil || len(acts) != 2 || acts[0].DeviceID != phone || acts[0].Platform != notify.APNs || acts[0].Token != "act1" || *acts[0].State.SoCPct != 50 ||
		!acts[0].PushedAt.Equal(t0) || acts[1].Token != "act2" || !acts[1].PushedAt.IsZero() {
		t.Fatalf("activities %+v %v", acts, err)
	}
	soc2 := 60.0
	state.SoCPct, state.ReadAt = &soc2, t0.Add(time.Minute).Unix()
	// Only the tablet was pushed to: the phone keeps what it was told.
	if err := s.MarkLiveActivities(ctx, a, car, charge, []string{tablet}, state, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	acts, _ = s.LiveActivities(ctx, a, car)
	if *acts[0].State.SoCPct != 50 || !acts[0].PushedAt.Equal(t0) || *acts[1].State.SoCPct != 60 || !acts[1].PushedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("marked %+v", acts)
	}
	if acts, err := s.LiveActivities(ctx, b, car); err != nil || len(acts) != 0 {
		t.Errorf("another account sees %+v %v", acts, err)
	}

	// A later charge takes the tablet's place, forgetting what was told; an earlier one
	// changes nothing; null is a dismissal.
	if err := s.PutLiveActivity(ctx, a, notify.LiveActivity{DeviceID: tablet, VehicleID: car, ChargeID: later, Token: "act3"}, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.PutLiveActivity(ctx, a, notify.LiveActivity{DeviceID: tablet, VehicleID: car, ChargeID: charge, Token: "act4"}, t0); err != nil {
		t.Fatal(err)
	}
	acts, _ = s.LiveActivities(ctx, a, car)
	if acts[1].ChargeID != later || acts[1].Token != "act3" || acts[1].State.SoCPct != nil || !acts[1].PushedAt.IsZero() {
		t.Errorf("later charge %+v", acts[1])
	}
	if err := s.PutLiveActivity(ctx, a, notify.LiveActivity{DeviceID: tablet, VehicleID: car, ChargeID: later}, t0); err != nil {
		t.Fatal(err)
	}
	if acts, _ = s.LiveActivities(ctx, a, car); acts[1].Token != "" || acts[1].ChargeID != later {
		t.Errorf("dismissed %+v", acts[1])
	}

	if err := s.EndLiveActivities(ctx, a, car, []string{charge}); err != nil {
		t.Fatal(err)
	}
	if acts, _ = s.LiveActivities(ctx, a, car); len(acts) != 1 || acts[0].DeviceID != tablet {
		t.Errorf("after the end %+v", acts)
	}
	if err := s.DeleteDevice(ctx, a, tablet); err != nil {
		t.Fatal(err)
	}
	if acts, _ = s.LiveActivities(ctx, a, car); len(acts) != 0 {
		t.Errorf("kept with the device gone: %+v", acts)
	}
	// The token to start them with: null removes it.
	if err := s.SetLiveActivityToken(ctx, a, phone, ""); err != nil {
		t.Fatal(err)
	}
	if devices, _ := s.Devices(ctx, a); devices[0].LiveActivityToken != "" {
		t.Errorf("start token kept %+v", devices[0])
	}
}

// TestDevicesOfAccess: a device remembers the access that registered it; revoking an API
// token removes its devices and their activities, and an extension removes those of its
// own access; a session's device stays.
func TestDevicesOfAccess(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, car := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	ua, err := s.CreateUser(ctx, a, "alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	tokenID, err := s.CreateToken(ctx, auth.Token{AccountID: a, UserID: ua, Name: "phone", CreatedAt: t0}, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	const byToken, byGrant, bySession = //nolint:gosec // devices' IDs
	"6f1c2a8e-0d5b-4c3a-9e7f-1a2b3c4d5e01", "6f1c2a8e-0d5b-4c3a-9e7f-1a2b3c4d5e02", "6f1c2a8e-0d5b-4c3a-9e7f-1a2b3c4d5e03"
	for id, access := range map[string]string{byToken: tokenID, byGrant: "grant:g1", bySession: ""} {
		d := device(ua, id)
		d.AccessID = access
		if err := s.PutDevice(ctx, a, d); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.StartLiveActivities(ctx, a, car, "c1", []string{byToken}, notify.ChargeState{Status: notify.LiveCharging}, t0); err != nil {
		t.Fatal(err)
	}
	devices, _ := s.Devices(ctx, a)
	if len(devices) != 3 || devices[0].AccessID == "" && devices[1].AccessID == "" {
		t.Fatalf("devices %+v", devices)
	}
	if ok, err := s.DeleteToken(ctx, a, ua, tokenID); err != nil || !ok {
		t.Fatalf("delete token: %v %v", ok, err)
	}
	if n, err := s.DeleteDevicesOfAccess(ctx, a, "grant:g1"); err != nil || n != 1 {
		t.Fatalf("delete grant's: %d %v", n, err)
	}
	devices, _ = s.Devices(ctx, a)
	if len(devices) != 1 || devices[0].ID != bySession {
		t.Errorf("left %+v", devices)
	}
	if acts, _ := s.LiveActivities(ctx, a, car); len(acts) != 0 {
		t.Errorf("activities left %+v", acts)
	}
}
