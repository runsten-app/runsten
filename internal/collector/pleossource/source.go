// Package pleossource reads the Pleos Vehicle Data API for the collector: it adapts
// the client of internal/pleos to collector.Source, which knows neither its transport
// nor its formats.
//
// A connection is the owner's key of one brand, and the brand its connection provider
// names. The key's access token is short and never stored: asked for with the key, it
// is kept until five minutes before its end (the JWT's claim tells it), and a refusal
// asks for another, once. Pleos keeps one token per key at a time: a refused token
// another has taken the place of tell that another client reads with the same key
// (evcc, Home Assistant), which the collector tells the user as key_shared.
package pleossource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/pleos"
)

// Credential is what a connection holds of the owner's key: its two values, and the
// brand the connection's provider reads, which rides in every call's header.
type Credential struct {
	ClientID, ClientSecret string
	Brand                  pleos.Brand
}

// Daily is the calls a day granted each key (API pleos in api_calls). None is
// published (the term forbid raising the limits, which says there are): 2,000 is
// prudent, a parked car making about 360 and a day on the road about 550 more
// (assumption, ADR 0049 §4).
const Daily = 2_000

const (
	// tokenMargin is taken off a token's lifetime: another is asked for before this
	// one ends, so that no call meets its refusal (assumption, ADR 0049 §3: five
	// minutes, an hour's lifetime seen once).
	tokenMargin = 5 * time.Minute
	// defaultTokenTTL is kept when the JWT's expiry is unreadable: fifty-five minutes,
	// the observed hour less the margin.
	defaultTokenTTL = 55 * time.Minute
	// keySharedWindow is how far back the refusals of live tokens tell of another
	// client on the same key, and keySharedAfter how many of them: three in the hour
	// (assumption, ADR 0049 §3, one user's observation behind it). keySharedWait is
	// how long the connection waits before it reads again.
	keySharedWindow = time.Hour
	keySharedAfter  = 3
	keySharedWait   = 15 * time.Minute
)

// Intervals are the source's cadences (ADR 0049 §4), bounded by the car's sends: the
// API returns the last state the vehicle sent and never wakes it, so reading faster
// than it sends only spends calls. Parked, ten minutes; driving or charging, two;
// the rest, an hour; the consent, a day.
func Intervals() collector.Intervals {
	return collector.Intervals{Parked: 10 * time.Minute, Active: 2 * time.Minute, Rare: time.Hour, Details: 24 * time.Hour}
}

// Source is the Pleos Vehicle Data API as a collector.Source, read through the
// connections of each provider of pleos.Providers: the three brands, each with its own
// key.
type Source struct {
	client     *pleos.Client
	credential func(ctx context.Context, accountID, connectionID string) (Credential, error)
	clk        clock.Clock

	// refusals are the times a live token was refused, by connection: one token
	// living per key, a refusal another client took is the same key's. keys are the
	// client IDs they were counted for: another key starts from none.
	mu       sync.Mutex
	refusals map[string][]time.Time
	keys     map[string]string
}

// New returns the Source reading through client, each connection's key read by
// credential (the store's).
func New(client *pleos.Client, credential func(ctx context.Context, accountID, connectionID string) (Credential, error), clk clock.Clock) *Source {
	return &Source{client: client, credential: credential, clk: clk, refusals: map[string][]time.Time{}, keys: map[string]string{}}
}

// Spec implements collector.Source. The mode is read first (driving tells the engine,
// batteries the charge), so that a transition brings the readings of its end within
// the same pass; powertrains, locations and status are optional: a model the API's
// compatibility matrix says tells none of them, and their 4045 is told to no one.
func (*Source) Spec() collector.SourceSpec {
	ep := func(e pleos.Endpoint, parked, driving, charging collector.Pace, optional bool) collector.EndpointSpec {
		return collector.EndpointSpec{
			Endpoint: collector.Endpoint(e), API: pleos.Source,
			Parked: parked, Driving: driving, Charging: charging, Optional: optional,
		}
	}
	return collector.SourceSpec{
		Name:      pleos.Source,
		Providers: providers(),
		Endpoints: []collector.EndpointSpec{
			ep(pleos.Driving, collector.PaceParked, collector.PaceActive, collector.PaceActive, false),
			ep(pleos.Batteries, collector.PaceParked, collector.PaceActive, collector.PaceActive, false),
			ep(pleos.Powertrains, collector.PaceRare, collector.PaceParked, collector.PaceParked, false),
			ep(pleos.Locations, collector.PaceRare, collector.PaceActive, collector.PaceActive, true),
			ep(pleos.Status, collector.PaceRare, collector.PaceRare, collector.PaceRare, true),
			ep(pleos.Consent, collector.PaceDetails, collector.PaceDetails, collector.PaceDetails, false),
		},
		// The contact, the position and the range are read at once when a trip or a
		// charge ends, or a charge starts.
		Settled: []collector.Endpoint{
			collector.Endpoint(pleos.Driving), collector.Endpoint(pleos.Locations),
			collector.Endpoint(pleos.Powertrains),
		},
		Daily:     Daily,
		Intervals: Intervals(),
	}
}

// providers lists the connection providers the source reads: one per brand, each with
// its own key and its own refusal.
func providers() []string {
	out := make([]string, 0, len(pleos.Brands()))
	for _, b := range pleos.Brands() {
		out = append(out, b.Provider())
	}
	return out
}

