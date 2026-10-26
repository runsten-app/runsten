package collector

import (
	"context"
	"time"

	"runsten/internal/volvo"
)

// Endpoint is what a call reads of a vehicle, named as its source names it: a
// volvo.Endpoint, a domain of the Polestar Data Portal.
type Endpoint string

// Pace is how often an endpoint is read: one of the Intervals.
type Pace int

// The paces.
const (
	PaceParked  Pace = iota // Intervals.Parked
	PaceActive              // Intervals.Active
	PaceRare                // Intervals.Rare
	PaceDetails             // Intervals.Details
)

// EndpointSpec is an endpoint of a source: its pace in each mode, and what its calls
// count on.
type EndpointSpec struct {
	Endpoint Endpoint
	// API is what its calls are counted on in the budget, with the key or the credential
	// they are made with.
	API                       string
	Parked, Driving, Charging Pace
	// Optional: the credential may not be granted it, which is then no failure told to
	// the user (the vehicle's health, which the trips and charges do without).
	Optional bool
}

func (s EndpointSpec) pace(m mode) Pace {
	switch m {
	case driving:
		return s.Driving
	case charging:
		return s.Charging
	}
	return s.Parked
}

// SourceSpec is what the collector knows of a provider's API.
type SourceSpec struct {
	// Name names the provider of the connections it reads (Target.Provider), and the
	// source of their snapshots.
	Name string
	// Endpoints are read in this order in a pass: those that tell the mode first, so that
	// a transition triggers the readings of its end within the same pass.
	Endpoints []EndpointSpec
	// Settled are read at once when a trip or a charge ends.
	Settled []Endpoint
	// Daily is the number of calls a day granted per API and credential, which the
	// budget spreads as it spreads Quota.Daily.
	Daily int
}

// Activity is what the responses tell of a vehicle's mode: the states of its engine
// and of its charge as the provider names them, which the debug page shows, and whether
// it drives or charges.
type Activity struct {
	Engine, Charging string
	Driving, Charges bool
}

// Source is a provider whose vehicles a connection reads with a credential of its own
// (the Polestar Data Portal), rather than with the Volvo ID's tokens and the application
// keys of the Volvo API, which New takes. The binary adapts the provider's client: the
// collector knows neither its transport nor its formats.
type Source interface {
	Spec() SourceSpec
	// Login asks for an access to the vehicles of the connection's credential, valid for
	// the duration returned. Its errors, and those of the session, are a *SourceError
	// when the provider answered.
	Login(ctx context.Context, accountID, connectionID string) (Session, time.Duration, error)
	// Observe reads what the response of ep tells of the vehicle's mode into a, leaving
	// what it does not tell.
	Observe(ep Endpoint, raw []byte, a *Activity) error
	// Fingerprint returns the fingerprint of a response's values, the provider's
	// timestamps excluded: two responses with the same bring nothing new.
	Fingerprint(raw []byte) ([]byte, error)
	// Upload reads what a response of ep tells of the vehicle's upload it carries, for
	// the logs.
	Upload(ep Endpoint, raw []byte) (Upload, error)
}

// Upload is what a response tells of the vehicle's upload it carries: the logs measure
// with it how often a provider's vehicles upload, and what each model gives, which no
// documentation says. It holds neither the VIN nor any value.
type Upload struct {
	At         time.Time // when the vehicle sent it; zero if not told
	ReceivedAt time.Time // when the provider received it; zero if not told
	ModelYear  int       // 0 if not known
	Fields     []string  // the paths of its fields, sorted
}

// Session reads vehicles with an access of a credential.
type Session interface {
	Fetch(ctx context.Context, vin string, ep Endpoint) ([]byte, error)
}

// SourceStore records what a Source's provider tells of the connections and vehicles.
type SourceStore interface {
	// SetCredentialRefused records that the provider refused the connection's credential
	// set at setAt; a credential set since is left alone.
	SetCredentialRefused(ctx context.Context, accountID, connectionID string, setAt time.Time) error
	// SetUnshared records that the vehicle is no longer shared with its connection's
	// credential.
	SetUnshared(ctx context.Context, accountID, vehicleID string) error
}

// ErrorKind classifies a SourceError by what the collector does of it.
type ErrorKind int

