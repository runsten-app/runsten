// Package renaultsource reads the My Renault app's API for the collector (ADR 0050): it
// adapts the client of internal/renault to collector.Source, which knows neither its
// transport nor its formats.
//
// A connection is the owner's sign-in to their My Renault account: a login token and the
// device it was given to, never the password. Each access is a JWT of fifteen minutes
// asked for with the login token; a login token Gigya refuses marks the connection,
// which only a new sign-in reads again. The account's vehicles are listed at most once
// an hour (the person, their accounts, the vehicles of each), and again when one is
// refused: Kamereon gave some accounts a new id in October 2026.
package renaultsource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/renault"
)

// Credential is what a connection holds of the owner's sign-in.
type Credential struct {
	Session  renault.Session
	PersonID string
	// Country is the account's, which every call names: an account read under another
	// has answered empty data.
	Country string
}

// Daily is the calls a day counted per connection (API renault in api_calls): Renault
// publishes no quota, and Home Assistant holds 60 an hour per account; 50 an hour leaves
// the vehicles' lists and another client a margin (ADR 0050 §4).
const Daily = 1_200

const (
	// jwtMargin is taken off a JWT's lifetime, as renault-api does.
	jwtMargin = time.Minute
	// defaultJWTTTL is kept when the JWT's expiry is unreadable: its fifteen minutes
	// less the margin.
	defaultJWTTTL = 14 * time.Minute
	// listEvery is how often the account's vehicles are listed again: Home Assistant's
	// hour.
	listEvery = time.Hour
)

// Intervals are the source's cadences (ADR 0050 §4): Kamereon returns the last state
// the car sent and never wakes it. Parked, ten minutes; charging, five; the position
// and the climate, half an hour; the details, a day.
func Intervals() collector.Intervals {
	return collector.Intervals{Parked: 10 * time.Minute, Active: 5 * time.Minute, Rare: 30 * time.Minute, Details: 24 * time.Hour}
}

// Source is the My Renault app's API as a collector.Source.
type Source struct {
	client     *renault.Client
	credential func(ctx context.Context, accountID, connectionID string) (Credential, error)
	clk        clock.Clock

	// lists are the vehicles of each connection's account, and when they were listed.
	mu    sync.Mutex
	lists map[string]list
}

// list is a connection's vehicles, by VIN, and when they were listed.
type list struct {
	at       time.Time
	vehicles map[string]vehicle
}

// vehicle is where a vehicle is read: its Kamereon account, and its model's code.
type vehicle struct {
	account, model string
}

// New returns the Source reading through client, each connection's sign-in read by
// credential (the store's).
func New(client *renault.Client, credential func(ctx context.Context, accountID, connectionID string) (Credential, error), clk clock.Clock) *Source {
	return &Source{client: client, credential: credential, clk: clk, lists: map[string]list{}}
}

// Spec implements collector.Source. The battery is read first, which tells the charge.
// Kamereon tells no driving: a trip is derived from the odometer and the position.
func (*Source) Spec() collector.SourceSpec {
	ep := func(e renault.Endpoint, parked, driving, charging collector.Pace, optional bool) collector.EndpointSpec {
		return collector.EndpointSpec{
			Endpoint: collector.Endpoint(e), API: renault.Source,
			Parked: parked, Driving: driving, Charging: charging, Optional: optional,
		}
	}
	return collector.SourceSpec{
		Name: renault.Source,
		Endpoints: []collector.EndpointSpec{
			ep(renault.BatteryStatus, collector.PaceParked, collector.PaceParked, collector.PaceActive, false),
			ep(renault.Cockpit, collector.PaceParked, collector.PaceActive, collector.PaceParked, false),
			ep(renault.Location, collector.PaceRare, collector.PaceActive, collector.PaceRare, true),
			ep(renault.HVACStatus, collector.PaceRare, collector.PaceRare, collector.PaceRare, true),
			ep(renault.Details, collector.PaceDetails, collector.PaceDetails, collector.PaceDetails, false),
		},
		// The battery, the odometer and the position are read at once when a trip or
		// a charge ends, or a charge starts.
		Settled: []collector.Endpoint{
			collector.Endpoint(renault.BatteryStatus), collector.Endpoint(renault.Cockpit),
			collector.Endpoint(renault.Location),
		},
		Daily:     Daily,
		Intervals: Intervals(),
	}
}

