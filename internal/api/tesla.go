package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/flags"
	"runsten/internal/oauth"
)

// The owner's refresh token of Tesla's Owner API, the unofficial API of the Tesla app,
// which a tool such as Tesla Auth gives them: Runsten never asks for the Tesla account's
// password. It is checked before it is stored, by refreshing it and listing the
// vehicles, which are then added to the account. The refresh may give another refresh
// token in exchange, the one given no longer refreshing: that one is stored. Neither is
// returned, logged nor exported: only the last four characters of the one given, when it
// was given, and whether it was refused.

// TeslaConnection is what the user is shown again of the account's refresh token.
type TeslaConnection = CredentialConnection

// TeslaTokens keeps the account's refresh token. Every method is restricted to the
// account.
type TeslaTokens interface {
	TeslaConnection(ctx context.Context, accountID string) (TeslaConnection, error)
	// SetTeslaToken stores token, set at at, in the account's Tesla connection, created
	// if it has none; a refusal of the previous one is cleared. given is the token the
	// owner gave, token the one to refresh with, which a refresh may have given for it;
	// only given's last four characters are kept in plaintext. It returns the
	// connection's ID.
	SetTeslaToken(ctx context.Context, accountID, token, given string, at time.Time) (string, error)
	// DeleteTeslaToken erases it: the connection stays, with its vehicles and their
	// history, no longer read until another.
	DeleteTeslaToken(ctx context.Context, accountID string) error
}

// TeslaVehicles lists the VINs a refresh token reads: a refresh, then the vehicles. It
// returns the refresh token to keep: the one a refresh gave in exchange, or the one
// given. An error of a token Tesla refuses has a TokenRefused method that reports true; one of an account the Owner API is closed to, an APIClosed method.
type TeslaVehicles interface {
	TeslaVehicles(ctx context.Context, refreshToken string) (vins []string, keep string, err error)
}

type teslaTokenJSON struct {
	Last4     string     `json:"last4" doc:"The last four characters of the refresh token given, to tell it apart: the token itself is never returned."`
	SetAt     time.Time  `json:"set_at" pattern:"Z$" doc:"When it was given."`
	RefusedAt *time.Time `json:"refused_at" pattern:"Z$" doc:"When Tesla refused it (signed out of the Tesla account, or the password changed), or closed its Owner API to the account: its vehicles are not read until another. null: not refused."`
}

type teslaTokenUpdateJSON struct {
	// Assumption: a refresh token is printable ASCII without spaces (those seen are
	// JWTs, a few hundred characters); the bounds only catch a paste gone wrong.
	RefreshToken string `json:"refresh_token" minLength:"16" maxLength:"8192" pattern:"^[!-~]+$" doc:"A refresh token of the owner's Tesla account, from Tesla Auth or Auth app for Tesla. It is never returned."`
}

type teslaInput struct {
	Body teslaTokenUpdateJSON
}

func teslaOf(c TeslaConnection) null[teslaTokenJSON] {
	if c.SetAt.IsZero() {
		return null[teslaTokenJSON]{}
	}
	return known(teslaTokenJSON{Last4: c.Last4, SetAt: c.SetAt, RefusedAt: timeOrNil(c.RefusedAt)})
}

// putTesla checks the refresh token, then stores the one to keep and adds its vehicles to
// the account. A token Tesla cannot check is not stored: the listing is how its vehicles
// come in, and none would.
func (s *Server) putTesla(ctx context.Context, in *teslaInput) (*body[connectionSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	if !s.enabled(ctx, flags.SourceTesla, account) {
		return nil, apiError(http.StatusForbidden, codeFeatureUnavailable, "Tesla is not offered to this account")
	}
	if err := s.admit(ctx, account); err != nil {
		return nil, err
	}
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	vins, keep, err := s.TeslaVehicles.TeslaVehicles(check, in.Body.RefreshToken)
	var refused interface{ TokenRefused() bool }
	var closed interface{ APIClosed() bool }
	switch {
	case errors.As(err, &refused) && refused.TokenRefused():
		return nil, apiError(http.StatusBadRequest, codeCredentialRefused, "Tesla refused this refresh token")
	case errors.As(err, &closed) && closed.APIClosed():
		return nil, apiError(http.StatusConflict, codeAPIClosed, "Tesla's Owner API is closed to this account")
	case err != nil:
		s.Log.Warn("Tesla refresh token not checked", "err", err)
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "Tesla did not answer, try again later")
	case len(vins) == 0:
		return nil, apiError(http.StatusConflict, codeNoVehicle, "the Tesla account has no vehicle")
	}
	limit, err := s.vehicleCap(ctx, account)
	if err != nil {
		return nil, s.internal("vehicles not read", err)
	}
	if limit.Exceeded(vins) {
		return nil, s.tooManyVehicles()
	}
	// The token given may no longer refresh once checked: the one to keep is stored,
	// whatever happens next.
	id, err := s.Tesla.SetTeslaToken(ctx, account, keep, in.Body.RefreshToken, s.Clock.Now())
	if err != nil {
		return nil, s.internal("Tesla refresh token not stored", err)
	}
	if err := oauth.AddVehicles(ctx, s.Enrollment, account, id, vins); err != nil {
		return nil, s.internal("vehicles not recorded", err)
	}
	s.Log.Info("Tesla refresh token set", "account", account, "vehicles", len(vins))
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) deleteTesla(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	if err := s.Tesla.DeleteTeslaToken(ctx, account); err != nil {
		return nil, s.internal("Tesla refresh token not deleted", err)
	}
	s.Log.Info("Tesla refresh token deleted", "account", account)
	return &struct{}{}, nil
}

func (s *Server) registerTesla(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putTeslaToken", Method: http.MethodPut, Path: "/connection/tesla", Tags: []string{"connection"},
		Summary: "Give the account's Tesla refresh token",
		Description: "A refresh token of the owner's Tesla account reads their Tesla vehicles through Tesla's Owner API, the " +
			"unofficial API of the Tesla app. It is checked first: it is refreshed and the account's vehicles are listed. A token " +
			"Tesla refuses, or whose account has no vehicle, is not stored; one it accepts replaces the previous one (the refresh " +
			"token Tesla gives in exchange, if it does), and its vehicles are added to the account, unless they would be more than " +
			"the instance lets an account have. The token travels in the body only, and is never returned.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The connection.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `credential_refused`: Tesla refused the refresh token.",
		http.StatusForbidden: "`feature_unavailable`: Tesla is not offered to the account (the flag `source.tesla`). " +
			errNotAdmitted,
		http.StatusConflict: "`api_closed`: Tesla's Owner API is closed to the account. `no_vehicle`: the Tesla account has " +
			"no vehicle. `too_many_vehicles`: the account would have more vehicles than the instance lets it have. The token " +
			"is not stored.",
		http.StatusBadGateway: "`source_unavailable`: Tesla did not answer; the token is not stored.",
	})), s.putTesla)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteTeslaToken", Method: http.MethodDelete, Path: "/connection/tesla", Tags: []string{"connection"},
		Summary:       "Remove the account's Tesla refresh token",
		Description:   "The refresh token is erased. The Tesla vehicles stay, with their history, and are no longer read until another token is given.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deleteTesla)
}
