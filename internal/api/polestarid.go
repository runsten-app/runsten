package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/flags"
	"runsten/internal/oauth"
)

// The owner's Polestar ID (ADR 0042), which reads the Polestar app's API, unofficial: for
// the Polestars the Data Portal does not serve (outside the EU/EEA), on an instance that
// turns the flag source.polestar-id on. The owner chooses how Runsten connects:
//
//   - token: Runsten gives the address of Polestar's sign-in page, built with a PKCE
//     verifier it keeps (PolestarIDFlows); the owner signs in there and pastes back the
//     address polestar.com's sign-in-callback was given, whose code Runsten exchanges.
//     Only the refresh token is kept.
//   - password: Runsten signs in with the owner's email and password, as the app does, and
//     keeps them to sign in again when the refresh token is refused.
//
// Either is checked before it is stored, by signing in and listing the cars. Nothing of
// it is returned, logged nor exported: only the mode, the last four characters of the
// refresh token the connection gave, when, and whether it was refused.

// PolestarIDCredential is what a Polestar ID connection holds: the refresh token, and in
// the password mode the email and password.
type PolestarIDCredential struct {
	Refresh, Email, Password string
}

// PolestarIDConnection is what the user is shown again of the account's Polestar ID.
type PolestarIDConnection struct {
	CredentialConnection
	Password bool // the password mode: the email and password are kept
}

// PolestarIDCredentials keeps the account's Polestar ID. Every method is restricted to
// the account.
type PolestarIDCredentials interface {
	PolestarIDConnection(ctx context.Context, accountID string) (PolestarIDConnection, error)
	// SetPolestarID stores c, set at at, in the account's Polestar ID connection, created
	// if it has none; a refusal of the previous one is cleared. It returns the
	// connection's ID.
	SetPolestarID(ctx context.Context, accountID string, c PolestarIDCredential, at time.Time) (string, error)
	// DeletePolestarID erases it: the connection stays, with its vehicles and their
	// history, no longer read until another.
	DeletePolestarID(ctx context.Context, accountID string) error
}

// PolestarIDCars is what a sign-in gives: the refresh token to keep, and the VINs of the
// account's cars.
type PolestarIDCars struct {
	Refresh string
	VINs    []string
}

// PolestarIDAccounts signs in to the Polestar ID and lists the cars. An error of a code,
// an email or a password the Polestar ID refuses has a Refused method that reports true.
type PolestarIDAccounts interface {
	// AuthorizeURL is the sign-in page for a PKCE challenge (S256) and a state.
	AuthorizeURL(challenge, state string) string
	// CodeOf reads the code and the state of the address the owner pasted back; an error
	// when it holds no code.
	CodeOf(pasted string) (code, state string, err error)
	Exchange(ctx context.Context, code, verifier string) (PolestarIDCars, error)
	SignIn(ctx context.Context, email, password string) (PolestarIDCars, error)
}

type polestarIDJSON struct {
	Mode      polestarIDModeJSON `json:"mode" doc:"How Runsten connects: token, with the refresh token of a sign-in on Polestar's page; password, with the email and password, kept to sign in again."`
	Last4     string             `json:"last4" doc:"The last four characters of the refresh token the connection gave, to tell it apart: the Polestar ID gives a new one at every refresh, none is returned."`
	SetAt     time.Time          `json:"set_at" pattern:"Z$" doc:"When the owner connected."`
	RefusedAt *time.Time         `json:"refused_at" pattern:"Z$" doc:"When the Polestar ID refused the connection (the session ended, the password changed): its vehicles are not read until the owner connects again. null: not refused."`
}

type polestarIDModeJSON string

// Schema lists the modes.
func (polestarIDModeJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(polestarIDToken, polestarIDPassword)
}

const (
	polestarIDToken    polestarIDModeJSON = "token"
	polestarIDPassword polestarIDModeJSON = "password"
)

func polestarIDOf(c PolestarIDConnection) null[polestarIDJSON] {
	if c.SetAt.IsZero() {
		return null[polestarIDJSON]{}
	}
	mode := polestarIDToken
	if c.Password {
		mode = polestarIDPassword
	}
	return known(polestarIDJSON{Mode: mode, Last4: c.Last4, SetAt: c.SetAt, RefusedAt: timeOrNil(c.RefusedAt)})
}

type polestarIDAuthorizationJSON struct {
	URL   string `json:"url" format:"uri" doc:"Polestar's sign-in page, to open in a browser: once signed in, polestar.com's page shows an error, but the address it was sent to holds the code to paste back."`
	State string `json:"state" minLength:"1" doc:"The authorization's state, to send back with the address pasted, within 10 minutes."`
}

