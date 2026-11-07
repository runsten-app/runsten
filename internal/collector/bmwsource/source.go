// Package bmwsource reads BMW CarData for the collector (ADR 0045): it adapts the client
// of internal/bmw to collector.Source, which knows neither its transport nor its formats.
//
// A connection is the owner's client ID and the refresh token of its device flow. Each
// access is asked for with the refresh token, and the one BMW gives in exchange, a new one
// at every refresh, is kept before the access is used.
//
// The car pushes its changes to the stream: the source holds one subscription per
// connection, outside the sessions, reconnected with the ID token of each access. The
// collector reads what it received as the endpoint Stream, at its usual paces, without a
// call (collector.EndpointSpec.Local). The API, 50 calls a day, is the safety net: the
// container's values hourly, the basic data and the charging history daily.
package bmwsource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"runsten/internal/bmw"
	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
)

// Credential is what a connection holds of the owner's CarData access.
type Credential struct {
	ClientID string
	// GCID is the BMW account's: the stream's username.
	GCID    string
	Refresh string
	// ContainerID is Runsten's container in the account, named ContainerName; empty until
	// created.
	ContainerID, ContainerName string
	// SetAt is when the owner connected: a rotation replaces the refresh token of this
	// connection only, never one the owner set since.
	SetAt time.Time
}

// Store reads and keeps each connection's credential: the store's.
type Store interface {
	// BMWCredential returns the connection's credential.
	BMWCredential(ctx context.Context, accountID, connectionID string) (Credential, error)
	// RotateBMWToken replaces the refresh token of the credential set at setAt with token,
	// which a refresh gave; a credential the owner set since is left alone.
	RotateBMWToken(ctx context.Context, accountID, connectionID string, setAt time.Time, token string) error
	// SetBMWContainer records the container of the credential set at setAt.
	SetBMWContainer(ctx context.Context, accountID, connectionID string, setAt time.Time, containerID, name string) error
}

// Daily is the API's calls a day counted per connection: 40 of BMW's 50, the rest left to
// the owner (ADR 0045 §4).
const Daily = 40

// Intervals are the cadences (ADR 0045 §4): the stream's values every minute parked,
// every 15 s driving or charging; the container's values hourly; the basic data and the
// charging history daily.
func Intervals() collector.Intervals {
	return collector.Intervals{Parked: time.Minute, Active: 15 * time.Second, Rare: time.Hour, Details: 24 * time.Hour}
}

const (
	// accessMargin is taken off an access's lifetime: the next is asked for, and the
	// stream reconnected with its ID token, before BMW closes the stream at the old one's
	// end.
	accessMargin = 4 * time.Minute
	// idleClose is how long a subscription no session read stays open: the connection
	// removed, the flag turned off, the account gone.
	idleClose = 10 * time.Minute
	// streamRetry is how long after a connection that failed the stream is tried again;
	// heldRetry after one another client holds (Home Assistant, evcc).
	streamRetry = time.Minute
	heldRetry   = 5 * time.Minute
	// historyDays is how far back the charging history is read: BMW keeps about 30 days.
	historyDays = 30
)

// Source is CarData as a collector.Source.
type Source struct {
	client    *bmw.Client
	store     Store
	clk       clock.Clock
	streamURL string

	mu sync.Mutex
	// rotated are the refresh tokens a refresh gave that could not be kept yet, by
	// connection.
	rotated map[string]rotation
	// subs are the subscriptions to the stream, by connection.
	subs map[string]*subscription
	// spent is when each connection may call the API again, its day's calls spent.
	spent map[string]time.Time
	// containerWait is when each connection may try again to set up its container, after
	// a failure.
	containerWait map[string]time.Time
}

// rotation is a refresh token given in exchange for the one of the credential set at
// setAt.
type rotation struct {
	setAt time.Time
	token string
}

