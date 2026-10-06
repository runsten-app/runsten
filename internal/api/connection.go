package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/oauth"
)

// The account's application key: Volvo counts its quota on the application of the key a
// call is made with (the vcc-api-key header), not on the OAuth client that issued the
// token. On an instance without a key of its own (the hosted offer), each account gives
// the key of an application it created on Volvo's developer portal, unpublished: its
// vehicles spend its own quota. An instance with its own key (self-hosting) reads every
// vehicle with it: an account may not give one. The key is never returned: only its
// last four characters.

// Keys keeps the account's own application key. Every method is restricted to the
// account.
type Keys interface {
	// Connection returns the account's connection; a zero ID: none.
	Connection(ctx context.Context, accountID string) (Connection, error)
	// AccountKey returns the account's own key; zero: none, the instance's applies.
	AccountKey(ctx context.Context, accountID string) (oauth.APIKey, error)
	// SetAPIKey stores the account's own key, set at at, in its connection, created
	// if the Volvo ID is not connected yet; a refusal of the previous key is cleared.
	// It returns the connection's ID.
	SetAPIKey(ctx context.Context, accountID, key string, at time.Time) (string, error)
	// DeleteAPIKey removes it: the instance's applies again.
	DeleteAPIKey(ctx context.Context, accountID string) error
}

// Tokens gives the access token of a connection, refreshed if needed (oauth.Manager).
type Tokens interface {
	Token(ctx context.Context, accountID, connectionID string) (string, error)
}

// Connection is the account's provider connection.
type Connection struct {
	ID           string // empty: none
	Connected    bool   // a Volvo ID is connected: the connection has tokens
	ReauthReason string
	Key          KeyInfo // zero SetAt: the account has no key of its own
}

// KeyInfo is what the user is shown again of their key.
type KeyInfo struct {
	Last4     string
	SetAt     time.Time
	RefusedAt time.Time // zero: not refused
}

type apiKeyJSON struct {
	Last4     string     `json:"last4" doc:"The key's last four characters, to tell it apart: the key itself is never returned."`
	SetAt     time.Time  `json:"set_at" pattern:"Z$" doc:"When it was given."`
	RefusedAt *time.Time `json:"refused_at" pattern:"Z$" doc:"When Volvo refused it (regenerated, or its application deleted on the portal): the vehicles are not read until another key. null: not refused."`
}

type instanceKeyJSON struct {
	Last4 string `json:"last4" doc:"The last four characters of the instance's key (RUNSTEN_VOLVO_API_KEY), to check it is the one of the application: the key itself is never returned."`
}

type connectionSettingsJSON struct {
	Connected   bool                  `json:"connected" doc:"A Volvo ID is connected to the account."`
	ClientID    string                `json:"client_id" doc:"The client ID of the instance's Volvo application (RUNSTEN_VOLVO_CLIENT_ID): the Volvo IDs connect through it, and its secret, never returned, refreshes the tokens."`
	InstanceKey null[instanceKeyJSON] `json:"instance_key" doc:"The instance's own application key, which reads every vehicle: an account may not give one. null: none, the account's key is required, before connecting a Volvo ID."`
	APIKey      null[apiKeyJSON]      `json:"api_key" doc:"The account's own application key; null: none. Not used while instance_key is true."`
}

type apiKeyUpdateJSON struct {
	// Assumption: a key is printable ASCII without spaces (those seen are 32 hex
	// digits); the bounds only catch a paste gone wrong.
	Key string `json:"key" minLength:"16" maxLength:"128" pattern:"^[!-~]+$" doc:"The primary key (vcc-api-key) of an application created on Volvo's developer portal. It is never returned."`
}

type apiKeyInput struct {
	Body apiKeyUpdateJSON
}

// keyCheckTimeout bounds the listing of the vehicles that checks a new key, below the
// server's write timeout.
const keyCheckTimeout = 20 * time.Second

func (s *Server) connectionSettings(ctx context.Context, account string) (connectionSettingsJSON, error) {
	c, err := s.Keys.Connection(ctx, account)
	if err != nil {
		return connectionSettingsJSON{}, s.internal("connection not read", err)
	}
	out := connectionSettingsJSON{Connected: c.Connected, ClientID: s.ClientID}
	if s.InstanceKey {
		out.InstanceKey = known(instanceKeyJSON{Last4: s.InstanceKeyLast4})
	}
	if !c.Key.SetAt.IsZero() {
		out.APIKey = known(apiKeyJSON{Last4: c.Key.Last4, SetAt: c.Key.SetAt, RefusedAt: timeOrNil(c.Key.RefusedAt)})
	}
	return out, nil
}

