// Package notify tells a user's devices what happened while the app was closed: a
// connection to a maker about to expire or refused, a charge interrupted or finished, a
// trip ended.
//
// The collector, and the API, emit typed events (Event): a kind, an account, a vehicle
// or none, an instant, arguments, and a key so that an event notifies once at most. A
// Notifier reads the account's preferences, records the delivery, then hands it to a
// PushSender, which reaches the devices (APNs, FCM). The core has no PushSender: without
// one, nothing is sent, nothing is recorded, and the instance says it serves no
// notifications.
//
// The kinds are registered (Registry): the core declares its own, an extension adds its
// own through the same registry. An unknown value triggers nothing: without a charge
// limit, no charge is told interrupted.
//
// The package knows neither the network nor the database: the store implements its
// Store, an extension its PushSender.
package notify

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync"
)

// Level is what a kind is about: the account, or one of its vehicles.
type Level string

const (
	// AccountLevel kinds are about the account: their preference is the account's.
	AccountLevel Level = "account"
	// VehicleLevel kinds are about a vehicle: their preference is a default for every
	// vehicle, which each vehicle may override.
	VehicleLevel Level = "vehicle"
)

// Kind names a kind of notification, in snake case: credential_expiring.
type Kind string

// The core's kinds.
const (
	// CredentialExpiring: the credential a connection to a maker reads with expires
	// soon, or has expired (publish.RenewAhead).
	CredentialExpiring Kind = "credential_expiring" //nolint:gosec // a kind of notification, not a credential
	// CredentialRefused: the maker refused the connection's credential, or its grant was
	// lost: its vehicles are no longer read.
	CredentialRefused Kind = "credential_refused" //nolint:gosec // a kind of notification, not a credential
	// AccessGranted: a new access to the account, such as a personal access token.
	AccessGranted Kind = "access_granted"
	// ImportFinished: an import of a vehicle's history ended, done or failed.
	ImportFinished Kind = "import_finished"
	// ChargeInterrupted: a charge stopped before its limit, the cable still connected.
	ChargeInterrupted Kind = "charge_interrupted"
	// ChargeFinished: a charge reached its limit.
	ChargeFinished Kind = "charge_finished"
	// TripToClassify: a trip ended without a purpose, neither marked nor given by a
	// place's rule.
	TripToClassify Kind = "trip_to_classify"
	// ChargeStarted: a charge started.
	ChargeStarted Kind = "charge_started"
	// TripEnded: a trip ended, with its distance and energy.
	TripEnded Kind = "trip_ended"
	// ChargeLiveActivity: the charge in progress shown on the lock screen as it goes on
	// (a Live Activity), started, updated and ended by the instance (Notifier.Live).
	ChargeLiveActivity Kind = "charge_live_activity"
	// NotCharged: the vehicle was plugged in at one of the account's places in the
	// evening, and by the morning its state of charge has not risen (core.NotCharged).
	// Told once a night.
	NotCharged Kind = "not_charged"
	// BatteryLow: the vehicle is parked with its state of charge under a threshold
	// (core.BatteryLow). Told once an episode: again only once the state of charge went
	// back to the threshold or above.
	BatteryLow Kind = "battery_low"
	// LeftOpen: the vehicle has stood parked, and unlocked or with a window open, for
	// more than publish.LeftOpenAfter (publish.LeftOpen). Told once an episode.
	LeftOpen Kind = "left_open"
)

// Threshold bounds and defaults a kind's setting, a whole number: a percentage of the
// state of charge for BatteryLow.
type Threshold struct {
	Min, Max, Default int
}

// The low battery's threshold, in % of the state of charge.
const (
	BatteryLowMin     = 5
	BatteryLowMax     = 50
	BatteryLowDefault = 20
)

// Spec declares a kind: its level, whether it is on when the user chose nothing, and
// whether the user may turn it off at all.
type Spec struct {
	Kind    Kind
	Level   Level
	Default bool
	// Always: the user cannot turn it off (a message about the account itself). Its
	// Default is then true.
	Always bool
	// Threshold is the setting of a kind that has one, which the user sets for every
	// vehicle and for each vehicle; nil: the kind has none.
	Threshold *Threshold
}

// CoreKinds are the core's kinds, in the order they are listed.
func CoreKinds() []Spec {
	return []Spec{
		{Kind: CredentialExpiring, Level: AccountLevel, Default: true},
		{Kind: CredentialRefused, Level: AccountLevel, Default: true},
		{Kind: AccessGranted, Level: AccountLevel, Default: true},
		{Kind: ImportFinished, Level: AccountLevel, Default: true},
		{Kind: ChargeInterrupted, Level: VehicleLevel, Default: true},
		{Kind: ChargeFinished, Level: VehicleLevel, Default: true},
		{Kind: TripToClassify, Level: VehicleLevel},
		{Kind: ChargeStarted, Level: VehicleLevel},
		{Kind: TripEnded, Level: VehicleLevel},
		{Kind: ChargeLiveActivity, Level: VehicleLevel, Default: true},
		{Kind: NotCharged, Level: VehicleLevel, Default: true},
		{
			Kind: BatteryLow, Level: VehicleLevel, Default: true,
			Threshold: &Threshold{Min: BatteryLowMin, Max: BatteryLowMax, Default: BatteryLowDefault},
		},
		{Kind: LeftOpen, Level: VehicleLevel},
	}
}

// ErrUnknownKind is returned for a kind no one registered.
var ErrUnknownKind = errors.New("unknown notification kind")

var kindName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Registry holds the kinds a build knows, the core's and its extensions'. It is safe
// for concurrent use; the binaries register every kind before they serve.
type Registry struct {
	mu    sync.RWMutex
	specs []Spec
}

// NewRegistry returns a registry of the core's kinds.
func NewRegistry() *Registry {
	return &Registry{specs: CoreKinds()}
}

// Register adds a kind, listed after those registered before it. A kind registered
// twice, of no known level, or not in snake case is refused.
func (r *Registry) Register(s Spec) error {
	if !kindName.MatchString(string(s.Kind)) {
		return fmt.Errorf("notification kind %q: not in snake case", s.Kind)
	}
	if s.Level != AccountLevel && s.Level != VehicleLevel {
		return fmt.Errorf("notification kind %q: unknown level %q", s.Kind, s.Level)
	}
	if t := s.Threshold; t != nil {
		if s.Level != VehicleLevel || t.Min > t.Default || t.Default > t.Max {
			return fmt.Errorf("notification kind %q: a threshold of a vehicle's kind, min <= default <= max", s.Kind)
		}
		s.Threshold = &Threshold{Min: t.Min, Max: t.Max, Default: t.Default}
	}
	if s.Always {
		s.Default = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if slices.ContainsFunc(r.specs, func(o Spec) bool { return o.Kind == s.Kind }) {
		return fmt.Errorf("notification kind %q: registered twice", s.Kind)
	}
	r.specs = append(r.specs, s)
	return nil
}

// Spec returns the kind's declaration.
func (r *Registry) Spec(k Kind) (Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i := slices.IndexFunc(r.specs, func(s Spec) bool { return s.Kind == k })
	if i < 0 {
		return Spec{}, false
	}
	return r.specs[i], true
}

// Kinds returns the kinds of a level, in the order they were registered.
func (r *Registry) Kinds(l Level) []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Spec
	for _, s := range r.specs {
		if s.Level == l {
			out = append(out, s)
		}
	}
	return out
}
