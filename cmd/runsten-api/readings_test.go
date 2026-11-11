package main

import (
	"slices"
	"testing"

	"runsten/internal/bmw"
	"runsten/internal/pleos"
	"runsten/internal/polestar"
	"runsten/internal/tesla"
	"runsten/internal/volvo"
)

// TestPrivateFields: every source the derivation reads has its private fields, or the
// readings' CSV file would leave its responses out; Tesla's tokens are among them.
func TestPrivateFields(t *testing.T) {
	got := privateFields()
	for _, source := range []string{volvo.Source, polestar.Source, tesla.Source, bmw.Source, pleos.Source} {
		if _, ok := got[source]; !ok {
			t.Errorf("%s: no private fields", source)
		}
	}
	if !slices.Contains(got[tesla.Source], "response.tokens") {
		t.Errorf("tesla: %v", got[tesla.Source])
	}
}
