package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/flags"
)

// The owner's sign-in to their My Renault account (ADR 0050), which reads its Renaults,
// Dacias and Alpines through the My Renault app's API: unofficial, behind the flag
// source.renault, off by default. The owner gives their email and password; Runsten
// signs in once and keeps what Renault gives back — a login token and the device it was
// given to —, never the password. An account that asks for its one-time code gets it by
// email: the sign-in waits for it in memory, ten minutes at most, password included, and
// the code completes it. The account's electric vehicles are then added. Nothing of the
// sign-in is returned, logged nor exported: only the last four characters of the
// person's id, when the owner signed in, and whether Renault refused it since.

// RenaultCredential is what a sign-in gives, which the connection keeps.
type RenaultCredential struct {
	LoginToken, GMID, UCID, PersonID, Country string
}

// RenaultSignIn is what a sign-in comes to: the credential and the account's electric
// vehicles, or a one-time code sent, which RenaultVerify completes.
type RenaultSignIn struct {
	Credential RenaultCredential
	// VINs are the account's electric vehicles; Others how many it has besides
	// (combustion, hybrids), not read.
	VINs   []string
	Others int
	// Pending is set when the account asked for its one-time code: the address it was
	// sent to, masked, and what RenaultVerify takes back.
	Pending *RenaultPending
}

// RenaultPending is a one-time code sent, waiting to be entered.
type RenaultPending struct {
	MaskedMail string
	// State is what the code completes, opaque here: it holds no password.
	State any
}

// RenaultAccounts signs an owner in to My Renault, and lists their vehicles. An error of
// an email and password Renault refuses has a CredentialRefused method that reports
// true; one of a wrong code, a WrongCode method.
type RenaultAccounts interface {
	// RenaultSignIn signs in with the email and password; the code, when the account
	// asks for it, is sent in the language lang.
	RenaultSignIn(ctx context.Context, email, password, lang string) (RenaultSignIn, error)
	// RenaultVerify enters the code of a pending sign-in, and signs in again.
	RenaultVerify(ctx context.Context, state any, email, password, code string) (RenaultSignIn, error)
}

// RenaultCredentials keeps the account's sign-in. Every method is restricted to the
// account.
type RenaultCredentials interface {
	// RenaultConnection returns the account's Renault connection; a zero ID: none.
	RenaultConnection(ctx context.Context, accountID string) (CredentialConnection, error)
	// SetRenaultCredential stores the sign-in, set at at, in the account's Renault
	// connection, created if it has none; a refusal of the previous one is cleared. It
	// returns the connection's ID.
	SetRenaultCredential(ctx context.Context, accountID string, c RenaultCredential, at time.Time) (string, error)
	// DeleteRenaultCredential erases the sign-in: the connection stays, with its
	// vehicles and their history, no longer read until another.
	DeleteRenaultCredential(ctx context.Context, accountID string) error
}

const (
	// renaultCodeLife is how long a sign-in waits for its code: Gigya's code lives
	// minutes (assumption: renault-api tells no lifetime).
	renaultCodeLife = 10 * time.Minute
	// renaultMaxPending caps the sign-ins waiting on the instance.
	renaultMaxPending = 1000
)

// renaultWaiting is a sign-in waiting for its code, in memory: the password with it, to
// sign in again once the code is entered. Its id tells it from a later sign-in of the
// same account, which replaces it.
type renaultWaiting struct {
	id              uint64
	email, password string
	state           any
	expires         time.Time
}

// renaultPending are the sign-ins waiting for their code, one per account: a new
// sign-in replaces the previous. A restart forgets them: the owner signs in again. An
// entry leaves at its expiry by a timer of its own, so that no password outlives its
// code on an idle instance.
type renaultPending struct {
	mu      sync.Mutex
	waiting map[string]renaultWaiting
	// connect makes a sign-in's last step one: taking or dropping the waiting entry,
	// and storing the credential. A verification that took its entry then cannot
	// overwrite a sign-in stored meanwhile, nor the reverse.
	connect sync.Mutex
	seq     uint64
	max     int // 0: renaultMaxPending
}

// put keeps the account's sign-in, and returns it with its id; false when the instance
// has too many waiting.
func (p *renaultPending) put(account string, w renaultWaiting, now time.Time) (renaultWaiting, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.waiting == nil {
		p.waiting = map[string]renaultWaiting{}
	}
	for a, x := range p.waiting {
		if !now.Before(x.expires) {
			delete(p.waiting, a)
		}
	}
	limit := p.max
	if limit == 0 {
		limit = renaultMaxPending
	}
	if _, ok := p.waiting[account]; !ok && len(p.waiting) >= limit {
		return renaultWaiting{}, false
	}
	p.seq++
	w.id = p.seq
	p.waiting[account] = w
	id := w.id // the id alone: the timer must not hold the password
	time.AfterFunc(w.expires.Sub(now), func() { p.take(account, id) })
	return w, true
}

// get returns the account's sign-in waiting, if not expired.
func (p *renaultPending) get(account string, now time.Time) (renaultWaiting, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w, ok := p.waiting[account]
	if ok && !now.Before(w.expires) {
		delete(p.waiting, account)
		return renaultWaiting{}, false
	}
	return w, ok
}

