package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"runsten/internal/platform/clock"
)

// Event is something that happened, which may be told to the account's devices.
type Event struct {
	Kind      Kind
	AccountID string
	// VehicleID is the vehicle a vehicle's kind is about; empty for an account's.
	VehicleID string
	// At is when it happened, as well as it is known.
	At time.Time
	// Key identifies the event within its kind and account: the same key notifies once at
	// most (a charge's detection time, a connection and its expiry).
	Key string
	// Args are what a device needs to write the text in its own language: figures, never
	// an address, a place or a position, which would go through Apple's and Google's
	// servers.
	Args map[string]string
}

// Platform is the push service that reaches a device.
type Platform string

// The push services.
const (
	APNs Platform = "apns" // Apple's, for iOS
	FCM  Platform = "fcm"  // Firebase Cloud Messaging, for Android
)

// Platforms are the push services a device may name.
var Platforms = []Platform{APNs, FCM}

// Device is an app installed on a phone that signed in to the account, and the token its
// push service reaches it with.
type Device struct {
	// ID is chosen by the app, once per installation.
	ID       string
	UserID   string
	Platform Platform
	Token    string
	// LiveActivityToken is the token the push service starts a charge's activity on the
	// device with (an iOS push-to-start token); empty: none is started.
	LiveActivityToken string
	Locale            string // BCP 47, as fr-FR: the language the device writes the texts in
	AppVersion        string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Preferences are the choices the account's user made; a kind not in them takes its
// default.
type Preferences struct {
	// Account holds the account's kinds the user turned on or off.
	Account map[Kind]bool
	// VehiclesDefault holds the vehicles' kinds turned on or off for every vehicle.
	VehiclesDefault map[Kind]bool
	// Vehicles holds, by vehicle ID, the vehicles' kinds turned on or off for that vehicle,
	// whatever VehiclesDefault says.
	Vehicles map[string]map[Kind]bool
}

// Enabled tells whether the user is to be told of s, about the vehicle vehicleID for a
// vehicle's kind.
func (p Preferences) Enabled(s Spec, vehicleID string) bool {
	if s.Always {
		return true
	}
	if s.Level == AccountLevel {
		if on, ok := p.Account[s.Kind]; ok {
			return on
		}
		return s.Default
	}
	if on, ok := p.Vehicles[vehicleID][s.Kind]; ok {
		return on
	}
	if on, ok := p.VehiclesDefault[s.Kind]; ok {
		return on
	}
	return s.Default
}

// Delivery is an event told to the devices of its account: recorded before it is sent.
type Delivery struct {
	ID      string // the recorded delivery's
	Event   Event
	Devices []Device
}

// PushSender sends a delivery to its devices, through their push services: an
// extension's. It removes a device whose token its push service no longer knows. Its
// errors are logged; a delivery is never sent again, a lost push being better than a
// duplicate. It returns when its context ends: a notification takes Timeout at most,
// which an API request or a collector's pass waits on.
type PushSender interface {
	Send(ctx context.Context, d Delivery) error
}

// Store keeps the devices, the preferences and the deliveries, each within its account.
type Store interface {
	NotificationPreferences(ctx context.Context, accountID string) (Preferences, error)
	Devices(ctx context.Context, accountID string) ([]Device, error)
	// RecordDelivery records e's delivery at now, once per account, kind and key, and
	// returns its ID and whether to send it: not when one was recorded before under the
	// key, nor when the vehicle already had perVehicle deliveries sent in the hour before
	// now (then it is recorded held back, and never sent).
	RecordDelivery(ctx context.Context, e Event, now time.Time, perVehicle int) (id string, send bool, err error)
}

// PerVehicleHour is how many notifications a vehicle sends at most in an hour: a car
// charging on and off under the sun would otherwise ring all afternoon.
const PerVehicleHour = 4

// Timeout bounds a notification, its reads, its record and its send, whatever the
// caller's context allows: an API request carries on without its own cancellation.
const Timeout = 10 * time.Second

// Fresh is how old a vehicle's event may be to be told: a vehicle read again after a
// long time, or derived again from its history, would otherwise tell its past.
const Fresh = time.Hour

// Retention is how long a delivery is kept: past it, its key may notify again. A
// vehicle's event is told only while Fresh; a credential still refused, or still expired,
// a month later is told again, as a reminder.
const Retention = 30 * 24 * time.Hour

// Notifier tells the devices of an account the events its user wants to know of.
type Notifier struct {
	reg *Registry
	st  Store
	clk clock.Clock
	log *slog.Logger

	mu     sync.RWMutex
	sender PushSender
	// timeout is Timeout, shorter in the tests.
	timeout time.Duration
}

// New returns a Notifier of the kinds of reg, which sends nothing until it is given a
// PushSender.
func New(reg *Registry, st Store, clk clock.Clock, log *slog.Logger) *Notifier {
	return &Notifier{reg: reg, st: st, clk: clk, log: log, timeout: Timeout}
}

// Registry is the kinds the Notifier knows, to which an extension adds its own.
func (n *Notifier) Registry() *Registry { return n.reg }

// SetSender has the deliveries sent by s: an extension's. Call it before the binary
// serves.
func (n *Notifier) SetSender(s PushSender) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sender = s
}