func (s *Server) getConnection(ctx context.Context, _ *struct{}) (*body[connectionSettingsJSON], error) {
	out, err := s.connectionSettings(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

// putAPIKey stores the account's key. With a Volvo ID connected, the vehicles are listed
// with it first: a key Volvo refuses is not stored, one it accepts records the vehicles,
// those a key refused at the connection could not list. When Volvo cannot tell (no
// token, no response), the key is stored as is: the collector finds out.
func (s *Server) putAPIKey(ctx context.Context, in *apiKeyInput) (*body[connectionSettingsJSON], error) {
	if s.InstanceKey {
		return nil, apiError(http.StatusConflict, codeInstanceKey, "this instance reads every vehicle with its own application key")
	}
	account := sessionFrom(ctx).AccountID
	c, err := s.Keys.Connection(ctx, account)
	if err != nil {
		return nil, s.internal("connection not read", err)
	}
	var vins []string
	if c.Connected && c.ReauthReason == "" {
		check, cancel := context.WithTimeout(ctx, keyCheckTimeout)
		defer cancel()
		token, err := s.Tokens.Token(check, account, c.ID)
		if err == nil {
			vins, err = s.Vehicles.Vehicles(check, in.Body.Key, token)
		}
		var r interface{ KeyRefused() bool }
		switch {
		case errors.As(err, &r) && r.KeyRefused():
			return nil, apiError(http.StatusBadRequest, codeAPIKeyRefused, "Volvo refused this application key")
		case err != nil:
			s.Log.Warn("application key not checked", "err", err)
		}
	}
	if s.MaxVehicles > 0 && len(vins) > s.MaxVehicles {
		return nil, apiError(http.StatusConflict, codeTooManyVehicles,
			fmt.Sprintf("the Volvo ID gives access to more than %d vehicles", s.MaxVehicles))
	}
	id, err := s.Keys.SetAPIKey(ctx, account, in.Body.Key, s.Clock.Now())
	if err != nil {
		return nil, s.internal("application key not stored", err)
	}
	if err := oauth.AddVehicles(ctx, s.Enrollment, account, id, vins); err != nil {
		return nil, s.internal("vehicles not recorded", err)
	}
	s.Log.Info("application key set", "account", account, "vehicles", len(vins))
	out, err := s.connectionSettings(ctx, account)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) deleteAPIKey(ctx context.Context, _ *struct{}) (*struct{}, error) {
	account := sessionFrom(ctx).AccountID
	if err := s.Keys.DeleteAPIKey(ctx, account); err != nil {
		return nil, s.internal("application key not deleted", err)
	}
	s.Log.Info("application key deleted", "account", account)
	return &struct{}{}, nil
}

func (s *Server) registerConnection(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getConnection", Method: http.MethodGet, Path: "/connection", Tags: []string{"connection"},
		Summary:     "The account's connection to Volvo",
		Description: "Whether a Volvo ID is connected, and the application key its vehicles are read with.",
	}, "The connection.", map[int]string{http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal}),
		s.getConnection)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putAPIKey", Method: http.MethodPut, Path: "/connection/api-key", Tags: []string{"connection"},
		Summary: "Give the account's application key",
		Description: "Only on an instance without a key of its own (`instance_key` false). Volvo counts its quota of calls on " +
			"the application of the key: with the account's own, its vehicles spend their own quota. With a Volvo ID connected, " +
			"the vehicles are listed with the key first: a key Volvo refuses is not stored, one it accepts records them, unless " +
			"they are more than the instance lets an account have. The tokens are left alone: no new consent.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The connection.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `api_key_refused`: Volvo refused the key.",
		http.StatusConflict: "`instance_key`: the instance has a key of its own, which reads every vehicle. " +
			"`too_many_vehicles`: the Volvo ID gives access to more vehicles than the instance lets an account have; the key is not stored.",
	})), s.putAPIKey)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteAPIKey", Method: http.MethodDelete, Path: "/connection/api-key", Tags: []string{"connection"},
		Summary:       "Remove the account's application key",
		Description:   "The vehicles are no longer read until another key is given. On an instance with a key of its own, it only removes a key left from before: the instance's reads every vehicle.",
		DefaultStatus: http.StatusNoContent,
	}, "Removed.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deleteAPIKey)
}
