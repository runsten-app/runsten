package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/oauth"
)

// The owner's credential of the Polestar Data Portal: an OAuth client they create there,
// its secret and the account it reads (the EU Data Act's machine-to-machine access). It
// is checked before it is stored, by asking for a token and listing the vehicles, which
// are then added to the account. It is never returned, logged nor exported: only the
// last four characters of its client ID, when it was set, and whether it was refused.

// PolestarCredential is a credential of the Data Portal.
type PolestarCredential struct {
	ClientID, ClientSecret, AccountID string
}

// PolestarConnection is what the user is shown again of the account's credential.
type PolestarConnection struct {
	ID        string    // empty: no Polestar connection
	Last4     string    // of the client ID
	SetAt     time.Time // zero: no credential (none set, or removed)
	RefusedAt time.Time // zero: not refused
}

// PolestarCredentials keeps the account's credential. Every method is restricted to the
// account.
type PolestarCredentials interface {
	PolestarConnection(ctx context.Context, accountID string) (PolestarConnection, error)
	// SetPolestarCredential stores the credential, set at at, in the account's Polestar
	// connection, created if it has none; a refusal of the previous one is cleared. It
	// returns the connection's ID.
	SetPolestarCredential(ctx context.Context, accountID string, c PolestarCredential, at time.Time) (string, error)
	// DeletePolestarCredential erases it: the connection stays, with its vehicles and
	// their history, no longer read until another.
	DeletePolestarCredential(ctx context.Context, accountID string) error
}

// PolestarVehicles lists the VINs a credential reads: a token, then the vehicles. An
// error of a credential the Data Portal refuses has a CredentialRefused method that
// reports true.
type PolestarVehicles interface {
	PolestarVehicles(ctx context.Context, c PolestarCredential) ([]string, error)
}

// polestarLifetime is how long a credential reads after it is created, as the Data
// Portal tells (according to the community): the user creates another after it.
const polestarLifetime = 90 * 24 * time.Hour

type polestarCredentialJSON struct {
	Last4     string     `json:"last4" doc:"The last four characters of the credential's client ID, to tell it apart: the credential itself is never returned."`
	SetAt     time.Time  `json:"set_at" pattern:"Z$" doc:"When it was given."`
	ExpiresAt time.Time  `json:"expires_at" pattern:"Z$" doc:"When it expires at the latest: the Data Portal's credentials read for 90 days after they are created (an assumption: according to the community), counted here from when it was given. The user creates another then."`
	RefusedAt *time.Time `json:"refused_at" pattern:"Z$" doc:"When the Data Portal refused it (expired, or deleted on the portal): its vehicles are not read until another. null: not refused."`
}

type polestarCredentialUpdateJSON struct {
	// Assumption: the three values are printable ASCII without spaces (the Data Portal's
	// are UUIDs and random strings); the bounds only catch a paste gone wrong.
	ClientID     string `json:"client_id" minLength:"4" maxLength:"256" pattern:"^[!-~]+$" doc:"The client ID of a credential created on Polestar's Data Portal."`
	ClientSecret string `json:"client_secret" minLength:"4" maxLength:"512" pattern:"^[!-~]+$" doc:"Its client secret. It is never returned."`
	AccountID    string `json:"account_id" minLength:"4" maxLength:"256" pattern:"^[!-~]+$" doc:"The Account ID the portal shows with it, which every call names."`
}

type polestarInput struct {
	Body polestarCredentialUpdateJSON
}

func polestarOf(c PolestarConnection) null[polestarCredentialJSON] {
	if c.SetAt.IsZero() {
		return null[polestarCredentialJSON]{}
	}
	return known(polestarCredentialJSON{
		Last4: c.Last4, SetAt: c.SetAt, ExpiresAt: c.SetAt.Add(polestarLifetime), RefusedAt: timeOrNil(c.RefusedAt),
	})
}

// putPolestar checks the credential, then stores it and adds its vehicles to the
// account. Unlike a Volvo key, a credential the Data Portal cannot check is not stored:
// the listing is how its vehicles come in, and none would.
func (s *Server) putPolestar(ctx context.Context, in *polestarInput) (*body[connectionSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	if err := s.admit(ctx, account); err != nil {
		return nil, err
	}
	cred := PolestarCredential{ClientID: in.Body.ClientID, ClientSecret: in.Body.ClientSecret, AccountID: in.Body.AccountID}
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	vins, err := s.PolestarVehicles.PolestarVehicles(check, cred)
	var r interface{ CredentialRefused() bool }
	switch {
	case errors.As(err, &r) && r.CredentialRefused():
		return nil, apiError(http.StatusBadRequest, codeCredentialRefused, "the Polestar Data Portal refused this credential")
	case err != nil:
		s.Log.Warn("Polestar credential not checked", "err", err)
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "the Polestar Data Portal did not answer, try again later")
	case len(vins) == 0:
		return nil, apiError(http.StatusConflict, codeNoVehicle, "the credential reads no vehicle: share one with it on the Data Portal")
	}
	limit, err := s.vehicleCap(ctx, account)
	if err != nil {
		return nil, s.internal("vehicles not read", err)
	}
	if limit.Exceeded(vins) {
		return nil, s.tooManyVehicles()
	}
	id, err := s.Polestar.SetPolestarCredential(ctx, account, cred, s.Clock.Now())
	if err != nil {
		return nil, s.internal("Polestar credential not stored", err)
	}
	if err := oauth.AddVehicles(ctx, s.Enrollment, account, id, vins); err != nil {
		return nil, s.internal("vehicles not recorded", err)
	}
	s.Log.Info("Polestar credential set", "account", account, "vehicles", len(vins))
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) deletePolestar(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	if err := s.Polestar.DeletePolestarCredential(ctx, account); err != nil {
		return nil, s.internal("Polestar credential not deleted", err)
	}
	s.Log.Info("Polestar credential deleted", "account", account)
	return &struct{}{}, nil
}

func (s *Server) registerPolestar(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putPolestarCredential", Method: http.MethodPut, Path: "/connection/polestar", Tags: []string{"connection"},
		Summary: "Give the account's Polestar credential",
		Description: "A credential the owner created on Polestar's Data Portal reads their Polestar vehicles. It is checked first: " +
			"a token is asked for with it and its vehicles are listed. A credential the portal refuses, or that reads no vehicle, " +
			"is not stored; one it accepts replaces the previous one, and its vehicles are added to the account, unless they would " +
			"be more than the instance lets an account have. The values travel in the body only, and are never returned.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The connection.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `credential_refused`: the Data Portal refused the credential.",
		http.StatusForbidden:  errNotAdmitted,
		http.StatusConflict: "`no_vehicle`: the credential reads no vehicle, none being shared with it on the portal. " +
			"`too_many_vehicles`: the account would have more vehicles than the instance lets it have. The credential is not stored.",
		http.StatusBadGateway: "`source_unavailable`: the Data Portal did not answer; the credential is not stored.",
	})), s.putPolestar)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deletePolestarCredential", Method: http.MethodDelete, Path: "/connection/polestar", Tags: []string{"connection"},
		Summary:       "Remove the account's Polestar credential",
		Description:   "The credential is erased. The Polestar vehicles stay, with their history, and are no longer read until another credential is given.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deletePolestar)
}
