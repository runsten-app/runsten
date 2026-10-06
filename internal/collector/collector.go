// Package collector polls the vehicle API for every account and stores the deduplicated
// raw responses. After each pass over a vehicle, it has the derived events updated.
//
// Polling is adaptive: the vehicle state (parked, driving, charging), inferred from the
// latest responses, sets the interval of each endpoint. The end of a trip or of a
// charge triggers an immediate reading of the location, statistics and odometer.
//
// Access tokens come from a TokenSource, asked once per pass and vehicle. A rejected
// token is renewed once; a connection that lost its grant is no longer polled.
//
// Volvo counts its quota on the application key: a connection reads with the
// instance's, which every account shares, or, on an instance without one (the hosted
// offer), with its own. A budget spreads each key's quota over the day and, on the
// instance's key, when calls must wait, the accounts that called the least lately go
// first.
//
// An extension's Policy may read an account's vehicles less often, without an active
// mode, or only some of them, or not publish their state: the hosted offer's free plan.
//
// A Publisher sends the state of each vehicle the pass read on to its account's broker;
// a publication that fails never stops the reads.
package collector

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/volvo"
)

// Target is a vehicle to poll.
type Target struct {
	AccountID    string
	VehicleID    string
	VIN          string
	ConnectionID string
	// ReauthReason is set when the connection lost its grant: the vehicle is not
	// polled until the user authorizes again.
	ReauthReason string
	// KeySetAt is when the connection's own application key was set; zero: it has
	// none, and reads with the instance's. The key itself is read separately
	// (ConnectionKey), only when a call is due.
	KeySetAt time.Time
	// KeyRefused is set when the vendor refused the connection's own key: the vehicle
	// is not polled until the user sets another.
	KeyRefused bool
}

// ownKey reports whether the target's connection reads with its own application key.
func (t Target) ownKey() bool { return !t.KeySetAt.IsZero() }

// Snapshot is a raw response to store.
type Snapshot struct {
	AccountID string
	VehicleID string
	Endpoint  volvo.Endpoint
	FetchedAt time.Time
	Payload   []byte
	Hash      []byte // fingerprint of the values, timestamps excluded
}

// API queries the provider, with an application key (empty: the instance's) and a
// user's token.
type API interface {
	Fetch(ctx context.Context, key, token, vin string, ep volvo.Endpoint) ([]byte, error)
}

// Store reads the vehicles to poll and stores the snapshots.
type Store interface {
	Targets(ctx context.Context) ([]Target, error)
	// SaveSnapshot stores s if its values differ from the previous snapshot of the same
	// endpoint; otherwise it only records the check time. Returns true if stored.
	SaveSnapshot(ctx context.Context, s Snapshot) (bool, error)
	// SaveStatus replaces the vehicle's status, but for a zero ReadAt or Failure, which
	// keep those saved before: a restarted collector knows neither.
	SaveStatus(ctx context.Context, s Status) error
	// SaveCalls adds calls to those counted per account, API and hour.
	SaveCalls(ctx context.Context, calls []Calls) error
	// Calls returns the calls of every account counted since a time.
	Calls(ctx context.Context, since time.Time) ([]Calls, error)
	// ConnectionKey returns the connection's own application key; empty: none.
	ConnectionKey(ctx context.Context, accountID, connectionID string) (string, error)
	// SetKeyRefused records whether the vendor refused the connection's own key set at
	// setAt; a key set since is left alone.
	SetKeyRefused(ctx context.Context, accountID, connectionID string, setAt time.Time, refused bool) error
}

// TokenSource provides the access tokens of the connections. An error implementing
// ReauthRequired() bool (returning true) means that the connection lost its grant.
type TokenSource interface {
	// Token returns a valid access token, refreshed first if it is about to expire.
	Token(ctx context.Context, accountID, connectionID string) (string, error)
	// Refresh is called after the API rejected the token rejected: it renews the
	// connection, unless another refresher already did, and returns the new token.
	Refresh(ctx context.Context, accountID, connectionID, rejected string) (string, error)
	// KeepAlive refreshes the connections nobody used lately, so that their refresh
	// token does not lapse while their vehicles are not polled.
	KeepAlive(ctx context.Context) error
}

// Deriver updates the events derived from a vehicle's snapshots.
type Deriver interface {
	Update(ctx context.Context, accountID, vehicleID string) error
}

