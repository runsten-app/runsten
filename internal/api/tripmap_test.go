package api

import (
	"encoding/json"
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

// TestTripMapHistory: the trips before the account's history are not drawn, and a period
// wholly before it is empty, from equal to to.
func TestTripMapHistory(t *testing.T) {
	e, c := readerEnv(t)
	e.clk.Advance(18 * time.Hour)
	e.server.Limits = &historyLimit{account: account, from: h(12, 0)}
	read := func(q string) (m struct {
		From, To time.Time
		Trips    int
	},
	) {
		t.Helper()
		resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/trip-map"+q, c)
		if resp.status != http.StatusOK {
			t.Fatalf("%s: %d %s", q, resp.status, body)
		}
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	// The way home only, after noon; the reconstructed trip has no position.
	if m := read(""); m.Trips != 1 || !m.From.Equal(h(12, 0)) {
		t.Errorf("whole history: %+v", m)
	}
	if m := read("?from=2026-09-28T00:00:00Z&to=2026-09-28T06:00:00Z"); m.Trips != 0 || !m.From.Equal(m.To) || !m.To.Equal(h(6, 0)) {
		t.Errorf("before the history: %+v", m)
	}
}
