//go:build !premium

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNoExtensions: the public build registers no route of its own on the mux.
func TestNoExtensions(t *testing.T) {
	mux := http.NewServeMux()
	if err := registerExtensions(t.Context(), extensions{mux: mux}); err != nil {
		t.Fatal(err)
	}
	if _, pattern := mux.Handler(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)); pattern != "" {
		t.Errorf("route %q registered", pattern)
	}
}
