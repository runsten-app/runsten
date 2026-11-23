package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/notify"
	"runsten/internal/platform/clock"
)

// The alerts on a vehicle's state: a low battery, a vehicle left open, a night not charged.
// Their rules are core's and publish's, the same that MQTT's binary sensors state; this
// runs them for the notifications. battery_low and left_open are evaluated on each reading
// of the vehicle (notifying.Update), and all three on a timer (stateAlerts.run): the
// overnight check, and a state that lasts without a new reading to tell it.

// alertsEvery is how often every vehicle's alerts are evaluated.
const alertsEvery = 5 * time.Minute

// openLookBack is how far back the start of a vehicle's stay open is looked for, once per
// episode.
const openLookBack = 48 * time.Hour

// nightLookBack is how much of the vehicle's readings judges the latest night: its evening
// began at most 24 hours before its morning, the check follows within a few hours.
const nightLookBack = 40 * time.Hour

// stateAlerts tells the account's devices of the states a vehicle's readings show.
type stateAlerts struct {
	// current returns the vehicle's latest known values (derive.Deriver.Current).
	current func(ctx context.Context, accountID, vehicleID string, at time.Time) (core.Current, error)
	// records returns its records since an instant (derive.Deriver.Records).
	records func(ctx context.Context, accountID, vehicleID string, from time.Time) ([]core.Record, error)
	// places lists the account's places (store.Store.Places).
	places func(ctx context.Context, accountID string) ([]core.Place, error)
	// targets lists the vehicles to evaluate on the timer (store.Store.Targets).
	targets  func(ctx context.Context) ([]collector.Target, error)
	notifier *notify.Notifier
	clk      clock.Clock
	log      *slog.Logger

	mu       sync.Mutex
	episodes map[string]*episode // by vehicle
}

// episode is what the evaluator keeps of a vehicle between two evaluations, so that an
// episode's start is looked for in its history once, not at every reading. It is lost on a
// restart, and found again in the history: the same start, hence the same key.
type episode struct {
	// low is when the state of charge fell under lowThreshold, zero when it is not under.
	low          time.Time
	lowThreshold int
	// open is since when the vehicle stands parked and open, zero when it does not.
	open time.Time
	// run serializes the vehicle's evaluations: the one after a reading and the timer's may
	// meet. It is held from the first read to the notification, so that an episode's end
	// never comes between the check that an alert is due and its delivery.
	run sync.Mutex
}

// episode returns the vehicle's, to be used with mu held.
func (s *stateAlerts) episode(vehicleID string) *episode {
	if s.episodes == nil {
		s.episodes = map[string]*episode{}
	}
	ep := s.episodes[vehicleID]
	if ep == nil {
		ep = &episode{}
		s.episodes[vehicleID] = ep
	}
	return ep
}

// observe evaluates the alerts that a reading may start: a low battery and a vehicle
// left open. Their failures are joined; one never holds the other back.
func (s *stateAlerts) observe(ctx context.Context, accountID, vehicleID string) error {
	// One evaluation of a vehicle at a time, history reads and notification included; the
	// global lock only guards the map and the fields, never a read or a send.
	s.mu.Lock()
	run := &s.episode(vehicleID).run
	s.mu.Unlock()
	run.Lock()
	defer run.Unlock()
	now := s.clk.Now()
	cur, err := s.current(ctx, accountID, vehicleID, now)
	if err != nil {
		return fmt.Errorf("current state: %w", err)
	}
	return errors.Join(
		wrap("battery low", s.batteryLow(ctx, accountID, vehicleID, cur, now)),
		wrap("left open", s.leftOpen(ctx, accountID, vehicleID, cur, now)),
	)
}

func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}

// resetLow and resetOpen forget the episode in progress, to be used with mu held.
func (ep *episode) resetLow()  { ep.low = time.Time{} }
func (ep *episode) resetOpen() { ep.open = time.Time{} }

