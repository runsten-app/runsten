//go:build !premium

package main

import "context"

// registerExtensions adds nothing: the public build reads every account at the same
// intervals. The hosted offer's build (-tags premium) replaces this file with one that
// sets the collector's read policy.
func registerExtensions(context.Context, extensions) error { return nil }
