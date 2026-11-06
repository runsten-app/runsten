package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/flags"
	"runsten/internal/oauth"
)

// BMW CarData (ADR 0045), BMW's own API, for the BMWs and the Minis. The owner creates a
// client ID in the MyBMW or MyMINI portal and gives it; Runsten starts BMW's device flow
// with it (RFC 8628, with PKCE: a public client, no secret, no password) and shows the
// code the owner enters on BMW's page. Meanwhile the web interface asks Runsten to poll
// the token endpoint, at the interval BMW gives, until the owner approved it. The device
// code and the PKCE verifier are kept in memory, one flow per account, until it ends.
//
// Once approved, the vehicles the account is the primary user of are listed and added,
// and the credential stored: the client ID, the GCID and the refresh token, sealed.
// Nothing of it is returned, logged nor exported: only the client ID's last four
// characters, when it was given, and whether it was refused.

// BMWCredential is what a BMW connection holds: the client ID, the BMW account's GCID
// (the stream's username), the refresh token, and the ID of Runsten's container, which
// the collector creates.
type BMWCredential struct {
	ClientID, GCID, Refresh, ContainerID string
}

// BMWCredentials keeps the account's BMW connection. Every method is restricted to the
// account.
type BMWCredentials interface {
	BMWConnection(ctx context.Context, accountID string) (CredentialConnection, error)
	// SetBMW stores c, set at at, in the account's BMW connection, created if it has
	// none; a refusal of the previous one is cleared. It returns the connection's ID.
	SetBMW(ctx context.Context, accountID string, c BMWCredential, at time.Time) (string, error)
	// DeleteBMW erases it: the connection stays, with its vehicles and their history,
	// no longer read until another.
	DeleteBMW(ctx context.Context, accountID string) error
}

// BMWDevice is a device code BMW gave: the owner enters UserCode at VerificationURI
// while Runsten polls with DeviceCode and the PKCE Verifier every Interval.
type BMWDevice struct {
	DeviceCode, Verifier, UserCode, VerificationURI, VerificationURIComplete string
	ExpiresIn, Interval                                                      time.Duration
}

// BMWTokens are what an approved device code gives.
type BMWTokens struct {
	Access, Refresh, GCID string
}

// BMWOutcome is what an error of BMWAccounts means for the flow.
type BMWOutcome int

// The outcomes. BMWUnavailable: BMW did not answer, or not as expected.
const (
	BMWUnavailable BMWOutcome = iota
	// BMWPending: the owner has not approved the code yet; BMWSlowDown: the same, polled
	// too often.
	BMWPending
	BMWSlowDown
	// BMWExpired: the code expired before the owner approved it; BMWDenied: the owner
	// refused it.
	BMWExpired
	BMWDenied
	// BMWClientRefused: BMW does not know the client ID.
	BMWClientRefused
	// BMWScopeMissing: the client ID lacks CarData's API (CU-103): the owner subscribes
	// to it in the portal.
	BMWScopeMissing
)

// BMWError is an error of BMWAccounts, with what it means.
type BMWError struct {
	Outcome BMWOutcome
	Err     error
}

func (e *BMWError) Error() string { return e.Err.Error() }
func (e *BMWError) Unwrap() error { return e.Err }

// BMWAccounts runs the device flow and lists the account's vehicles. Its errors are
// *BMWError when they mean something for the flow.
type BMWAccounts interface {
	DeviceCode(ctx context.Context, clientID string) (BMWDevice, error)
	PollToken(ctx context.Context, clientID, deviceCode, verifier string) (BMWTokens, error)
	// PrimaryVINs are the VINs of the vehicles the account is the primary user of: the
	// only ones CarData reads.
	PrimaryVINs(ctx context.Context, access string) ([]string, error)
}

// bmwMaxFlows caps the device flows pending on the instance, so that repeated starts
// cannot exhaust memory.
const bmwMaxFlows = 1000

// bmwFlow is a device flow pending: the client ID, the device code, and when it may be
// polled again.
type bmwFlow struct {
	clientID string
	device   BMWDevice
	expires  time.Time
	interval time.Duration
	next     time.Time
}

// bmwFlows are the device flows pending, one per account: a new one replaces the
// previous. In memory: a restart forgets them, and the owner starts again.
type bmwFlows struct {
	mu      sync.Mutex
	pending map[string]bmwFlow
	max     int // 0: bmwMaxFlows
}

