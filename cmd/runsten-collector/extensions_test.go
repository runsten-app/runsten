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

// TestReadProviders: the public build's normalizers read the core's providers, Volvo's
// for an empty one; a provider only an extension reads is not, and is not rebuilt.
func TestReadProviders(t *testing.T) {
	read := readProviders(normalizers())
	for _, p := range []string{"", "volvo", "polestar", "tesla", "polestar_id"} {
		if !read(p) {
			t.Errorf("%q not read", p)
		}
	}
	if read("tesla_fleet") {
		t.Error("tesla_fleet read without its extension")
	}
}
