package notify

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"runsten/internal/core"
)

// A charge in progress may be shown on a phone's lock screen, as long as it runs: an iOS
// Live Activity, or an Android Live Update, which the phone cannot update while its app
// sleeps. The instance tells it, through the PushSender when it is also a
// LiveActivitySender: it starts the activity on the devices that gave a token to start one
// with (Device.LiveActivityToken), updates each activity whose own token a device
// registered, at most once a reading, and ends them once the charge is over. FCM has no
// token of an activity's own: an activity started on an FCM device takes the device's
// LiveActivityToken (its registration token) at once.

// LiveEvent is what a push does to an activity.
type LiveEvent string

// The pushes of an activity.
const (
	LiveStart  LiveEvent = "start"  // to the devices' tokens to start one
	LiveUpdate LiveEvent = "update" // to the activities' own tokens
	LiveEnd    LiveEvent = "end"    // to the activities' own tokens, with the final figures
)

// LiveStatus is where the charge stands.
type LiveStatus string

// The statuses of a charge an activity shows.
const (
	LiveCharging    LiveStatus = "charging"    // its latest reading charging
	LivePaused      LiveStatus = "paused"      // the cable connected, not charging (scheduled, an error)
	LiveFinished    LiveStatus = "finished"    // over, its limit reached
	LiveInterrupted LiveStatus = "interrupted" // over below its limit
	LiveEnded       LiveStatus = "ended"       // over, its end unseen: the figures last told
)

// ChargeState is what an activity shows of its charge: figures only, never a place, an
// address or a position, which would go through Apple's and Google's servers. Unknown is
// null. Times are Unix seconds, which a device decodes whatever its date format.
type ChargeState struct {
	Status       LiveStatus `json:"status"`
	SoCPct       *float64   `json:"soc_pct"`
	TargetSoCPct *float64   `json:"target_soc_pct"`
	PowerW       *float64   `json:"power_w"`
	// EnergyKWh is the energy added so far: the vehicle's own counter, else the state of
	// charge's estimate, else the power's.
	EnergyKWh *float64 `json:"energy_kwh"`
	// EndsAt is when the limit would be reached at the present power: a rough estimate,
	// null unless charging with the figures it rests on known.
	EndsAt *int64 `json:"ends_at"`
	// ReadAt is the reading the figures are of.
	ReadAt int64 `json:"read_at"`
}

// LiveActivity is an activity a device shows of a vehicle's charge, or was asked to:
// Token is empty until the device registers the activity's own token (on FCM, the
// device's LiveActivityToken from the start), and once it dismissed it. A device has one activity per vehicle at most.
type LiveActivity struct {
	DeviceID string
	// Platform is the device's push service.
	Platform  Platform
	VehicleID string
	// ChargeID is the charge's, as the API gives it (EventID).
	ChargeID string
	Token    string
	// State is the latest told, and PushedAt when; zero before the first.
	State    ChargeState
	PushedAt time.Time
}

// LivePush is an activity's push: a start to Devices, an update or an end to Activities.
type LivePush struct {
	Event     LiveEvent
	AccountID string
	VehicleID string
	ChargeID  string
	State     ChargeState
	// Devices are those to start the activity on, each with its LiveActivityToken.
	Devices []Device
	// Activities are those to update or end, each with its Token.
	Activities []LiveActivity
}

// LiveActivitySender is a PushSender that also sends the activities' pushes: an
// extension's. Its errors are logged, as Send's; an activity is not told again before
// the next reading.
type LiveActivitySender interface {
	SendLiveActivity(ctx context.Context, p LivePush) error
}

// LiveStore keeps the activities, within their account: the Store, when the instance
// shows them.
type LiveStore interface {
	// LiveActivities returns the vehicle's activities, on every device of the account.
	LiveActivities(ctx context.Context, accountID, vehicleID string) ([]LiveActivity, error)
	// StartLiveActivities records an activity of the charge on each of the devices that
	// has none of the vehicle, without a token, but on an FCM device its LiveActivityToken;
	// it returns those it recorded: once recorded, a device is never asked again to start
	// it.
	StartLiveActivities(ctx context.Context, accountID, vehicleID, chargeID string, deviceIDs []string, s ChargeState, now time.Time) ([]string, error)
	// MarkLiveActivities records s as told at now to the vehicle's activities of the
	// charge on those devices, the ones it was pushed to.
	MarkLiveActivities(ctx context.Context, accountID, vehicleID, chargeID string, deviceIDs []string, s ChargeState, now time.Time) error
	// DismissLiveActivities records the vehicle's activities of the charge on those
	// devices as dismissed (no token): none is started again for the charge.
	DismissLiveActivities(ctx context.Context, accountID, vehicleID, chargeID string, deviceIDs []string) error
	// EndLiveActivities forgets the vehicle's activities of those charges.
	EndLiveActivities(ctx context.Context, accountID, vehicleID string, chargeIDs []string) error
}