// subscription is a connection's subscription to the stream, and the ID token the last
// access gave.
type subscription struct {
	*bmw.Subscription
	idToken string
	readAt  time.Time // when a session last read it
	retryAt time.Time // when to try connecting again, after a failure
	tried   string    // the ID token of the last failure
}

// New returns the Source reading through client and the stream at streamURL
// (bmw.DefaultStreamURL), with each connection's credential kept in st.
func New(client *bmw.Client, streamURL string, st Store, clk clock.Clock) *Source {
	return &Source{
		client: client, store: st, clk: clk, streamURL: streamURL,
		rotated: map[string]rotation{}, subs: map[string]*subscription{}, spent: map[string]time.Time{},
		containerWait: map[string]time.Time{},
	}
}

// Spec implements collector.Source.
func (*Source) Spec() collector.SourceSpec {
	return collector.SourceSpec{
		Name: bmw.Source,
		Endpoints: []collector.EndpointSpec{
			// What the stream received: it tells the mode, read first.
			{
				Endpoint: collector.Endpoint(bmw.Stream), API: bmw.Source, Local: true,
				Parked: collector.PaceParked, Driving: collector.PaceActive, Charging: collector.PaceActive,
			},
			// The safety net: a value the stream did not push, the state before its first
			// message after a restart.
			{
				Endpoint: collector.Endpoint(bmw.TelematicData), API: bmw.Source,
				Parked: collector.PaceRare, Driving: collector.PaceRare, Charging: collector.PaceRare,
			},
			{
				Endpoint: collector.Endpoint(bmw.BasicData), API: bmw.Source,
				Parked: collector.PaceDetails, Driving: collector.PaceDetails, Charging: collector.PaceDetails,
			},
			// The charging sessions of the last 30 days, with the energy each drew from the
			// grid, which the charges take (ADR 0045 §5); optional.
			{
				Endpoint: collector.Endpoint(bmw.ChargingHistory), API: bmw.Source, Optional: true,
				Parked: collector.PaceDetails, Driving: collector.PaceDetails, Charging: collector.PaceDetails,
			},
			// The vehicle's picture, served by runsten-api: a call a day, an identical one
			// stored once; optional, a vehicle may have none.
			{
				Endpoint: collector.Endpoint(bmw.Image), API: bmw.Source, Optional: true,
				Parked: collector.PaceDetails, Driving: collector.PaceDetails, Charging: collector.PaceDetails,
			},
		},
		Settled:   []collector.Endpoint{collector.Endpoint(bmw.Stream)},
		Daily:     Daily,
		Intervals: Intervals(),
	}
}

