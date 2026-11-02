// Package polestaridsource reads the Polestar app's API for the collector (ADR 0042): it
// adapts the client of internal/polestarid to collector.Source, which knows neither its
// transport nor its formats.
//
// A connection is the owner's Polestar ID: a refresh token, and in the password mode the
// email and password too. Each access is asked for with the refresh token, and the one the
// Polestar ID gives in exchange, a new one at every refresh, is kept before the access is
// used: the one given no longer refreshes. A refresh token refused, the password signs in
// again when the owner gave it. An access lives 5 minutes: the collector asks for one at
// nearly every pass.
package polestaridsource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/polestarid"
)

// Credential is what a connection holds of the owner's Polestar ID.
type Credential struct {
	Refresh string
	// Email and Password are set in the password mode only: they sign in again when the
	// refresh token is refused.
	Email, Password string
	// SetAt is when the owner connected: a rotation replaces the refresh token of this
	// connection only, never one the owner set since.
	SetAt time.Time
}

// Store reads and keeps each connection's credential: the store's.
type Store interface {
	// PolestarIDCredential returns the connection's credential.
	PolestarIDCredential(ctx context.Context, accountID, connectionID string) (Credential, error)
	// RotatePolestarIDToken replaces the refresh token of the credential set at setAt with
	// token, which a refresh or a sign-in gave; a credential the owner set since is left
	// alone.
	RotatePolestarIDToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error
}

// Daily is the calls a day counted per connection, the GraphQL's and the gRPC's together:
// a prudent cap, the API publishing no limit (ADR 0042 §6). A car parked all day is read
// about 600 times (the telematics and the battery every 5 minutes, the charge limit every
// hour), a day with 8 hours of charge about 1,400.
const Daily = 2_000

// Intervals are the cadences (ADR 0042 §6): the telematics and the gRPC's battery every 5
// minutes parked, every minute charging; the charge limit every hour; the model once a
// day. The API tells no driving: a trip is derived from the odometer.
func Intervals() collector.Intervals {
	return collector.Intervals{Parked: 5 * time.Minute, Active: time.Minute, Rare: time.Hour, Details: 24 * time.Hour}
}

// Source is the Polestar app's API as a collector.Source.
type Source struct {
	client *polestarid.Client
	store  Store

	mu sync.Mutex
	// rotated are the refresh tokens a refresh gave that could not be kept yet, by
	// connection: the one stored no longer refreshes.
	rotated map[string]rotation
}

// rotation is a refresh token given in exchange for the one of the credential set at
// setAt.
type rotation struct {
	setAt time.Time
	token string
}

// New returns the Source reading through client, with each connection's credential kept
// in st.
func New(client *polestarid.Client, st Store) *Source {
	return &Source{client: client, store: st, rotated: map[string]rotation{}}
}

// Spec implements collector.Source.
func (*Source) Spec() collector.SourceSpec {
	return collector.SourceSpec{
		Name: polestarid.Source,
		Endpoints: []collector.EndpointSpec{
			{
				Endpoint: collector.Endpoint(polestarid.Telematics), API: polestarid.Source,
				Parked: collector.PaceParked, Driving: collector.PaceActive, Charging: collector.PaceActive,
			},
			// The gRPC's, after the telematics, which they never hold back: optional, a car
			// a service does not serve (a Polestar 2 on PCCS) is asked again a day later,
			// told to no one, and read through the GraphQL alone meanwhile.
			{
				Endpoint: collector.Endpoint(polestarid.Battery), API: polestarid.Source, Optional: true,
				Parked: collector.PaceParked, Driving: collector.PaceActive, Charging: collector.PaceActive,
			},
			{
				Endpoint: collector.Endpoint(polestarid.TargetSoC), API: polestarid.Source, Optional: true,
				Parked: collector.PaceRare, Driving: collector.PaceRare, Charging: collector.PaceRare,
			},
			{
				Endpoint: collector.Endpoint(polestarid.Cars), API: polestarid.Source,
				Parked: collector.PaceDetails, Driving: collector.PaceDetails, Charging: collector.PaceDetails,
			},
		},
		Settled:   []collector.Endpoint{collector.Endpoint(polestarid.Telematics), collector.Endpoint(polestarid.Battery)},
		Daily:     Daily,
		Intervals: Intervals(),
	}
}

// Login implements collector.Source: an access of the connection's refresh token, or of
// a sign-in with the password when it is refused, the refresh token given in exchange
// kept first; then the account's cars.
func (s *Source) Login(ctx context.Context, accountID, connectionID string) (collector.Session, time.Duration, error) {
	cred, err := s.store.PolestarIDCredential(ctx, accountID, connectionID)
	if err != nil {
		return nil, 0, fmt.Errorf("polestar id credential: %w", err)
	}
	refresh := s.refreshToken(connectionID, cred.Refresh, cred.SetAt)
	tok, err := s.client.Refresh(ctx, refresh)
	var e *polestarid.APIError
	if err != nil && errors.As(err, &e) && e.Kind == polestarid.KindTokenRefused && cred.Email != "" {
		tok, err = s.client.SignIn(ctx, cred.Email, cred.Password)
	}
	if err != nil {
		return nil, 0, sourceError(err)
	}
	if tok.Refresh != cred.Refresh {
		s.keep(connectionID, rotation{setAt: cred.SetAt, token: tok.Refresh})
		if err := s.store.RotatePolestarIDToken(ctx, accountID, connectionID, cred.SetAt, tok.Refresh); err != nil {
			return nil, 0, fmt.Errorf("keep the polestar id refresh token given: %w", err)
		}
		s.keep(connectionID, rotation{})
	}
	sess := &session{client: s.client, token: tok.Access}
	if err := sess.list(ctx); err != nil {
		return nil, 0, err
	}
	return sess, tok.ExpiresIn, nil
}

