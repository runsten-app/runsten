package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/notify"
	"runsten/internal/platform/clock"
)

var now = time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)

// notifyStore keeps one device, the preferences given, and the deliveries' keys.
type notifyStore struct {
	mu    sync.Mutex
	prefs notify.Preferences
	seen  map[string]bool
}

func (s *notifyStore) NotificationPreferences(context.Context, string) (notify.Preferences, error) {
	return s.prefs, nil
}

func (s *notifyStore) Devices(context.Context, string) ([]notify.Device, error) {
	return []notify.Device{{ID: "d", Platform: notify.APNs, Token: "t"}}, nil
}

func (s *notifyStore) RecordDelivery(_ context.Context, e notify.Event, _ time.Time, _ int) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(e.Kind) + "|" + e.VehicleID + "|" + e.Key
	if s.seen[key] {
		return "", false, nil
	}
	s.seen[key] = true
	return key, true, nil
}

type sent struct {
	mu     sync.Mutex
	events []string
}

func (s *sent) Send(_ context.Context, d notify.Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, string(d.Event.Kind)+" "+d.Event.VehicleID)
	return nil
}

func (s *sent) sorted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.events)
	slices.Sort(out)
	return out
}

func notifierOf(prefs notify.Preferences, sender notify.PushSender) *notify.Notifier {
	n := notify.New(notify.NewRegistry(), &notifyStore{prefs: prefs, seen: map[string]bool{}}, clock.NewManual(now), slog.New(slog.DiscardHandler))
	if sender != nil {
		n.SetSender(sender)
	}
	return n
}

// TestNotifying: a derivation's charges and trips are told once each; a trip without a
// purpose is told to classify; without a sender, nothing more than the derivation is read.
func TestNotifying(t *testing.T) {
	ended := core.Bounds{After: now.Add(-6 * time.Minute), Before: now.Add(-5 * time.Minute)}
	res := core.Result{
		Charges: []core.Charge{
			{DetectedAt: now.Add(-time.Hour), End: ended, EndSoC: some(80.0), TargetSoC: some(80.0)},
			{DetectedAt: now.Add(-50 * time.Minute), End: ended, EndSoC: some(60.0), TargetSoC: some(80.0)},
		},
		Trips: []core.Trip{
			{DetectedAt: now.Add(-40 * time.Minute), End: ended, DistanceKm: some(12.0)},
			{DetectedAt: now.Add(-30 * time.Minute), End: ended, DistanceKm: some(3.0)},
		},
	}
	reads := 0
	n := notifying{
		update: func(context.Context, string, string) (core.Result, error) { return res, nil },
		current: func(context.Context, string, string, time.Time) (core.Current, error) {
			reads++
			return core.Current{Snapshot: core.Snapshot{Connection: core.Value[core.Connection]{V: core.Connected, OK: true}}}, nil
		},
		purposes: func(_ context.Context, _, _ string, trips []core.Trip) ([]core.Value[core.TripPurpose], error) {
			reads++
			// The first has a purpose; the second none.
			out := make([]core.Value[core.TripPurpose], len(trips))
			out[0] = core.Value[core.TripPurpose]{OK: true}
			return out, nil
		},
		clk: clock.NewManual(now), log: slog.New(slog.DiscardHandler),
	}

	t.Run("without a sender", func(t *testing.T) {
		n.notifier = notifierOf(notify.Preferences{}, nil)
		if err := n.Update(t.Context(), "a", "v"); err != nil || reads != 0 {
			t.Errorf("%v, %d reads", err, reads)
		}
	})

	t.Run("told once", func(t *testing.T) {
		s := &sent{}
		n.notifier = notifierOf(notify.Preferences{VehiclesDefault: map[notify.Kind]bool{notify.TripToClassify: true}}, s)
		for range 2 {
			if err := n.Update(t.Context(), "a", "v"); err != nil {
				t.Fatal(err)
			}
		}
		want := []string{"charge_finished v", "charge_interrupted v", "trip_to_classify v"}
		if got := s.sorted(); !slices.Equal(got, want) {
			t.Errorf("sent %v, want %v", got, want)
		}
	})

	t.Run("trips to classify off: their purposes not read", func(t *testing.T) {
		reads = 0
		n.notifier = notifierOf(notify.Preferences{}, &sent{})
		if err := n.Update(t.Context(), "a", "v"); err != nil || reads != 1 {
			t.Errorf("%v, %d reads", err, reads)
		}
	})

	t.Run("failures", func(t *testing.T) {
		n.notifier = notifierOf(notify.Preferences{}, &sent{})
		down := errors.New("down")
		failing := n
		failing.update = func(context.Context, string, string) (core.Result, error) { return core.Result{}, down }
		if err := failing.Update(t.Context(), "a", "v"); !errors.Is(err, down) {
			t.Errorf("derivation failed: %v", err)
		}
		// A notification that fails is logged: the derivation succeeded.
		failing = n
		failing.current = func(context.Context, string, string, time.Time) (core.Current, error) { return core.Current{}, down }
		if err := failing.Update(t.Context(), "a", "v"); err != nil {
			t.Errorf("current state failed: %v", err)
		}
		if err := failing.tell(t.Context(), "a", "v", res); !errors.Is(err, down) {
			t.Errorf("tell: %v", err)
		}
		failing = n
		failing.notifier = notifierOf(notify.Preferences{VehiclesDefault: map[notify.Kind]bool{notify.TripToClassify: true}}, &sent{})
		failing.purposes = func(context.Context, string, string, []core.Trip) ([]core.Value[core.TripPurpose], error) {
			return nil, down
		}
		if err := failing.tell(t.Context(), "a", "v", res); !errors.Is(err, down) {
			t.Errorf("purposes failed: %v", err)
		}
	})
}

