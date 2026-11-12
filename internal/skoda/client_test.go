package skoda

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/skodaapi"
	simvehicle "runsten/internal/simulator/vehicle"
)

const simVIN = "TMBJC9NY0RF000001"

// newSim starts the simulator on the Enyaq of demonstration: this is the contract test
// between the client and the fake API. It also answers how its faults are classified.
func newSim(t *testing.T) (*Client, *clock.Manual, *skodaapi.Handler) {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/enyaq.yaml") //nolint:gosec // the test's own scenario
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := scenario.NewSimulation(sc, simvehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	h := skodaapi.NewHandler([]skodaapi.Source{sim}, clk, skodaapi.DevAPIKey, skodaapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+skodaapi.Prefix, srv.Client()), clk, h
}

func TestVehicle(t *testing.T) {
	c, clk, h := newSim(t)
	resp, err := c.Vehicle(context.Background(), skodaapi.DevAPIKey, simVIN, Parts()...)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.ExpiresAt.Equal(h.ExpiresAt()) || !resp.ExpiresAt.After(clk.Now()) {
		t.Errorf("expires at %v, want %v", resp.ExpiresAt, h.ExpiresAt())
	}
	if want := (RateLimit{Limit: 20, Remaining: 19, Reset: time.Hour, Known: true}); resp.Limit != want {
		t.Errorf("limit = %+v, want %+v", resp.Limit, want)
	}
	s, err := Normalize(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !s.SoC.OK || s.SoC.V != 62 || !s.OdometerKm.OK || s.OdometerKm.V != 18420 {
		t.Errorf("SoC %+v, odometer %+v", s.SoC, s.OdometerKm)
	}
	if !s.Engine.OK || s.Engine.V != core.EngineStopped || !s.Position.OK {
		t.Errorf("parked at home: engine %+v, position %+v", s.Engine, s.Position)
	}
	if s.Family.V != "Enyaq" || s.ModelYear.V != 2024 || s.Brand.V != Brand || s.Powertrain.V != core.Electric {
		t.Errorf("model: %q %d %q %q", s.Family.V, s.ModelYear.V, s.Brand.V, s.Powertrain.V)
	}
	if s.Sheet.OK {
		t.Errorf("the model's name is no owner's name: %+v", s.Sheet)
	}

	// Two hours later the car drives: IN_MOTION, the position kept.
	clk.Advance(2*time.Hour + 10*time.Minute)
	resp, err = c.Vehicle(context.Background(), skodaapi.DevAPIKey, simVIN, Parts()...)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Limit.Remaining != 19 {
		t.Errorf("a new hour opens a new window: remaining %d", resp.Limit.Remaining)
	}
	if s, _ := Normalize(resp.Body); s.Engine.V != core.EngineRunning || s.Covers&core.FieldPosition != 0 {
		t.Errorf("driving: engine %+v, covers position %v", s.Engine, s.Covers&core.FieldPosition != 0)
	}
}

func TestVehicleInclude(t *testing.T) {
	c, _, _ := newSim(t)
	resp, err := c.Vehicle(context.Background(), skodaapi.DevAPIKey, simVIN, "info")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(resp.Body), "charging") || !strings.Contains(string(resp.Body), `"name":"Enyaq"`) {
		t.Errorf("info alone: %s", resp.Body)
	}
	_, err = c.Vehicle(context.Background(), skodaapi.DevAPIKey, simVIN, "nonsense")
	var e *APIError
	if !errors.As(err, &e) || e.Kind != KindBadRequest {
		t.Fatalf("an unknown include: %v", err)
	}
}

func TestVehicleErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*skodaapi.Handler)
		key   string
		vin   string
		kind  Kind
		check func(*testing.T, *APIError)
	}{
		{name: "wrong key", key: "another-key", kind: KindKeyRefused},
		{name: "expired", setup: func(h *skodaapi.Handler) { h.ExpireIn(0) }, kind: KindKeyExpired},
		{name: "unknown VIN", vin: "TMBJC9NY0RF000002", kind: KindNotAuthorized},
		{name: "uncovered", setup: func(h *skodaapi.Handler) { h.Uncover(simVIN) }, kind: KindNotAuthorized},
		{
			name: "outage", setup: func(h *skodaapi.Handler) { h.Outage(time.Hour) }, kind: KindOther,
			check: func(t *testing.T, e *APIError) {
				t.Helper()
				if e.Status != http.StatusServiceUnavailable || !e.Limit.Known || e.Limit.Remaining != 19 {
					t.Errorf("a 5xx counts and tells the quota: %+v", e)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, h := newSim(t)
			if tt.setup != nil {
				tt.setup(h)
			}
			key, vin := cmpOr(tt.key, skodaapi.DevAPIKey), cmpOr(tt.vin, simVIN)
			_, err := c.Vehicle(context.Background(), key, vin, Parts()...)
			var e *APIError
			if !errors.As(err, &e) || e.Kind != tt.kind {
				t.Fatalf("err = %v, want kind %d", err, tt.kind)
			}
			if strings.Contains(e.Error(), vin) || strings.Contains(e.Error(), key) {
				t.Errorf("the error tells the VIN or the key: %v", e)
			}
			if got := e.CredentialRefused(); got != (tt.kind == KindKeyExpired || tt.kind == KindKeyRefused) {
				t.Errorf("CredentialRefused = %v", got)
			}
			if got := e.KeyExpired(); got != (tt.kind == KindKeyExpired) {
				t.Errorf("KeyExpired = %v", got)
			}
			if got := e.NotCovered(); got != (tt.kind == KindNotAuthorized) {
				t.Errorf("NotCovered = %v", got)
			}
			if tt.check != nil {
				tt.check(t, e)
			}
		})
	}
}

