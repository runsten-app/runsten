//go:build !premium

package main

import "testing"

// TestNoExtensions: the public build needs nothing of the extensions.
func TestNoExtensions(t *testing.T) {
	if err := registerExtensions(t.Context(), extensions{}); err != nil {
		t.Fatal(err)
	}
}
