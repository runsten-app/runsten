// Package teslasource reads Tesla's Owner API for the collector: it adapts the client of
// internal/tesla to collector.Source, which knows neither its transport nor its formats.
//
// A connection is the owner's refresh token. Each access is asked for with it, and the
// refresh token Tesla may give in exchange is kept before the access is used: the one
// given may no longer refresh. The vehicles are read by Tesla's identifier, listed with
// each access.
//
// A car read by vehicle_data does not fall asleep: vehicle_data is read only while the
// car is awake, and is left alone for a while once the car is parked, so that it may
// fall asleep (collector.Sleep). The car's state, read without waking it, is read
// throughout.
package teslasource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/tesla"
)

// Store reads and keeps the refresh token of each connection: the store's.
type Store interface {
	// TeslaToken returns the connection's refresh token, and when the owner set it.
	TeslaToken(ctx context.Context, accountID, connectionID string) (token string, setAt time.Time, err error)
	// RotateTeslaToken replaces the refresh token set at setAt with token, which a refresh
	// gave in exchange; a token the owner set since is left alone.
	RotateTeslaToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error
}

// Daily is the calls a day counted per refresh token: a prudent cap, the Owner API
// publishing no limit (ADR 0040 §4). A car awake all day is read about 1,700 times.
const Daily = 3_000

// Intervals are the cadences, to be tuned on a real car (ADR 0040 §4, §8): the car's
// state every minute; vehicle_data every minute parked and awake or charging, every 15 s
// driving. TeslaMate reads more often (2.5 s driving): the trips and charges do without,
// and each call is one more risk of being limited.
func Intervals() collector.Intervals {
	return collector.Intervals{Parked: time.Minute, Active: 15 * time.Second, Rare: time.Hour, Details: 24 * time.Hour}
}

// The sleep window: parked and awake for IdleBeforeSleep, vehicle_data is not read for
// SleepWindow, so that the car may fall asleep. A trip started meanwhile is seen at its
// end or at the window's, and derived from the odometer.
const (
	IdleBeforeSleep = 3 * time.Minute
	SleepWindow     = 15 * time.Minute
)

// Source is the Owner API as a collector.Source.
type Source struct {
	client *tesla.Client
	store  Store

	mu sync.Mutex
	// rotated are the refresh tokens a refresh gave that could not be kept yet, by
	// connection: the one stored may no longer refresh.
	rotated map[string]rotation
}

// rotation is a refresh token given in exchange for the one set at setAt.
type rotation struct {
	setAt time.Time
	token string
}

// New returns the Source reading through client, with the refresh token of each
// connection kept in st.
func New(client *tesla.Client, st Store) *Source {
	return &Source{client: client, store: st, rotated: map[string]rotation{}}
}

// Spec implements collector.Source.
func (*Source) Spec() collector.SourceSpec {
	return collector.SourceSpec{
		Name: tesla.Source,
		Endpoints: []collector.EndpointSpec{
			// It tells whether the car is awake. Driving, vehicle_data tells it.
			{
				Endpoint: collector.Endpoint(tesla.VehicleState), API: tesla.Source,
				Parked: collector.PaceParked, Driving: collector.PaceRare, Charging: collector.PaceParked,
			},
			{
				Endpoint: collector.Endpoint(tesla.VehicleData), API: tesla.Source,
				Parked: collector.PaceParked, Driving: collector.PaceActive, Charging: collector.PaceParked,
				Awake: true,
			},
		},
		Settled:   []collector.Endpoint{collector.Endpoint(tesla.VehicleData)},
		Daily:     Daily,
		Intervals: Intervals(),
		Sleep:     collector.Sleep{IdleBefore: IdleBeforeSleep, Window: SleepWindow},
	}
}