// Publisher sends the vehicles' state on, to the broker each account set (MQTT). Without
// one for an account, it does nothing. Its errors are logged, never fatal: they name
// the account and a kind of failure, never the broker's message nor a secret.
type Publisher interface {
	// Sync is called at the start of each pass: it opens, keeps or closes each account's
	// connection, as its configuration was set, changed or removed. vehicles are each
	// account's, by ID, those the policy leaves unread included: one gone from the
	// account is removed from its broker. The accounts in withheld publish nothing:
	// theirs is closed.
	Sync(ctx context.Context, vehicles map[string][]string, withheld map[string]bool) error
	// Publish sends the vehicle's current state, after a pass that saved a snapshot of it.
	Publish(ctx context.Context, accountID, vehicleID string) error
}

// Policy limits how the accounts' vehicles are read: an extension's, such as the hosted
// offer's free plan. Without one, every vehicle of every account is read at the
// Intervals.
type Policy interface {
	// Reads returns, per account, how its vehicles are read as of at (the collector's
	// time). An account left out has all its vehicles read at the Intervals.
	Reads(ctx context.Context, at time.Time) (map[string]Reads, error)
}

// Reads is how a Policy reads an account's vehicles.
type Reads struct {
	// Every is how often they are read at most, without an active mode then: neither
	// driving nor charging reads them sooner. Zero: at the Intervals.
	Every time.Duration
	// Only, unless nil, are the only vehicles of the account read, by ID: the others
	// are not, and what was read of them stays.
	Only []string
	// NoPublish withholds the account's publication: the Publisher closes its
	// connection, and is not asked to publish its vehicles.
	NoPublish bool
}

// reads reports whether the policy lets t's vehicle be read.
func (r Reads) reads(t Target) bool { return r.Only == nil || slices.Contains(r.Only, t.VehicleID) }

// Observer receives what the collector does, for debugging. Its methods are called
// from the collector goroutine and must not block.
type Observer interface {
	// Called is invoked after each API call.
	Called(Call)
	// Observed is invoked after each pass over a vehicle.
	Observed(VehicleState)
}

// Call describes an API call and its outcome. It never contains the token nor the
// application key.
type Call struct {
	At        time.Time
	Duration  time.Duration
	AccountID string
	VehicleID string
	VIN       string
	Endpoint  volvo.Endpoint
	Status    int    // 200, HTTP status of the error, or 0 if the API did not respond
	Err       string // empty if the call succeeded
	Stored    bool   // new response, stored; false for a duplicate
	Payload   []byte // raw response, if the call succeeded
}

// VehicleState is the collector state for a vehicle.
type VehicleState struct {
	At          time.Time
	AccountID   string
	VehicleID   string
	VIN         string
	Mode        string
	Engine      string
	Charging    string
	Next        map[volvo.Endpoint]time.Time
	PausedUntil time.Time            // rate limit or token rejected
	APIBlocked  map[string]time.Time // quota exhausted, per API, of the key the vehicle reads with
	// ReauthReason is set when the connection lost its grant: nothing is polled.
	ReauthReason string
	// Key is the application key the vehicle reads with: "own", "instance", "refused"
	// (its own, refused) or "missing" (none, and the instance has none).
	Key string
}

// Status is what the collector tells the user of a vehicle, after a pass: whether it
// runs, and why nothing is read, if so. Unlike VehicleState, it is stored.
type Status struct {
	AccountID string
	VehicleID string
	PassedAt  time.Time
	Mode      string
	ReadAt    time.Time // the latest successful call; zero: none since the collector started
	// NextAt is the earliest call due, blocks and pauses included; zero while the
	// connection lost its grant, or has no key it may read with: nothing is due until
	// the user acts.
	NextAt      time.Time
	PausedUntil time.Time            // zero: not paused
	Quota       map[string]time.Time // APIs whose quota is exhausted, until when
	Failure     Failure              // the latest failed call; zero: none since the collector started
}

// Failure is a failed call, as the user may be told of it: never its message, which
// holds the VIN of a URL, or the vendor's words.
type Failure struct {
	At       time.Time
	Endpoint volvo.Endpoint // empty when no call was made: no access token
	Status   int            // HTTP status, or 0 if the API did not respond
	Kind     string         // one of the Fail constants
}

// The kinds of a Failure.
const (
	FailQuota       = "quota"        // quota exhausted: the API is blocked for a while
	FailRateLimited = "rate_limited" // 429: the account is paused
	FailRejected    = "unauthorized" // token rejected again after a refresh
	FailNotFound    = "not_found"    // unknown VIN, or removed from the account
	FailUnavailable = "unavailable"  // no response, or a server error
	FailToken       = "token"        // no access token, the grant still held (Volvo ID unreachable)
	FailKeyRefused  = "key_refused"  // the application key refused, not the token
	FailOther       = "other"
)

// statusEvery is how often an unchanged status is written again: its PassedAt tells
// the user that the collector runs, without a write on every pass.
const statusEvery = time.Minute