// batteryLow tells of the vehicle parked under its threshold, once per episode: from the
// first reading under it, until one at or above it. The episode is only followed while the
// user wants the kind: re-enabled, it is found again in the history.
func (s *stateAlerts) batteryLow(ctx context.Context, accountID, vehicleID string, cur core.Current, now time.Time) error {
	soc := cur.Snapshot.SoC
	if !soc.OK {
		return nil
	}
	if soc.V >= notify.BatteryLowMax {
		s.mu.Lock()
		s.episode(vehicleID).resetLow() // above every threshold: no episode
		s.mu.Unlock()
		return nil
	}
	wants, err := s.notifier.Wants(ctx, accountID, vehicleID, notify.BatteryLow)
	if err != nil {
		return err //nolint:wrapcheck // the notifier's, which says what failed
	}
	if !wants {
		s.mu.Lock()
		s.episode(vehicleID).resetLow()
		s.mu.Unlock()
		return nil
	}
	threshold, err := s.notifier.Threshold(ctx, accountID, vehicleID, notify.BatteryLow)
	if err != nil {
		return fmt.Errorf("threshold: %w", err)
	}
	s.mu.Lock()
	ep := s.episode(vehicleID)
	if soc.V >= float64(threshold) {
		ep.resetLow()
		s.mu.Unlock()
		return nil
	}
	since, known := ep.low, ep.lowThreshold == threshold
	s.mu.Unlock()
	if since.IsZero() || !known {
		// Under the threshold, the start of the episode not known yet (first evaluation,
		// a restart, another threshold): in the history, once.
		records, err := s.records(ctx, accountID, vehicleID, now.Add(-notify.EpisodeLookBack))
		if err != nil {
			return fmt.Errorf("history: %w", err)
		}
		since = core.LowSince(records, float64(threshold))
		s.mu.Lock()
		ep.low, ep.lowThreshold = since, threshold
		s.mu.Unlock()
	}
	e, ok := notify.BatteryLowEvent(accountID, vehicleID, cur, threshold, since, now)
	if !ok {
		return nil
	}
	return s.tell(ctx, e)
}

// leftOpen tells of the vehicle parked and open for more than publish.LeftOpenAfter, once
// per episode. A vehicle whose maker reports neither locks nor windows is never open.
func (s *stateAlerts) leftOpen(ctx context.Context, accountID, vehicleID string, cur core.Current, now time.Time) error {
	snap := cur.Snapshot
	open := snap.Covers&(core.FieldDoors|core.FieldWindows) != 0 && snap.Engine.OK && snap.Engine.V == core.EngineStopped && cur.OpenReadSince(now.Add(-notify.OpeningsFresh))
	wants := false
	if open {
		var err error
		if wants, err = s.notifier.Wants(ctx, accountID, vehicleID, notify.LeftOpen); err != nil {
			return err //nolint:wrapcheck // the notifier's, which says what failed
		}
	}
	s.mu.Lock()
	ep := s.episode(vehicleID)
	if !open || !wants {
		ep.resetOpen()
	}
	since := ep.open
	s.mu.Unlock()
	if !open || !wants {
		return nil
	}
	if since.IsZero() {
		records, err := s.records(ctx, accountID, vehicleID, now.Add(-openLookBack))
		if err != nil {
			return fmt.Errorf("history: %w", err)
		}
		since = core.OpenSinceFresh(records, notify.OpeningsFresh)
		s.mu.Lock()
		ep.open = since
		s.mu.Unlock()
	}
	e, ok := notify.LeftOpenEvent(accountID, vehicleID, cur, since, now)
	if !ok {
		return nil
	}
	return s.tell(ctx, e)
}