// Login implements collector.Source: an access of the connection's refresh token, the
// refresh token given in exchange kept first. The stream is reconnected with its ID token
// at the next read. It makes no call of the API, whose calls are counted.
func (s *Source) Login(ctx context.Context, accountID, connectionID string) (collector.Session, time.Duration, error) {
	// Its own subscription is kept, idle or not: given the new ID token, it closes its
	// connection before opening the next, and waits for the broker to free the slot.
	s.sweep(connectionID)
	cred, err := s.store.BMWCredential(ctx, accountID, connectionID)
	if err != nil {
		return nil, 0, fmt.Errorf("bmw credential: %w", err)
	}
	tok, err := s.client.Refresh(ctx, cred.ClientID, s.refreshToken(connectionID, cred.Refresh, cred.SetAt))
	if err != nil {
		return nil, 0, sourceError(err)
	}
	if tok.Refresh != cred.Refresh {
		s.keep(connectionID, rotation{setAt: cred.SetAt, token: tok.Refresh})
		if err := s.store.RotateBMWToken(ctx, accountID, connectionID, cred.SetAt, tok.Refresh); err != nil {
			return nil, 0, fmt.Errorf("keep the bmw refresh token given: %w", err)
		}
		s.keep(connectionID, rotation{})
	}
	gcid := tok.GCID
	if gcid == "" {
		gcid = cred.GCID
	}
	s.subscribe(connectionID, gcid, tok.ID)
	sess := &session{src: s, accountID: accountID, connectionID: connectionID, token: tok.Access, setAt: cred.SetAt}
	if cred.ContainerName == bmw.ContainerName() {
		// A container of a previous release lacks descriptors: the first read of the API
		// replaces it.
		sess.container = cred.ContainerID
	}
	ttl := tok.ExpiresIn - accessMargin
	if ttl <= 0 {
		ttl = tok.ExpiresIn
	}
	return sess, ttl, nil
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

// subscribe gives the connection's subscription the ID token of a new access, creating
// it, or replacing one of another account.
func (s *Source) subscribe(connectionID, gcid, idToken string) {
	s.mu.Lock()
	sub := s.subs[connectionID]
	var old *bmw.Subscription
	if sub != nil && sub.GCID() != gcid {
		old, sub = sub.Subscription, nil
	}
	if sub == nil {
		sub = &subscription{Subscription: bmw.NewSubscription(s.streamURL, gcid)}
		s.subs[connectionID] = sub
	}
	sub.idToken, sub.readAt = idToken, s.clk.Now()
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

// sweep closes the subscriptions no session read for idleClose, but the connection
// keep's.
func (s *Source) sweep(keep string) {
	now := s.clk.Now()
	var idle []*bmw.Subscription
	s.mu.Lock()
	for id, sub := range s.subs {
		if id != keep && now.Sub(sub.readAt) >= idleClose {
			idle = append(idle, sub.Subscription)
			delete(s.subs, id)
		}
	}
	s.mu.Unlock()
	for _, sub := range idle {
		sub.Close()
	}
}

// Run closes the subscriptions left unread, every minute, and all of them when ctx is
// done.
func (s *Source) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			subs := s.subs
			s.subs = map[string]*subscription{}
			s.mu.Unlock()
			for _, sub := range subs {
				sub.Close()
			}
			return
		case <-t.C:
			s.sweep("")
		}
	}
}

// StreamState is what the connection page tells of a connection's stream.
type StreamState struct {
	Up bool
	// Held: another client holds the account's only connection to the stream (Home
	// Assistant, evcc).
	Held bool
	Err  error // why it is down; nil while up
}

// StreamState returns the state of the connection's stream; zero when the source holds
// no subscription for it.
func (s *Source) StreamState(connectionID string) StreamState {
	s.mu.Lock()
	sub := s.subs[connectionID]
	s.mu.Unlock()
	if sub == nil {
		return StreamState{}
	}
	up, err := sub.Up()
	return StreamState{Up: up, Held: !up && held(err), Err: err}
}

// Observe implements collector.Source: the stream's values and the container's tell
// whether the car drives and charges.
func (*Source) Observe(ep collector.Endpoint, raw []byte, a *collector.Activity) error {
	e := bmw.Endpoint(ep)
	if e != bmw.Stream && e != bmw.TelematicData {
		return nil
	}
	s, err := bmw.Normalize(e, raw)
	if err != nil {
		return err //nolint:wrapcheck // bmw's, which says what failed
	}
	if s.Covers&core.FieldEngine != 0 && s.Engine.OK {
		a.Engine, a.Driving = string(s.Engine.V), s.Engine.V == core.EngineRunning
	}
	if s.Covers&core.FieldCharging != 0 && s.Charging.OK {
		a.Charging, a.Charges = string(s.Charging.V), s.Charging.V == core.ChargingActive
	}
	return nil
}

// Fingerprint implements collector.Source.
func (*Source) Fingerprint(raw []byte) ([]byte, error) {
	return bmw.Fingerprint(raw) //nolint:wrapcheck // bmw's, which says what failed
}

// Upload implements collector.Source.
func (*Source) Upload(ep collector.Endpoint, raw []byte) (collector.Upload, error) {
	u, err := bmw.UploadOf(bmw.Endpoint(ep), raw)
	if err != nil {
		return collector.Upload{}, err //nolint:wrapcheck // bmw's, which says what failed
	}
	return collector.Upload{At: u.At, Fields: u.Fields}, nil
}

