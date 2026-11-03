//go:build !premium

package main

import "testing"

// TestNoExtensions: the public build needs nothing of the extensions.
func TestNoExtensions(t *testing.T) {
	if err := registerExtensions(t.Context(), extensions{}); err != nil {
		t.Fatal(err)
	}
}

// TestNoExtensionNormalizers: the public build derives the readings of its own sources
// only.
func TestNoExtensionNormalizers(t *testing.T) {
	if n := extensionNormalizers(); n != nil {
		t.Errorf("normalizers %v", n)
	}
}