// take forgets the account's sign-in waiting if it is still the one of id, and reports
// whether it was: false, a later sign-in replaced it, or it was dropped.
func (p *renaultPending) take(account string, id uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if w, ok := p.waiting[account]; ok && w.id == id {
		delete(p.waiting, account)
		return true
	}
	return false
}

// drop forgets the account's sign-in waiting, whichever it is.
func (p *renaultPending) drop(account string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.waiting, account)
}

type renaultJSON struct {
	Last4     string     `json:"last4" doc:"The last four characters of the account's person id at Renault, to tell a sign-in apart: the login token is never returned, the password never kept."`
	SetAt     time.Time  `json:"set_at" pattern:"Z$" doc:"When the owner signed in."`
	RefusedAt *time.Time `json:"refused_at" pattern:"Z$" doc:"When Renault refused the sign-in (a password changed, the session ended): its vehicles are not read until the owner signs in again. null: not refused."`
}

func renaultOf(c CredentialConnection) null[renaultJSON] {
	if c.SetAt.IsZero() {
		return null[renaultJSON]{}
	}
	return known(renaultJSON{Last4: c.Last4, SetAt: c.SetAt, RefusedAt: timeOrNil(c.RefusedAt)})
}

type renaultSignInBodyJSON struct {
	Email    string `json:"email" format:"email" maxLength:"254" doc:"The email of the My Renault account (the same for MyDacia and MyAlpine)."`
	Password string `json:"password" minLength:"1" maxLength:"256" doc:"Its password. It signs in once and is never stored."` //nolint:gosec // a request's field, never stored
	Language string `json:"language" pattern:"^[a-z]{2}$" doc:"The language of the one-time code's email, when the account asks for one (fr, en, de…): the user's."`
}

type renaultSignInInput struct {
	Body renaultSignInBodyJSON
}

type renaultCodeBodyJSON struct {
	Code string `json:"code" pattern:"^[0-9]{4,8}$" doc:"The one-time code Renault emailed."`
}

type renaultCodeInput struct {
	Body renaultCodeBodyJSON
}

type renaultSignInJSON struct {
	CodeSentTo *string                      `json:"code_sent_to" doc:"The account asked for its one-time code, sent to this address (masked as Renault tells it): POST /connection/renault/code completes the sign-in, within ten minutes. null: signed in."`
	Connection null[connectionSettingsJSON] `json:"connection" doc:"The connection, once signed in; null while the code is awaited."`
	Others     int                          `json:"others" doc:"How many vehicles of the account are not electric, and not read."`
}