// session reads with an access token. The collector uses it from one goroutine.
type session struct {
	src                     *Source
	accountID, connectionID string
	token                   string
	container               string
	setAt                   time.Time
}

// errSpent: the day's calls of the API are spent, until 00:00 UTC.
var errSpent = errors.New("bmw: the day's calls of the API are spent")

// errContainerWait: setting up the container failed today, tried again from 00:00 UTC.
var errContainerWait = errors.New("bmw: the container could not be set up today")

// Fetch implements collector.Session.
func (s *session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	e := bmw.Endpoint(ep)
	if e == bmw.Stream {
		return s.stream(ctx, vin)
	}
	now := s.src.clk.Now()
	s.src.mu.Lock()
	until := s.src.spent[s.connectionID]
	s.src.mu.Unlock()
	if now.Before(until) {
		// Not the connection's failure: the stream reads on.
		return nil, &collector.SourceError{Kind: collector.KindNotAvailable, Err: errSpent}
	}
	var raw []byte
	var err error
	switch e {
	case bmw.TelematicData:
		raw, err = s.telematicData(ctx, vin)
	case bmw.BasicData:
		raw, err = s.src.client.BasicData(ctx, s.token, vin)
	case bmw.ChargingHistory:
		raw, err = s.src.client.ChargingHistory(ctx, s.token, vin, now.AddDate(0, 0, -historyDays), now)
	case bmw.Image:
		raw, err = s.src.client.Image(ctx, s.token, vin)
	default:
		return nil, fmt.Errorf("bmw: unknown endpoint %q", ep)
	}
	if err != nil {
		return nil, s.apiError(err, now)
	}
	return raw, nil
}

// stream returns what the stream received of the vehicle, connecting first when it is
// down or its ID token is not the access's: no call of the API.
func (s *session) stream(ctx context.Context, vin string) ([]byte, error) {
	now := s.src.clk.Now()
	s.src.mu.Lock()
	sub := s.src.subs[s.connectionID]
	if sub == nil { // swept since the login: the next access subscribes again
		s.src.mu.Unlock()
		return nil, &collector.SourceError{Kind: collector.KindUnauthorized, Err: errors.New("bmw: no subscription to the stream")}
	}
	sub.readAt = now
	idToken := sub.idToken
	try := sub.tried != idToken || !now.Before(sub.retryAt)
	s.src.mu.Unlock()
	if !try {
		// Waiting to try again: a stream another client holds is told until then, rather
		// than the values it left.
		if up, lost := sub.Up(); !up && held(lost) {
			return nil, &collector.SourceError{Kind: collector.KindStreamHeld, Err: lost}
		}
		return sub.Values(vin), nil
	}
	err := sub.Connect(ctx, idToken)
	if err == nil {
		return sub.Values(vin), nil
	}
	retry := streamRetry
	if held(err) {
		retry = heldRetry
	}
	s.src.mu.Lock()
	sub.tried, sub.retryAt = idToken, now.Add(retry)
	s.src.mu.Unlock()
	var se *bmw.StreamError
	switch {
	case errors.As(err, &se) && se.Code == bmw.RefusedBadPassword:
		// The ID token refused, or expired: a new access gives another.
		return nil, &collector.SourceError{Kind: collector.KindUnauthorized, Err: err}
	case held(err):
		return nil, &collector.SourceError{Kind: collector.KindStreamHeld, Err: err}
	}
	return nil, err //nolint:wrapcheck // bmw's, which says what failed
}

// held reports whether err is the stream's refusal of a second connection: another
// client holds the account's only one.
func held(err error) bool {
	var se *bmw.StreamError
	return errors.As(err, &se) && se.Code == bmw.RefusedNotAuthorized
}

