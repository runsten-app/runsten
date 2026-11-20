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
