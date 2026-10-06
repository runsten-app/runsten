package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"runsten/internal/auth"
	"runsten/internal/oauth"
)

// Connecting a Volvo ID: GET /auth/volvo/start sends the browser to Volvo ID, GET
// /auth/volvo/callback receives the authorization code, exchanges it and records the
// connection in the account of the signed-in user.
//
// Defenses of the flow: a signed-in user at both ends, a single-use state that expires,
// bound to the account that started it and to the browser by a cookie (login CSRF),
// PKCE, and no redirect to an address taken from the request.

// Authorizer is the provider's authorization server.
type Authorizer interface {
	// AuthorizeURL returns the address where the user authorizes the application.
	AuthorizeURL(state, challenge string) string
	// Exchange exchanges an authorization code and its PKCE verifier for tokens.
	Exchange(ctx context.Context, code, verifier string) (oauth.Grant, error)
}

// Admission may hold an account back from connecting a Volvo ID: an extension's, such
// as terms the account has yet to accept. Without one, every account may connect.
type Admission interface {
	// Admit returns where the account must go first, a route of the web interface
	// (as "terms"), from the extension's configuration; empty: it may connect.
	Admit(ctx context.Context, accountID string) (route string, err error)
}

// stateCookie binds a pending flow to the browser that started it.
const stateCookie = "runsten_volvo_state"

// callbackTimeout bounds the code exchange and the enrollment, below the server's
// write timeout.
const callbackTimeout = 20 * time.Second

func (s *Server) start(w http.ResponseWriter, r *http.Request, sess auth.Session) {
	if s.Admission != nil {
		route, err := s.Admission.Admit(r.Context(), sess.AccountID)
		if err != nil {
			s.Log.Error("admission not read", "err", err)
			s.fail(w, r, http.StatusInternalServerError, "Try again later", "The connection could not be started: see the logs.")
			return
		}
		if route != "" {
			w.Header().Set("Location", s.appURL(r, route)) // relative, as backToApp's
			w.WriteHeader(http.StatusSeeOther)
			return
		}
	}
	if !s.InstanceKey {
		// Without a key, the grant would read nothing: ask for it before the consent.
		key, err := s.Keys.AccountKey(r.Context(), sess.AccountID)
		if err != nil {
			s.Log.Error("application key not read", "err", err)
			s.fail(w, r, http.StatusInternalServerError, "Try again later", "The connection could not be started: see the logs.")
			return
		}
		if key.Value == "" {
			s.fail(w, r, http.StatusConflict, "Volvo key required", noKeyMessage)
			return
		}
	}
	state, challenge, err := s.Flows.Start(sess.AccountID)
	if err != nil {
		s.Log.Warn("authorization not started", "err", err)
		s.fail(w, r, http.StatusServiceUnavailable, "Try again later", "Too many authorizations are pending.")
		return
	}
	// No Path: the browser scopes the cookie to the directory of this route, which
	// also works behind a reverse proxy that adds a prefix.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure unless the redirect URI is http on loopback (validated at startup)
		Name: stateCookie, Value: state, HttpOnly: true, Secure: s.SecureCookies,
		SameSite: http.SameSiteLaxMode, // sent on the top-level redirect back from Volvo ID
	})
	// The target comes from the configuration only: no open redirect.
	http.Redirect(w, r, s.Authorizer.AuthorizeURL(state, challenge), http.StatusFound)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request, sess auth.Session) {
	q := r.URL.Query()
	state := q.Get("state")
	cookie, err := r.Cookie(stateCookie)
	http.SetCookie(w, &http.Cookie{Name: stateCookie, MaxAge: -1, HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode}) //nolint:gosec // as in start
	if err != nil || !oauth.SameState(cookie.Value, state) {
		// Either another browser started this flow (login CSRF), or the host differs
		// from the one of /auth/volvo/start (cookies are per host).
		s.fail(w, r, http.StatusBadRequest, "Authorization rejected", "This authorization was not started from this browser. Start again from the Connection page, on the host of the registered redirect URI.")
		return
	}
	verifier, owner, err := s.Flows.Finish(state)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "Authorization rejected", "This authorization expired or was already used. Start again from the Connection page.")
		return
	}
	if owner != sess.AccountID {
		// The user signed out and another one signed in, in the same browser.
		s.fail(w, r, http.StatusBadRequest, "Authorization rejected", "This authorization was started by another user. Start again from the Connection page.")
		return
	}
	if code := q.Get("error"); code != "" {
		s.Log.Warn("authorization refused by Volvo ID", "error", truncate(code, 64))
		s.fail(w, r, http.StatusBadRequest, "Authorization refused", "Volvo ID answered: "+truncate(code, 64)+".")
		return
	}
	code := q.Get("code")
	if code == "" {
		s.fail(w, r, http.StatusBadRequest, "Authorization rejected", "The response of Volvo ID has no authorization code.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), callbackTimeout)
	defer cancel()
	at := s.Clock.Now() // before the exchange: expiries are counted conservatively
	g, err := s.Authorizer.Exchange(ctx, code, verifier)
	if err != nil {
		s.Log.Warn("code exchange failed", "err", err)
		s.fail(w, r, http.StatusBadGateway, "Connection failed", "Volvo ID refused the authorization code. Start again from the Connection page.")
		return
	}
	// The instance's key reads every vehicle; without it, the account's does.
	var key oauth.APIKey
	if !s.InstanceKey {
		if key, err = s.Keys.AccountKey(ctx, sess.AccountID); err != nil {
			s.Log.Error("application key not read", "err", err)
			s.fail(w, r, http.StatusInternalServerError, "Connection failed", "The connection could not be recorded: see the logs.")
			return
		}
	}
	if key.Value == "" && !s.InstanceKey {
		s.fail(w, r, http.StatusConflict, "Volvo key required", noKeyMessage)
		return
	}
	vins, err := oauth.Enroll(ctx, s.Enrollment, s.Vehicles, sess.AccountID, key, oauth.NewCredentials(g, at), s.MaxVehicles)
	if errors.Is(err, oauth.ErrKeyRefused) {
		// The grant is kept: a corrected key lists the vehicles, without a new consent.
		s.Log.Warn("Volvo ID connected, application key refused", "account", sess.AccountID)
		s.backToApp(w, r, outcomeKeyRefused)
		return
	}
	if errors.Is(err, oauth.ErrTooManyVehicles) {
		// The connection in place, if any, stays.
		s.Log.Warn("Volvo ID refused: too many vehicles", "account", sess.AccountID, "max", s.MaxVehicles)
		s.backToApp(w, r, outcomeTooManyVehicles)
		return
	}
	if err != nil {
		s.Log.Error("connection not recorded", "err", err)
		msg := "The connection could not be recorded: see the logs."
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = "Volvo did not answer in time. Start again from the Connection page."
		}
		s.fail(w, r, http.StatusBadGateway, "Connection failed", msg)
		return
	}
	s.Log.Info("Volvo ID connected", "account", sess.AccountID, "vehicles", len(vins),
		"refreshable", g.RefreshToken != "", "expires_in", g.ExpiresIn.String())
	s.backToApp(w, r, outcomeConnected)
}