// telematicData reads the container's values, creating Runsten's container first when
// the connection has none or one of a previous release, or again when the owner deleted
// it.
func (s *session) telematicData(ctx context.Context, vin string) ([]byte, error) {
	if s.container == "" {
		if err := s.ensureContainer(ctx); err != nil {
			return nil, err
		}
	}
	raw, err := s.src.client.TelematicData(ctx, s.token, vin, s.container)
	var e *bmw.APIError
	if errors.As(err, &e) && e.Kind == bmw.KindNotFound {
		if err := s.ensureContainer(ctx); err != nil {
			return nil, err
		}
		raw, err = s.src.client.TelematicData(ctx, s.token, vin, s.container)
	}
	return raw, err //nolint:wrapcheck // bmw's, which says what failed
}

// ensureContainer sets up the connection's container. It makes up to 3 calls (the list,
// a deletion, the creation) that the collector counts as one: one that failed is not
// tried again before the next day of BMW's quota, 00:00 UTC, so that its retries never
// spend the day's calls unseen; the stream reads on. An access refused is not its
// failure: the next access tries again.
func (s *session) ensureContainer(ctx context.Context) error {
	now := s.src.clk.Now()
	s.src.mu.Lock()
	wait := s.src.containerWait[s.connectionID]
	s.src.mu.Unlock()
	if now.Before(wait) {
		return &collector.SourceError{Kind: collector.KindNotAvailable, Err: errContainerWait}
	}
	ct, err := s.src.client.EnsureContainer(ctx, s.token)
	if err != nil {
		var e *bmw.APIError
		if !errors.As(err, &e) || e.Kind != bmw.KindUnauthorized {
			y, m, d := now.UTC().Date()
			s.src.mu.Lock()
			s.src.containerWait[s.connectionID] = time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
			s.src.mu.Unlock()
		}
		return err //nolint:wrapcheck // bmw's, which says what failed
	}
	s.src.mu.Lock()
	delete(s.src.containerWait, s.connectionID)
	s.src.mu.Unlock()
	s.container = ct.ID
	if err := s.src.store.SetBMWContainer(ctx, s.accountID, s.connectionID, s.setAt, ct.ID, ct.Name); err != nil {
		return fmt.Errorf("keep the bmw container: %w", err)
	}
	return nil
}

// apiError tells the collector what a refusal of the API means. The day's calls spent,
// the API is left alone until 00:00 UTC, and the stream reads on.
func (s *session) apiError(err error, now time.Time) error {
	var pe *bmw.PictureError
	if errors.As(err, &pe) {
		// BMW answered, without a picture kept: the endpoint's, tried again later, no
		// failure told.
		return &collector.SourceError{Kind: collector.KindNotAvailable, Status: pe.Status, Err: err}
	}
	var e *bmw.APIError
	if errors.As(err, &e) && e.Kind == bmw.KindRateLimited {
		y, m, d := now.UTC().Date()
		s.src.mu.Lock()
		s.src.spent[s.connectionID] = time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
		s.src.mu.Unlock()
		return &collector.SourceError{Kind: collector.KindNotAvailable, Status: e.Status, Err: err}
	}
	return sourceError(err)
}

// sourceError tells the collector what a refusal of CarData means.
func sourceError(err error) error {
	var e *bmw.APIError
	if !errors.As(err, &e) {
		return err
	}
	se := &collector.SourceError{Kind: collector.KindOther, Status: e.Status, Err: err}
	switch e.Kind {
	case bmw.KindTokenRefused, bmw.KindClientRefused, bmw.KindScopeMissing:
		se.Kind = collector.KindCredentialRefused
	case bmw.KindUnauthorized:
		se.Kind = collector.KindUnauthorized
	case bmw.KindUnshared:
		se.Kind = collector.KindUnshared
	case bmw.KindRateLimited:
		se.Kind = collector.KindNotAvailable
	case bmw.KindOther, bmw.KindPending, bmw.KindSlowDown, bmw.KindExpired, bmw.KindDenied, bmw.KindNotFound:
	}
	return se
}