type polestarIDCodeJSON struct {
	State string `json:"state" minLength:"1" maxLength:"256" doc:"The state POST /connection/polestar-id/authorization gave."`
	// The address is long (its code and state); the bound only catches a paste gone wrong.
	Callback string `json:"callback" minLength:"1" maxLength:"4096" doc:"The address polestar.com's sign-in-callback was given after the sign-in (https://www.polestar.com/sign-in-callback?code=…), or its code alone."`
}

type polestarIDPasswordJSON struct {
	Email    string `json:"email" minLength:"3" maxLength:"320" pattern:"^[^\\s@]+@[^\\s@]+$" doc:"The Polestar ID's email."`
	Password string `json:"password" minLength:"1" maxLength:"1024" doc:"Its password, kept sealed to sign in again. It is never returned."`
}

type polestarIDCodeInput struct {
	Body polestarIDCodeJSON
}

type polestarIDPasswordInput struct {
	Body polestarIDPasswordJSON
}

// offerPolestarID refuses the account the flag is off for, or that must first go to a
// page of the web interface.
func (s *Server) offerPolestarID(ctx context.Context, account string) error {
	if !s.enabled(ctx, flags.SourcePolestarID, account) {
		return apiError(http.StatusForbidden, codeFeatureUnavailable, "the Polestar ID is not offered to this account")
	}
	return s.admit(ctx, account)
}

