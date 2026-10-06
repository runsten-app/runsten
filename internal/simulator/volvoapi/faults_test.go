package volvoapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

const odometerPath = "/connected-vehicle/v2/vehicles/" + vin + "/odometer"

func newFaultyServer(t *testing.T, limits Limits, faults Faults) (*httptest.Server, *clock.Manual) {
	t.Helper()
	clk := clock.NewManual(t0)
	srv := httptest.NewServer(NewHandler([]Source{fakeSource{report(allSupported())}}, clk, limits, faults, DefaultOAuth()))
	t.Cleanup(srv.Close)
	return srv, clk
}

func TestTokenExpires(t *testing.T) {
	srv, clk := newFaultyServer(t, Limits{TokenTTL: 30 * time.Minute}, Faults{})
	if code, _ := get(t, srv, odometerPath, true); code != http.StatusOK {
		t.Fatalf("fresh token: %d", code)
	}
	clk.Advance(29 * time.Minute)
	if code, _ := get(t, srv, odometerPath, true); code != http.StatusOK {
		t.Fatalf("before expiration: %d", code)
	}
	clk.Advance(time.Minute)
	code, body := get(t, srv, odometerPath, true)
	if code != http.StatusUnauthorized {
		t.Fatalf("after expiration: %d %v", code, body)
	}
	if e, _ := body["error"].(map[string]any); e["description"] != "Access token expired" {
		t.Errorf("body = %v", body)
	}
	// Energy responds in its own format.
	if code, body := get(t, srv, "/energy/v2/vehicles/"+vin+"/state", true); code != http.StatusUnauthorized || body["code"] != "AUTHORIZATION_ERROR" {
		t.Errorf("Energy : %d %v", code, body)
	}
}

func TestOutage(t *testing.T) {
	srv, clk := newFaultyServer(t, DefaultLimits(), Faults{
		Outages: []Outage{{From: t0.Add(time.Hour), To: t0.Add(2 * time.Hour)}},
	})
	steps := []struct {
		advance time.Duration
		want    int
	}{
		{0, http.StatusOK},
		{time.Hour, http.StatusServiceUnavailable}, // start inclusive
		{59 * time.Minute, http.StatusServiceUnavailable},
		{time.Minute, http.StatusOK}, // end exclusive
	}
	for i, st := range steps {
		clk.Advance(st.advance)
		code, body := get(t, srv, odometerPath, true)
		if code != st.want {
			t.Fatalf("step %d: %d, want %d", i, code, st.want)
		}
		if code == http.StatusServiceUnavailable && body["statusCode"] != float64(503) {
			t.Errorf("body = %v", body)
		}
	}
}

func TestRandomErrors(t *testing.T) {
	draws := []float64{0.5, 0.05, 0.4, 0.09, 0.99} // error if < 0.1; the next draw picks the status
	i := 0
	next := func() float64 { v := draws[i%len(draws)]; i++; return v }
	srv, _ := newFaultyServer(t, DefaultLimits(), Faults{ErrorRate: 0.1, Rand: next})

	want := []int{http.StatusOK, http.StatusBadGateway, http.StatusGatewayTimeout}
	for n, w := range want {
		if code, _ := get(t, srv, odometerPath, true); code != w {
			t.Errorf("call %d: %d, want %d", n+1, code, w)
		}
	}
}

func TestErrorsAreNotCounted(t *testing.T) {
	srv, clk := newFaultyServer(t, Limits{DailyQuota: 1}, Faults{
		Outages: []Outage{{From: t0, To: t0.Add(time.Minute)}},
	})
	get(t, srv, odometerPath, true) // outage: not counted
	clk.Advance(time.Minute)
	if code, _ := get(t, srv, odometerPath, true); code != http.StatusOK {
		t.Errorf("quota consumed by a failed call: %d", code)
	}
}

func TestLatency(t *testing.T) {
	srv, _ := newFaultyServer(t, DefaultLimits(), Faults{Latency: 50 * time.Millisecond})
	begin := time.Now() //nolint:forbidigo // measures a real latency
	get(t, srv, odometerPath, true)
	if d := time.Since(begin); d < 50*time.Millisecond {
		t.Errorf("latency not applied: %v", d)
	}

	// A request canceled during the latency does not wait.
	slow, _ := newFaultyServer(t, DefaultLimits(), Faults{Latency: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, slow.URL+odometerPath, http.NoBody)
	if resp, err := slow.Client().Do(req); err == nil {
		_ = resp.Body.Close()
		t.Error("canceled request was served")
	}
}
