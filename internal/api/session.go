package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/auth"
)

// maxLoginBody bounds a login request.
const maxLoginBody = 4 << 10

// handler is a page that requires a session.
type handler func(w http.ResponseWriter, r *http.Request, sess auth.Session)

// cookieName is the session cookie. Over https, the __Host- prefix makes the browser
// refuse it unless it is Secure, for the whole host, without Domain: a sibling
// subdomain cannot plant or shadow it.
func (s *Server) cookieName() string {
	if s.SecureCookies {
		return "__Host-runsten_session"
	}
	return "runsten_session"
}

// SessionCookie is the cookie that hands a session's token to the browser, for a
// session opened here or by the hosted offer's sign-in.
func (s *Server) SessionCookie(token string, expires time.Time) http.Cookie {
	return http.Cookie{ //nolint:gosec // Secure unless served over plain http (loopback, SSH tunnel)
		Name: s.cookieName(), Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: s.SecureCookies,
		// Lax, not Strict: the browser must send it back on the redirect from Volvo ID.
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) clearedCookie() http.Cookie {
	return http.Cookie{ //nolint:gosec // as in SessionCookie
		Name: s.cookieName(), Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	c := s.clearedCookie()
	http.SetCookie(w, &c)
}

// session returns the request's session. ok is false without a valid one; err is set
// if it could not be checked.
func (s *Server) session(w http.ResponseWriter, r *http.Request) (sess auth.Session, ok bool, err error) {
	c, err := r.Cookie(s.cookieName())
	if errors.Is(err, http.ErrNoCookie) {
		return auth.Session{}, false, nil
	}
	sess, err = s.Sessions.Authenticate(r.Context(), c.Value)
	switch {
	case errors.Is(err, auth.ErrNoSession):
		s.clearSessionCookie(w)
		return auth.Session{}, false, nil
	case err != nil:
		return auth.Session{}, false, err //nolint:wrapcheck // logged by the caller
	}
	return sess, true, nil
}

// page requires a session, and sends the browser to the login page without one.
func (s *Server) page(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok, err := s.session(w, r)
		switch {
		case err != nil:
			s.Log.Error("session check failed", "err", err)
			render(w, http.StatusInternalServerError, view{Title: "Error", Message: "The session could not be checked: see the logs."})
		case !ok:
			redirect(w, r, "login")
		default:
			h(w, r, sess)
		}
	}
}

// redirect sends the browser to target, a path relative to the root of runsten-api.
// The Location is relative to the request (RFC 9110 §10.2.2): the browser resolves it
// against the URL it used, so that a reverse proxy may serve runsten-api under a
// prefix.
func redirect(w http.ResponseWriter, r *http.Request, target string) {
	up := strings.Repeat("../", strings.Count(r.URL.Path, "/")-1)
	if up == "" && target == "" {
		target = "./"
	}
	w.Header().Set("Location", up+target)
	w.WriteHeader(http.StatusSeeOther)
}

// client identifies the origin of a login attempt, for throttling: the peer address.
// Behind a reverse proxy it is the proxy's, and the username limit still applies.
func client(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// login checks the credentials and returns the session cookie. The caller answers
// failures in its format.
func (s *Server) login(ctx context.Context, username, password, from string) (auth.Session, http.Cookie, error) {
	token, sess, err := s.Sessions.Login(ctx, username, password, from)
	if err != nil {
		return auth.Session{}, http.Cookie{}, err //nolint:wrapcheck // auth errors, tested by the caller
	}
	s.Log.Info("user logged in", "user", sess.UserID, "account", sess.AccountID)
	return sess, s.SessionCookie(token, sess.ExpiresAt), nil
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok, _ := s.session(w, r); ok {
		redirect(w, r, "")
		return
	}
	render(w, http.StatusOK, view{Title: "Sign in", Login: true})
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	if err := r.ParseForm(); err != nil {
		render(w, http.StatusBadRequest, view{Title: "Sign in", Login: true, Error: "The form could not be read."})
		return
	}
	_, cookie, err := s.login(r.Context(), r.PostForm.Get("username"), r.PostForm.Get("password"), client(r.RemoteAddr))
	if te, ok := auth.IsThrottled(err); ok {
		w.Header().Set("Retry-After", retryAfter(te.RetryIn))
		render(w, http.StatusTooManyRequests, view{
			Title: "Sign in", Login: true,
			Error: "Too many failed attempts. Try again in " + te.RetryIn.Round(time.Minute).String() + ".",
		})
		return
	}
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		render(w, http.StatusUnauthorized, view{Title: "Sign in", Login: true, Error: "Invalid username or password."})
	case err != nil:
		s.Log.Error("login failed", "err", err)
		render(w, http.StatusInternalServerError, view{Title: "Sign in", Login: true, Error: "The login failed: see the logs."})
	default:
		http.SetCookie(w, &cookie)
		redirect(w, r, "")
	}
}

func (s *Server) logoutForm(w http.ResponseWriter, r *http.Request) {
	if sess, ok, _ := s.session(w, r); ok {
		if err := s.Sessions.Logout(r.Context(), sess); err != nil {
			s.Log.Error("logout failed", "err", err)
			render(w, http.StatusInternalServerError, view{Title: "Error", Message: "The session could not be ended: see the logs."})
			return
		}
	}
	s.clearSessionCookie(w)
	redirect(w, r, "login")
}

func (s *Server) home(w http.ResponseWriter, _ *http.Request, sess auth.Session) {
	render(w, http.StatusOK, view{Title: "Runsten", Username: sess.Username})
}

// connectionPage stands for the Connection page of the web interface when runsten-api
// is served alone: the outcome of the Volvo ID flow, else the home page.
func (s *Server) connectionPage(w http.ResponseWriter, r *http.Request, sess auth.Session) {
	v := view{Title: "Runsten", Username: sess.Username}
	switch r.URL.Query().Get("volvo") {
	case outcomeConnected:
		v = view{Title: "Volvo ID connected", Message: "Your vehicles are recorded: the collector reads them from its next pass. You can close this page.", Back: "./"}
	case outcomeKeyRefused:
		v = view{Title: "Volvo key refused", Error: keyRefusedMessage, Back: "./"}
	case outcomeTooManyVehicles:
		v = view{Title: "Too many vehicles", Error: tooManyVehiclesMessage, Back: "./"}
	}
	render(w, http.StatusOK, v)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userJSON struct {
	ID       string `json:"id"`
	Username string `json:"username" doc:"Lowercase."`
}

type sessionJSON struct {
	User      userJSON                `json:"user"`
	ExpiresAt time.Time               `json:"expires_at" pattern:"Z$" doc:"The absolute end of the session. It ends sooner after 7 days without a request, or when the password changes."`
	Limits    null[accountLimitsJSON] `json:"limits" doc:"What the account's offer hides, as of now; null: nothing, the whole history is shown. Only an instance that limits its accounts sets it."`
}

// sessionOf is the session, with its account's limits.
func (s *Server) sessionOf(ctx context.Context, sess auth.Session) (sessionJSON, error) {
	l, err := s.limitsOf(ctx, sess.AccountID)
	if err != nil {
		return sessionJSON{}, err
	}
	return sessionJSON{
		User: userJSON{ID: sess.UserID, Username: sess.Username}, ExpiresAt: sess.ExpiresAt, Limits: limitsOfAccount(l),
	}, nil
}

type loginInput struct {
	Body credentials
}

type sessionCreated struct {
	SetCookie http.Cookie `header:"Set-Cookie" doc:"The session cookie, runsten_session (or __Host-runsten_session over https): HttpOnly, SameSite=Lax, Path=/, until the session's absolute expiry."`
	Body      sessionJSON
}

type sessionDeleted struct {
	SetCookie http.Cookie `header:"Set-Cookie" doc:"Clears the session cookie (Max-Age=0)."`
}

type clientKey struct{}

// loginRequest passes on the client address.
func (s *Server) loginRequest(ctx huma.Context, next func(huma.Context)) {
	next(huma.WithValue(ctx, clientKey{}, client(ctx.RemoteAddr())))
}

// createSession is the JSON login.
func (s *Server) createSession(ctx context.Context, in *loginInput) (*sessionCreated, error) {
	from, _ := ctx.Value(clientKey{}).(string)
	sess, cookie, err := s.login(ctx, in.Body.Username, in.Body.Password, from)
	if te, ok := auth.IsThrottled(err); ok {
		tooMany := apiError(http.StatusTooManyRequests, codeTooManyAttempts, "too many failed logins, retry later")
		return nil, huma.ErrorWithHeaders(tooMany, http.Header{"Retry-After": {retryAfter(te.RetryIn)}}) //nolint:wrapcheck // the error of the contract, with its header
	}
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return nil, apiError(http.StatusUnauthorized, codeInvalidCredentials, "invalid username or password")
	case err != nil:
		return nil, s.internal("login failed", err)
	}
	j, err := s.sessionOf(ctx, sess)
	if err != nil {
		return nil, err
	}
	return &sessionCreated{SetCookie: cookie, Body: j}, nil
}

func (s *Server) getSession(ctx context.Context, _ *struct{}) (*body[sessionJSON], error) {
	j, err := s.sessionOf(ctx, sessionFrom(ctx))
	if err != nil {
		return nil, err
	}
	return respond(j), nil
}

func (s *Server) deleteSession(ctx context.Context, _ *struct{}) (*sessionDeleted, error) {
	if err := s.Sessions.Logout(ctx, sessionFrom(ctx)); err != nil {
		return nil, s.internal("logout failed", err)
	}
	return &sessionDeleted{SetCookie: s.clearedCookie()}, nil
}

// registerSession registers the sign-in and sign-out of the JSON API.
func (s *Server) registerSession(api huma.API) {
	login := s.publicOperation(huma.Operation{
		OperationID: "createSession", Method: http.MethodPost, Path: "/session", Tags: []string{"session"},
		Summary: "Sign in",
		Description: "Checks the credentials and sets the session cookie. After 10 failures in 15 minutes for a " +
			"username or a client address, attempts are refused until the end of the window.",
		MaxBodyBytes: maxLoginBody,
		Middlewares:  huma.Middlewares{s.requireJSON, s.loginRequest},
		// No session: an empty requirement, huma omits an empty list.
		Security: []map[string][]string{{}},
	}, "Signed in.", map[int]string{
		http.StatusBadRequest:           "`invalid_body`: not a single JSON object with username and password, an unknown field, or more than 4 KiB.",
		http.StatusUnauthorized:         "`invalid_credentials`: unknown username or wrong password (the same answer for both).",
		http.StatusForbidden:            errCrossOrigin,
		http.StatusUnsupportedMediaType: "`unsupported_media_type`: the body is not `application/json`.",
		http.StatusTooManyRequests:      "`too_many_attempts`: too many failed logins.",
		http.StatusInternalServerError:  errInternal,
	})
	one := 1.0
	login.Responses[strconv.Itoa(http.StatusTooManyRequests)].Headers = map[string]*huma.Param{
		"Retry-After": {
			Description: "Seconds to wait before the next attempt.", Required: true,
			Schema: &huma.Schema{Type: huma.TypeInteger, Minimum: &one},
		},
	}
	huma.Register(api, login, s.createSession)

	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "getSession", Method: http.MethodGet, Path: "/session", Tags: []string{"session"},
		Summary: "The current session",
	}, "The session of the cookie.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errSessionOnly, http.StatusInternalServerError: errInternal,
	}), s.getSession)

	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "deleteSession", Method: http.MethodDelete, Path: "/session", Tags: []string{"session"},
		Summary: "Sign out", Description: "Deletes the session and clears its cookie.",
		DefaultStatus: http.StatusNoContent,
	}, "Signed out.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin + " " + errSessionOnly,
		http.StatusInternalServerError: errInternal,
	}), s.deleteSession)
}

// retryAfter is a Retry-After value in whole seconds, rounded up.
func retryAfter(d time.Duration) string {
	return strconv.Itoa(int((d + time.Second - 1) / time.Second))
}