// Login implements collector.Source: a token of the connection's key, kept until five
// minutes before its end. The brand is the connection's, named by its provider.
func (s *Source) Login(ctx context.Context, accountID, connectionID string) (collector.Session, time.Duration, error) {
	cred, err := s.credential(ctx, accountID, connectionID)
	if err != nil {
		return nil, 0, fmt.Errorf("pleos credential: %w", err)
	}
	s.mu.Lock()
	if s.keys[connectionID] != cred.ClientID {
		// Another key: the refusals of the previous one tell nothing of it.
		s.keys[connectionID] = cred.ClientID
		delete(s.refusals, connectionID)
	}
	s.mu.Unlock()
	tok, err := s.client.Token(ctx, cred.Brand, pleos.Credential{ClientID: cred.ClientID, ClientSecret: cred.ClientSecret})
	if err != nil {
		return nil, 0, sourceError(err)
	}
	// The lifetime the token's JWT tells; without it, or with an end already past
	// (a clock apart), the default the observation leaves. A token shorter than the
	// margin is kept its whole life: its refusals past it are not a live token's.
	ttl := defaultTokenTTL
	if !tok.ExpiresAt.IsZero() {
		switch life := tok.ExpiresAt.Sub(s.clk.Now()); {
		case life > tokenMargin:
			ttl = life - tokenMargin
		case life > 0:
			ttl = life
		}
	}
	return &session{
		src: s, token: tok.Value, brand: cred.Brand, until: s.clk.Now().Add(ttl), connectionID: connectionID,
	}, ttl, nil
}

// Observe implements collector.Source: the ignition tells whether the car drives, the
// batteries whether it charges.
func (*Source) Observe(ep collector.Endpoint, raw []byte, a *collector.Activity) error {
	s, err := pleos.Normalize(pleos.Endpoint(ep), raw)
	if err != nil {
		return err //nolint:wrapcheck // pleos's, which names the endpoint
	}
	if s.Covers&core.FieldEngine != 0 && s.Engine.OK {
		a.Engine, a.Driving = string(s.Engine.V), s.Engine.V == core.EngineRunning
	}
	if s.Covers&core.FieldCharging != 0 && s.Charging.OK {
		a.Charging, a.Charges = string(s.Charging.V), s.Charging.V == core.ChargingActive
	}
	return nil
}

// Fingerprint implements collector.Source: the values alone, the envelope and the
// car's send times left out. An unchanged send only extends how long it is known to
// hold.
func (*Source) Fingerprint(raw []byte) ([]byte, error) {
	return pleos.Fingerprint(raw) //nolint:wrapcheck // pleos's, which says what failed
}

// Upload implements collector.Source.
func (*Source) Upload(_ collector.Endpoint, raw []byte) (collector.Upload, error) {
	u, err := pleos.UploadOf(raw)
	if err != nil {
		return collector.Upload{}, err //nolint:wrapcheck // pleos's, which says what failed
	}
	return collector.Upload{At: u.At, Fields: u.Fields}, nil
}

// session reads with a token of a connection's key, kept until five minutes before
// its end. The collector uses it from one goroutine.
type session struct {
	src          *Source
	token        string
	brand        pleos.Brand
	until        time.Time // when the token is dropped for another
	connectionID string
}

// Fetch implements collector.Session. The consent, which a snapshot of its own is kept
// for each vehicle, is kept to the entry of the vehicle read: the client decides which
// one is whose, the normalizer knowing no vehicle.
func (s *session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	e := pleos.Endpoint(ep)
	var raw []byte
	var err error
	if e == pleos.Consent {
		raw, err = s.src.client.Consent(ctx, s.token, s.brand)
		if err == nil {
			raw, err = pleos.VehicleOfConsent(raw, vin)
		}
	} else {
		raw, err = s.src.client.Fetch(ctx, s.token, s.brand, vin, e)
	}
	if err != nil {
		return nil, s.src.fetchError(err, s)
	}
	return raw, nil
}

// fetchError tells the collector what a refusal means. A live token refused — one the
// JWT has living past the refusal — counts toward the key others read, Pleos keeping
// one token per key: counted three times within the hour, the connection waits as
// long, and the user is told (key_shared).
func (s *Source) fetchError(err error, sess *session) error {
	var e *pleos.APIError
	if !errors.As(err, &e) || e.Kind != pleos.KindUnauthorized {
		return sourceError(err)
	}
	now := s.clk.Now()
	if now.After(sess.until) {
		// The token's time was up: a new one, nothing shared.
		return sourceError(err)
	}
	s.mu.Lock()
	from := now.Add(-keySharedWindow)
	kept := make([]time.Time, 0, len(s.refusals[sess.connectionID])+1)
	for _, t := range s.refusals[sess.connectionID] {
		if t.After(from) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	s.refusals[sess.connectionID] = kept
	shared := len(kept) >= keySharedAfter
	s.mu.Unlock()
	if !shared {
		return sourceError(err)
	}
	return &collector.SourceError{Kind: collector.KindKeyShared, Status: e.Status, RetryIn: keySharedWait, Err: err}
}

// sourceError tells the collector what a refusal of the API means.
func sourceError(err error) error {
	var e *pleos.APIError
	if !errors.As(err, &e) {
		return err
	}
	se := &collector.SourceError{Status: e.Status, RetryIn: e.RetryIn, Err: err}
	switch e.Kind {
	case pleos.KindCredentialRefused:
		se.Kind = collector.KindCredentialRefused
	case pleos.KindUnauthorized:
		se.Kind = collector.KindUnauthorized
	case pleos.KindUnshared:
		se.Kind = collector.KindUnshared
	case pleos.KindNotAvailable:
		se.Kind = collector.KindNotAvailable
	case pleos.KindRateLimited:
		se.Kind = collector.KindRateLimited
	case pleos.KindOther:
		se.Kind = collector.KindOther
	}
	return se
}
