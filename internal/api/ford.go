package api

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/auth"
	"runsten/internal/flags"
)

// FordConnect Query, Ford's read-only API, behind the flag source.ford. The owner creates
// an application on Ford's developer portal and gives its client ID and secret; Runsten
// gives back the address of Ford's sign-in, where they choose the car and consent. Ford
// then sends them to the application's redirect address with a code:
//
//   - to this instance (GET /auth/ford/callback), when its address is one Ford accepts
//     as a redirect address: https, or http on localhost;
//   - else to a localhost address nothing answers, which the owner pastes back
//     (PUT /connection/ford/code), as for the Polestar ID.
//
// Either way the code is exchanged with the application's secret, the garage tells the
// one car the grant reads, and the connection is stored: the client ID, the secret, the
// redirect address and the refresh token, sealed. Nothing of it is returned, logged nor
// exported: only the client ID's last four characters, when it was given, when its grant
// ends, and whether it was refused. The application and the state of a sign-in are kept
// in memory, one sign-in per account, until it ends.

// FordCredential is what a Ford connection holds: the application's client ID and
// secret, the redirect address the sign-in was asked with (the token endpoint is given
// it again at every refresh), and the refresh token.
type FordCredential struct {
	ClientID, ClientSecret, RedirectURI, Refresh string
}

// FordCredentials keeps the account's Ford connection. Every method is restricted to the
// account.
type FordCredentials interface {
	FordConnection(ctx context.Context, accountID string) (CredentialConnection, error)
	// SetFord stores c, set at at and its grant ending at expiresAt (zero: not told), in
	// the account's Ford connection, created if it has none; a refusal of the previous
	// one is cleared. It returns the connection's ID.
	SetFord(ctx context.Context, accountID string, c FordCredential, expiresAt, at time.Time) (string, error)
	// DeleteFord erases it: the connection stays, with its vehicle and its history, no
	// longer read until another.
	DeleteFord(ctx context.Context, accountID string) error
}

// FordGrant is what a sign-in gives: the refresh token to keep, the VIN of the car the
// grant reads, and when the grant ends (zero: not told).
type FordGrant struct {
	Refresh, VIN string
	ExpiresAt    time.Time
}

// FordOutcome is what an error of FordAccounts means for the sign-in.
type FordOutcome int

// The outcomes. FordUnavailable: Ford did not answer, or not as expected.
const (
	FordUnavailable FordOutcome = iota
	// FordCodeRefused: Ford refused the code: used already, or too old.
	FordCodeRefused
	// FordApplicationRefused: Ford does not know the client ID, or the secret is not
	// its own.
	FordApplicationRefused
	// FordNoVehicle: the grant reads no car.
	FordNoVehicle
)

// FordError is an error of FordAccounts, with what it means.
type FordError struct {
	Outcome FordOutcome
	Err     error
}

func (e *FordError) Error() string { return e.Err.Error() }
func (e *FordError) Unwrap() error { return e.Err }

// FordAccounts builds the sign-in's address and completes a sign-in. Its errors are
// *FordError when they mean something for the sign-in.
type FordAccounts interface {
	// AuthorizeURL is Ford's sign-in page for the application c and a state.
	AuthorizeURL(c FordCredential, state string) (string, error)
	// Connect exchanges the code of a sign-in made with the application c, and reads the
	// car the grant covers.
	Connect(ctx context.Context, c FordCredential, code string) (FordGrant, error)
}

const (
	// fordMaxFlows caps the sign-ins pending on the instance, so that repeated starts
	// cannot exhaust memory.
	fordMaxFlows = 1000
	// fordFlowTTL is how long a sign-in may take: the owner signs in to Ford, chooses
	// the car and consents meanwhile.
	fordFlowTTL = 15 * time.Minute
	// fordStateLen is the state's length: the longest Ford's sign-in carries back whole
	// (reported by another client: 16 characters), 80 random bits.
	fordStateLen = 16
	// fordCallbackPath is the route Ford sends the owner back to, on this instance.
	fordCallbackPath = "auth/ford/callback"
	// fordPasteRedirect is the redirect address of an instance Ford cannot send back
	// to: a localhost one, which Ford accepts in http, and which nothing needs to answer.
	fordPasteRedirect = "http://localhost/" + fordCallbackPath
)

// fordFlow is a sign-in pending: the account that started it, and the application it
// was started with.
type fordFlow struct {
	account string
	cred    FordCredential
	expires time.Time
}

