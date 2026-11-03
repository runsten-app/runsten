//go:build !premium

package main

import (
	"context"

	"runsten/internal/derive"
)

// registerExtensions adds nothing: the public build is complete on its own. The hosted
// offer's build (-tags premium) replaces this file with one that registers its modules.
func registerExtensions(context.Context, extensions) error { return nil }

// extensionNormalizers adds none: the public build derives the readings of its own
// sources only. The hosted offer's build replaces it with one that adds the normalizers
// of the sources it reads (the provider of a connection the core does not read itself).
func extensionNormalizers() []derive.Normalizer { return nil }