// keyRetry is how long a refused key is not tried again. The instance's key is only
// replaced with a restart, but a refusal that was not the key's (see volvo.KindKeyRefused)
// should not stop the reads for good; a connection's own key stays refused until the
// user sets another.
const keyRetry = time.Hour

type nopObserver struct{}

func (nopObserver) Called(Call)           {}
func (nopObserver) Observed(VehicleState) {}

// Intervals tunes the polling.
type Intervals struct {
	Parked  time.Duration // parked vehicle: charging and engine state
	Active  time.Duration // driving or charging
	Rare    time.Duration // location, odometer, diagnostics and the like outside transitions
	Details time.Duration // vehicle details (battery capacity), and an optional endpoint refused
}

// DefaultIntervals returns the default polling intervals.
func DefaultIntervals() Intervals {
	return Intervals{Parked: 10 * time.Minute, Active: time.Minute, Rare: time.Hour, Details: 24 * time.Hour}
}

type mode int

const (
	parked mode = iota
	driving
	charging
)

func (m mode) String() string { return [...]string{"parked", "driving", "charging"}[m] }

// endpoints in pass order: engine and charging state first, so that a transition
// triggers the end-of-trip/charge readings within the same pass.
func endpoints() []volvo.Endpoint {
	return []volvo.Endpoint{
		volvo.EngineStatus, volvo.EnergyState,
		volvo.Odometer, volvo.Location, volvo.Statistics,
		volvo.Details, volvo.Diagnostics, volvo.Brakes, volvo.Engine, volvo.Fuel,
		volvo.Tyres, volvo.Warnings, volvo.Doors, volvo.Windows,
	}
}

// optional reports whether ep may be missing for a vehicle: its scope absent from an
// application or a grant made before it was requested, or the model without it.
func optional(ep volvo.Endpoint) bool {
	switch ep {
	case volvo.Brakes, volvo.Engine, volvo.Fuel:
		return true
	default:
		return false
	}
}

func (iv Intervals) of(m mode, ep volvo.Endpoint) time.Duration {
	switch ep {
	case volvo.EngineStatus:
		if m == driving {
			return iv.Active
		}
		return iv.Parked
	case volvo.EnergyState:
		if m == parked {
			return iv.Parked
		}
		return iv.Active
	case volvo.Odometer:
		if m == driving {
			return iv.Active
		}
		return iv.Rare
	case volvo.Details:
		return iv.Details
	default:
		return iv.Rare
	}
}

type vehicleState struct {
	mode     mode
	engine   string
	charging string
	next     map[volvo.Endpoint]time.Time
	// missing holds the optional endpoints the API refused, until when they are not
	// called: apart from next, which a transition or a policy brings forward.
	missing map[volvo.Endpoint]time.Time
	readAt  time.Time
	failure Failure
}

// Collector polls the vehicles. It is not safe for concurrent use: Run and PollOnce
// run in a single goroutine.
type Collector struct {
	api    API
	store  Store
	tokens TokenSource
	clk    clock.Clock
	iv     Intervals
	log    *slog.Logger
	obs    Observer
	der    Deriver
	pub    Publisher
	budget *budget
	policy Policy
	// reads is the policy's latest answer, which holds while it cannot be asked.
	reads map[string]Reads
	// noInstanceKey: the instance has no application key, a connection reads with its
	// own or not at all.
	noInstanceKey bool

	vehicles     map[string]*vehicleState // by VehicleID
	restored     bool                     // the budget replayed the saved calls
	apiBlocked   map[unit]time.Time       // quota exhausted
	keyRefused   map[keyID]time.Time      // application keys refused, until when
	accountPause map[string]time.Time     // rate limit or token rejected, per account
	reauthLogged map[string]bool          // connections whose loss was logged, by ConnectionID
	keyLogged    map[string]bool          // connections whose missing or refused key was logged
	written      map[string]Status        // the latest status written, by VehicleID
}

// keyID is an application key: a connection's own, as set at a time, or the instance's
// (zero).
type keyID struct {
	connection string
	setAt      time.Time
}

func keyOf(t Target) keyID {
	if !t.ownKey() {
		return keyID{}
	}
	return keyID{connection: t.ConnectionID, setAt: t.KeySetAt}
}

