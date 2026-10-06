package debugui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/platform/clock"
	"runsten/internal/volvo"
)

var t0 = time.Date(2026, 9, 28, 7, 0, 0, 0, time.UTC)

func page(t *testing.T, j *Journal, query string) (int, http.Header, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	j.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/"+query, http.NoBody))
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, rec.Header(), string(body)
}

func call(ep volvo.Endpoint, at time.Time) collector.Call {
	return collector.Call{At: at, VehicleID: "v1", VIN: "YV1SMLT0000DT0001", Endpoint: ep, Status: 200, Stored: true, Payload: []byte(`{"a":1}`)}
}

func TestRingKeepsMostRecent(t *testing.T) {
	j := New(3, clock.NewManual(t0))
	for i := range 5 {
		c := call(volvo.Odometer, t0.Add(time.Duration(i)*time.Second))
		j.Called(c)
	}
	got := j.recent(func(collector.Call) bool { return true })
	if len(got) != 3 || !got[0].At.Equal(t0.Add(4*time.Second)) || !got[2].At.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("recent = %v", got)
	}
}

func TestPage(t *testing.T) {
	clk := clock.NewManual(t0)
	j := New(10, clk)
	j.Observed(collector.VehicleState{
		At: t0, VehicleID: "v1", VIN: "YV1SMLT0000DT0001", Mode: "driving", Engine: "RUNNING",
		Next:       map[volvo.Endpoint]time.Time{volvo.EngineStatus: t0.Add(time.Minute)},
		APIBlocked: map[string]time.Time{volvo.APIEnergy: t0.Add(2 * time.Hour)},
	})
	j.Called(call(volvo.Odometer, t0))
	dup := call(volvo.Location, t0)
	dup.Stored = false
	j.Called(dup)
	j.Called(collector.Call{At: t0, VehicleID: "v1", VIN: "YV1SMLT0000DT0001", Endpoint: volvo.EnergyState, Status: 403, Err: "quota"})
	j.Called(collector.Call{At: t0, VehicleID: "v1", Endpoint: volvo.Doors, Status: 200, Payload: []byte(`{"x":"<script>alert(1)</script>"}`)})

	code, h, body := page(t, j, "")
	if code != http.StatusOK || !strings.HasPrefix(h.Get("Content-Type"), "text/html") ||
		!strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("status %d, headers %v", code, h)
	}
	for _, want := range []string{
		"YV1SMLT0000DT0001", "driving", "RUNNING", "1m0s", "quota energy: 2h0m0s",
		"200 · stored", "200 · duplicate", "403", `&#34;a&#34;: 1`, `http-equiv="refresh"`,
		"connected-vehicle</code> <span class=\"muted\">· 2 / 10000", "energy</code> <span class=\"muted\">· 1 / 10000",
		"just now", `class="on">all`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page without %q", want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Error("raw response not escaped")
	}

	t.Run("filters", func(t *testing.T) {
		_, _, errs := page(t, j, "?errors=1")
		if !strings.Contains(errs, "quota") || strings.Contains(errs, "200 · stored") {
			t.Error("errors filter")
		}
		_, _, loc := page(t, j, "?endpoint=location&pause=1")
		if !strings.Contains(loc, "200 · duplicate") || strings.Contains(loc, "200 · stored") || strings.Contains(loc, `http-equiv="refresh"`) {
			t.Error("endpoint or pause filter")
		}
		_, _, other := page(t, j, "?vehicle=v2")
		if !strings.Contains(other, "No calls.") {
			t.Error("vehicle filter")
		}
	})

	t.Run("other paths", func(t *testing.T) {
		rec := httptest.NewRecorder()
		j.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody))
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST: %d", rec.Code)
		}
		rec = httptest.NewRecorder()
		j.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin", http.NoBody))
		if rec.Code != http.StatusNotFound {
			t.Errorf("/admin: %d", rec.Code)
		}
	})
}

func TestCounters(t *testing.T) {
	clk := clock.NewManual(t0)
	j := New(2, clk)
	old := call(volvo.Odometer, t0.Add(-25*time.Hour))
	old.Duration = 100 * time.Millisecond
	j.Called(old)
	for i := range 3 {
		c := call(volvo.Odometer, t0.Add(time.Duration(i)*time.Minute))
		c.Duration = 400 * time.Millisecond
		c.Stored = i == 0
		j.Called(c)
	}
	j.Called(collector.Call{At: t0, Endpoint: volvo.Location, Status: 429, Err: "rate limited"})
	clk.Advance(time.Hour)

	eps, tot, qs := j.counters()
	if tot != (totals{Calls: 5, Stored: 2, Dups: 2, Errors: 1, ErrorRate: 20}) {
		t.Errorf("totals = %+v", tot)
	}
	if len(eps) != 2 || eps[1].Endpoint != volvo.Odometer || eps[1].Calls != 4 || eps[1].AvgMS() != 325 ||
		eps[0].LastErr != "rate limited" {
		t.Errorf("endpoints = %+v", eps)
	}
	// The call of 25 hours ago is out of the window, although the ring no longer holds any of these.
	want := []quotaView{
		{API: volvo.APIConnectedVehicle, Used: 3, Limit: quotaPerDay},
		{API: volvo.APIEnergy, Limit: quotaPerDay},
		{API: volvo.APILocation, Used: 1, Limit: quotaPerDay},
	}
	if len(qs) != len(want) {
		t.Fatalf("quotas = %+v", qs)
	}
	for i := range want {
		if qs[i] != want[i] {
			t.Errorf("quota %d = %+v, want %+v", i, qs[i], want[i])
		}
	}
	if (endpointStats{}).AvgMS() != 0 || j.ago(time.Time{}) != "never" || j.ago(t0) != "1h0m0s ago" {
		t.Error("helpers")
	}
}

func TestReauthShown(t *testing.T) {
	j := New(5, clock.NewManual(t0))
	j.Observed(collector.VehicleState{At: t0, VehicleID: "v1", VIN: "YV1SMLT0000DT0001", Mode: "parked"})
	j.Observed(collector.VehicleState{
		At: t0, VehicleID: "v2", VIN: "YV1SMLT0000DT0002", Mode: "parked",
		ReauthReason: "re-authentication required: refresh refused: invalid_grant", Key: "refused",
	})
	_, _, body := page(t, j, "")
	for _, want := range []string{"active", "re-authentication required", "invalid_grant", "/auth/volvo/start", "Application key", "refused"} {
		if !strings.Contains(body, want) {
			t.Errorf("page without %q", want)
		}
	}
}

func TestEmptyPageAndHelpers(t *testing.T) {
	j := New(5, clock.NewManual(t0))
	_, _, body := page(t, j, "")
	if !strings.Contains(body, "No vehicle") || !strings.Contains(body, "No calls.") {
		t.Error("empty page")
	}
	if j.until(t0.Add(-time.Second)) != "due" {
		t.Error("until in the past")
	}
	if pretty([]byte("not json")) != "not json" {
		t.Error("pretty on invalid JSON")
	}
}
