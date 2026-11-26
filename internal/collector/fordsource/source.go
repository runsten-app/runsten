// Package fordsource reads FordConnect Query for the collector: it adapts the client of
// internal/ford to collector.Source, which knows neither its transport nor its formats.
//
// A connection is the owner's application on Ford's developer portal (its client ID and
// secret) and the refresh token of the sign-in they made with it. Each access is asked
// for with the refresh token; one Ford gives in exchange is kept before the access is
// used. A grant reads one car, the connection's only vehicle: the API takes no VIN.
//
// Ford publishes no limit of calls. Reports of other clients tell of a 429 for two calls
// close together, and of about one call a minute as safe: the source never makes two
// calls of a connection closer than minGap, and a 429 pauses the connection.
package fordsource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/ford"
	"runsten/internal/platform/clock"
)

// Credential is what a connection holds of the owner's application and grant.
type Credential struct {
	Application ford.Application
	Refresh     string
	// SetAt is when the owner connected: a rotation replaces the refresh token of this
	// connection only, never one the owner set since.
	SetAt time.Time
}

// Store reads and keeps each connection's credential: the store's.
type Store interface {
	// FordCredential returns the connection's credential.
	FordCredential(ctx context.Context, accountID, connectionID string) (Credential, error)
	// RotateFordToken replaces the refresh token of the credential set at setAt with
	// token, which a refresh gave; a credential the owner set since is left alone.
	RotateFordToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error
	// SetCredentialExpiry records when the connection's grant, set at setAt, ends; one
	// set since is left alone, and an expiry never moves back.
	SetCredentialExpiry(ctx context.Context, accountID, connectionID string, setAt, expiresAt time.Time) error
}

// Daily is the calls a day the collector's budget grants a connection (API ford in
// api_calls): a prudent cap above a read a minute (1,440), Ford publishing no limit.
const Daily = 2_000

const (
	// GrantLife is how long a sign-in reads before Ford asks for another: a year from
	// the owner's consent (Ford's answer to a user of another client, to check).
	GrantLife = 365 * 24 * time.Hour
	// accessMargin is taken off an access's lifetime.
	accessMargin = time.Minute
	// minGap is the least time between two calls of a connection (assumption: reports
	// tell of a 429 for two calls close together, none of how close).
	minGap = 20 * time.Second
)

// Intervals are the source's cadences (assumptions, to measure on a real car): the
// telemetry every minute driving or charging, every five minutes parked, which sees a
// trip or a charge start within that; the garage once a day. The telemetry is Ford's
// copy of what the car sent: reading it does not wake the car.
func Intervals() collector.Intervals {
	return collector.Intervals{Parked: 5 * time.Minute, Active: time.Minute, Rare: time.Hour, Details: 24 * time.Hour}
}

// Source is FordConnect Query as a collector.Source.
type Source struct {
	client *ford.Client
	store  Store
	clk    clock.Clock

	mu sync.Mutex
	// rotated are the refresh tokens a refresh gave that could not be kept yet, by
	// connection.
	rotated map[string]rotation
	// expiries are the grants' ends last recorded, by connection, so that the store is
	// written on a change alone.
	expiries map[string]time.Time
	// held is until when each vehicle is not to be read, by VIN: minGap after its
	// connection's last call.
	held map[string]time.Time
}

// rotation is a refresh token given in exchange for the one of the credential set at
// setAt.
type rotation struct {
	setAt time.Time
	token string
}

// New returns the Source reading through client, each connection's credential kept in
// st.
func New(client *ford.Client, st Store, clk clock.Clock) *Source {
	return &Source{
		client: client, store: st, clk: clk,
		rotated: map[string]rotation{}, expiries: map[string]time.Time{}, held: map[string]time.Time{},
	}
}

// Spec implements collector.Source. The telemetry is read first, which tells the mode;
// it tells everything of a trip's or a charge's end too, so that nothing is read again at
// once then (Settled is empty: a second call within the minute is what Ford refuses).
func (*Source) Spec() collector.SourceSpec {
	return collector.SourceSpec{
		Name: ford.Source,
		Endpoints: []collector.EndpointSpec{
			{
				Endpoint: collector.Endpoint(ford.Telemetry), API: ford.Source,
				Parked: collector.PaceParked, Driving: collector.PaceActive, Charging: collector.PaceActive,
			},
			{
				Endpoint: collector.Endpoint(ford.Garage), API: ford.Source,
				Parked: collector.PaceDetails, Driving: collector.PaceDetails, Charging: collector.PaceDetails,
			},
		},
		Daily:     Daily,
		Intervals: Intervals(),
	}
}

// Login implements collector.Source: an access of the connection's refresh token, the
// one given in exchange kept first, and the grant's end recorded.
func (s *Source) Login(ctx context.Context, accountID, connectionID string) (collector.Session, time.Duration, error) {
	cred, err := s.store.FordCredential(ctx, accountID, connectionID)
	if err != nil {
		return nil, 0, fmt.Errorf("ford credential: %w", err)
	}
	tok, err := s.client.Refresh(ctx, cred.Application, s.refreshToken(connectionID, cred.Refresh, cred.SetAt))
	if err != nil {
		return nil, 0, sourceError(err)
	}
	if tok.Refresh != cred.Refresh {
		s.keep(connectionID, rotation{setAt: cred.SetAt, token: tok.Refresh})
		if err := s.store.RotateFordToken(ctx, accountID, connectionID, cred.SetAt, tok.Refresh); err != nil {
			return nil, 0, fmt.Errorf("keep the ford refresh token given: %w", err)
		}
		s.keep(connectionID, rotation{})
	}
	// The read stands should the end not be written: it is, at the next login.
	_ = s.expire(ctx, accountID, connectionID, cred.SetAt, tok.AuthTime)
	ttl := tok.ExpiresIn - accessMargin
	if ttl <= 0 {
		ttl = tok.ExpiresIn
	}
	return &session{src: s, access: tok.Access}, ttl, nil
}

