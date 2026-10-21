package collector

import (
	"context"
	"errors"
	"time"
)

// session is a Source's access to a connection's vehicles, kept until it expires.
type session struct {
	Session
	credential keyID // the credential it was asked for with
	until      time.Time
}

const (
	// sessionMargin is how long before it expires an access is asked for again, so that
	// a call does not meet its end.
	sessionMargin = time.Minute
	// loginRetry is how long a connection waits after its access could not be asked for
	// (the provider unreachable, or failing): it is not asked for again on every pass.
	loginRetry = time.Minute
)

// credentialOf is the credential t reads with, as set at a time.
func credentialOf(t Target) keyID { return keyID{connection: t.ConnectionID, setAt: t.CredentialSetAt} }

// credentialBlocked reports whether t's connection has no credential that reads: the
// user removed it, or the provider refused it.
func (c *Collector) credentialBlocked(t Target) bool {
	return t.CredentialSetAt.IsZero() || t.CredentialRefused || c.credentialRefused[credentialOf(t)]
}

// pollSource polls the due endpoints of t through src, with its connection's credential,
// and reports whether a snapshot was saved (stored or checked again).
func (c *Collector) pollSource(ctx context.Context, t Target, vs *vehicleState, src *source) (saved bool) {
	switch {
	case src == nil:
		c.logOnce(t, "no source reads the connection's provider: its vehicles are not polled", "provider", t.Provider)
		return false
	case t.CredentialSetAt.IsZero():
		c.logOnce(t, "no credential: its vehicles are not polled until the user sets one")
		return false
	case c.credentialBlocked(t):
		c.logOnce(t, "credential refused: its vehicles are not polled until the user sets another")
		return false
	case t.Unshared || vs.unshared:
		return false
	}
	delete(c.sourceLogged, t.ConnectionID)
	var sess Session
	renewed := false // an access rejected is asked for again once per pass
	for _, spec := range src.Endpoints {
		now := c.clk.Now()
		if now.Before(c.connectionPause[t.ConnectionID]) || vs.unshared || c.credentialBlocked(t) {
			return saved
		}
		c.bringForward(t, vs, spec, now)
		if !c.due(t, vs, spec, now) {
			continue
		}
		if sess == nil {
			var err error
			if sess, err = c.session(ctx, t, src, now); err != nil {
				c.sourceFailed(ctx, t, vs, src, "", now, err)
				return saved
			}
		}
		vs.next[spec.Endpoint] = now.Add(c.interval(t, vs.mode, spec))
		raw, err := c.fetchSource(ctx, t, src, sess, spec, now)
		var se *SourceError
		if errors.As(err, &se) && se.Kind == KindUnauthorized && !renewed {
			renewed = true
			delete(c.sessions, t.ConnectionID)
			if sess, err = c.session(ctx, t, src, now); err != nil {
				c.sourceFailed(ctx, t, vs, src, "", now, err)
				return saved
			}
			raw, err = c.fetchSource(ctx, t, src, sess, spec, now)
		}
		if err != nil {
			c.sourceFailed(ctx, t, vs, src, spec.Endpoint, now, err)
			continue
		}
		saved = c.read(ctx, t, vs, src, spec.Endpoint, now, raw) || saved
	}
	return saved
}

// session returns the access of t's connection, asked for with its credential unless
// one is kept for it.
func (c *Collector) session(ctx context.Context, t Target, src *source, now time.Time) (Session, error) {
	cred := credentialOf(t)
	if s, ok := c.sessions[t.ConnectionID]; ok && s.credential == cred && now.Before(s.until) {
		return s.Session, nil
	}
	delete(c.sessions, t.ConnectionID)
	s, ttl, err := src.ext.Login(ctx, t.AccountID, t.ConnectionID)
	if err != nil {
		return nil, err //nolint:wrapcheck // the Source's; its type drives the reaction
	}
	c.sessions[t.ConnectionID] = &session{Session: s, credential: cred, until: now.Add(ttl - min(sessionMargin, ttl/2))}
	return s, nil
}

// fetchSource calls the provider, counts the call in the budget and reports a failed
// call to the observer.
func (c *Collector) fetchSource(ctx context.Context, t Target, src *source, sess Session, spec EndpointSpec, start time.Time) ([]byte, error) {
	c.budget.spend(t, spec.API, start)
	raw, err := sess.Fetch(ctx, t.VIN, spec.Endpoint)
	if err != nil {
		call := Call{
			At: start, Duration: c.clk.Now().Sub(start), AccountID: t.AccountID, VehicleID: t.VehicleID,
			VIN: t.VIN, Source: src.Name, Endpoint: spec.Endpoint, Err: err.Error(),
		}
		var se *SourceError
		if errors.As(err, &se) {
			call.Status = se.Status
		}
		c.obs.Called(call)
	}
	return raw, err //nolint:wrapcheck // the Source's; its type drives the reaction
}

