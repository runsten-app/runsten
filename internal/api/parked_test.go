package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestParkedJSON(t *testing.T) {
	e, c := readerEnv(t)
	e.clk.Advance(18 * time.Hour) // 23:00 UTC, after the last event
	base := e.api.URL + "/api/v1/vehicles/"

	// Without from nor to: from the first event until now, the sleep states read.
	resp, body := get(t, noFollow(), base+car+"/parked", c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "parked.json", body)

	// The stops are those the statistics sum, over the same period.
	var p struct {
		Items []struct {
			TimeS      int64    `json:"time_s"`
			SoCLossPct *float64 `json:"soc_loss_pct"`
			AsleepS    int64    `json:"asleep_s"`
			From       string   `json:"from"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	_, sb := get(t, noFollow(), base+car+"/stats", c)
	var s stats
	if err := json.Unmarshal([]byte(sb), &s); err != nil {
		t.Fatal(err)
	}
	var timeS, asleep int64
	var loss float64
	unknown := 0
	for i, it := range p.Items {
		timeS += it.TimeS
		asleep += it.AsleepS
		if it.SoCLossPct == nil {
			unknown++
		} else {
			loss += *it.SoCLossPct
		}
		if i > 0 && it.From > p.Items[i-1].From {
			t.Errorf("not newest first: %s after %s", it.From, p.Items[i-1].From)
		}
	}
	if sp := s.Totals.Parked; len(p.Items) == 0 || len(p.Items) != sp.Intervals || timeS != sp.TimeS ||
		loss != sp.SoCLossPct || unknown != sp.SoCLossUnknown || asleep != sp.AsleepS {
		t.Errorf("stops %d, %d s, %g %%, %d unknown, %d s asleep; stats %+v", len(p.Items), timeS, loss, unknown, asleep, sp)
	}

	// A period without an event, and a vehicle without any.
	for _, target := range []string{
		base + car + "/parked?from=2026-09-01T00:00:00Z&to=2026-09-15T00:00:00Z",
		base + parked + "/parked",
	} {
		resp, body := get(t, noFollow(), target, c)
		var empty parkedJSON
		if err := json.Unmarshal([]byte(body), &empty); resp.status != http.StatusOK || err != nil || len(empty.Items) != 0 {
			t.Errorf("%s: %d %s", target, resp.status, body)
		}
	}
	// Errors.
	resp, body = get(t, noFollow(), base+car+"/parked?from=2026-09-28T12:00:00Z&to=2026-09-28T12:00:00Z", c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("from = to: %d %s", resp.status, body)
	}
	wantError(t, body, codeInvalidParameter)
	resp, body = get(t, noFollow(), base+"0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1aff/parked", c)
	if resp.status != http.StatusNotFound {
		t.Errorf("another vehicle: %d %s", resp.status, body)
	}
	e.server.Readings = failingReadings{}
	defer func() { e.server.Readings = e.reader }()
	if resp, body := get(t, noFollow(), base+car+"/parked", c); resp.status != http.StatusInternalServerError {
		t.Errorf("sleep not read: %d %s", resp.status, body)
	}
}
