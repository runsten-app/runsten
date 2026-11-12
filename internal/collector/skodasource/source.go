// Package skodasource reads the MyŠkoda Public API for the collector: it adapts the client
// of internal/skoda to collector.Source, which knows neither its transport nor its
// formats.
//
// A connection is the owner's key, created in the MyŠkoda app and bound to the vehicles
// they ticked; there is no token to ask for. Škoda limits the calls per VIN (20 an hour,
// "not final"), which every response's RateLimit-* headers tell: the source holds a
// vehicle back once its quota is down to a margin, until it replenishes, and on a 429,
// without touching the connection's other vehicles (collector.Limiter). The key's expiry,
// on every success, is kept in the store, so that the Connection page warns before it.
package skodasource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/skoda"
)

// Credential is what a connection holds of the owner's key, and when it was set: the
// expiry the API tells is the key's set then, never one the user replaced since.
type Credential struct {
	APIKey string
	SetAt  time.Time
}

// Store records the key's expiry the API tells.
type Store interface {
	// SetCredentialExpiry records when the connection's key, set at setAt, expires; a
	// key set since is left alone, and an expiry never moves back.
	SetCredentialExpiry(ctx context.Context, accountID, connectionID string, setAt, expiresAt time.Time) error
}

// Daily is the calls a day the collector's budget grants a key (API skoda in api_calls):
// Škoda counts per VIN and per hour, which the headers tell and the source keeps to; the
// budget is set above it (20 an hour, 24 hours, 5 vehicles a key at most in the app) so
// that it never holds back before the headers do.
const Daily = 20 * 24 * 5

// reserve is the calls of a VIN's hour the source leaves unspent: another client of the
// same key (evcc, Home Assistant) spends the same quota, and a 5xx counts (assumption: 2,
// enough not to meet a 429 from a read the other made meanwhile).
const reserve = 2

// keyLife is how long a source's access lasts: there is none at Škoda, the key itself
// reads; the key is read again from the store at this pace, or when the user sets
// another.
const keyLife = 24 * time.Hour

// Intervals are the source's cadences, within Škoda's 20 calls an hour per VIN
// (assumption, to measure on a real car): parked every 20 minutes (3 an hour), driving or
// charging every 5 (12 an hour), which leaves the end of a trip or a charge read at once,
// and the margin. The rare and details paces read nothing apart: the one endpoint tells
// everything.
func Intervals() collector.Intervals {
	return collector.Intervals{Parked: 20 * time.Minute, Active: 5 * time.Minute, Rare: time.Hour, Details: 24 * time.Hour}
}

// Source is the MyŠkoda Public API as a collector.Source.
type Source struct {
	client     *skoda.Client
	credential func(ctx context.Context, accountID, connectionID string) (Credential, error)
	store      Store
	clk        clock.Clock

	// held are the VINs held back until their quota replenishes; expiries the key's
	// expiry last recorded, by connection, so that the store is written on a change
	// alone.
	mu       sync.Mutex
	held     map[string]time.Time
	expiries map[string]time.Time
}

// New returns the Source reading through client, each connection's key read by
// credential (the store's), its expiry recorded in st.
func New(client *skoda.Client, credential func(ctx context.Context, accountID, connectionID string) (Credential, error),
	st Store, clk clock.Clock,
) *Source {
	return &Source{
		client: client, credential: credential, store: st, clk: clk,
		held: map[string]time.Time{}, expiries: map[string]time.Time{},
	}
}

// Spec implements collector.Source: the one endpoint, read again at once when a trip or
// a charge ends, or a charge starts.
func (*Source) Spec() collector.SourceSpec {
	return collector.SourceSpec{
		Name: skoda.Source,
		Endpoints: []collector.EndpointSpec{{
			Endpoint: collector.Endpoint(skoda.Vehicle), API: skoda.Source,
			Parked: collector.PaceParked, Driving: collector.PaceActive, Charging: collector.PaceActive,
		}},
		Settled:   []collector.Endpoint{collector.Endpoint(skoda.Vehicle)},
		Daily:     Daily,
		Intervals: Intervals(),
	}
}

// Login implements collector.Source: the connection's key, which reads as it is.
func (s *Source) Login(ctx context.Context, accountID, connectionID string) (collector.Session, time.Duration, error) {
	cred, err := s.credential(ctx, accountID, connectionID)
	if err != nil {
		return nil, 0, fmt.Errorf("skoda credential: %w", err)
	}
	return &session{src: s, cred: cred, accountID: accountID, connectionID: connectionID}, keyLife, nil
}

