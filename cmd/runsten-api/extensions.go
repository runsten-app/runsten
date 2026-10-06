//go:build !premium

package main

import "context"

// registerExtensions adds nothing: the public build is complete on its own. The hosted
// offer's build (-tags premium) replaces this file with one that registers its modules.
func registerExtensions(context.Context, extensions) error { return nil }
