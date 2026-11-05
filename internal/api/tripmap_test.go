package api

import (
	"net/http"
	"testing"
	"time"
)

func TestTripMapJSON(t *testing.T) {
	e, c := readerEnv(t)
	e.clk.Advance(18 * time.Hour) // after the last event
	base := e.api.URL + "/api/v1/vehicles/"
	// The day of the fixtures: home to work and back, one link of two trips; the
	// reconstructed trip has no position.
	resp, body := get(t, noFollow(), base+car+"/trip-map", c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "trip-map.json", body)

	for _, q := range []string{"?from=2026-09-29T00:00:00Z&to=2026-09-28T00:00:00Z", "?from=nope"} {
		resp, body := get(t, noFollow(), base+car+"/trip-map"+q, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", q, resp.status, body)
		}
	}
	resp, body = get(t, noFollow(), base+foreign+"/trip-map", c)
	if resp.status != http.StatusNotFound {
		t.Errorf("foreign: %d %s", resp.status, body)
	}
}