// fordFlows are the sign-ins pending, by state, one per account: a new one replaces the
// previous. In memory: a restart forgets them, and the owner starts again.
type fordFlows struct {
	mu      sync.Mutex
	pending map[string]fordFlow
	// completing is the state of the sign-in each account is ending, its code being
	// exchanged: one whose connection was removed meanwhile, or that another sign-in
	// replaced, is not stored.
	completing map[string]string
}

// start keeps a sign-in of account with the application cred, and returns its state;
// false when too many are pending.
func (f *fordFlows) start(account string, cred FordCredential, now time.Time) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil {
		f.pending = map[string]fordFlow{}
	}
	for s, p := range f.pending {
		if !now.Before(p.expires) || p.account == account {
			delete(f.pending, s)
		}
	}
	delete(f.completing, account)
	if len(f.pending) >= fordMaxFlows {
		return "", false
	}
	state := rand.Text()[:fordStateLen]
	f.pending[state] = fordFlow{account: account, cred: cred, expires: now.Add(fordFlowTTL)}
	return state, true
}

// finish consumes the sign-in of state, which account started: it is used once, even if
// what follows fails. It is the one the account is ending from then on, until done.
func (f *fordFlows) finish(account, state string, now time.Time) (fordFlow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pending[state]
	delete(f.pending, state)
	if !ok || p.account != account || !now.Before(p.expires) {
		return fordFlow{}, false
	}
	if f.completing == nil {
		f.completing = map[string]string{}
	}
	f.completing[account] = state
	return p, true
}

// cancel forgets the sign-in of state, refused on Ford's page.
func (f *fordFlows) cancel(state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pending, state)
}

// completes reports whether the sign-in of state is still the one account is ending:
// neither its connection removed nor another sign-in started since finish.
func (f *fordFlows) completes(account, state string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.completing[account]
	return ok && s == state
}

// done ends what finish started, stored or not.
func (f *fordFlows) done(account, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.completing[account] == state {
		delete(f.completing, account)
	}
}

// drop forgets the sign-in of account, pending or ending, its connection removed
// meanwhile.
func (f *fordFlows) drop(account string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for s, p := range f.pending {
		if p.account == account {
			delete(f.pending, s)
		}
	}
	delete(f.completing, account)
}

// fordRedirect is the redirect address the owner declares for their application at
// Ford, and whether Ford sends them back to this instance with it: its own callback when
// the instance's address is one Ford accepts (https, or http on localhost), else a
// localhost address whose page the owner pastes back.
func (s *Server) fordRedirect() (uri string, callback bool) {
	u, err := url.Parse(s.AppURL)
	if err != nil || u.Host == "" {
		return fordPasteRedirect, false
	}
	host := u.Hostname()
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme == "https" || (u.Scheme == "http" && local) {
		return s.AppURL + fordCallbackPath, true
	}
	return fordPasteRedirect, false
}

type fordJSON struct {
	Last4     string     `json:"last4" doc:"The last four characters of the application's client ID, to tell it apart on Ford's developer portal: neither it, its secret nor a token is returned."`
	SetAt     time.Time  `json:"set_at" pattern:"Z$" doc:"When the owner connected."`
	ExpiresAt *time.Time `json:"expires_at" pattern:"Z$" doc:"When the grant ends: Ford asks for a new sign-in a year after the owner's. null: not told."`
	RefusedAt *time.Time `json:"refused_at" pattern:"Z$" doc:"When Ford refused the connection (the consent withdrawn, its year over, the application's secret changed): the car is not read until the owner connects again. null: not refused."`
}

type fordApplicationJSON struct {
	RedirectURI string `json:"redirect_uri" format:"uri" doc:"The redirect address to declare for the application on Ford's developer portal: Ford sends the owner back to it after the sign-in."`
	Callback    bool   `json:"callback" doc:"true: the redirect address is this instance's, and the sign-in ends here by itself. false: Ford accepts https, or http on localhost, which this instance's address is not; the redirect address is a localhost one whose page does not load, and the owner pastes its address back (PUT /connection/ford/code)."`
}