// Login implements collector.Source: a JWT of the connection's login token, kept until
// a minute before its end, and the account's vehicles when their list is an hour old.
func (s *Source) Login(ctx context.Context, accountID, connectionID string) (collector.Session, time.Duration, error) {
	cred, err := s.credential(ctx, accountID, connectionID)
	if err != nil {
		return nil, 0, fmt.Errorf("renault credential: %w", err)
	}
	jwt, err := s.client.JWT(ctx, cred.Session)
	if err != nil {
		return nil, 0, sourceError(err)
	}
	ttl := defaultJWTTTL
	if !jwt.ExpiresAt.IsZero() {
		switch life := jwt.ExpiresAt.Sub(s.clk.Now()); {
		case life > jwtMargin:
			ttl = life - jwtMargin
		case life > 0:
			ttl = life
		}
	}
	sess := &session{src: s, jwt: jwt.Value, cred: cred, connectionID: connectionID}
	s.mu.Lock()
	l, ok := s.lists[connectionID]
	s.mu.Unlock()
	if !ok || s.clk.Now().Sub(l.at) >= listEvery {
		var prev map[string]vehicle
		if ok {
			prev = l.vehicles
		}
		if l, err = s.relist(ctx, jwt.Value, cred, connectionID, prev); err != nil {
			return nil, 0, sourceError(err)
		}
	}
	sess.vehicles = l.vehicles
	return sess, ttl, nil
}

// relist lists the connection's vehicles and keeps the list, prev being the last one
// (nil: none).
func (s *Source) relist(ctx context.Context, jwt string, cred Credential, connectionID string, prev map[string]vehicle) (list, error) {
	l, err := s.list(ctx, jwt, cred, prev)
	if err != nil {
		return list{}, err
	}
	s.mu.Lock()
	s.lists[connectionID] = l
	s.mu.Unlock()
	return l, nil
}

// list reads the person's accounts, and the vehicles of each of type MYRENAULT. An
// account whose listing fails keeps its vehicles of prev: a passing error is not a car
// removed. Without a previous list, any failure fails the listing, tried again at the
// next login.
func (s *Source) list(ctx context.Context, jwt string, cred Credential, prev map[string]vehicle) (list, error) {
	p, err := s.client.Person(ctx, jwt, cred.PersonID, cred.Country)
	if err != nil {
		return list{}, fmt.Errorf("renault person: %w", err)
	}
	l := list{at: s.clk.Now(), vehicles: map[string]vehicle{}}
	for _, a := range p.Accounts {
		if a.Type != renault.AccountMyRenault {
			continue
		}
		vs, err := s.client.Vehicles(ctx, jwt, a.ID, cred.Country)
		if err != nil {
			if prev == nil {
				return list{}, fmt.Errorf("renault vehicles: %w", err)
			}
			for vin, v := range prev {
				if v.account == a.ID {
					l.vehicles[vin] = v
				}
			}
			continue
		}
		for _, v := range vs {
			l.vehicles[v.VIN] = vehicle{account: a.ID, model: v.ModelCode}
		}
	}
	return l, nil
}

// forget has the connection's vehicles listed again at its next login.
func (s *Source) forget(connectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lists, connectionID)
}

// Observe implements collector.Source: the battery tells whether the car charges.
// Nothing tells whether it drives.
func (*Source) Observe(ep collector.Endpoint, raw []byte, a *collector.Activity) error {
	if renault.Endpoint(ep) != renault.BatteryStatus {
		return nil
	}
	s, err := renault.Normalize(renault.BatteryStatus, raw)
	if err != nil {
		return err //nolint:wrapcheck // renault's, which names the endpoint
	}
	if s.Covers&core.FieldCharging != 0 && s.Charging.OK {
		a.Charging, a.Charges = string(s.Charging.V), s.Charging.V == core.ChargingActive
	}
	return nil
}