func (s *Server) postPolestarIDAuthorization(ctx context.Context, _ *struct{}) (*body[polestarIDAuthorizationJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.offerPolestarID(ctx, account); err != nil {
		return nil, err
	}
	state, challenge, err := s.PolestarIDFlows.Start(account)
	if err != nil {
		s.Log.Warn("Polestar ID authorization not started", "err", err)
		return nil, apiError(http.StatusServiceUnavailable, codeSourceUnavailable, "too many authorizations are pending, try again later")
	}
	return respond(polestarIDAuthorizationJSON{URL: s.PolestarIDAccounts.AuthorizeURL(challenge, state), State: state}), nil
}

// putPolestarIDCode exchanges the code of the address pasted back with the verifier of
// its authorization. The authorization is spent even when the exchange fails: the owner
// starts again.
func (s *Server) putPolestarIDCode(ctx context.Context, in *polestarIDCodeInput) (*body[connectionSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.offerPolestarID(ctx, account); err != nil {
		return nil, err
	}
	code, state, err := s.PolestarIDAccounts.CodeOf(in.Body.Callback)
	if err != nil {
		return nil, apiError(http.StatusBadRequest, codeInvalidBody, "the address pasted holds no code: paste the whole address of the sign-in-callback")
	}
	if state != "" && !oauth.SameState(state, in.Body.State) {
		return nil, apiError(http.StatusBadRequest, codeAuthorizationExpired, "the address pasted is of another sign-in: start again")
	}
	verifier, owner, err := s.PolestarIDFlows.Finish(in.Body.State)
	if err != nil || owner != account {
		return nil, apiError(http.StatusBadRequest, codeAuthorizationExpired, "this sign-in expired or was already used: start again")
	}
	return s.connectPolestarID(ctx, account, func(ctx context.Context) (PolestarIDCredential, PolestarIDCars, error) {
		cars, err := s.PolestarIDAccounts.Exchange(ctx, code, verifier)
		return PolestarIDCredential{Refresh: cars.Refresh}, cars, err //nolint:wrapcheck // read by connectPolestarID
	})
}

func (s *Server) putPolestarIDPassword(ctx context.Context, in *polestarIDPasswordInput) (*body[connectionSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.offerPolestarID(ctx, account); err != nil {
		return nil, err
	}
	return s.connectPolestarID(ctx, account, func(ctx context.Context) (PolestarIDCredential, PolestarIDCars, error) {
		cars, err := s.PolestarIDAccounts.SignIn(ctx, in.Body.Email, in.Body.Password)
		return PolestarIDCredential{Refresh: cars.Refresh, Email: in.Body.Email, Password: in.Body.Password}, cars, err //nolint:wrapcheck // read by connectPolestarID
	})
}

// connectPolestarID signs in, then stores the credential and adds the account's cars,
// those another connection of the account reads (the Data Portal) left to it. A sign-in
// that cannot be checked is not stored: the listing is how the cars come in.
func (s *Server) connectPolestarID(ctx context.Context, account string,
	signIn func(context.Context) (PolestarIDCredential, PolestarIDCars, error),
) (*body[connectionSettingsJSON], error) {
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	cred, cars, err := signIn(check)
	var r interface{ Refused() bool }
	switch {
	case errors.As(err, &r) && r.Refused():
		return nil, apiError(http.StatusBadRequest, codeCredentialRefused, "the Polestar ID refused the sign-in")
	case err != nil:
		s.Log.Warn("Polestar ID not checked", "err", err)
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "the Polestar ID did not answer, try again later")
	case len(cars.VINs) == 0:
		return nil, apiError(http.StatusConflict, codeNoVehicle, "the Polestar ID has no car")
	}
	others, err := s.otherConnections(ctx, account)
	if err != nil {
		return nil, s.internal("vehicles not read", err)
	}
	vins := slices.DeleteFunc(slices.Clone(cars.VINs), func(vin string) bool { return others[vin] })
	// The refresh token the sign-in gave is the only one that refreshes: it is stored,
	// whatever happens next.
	if err := s.enroll(ctx, account, vins, "Polestar ID not stored", func(ctx context.Context) (string, error) {
		return s.PolestarID.SetPolestarID(ctx, account, cred, s.Clock.Now()) //nolint:wrapcheck // wrapped by enroll
	}); err != nil {
		return nil, err
	}
	s.Log.Info("Polestar ID connected", "account", account, "password", cred.Email != "", "vehicles", len(vins),
		"read_elsewhere", len(cars.VINs)-len(vins))
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

// otherConnections are the VINs of the account another provider than the Polestar ID
// reads.
func (s *Server) otherConnections(ctx context.Context, account string) (map[string]bool, error) {
	vs, err := s.Reader.Vehicles(ctx, account)
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller says what failed
	}
	out := map[string]bool{}
	for _, v := range vs {
		if providerJSON(v.Provider) != providerPolestarID {
			out[v.VIN] = true
		}
	}
	return out, nil
}

func (s *Server) deletePolestarID(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	if err := s.PolestarID.DeletePolestarID(ctx, account); err != nil {
		return nil, s.internal("Polestar ID not deleted", err)
	}
	s.Log.Info("Polestar ID deleted", "account", account)
	return &struct{}{}, nil
}

func (s *Server) registerPolestarID(api huma.API) {
	const offered = "`feature_unavailable`: the Polestar ID is not offered to the account (the flag `source.polestar-id`). " + errNotAdmitted
	const stored = "`no_vehicle`: the Polestar ID has no car. `too_many_vehicles`: the account would have more vehicles than " +
		"the instance lets it have. Nothing is stored."
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "postPolestarIDAuthorization", Method: http.MethodPost, Path: "/connection/polestar-id/authorization",
		Tags: []string{"connection"}, Summary: "Start a sign-in to the Polestar ID",
		Description: "The address of Polestar's sign-in page, for the token mode: its PKCE verifier is kept by Runsten, for 10 " +
			"minutes. Once signed in, polestar.com's sign-in-callback page fails, the code being Runsten's: the owner pastes the " +
			"address it was given back (PUT /connection/polestar-id/code).",
	}, "The sign-in page.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: offered,
		http.StatusServiceUnavailable:  "`source_unavailable`: too many sign-ins are pending on the instance.",
		http.StatusInternalServerError: errInternal,
	}), s.postPolestarIDAuthorization)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putPolestarIDCode", Method: http.MethodPut, Path: "/connection/polestar-id/code", Tags: []string{"connection"},
		Summary: "Connect the account's Polestar ID with the code of a sign-in",
		Description: "The code of the address pasted back is exchanged for the tokens, with the verifier of the sign-in its state " +
			"names, which is spent. The account's cars are then listed, and added to it, but those another connection of the " +
			"account reads (Polestar's Data Portal). Only the refresh token is kept, never returned; it replaces a previous " +
			"connection, of either mode.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The connection.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `authorization_expired`: the sign-in expired, was already used, or is not the " +
			"address's. `credential_refused`: the Polestar ID refused the code.",
		http.StatusForbidden:  offered,
		http.StatusConflict:   stored,
		http.StatusBadGateway: "`source_unavailable`: the Polestar ID did not answer; nothing is stored.",
	})), s.putPolestarIDCode)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putPolestarIDPassword", Method: http.MethodPut, Path: "/connection/polestar-id/password", Tags: []string{"connection"},
		Summary: "Connect the account's Polestar ID with its email and password",
		Description: "Runsten signs in as the Polestar app does, then lists the account's cars and adds them, but those another " +
			"connection of the account reads (Polestar's Data Portal). The email and password are kept, sealed, to sign in " +
			"again when the Polestar ID ends the session; they travel in the body only, and are never returned. It replaces a " +
			"previous connection, of either mode.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The connection.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `credential_refused`: the Polestar ID refused the email or the password.",
		http.StatusForbidden:  offered,
		http.StatusConflict:   stored,
		http.StatusBadGateway: "`source_unavailable`: the Polestar ID did not answer; nothing is stored.",
	})), s.putPolestarIDPassword)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deletePolestarID", Method: http.MethodDelete, Path: "/connection/polestar-id", Tags: []string{"connection"},
		Summary:       "Remove the account's Polestar ID",
		Description:   "The refresh token is erased, and the email and password with it. The cars stay, with their history, and are no longer read until the owner connects again.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deletePolestarID)
}