func (f *bmwFlows) start(account string, fl bmwFlow, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil {
		f.pending = map[string]bmwFlow{}
	}
	for a, p := range f.pending {
		if !now.Before(p.expires) {
			delete(f.pending, a)
		}
	}
	limit := f.max
	if limit == 0 {
		limit = bmwMaxFlows
	}
	if _, ok := f.pending[account]; !ok && len(f.pending) >= limit {
		return false
	}
	f.pending[account] = fl
	return true
}

// take returns the account's flow, if pending and not expired.
func (f *bmwFlows) take(account string, now time.Time) (bmwFlow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.pending[account]
	if ok && !now.Before(fl.expires) {
		delete(f.pending, account)
		return bmwFlow{}, false
	}
	return fl, ok
}

// wait sets when the account's flow may be polled again, interval its new interval.
func (f *bmwFlows) wait(account string, interval time.Duration, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fl, ok := f.pending[account]; ok {
		fl.interval, fl.next = interval, now.Add(interval)
		f.pending[account] = fl
	}
}

func (f *bmwFlows) end(account string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pending, account)
}

type bmwJSON struct {
	Last4     string     `json:"last4" doc:"The last four characters of the client ID, to find it again in the MyBMW or MyMINI portal: the tokens are never returned."`
	SetAt     time.Time  `json:"set_at" pattern:"Z$" doc:"When the owner connected."`
	RefusedAt *time.Time `json:"refused_at" pattern:"Z$" doc:"When BMW refused the connection (the refresh token unused for two weeks, or the client ID's subscription withdrawn in the portal): its vehicles are not read until the owner connects again. null: not refused."`
}

func bmwOf(c CredentialConnection) null[bmwJSON] {
	if c.SetAt.IsZero() {
		return null[bmwJSON]{}
	}
	return known(bmwJSON{Last4: c.Last4, SetAt: c.SetAt, RefusedAt: timeOrNil(c.RefusedAt)})
}

type bmwClientJSON struct {
	// Assumption: a client ID is printable ASCII without spaces (BMW's are UUIDs); the
	// bounds only catch a paste gone wrong.
	ClientID string `json:"client_id" minLength:"8" maxLength:"128" pattern:"^[!-~]+$" doc:"The client ID created in the MyBMW or MyMINI portal (BMW CarData), with both of CarData's subscriptions."`
}

type bmwClientInput struct {
	Body bmwClientJSON
}

type bmwDeviceJSON struct {
	UserCode                string    `json:"user_code" minLength:"1" doc:"The code to enter on BMW's page."`
	VerificationURI         string    `json:"verification_uri" format:"uri" doc:"BMW's page, where the owner signs in and enters the code."`
	VerificationURIComplete *string   `json:"verification_uri_complete" format:"uri" doc:"BMW's page with the code already filled in; null when BMW gives none."`
	ExpiresAt               time.Time `json:"expires_at" pattern:"Z$" doc:"When the code expires: the owner starts again after it."`
	Interval                int       `json:"interval" minimum:"1" doc:"Seconds between two requests of POST /connection/bmw/token."`
}

// offerBMW refuses the account the flag is off for, or that must first go to a page of
// the web interface.
func (s *Server) offerBMW(ctx context.Context, account string) error {
	if !s.enabled(ctx, flags.SourceBMW, account) {
		return apiError(http.StatusForbidden, codeFeatureUnavailable, "BMW is not offered to this account")
	}
	return s.admit(ctx, account)
}