// postRenaultSignIn signs the owner in: either the vehicles are added and the connection
// returned, or the code is sent and awaited.
func (s *Server) postRenaultSignIn(ctx context.Context, in *renaultSignInInput) (*body[renaultSignInJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.renaultOffered(ctx, account); err != nil {
		return nil, err
	}
	lang := in.Body.Language
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	res, err := s.RenaultAccounts.RenaultSignIn(check, in.Body.Email, in.Body.Password, lang)
	if err != nil {
		return nil, s.renaultRefusal(err)
	}
	if res.Pending != nil {
		w := renaultWaiting{email: in.Body.Email, password: in.Body.Password, state: res.Pending.State, expires: s.Clock.Now().Add(renaultCodeLife)}
		if _, ok := s.renaultPending.put(account, w, s.Clock.Now()); !ok {
			return nil, apiError(http.StatusTooManyRequests, codeRateLimited, "too many sign-ins are waiting, try again later")
		}
		s.Log.Info("Renault code sent", "account", account)
		mail := res.Pending.MaskedMail
		return respond(renaultSignInJSON{CodeSentTo: &mail}), nil
	}
	// Signed in without a code: a sign-in still waiting for one is superseded, and its
	// code, entered later, connects nothing.
	s.renaultPending.connect.Lock()
	defer s.renaultPending.connect.Unlock()
	s.renaultPending.drop(account)
	out, err := s.renaultConnect(ctx, account, res)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

// postRenaultCode enters the code of the account's sign-in waiting.
func (s *Server) postRenaultCode(ctx context.Context, in *renaultCodeInput) (*body[renaultSignInJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.renaultOffered(ctx, account); err != nil {
		return nil, err
	}
	w, ok := s.renaultPending.get(account, s.Clock.Now())
	if !ok {
		return nil, apiError(http.StatusConflict, codeAuthorizationExpired, "no sign-in waits for a code, or it expired: sign in again")
	}
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	res, err := s.RenaultAccounts.RenaultVerify(check, w.state, w.email, w.password, in.Body.Code)
	var wrong interface{ WrongCode() bool }
	if errors.As(err, &wrong) && wrong.WrongCode() {
		return nil, apiError(http.StatusBadRequest, codeWrongCode, "Renault refused this code: check it, or sign in again for another")
	}
	// Taken only if still this request's: a sign-in made meanwhile replaced it, or
	// connected without a code, and this one's must not overwrite it.
	s.renaultPending.connect.Lock()
	defer s.renaultPending.connect.Unlock()
	current := s.renaultPending.take(account, w.id)
	if err != nil {
		return nil, s.renaultRefusal(err)
	}
	if !current {
		return nil, apiError(http.StatusConflict, codeAuthorizationExpired, "another sign-in replaced this one: enter its code, or sign in again")
	}
	out, err := s.renaultConnect(ctx, account, res)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

// renaultConnect stores a sign-in and adds the account's electric vehicles.
func (s *Server) renaultConnect(ctx context.Context, account string, res RenaultSignIn) (renaultSignInJSON, error) {
	if len(res.VINs) == 0 {
		return renaultSignInJSON{}, apiError(http.StatusConflict, codeNoVehicle,
			"the My Renault account has no electric vehicle: the cars appear once added in the app")
	}
	if err := s.enroll(ctx, account, res.VINs, "Renault sign-in not stored", func(ctx context.Context) (string, error) {
		return s.Renault.SetRenaultCredential(ctx, account, res.Credential, s.Clock.Now()) //nolint:wrapcheck // wrapped by enroll
	}); err != nil {
		return renaultSignInJSON{}, err
	}
	s.Log.Info("Renault signed in", "account", account, "vehicles", len(res.VINs), "others", res.Others)
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return renaultSignInJSON{}, err
	}
	return renaultSignInJSON{Connection: known(out), Others: res.Others}, nil
}

// renaultRefusal tells the user what Renault's refusal means.
func (s *Server) renaultRefusal(err error) error {
	var refused interface{ CredentialRefused() bool }
	if errors.As(err, &refused) && refused.CredentialRefused() {
		return apiError(http.StatusBadRequest, codeCredentialRefused,
			"Renault refused the email or the password; if they are right, Renault may have changed its app's key")
	}
	s.Log.Warn("Renault sign-in failed", "err", err)
	return apiError(http.StatusBadGateway, codeSourceUnavailable, "My Renault did not answer, try again later")
}

func (s *Server) renaultOffered(ctx context.Context, account string) error {
	if !s.enabled(ctx, flags.SourceRenault, account) {
		return apiError(http.StatusForbidden, codeFeatureUnavailable, "Renault is not offered to this account")
	}
	return s.admit(ctx, account)
}

func (s *Server) deleteRenault(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	s.renaultPending.connect.Lock()
	defer s.renaultPending.connect.Unlock()
	s.renaultPending.drop(account)
	if err := s.Renault.DeleteRenaultCredential(ctx, account); err != nil {
		return nil, s.internal("Renault sign-in not deleted", err)
	}
	s.Log.Info("Renault sign-in deleted", "account", account)
	return &struct{}{}, nil
}

func (s *Server) registerRenault(api huma.API) {
	const offered = "`feature_unavailable`: Renault is not offered to the account (the flag `source.renault`). " + errNotAdmitted
	const refused = errBody + " `credential_refused`: Renault refused the email or the password (or its app's key changed)."
	const unavailable = "`source_unavailable`: My Renault did not answer; nothing is stored."
	const conflicts = "`no_vehicle`: the account has no electric vehicle. `too_many_vehicles`: the account would have more " +
		"vehicles than the instance lets it have. Nothing is stored."
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "postRenaultSignIn", Method: http.MethodPost, Path: "/connection/renault/sign-in", Tags: []string{"connection"},
		Summary: "Sign in to the account's My Renault",
		Description: "The owner's email and password of My Renault (the same account as MyDacia and MyAlpine) sign in " +
			"once: Runsten keeps the login token Renault gives, never the password. When the account asks for its " +
			"one-time code, Renault emails it and `code_sent_to` says where: POST /connection/renault/code completes " +
			"the sign-in. Otherwise the account's electric vehicles are added, and the connection returned. A sign-in " +
			"replaces the previous one, its vehicles kept.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "Signed in, or the code sent.", writeErrors(map[int]string{
		http.StatusBadRequest:      refused,
		http.StatusForbidden:       offered,
		http.StatusConflict:        conflicts,
		http.StatusTooManyRequests: "`rate_limited`: too many sign-ins wait for their code on the instance.",
		http.StatusBadGateway:      unavailable,
	})), s.postRenaultSignIn)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "postRenaultCode", Method: http.MethodPost, Path: "/connection/renault/code", Tags: []string{"connection"},
		Summary:     "Enter the one-time code of the My Renault sign-in",
		Description: "The code Renault emailed completes the sign-in started within ten minutes; the account's electric vehicles are then added.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "Signed in: the connection, and how many vehicles are not electric.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `wrong_code`: Renault refused the code; the sign-in still waits. `credential_refused`: Renault refused the sign-in.",
		http.StatusForbidden:  offered,
		http.StatusConflict:   "`authorization_expired`: no sign-in waits for a code, or it expired. " + conflicts,
		http.StatusBadGateway: unavailable,
	})), s.postRenaultCode)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteRenaultCredential", Method: http.MethodDelete, Path: "/connection/renault", Tags: []string{"connection"},
		Summary:       "Remove the account's My Renault sign-in",
		Description:   "The sign-in is erased. The Renaults stay, with their history, and are no longer read until the owner signs in again.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deleteRenault)
}