// New creates a collector. obs and der may be nil. Without the instance's application
// key (q.NoInstanceKey), only the connections with their own key are read.
func New(api API, store Store, tokens TokenSource, clk clock.Clock, iv Intervals, q Quota, log *slog.Logger, obs Observer, der Deriver) *Collector {
	if obs == nil {
		obs = nopObserver{}
	}
	return &Collector{
		api: api, store: store, tokens: tokens, clk: clk, iv: iv, log: log, obs: obs, der: der,
		budget:        newBudget(q, clk.Now()),
		noInstanceKey: q.NoInstanceKey,
		vehicles:      map[string]*vehicleState{},
		apiBlocked:    map[unit]time.Time{},
		keyRefused:    map[keyID]time.Time{},
		accountPause:  map[string]time.Time{},
		reauthLogged:  map[string]bool{},
		keyLogged:     map[string]bool{},
		written:       map[string]Status{},
	}
}

// SetPolicy limits how the accounts' vehicles are read, from the next pass on.
// Call it before Run.
func (c *Collector) SetPolicy(p Policy) { c.policy = p }

// SetPublisher has the vehicles' state published after each pass that read them, from
// the next pass on. Call it before Run.
func (c *Collector) SetPublisher(p Publisher) { c.pub = p }

// Run polls the vehicles every tick until ctx is canceled. passed, if not nil, is
// called after each pass that could list the vehicles: it feeds a health check.
func (c *Collector) Run(ctx context.Context, tick time.Duration, passed func()) error {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		switch err := c.PollOnce(ctx); {
		case err != nil && ctx.Err() == nil:
			c.log.Error("polling pass", "err", err)
		case err == nil && passed != nil:
			passed()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// PollOnce keeps the idle connections alive, then polls the due endpoints of each
// vehicle, those of the accounts that called the least lately first, and saves the
// calls made. The first pass starts the budget from the calls saved before.
func (c *Collector) PollOnce(ctx context.Context) error {
	var saved []Calls
	if !c.restored {
		var err error
		if saved, err = c.store.Calls(ctx, c.clk.Now().Add(-budgetWindow).Truncate(time.Hour)); err != nil {
			return err //nolint:wrapcheck // Store error, already has context
		}
	}
	defer c.saveCalls(ctx)
	// The tokens are refreshed whatever the keys: a key refused, or missing, leaves the
	// grant alive for the day the user gives one.
	if err := c.tokens.KeepAlive(ctx); err != nil {
		c.log.Error("token keep-alive", "err", err)
	}
	targets, err := c.store.Targets(ctx)
	if err != nil {
		return err //nolint:wrapcheck // Store error, already has context
	}
	targets = slices.Clone(targets)
	if c.policy != nil {
		if reads, err := c.policy.Reads(ctx, c.clk.Now()); err != nil {
			c.log.Warn("read policy not read: the previous one holds", "err", err)
		} else {
			c.reads = reads
		}
	}
	if !c.noInstanceKey {
		// The instance's key reads every connection: a key of an account, left from
		// before the instance had one, is not used.
		for i := range targets {
			targets[i].KeySetAt, targets[i].KeyRefused = time.Time{}, false
		}
	}
	if !c.restored { // the saved calls count on the keys the connections have now
		c.budget.restore(saved, targets, c.clk.Now())
		c.restored = true
	}
	// The vehicles the policy leaves out are not polled: they are forgotten, as a
	// vehicle removed, until it lets them be read again.
	c.syncPublisher(ctx, targets)
	targets = slices.DeleteFunc(targets, func(t Target) bool { return !c.reads[t.AccountID].reads(t) })
	c.budget.order(targets, c.clk.Now())
	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		if ctx.Err() != nil {
			return nil //nolint:nilerr // shutdown requested: not a polling error
		}
		seen[t.VehicleID] = true
		if !c.pollVehicle(ctx, t) {
			continue
		}
		if c.der != nil {
			if err := c.der.Update(ctx, t.AccountID, t.VehicleID); err != nil {
				c.log.Error("derive events", "vehicle", t.VehicleID, "err", err)
			}
		}
		if c.pub != nil && !c.reads[t.AccountID].NoPublish {
			if err := c.pub.Publish(ctx, t.AccountID, t.VehicleID); err != nil && ctx.Err() == nil {
				c.log.Warn("publish state", "vehicle", t.VehicleID, "err", err)
			}
		}
	}
	for id := range c.vehicles {
		if !seen[id] {
			delete(c.vehicles, id)
			delete(c.written, id)
		}
	}
	return nil
}

// syncPublisher has the publisher's connections follow the brokers' configurations, the
// accounts' vehicles and the policy.
func (c *Collector) syncPublisher(ctx context.Context, targets []Target) {
	if c.pub == nil {
		return
	}
	vehicles := map[string][]string{}
	for _, t := range targets {
		vehicles[t.AccountID] = append(vehicles[t.AccountID], t.VehicleID)
	}
	withheld := map[string]bool{}
	for account, r := range c.reads {
		if r.NoPublish {
			withheld[account] = true
		}
	}
	if err := c.pub.Sync(ctx, vehicles, withheld); err != nil && ctx.Err() == nil {
		c.log.Warn("publisher not synchronized", "err", err)
	}
}

// pollVehicle polls the due endpoints of t and reports whether a snapshot was saved
// (stored or checked again).
func (c *Collector) pollVehicle(ctx context.Context, t Target) (saved bool) {
	vs, ok := c.vehicles[t.VehicleID]
	if !ok {
		// everything is due on the first pass
		vs = &vehicleState{next: map[volvo.Endpoint]time.Time{}, missing: map[volvo.Endpoint]time.Time{}}
		c.vehicles[t.VehicleID] = vs
	}
	defer func() { c.report(ctx, t, vs) }()
	if t.ReauthReason != "" {
		c.reauthRequired(t, t.ReauthReason)
		return false
	}
	if !c.keyUsable(t, c.clk.Now()) {
		return false
	}
	var token, key string
	renewed := false // a rejected token is renewed once per pass
	for _, ep := range endpoints() {
		now := c.clk.Now()
		if now.Before(c.accountPause[t.AccountID]) || c.keyBlocked(t, now) {
			return saved
		}
		// A policy lifted, or shortened, applies at once.
		if soonest := now.Add(c.interval(t, vs.mode, ep)); vs.next[ep].After(soonest) {
			vs.next[ep] = soonest
		}
		if now.Before(vs.next[ep]) || now.Before(vs.missing[ep]) || now.Before(c.apiBlocked[c.budget.unitOf(t, ep.API())]) || !c.allowed(t, ep.API(), now) {
			continue
		}
		if token == "" {
			var err error
			if token, err = c.tokens.Token(ctx, t.AccountID, t.ConnectionID); err != nil {
				t.ReauthReason = c.tokenFailed(t, vs, now, err)
				return saved
			}
			if t.ownKey() {
				if key, err = c.store.ConnectionKey(ctx, t.AccountID, t.ConnectionID); err != nil {
					c.log.Warn("application key not read", "account", t.AccountID, "connection", t.ConnectionID, "err", err)
					return saved
				}
			}
		}
		vs.next[ep] = now.Add(c.interval(t, vs.mode, ep))
		raw, err := c.fetch(ctx, t, key, token, ep)
		if isUnauthorized(err) && !renewed {
			renewed = true
			if token, err = c.tokens.Refresh(ctx, t.AccountID, t.ConnectionID, token); err != nil {
				t.ReauthReason = c.tokenFailed(t, vs, now, err)
				return saved
			}
			raw, err = c.fetch(ctx, t, key, token, ep)
		}
		if err != nil {
			c.fetchFailed(ctx, t, vs, ep, now, err)
			continue
		}
		delete(vs.missing, ep)
		stored, err := c.save(ctx, t, ep, now, raw)
		if err == nil {
			saved, vs.readAt = true, now
		}
		c.called(t, ep, now, raw, stored, err)
		c.observe(t, vs, ep, raw, now)
	}
	return saved
}

// interval is how long after a call of ep t's next is due, in mode m: at the policy's
// pace, whatever the mode, when it limits t's account.
func (c *Collector) interval(t Target, m mode, ep volvo.Endpoint) time.Duration {
	if every := c.reads[t.AccountID].Every; every > 0 {
		return max(every, c.iv.of(parked, ep))
	}
	return c.iv.of(m, ep)
}

// allowed reports whether the budget lets t call api now; a call it defers stays due,
// and is made at a later pass.
func (c *Collector) allowed(t Target, api string, now time.Time) bool {
	ok, changed := c.budget.allows(t, api, now)
	args := []any{"api", api}
	switch u := c.budget.unitOf(t, api); {
	case u.key != "":
		args = append(args, "connection", u.key)
	case u.account != "":
		args = append(args, "account", u.account)
	}
	switch {
	case changed && !ok:
		c.log.Warn("quota budget spent: calls wait", args...)
	case changed:
		c.log.Info("quota budget available again", args...)
	}
	return ok
}

// saveCalls saves the calls counted by the budget, even once ctx is canceled: they are
// what a restarted collector starts from. Those it could not save are kept for the
// next pass.
func (c *Collector) saveCalls(ctx context.Context) {
	calls := c.budget.unsavedCalls()
	if len(calls) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveCallsTimeout)
	defer cancel()
	if err := c.store.SaveCalls(ctx, calls); err != nil {
		c.log.Warn("save calls", "err", err)
		c.budget.keep(calls)
	}
}

