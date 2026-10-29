package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// VehicleDeletion deletes a vehicle with its whole history (*store.Store).
type VehicleDeletion interface {
	// DeleteVehicle deletes the vehicle and everything of it; ErrNotFound without such a
	// vehicle in the account. Its connection stays: it may read other vehicles.
	DeleteVehicle(ctx context.Context, accountID, vehicleID string) error
}

func (s *Server) deleteVehicle(ctx context.Context, in *vehicleInput) (*struct{}, error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	account := sessionFrom(ctx).AccountID
	switch err := s.Deletion.DeleteVehicle(ctx, account, v.ID); {
	case errors.Is(err, ErrNotFound):
		// Deleted meanwhile, by another request.
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such vehicle")
	case err != nil:
		return nil, s.internal("vehicle not deleted", err)
	}
	s.Log.Info("vehicle deleted", "account", account, "vehicle", v.ID)
	return &struct{}{}, nil
}

// registerDeletion registers the deletion of a vehicle.
func (s *Server) registerDeletion(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteVehicle", Method: http.MethodDelete, Path: "/vehicles/{vehicle}", Tags: []string{"vehicles"},
		Summary: "Delete a vehicle",
		Description: "The vehicle and its whole history: its readings, trips and charges, derived or imported, the costs " +
			"entered for them, and what the collector reported of it. Nothing of it is kept: the account's export " +
			"(`GET /account/export`) is the way to keep a copy first. Its connection stays, and reads the account's " +
			"other vehicles; a new authorization or credential that lists it again adds it as a new vehicle, " +
			"without its history.",
		DefaultStatus: http.StatusNoContent,
	}, "Deleted.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin,
		http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.deleteVehicle)
}