// Login implements collector.Source: an access token of the connection's refresh token,
// the refresh token given in exchange kept first, and the account's vehicles.
func (s *Source) Login(ctx context.Context, accountID, connectionID string) (collector.Session, time.Duration, error) {
	stored, setAt, err := s.store.TeslaToken(ctx, accountID, connectionID)
	if err != nil {
		return nil, 0, fmt.Errorf("tesla refresh token: %w", err)
	}
	refresh := s.refreshToken(connectionID, stored, setAt)
	tok, err := s.client.Refresh(ctx, refresh)
	if err != nil {
		return nil, 0, sourceError(err)
	}
	if tok.Refresh != stored {
		s.keep(connectionID, rotation{setAt: setAt, token: tok.Refresh})
		if err := s.store.RotateTeslaToken(ctx, accountID, connectionID, setAt, tok.Refresh); err != nil {
			return nil, 0, fmt.Errorf("keep the tesla refresh token given: %w", err)
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
// yet, unless the owner set another since.
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

// Observe implements collector.Source: the vehicle's state tells whether the car is
// awake; vehicle_data, which only an awake car answers, whether it drives or charges.
func (*Source) Observe(ep collector.Endpoint, raw []byte, a *collector.Activity) error {
	switch tesla.Endpoint(ep) {
	case tesla.VehicleState:
		st, err := tesla.StateOf(raw)
		if err != nil {
			return err //nolint:wrapcheck // tesla's, which says what failed
		}
		a.Asleep = st == tesla.Asleep || st == tesla.Offline
	case tesla.VehicleData:
		s, err := tesla.Normalize(tesla.VehicleData, raw)
		if err != nil {
			return err //nolint:wrapcheck // tesla's, which says what failed
		}
		a.Asleep = false
		if s.Covers&core.FieldEngine != 0 && s.Engine.OK {
			a.Engine, a.Driving = string(s.Engine.V), s.Engine.V == core.EngineRunning
		}
		if s.Covers&core.FieldCharging != 0 && s.Charging.OK {
			a.Charging, a.Charges = string(s.Charging.V), s.Charging.V == core.ChargingActive
		}
	}
	return nil
}

// Fingerprint implements collector.Source.
func (*Source) Fingerprint(raw []byte) ([]byte, error) {
	return tesla.Fingerprint(raw) //nolint:wrapcheck // tesla's, which says what failed
}

// Upload implements collector.Source.
func (*Source) Upload(_ collector.Endpoint, raw []byte) (collector.Upload, error) {
	u, err := tesla.UploadOf(raw)
	if err != nil {
		return collector.Upload{}, err //nolint:wrapcheck // tesla's, which says what failed
	}
	return collector.Upload{At: u.At, ModelYear: u.ModelYear, Fields: u.Fields}, nil
}

// session reads with an access token. The collector uses it from one goroutine.
type session struct {
	client *tesla.Client
	token  string
	ids    map[string]string // Tesla's identifier of each vehicle, by VIN
}

// list reads the account's vehicles.
func (s *session) list(ctx context.Context) error {
	products, err := s.client.Products(ctx, s.token)
	if err != nil {
		return sourceError(err)
	}
	s.ids = make(map[string]string, len(products))
	for _, p := range products {
		s.ids[p.VIN] = p.ID
	}
	return nil
}

// errNotListed: the account no longer lists the vehicle (sold, or removed from it).
var errNotListed = errors.New("tesla: the vehicle is not among the account's products")

// Fetch implements collector.Session. A vehicle the account did not list at login is
// looked for again, once: it may have been added since.
func (s *session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	id, ok := s.ids[vin]
	if !ok {
		if err := s.list(ctx); err != nil {
			return nil, err
		}
		if id, ok = s.ids[vin]; !ok {
			return nil, &collector.SourceError{Kind: collector.KindUnshared, Err: errNotListed}
		}
	}
	var raw []byte
	var err error
	switch tesla.Endpoint(ep) {
	case tesla.VehicleState:
		raw, err = s.client.Vehicle(ctx, s.token, id)
	case tesla.VehicleData:
		raw, err = s.client.VehicleData(ctx, s.token, id)
	default:
		return nil, fmt.Errorf("tesla: unknown endpoint %q", ep)
	}
	if err != nil {
		return nil, sourceError(err)
	}
	return raw, nil
}

// sourceError tells the collector what a refusal of the Owner API means.
func sourceError(err error) error {
	var e *tesla.APIError
	if !errors.As(err, &e) {
		return err
	}
	se := &collector.SourceError{Status: e.Status, RetryIn: e.RetryIn, Err: err}
	switch e.Kind {
	case tesla.KindTokenRefused:
		se.Kind = collector.KindCredentialRefused
	case tesla.KindUnauthorized:
		se.Kind = collector.KindUnauthorized
	case tesla.KindOwnerAPIClosed:
		se.Kind = collector.KindAPIClosed
	case tesla.KindUnavailable:
		se.Kind = collector.KindAsleep
	case tesla.KindRateLimited:
		se.Kind = collector.KindRateLimited
	case tesla.KindOther:
		se.Kind = collector.KindOther
	}
	return se
}
