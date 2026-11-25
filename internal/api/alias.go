package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
)

// MaxAlias is the most characters a vehicle's alias may have (the store's column checks
// it too): a label of the menu and of Home Assistant's device, not a description.
const MaxAlias = 40

// VehicleAliases writes the alias the user gives a vehicle (*store.Store).
type VehicleAliases interface {
	// SetVehicleAlias writes the vehicle's alias, empty to remove it; ErrNotFound without
	// such a vehicle in the account.
	SetVehicleAlias(ctx context.Context, accountID, vehicleID, alias string) error
}

type vehicleAliasJSON struct {
	Alias *string `json:"alias" maxLength:"40" doc:"The alias: a name the user chooses for the vehicle, used in place of its model's. Spaces around it are removed; it must keep a character, and holds no control character. null removes it."`
}

type vehicleAliasInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	Body    vehicleAliasJSON
}

// aliasOf checks an alias given and gives it as stored: trimmed, empty for none.
func aliasOf(given *string) (string, error) {
	if given == nil {
		return "", nil
	}
	alias := strings.TrimSpace(*given)
	switch {
	case alias == "":
		return "", invalidBody("alias", "empty: null removes the alias")
	case utf8.RuneCountInString(alias) > MaxAlias:
		return "", invalidBody("alias", "too long")
	case strings.ContainsFunc(alias, unicode.IsControl):
		return "", invalidBody("alias", "a control character")
	}
	return alias, nil
}

func (s *Server) putAlias(ctx context.Context, in *vehicleAliasInput) (*body[vehicleJSON], error) {
	alias, err := aliasOf(in.Body.Alias)
	if err != nil {
		return nil, err
	}
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	switch err := s.Aliases.SetVehicleAlias(ctx, sessionFrom(ctx).AccountID, v.ID, alias); {
	case errors.Is(err, ErrNotFound):
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such vehicle")
	case err != nil:
		return nil, s.internal("vehicle alias not written", err)
	}
	v.Alias = alias
	out, err := s.vehicleOf(ctx, v)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

// registerAlias registers the vehicle's alias.
func (s *Server) registerAlias(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putVehicleAlias", Method: http.MethodPut, Path: "/vehicles/{vehicle}/alias", Tags: []string{"vehicles"},
		Summary: "Set a vehicle's alias",
		Description: "The alias is a name the user chooses for the vehicle. It replaces the name derived from the " +
			"model, and the one the owner gave the car in its vendor's app: in the web interface, and as the name of " +
			"the vehicle's device in Home Assistant, from the vehicle's next reading. null removes it.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The vehicle, with its alias.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " Also: an `alias` empty once trimmed, longer than 40 characters, or holding a " +
			"control character.",
		http.StatusNotFound: errVehicle,
	})), s.putAlias)
}