// sourceFailed reacts to a failure of src's provider: of the access when ep is empty,
// else of a call of ep.
func (c *Collector) sourceFailed(ctx context.Context, t Target, vs *vehicleState, src *source, ep Endpoint, now time.Time, err error) {
	var se *SourceError
	if !errors.As(err, &se) {
		se = &SourceError{Kind: KindOther, Err: err}
	}
	args := []any{
		"account", t.AccountID, "connection", t.ConnectionID, "vehicle", t.VehicleID, "endpoint", ep,
		"status", se.Status,
	}
	switch se.Kind {
	case KindNotAvailable, KindNotGranted:
		// The endpoint's, not the vehicle's: tried again later, logged once until it
		// answers. Not given is nothing the user can act upon; not granted is told.
		if _, logged := vs.missing[ep]; !logged {
			c.log.Info("endpoint not given: tried again later", append(args, "err", err)...)
		}
		if se.Kind == KindNotAvailable {
			vs.missing[ep] = now.Add(c.interval(t, parked, EndpointSpec{Parked: PaceRare}))
			return
		}
		vs.missing[ep] = now.Add(c.detailsInterval(t))
		vs.failure = Failure{At: now, Endpoint: ep, Status: se.Status, Kind: FailNotGranted}
		return
	case KindCredentialRefused:
		// Not a passing state: refused, expired, or an account ID not the credential's.
		cred := credentialOf(t)
		c.credentialRefused[cred] = true
		delete(c.sessions, t.ConnectionID)
		if err := src.store.SetCredentialRefused(ctx, t.AccountID, t.ConnectionID, t.CredentialSetAt); err != nil {
			c.log.Warn("save refused credential", "connection", t.ConnectionID, "err", err)
		}
		c.log.Error("credential refused: its vehicles are no longer polled", args...)
		vs.failure = Failure{At: now, Endpoint: ep, Status: se.Status, Kind: FailCredentialRefused}
		return
	case KindUnshared:
		vs.unshared = true
		if err := src.store.SetUnshared(ctx, t.AccountID, t.VehicleID); err != nil {
			c.log.Warn("save unshared vehicle", "vehicle", t.VehicleID, "err", err)
		}
		c.log.Error("vehicle no longer shared with the credential: no longer polled", args...)
		vs.failure = Failure{At: now, Endpoint: ep, Status: se.Status, Kind: FailUnshared}
		return
	case KindRateLimited:
		c.connectionPause[t.ConnectionID] = now.Add(se.RetryIn)
		c.log.Warn("rate limit", append(args, "retry_in", se.RetryIn.String())...)
		vs.failure = Failure{At: now, Endpoint: ep, Status: se.Status, Kind: FailRateLimited}
		return
	case KindUnauthorized:
		// Rejected again with a new access: wait, rather than asking in a loop.
		delete(c.sessions, t.ConnectionID)
		c.connectionPause[t.ConnectionID] = now.Add(c.iv.Parked)
		c.log.Warn("access rejected again", append(args, "retry_in", c.iv.Parked.String())...)
		vs.failure = Failure{At: now, Endpoint: ep, Status: se.Status, Kind: FailRejected}
		return
	case KindOther:
	}
	if ep == "" {
		// No access: asked for again after a while, not on every pass.
		c.connectionPause[t.ConnectionID] = now.Add(loginRetry)
		c.log.Warn("no access", append(args, "err", err)...)
		vs.failure = Failure{At: now, Status: se.Status, Kind: FailToken}
		return
	}
	c.log.Warn("call", append(args, "err", err)...)
	vs.failure = Failure{At: now, Endpoint: ep, Status: se.Status, Kind: FailOther}
	if se.Status == 0 || se.Status >= 500 {
		vs.failure.Kind = FailUnavailable
	}
}

// logOnce logs an error once per connection, until its vehicles are polled again.
func (c *Collector) logOnce(t Target, msg string, args ...any) {
	if c.sourceLogged[t.ConnectionID] {
		return
	}
	c.sourceLogged[t.ConnectionID] = true
	c.log.Error(msg, append([]any{"account", t.AccountID, "connection", t.ConnectionID}, args...)...)
}
