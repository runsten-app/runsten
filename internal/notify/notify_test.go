package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/publish"
)

var t0 = time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)

// memStore keeps the preferences, the devices and the deliveries in memory, as the store
// does: one delivery per account, kind, vehicle and key.
type memStore struct {
	prefs   Preferences
	devices []Device
	// delivered are the deliveries recorded, held back ones included.
	delivered []recorded
	fail      error
}

type recorded struct {
	e    Event
	at   time.Time
	sent bool
}

func (m *memStore) NotificationPreferences(context.Context, string) (Preferences, error) {
	return m.prefs, m.fail
}

func (m *memStore) Devices(context.Context, string) ([]Device, error) { return m.devices, m.fail }

func (m *memStore) RecordDelivery(_ context.Context, e Event, now time.Time, perVehicle int) (string, bool, error) {
	if m.fail != nil {
		return "", false, m.fail
	}
	sent := 0
	for _, r := range m.delivered {
		if r.e.AccountID == e.AccountID && r.e.Kind == e.Kind && r.e.VehicleID == e.VehicleID && r.e.Key == e.Key {
			return "", false, nil
		}
		if e.VehicleID != "" && r.e.VehicleID == e.VehicleID && r.sent && r.at.After(now.Add(-time.Hour)) {
			sent++
		}
	}
	send := e.VehicleID == "" || sent < perVehicle
	m.delivered = append(m.delivered, recorded{e: e, at: now, sent: send})
	return fmt.Sprint("d", len(m.delivered)), send, nil
}

type fakeSender struct {
	sent []Delivery
	fail error
}

func (f *fakeSender) Send(_ context.Context, d Delivery) error {
	f.sent = append(f.sent, d)
	return f.fail
}

var quiet = slog.New(slog.DiscardHandler)

func newNotifier(st *memStore) (*Notifier, *fakeSender, *clock.Manual) {
	clk := clock.NewManual(t0)
	n := New(NewRegistry(), st, clk, quiet)
	s := &fakeSender{}
	n.SetSender(s)
	return n, s, clk
}

var phone = Device{ID: "d1", Platform: APNs, Token: "tok", Locale: "fr-FR"}