// The kinds of a SourceError.
const (
	// KindOther: the call failed (a server error); tried again at its next due time.
	KindOther ErrorKind = iota
	// KindCredentialRefused: the credential no longer reads; the connection is marked,
	// and not read until the user sets another.
	KindCredentialRefused
	// KindUnauthorized: the access expired; a new one is asked for, once per pass.
	KindUnauthorized
	// KindUnshared: the vehicle is no longer shared with the credential; it is marked,
	// and not read until it is listed again.
	KindUnshared
	// KindNotGranted: the endpoint is not granted to the credential; left out for a
	// details interval, and told to the user.
	KindNotGranted
	// KindNotAvailable: the vehicle does not give the endpoint; left out for a rare
	// interval, and not told: nothing the user can act upon.
	KindNotAvailable
	// KindRateLimited: the connection waits RetryIn.
	KindRateLimited
)

// SourceError is a refusal of a Source's provider. It holds neither the access nor the
// credential.
type SourceError struct {
	Kind    ErrorKind
	Status  int           // HTTP status
	RetryIn time.Duration // for KindRateLimited
	Err     error
}

func (e *SourceError) Error() string { return e.Err.Error() }

func (e *SourceError) Unwrap() error { return e.Err }

// source is what the collector knows of a provider: Volvo's, built in, or a Source.
type source struct {
	SourceSpec
	observe     func(ep Endpoint, raw []byte, a *Activity) error
	fingerprint func(raw []byte) ([]byte, error)
	ext         Source // nil for Volvo's, which the collector's API reads
	store       SourceStore
}

// optional reports whether ep is an optional endpoint of the source.
func (s *source) optional(ep Endpoint) bool {
	for _, spec := range s.Endpoints {
		if spec.Endpoint == ep {
			return spec.Optional
		}
	}
	return false
}

// volvoSource is the Volvo Cars API: its calls count on the key's quota (Quota).
func volvoSource() *source {
	return &source{
		SourceSpec: SourceSpec{
			Name:      volvo.Source,
			Endpoints: endpoints(),
			Settled:   []Endpoint{Endpoint(volvo.Location), Endpoint(volvo.Statistics), Endpoint(volvo.Odometer)},
		},
		observe:     observeVolvo,
		fingerprint: volvo.Fingerprint,
	}
}

// endpoints of the Volvo API in pass order: engine and charging state first.
func endpoints() []EndpointSpec {
	spec := func(ep volvo.Endpoint, parked, driving, charging Pace) EndpointSpec {
		return EndpointSpec{Endpoint: Endpoint(ep), API: ep.API(), Parked: parked, Driving: driving, Charging: charging}
	}
	rare := func(ep volvo.Endpoint) EndpointSpec { return spec(ep, PaceRare, PaceRare, PaceRare) }
	return []EndpointSpec{
		spec(volvo.EngineStatus, PaceParked, PaceActive, PaceParked),
		spec(volvo.EnergyState, PaceParked, PaceActive, PaceActive),
		spec(volvo.Odometer, PaceRare, PaceActive, PaceRare),
		rare(volvo.Location), rare(volvo.Statistics),
		spec(volvo.Details, PaceDetails, PaceDetails, PaceDetails),
		rare(volvo.Diagnostics), rare(volvo.Brakes), rare(volvo.Engine), rare(volvo.Fuel),
		rare(volvo.Tyres), rare(volvo.Warnings), rare(volvo.Doors), rare(volvo.Windows),
	}
}

// observeVolvo reads the engine status and the charging status.
func observeVolvo(ep Endpoint, raw []byte, a *Activity) error {
	switch volvo.Endpoint(ep) {
	case volvo.EngineStatus:
		e, err := volvo.ParseEngineStatus(raw)
		if err != nil {
			return err //nolint:wrapcheck // volvo's, which names the endpoint
		}
		a.Engine, a.Driving = e, e == "RUNNING"
	case volvo.EnergyState:
		s, err := volvo.ParseChargingStatus(raw)
		if err != nil {
			return err //nolint:wrapcheck // volvo's, which names the endpoint
		}
		a.Charging, a.Charges = s, s == "CHARGING"
	}
	return nil
}

// AddSource has the vehicles of the connections of src's provider read through it,
// from the next pass on; st records what the provider tells of them. Call it before
// Run.
func (c *Collector) AddSource(src Source, st SourceStore) {
	spec := src.Spec()
	spec.Endpoints = append([]EndpointSpec(nil), spec.Endpoints...)
	c.sources[spec.Name] = &source{SourceSpec: spec, observe: src.Observe, fingerprint: src.Fingerprint, ext: src, store: st}
	for _, ep := range spec.Endpoints {
		c.budget.addSource(spec.Name, ep.API, spec.Daily)
	}
}

// sourceOf returns the source t is read through; nil when none reads its provider.
func (c *Collector) sourceOf(t Target) *source {
	switch {
	case t.readsVolvo() && c.noVolvo:
		return nil
	case t.readsVolvo():
		return c.volvo
	}
	return c.sources[t.Provider]
}