// TestCredentialsNotified: a credential refused is told once, whatever the checks; an
// account whose connections cannot be read does not keep the others from being told.
func TestCredentialsNotified(t *testing.T) {
	s := &sent{}
	var purged []time.Time
	c := credentials{
		purge: func(_ context.Context, before time.Time) (int, error) {
			purged = append(purged, before)
			return 1, nil
		},
		accounts: func(context.Context) ([]string, error) { return []string{"a", "b"}, nil },
		connections: func(_ context.Context, account string) ([]notify.Connection, error) {
			if account == "b" {
				return nil, errors.New("down")
			}
			return []notify.Connection{{ID: "c", Provider: "polestar", RefusedAt: now.Add(-time.Hour)}}, nil
		},
		notifier: notifierOf(notify.Preferences{}, s), clk: clock.NewManual(now), log: slog.New(slog.DiscardHandler),
	}
	for range 2 {
		if err := c.check(t.Context()); err == nil {
			t.Error("account b's failure not returned")
		}
	}
	if got := s.sorted(); !slices.Equal(got, []string{"credential_refused "}) {
		t.Errorf("sent %v", got)
	}
	// Without a sender, the accounts are not even listed.
	c.notifier = notifierOf(notify.Preferences{}, nil)
	c.accounts = func(context.Context) ([]string, error) { return nil, errors.New("listed") }
	if err := c.check(t.Context()); err != nil {
		t.Errorf("without a sender: %v", err)
	}
	// run purges and checks at once, then stops with its context; a purge that fails
	// does not keep the credentials from being checked.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c.run(ctx)
	if !slices.Equal(purged, []time.Time{now.Add(-notify.Retention)}) {
		t.Errorf("purged before %v", purged)
	}
	c.purge = func(context.Context, time.Time) (int, error) { return 0, errors.New("down") }
	c.run(ctx)
}

func TestAccountsOf(t *testing.T) {
	type target struct{ account string }
	got := accountsOf([]target{{"b"}, {"a"}, {"b"}}, func(t target) string { return t.account })
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("accounts %v", got)
	}
}