// TestOneEventOneDelivery: an event is sent once to every device, however often it is
// emitted again.
func TestOneEventOneDelivery(t *testing.T) {
	st := &memStore{devices: []Device{phone, {ID: "d2", Platform: FCM, Token: "tok2"}}}
	n, s, _ := newNotifier(st)
	e := Event{Kind: CredentialRefused, AccountID: "a", Key: "c@1", At: t0}
	for range 3 {
		if err := n.Notify(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.sent) != 1 || len(s.sent[0].Devices) != 2 || s.sent[0].Event.Key != "c@1" || s.sent[0].ID == "" {
		t.Errorf("sent %+v", s.sent)
	}
	// Another key is another event.
	e.Key = "c@2"
	if err := n.Notify(t.Context(), e); err != nil || len(s.sent) != 2 {
		t.Errorf("another key: %v, %d sent", err, len(s.sent))
	}
}

// TestKindOff: a kind turned off sends nothing and records nothing; a vehicle's own
// choice wins over the vehicles' default, which wins over the kind's.
func TestKindOff(t *testing.T) {
	charge := func(vehicle string) Event {
		return Event{Kind: ChargeFinished, AccountID: "a", VehicleID: vehicle, Key: "k", At: t0}
	}
	for _, tc := range []struct {
		name  string
		prefs Preferences
		e     Event
		sent  bool
	}{
		{"default on", Preferences{}, charge("v1"), true},
		{"default off", Preferences{}, Event{Kind: TripEnded, AccountID: "a", VehicleID: "v1", Key: "k"}, false},
		{"account off", Preferences{Account: map[Kind]bool{CredentialRefused: false}}, Event{Kind: CredentialRefused, AccountID: "a", Key: "k"}, false},
		{"vehicles off", Preferences{VehiclesDefault: map[Kind]bool{ChargeFinished: false}}, charge("v1"), false},
		{
			"vehicle on", Preferences{
				VehiclesDefault: map[Kind]bool{ChargeFinished: false},
				Vehicles:        map[string]map[Kind]bool{"v1": {ChargeFinished: true}},
			}, charge("v1"), true,
		},
		{"another vehicle off", Preferences{Vehicles: map[string]map[Kind]bool{"v2": {ChargeFinished: false}}}, charge("v1"), true},
		{"vehicle off", Preferences{Vehicles: map[string]map[Kind]bool{"v1": {ChargeFinished: false}}}, charge("v1"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &memStore{prefs: tc.prefs, devices: []Device{phone}}
			n, s, _ := newNotifier(st)
			if err := n.Notify(t.Context(), tc.e); err != nil {
				t.Fatal(err)
			}
			if got := len(s.sent) == 1; got != tc.sent {
				t.Errorf("sent %v, want %v", got, tc.sent)
			}
			if !tc.sent && len(st.delivered) != 0 {
				t.Errorf("recorded %+v", st.delivered)
			}
			wants, err := n.Wants(t.Context(), tc.e.AccountID, tc.e.VehicleID, tc.e.Kind)
			if err != nil || wants != tc.sent {
				t.Errorf("wants %v, %v", wants, err)
			}
		})
	}
}

// TestAlways: a kind the user cannot turn off is sent whatever the preferences say.
func TestAlways(t *testing.T) {
	st := &memStore{prefs: Preferences{Account: map[Kind]bool{"service_message": false}}, devices: []Device{phone}}
	n, s, _ := newNotifier(st)
	if err := n.Registry().Register(Spec{Kind: "service_message", Level: AccountLevel, Always: true}); err != nil {
		t.Fatal(err)
	}
	if err := n.Notify(t.Context(), Event{Kind: "service_message", AccountID: "a", Key: "m1"}); err != nil || len(s.sent) != 1 {
		t.Errorf("%v, %d sent", err, len(s.sent))
	}
	if sp, _ := n.Registry().Spec("service_message"); !sp.Default {
		t.Error("an Always kind is on by default")
	}
}

// TestNothingToSend: without a sender or a device, nothing is sent nor recorded, and the
// Notifier tells it serves no notifications.
func TestNothingToSend(t *testing.T) {
	st := &memStore{devices: []Device{phone}}
	n := New(NewRegistry(), st, clock.NewManual(t0), quiet)
	e := Event{Kind: CredentialRefused, AccountID: "a", Key: "k"}
	if err := n.Notify(t.Context(), e); err != nil || n.Sends() || len(st.delivered) != 0 {
		t.Errorf("without a sender: %v, sends %v, %+v", err, n.Sends(), st.delivered)
	}
	if wants, err := n.Wants(t.Context(), "a", "", CredentialRefused); err != nil || wants {
		t.Errorf("wants without a sender: %v, %v", wants, err)
	}
	var none *Notifier
	if none.Sends() {
		t.Error("a nil Notifier sends")
	}

	st.devices = nil
	n, s, _ := newNotifier(st)
	if err := n.Notify(t.Context(), e); err != nil || len(s.sent) != 0 || len(st.delivered) != 0 {
		t.Errorf("without a device: %v, %+v", err, st.delivered)
	}
}

// TestPerVehicleHour: a vehicle sends PerVehicleHour notifications at most in an hour;
// those beyond are recorded held back, never sent later. The account's are not counted.
func TestPerVehicleHour(t *testing.T) {
	st := &memStore{devices: []Device{phone}}
	n, s, clk := newNotifier(st)
	for i := range PerVehicleHour + 2 {
		e := Event{Kind: ChargeFinished, AccountID: "a", VehicleID: "v1", Key: string(rune('a' + i))}
		if err := n.Notify(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.sent) != PerVehicleHour {
		t.Errorf("%d sent within the hour", len(s.sent))
	}
	if err := n.Notify(t.Context(), Event{Kind: ChargeFinished, AccountID: "a", VehicleID: "v2", Key: "a"}); err != nil || len(s.sent) != PerVehicleHour+1 {
		t.Errorf("another vehicle: %v, %d sent", err, len(s.sent))
	}
	clk.Advance(time.Hour + time.Second)
	if err := n.Notify(t.Context(), Event{Kind: ChargeFinished, AccountID: "a", VehicleID: "v1", Key: "z"}); err != nil || len(s.sent) != PerVehicleHour+2 {
		t.Errorf("an hour later: %v, %d sent", err, len(s.sent))
	}
	// Held back, then emitted again: never sent.
	if err := n.Notify(t.Context(), Event{Kind: ChargeFinished, AccountID: "a", VehicleID: "v1", Key: "e"}); err != nil || len(s.sent) != PerVehicleHour+2 {
		t.Errorf("held back: %v, %d sent", err, len(s.sent))
	}
}

// TestInvalidEvents: an unknown kind, a vehicle's kind without a vehicle or an account's
// with one, an event without a key are errors, whether a sender is set or not.
func TestInvalidEvents(t *testing.T) {
	st := &memStore{devices: []Device{phone}}
	n, s, _ := newNotifier(st)
	for _, e := range []Event{
		{Kind: "unknown", AccountID: "a", Key: "k"},
		{Kind: ChargeFinished, AccountID: "a", Key: "k"},
		{Kind: CredentialRefused, AccountID: "a", VehicleID: "v", Key: "k"},
		{Kind: CredentialRefused, AccountID: "a"},
		{Kind: CredentialRefused, Key: "k"},
	} {
		if err := n.Notify(t.Context(), e); err == nil {
			t.Errorf("%+v accepted", e)
		}
	}
	if err := n.NotifyAll(t.Context(), []Event{{Kind: "unknown", AccountID: "a", Key: "k"}}); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("NotifyAll: %v", err)
	}
	if _, err := n.Wants(t.Context(), "a", "", "unknown"); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("Wants: %v", err)
	}
	if len(s.sent) != 0 {
		t.Errorf("sent %+v", s.sent)
	}
}