// LiveEvery is how often an activity is told at most: once a reading, which comes a
// minute apart at best, the margin taking a pass that comes a little early.
const LiveEvery = 50 * time.Second

// Live tells the vehicle's activities what its derivation res found, when the instance
// shows them (a LiveActivitySender and a LiveStore): a charge in progress starts the
// activity on the devices that have none of it and updates the others, a charge over
// ends them, and so does the user turning ChargeLiveActivity off for the vehicle.
// Without them, nothing is read.
func (n *Notifier) Live(ctx context.Context, accountID, vehicleID string, res core.Result) error {
	sender, store := n.liveSender()
	if sender == nil || store == nil {
		return nil
	}
	caller := ctx
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	acts, err := store.LiveActivities(ctx, accountID, vehicleID)
	if err != nil {
		return fmt.Errorf("live activities: %w", err)
	}
	now := n.clk.Now()
	// The charge open at the latest reading stays current however old the reading: its
	// activities end once it is over, never because its readings paused. Only a fresh
	// one starts or updates them.
	current, fresh := "", false
	var state ChargeState
	if c := res.InProgress; c.OK && !c.V.Reconstructed {
		current, state = EventID(c.V.DetectedAt), InProgressState(c.V)
		fresh = !c.V.End.Before.Before(now.Add(-Fresh))
	}
	// Turned off for the vehicle: those shown end, and stay recorded as dismissed while
	// the charge runs, so that turning it on again starts none a person dismissed.
	off := false
	if current != "" {
		on, err := n.liveWanted(ctx, accountID, vehicleID)
		if err != nil {
			return err
		}
		off = !on
	}
	push := func(p LivePush) {
		if err := sender.SendLiveActivity(ctx, p); err != nil && caller.Err() == nil {
			n.log.Warn("live activity not sent", "event", string(p.Event), "account", accountID, "vehicle", vehicleID, "charge", p.ChargeID, "err", err)
		}
	}
	var errs []error
	if ended := endLive(accountID, vehicleID, current, acts, res.Charges, push); len(ended) > 0 {
		if err := store.EndLiveActivities(ctx, accountID, vehicleID, ended); err != nil {
			errs = append(errs, fmt.Errorf("end live activities: %w", err))
		}
	}
	// Those ended are forgotten: their devices may start the charge in progress.
	acts = slices.DeleteFunc(acts, func(a LiveActivity) bool { return a.ChargeID != current })
	if current != "" && off {
		var shown []LiveActivity
		var devices []string
		for _, a := range acts {
			if a.Token != "" {
				shown, devices = append(shown, a), append(devices, a.DeviceID)
			}
		}
		if len(shown) > 0 {
			push(LivePush{Event: LiveEnd, AccountID: accountID, VehicleID: vehicleID, ChargeID: current, State: state, Activities: shown})
			if err := store.DismissLiveActivities(ctx, accountID, vehicleID, current, devices); err != nil {
				errs = append(errs, fmt.Errorf("dismiss live activities: %w", err))
			}
		}
		return errors.Join(errs...)
	}
	if current == "" || !fresh {
		return errors.Join(errs...)
	}
	if err := n.startLive(ctx, store, accountID, vehicleID, current, state, acts, now, push); err != nil {
		errs = append(errs, err)
	}
	var due []LiveActivity
	var devices []string
	for _, a := range acts {
		if a.Token != "" && a.State.ReadAt != state.ReadAt && now.Sub(a.PushedAt) >= LiveEvery {
			due = append(due, a)
			devices = append(devices, a.DeviceID)
		}
	}
	if len(due) > 0 {
		push(LivePush{Event: LiveUpdate, AccountID: accountID, VehicleID: vehicleID, ChargeID: current, State: state, Activities: due})
		if err := store.MarkLiveActivities(ctx, accountID, vehicleID, current, devices, state, now); err != nil {
			errs = append(errs, fmt.Errorf("mark live activities: %w", err))
		}
	}
	return errors.Join(errs...)
}