// TestRateLimit: 20 calls an hour per VIN, then a 429 that waits the window's end; the
// 401s and 403s count in no quota.
func TestRateLimit(t *testing.T) {
	c, clk, _ := newSim(t)
	ctx := context.Background()
	if _, err := c.Vehicle(ctx, "another-key", simVIN); err == nil {
		t.Fatal("another key read")
	}
	for i := range 20 {
		resp, err := c.Vehicle(ctx, skodaapi.DevAPIKey, simVIN, "info")
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		if resp.Limit.Remaining != 19-i {
			t.Fatalf("call %d: remaining %d", i+1, resp.Limit.Remaining)
		}
		clk.Advance(time.Minute)
	}
	_, err := c.Vehicle(ctx, skodaapi.DevAPIKey, simVIN, "info")
	var e *APIError
	if !errors.As(err, &e) || e.Kind != KindRateLimited || e.Problem != "rate-limit-exceeded" {
		t.Fatalf("21st call: %v", err)
	}
	if e.RetryIn != 40*time.Minute {
		t.Errorf("retry in %v, want 40m (the window's end)", e.RetryIn)
	}
	clk.Advance(e.RetryIn)
	if _, err := c.Vehicle(ctx, skodaapi.DevAPIKey, simVIN, "info"); err != nil {
		t.Errorf("past the window: %v", err)
	}
}

// TestErrorsHideTheVIN: neither a transport error nor a problem's message, cut to its
// length, tells the VIN.
func TestErrorsHideTheVIN(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", &http.Client{Timeout: time.Second})
	_, err := c.Vehicle(context.Background(), "key", simVIN)
	if err == nil || strings.Contains(err.Error(), simVIN) {
		t.Errorf("transport error: %v", err)
	}
	long := problem{Title: strings.Repeat("x", 190), Detail: "vehicle " + simVIN}
	if m := long.message(); strings.Contains(m, simVIN[:8]) || len(m) > 200 {
		t.Errorf("message %q", m)
	}
}

func TestRateRetry(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
		limit  RateLimit
		want   time.Duration
	}{
		{"seconds", http.Header{"Retry-After": {"120"}}, RateLimit{}, 2 * time.Minute},
		{"date", http.Header{
			"Retry-After": {"Wed, 11 Nov 2026 10:05:00 GMT"}, "Date": {"Wed, 11 Nov 2026 10:00:00 GMT"},
		}, RateLimit{}, 5 * time.Minute},
		{"reset", http.Header{}, RateLimit{Known: true, Reset: 7 * time.Minute}, 7 * time.Minute},
		{"none", http.Header{}, RateLimit{}, defaultRateRetry},
	}
	for _, tt := range tests {
		if got := rateRetry(tt.header, tt.limit); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHeaders(t *testing.T) {
	h := http.Header{"X-Api-Key-Expires-At": {"2027-03-01T10:00:00+01:00"}}
	if got := expiresAt(h); !got.Equal(time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("expires at %v", got)
	}
	if got := expiresAt(http.Header{"X-Api-Key-Expires-At": {"soon"}}); !got.IsZero() {
		t.Errorf("unreadable: %v", got)
	}
	if got := rateLimitOf(http.Header{"Ratelimit-Limit": {"20"}}); got.Known {
		t.Errorf("no remaining: %+v", got)
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