// Fingerprint implements collector.Source: the values and the car's send times, the
// pictures' links left out.
func (*Source) Fingerprint(raw []byte) ([]byte, error) {
	return renault.Fingerprint(raw) //nolint:wrapcheck // renault's, which says what failed
}

// Upload implements collector.Source.
func (*Source) Upload(_ collector.Endpoint, raw []byte) (collector.Upload, error) {
	u, err := renault.UploadOf(raw)
	if err != nil {
		return collector.Upload{}, err //nolint:wrapcheck // renault's, which says what failed
	}
	return collector.Upload{At: u.At, Fields: u.Fields}, nil
}

// session reads with a JWT of a connection's login token. The collector uses it from one
// goroutine.
type session struct {
	src          *Source
	jwt          string
	cred         Credential
	connectionID string
	vehicles     map[string]vehicle
	relisted     bool // the vehicles were listed again in this session
}

// errNotListed is a vehicle the account no longer lists.
var errNotListed = errors.New("renault: the account does not list the vehicle")

// Fetch implements collector.Session. An endpoint the vehicle's model does not serve is
// not called; a response of the car's state carries the model's code, which its
// normalization reads the quirks of.
func (s *session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	v, ok := s.vehicles[vin]
	if !ok {
		var err error
		if v, ok, err = s.relocate(ctx, vin); err != nil {
			return nil, sourceError(err)
		} else if !ok {
			return nil, &collector.SourceError{Kind: collector.KindUnshared, Err: errNotListed}
		}
	}
	e := renault.Endpoint(ep)
	if !renault.Serves(v.model, e) {
		return nil, &collector.SourceError{
			Kind: collector.KindNotAvailable, Err: fmt.Errorf("renault: model %s does not serve %s", v.model, e),
		}
	}
	raw, err := s.src.client.Fetch(ctx, s.jwt, v.account, s.cred.Country, vin, e)
	var ae *renault.APIError
	if errors.As(err, &ae) && ae.Kind == renault.KindUnshared {
		// Renault moves an account to another id at times: the vehicles listed again
		// tell its new one, read again before the car is taken for unshared.
		nv, found, lerr := s.relocate(ctx, vin)
		switch {
		case lerr != nil:
			// Not listed: unknown whether the car left, so not told unshared.
			return nil, sourceError(lerr)
		case found && nv.account != v.account:
			v = nv
			raw, err = s.src.client.Fetch(ctx, s.jwt, v.account, s.cred.Country, vin, e)
		}
	}
	if err != nil {
		return nil, sourceError(err)
	}
	if e == renault.Details {
		return raw, nil
	}
	return renault.WithModel(raw, v.model) //nolint:wrapcheck // renault's, which says what failed
}

// relocate lists the account's vehicles again, once a session, and returns where vin is
// read now; false: not listed. A listing that fails returns its error, the list dropped
// to be listed at the next login.
func (s *session) relocate(ctx context.Context, vin string) (vehicle, bool, error) {
	if !s.relisted {
		s.relisted = true
		l, err := s.src.relist(ctx, s.jwt, s.cred, s.connectionID, s.vehicles)
		if err != nil {
			s.src.forget(s.connectionID)
			return vehicle{}, false, err
		}
		s.vehicles = l.vehicles
	}
	v, ok := s.vehicles[vin]
	return v, ok, nil
}

// sourceError tells the collector what a refusal of the API means.
func sourceError(err error) error {
	var e *renault.APIError
	if !errors.As(err, &e) {
		return err
	}
	se := &collector.SourceError{Status: e.Status, RetryIn: e.RetryIn, Err: err}
	switch e.Kind {
	case renault.KindCredentialRefused, renault.KindTFARequired, renault.KindWrongCode:
		// A login token refused, or a sign-in that asks for a code no one is there to
		// enter: only the owner signs in again.
		se.Kind = collector.KindCredentialRefused
	case renault.KindUnauthorized:
		se.Kind = collector.KindUnauthorized
	case renault.KindRateLimited:
		se.Kind = collector.KindRateLimited
	case renault.KindNotAvailable:
		se.Kind = collector.KindNotAvailable
	case renault.KindUnshared:
		se.Kind = collector.KindUnshared
	case renault.KindOther:
		se.Kind = collector.KindOther
	}
	return se
}