// The outcomes of a connection the Connection page tells, in its volvo query parameter.
const (
	outcomeConnected       = "connected"
	outcomeKeyRefused      = "key_refused"
	outcomeTooManyVehicles = "too_many_vehicles"
)

// backToApp sends the browser back to the Connection page of the web interface, which
// tells the outcome: the flow was started from it.
func (s *Server) backToApp(w http.ResponseWriter, r *http.Request, outcome string) {
	w.Header().Set("Location", s.appURL(r, "connection?volvo="+outcome))
	w.WriteHeader(http.StatusSeeOther)
}

// appURL is a route of the web interface: under AppURL if configured, else at the root
// of the host of the request, relative to it like redirect (runsten-web relays /auth/
// to runsten-api, under the same prefix).
func (s *Server) appURL(r *http.Request, route string) string {
	if s.AppURL != "" {
		return s.AppURL + route
	}
	return strings.Repeat("../", strings.Count(r.URL.Path, "/")-1) + route
}

// fail renders an error page of the flow, with the way back to the Connection page.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	render(w, status, view{Title: title, Message: message, Back: s.appURL(r, "connection")})
}

// keyRefusedMessage: the grant is kept, a corrected key lists the vehicles.
const keyRefusedMessage = "Your Volvo ID is connected, but Volvo refused your application key. Correct it on the Connection page: the vehicles are listed then, without connecting again."

// tooManyVehiclesMessage: the Volvo ID gives access to more vehicles than the instance
// lets an account have.
const tooManyVehiclesMessage = "Your Volvo ID gives access to more vehicles than this instance lets an account have: it was not connected."

// noKeyMessage asks for the account's application key, on an instance without its own.
const noKeyMessage = "This instance has no Volvo application key of its own: give yours on the Connection page first, then connect your Volvo ID."

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
