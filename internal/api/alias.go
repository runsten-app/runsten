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

// VehicleAliases writes the name the user gives a vehicle (*store.Store).
type VehicleAliases interface {
	// SetVehicleAlias writes the vehicle's alias, empty to remove it; ErrNotFound without
	// such a vehicle in the account.
	SetVehicleAlias(ctx context.Context, accountID, vehicleID, alias string) error
}

type vehicleAliasJSON struct {
	Alias *string `json:"alias" maxLength:"40" doc:"The vehicle's name of the user's own, shown wherever the vehicle is named and published to MQTT as its device's name. Spaces around it are removed; it must keep a character, and holds no control character. null removes it: the vehicle is named after its model again."`
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
		Summary: "Name a vehicle",
		Description: "A name of the user's own for the vehicle, which outranks the one derived from its model and the " +
			"name its owner gave it in its vendor's app: the web interface shows it wherever the vehicle is named, and " +
			"the collector publishes it to MQTT as the name of the vehicle's device in Home Assistant, from the " +
			"vehicle's next reading. null removes it.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The vehicle, with its alias.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " Also: an `alias` empty once trimmed, longer than 40 characters, or holding a " +
			"control character.",
		http.StatusNotFound: errVehicle,
	})), s.putAlias)
}