type fordAuthorizationUpdateJSON struct {
	// Assumption: a client ID and a secret are printable ASCII without spaces (those
	// seen are a UUID and a token); the bounds only catch a paste gone wrong.
	ClientID     string `json:"client_id" minLength:"8" maxLength:"128" pattern:"^[!-~]+$" doc:"The client ID of the application the owner created on Ford's developer portal."`
	ClientSecret string `json:"client_secret" minLength:"8" maxLength:"512" pattern:"^[!-~]+$" doc:"Its secret, kept sealed to refresh the access. It is never returned."`
}

type fordAuthorizationJSON struct {
	URL         string `json:"url" format:"uri" doc:"Ford's sign-in page, to open in a browser: the owner signs in to Ford, chooses the car and consents."`
	State       string `json:"state" minLength:"1" doc:"The sign-in's state, to send back with the address pasted when callback is false, within 15 minutes."`
	RedirectURI string `json:"redirect_uri" format:"uri" doc:"The redirect address the sign-in was asked with: the application must declare it."`
	Callback    bool   `json:"callback" doc:"true: Ford sends the owner back to this instance, which ends the sign-in. false: the owner pastes back the address Ford sent them to."`
}

type fordCodeJSON struct {
	State string `json:"state" minLength:"1" maxLength:"64" doc:"The state POST /connection/ford/authorization gave."`
	// The address is long (its code and state); the bound only catches a paste gone wrong.
	Callback string `json:"callback" minLength:"1" maxLength:"8192" doc:"The address Ford sent the owner to after the sign-in (http://localhost/auth/ford/callback?code=…), or its code alone."`
}

type fordAuthorizationInput struct {
	Body fordAuthorizationUpdateJSON
}

type fordCodeInput struct {
	Body fordCodeJSON
}

func fordOf(c CredentialConnection) null[fordJSON] {
	if c.SetAt.IsZero() {
		return null[fordJSON]{}
	}
	return known(fordJSON{Last4: c.Last4, SetAt: c.SetAt, ExpiresAt: timeOrNil(c.ExpiresAt), RefusedAt: timeOrNil(c.RefusedAt)})
}

// offerFord refuses the account the flag is off for, or that must first go to a page of
// the web interface.
func (s *Server) offerFord(ctx context.Context, account string) error {
	if !s.enabled(ctx, flags.SourceFord, account) {
		return apiError(http.StatusForbidden, codeFeatureUnavailable, "Ford is not offered to this account")
	}
	return s.admit(ctx, account)
}