func (s *Server) postBMWAuthorization(ctx context.Context, in *bmwClientInput) (*body[bmwDeviceJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.offerBMW(ctx, account); err != nil {
		return nil, err
	}
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	d, err := s.BMWAccounts.DeviceCode(check, in.Body.ClientID)
	var be *BMWError
	switch {
	case errors.As(err, &be) && be.Outcome == BMWClientRefused:
		return nil, apiError(http.StatusBadRequest, codeCredentialRefused, "BMW does not know this client ID")
	case err != nil:
		s.Log.Warn("BMW device code not given", "err", err)
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "BMW did not answer, try again later")
	case !secureURL(d.VerificationURI) || (d.VerificationURIComplete != "" && !secureURL(d.VerificationURIComplete)):
		// The owner signs in there: never a page another could change on the way.
		s.Log.Warn("BMW device code with a verification page not over https")
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "BMW did not answer as expected, try again later")
	}
	now := s.Clock.Now()
	fl := bmwFlow{clientID: in.Body.ClientID, device: d, expires: now.Add(d.ExpiresIn), interval: d.Interval, next: now.Add(d.Interval)}
	if !s.bmwFlows.start(account, fl, now) {
		return nil, apiError(http.StatusServiceUnavailable, codeSourceUnavailable, "too many authorizations are pending, try again later")
	}
	out := bmwDeviceJSON{
		UserCode: d.UserCode, VerificationURI: d.VerificationURI, ExpiresAt: fl.expires.UTC(),
		Interval: int(max(time.Second, d.Interval).Round(time.Second) / time.Second),
	}
	if d.VerificationURIComplete != "" {
		out.VerificationURIComplete = &d.VerificationURIComplete
	}
	return respond(out), nil
}

// secureURL reports whether u is an https URL with a host.
func secureURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && p.Scheme == "https" && p.Host != ""
}

// pending answers that the owner has not approved the code yet, with when to ask again.
func pending(wait time.Duration) error {
	return huma.ErrorWithHeaders( //nolint:wrapcheck // the error of the contract, with its header
		apiError(http.StatusConflict, codeAuthorizationPending, "the code is not approved yet on BMW's page"),
		http.Header{"Retry-After": {retryAfter(wait)}})
}

// postBMWToken polls the token endpoint once for the account's pending flow, no sooner
// than BMW's interval. Approved, it lists the vehicles, stores the credential and adds
// them. A flow that ends, approved or not, is forgotten.
func (s *Server) postBMWToken(ctx context.Context, _ *struct{}) (*body[connectionSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.offerBMW(ctx, account); err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	fl, ok := s.bmwFlows.take(account, now)
	if !ok {
		return nil, apiError(http.StatusBadRequest, codeAuthorizationExpired, "no code is waiting, or it expired: start again")
	}
	if now.Before(fl.next) {
		return nil, pending(fl.next.Sub(now))
	}
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	tok, err := s.BMWAccounts.PollToken(check, fl.clientID, fl.device.DeviceCode, fl.device.Verifier)
	var be *BMWError
	if errors.As(err, &be) {
		switch be.Outcome {
		case BMWPending:
			s.bmwFlows.wait(account, fl.interval, now)
			return nil, pending(fl.interval)
		case BMWSlowDown: // RFC 8628 §3.5: 5 seconds more from now on
			s.bmwFlows.wait(account, fl.interval+5*time.Second, now)
			return nil, pending(fl.interval + 5*time.Second)
		case BMWExpired:
			s.bmwFlows.end(account)
			return nil, apiError(http.StatusBadRequest, codeAuthorizationExpired, "the code expired before it was approved: start again")
		case BMWDenied, BMWClientRefused:
			s.bmwFlows.end(account)
			return nil, apiError(http.StatusBadRequest, codeCredentialRefused, "BMW refused the authorization")
		case BMWUnavailable, BMWScopeMissing:
		}
	}
	if err != nil {
		// Tried again at the next request: the code may still be approved.
		s.bmwFlows.wait(account, fl.interval, now)
		s.Log.Warn("BMW token not given", "err", err)
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "BMW did not answer, try again later")
	}
	// The device code is spent: whatever happens next, the owner starts again.
	s.bmwFlows.end(account)
	vins, err := s.BMWAccounts.PrimaryVINs(check, tok.Access)
	switch {
	case errors.As(err, &be) && be.Outcome == BMWScopeMissing:
		return nil, apiError(http.StatusConflict, codeScopeMissing, "the client ID lacks CarData's API: subscribe to it in the portal, then start again")
	case err != nil:
		s.Log.Warn("BMW vehicles not listed", "err", err)
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "BMW did not answer, try again later")
	case len(vins) == 0:
		return nil, apiError(http.StatusConflict, codeNoVehicle, "the BMW account is the primary user of no vehicle")
	}
	limit, err := s.vehicleCap(ctx, account)
	if err != nil {
		return nil, s.internal("vehicles not read", err)
	}
	if limit.Exceeded(vins) {
		return nil, s.tooManyVehicles()
	}
	cred := BMWCredential{ClientID: fl.clientID, GCID: tok.GCID, Refresh: tok.Refresh}
	id, err := s.BMW.SetBMW(ctx, account, cred, s.Clock.Now())
	if err != nil {
		return nil, s.internal("BMW connection not stored", err)
	}
	if err := oauth.AddVehicles(ctx, s.Enrollment, account, id, vins); err != nil {
		return nil, s.internal("vehicles not recorded", err)
	}
	s.Log.Info("BMW connected", "account", account, "vehicles", len(vins))
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) deleteBMW(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	s.bmwFlows.end(account)
	if err := s.BMW.DeleteBMW(ctx, account); err != nil {
		return nil, s.internal("BMW connection not deleted", err)
	}
	s.Log.Info("BMW connection deleted", "account", account)
	return &struct{}{}, nil
}