// saveCallsTimeout bounds the save of the calls, which a stopping collector still makes.
const saveCallsTimeout = 5 * time.Second

// fetch calls the API with key (empty: the instance's), counts the call in the budget
// and reports a failed call to the observer.
func (c *Collector) fetch(ctx context.Context, t Target, key, token string, ep volvo.Endpoint) ([]byte, error) {
	start := c.clk.Now()
	c.budget.spend(t, ep.API(), start)
	raw, err := c.api.Fetch(ctx, key, token, t.VIN, ep)
	if err != nil {
		call := Call{
			At: start, Duration: c.clk.Now().Sub(start), AccountID: t.AccountID, VehicleID: t.VehicleID,
			VIN: t.VIN, Endpoint: ep, Err: err.Error(),
		}
		var apiErr *volvo.APIError
		if errors.As(err, &apiErr) {
			call.Status = apiErr.Status
		}
		c.obs.Called(call)
	}
	return raw, err //nolint:wrapcheck // API error, already has context; its type drives the reaction
}

// called reports a successful call to the observer.
func (c *Collector) called(t Target, ep volvo.Endpoint, at time.Time, raw []byte, stored bool, err error) {
	call := Call{
		At: at, Duration: c.clk.Now().Sub(at), AccountID: t.AccountID, VehicleID: t.VehicleID, VIN: t.VIN,
		Endpoint: ep, Status: http200, Payload: raw, Stored: stored,
	}
	if err != nil {
		call.Err = err.Error()
	}
	c.obs.Called(call)
}