// HeldUntil implements collector.Limiter: until when the VIN's quota is spent.
func (s *Source) HeldUntil(vin string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held[vin]
}

// Observe implements collector.Source: the parking position tells whether the car
// drives, the charging state whether it charges.
func (*Source) Observe(_ collector.Endpoint, raw []byte, a *collector.Activity) error {
	s, err := skoda.Normalize(raw)
	if err != nil {
		return err //nolint:wrapcheck // skoda's, which says what failed
	}
	if s.Covers&core.FieldEngine != 0 && s.Engine.OK {
		a.Engine, a.Driving = string(s.Engine.V), s.Engine.V == core.EngineRunning
	}
	if s.Covers&core.FieldCharging != 0 && s.Charging.OK {
		a.Charging, a.Charges = string(s.Charging.V), s.Charging.V == core.ChargingActive
	}
	return nil
}

// Fingerprint implements collector.Source: the values and the parts' send times, the
// picture's link left out.
func (*Source) Fingerprint(raw []byte) ([]byte, error) {
	return skoda.Fingerprint(raw) //nolint:wrapcheck // skoda's, which says what failed
}

// Upload implements collector.Source.
func (*Source) Upload(_ collector.Endpoint, raw []byte) (collector.Upload, error) {
	u, err := skoda.UploadOf(raw)
	if err != nil {
		return collector.Upload{}, err //nolint:wrapcheck // skoda's, which says what failed
	}
	return collector.Upload{At: u.At, ModelYear: u.ModelYear, Fields: u.Fields}, nil
}

// limit holds the VIN back when its quota is down to the margin, until it replenishes.
func (s *Source) limit(vin string, l skoda.RateLimit) {
	if !l.Known {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.Remaining > reserve {
		delete(s.held, vin)
		return
	}
	s.held[vin] = s.clk.Now().Add(l.Reset)
}

// hold holds the VIN back for d, after a 429.
func (s *Source) hold(vin string, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if until := s.clk.Now().Add(d); until.After(s.held[vin]) {
		s.held[vin] = until
	}
}

// expire records the key's expiry when it changed.
func (s *Source) expire(ctx context.Context, sess *session, at time.Time) error {
	if at.IsZero() {
		return nil
	}
	s.mu.Lock()
	same := s.expiries[sess.connectionID].Equal(at)
	s.mu.Unlock()
	if same {
		return nil
	}
	if err := s.store.SetCredentialExpiry(ctx, sess.accountID, sess.connectionID, sess.cred.SetAt, at); err != nil {
		return fmt.Errorf("skoda key expiry: %w", err)
	}
	s.mu.Lock()
	s.expiries[sess.connectionID] = at
	s.mu.Unlock()
	return nil
}

// session reads with a connection's key. The collector uses it from one goroutine.
type session struct {
	src                     *Source
	cred                    Credential
	accountID, connectionID string
}

// Fetch implements collector.Session: the vehicle's parts the collector reads, its quota
// and the key's expiry kept.
func (s *session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	if skoda.Endpoint(ep) != skoda.Vehicle {
		return nil, fmt.Errorf("skoda: unknown endpoint %q", ep)
	}
	resp, err := s.src.client.Vehicle(ctx, s.cred.APIKey, vin, skoda.Parts()...)
	if err != nil {
		return nil, s.src.fetchError(vin, err)
	}
	s.src.limit(vin, resp.Limit)
	// The read stands should the expiry not be written: it is, at the next read.
	_ = s.src.expire(ctx, s, resp.ExpiresAt)
	return resp.Body, nil
}

// fetchError tells the collector what a refusal of the API means, and holds the VIN
// back on its quota (a 5xx counts in it, and tells it too).
func (s *Source) fetchError(vin string, err error) error {
	var e *skoda.APIError
	if !errors.As(err, &e) {
		return err
	}
	s.limit(vin, e.Limit)
	se := &collector.SourceError{Status: e.Status, RetryIn: e.RetryIn, Err: err}
	switch e.Kind {
	case skoda.KindKeyExpired, skoda.KindKeyRefused:
		se.Kind = collector.KindCredentialRefused
	case skoda.KindNotAuthorized, skoda.KindNotFound:
		se.Kind = collector.KindUnshared
	case skoda.KindRateLimited:
		s.hold(vin, e.RetryIn)
		se.Kind = collector.KindVehicleLimited
	case skoda.KindBadRequest, skoda.KindOther:
		se.Kind = collector.KindOther
	}
	return se
}
