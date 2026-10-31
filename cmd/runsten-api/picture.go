package main

import (
	"context"
	"fmt"

	"runsten/internal/api"
	"runsten/internal/picture"
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