// refreshToken returns the refresh token to use: the one a refresh gave and the store
// could not keep, while the credential is the same; else the stored one.
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

// keep holds a refresh token given until the store has it; a zero rotation drops it.
func (s *Source) keep(connectionID string, r rotation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.token == "" {
		delete(s.rotated, connectionID)
		return
	}
	s.rotated[connectionID] = r
}

// expire records the grant's end, GrantLife after the owner's sign-in, when it changed.
func (s *Source) expire(ctx context.Context, accountID, connectionID string, setAt, authTime time.Time) error {
	if authTime.IsZero() {
		return nil
	}
	at := authTime.Add(GrantLife)
	s.mu.Lock()
	same := s.expiries[connectionID].Equal(at)
	s.mu.Unlock()
	if same {
		return nil
	}
	if err := s.store.SetCredentialExpiry(ctx, accountID, connectionID, setAt, at); err != nil {
		return fmt.Errorf("ford grant expiry: %w", err)
	}
	s.mu.Lock()
	s.expiries[connectionID] = at
	s.mu.Unlock()
	return nil
}

// HeldUntil implements collector.Limiter: until when the vehicle's connection made its
// last call too recently for another.
func (s *Source) HeldUntil(vin string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held[vin]
}

// reserve reports whether the vehicle may be called now, and holds it for minGap if so;
// else how long it still waits.
func (s *Source) reserve(vin string) (time.Duration, bool) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if until := s.held[vin]; now.Before(until) {
		return until.Sub(now), false
	}
	s.held[vin] = now.Add(minGap)
	return 0, true
}

// Observe implements collector.Source: the ignition tells whether the car drives, the
// charge's status whether it charges. The garage tells neither.
func (*Source) Observe(ep collector.Endpoint, raw []byte, a *collector.Activity) error {
	if ford.Endpoint(ep) != ford.Telemetry {
		return nil
	}
	s, err := ford.Normalize(ford.Telemetry, raw)
	if err != nil {
		return err //nolint:wrapcheck // ford's, which says what failed
	}
	if s.Covers&core.FieldEngine != 0 && s.Engine.OK {
		a.Engine, a.Driving = string(s.Engine.V), s.Engine.V == core.EngineRunning
	}
	if s.Covers&core.FieldCharging != 0 && s.Charging.OK {
		a.Charging, a.Charges = string(s.Charging.V), s.Charging.V == core.ChargingActive
	}
	return nil
}

// Fingerprint implements collector.Source: the values and the metrics' send times.
func (*Source) Fingerprint(raw []byte) ([]byte, error) {
	return ford.Fingerprint(raw) //nolint:wrapcheck // ford's, which says what failed
}

// Upload implements collector.Source.
func (*Source) Upload(ep collector.Endpoint, raw []byte) (collector.Upload, error) {
	u, err := ford.UploadOf(ford.Endpoint(ep), raw)
	if err != nil {
		return collector.Upload{}, err //nolint:wrapcheck // ford's, which says what failed
	}
	return collector.Upload{At: u.At, Fields: u.Fields}, nil
}

// session reads with an access of a connection's grant. The collector uses it from one
// goroutine.
type session struct {
	src    *Source
	access string
}

// errTooSoon is a call held back, the connection's last one too recent.
var errTooSoon = errors.New("ford: held back, the last call too recent")

// Fetch implements collector.Session: the grant's car, whatever the VIN, which the API
// does not take; never within minGap of the connection's last call.
func (s *session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	if wait, ok := s.src.reserve(vin); !ok {
		return nil, &collector.SourceError{Kind: collector.KindVehicleLimited, RetryIn: wait, Err: errTooSoon}
	}
	raw, err := s.src.client.Fetch(ctx, s.access, ford.Endpoint(ep))
	if err != nil {
		return nil, sourceError(err)
	}
	return raw, nil
}

// sourceError tells the collector what a refusal of Ford means.
func sourceError(err error) error {
	var e *ford.APIError
	if !errors.As(err, &e) {
		return err
	}
	se := &collector.SourceError{Status: e.Status, RetryIn: e.RetryIn, Err: err}
	switch e.Kind {
	case ford.KindGrantRefused, ford.KindClientRefused:
		// The grant withdrawn or over, or the application refused: only the owner
		// connects again.
		se.Kind = collector.KindCredentialRefused
	case ford.KindUnauthorized:
		se.Kind = collector.KindUnauthorized
	case ford.KindRateLimited:
		se.Kind = collector.KindRateLimited
	case ford.KindForbidden, ford.KindNotFound, ford.KindOther:
		// What Ford means by a 403 or a 404 here is not known: a failure, tried again.
		se.Kind = collector.KindOther
	}
	return se
}