func isUnauthorized(err error) bool {
	var apiErr *volvo.APIError
	return errors.As(err, &apiErr) && apiErr.Kind == volvo.KindUnauthorized
}

// tokenFailed handles a TokenSource error and returns the re-authentication reason if
// the grant is lost: the connection is then no longer polled. Any other failure
// (provider or database down) is retried at the next pass, the TokenSource spacing
// its own attempts.
func (c *Collector) tokenFailed(t Target, vs *vehicleState, now time.Time, err error) (reauthReason string) {
	var r interface{ ReauthRequired() bool }
	if errors.As(err, &r) && r.ReauthRequired() {
		c.reauthRequired(t, err.Error())
		return err.Error()
	}
	vs.failure = Failure{At: now, Kind: FailToken}
	c.log.Warn("no access token", "account", t.AccountID, "connection", t.ConnectionID, "err", err)
	return ""
}

// keyUsable reports whether t has an application key it may read with now: its own,
// not refused, or the instance's. Else its vehicles are not polled, which is logged
// once per connection.
func (c *Collector) keyUsable(t Target, now time.Time) bool {
	var msg string
	switch {
	case !t.ownKey() && c.noInstanceKey:
		msg = "connection without an application key, and the instance has none: its vehicles are not polled"
	case c.keyBlocked(t, now):
		msg = "application key refused: the vehicles that read with it are not polled"
	default:
		delete(c.keyLogged, t.ConnectionID)
		return true
	}
	if !c.keyLogged[t.ConnectionID] {
		c.keyLogged[t.ConnectionID] = true
		c.log.Error(msg, "account", t.AccountID, "connection", t.ConnectionID, "own_key", t.ownKey())
	}
	return false
}

// keyBlocked reports whether the key t reads with was refused: its own until the user
// sets another, the instance's for keyRetry.
func (c *Collector) keyBlocked(t Target, now time.Time) bool {
	return (t.ownKey() && t.KeyRefused) || now.Before(c.keyRefused[keyOf(t)])
}

// keyState names the key t reads with, for the debug page.
func (c *Collector) keyState(t Target, now time.Time) string {
	switch {
	case !t.ownKey() && c.noInstanceKey:
		return "missing"
	case c.keyBlocked(t, now) && t.ownKey():
		return "refused"
	case c.keyBlocked(t, now):
		return "instance refused"
	case t.ownKey():
		return "own"
	}
	return "instance"
}

// reauthRequired logs once per connection that its vehicles are no longer polled.
func (c *Collector) reauthRequired(t Target, reason string) {
	if c.reauthLogged[t.ConnectionID] {
		return
	}
	c.reauthLogged[t.ConnectionID] = true
	c.log.Error("connection requires re-authentication: its vehicles are no longer polled",
		"account", t.AccountID, "connection", t.ConnectionID, "reason", reason)
}

// http200 and http403 avoid importing net/http for two constants.
const (
	http200 = 200
	http403 = 403
)