// postFordAuthorization starts a sign-in with the owner's application: nothing is
// checked yet, Ford's page tells an application it does not know.
func (s *Server) postFordAuthorization(ctx context.Context, in *fordAuthorizationInput) (*body[fordAuthorizationJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.offerFord(ctx, account); err != nil {
		return nil, err
	}
	redirectURI, callback := s.fordRedirect()
	cred := FordCredential{ClientID: in.Body.ClientID, ClientSecret: in.Body.ClientSecret, RedirectURI: redirectURI}
	state, ok := s.fordFlows.start(account, cred, s.Clock.Now())
	if !ok {
		s.Log.Warn("Ford sign-in not started: too many pending")
		return nil, apiError(http.StatusServiceUnavailable, codeSourceUnavailable, "too many sign-ins are pending, try again later")
	}
	u, err := s.FordAccounts.AuthorizeURL(cred, state)
	if err != nil {
		return nil, s.internal("Ford sign-in address not built", err)
	}
	return respond(fordAuthorizationJSON{URL: u, State: state, RedirectURI: redirectURI, Callback: callback}), nil
}

// fordCodeOf reads the code and the state of what the owner pasted back: the address
// Ford sent them to, or its code alone.
func fordCodeOf(pasted string) (code, state string) {
	pasted = strings.TrimSpace(pasted)
	if u, err := url.Parse(pasted); err == nil && u.Scheme != "" {
		return u.Query().Get("code"), u.Query().Get("state")
	}
	if strings.ContainsAny(pasted, " ?&=/") {
		return "", ""
	}
	return pasted, ""
}

// putFordCode ends a sign-in whose page the owner pastes back.
func (s *Server) putFordCode(ctx context.Context, in *fordCodeInput) (*body[connectionSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.offerFord(ctx, account); err != nil {
		return nil, err
	}
	code, state := fordCodeOf(in.Body.Callback)
	if code == "" {
		return nil, apiError(http.StatusBadRequest, codeInvalidBody, "the address pasted holds no code: paste the whole address of the page Ford sent you to")
	}
	if state != "" && state != in.Body.State {
		return nil, apiError(http.StatusBadRequest, codeAuthorizationExpired, "the address pasted is of another sign-in: start again")
	}
	if err := s.connectFord(ctx, account, in.Body.State, code); err != nil {
		return nil, err
	}
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

// connectFord exchanges the code of the sign-in state names, which is spent even when
// the exchange fails, then stores the connection and adds the grant's car to the
// account. A sign-in that cannot be checked is not stored, nor one without a refresh
// token, nor one whose connection was removed, or that another sign-in replaced, while
// Ford answered: the check and the store are under the account's lock, as the removal.
func (s *Server) connectFord(ctx context.Context, account, state, code string) error {
	fl, ok := s.fordFlows.finish(account, state, s.Clock.Now())
	if !ok {
		return apiError(http.StatusBadRequest, codeAuthorizationExpired, "this sign-in expired or was already used: start again")
	}
	defer s.fordFlows.done(account, state)
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	grant, err := s.FordAccounts.Connect(check, fl.cred, code)
	if err != nil {
		var fe *FordError
		if errors.As(err, &fe) {
			switch fe.Outcome {
			case FordCodeRefused:
				return apiError(http.StatusBadRequest, codeAuthorizationExpired, "Ford refused the code, used already or too old: start again")
			case FordApplicationRefused:
				return apiError(http.StatusBadRequest, codeCredentialRefused, "Ford refused the application's client ID or secret")
			case FordNoVehicle:
				return apiError(http.StatusConflict, codeNoVehicle, "the sign-in reads no car")
			case FordUnavailable:
			}
		}
		s.Log.Warn("Ford sign-in not completed", "err", err)
		return apiError(http.StatusBadGateway, codeSourceUnavailable, "Ford did not answer, try again later")
	}
	if grant.Refresh == "" {
		// Nothing to read the car with once the access is over: a connection that would
		// be refused at its first refresh is not stored.
		s.Log.Warn("Ford sign-in not completed: no refresh token given")
		return apiError(http.StatusBadGateway, codeSourceUnavailable, "Ford gave no refresh token, try again later")
	}
	cred := fl.cred
	cred.Refresh = grant.Refresh
	var refused error
	err = s.Locks.WithAccountLock(ctx, account, func(ctx context.Context) error {
		if !s.fordFlows.completes(account, state) {
			refused = apiError(http.StatusBadRequest, codeAuthorizationExpired,
				"the connection was removed, or another sign-in started, meanwhile: start again")
			return refused
		}
		refused = s.enrollLocked(ctx, account, []string{grant.VIN}, "Ford connection not stored", func(ctx context.Context) (string, error) {
			return s.Ford.SetFord(ctx, account, cred, grant.ExpiresAt, s.Clock.Now()) //nolint:wrapcheck // wrapped by enrollLocked
		})
		return refused
	})
	if refused != nil {
		return refused
	}
	if err != nil {
		return s.internal("account not locked", err)
	}
	s.Log.Info("Ford connected", "account", account)
	return nil
}

// The outcomes the callback comes back to the Connection page with
// (connection?ford=<outcome>), which tells them in Ford's tab.
const (
	fordOutcomeConnected   = "connected"
	fordOutcomeDenied      = "denied"
	fordOutcomeExpired     = "expired"
	fordOutcomeRefused     = "application_refused"
	fordOutcomeNoVehicle   = outcomeNoVehicle
	fordOutcomeTooMany     = outcomeTooManyVehicles
	fordOutcomeUnavailable = "unavailable"
	fordOutcomeNotOffered  = "not_offered"
)

// fordCallback is where Ford sends the owner back, on an instance whose address Ford
// accepts: it ends the sign-in and goes back to the Connection page with its outcome.
func (s *Server) fordCallback(w http.ResponseWriter, r *http.Request, sess auth.Session) {
	back := func(outcome string) {
		w.Header().Set("Location", s.appURL(r, "connection?ford="+outcome))
		w.WriteHeader(http.StatusSeeOther)
	}
	if err := s.offerFord(r.Context(), sess.AccountID); err != nil {
		back(fordOutcomeNotOffered)
		return
	}
	q := r.URL.Query()
	if refusal := q.Get("error"); refusal != "" {
		// The owner refused the consent, or Ford's page failed: the sign-in is spent.
		s.Log.Warn("Ford sign-in refused on Ford's page", "error", truncate(refusal, 64))
		s.fordFlows.cancel(q.Get("state"))
		back(fordOutcomeDenied)
		return
	}
	code := q.Get("code")
	if code == "" {
		back(fordOutcomeExpired)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), callbackTimeout)
	defer cancel()
	err := s.connectFord(ctx, sess.AccountID, q.Get("state"), code)
	var e *errorJSON
	switch {
	case err == nil:
		back(fordOutcomeConnected)
	case !errors.As(err, &e):
		back(fordOutcomeUnavailable)
	case e.Detail.Code == codeAuthorizationExpired:
		back(fordOutcomeExpired)
	case e.Detail.Code == codeCredentialRefused:
		back(fordOutcomeRefused)
	case e.Detail.Code == codeNoVehicle:
		back(fordOutcomeNoVehicle)
	case e.Detail.Code == codeTooManyVehicles:
		back(fordOutcomeTooMany)
	default:
		back(fordOutcomeUnavailable)
	}
}

func (s *Server) deleteFord(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	// Under the account's lock, as a sign-in's store: one ending meanwhile either is
	// dropped here before it stores, or stored first and removed here.
	var failed error
	err := s.Locks.WithAccountLock(ctx, account, func(ctx context.Context) error {
		s.fordFlows.drop(account)
		failed = s.Ford.DeleteFord(ctx, account)
		return failed //nolint:wrapcheck // told below
	})
	if failed != nil {
		return nil, s.internal("Ford connection not deleted", failed)
	}
	if err != nil {
		return nil, s.internal("account not locked", err)
	}
	s.Log.Info("Ford connection deleted", "account", account)
	return &struct{}{}, nil
}

func (s *Server) registerFord(api huma.API) {
	const offered = "`feature_unavailable`: Ford is not offered to the account (the flag `source.ford`). " + errNotAdmitted
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "postFordAuthorization", Method: http.MethodPost, Path: "/connection/ford/authorization",
		Tags: []string{"connection"}, Summary: "Start a sign-in to Ford with the owner's application",
		Description: "The address of Ford's sign-in page, for the application the owner created on Ford's developer portal " +
			"(FordConnect Query): its client ID and secret are kept by Runsten for 15 minutes, in memory, with the sign-in's " +
			"state. The owner signs in to Ford there, chooses the car and consents; Ford then sends them to the redirect " +
			"address, which the application must declare (GET /connection tells it). With callback true it is this " +
			"instance's, which ends the sign-in and comes back to the web interface's Connection page " +
			"(connection?ford=connected, or denied, expired, application_refused, no_vehicle, too_many_vehicles, " +
			"unavailable, not_offered). With callback false the page does not load, and the owner pastes its address " +
			"back (PUT /connection/ford/code). A new sign-in replaces the account's previous one.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The sign-in page.", writeErrors(map[int]string{
		http.StatusBadRequest:         errBody,
		http.StatusForbidden:          offered,
		http.StatusServiceUnavailable: "`source_unavailable`: too many sign-ins are pending on the instance.",
	})), s.postFordAuthorization)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putFordCode", Method: http.MethodPut, Path: "/connection/ford/code", Tags: []string{"connection"},
		Summary: "Connect the account's Ford with the code of a sign-in",
		Description: "The code of the address pasted back is exchanged for the tokens, with the application of the sign-in " +
			"its state names, which is spent. The garage then tells the car the grant reads, which is added to the " +
			"account. The client ID, the secret and the refresh token are kept, sealed, never returned; they replace a " +
			"previous connection, whose car stays with its history.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The connection.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `authorization_expired`: the sign-in expired, was already used, is not the " +
			"address's, Ford refused its code, or the connection was removed or another sign-in started while Ford " +
			"answered. `credential_refused`: Ford refused the application's client ID or secret.",
		http.StatusForbidden: offered,
		http.StatusConflict: "`no_vehicle`: the sign-in reads no car. `too_many_vehicles`: the account would have more " +
			"vehicles than the instance lets it have. Nothing is stored.",
		http.StatusBadGateway: "`source_unavailable`: Ford did not answer, or gave no refresh token; nothing is stored.",
	})), s.putFordCode)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteFord", Method: http.MethodDelete, Path: "/connection/ford", Tags: []string{"connection"},
		Summary:       "Remove the account's Ford connection",
		Description:   "The application's client ID and secret and the refresh token are erased. The car stays, with its history, and is no longer read until the owner connects again.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deleteFord)
}
