package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/flags"
)

// The owner's key of the MyŠkoda Public API, the access Škoda opened to its owners: a key
// created in the MyŠkoda app, bound to the cars ticked then, and pasted here with the VIN
// of each (the API lists no vehicle). Each VIN is checked with the key before it is
// stored, and the vehicles are then added to the account. The key is never returned,
// logged nor exported: only its last four characters, when it was given, when it
// expires (the API tells it on every response), and whether it was refused. Škoda's terms
// allow a personal use only: the flag source.skoda lets an instance serving others turn
// the source off.

// SkodaMaxVINs is how many VINs a key may be given: the MyŠkoda app binds a key to at
// most five vehicles.
const SkodaMaxVINs = 5

// vinPattern is a VIN: 17 characters, without I, O nor Q (ISO 3779).
var vinPattern = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`)

// SkodaKeys keeps the account's key. Every method is restricted to the account.
type SkodaKeys interface {
	// SkodaConnection returns the account's Škoda connection; a zero ID: none.
	SkodaConnection(ctx context.Context, accountID string) (CredentialConnection, error)
	// SetSkodaCredential stores the key, set at at and expiring at expiresAt (zero: not
	// told), in the account's Škoda connection, created if it has none; a refusal of the
	// previous one is cleared. It returns the connection's ID.
	SetSkodaCredential(ctx context.Context, accountID, key string, expiresAt, at time.Time) (string, error)
	// DeleteSkodaCredential erases the key: the connection stays, with its vehicles and
	// their history, no longer read until another.
	DeleteSkodaCredential(ctx context.Context, accountID string) error
}

// SkodaVehicles checks that a key reads a vehicle, and returns when the key expires (zero:
// not told). An error of a key Škoda refuses has a CredentialRefused method that reports
// true, and a KeyExpired one when it expired; one of a vehicle the key does not cover, a
// NotCovered method that reports true.
type SkodaVehicles interface {
	CheckSkoda(ctx context.Context, key, vin string) (time.Time, error)
}

type skodaKeyJSON struct {
	Last4     string     `json:"last4" doc:"The key's last four characters, to tell it apart in the MyŠkoda app: the key itself is never returned."`
	SetAt     time.Time  `json:"set_at" pattern:"Z$" doc:"When it was given."`
	ExpiresAt *time.Time `json:"expires_at" pattern:"Z$" doc:"When it expires, as the API tells on every response: the owner creates or renews a key in the MyŠkoda app before. null: not told yet."`
	RefusedAt *time.Time `json:"refused_at" pattern:"Z$" doc:"When Škoda refused it (expired, or deleted in the app): its vehicles are not read until another. null: not refused."`
}

type skodaCredentialUpdateJSON struct {
	// Assumption: a key is printable ASCII without spaces (its format is not
	// published); the bounds only catch a paste gone wrong.
	APIKey string   `json:"api_key" minLength:"8" maxLength:"512" pattern:"^[!-~]+$" doc:"A key created in the MyŠkoda app (Settings, API keys), with the cars it reads ticked. It is never returned."`
	VINs   []string `json:"vins" minItems:"1" maxItems:"5" uniqueItems:"true" doc:"The VIN of each car the key reads: the API does not list them. 17 characters, without I, O nor Q." nullable:"false"`
}

type skodaInput struct {
	Body skodaCredentialUpdateJSON
}

func skodaOf(c CredentialConnection) null[skodaKeyJSON] {
	if c.SetAt.IsZero() {
		return null[skodaKeyJSON]{}
	}
	return known(skodaKeyJSON{
		Last4: c.Last4, SetAt: c.SetAt, ExpiresAt: timeOrNil(c.ExpiresAt), RefusedAt: timeOrNil(c.RefusedAt),
	})
}

// putSkoda checks the key on each VIN, then stores it and adds the vehicles to the
// account. A key Škoda cannot check is not stored: nothing would tell its vehicles are
// read. A key given to an account with one replaces it, its vehicles kept.
func (s *Server) putSkoda(ctx context.Context, in *skodaInput) (*body[connectionSettingsJSON], error) {
	account := sessionFrom(ctx).AccountID
	if !s.enabled(ctx, flags.SourceSkoda, account) {
		return nil, apiError(http.StatusForbidden, codeFeatureUnavailable, "Škoda is not offered to this account")
	}
	if err := s.admit(ctx, account); err != nil {
		return nil, err
	}
	vins := make([]string, 0, len(in.Body.VINs))
	for _, v := range in.Body.VINs {
		v = strings.ToUpper(strings.TrimSpace(v)) // as pasted from the registration papers
		if !vinPattern.MatchString(v) {
			return nil, apiError(http.StatusBadRequest, codeInvalidBody, "a VIN has 17 characters, letters and digits, without I, O nor Q")
		}
		vins = append(vins, v)
	}
	check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	var expiresAt time.Time
	for _, vin := range vins {
		at, err := s.SkodaVehicles.CheckSkoda(check, in.Body.APIKey, vin)
		var (
			refused interface{ CredentialRefused() bool }
			expired interface{ KeyExpired() bool }
			covered interface{ NotCovered() bool }
		)
		switch {
		case errors.As(err, &expired) && expired.KeyExpired():
			return nil, apiError(http.StatusBadRequest, codeKeyExpired, "this key has expired: create a new one in the MyŠkoda app")
		case errors.As(err, &refused) && refused.CredentialRefused():
			return nil, apiError(http.StatusBadRequest, codeCredentialRefused, "Škoda refused this key")
		case errors.As(err, &covered) && covered.NotCovered():
			return nil, apiError(http.StatusConflict, codeVINNotCovered, "the key does not read the vehicle "+vin+": tick it when creating the key in the MyŠkoda app")
		case err != nil:
			s.Log.Warn("Škoda key not checked", "err", err)
			return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "the MyŠkoda Public API did not answer, try again later")
		}
		if at.After(expiresAt) {
			expiresAt = at
		}
	}
	if err := s.enroll(ctx, account, vins, "Škoda key not stored", func(ctx context.Context) (string, error) {
		return s.Skoda.SetSkodaCredential(ctx, account, in.Body.APIKey, expiresAt, s.Clock.Now()) //nolint:wrapcheck // wrapped by enroll
	}); err != nil {
		return nil, err
	}
	s.Log.Info("Škoda key set", "account", account, "vehicles", len(vins))
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) deleteSkoda(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	if err := s.Skoda.DeleteSkodaCredential(ctx, account); err != nil {
		return nil, s.internal("Škoda key not deleted", err)
	}
	s.Log.Info("Škoda key deleted", "account", account)
	return &struct{}{}, nil
}

func (s *Server) registerSkoda(api huma.API) {
	const offered = "`feature_unavailable`: Škoda is not offered to the account (the flag `source.skoda`). " + errNotAdmitted
	const refused = errBody + " `credential_refused`: Škoda refused the key. `key_expired`: the key has expired."
	const conflicts = "`vin_not_covered`: the key does not read one of the vehicles, which was not ticked when the key " +
		"was created in the MyŠkoda app; the message names it. `too_many_vehicles`: the account would have more " +
		"vehicles than the instance lets it have. The key is not stored."
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putSkodaCredential", Method: http.MethodPut, Path: "/connection/skoda", Tags: []string{"connection"},
		Summary: "Give the account's MyŠkoda key",
		Description: "A key the owner created in the MyŠkoda app reads the cars ticked then through the MyŠkoda " +
			"Public API, with the VIN of each, which the API does not list. The key is checked first on each VIN: " +
			"one Škoda refuses, or that does not read one of them, is not stored; one it accepts replaces the " +
			"previous one, its expiry kept, and the vehicles are added to the account, unless they would be more " +
			"than the instance lets an account have. The key travels in the body only, and is never returned.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The connection.", writeErrors(map[int]string{
		http.StatusBadRequest: refused,
		http.StatusForbidden:  offered,
		http.StatusConflict:   conflicts,
		http.StatusBadGateway: "`source_unavailable`: the MyŠkoda Public API did not answer; the key is not stored.",
	})), s.putSkoda)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteSkodaCredential", Method: http.MethodDelete, Path: "/connection/skoda", Tags: []string{"connection"},
		Summary:       "Remove the account's MyŠkoda key",
		Description:   "The key is erased. The Škodas stay, with their history, and are no longer read until another key is given.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deleteSkoda)
}