// notCharged tells of the night just ended, when the vehicle stood plugged in at one of the
// account's places and its state of charge did not rise (core.NotCharged), from 07:00
// there for notify.NotChargedWithin: once a night, whenever the timer comes by.
func (s *stateAlerts) notCharged(ctx context.Context, accountID, vehicleID string) error {
	now := s.clk.Now()
	places, err := s.places(ctx, accountID)
	if err != nil {
		return fmt.Errorf("places: %w", err)
	}
	if !notify.MorningCheckDue(places, now) {
		return nil
	}
	wants, err := s.notifier.Wants(ctx, accountID, vehicleID, notify.NotCharged)
	if err != nil || !wants {
		return err //nolint:wrapcheck // the notifier's, which says what failed
	}
	records, err := s.records(ctx, accountID, vehicleID, now.Add(-nightLookBack))
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	e, ok := notify.NotChargedEvent(accountID, vehicleID, core.NotCharged(records, places, now), now)
	if !ok {
		return nil
	}
	return s.tell(ctx, e)
}

func (s *stateAlerts) tell(ctx context.Context, e notify.Event) error {
	if err := s.notifier.Notify(ctx, e); err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	return nil
}

// run evaluates every vehicle's three alerts every alertsEvery until ctx is canceled.
func (s *stateAlerts) run(ctx context.Context) {
	t := time.NewTicker(alertsEvery)
	defer t.Stop()
	for {
		if err := s.check(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("vehicles' alerts", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// check evaluates the alerts of each vehicle once, when the notifier sends.
func (s *stateAlerts) check(ctx context.Context) error {
	if !s.notifier.Sends() {
		return nil
	}
	targets, err := s.targets(ctx)
	if err != nil {
		return fmt.Errorf("vehicles: %w", err)
	}
	var errs []error
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}
		for _, err := range []error{
			s.observe(ctx, t.AccountID, t.VehicleID),
			wrap("not charged", s.notCharged(ctx, t.AccountID, t.VehicleID)),
		} {
			if err != nil {
				errs = append(errs, fmt.Errorf("vehicle %s: %w", t.VehicleID, err))
			}
		}
	}
	return errors.Join(errs...)
}

// alertStates gives MQTT's binary sensors what they rest on, as the notifications read it:
// the threshold of a vehicle's preferences, and the latest night, judged at most every
// alertsEvery for a vehicle.
type alertStates struct {
	notifier *notify.Notifier
	records  func(ctx context.Context, accountID, vehicleID string, from time.Time) ([]core.Record, error)
	places   func(ctx context.Context, accountID string) ([]core.Place, error)

	mu     sync.Mutex
	nights map[string]judged // by vehicle
}

type judged struct {
	at    time.Time
	night core.Value[core.NightCheck]
}

// BatteryLowThreshold implements mqtt.Alerts.
func (a *alertStates) BatteryLowThreshold(ctx context.Context, accountID, vehicleID string) (int, error) {
	n, err := a.notifier.Threshold(ctx, accountID, vehicleID, notify.BatteryLow)
	if err != nil {
		return 0, fmt.Errorf("threshold: %w", err)
	}
	return n, nil
}

// NightCheck implements mqtt.Alerts.
func (a *alertStates) NightCheck(ctx context.Context, accountID, vehicleID string, at time.Time) (core.Value[core.NightCheck], error) {
	a.mu.Lock()
	j, ok := a.nights[vehicleID]
	a.mu.Unlock()
	if ok && at.Sub(j.at) < alertsEvery && !at.Before(j.at) {
		return j.night, nil
	}
	places, err := a.places(ctx, accountID)
	if err != nil {
		return core.Value[core.NightCheck]{}, fmt.Errorf("places: %w", err)
	}
	records, err := a.records(ctx, accountID, vehicleID, at.Add(-nightLookBack))
	if err != nil {
		return core.Value[core.NightCheck]{}, fmt.Errorf("history: %w", err)
	}
	night := core.NotCharged(records, places, at)
	a.mu.Lock()
	if a.nights == nil {
		a.nights = map[string]judged{}
	}
	a.nights[vehicleID] = judged{at: at, night: night}
	a.mu.Unlock()
	return night, nil
}