// Sends tells whether the Notifier has a PushSender: without one, the instance serves no
// notifications.
func (n *Notifier) Sends() bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.sender != nil
}

// Wants tells whether an event of kind k about vehicleID (empty for an account's kind)
// would be sent: a sender, a device, and the user's preference. An emitter asks it
// first when an event costs reads to build.
func (n *Notifier) Wants(ctx context.Context, accountID, vehicleID string, k Kind) (bool, error) {
	if !n.Sends() {
		return false, nil
	}
	s, ok := n.reg.Spec(k)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrUnknownKind, k)
	}
	p, err := n.st.NotificationPreferences(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("notification preferences: %w", err)
	}
	if !p.Enabled(s, vehicleID) {
		return false, nil
	}
	devices, err := n.st.Devices(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("devices: %w", err)
	}
	return len(devices) > 0, nil
}

// Notify tells e to the devices of its account, if the user wants to know of it: the
// delivery is recorded, once per key, then handed to the PushSender. Without one,
// nothing is done.
func (n *Notifier) Notify(ctx context.Context, e Event) error {
	s, ok := n.reg.Spec(e.Kind)
	switch {
	case !ok:
		return fmt.Errorf("%w: %s", ErrUnknownKind, e.Kind)
	case e.AccountID == "" || e.Key == "":
		return fmt.Errorf("notification %s: an account and a key are required", e.Kind)
	case (s.Level == VehicleLevel) != (e.VehicleID != ""):
		return fmt.Errorf("notification %s: a vehicle is required for a vehicle's kind, and only for one", e.Kind)
	}
	n.mu.RLock()
	sender := n.sender
	n.mu.RUnlock()
	if sender == nil {
		return nil
	}
	caller := ctx
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	p, err := n.st.NotificationPreferences(ctx, e.AccountID)
	if err != nil {
		return fmt.Errorf("notification preferences: %w", err)
	}
	if !p.Enabled(s, e.VehicleID) {
		return nil
	}
	devices, err := n.st.Devices(ctx, e.AccountID)
	if err != nil {
		return fmt.Errorf("devices: %w", err)
	}
	if len(devices) == 0 {
		return nil
	}
	id, send, err := n.st.RecordDelivery(ctx, e, n.clk.Now(), PerVehicleHour)
	if err != nil {
		return fmt.Errorf("record delivery: %w", err)
	}
	if !send {
		return nil
	}
	if err := sender.Send(ctx, Delivery{ID: id, Event: e, Devices: devices}); err != nil {
		if caller.Err() == nil { // its own Timeout is told
			n.log.Warn("notification not sent", "kind", string(e.Kind), "account", e.AccountID, "delivery", id, "err", err)
		}
		return nil
	}
	n.log.Info("notification sent", "kind", string(e.Kind), "account", e.AccountID, "delivery", id, "devices", len(devices))
	return nil
}

// NotifyAll tells each event, and returns their errors joined.
func (n *Notifier) NotifyAll(ctx context.Context, events []Event) error {
	var errs []error
	for _, e := range events {
		errs = append(errs, n.Notify(ctx, e))
	}
	return errors.Join(errs...)
}