func (s *Server) registerBMW(api huma.API) {
	const offered = "`feature_unavailable`: BMW is not offered to the account (the flag `source.bmw`). " + errNotAdmitted
	auth := s.operation(huma.Operation{
		OperationID: "postBMWAuthorization", Method: http.MethodPost, Path: "/connection/bmw/authorization", Tags: []string{"connection"},
		Summary: "Start BMW's device flow with the owner's client ID",
		Description: "BMW gives a code, which the owner enters on BMW's page, signed in to their MyBMW or MyMINI account, within " +
			"its lifetime. Runsten keeps the device code and its PKCE verifier in memory, one flow per account: a new start replaces " +
			"the previous. Then POST /connection/bmw/token, every `interval` seconds, until it connects.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The code to enter.", writeErrors(map[int]string{
		http.StatusBadRequest:         errBody + " `credential_refused`: BMW does not know the client ID.",
		http.StatusForbidden:          offered,
		http.StatusBadGateway:         "`source_unavailable`: BMW did not answer.",
		http.StatusServiceUnavailable: "`source_unavailable`: too many flows are pending on the instance.",
	}))
	huma.Register(api, auth, s.postBMWAuthorization)
	token := s.operation(huma.Operation{
		OperationID: "postBMWToken", Method: http.MethodPost, Path: "/connection/bmw/token", Tags: []string{"connection"},
		Summary: "Connect the account's BMW once the owner approved the code",
		Description: "Asks BMW once, no sooner than its interval, whether the owner approved the code. Approved, the vehicles the " +
			"BMW account is the primary user of are listed and added to the account, and the connection stored: the client ID, the " +
			"account's identifier and the refresh token, sealed, never returned. It replaces a previous connection.",
	}, "The connection.", map[int]string{
		http.StatusUnauthorized: errUnauthorized,
		http.StatusBadRequest: "`authorization_expired`: no code is waiting (none started, or the instance restarted), or it " +
			"expired. `credential_refused`: the owner or BMW refused the authorization.",
		http.StatusForbidden: offered,
		http.StatusConflict: "`authorization_pending`: not approved yet; ask again after `Retry-After`. `scope_missing`: the client " +
			"ID lacks CarData's API subscription. `no_vehicle`: the BMW account is the primary user of no vehicle. " +
			"`too_many_vehicles`: the account would have more vehicles than the instance lets it have. Nothing is stored but " +
			"while pending, and the code is spent once approved.",
		http.StatusBadGateway:          "`source_unavailable`: BMW did not answer; nothing is stored, the code may still be approved.",
		http.StatusInternalServerError: errInternal,
	})
	one := 1.0
	token.Responses[strconv.Itoa(http.StatusConflict)].Headers = map[string]*huma.Param{
		"Retry-After": {
			Description: "Seconds to wait before asking again, while `authorization_pending`.",
			Schema:      &huma.Schema{Type: huma.TypeInteger, Minimum: &one},
		},
	}
	huma.Register(api, token, s.postBMWToken)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteBMW", Method: http.MethodDelete, Path: "/connection/bmw", Tags: []string{"connection"},
		Summary:       "Remove the account's BMW connection",
		Description:   "The client ID and the tokens are erased, and a flow pending forgotten. The vehicles stay, with their history, and are no longer read until the owner connects again.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deleteBMW)
}
