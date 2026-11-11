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

// The owner's key of the Pleos Vehicle Data API, the access Hyundai, Kia and Genesis
// opened to private owners in Europe (the EU Data Act): one per brand, created in the
// Pleos Playground and pasted here. It is checked before it is stored, by asking for a
// token and listing the vehicles, which are then added to the account. The key is never
// returned, logged nor exported: only the last four characters of its client ID, when
// it was given, and whether it was refused. Its validity the API does not tell: the
// Playground does, and a refused key asks for another.

// The brands the API reads, as its Brand header names them and the connection
// providers carry: the brand is the user's tab, never guessed.
const (
	PleosHyundai = "hyundai"
	PleosKia     = "kia"
	PleosGenesis = "genesis"
)

// PleosProvider is the connection provider of a brand's key.
func PleosProvider(brand string) string { return "pleos_" + brand }

// PleosCredential is a key the owner created in the Pleos Playground.
type PleosCredential struct {
	ClientID, ClientSecret string
}

// PleosKeys keeps the account's key of each brand. Every method is restricted to the
// account.
type PleosKeys interface {
	// PleosConnection returns the brand's connection; a zero ID: none.
	PleosConnection(ctx context.Context, accountID, brand string) (CredentialConnection, error)
	// SetPleosCredential stores the key, set at at, in the account's connection of the
	// brand, created if it has none; a refusal of the previous one is cleared. It
	// returns the connection's ID.
	SetPleosCredential(ctx context.Context, accountID, brand string, c PleosCredential, at time.Time) (string, error)
	// DeletePleosCredential erases the brand's key: the connection stays, with its
	// vehicles and their history, no longer read until another.
	DeletePleosCredential(ctx context.Context, accountID, brand string) error
}

// PleosVehicles lists the VINs a key reads. An error of a key Pleos refuses has a
// CredentialRefused method that reports true.
type PleosVehicles interface {
	PleosVehicles(ctx context.Context, brand string, c PleosCredential) ([]string, error)
}

type pleosKeyJSON struct {
	Last4     string     `json:"last4" doc:"The last four characters of the key's client ID, to tell it apart: the key itself, above all its secret, is never returned."`
	SetAt     time.Time  `json:"set_at" pattern:"Z$" doc:"When it was given. The API tells no expiry: the Playground does, and a key it refuses asks for another."`
	RefusedAt *time.Time `json:"refused_at" pattern:"Z$" doc:"When Pleos refused it (revoked or replaced in the Playground, or expired): its vehicles are not read until another. null: not refused."`
}

type pleosKeysJSON struct {
	Hyundai null[pleosKeyJSON] `json:"hyundai" doc:"The account's key of its Hyundais; null: none."`
	Kia     null[pleosKeyJSON] `json:"kia" doc:"The account's key of its Kias; null: none."`
	Genesis null[pleosKeyJSON] `json:"genesis" doc:"The account's key of its Genesises; null: none."`
}

type pleosCredentialUpdateJSON struct {
	// Assumption: the two values are printable ASCII without spaces (those seen are
	// UUIDs and random strings); the bounds only catch a paste gone wrong.
	ClientID     string `json:"client_id" minLength:"4" maxLength:"256" pattern:"^[!-~]+$" doc:"The client ID of a key created in the Pleos Playground, on the tab of its brand."`
	ClientSecret string `json:"client_secret" minLength:"4" maxLength:"512" pattern:"^[!-~]+$" doc:"Its client secret. It is never returned."`
}

type pleosInput struct {
	Brand string `path:"brand" pattern:"^(hyundai|kia|genesis)$" doc:"The brand of the key: the API of each is the same, the key and the cars are not."`
	Body  pleosCredentialUpdateJSON
}

// pleosBrand is the brand of the path alone, for the requests that carry no key.
type pleosBrand struct {
	Brand string `path:"brand" pattern:"^(hyundai|kia|genesis)$" doc:"The brand of the key: the API of each is the same, the key and the cars are not."`
}

func pleosOf(keys map[string]CredentialConnection) pleosKeysJSON {
	of := func(brand string) null[pleosKeyJSON] {
		c, ok := keys[brand]
		if !ok || c.SetAt.IsZero() {
			return null[pleosKeyJSON]{}
		}
		return known(pleosKeyJSON{Last4: c.Last4, SetAt: c.SetAt, RefusedAt: timeOrNil(c.RefusedAt)})
	}
	return pleosKeysJSON{Hyundai: of(PleosHyundai), Kia: of(PleosKia), Genesis: of(PleosGenesis)}
}