// report sends the vehicle state to the observer (as a copy: the observer reads it
// from another goroutine), and saves its status when it changed, or statusEvery after
// the last write.
func (c *Collector) report(ctx context.Context, t Target, vs *vehicleState) {
	next := make(map[volvo.Endpoint]time.Time, len(vs.next))
	for ep, at := range vs.next {
		next[ep] = at
	}
	blocked := c.blockedAPIs(t)
	c.obs.Observed(VehicleState{
		At: c.clk.Now(), AccountID: t.AccountID, VehicleID: t.VehicleID, VIN: t.VIN,
		Mode: vs.mode.String(), Engine: vs.engine, Charging: vs.charging, Next: next,
		PausedUntil: c.accountPause[t.AccountID], APIBlocked: blocked, ReauthReason: t.ReauthReason,
		Key: c.keyState(t, c.clk.Now()),
	})

	s := c.status(t, vs)
	if last, ok := c.written[t.VehicleID]; ok && sameStatus(last, s) && s.PassedAt.Sub(last.PassedAt) < statusEvery {
		return
	}
	if err := c.store.SaveStatus(ctx, s); err != nil {
		if ctx.Err() == nil {
			c.log.Warn("save status", "vehicle", t.VehicleID, "err", err)
		}
		return
	}
	c.written[t.VehicleID] = s
}

// status is the vehicle's status as of now: the blocks that already ended are left out.
func (c *Collector) status(t Target, vs *vehicleState) Status {
	now := c.clk.Now()
	s := Status{
		AccountID: t.AccountID, VehicleID: t.VehicleID, PassedAt: now, Mode: vs.mode.String(),
		ReadAt: vs.readAt, Failure: vs.failure, Quota: map[string]time.Time{},
	}
	if pause := c.accountPause[t.AccountID]; pause.After(now) {
		s.PausedUntil = pause
	}
	for api, until := range c.blockedAPIs(t) {
		if until.After(now) {
			s.Quota[api] = until
		}
	}
	if t.ReauthReason != "" || (!t.ownKey() && c.noInstanceKey) || (t.ownKey() && t.KeyRefused) {
		return s // nothing is due until the user acts
	}
	for i, ep := range endpoints() {
		due := vs.next[ep]
		for _, block := range []time.Time{vs.missing[ep], c.apiBlocked[c.budget.unitOf(t, ep.API())], c.accountPause[t.AccountID], c.keyRefused[keyOf(t)]} {
			if block.After(due) {
				due = block
			}
		}
		if i == 0 || due.Before(s.NextAt) {
			s.NextAt = due
		}
	}
	if s.NextAt.Before(now) {
		s.NextAt = now // due, at the next pass
	}
	return s
}

// blockedAPIs returns the APIs whose quota, of the key t reads with, is exhausted, and
// until when.
func (c *Collector) blockedAPIs(t Target) map[string]time.Time {
	blocked := map[string]time.Time{}
	for u, until := range c.apiBlocked {
		if u == c.budget.unitOf(t, u.api) {
			blocked[u.api] = until
		}
	}
	return blocked
}

// sameStatus compares two statuses but for their PassedAt.
func sameStatus(a, b Status) bool {
	return a.AccountID == b.AccountID && a.Mode == b.Mode && a.ReadAt.Equal(b.ReadAt) && a.NextAt.Equal(b.NextAt) &&
		a.PausedUntil.Equal(b.PausedUntil) && maps.EqualFunc(a.Quota, b.Quota, time.Time.Equal) && a.Failure == b.Failure
}

func (c *Collector) save(ctx context.Context, t Target, ep volvo.Endpoint, now time.Time, raw []byte) (bool, error) {
	hash, err := volvo.Fingerprint(raw)
	if err != nil {
		c.log.Error("fingerprint", "vehicle", t.VehicleID, "endpoint", ep, "err", err)
		return false, err //nolint:wrapcheck // already has context from volvo
	}
	stored, err := c.store.SaveSnapshot(ctx, Snapshot{
		AccountID: t.AccountID, VehicleID: t.VehicleID, Endpoint: ep, FetchedAt: now, Payload: raw, Hash: hash,
	})
	if err != nil {
		c.log.Error("store", "vehicle", t.VehicleID, "endpoint", ep, "err", err)
		return false, err //nolint:wrapcheck // Store error, already has context
	}
	c.log.Debug("snapshot", "vehicle", t.VehicleID, "endpoint", ep, "stored", stored)
	return stored, nil
}