// endLive ends the activities of the charges other than current, each with its final
// figures when the derivation holds it, else those last told; it returns their charges.
func endLive(accountID, vehicleID, current string, acts []LiveActivity, charges []core.Charge, push func(LivePush)) []string {
	var ended []string
	for _, a := range acts {
		if a.ChargeID != current && !slices.Contains(ended, a.ChargeID) {
			ended = append(ended, a.ChargeID)
		}
	}
	for _, id := range ended {
		var tokens []LiveActivity
		state := ChargeState{}
		for _, a := range acts {
			if a.ChargeID != id {
				continue
			}
			if a.State.ReadAt > state.ReadAt {
				state = a.State
			}
			if a.Token != "" {
				tokens = append(tokens, a)
			}
		}
		state.Status, state.PowerW, state.EndsAt = LiveEnded, nil, nil
		for _, c := range charges {
			if EventID(c.DetectedAt) == id {
				state = EndedState(c)
			}
		}
		if len(tokens) > 0 {
			push(LivePush{Event: LiveEnd, AccountID: accountID, VehicleID: vehicleID, ChargeID: id, State: state, Activities: tokens})
		}
	}
	return ended
}

// startLive starts the activity of the charge on the devices that gave a token to start
// one with and have no activity of the vehicle.
func (n *Notifier) startLive(ctx context.Context, store LiveStore, accountID, vehicleID, charge string, state ChargeState, acts []LiveActivity, now time.Time, push func(LivePush)) error {
	devices, err := n.st.Devices(ctx, accountID)
	if err != nil {
		return fmt.Errorf("devices: %w", err)
	}
	var ids []string
	for _, d := range devices {
		if d.LiveActivityToken != "" && !slices.ContainsFunc(acts, func(a LiveActivity) bool { return a.DeviceID == d.ID }) {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	started, err := store.StartLiveActivities(ctx, accountID, vehicleID, charge, ids, state, now)
	if err != nil {
		return fmt.Errorf("start live activities: %w", err)
	}
	devices = slices.DeleteFunc(devices, func(d Device) bool { return !slices.Contains(started, d.ID) })
	if len(devices) > 0 {
		push(LivePush{Event: LiveStart, AccountID: accountID, VehicleID: vehicleID, ChargeID: charge, State: state, Devices: devices})
	}
	return nil
}

// liveWanted tells whether the user wants the vehicle's charges shown as they go on
// (ChargeLiveActivity), the kind's default when the registry does not have it.
func (n *Notifier) liveWanted(ctx context.Context, accountID, vehicleID string) (bool, error) {
	s, ok := n.reg.Spec(ChargeLiveActivity)
	if !ok {
		return true, nil
	}
	p, err := n.st.NotificationPreferences(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("notification preferences: %w", err)
	}
	return p.Enabled(s, vehicleID), nil
}

func (n *Notifier) liveSender() (LiveActivitySender, LiveStore) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	s, _ := n.sender.(LiveActivitySender)
	l, _ := n.st.(LiveStore)
	return s, l
}

// maxEstimate bounds EndsAt: further, a power read at the start of a charge says nothing.
const maxEstimate = 48 * time.Hour

// InProgressState is what an activity shows of a charge in progress.
func InProgressState(c core.ChargeInProgress) ChargeState {
	s := ChargeState{
		Status: LiveCharging, SoCPct: ptr(c.EndSoC), TargetSoCPct: ptr(c.TargetSoC), PowerW: ptr(c.PowerW),
		EnergyKWh: energy(c.Charge), ReadAt: c.End.Before.Unix(),
	}
	if c.End.Before.After(c.End.After) {
		s.Status, s.PowerW = LivePaused, nil
		return s
	}
	if c.EndSoC.OK && c.TargetSoC.OK && c.PowerW.OK && c.PowerW.V > 0 && c.Capacity.OK && c.TargetSoC.V > c.EndSoC.V {
		hours := (c.TargetSoC.V - c.EndSoC.V) / 100 * c.Capacity.V.KWh / (c.PowerW.V / 1000)
		if d := time.Duration(hours * float64(time.Hour)); d <= maxEstimate {
			at := c.End.Before.Add(d).Unix()
			s.EndsAt = &at
		}
	}
	return s
}

// EndedState is what an activity shows of a charge over: finished when it reached its
// limit, else interrupted.
func EndedState(c core.Charge) ChargeState {
	s := ChargeState{
		Status: LiveInterrupted, SoCPct: ptr(c.EndSoC), TargetSoCPct: ptr(c.TargetSoC),
		EnergyKWh: energy(c), ReadAt: c.End.Before.Unix(),
	}
	if c.EndSoC.OK && c.TargetSoC.OK && c.EndSoC.V >= c.TargetSoC.V-SoCMargin {
		s.Status = LiveFinished
	}
	return s
}

func energy(c core.Charge) *float64 {
	for _, e := range []core.Value[float64]{c.EnergyMeasuredKWh, c.EnergySoCKWh, c.EnergyPowerKWh} {
		if e.OK {
			return ptr(e)
		}
	}
	return nil
}

func ptr(v core.Value[float64]) *float64 {
	if !v.OK {
		return nil
	}
	x := v.V
	return &x
}
