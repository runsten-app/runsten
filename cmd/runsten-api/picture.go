package main

import (
	"context"
	"fmt"

	"runsten/internal/api"
	"runsten/internal/bmw"
	"runsten/internal/core"
	"runsten/internal/picture"
	"runsten/internal/store"
)

// pictures is the API's Pictures: the relay's.
type pictures struct{ r *picture.Relay }

func (p pictures) Serves(u string) bool { return p.r.Serves(u) }

func (p pictures) Picture(ctx context.Context, u string) (api.Picture, error) {
	pic, err := p.r.Picture(ctx, u)
	if err != nil {
		return api.Picture{}, fmt.Errorf("relay: %w", err)
	}
	return api.Picture{Type: pic.Type, Data: pic.Data}, nil
}

// storedPictures is the API's StoredPictures: BMW's, the latest of its Image responses.
type storedPictures struct{ st *store.Store }

// bmwImage is the kind of response BMW's pictures are stored as.
var bmwImage = core.Kind{Source: bmw.Source, Endpoint: string(bmw.Image)}

func (p storedPictures) HasStoredPicture(ctx context.Context, accountID, vehicleID string) (bool, error) {
	has, err := p.st.HasResponse(ctx, accountID, vehicleID, bmwImage)
	if err != nil {
		return false, fmt.Errorf("stored picture: %w", err)
	}
	return has, nil
}

func (p storedPictures) StoredPicture(ctx context.Context, accountID, vehicleID string) (api.Picture, bool, error) {
	r, ok, err := p.st.LatestResponse(ctx, accountID, vehicleID, bmwImage)
	if err != nil || !ok {
		return api.Picture{}, false, err //nolint:wrapcheck // the store's, which says what failed
	}
	pic, err := bmw.DecodePicture(r.Payload)
	if err != nil {
		return api.Picture{}, false, fmt.Errorf("stored picture: %w", err)
	}
	return api.Picture{Type: pic.Type, Data: pic.Data}, true, nil
}