// observe updates the vehicle state and reschedules on a transition.
func (c *Collector) observe(t Target, vs *vehicleState, ep volvo.Endpoint, raw []byte, now time.Time) {
	var err error
	switch ep {
	case volvo.EngineStatus:
		vs.engine, err = volvo.ParseEngineStatus(raw)
	case volvo.EnergyState:
		vs.charging, err = volvo.ParseChargingStatus(raw)
	default:
		return
	}
	if err != nil {
		c.log.Warn("read state", "vehicle", t.VehicleID, "endpoint", ep, "err", err)
		return
	}

	m := parked
	switch {
	case vs.engine == "RUNNING":
		m = driving
	case vs.charging == "CHARGING":
		m = charging
	}
	if m == vs.mode {
		return
	}
	c.log.Info("transition", "vehicle", t.VehicleID, "from", vs.mode.String(), "to", m.String())
	old := vs.mode
	vs.mode = m
	for e, at := range vs.next {
		if sooner := now.Add(c.interval(t, m, e)); sooner.Before(at) {
			vs.next[e] = sooner
		}
	}
	if old != parked && c.reads[t.AccountID].Every == 0 { // end of trip or charge: immediate readings
		for _, e := range []volvo.Endpoint{volvo.Location, volvo.Statistics, volvo.Odometer} {
			vs.next[e] = now
		}
	}
}

func (c *Collector) fetchFailed(ctx context.Context, t Target, vs *vehicleState, ep volvo.Endpoint, now time.Time, err error) {
	var apiErr *volvo.APIError
	if errors.As(err, &apiErr) && optional(ep) && missingEndpoint(apiErr) {
		// Not a failure the user can act upon: tried again at the details' pace, logged
		// once until it answers.
		if _, logged := vs.missing[ep]; !logged {
			c.log.Info("optional endpoint unavailable: tried again later", "vehicle", t.VehicleID, "endpoint", ep,
				"status", apiErr.Status)
		}
		vs.missing[ep] = now.Add(c.interval(t, vs.mode, volvo.Details))
		return
	}
	vs.failure = Failure{At: now, Endpoint: ep, Kind: FailUnavailable}
	if apiErr == nil {
		c.log.Warn("call", "vehicle", t.VehicleID, "endpoint", ep, "err", err)
		return
	}
	vs.failure.Status, vs.failure.Kind = apiErr.Status, failureKind(apiErr)
	switch apiErr.Kind {
	case volvo.KindQuota:
		c.apiBlocked[c.budget.unitOf(t, ep.API())] = now.Add(apiErr.RetryIn)
		c.log.Error("quota exhausted", "api", ep.API(), "account", t.AccountID, "own_key", t.ownKey(),
			"retry_in", apiErr.RetryIn.String())
	case volvo.KindKeyRefused:
		// The key, not the grant: the tokens and the re-authentication are left alone.
		c.keyRefused[keyOf(t)] = now.Add(keyRetry)
		if t.ownKey() {
			if err := c.store.SetKeyRefused(ctx, t.AccountID, t.ConnectionID, t.KeySetAt, true); err != nil {
				c.log.Warn("save refused key", "connection", t.ConnectionID, "err", err)
			}
		}
		c.log.Error("application key refused", "account", t.AccountID, "connection", t.ConnectionID,
			"own_key", t.ownKey(), "status", apiErr.Status)
	case volvo.KindRateLimited:
		c.accountPause[t.AccountID] = now.Add(apiErr.RetryIn)
		c.log.Warn("rate limit", "account", t.AccountID, "retry_in", apiErr.RetryIn.String())
	case volvo.KindUnauthorized:
		// Rejected again right after a refresh: the provider disagrees with the grant
		// we hold. Wait, rather than refreshing in a loop.
		c.accountPause[t.AccountID] = now.Add(c.iv.Parked)
		c.log.Warn("token rejected after a refresh", "account", t.AccountID, "retry_in", c.iv.Parked.String())
	default:
		c.log.Warn("call", "vehicle", t.VehicleID, "endpoint", ep, "status", apiErr.Status, "err", apiErr.Message)
	}
}

// missingEndpoint reports whether e tells that an endpoint is not there for the vehicle:
// a 404, or a 403 that is neither the quota nor a refused key. Assumption: a scope
// missing from the token is such a 403 (not observed).
func missingEndpoint(e *volvo.APIError) bool {
	return e.Kind == volvo.KindNotFound || (e.Kind == volvo.KindOther && e.Status == http403)
}

func failureKind(e *volvo.APIError) string {
	switch e.Kind {
	case volvo.KindQuota:
		return FailQuota
	case volvo.KindRateLimited:
		return FailRateLimited
	case volvo.KindUnauthorized:
		return FailRejected
	case volvo.KindNotFound:
		return FailNotFound
	case volvo.KindKeyRefused:
		return FailKeyRefused
	case volvo.KindOther:
	}
	if e.Status == 0 || e.Status >= 500 {
		return FailUnavailable
	}
	return FailOther
}
