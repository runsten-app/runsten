package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"runsten/internal/core"
	"runsten/internal/notify"
	"runsten/internal/platform/clock"
)

// notifying derives each vehicle the collector read, then tells the account's devices of
// what the derivation found: a charge started, finished or interrupted, a trip ended; the
// Live Activities of its charge in progress; and the alerts of its state (a low battery, a
// vehicle left open).
// Without a PushSender, the derivation alone runs: nothing more is read.
type notifying struct {
	// update derives the vehicle and returns what it found (derive.Deriver.UpdateResult).
	update func(ctx context.Context, accountID, vehicleID string) (core.Result, error)
	// current returns the vehicle's latest known values (derive.Deriver.Current): its cable.
	current func(ctx context.Context, accountID, vehicleID string, at time.Time) (core.Current, error)
	// purposes gives the purpose of each trip, its mark else the places' rules.
	purposes func(ctx context.Context, accountID, vehicleID string, trips []core.Trip) ([]core.Value[core.TripPurpose], error)
	notifier *notify.Notifier
	// alerts evaluates the vehicle's state after each reading; nil: none.
	alerts *stateAlerts
	clk    clock.Clock
	log    *slog.Logger
}

// Update implements collector.Deriver: the derivation, then its notifications, whose
// failure is logged, never the derivation's.
func (n notifying) Update(ctx context.Context, accountID, vehicleID string) error {
	res, err := n.update(ctx, accountID, vehicleID)
	if err != nil || !n.notifier.Sends() {
		return err
	}
	if err := n.tell(ctx, accountID, vehicleID, res); err != nil && ctx.Err() == nil {
		n.log.Warn("vehicle's notifications", "account", accountID, "vehicle", vehicleID, "err", err)
	}
	if err := n.notifier.Live(ctx, accountID, vehicleID, res); err != nil && ctx.Err() == nil {
		n.log.Warn("vehicle's live activities", "account", accountID, "vehicle", vehicleID, "err", err)
	}
	if n.alerts != nil {
		if err := n.alerts.observe(ctx, accountID, vehicleID); err != nil && ctx.Err() == nil {
			n.log.Warn("vehicle's alerts", "account", accountID, "vehicle", vehicleID, "err", err)
		}
	}
	return nil
}

// tell notifies what res found. A read that fails keeps nothing else from being told:
// without the cable, no charge is told interrupted; without the trips' purposes, none is
// told to classify. Its error is returned after.
func (n notifying) tell(ctx context.Context, accountID, vehicleID string, res core.Result) error {
	now := n.clk.Now()
	var errs []error
	var plugged core.Value[core.Connection]
	if len(res.Charges) > 0 {
		cur, err := n.current(ctx, accountID, vehicleID, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("current state: %w", err))
		} else {
			plugged = cur.Snapshot.Connection
		}
	}
	events := notify.VehicleEvents(accountID, vehicleID, res, plugged, now)
	if trips := notify.FreshTrips(res, now); len(trips) > 0 {
		toClassify, err := n.toClassify(ctx, accountID, vehicleID, trips)
		errs = append(errs, err)
		events = append(events, toClassify...)
	}
	if err := n.notifier.NotifyAll(ctx, events); err != nil {
		errs = append(errs, fmt.Errorf("notify: %w", err))
	}
	return errors.Join(errs...)
}

// toClassify are the trips without a purpose, neither marked nor given by a place's rule,
// when the user wants to know of them: a trip marked none by hand has its answer.
func (n notifying) toClassify(ctx context.Context, accountID, vehicleID string, trips []core.Trip) ([]notify.Event, error) {
	wants, err := n.notifier.Wants(ctx, accountID, vehicleID, notify.TripToClassify)
	if err != nil || !wants {
		return nil, err //nolint:wrapcheck // the notifier's, which says what failed
	}
	purposes, err := n.purposes(ctx, accountID, vehicleID, trips)
	if err != nil {
		return nil, fmt.Errorf("trips' purposes: %w", err)
	}
	var out []notify.Event
	for i, t := range trips {
		if !purposes[i].OK {
			out = append(out, notify.TripToClassifyEvent(accountID, vehicleID, t))
		}
	}
	return out, nil
}