// putPleos checks the key, then stores it and adds its vehicles to the account. A key
// Pleos cannot check is not stored: the listing is how its vehicles come in, and none
// would. Checking it asks for a token, which ends the previous one: a collector
// reading the same key asks for another, as it does when any other client takes its
// place.
func (s *Server) putPleos(ctx context.Context, in *pleosInput) (*body[connectionSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	if !s.enabled(ctx, flags.SourcePleos, account) {
		return nil, apiError(http.StatusForbidden, codeFeatureUnavailable, "Hyundai, Kia and Genesis are not offered to this account")
	}
	if err := s.admit(ctx, account); err != nil {
		return nil, err
	}
	brand, cred := in.Brand, PleosCredential{ClientID: in.Body.ClientID, ClientSecret: in.Body.ClientSecret}
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	vins, err := s.PleosVehicles.PleosVehicles(check, brand, cred)
	var refused interface{ CredentialRefused() bool }
	switch {
	case errors.As(err, &refused) && refused.CredentialRefused():
		return nil, apiError(http.StatusBadRequest, codeCredentialRefused, "the Pleos Playground refused this key")
	case err != nil:
		s.Log.Warn("Pleos key not checked", "err", err)
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "the Pleos Vehicle Data API did not answer, try again later")
	case len(vins) == 0:
		return nil, apiError(http.StatusConflict, codeNoVehicle, "the key reads no vehicle: it lists only the cars registered when it was issued, and only with the primary user's account")
	}
	limit, err := s.vehicleCap(ctx, account)
	if err != nil {
		return nil, s.internal("vehicles not read", err)
	}
	if limit.Exceeded(vins) {
		return nil, s.tooManyVehicles()
	}
	id, err := s.Pleos.SetPleosCredential(ctx, account, brand, cred, s.Clock.Now())
	if err != nil {
		return nil, s.internal("Pleos key not stored", err)
	}
	if err := oauth.AddVehicles(ctx, s.Enrollment, account, id, vins); err != nil {
		return nil, s.internal("vehicles not recorded", err)
	}
	s.Log.Info("Pleos key set", "account", account, "brand", brand, "vehicles", len(vins))
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) deletePleos(ctx context.Context, in *pleosBrand) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	if err := s.Pleos.DeletePleosCredential(ctx, account, in.Brand); err != nil {
		return nil, s.internal("Pleos key not deleted", err)
	}
	s.Log.Info("Pleos key deleted", "account", account, "brand", in.Brand)
	return &struct{}{}, nil
}

func (s *Server) registerPleos(api huma.API) {
	const offered = "`feature_unavailable`: Hyundai, Kia and Genesis are not offered to the account (the flag `source.pleos`). " + errNotAdmitted
	const refused = errBody + " `credential_refused`: the Pleos Playground refused the key."
	const conflicts = "`no_vehicle`: the key reads no vehicle, none being registered to the primary user's account " +
		"when it was issued. `too_many_vehicles`: the account would have more vehicles than the instance lets it " +
		"have. The key is not stored."
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putPleosCredential", Method: http.MethodPut, Path: "/connection/pleos/{brand}", Tags: []string{"connection"},
		Summary: "Give the account's Pleos key of a brand",
		Description: "A key the owner created in the Pleos Playground reads the cars of its brand — Hyundai, Kia or " +
			"Genesis — through the Pleos Vehicle Data API, the EU Data Act's access. It is checked first: a token is " +
			"asked for with it and its vehicles are listed. A key the Playground refuses, or that reads no vehicle, is " +
			"not stored; one it accepts replaces the previous one, and its vehicles are added to the account, unless " +
			"they would be more than the instance lets an account have. The values travel in the body only, and are " +
			"never returned.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The connection.", writeErrors(map[int]string{
		http.StatusBadRequest: refused,
		http.StatusForbidden:  offered,
		http.StatusConflict:   conflicts,
		http.StatusBadGateway: "`source_unavailable`: the Pleos Vehicle Data API did not answer; the key is not stored.",
	})), s.putPleos)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deletePleosCredential", Method: http.MethodDelete, Path: "/connection/pleos/{brand}", Tags: []string{"connection"},
		Summary:       "Remove the account's Pleos key of a brand",
		Description:   "The key is erased. The cars of the brand stay, with their history, and are no longer read until another key is given.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deletePleos)
}