// refreshToken is the refresh token to refresh with: the one a refresh gave, not kept
// yet, unless the owner connected again since.
func (s *Source) refreshToken(connectionID, stored string, setAt time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rotated[connectionID]
	if !ok || !r.setAt.Equal(setAt) {
		delete(s.rotated, connectionID)
		return stored
	}
	return r.token
}

// keep remembers r for the connection until it is stored; a zero r: it was.
func (s *Source) keep(connectionID string, r rotation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.token == "" {
		delete(s.rotated, connectionID)
		return
	}
	s.rotated[connectionID] = r
}

// Observe implements collector.Source: the telematics and the gRPC's battery tell whether
// the car charges. They tell no driving.
func (*Source) Observe(ep collector.Endpoint, raw []byte, a *collector.Activity) error {
	e := polestarid.Endpoint(ep)
	if e != polestarid.Telematics && e != polestarid.Battery {
		return nil
	}
	s, err := polestarid.Normalize(e, raw)
	if err != nil {
		return err //nolint:wrapcheck // polestarid's, which says what failed
	}
	if s.Covers&core.FieldCharging != 0 && s.Charging.OK {
		a.Charging, a.Charges = string(s.Charging.V), s.Charging.V == core.ChargingActive
	}
	return nil
}

// Fingerprint implements collector.Source.
func (*Source) Fingerprint(raw []byte) ([]byte, error) {
	return polestarid.Fingerprint(raw) //nolint:wrapcheck // polestarid's, which says what failed
}

// Upload implements collector.Source.
func (*Source) Upload(ep collector.Endpoint, raw []byte) (collector.Upload, error) {
	u, err := polestarid.UploadOf(polestarid.Endpoint(ep), raw)
	if err != nil {
		return collector.Upload{}, err //nolint:wrapcheck // polestarid's, which says what failed
	}
	return collector.Upload{At: u.At, ModelYear: u.ModelYear, Fields: u.Fields}, nil
}

// session reads with an access token. The collector uses it from one goroutine.
type session struct {
	client *polestarid.Client
	token  string
	cars   map[string][]byte // each car's entry of the list, by VIN
}

// list reads the account's cars.
func (s *session) list(ctx context.Context) error {
	cars, err := s.client.Cars(ctx, s.token)
	if err != nil {
		return sourceError(err)
	}
	s.cars = make(map[string][]byte, len(cars))
	for _, c := range cars {
		s.cars[c.VIN] = c.Raw
	}
	return nil
}

// errNotListed: the account no longer lists the car (sold, or removed from it).
var errNotListed = errors.New("polestar id: the car is not among the account's cars")

// Fetch implements collector.Session. A car the account did not list at login is looked
// for again, once: it may have been added since. Its model is the entry of the login's
// list: no call.
func (s *session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	car, ok := s.cars[vin]
	if !ok {
		if err := s.list(ctx); err != nil {
			return nil, err
		}
		if car, ok = s.cars[vin]; !ok {
			return nil, &collector.SourceError{Kind: collector.KindUnshared, Err: errNotListed}
		}
	}
	switch polestarid.Endpoint(ep) {
	case polestarid.Cars:
		return car, nil
	case polestarid.Telematics:
		raw, err := s.client.Telematics(ctx, s.token, vin)
		if err != nil {
			return nil, sourceError(err)
		}
		return raw, nil
	case polestarid.Battery:
		raw, err := s.client.Battery(ctx, s.token, vin)
		if err != nil {
			return nil, grpcError(err)
		}
		return raw, nil
	case polestarid.TargetSoC:
		raw, err := s.client.TargetSoC(ctx, s.token, vin)
		if err != nil {
			return nil, grpcError(err)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("polestar id: unknown endpoint %q", ep)
}

// grpcError tells the collector what a refusal of a gRPC service means: the endpoint's,
// never the connection's, which the GraphQL reads on.
//
// The access is the one the GraphQL reads with, asked for again a minute before it ends:
// a service refusing it (UNAUTHENTICATED) or limiting it refuses the car or the account,
// not the access, whose end the GraphQL's refusal tells. Assumption, not seen on a real
// car: the endpoint is left out for a rare interval, rather than the connection paused.
func grpcError(err error) error {
	var e *polestarid.APIError
	if !errors.As(err, &e) {
		return err
	}
	se := &collector.SourceError{Kind: collector.KindOther, Status: e.Status, Err: err}
	switch e.Kind {
	case polestarid.KindNotProvisioned:
		se.Kind = collector.KindNotGranted
	case polestarid.KindUnauthorized, polestarid.KindRateLimited:
		se.Kind = collector.KindNotAvailable
	case polestarid.KindOther, polestarid.KindTokenRefused, polestarid.KindSignInRefused:
	}
	return se
}

// sourceError tells the collector what a refusal of the Polestar ID or the API means.
func sourceError(err error) error {
	var e *polestarid.APIError
	if !errors.As(err, &e) {
		return err
	}
	se := &collector.SourceError{Status: e.Status, RetryIn: e.RetryIn, Err: err}
	switch e.Kind {
	case polestarid.KindTokenRefused, polestarid.KindSignInRefused:
		se.Kind = collector.KindCredentialRefused
	case polestarid.KindUnauthorized:
		se.Kind = collector.KindUnauthorized
	case polestarid.KindRateLimited:
		se.Kind = collector.KindRateLimited
	case polestarid.KindOther, polestarid.KindNotProvisioned:
		se.Kind = collector.KindOther
	}
	return se
}