// credentialsEvery is how often the connections' credentials are checked: a refusal is
// told within it, an expiry two weeks ahead.
const credentialsEvery = 5 * time.Minute

// credentials tells the accounts' devices of the connections' credentials that need
// their user, refused or about to expire: a vehicle whose credential was refused is no
// longer read, so its passes cannot tell it.
type credentials struct {
	// accounts lists the accounts with a vehicle (store.Store.Targets).
	accounts func(ctx context.Context) ([]string, error)
	// connections reads an account's (store.Store.NotifyConnections).
	connections func(ctx context.Context, accountID string) ([]notify.Connection, error)
	// purge deletes the deliveries recorded before an instant (store.Store.PurgeDeliveries).
	purge    func(ctx context.Context, before time.Time) (int, error)
	notifier *notify.Notifier
	clk      clock.Clock
	log      *slog.Logger
}

// run checks the credentials every credentialsEvery until ctx is canceled, and purges
// the deliveries past notify.Retention, sent or not: a sender may have been there before.
func (c credentials) run(ctx context.Context) {
	t := time.NewTicker(credentialsEvery)
	defer t.Stop()
	for {
		if n, err := c.purge(ctx, c.clk.Now().Add(-notify.Retention)); err != nil && ctx.Err() == nil {
			c.log.Warn("notifications' deliveries not purged", "err", err)
		} else if n > 0 {
			c.log.Info("notifications' deliveries purged", "deliveries", n)
		}
		if err := c.check(ctx); err != nil && ctx.Err() == nil {
			c.log.Warn("credentials' notifications", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// check tells of each account's credentials once, when the notifier sends.
func (c credentials) check(ctx context.Context) error {
	if !c.notifier.Sends() {
		return nil
	}
	accounts, err := c.accounts(ctx)
	if err != nil {
		return fmt.Errorf("accounts: %w", err)
	}
	var errs []error
	for _, account := range accounts {
		conns, err := c.connections(ctx, account)
		if err != nil {
			errs = append(errs, fmt.Errorf("connections of %s: %w", account, err))
			continue
		}
		errs = append(errs, c.notifier.NotifyAll(ctx, notify.CredentialEvents(account, conns, c.clk.Now())))
	}
	return errors.Join(errs...)
}

// accountsOf lists, once each and sorted, the accounts of targets.
func accountsOf[T any](targets []T, account func(T) string) []string {
	var out []string
	for _, t := range targets {
		out = append(out, account(t))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// purposeStore reads what a trip's purpose rests on (store.Store).
type purposeStore interface {
	TripMarks(ctx context.Context, accountID, vehicleID string, trips []core.Trip) ([]core.Value[core.TripMark], error)
	Places(ctx context.Context, accountID string) ([]core.Place, error)
}

// tripPurposes gives the purpose of each trip as the API does: its mark, else the rules
// of the account's places.
func tripPurposes(st purposeStore) func(ctx context.Context, accountID, vehicleID string, trips []core.Trip) ([]core.Value[core.TripPurpose], error) {
	return func(ctx context.Context, accountID, vehicleID string, trips []core.Trip) ([]core.Value[core.TripPurpose], error) {
		marks, err := st.TripMarks(ctx, accountID, vehicleID, trips)
		if err != nil {
			return nil, fmt.Errorf("trip marks: %w", err)
		}
		places, err := st.Places(ctx, accountID)
		if err != nil {
			return nil, fmt.Errorf("places: %w", err)
		}
		out := make([]core.Value[core.TripPurpose], len(trips))
		for i, t := range trips {
			out[i] = core.PurposeOf(t, marks[i], places)
		}
		return out, nil
	}
}