// TestFailures: a store that fails is an error; a sender that fails is logged, and the
// delivery is not sent again.
func TestFailures(t *testing.T) {
	st := &memStore{devices: []Device{phone}, fail: errors.New("down")}
	n, s, _ := newNotifier(st)
	e := Event{Kind: CredentialRefused, AccountID: "a", Key: "k"}
	if err := n.Notify(t.Context(), e); err == nil {
		t.Error("a store down is not an error")
	}
	if _, err := n.Wants(t.Context(), "a", "", CredentialRefused); err == nil {
		t.Error("Wants: a store down is not an error")
	}
	st.fail = nil
	s.fail = errors.New("APNs down")
	for range 2 {
		if err := n.Notify(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.sent) != 1 {
		t.Errorf("sent %d times", len(s.sent))
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	for _, s := range []Spec{
		{Kind: CredentialRefused, Level: AccountLevel},
		{Kind: "Payment-Failed", Level: AccountLevel},
		{Kind: "payment_failed", Level: "planet"},
	} {
		if err := r.Register(s); err == nil {
			t.Errorf("%+v registered", s)
		}
	}
	if err := r.Register(Spec{Kind: "payment_failed", Level: AccountLevel, Default: true}); err != nil {
		t.Fatal(err)
	}
	var account, vehicle []Kind
	for _, s := range r.Kinds(AccountLevel) {
		account = append(account, s.Kind)
	}
	for _, s := range r.Kinds(VehicleLevel) {
		vehicle = append(vehicle, s.Kind)
	}
	if want := []Kind{CredentialExpiring, CredentialRefused, AccessGranted, ImportFinished, "payment_failed"}; !slices.Equal(account, want) {
		t.Errorf("account's kinds %v", account)
	}
	if want := []Kind{ChargeInterrupted, ChargeFinished, TripToClassify, ChargeStarted, TripEnded}; !slices.Equal(vehicle, want) {
		t.Errorf("vehicle's kinds %v", vehicle)
	}
	if _, ok := r.Spec("nothing"); ok {
		t.Error("an unknown kind found")
	}
}

func TestCredentialEvents(t *testing.T) {
	expires := t0.Add(10 * 24 * time.Hour)
	conns := []Connection{
		{ID: "valid", Provider: "polestar", ExpiresAt: t0.Add(publish.RenewAhead + time.Hour)},
		{ID: "expiring", Provider: "polestar", ExpiresAt: expires},
		{ID: "expired", Provider: "pleos_kia", ExpiresAt: t0.Add(-time.Hour)},
		{ID: "refused", Provider: "polestar", ExpiresAt: expires, RefusedAt: t0.Add(-2 * time.Hour)},
		{ID: "lost", Provider: "volvo", ReauthAt: t0.Add(-3 * time.Hour)},
		{ID: "volvo", Provider: "volvo"},
	}
	got := map[string]Event{}
	for _, e := range CredentialEvents("a", conns, t0) {
		got[e.Key] = e
	}
	want := map[string]Kind{
		"expiring@2026-11-30T18:00:00Z": CredentialExpiring,
		"expired@2026-11-20T17:00:00Z":  CredentialExpiring,
		"refused@2026-11-20T16:00:00Z":  CredentialRefused,
		"lost@2026-11-20T15:00:00Z":     CredentialRefused,
	}
	if len(got) != len(want) {
		t.Errorf("events %v", slices.Collect(maps.Keys(got)))
	}
	for key, k := range want {
		if e, ok := got[key]; !ok || e.Kind != k || e.AccountID != "a" || e.VehicleID != "" {
			t.Errorf("%s: %+v", key, e)
		}
	}
	if e := got["expiring@2026-11-30T18:00:00Z"]; e.Args["expires_at"] != "2026-11-30T18:00:00Z" || e.Args["provider"] != "polestar" {
		t.Errorf("expiring's args %v", e.Args)
	}
}

func TestVehicleEvents(t *testing.T) {
	some := func(v float64) core.Value[float64] { return core.Value[float64]{V: v, OK: true} }
	at := t0.Add
	charge := func(detected, ended time.Duration, end, target core.Value[float64]) core.Charge {
		return core.Charge{
			DetectedAt: at(detected), Start: core.Bounds{After: at(detected - time.Minute), Before: at(detected)},
			End: core.Bounds{After: at(ended - time.Minute), Before: at(ended)}, StartSoC: some(40), EndSoC: end, TargetSoC: target,
		}
	}
	trip := func(detected, ended time.Duration) core.Trip {
		return core.Trip{
			DetectedAt: at(detected), End: core.Bounds{After: at(ended - time.Minute), Before: at(ended)},
			DistanceKm: some(12.34), EnergyKWh: core.Value[float64]{},
		}
	}
	reconstructed := charge(-20*time.Minute, -10*time.Minute, some(80), some(80))
	reconstructed.Reconstructed = true
	imported := trip(-30*time.Minute, -5*time.Minute)
	imported.Import = core.Value[core.Import]{OK: true}
	res := core.Result{
		Charges: []core.Charge{
			charge(-50*time.Minute, -10*time.Minute, some(79), some(80)),             // finished, one point under
			charge(-40*time.Minute, -9*time.Minute, some(62), some(80)),              // interrupted while plugged
			charge(-3*time.Hour, -2*time.Hour, some(80), some(80)),                   // stale
			charge(-30*time.Minute, -8*time.Minute, some(70), core.Value[float64]{}), // no limit known
			reconstructed,
		},
		Trips:      []core.Trip{trip(-30*time.Minute, -5*time.Minute), trip(-5*time.Hour, -4*time.Hour), imported},
		InProgress: core.Value[core.ChargeInProgress]{V: core.ChargeInProgress{Charge: charge(-2*time.Minute, 0, some(50), some(80))}, OK: true},
	}
	plugged := core.Value[core.Connection]{V: core.Connected, OK: true}
	describe := func(es []Event) []string {
		var out []string
		for _, e := range es {
			if e.AccountID != "a" || e.VehicleID != "v" {
				t.Errorf("%+v", e)
			}
			out = append(out, string(e.Kind)+" "+e.Key)
		}
		slices.Sort(out)
		return out
	}
	got := describe(VehicleEvents("a", "v", res, plugged, t0))
	want := []string{
		"charge_finished 2026-11-20T17:10:00.000000Z",
		"charge_interrupted 2026-11-20T17:20:00.000000Z",
		"charge_started 2026-11-20T17:58:00.000000Z",
		"trip_ended 2026-11-20T17:30:00.000000Z",
	}
	if !slices.Equal(got, want) {
		t.Errorf("events\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Unplugged, or the cable not known: no charge told interrupted.
	for _, p := range []core.Value[core.Connection]{{V: core.Disconnected, OK: true}, {}} {
		for _, s := range describe(VehicleEvents("a", "v", res, p, t0)) {
			if strings.HasPrefix(s, "charge_interrupted") {
				t.Errorf("cable %+v: %s", p, s)
			}
		}
	}
	for _, e := range VehicleEvents("a", "v", res, plugged, t0) {
		switch e.Kind {
		case TripEnded:
			if !maps.Equal(e.Args, map[string]string{"trip": "2026-11-20T17:30:00.000000Z", "distance_km": "12.3"}) {
				t.Errorf("trip's args %v", e.Args)
			}
		case ChargeInterrupted:
			if !maps.Equal(e.Args, map[string]string{"charge": e.Key, "start_soc": "40", "end_soc": "62", "target_soc": "80"}) {
				t.Errorf("charge's args %v", e.Args)
			}
		}
	}
	trips := FreshTrips(res, t0)
	if len(trips) != 1 {
		t.Fatalf("fresh trips %+v", trips)
	}
	if e := TripToClassifyEvent("a", "v", trips[0]); e.Kind != TripToClassify || e.Key != "2026-11-20T17:30:00.000000Z" || e.VehicleID != "v" {
		t.Errorf("to classify %+v", e)
	}
}
