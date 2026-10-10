package volvo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
)

const simVIN = "YV1SMLT0000DT0001"

// newSim starts the simulator on the home-work commute day: this is the contract test
// between the client and the fake backend.
func newSim(t *testing.T, limits volvoapi.Limits) (*Client, *clock.Manual) {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/commute.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	srv := httptest.NewServer(volvoapi.NewHandler([]volvoapi.Source{sim}, clk, limits, volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/", "key", srv.Client()), clk
}

func TestVehicles(t *testing.T) {
	c, _ := newSim(t, volvoapi.DefaultLimits())
	vins, err := c.Vehicles(context.Background(), "", "token")
	if err != nil {
		t.Fatal(err)
	}
	if len(vins) != 1 || vins[0] != simVIN {
		t.Fatalf("vins = %v", vins)
	}
}

func TestFetchAllEndpoints(t *testing.T) {
	c, _ := newSim(t, volvoapi.DefaultLimits())
	for _, ep := range []Endpoint{
		Details, EngineStatus, Odometer, Statistics, Diagnostics, Brakes, Engine, Fuel, Tyres, Warnings, Doors, Windows,
		EnergyState, Location,
	} {
		t.Run(string(ep), func(t *testing.T) {
			raw, err := c.Fetch(context.Background(), "", "token", simVIN, ep)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Fingerprint(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParseFromSimulator(t *testing.T) {
	c, clk := newSim(t, volvoapi.DefaultLimits())
	ctx := context.Background()

	status := func() (string, string) {
		t.Helper()
		es, err := c.Fetch(ctx, "", "token", simVIN, EngineStatus)
		if err != nil {
			t.Fatal(err)
		}
		en, err := c.Fetch(ctx, "", "token", simVIN, EnergyState)
		if err != nil {
			t.Fatal(err)
		}
		engine, err := ParseEngineStatus(es)
		if err != nil {
			t.Fatal(err)
		}
		charging, err := ParseChargingStatus(en)
		if err != nil {
			t.Fatal(err)
		}
		return engine, charging
	}

	if e, _ := status(); e != "STOPPED" {
		t.Errorf("at start: engine = %q", e)
	}
	clk.Advance(2*time.Hour + 10*time.Minute) // on the way to work
	if e, _ := status(); e != "RUNNING" {
		t.Errorf("while driving: engine = %q", e)
	}
	clk.Advance(11*time.Hour + 30*time.Minute) // back home, charging
	if e, ch := status(); e != "STOPPED" || ch != "CHARGING" {
		t.Errorf("while charging: engine = %q, charging = %q", e, ch)
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		setup func(t *testing.T) (*Client, string)
		kind  Kind
		retry time.Duration
	}{
		{"missing token", func(t *testing.T) (*Client, string) {
			c, _ := newSim(t, volvoapi.DefaultLimits())
			return c, ""
		}, KindUnauthorized, 0},
		{"rate limit", func(t *testing.T) (*Client, string) {
			c, _ := newSim(t, volvoapi.Limits{PerMinute: 1})
			_, _ = c.Fetch(ctx, "", "token", simVIN, Odometer)
			return c, "token"
		}, KindRateLimited, 61 * time.Second}, // the simulator rounds up to the next second
		{"quota", func(t *testing.T) (*Client, string) {
			c, _ := newSim(t, volvoapi.Limits{DailyQuota: 1})
			_, _ = c.Fetch(ctx, "", "token", simVIN, Odometer)
			return c, "token"
		}, KindQuota, 19 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, token := tt.setup(t)
			_, err := c.Fetch(ctx, "", token, simVIN, Odometer)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want APIError", err)
			}
			if apiErr.Kind != tt.kind || apiErr.RetryIn != tt.retry {
				t.Errorf("kind = %v, retry = %v; want %v, %v", apiErr.Kind, apiErr.RetryIn, tt.kind, tt.retry)
			}
		})
	}

	c, _ := newSim(t, volvoapi.DefaultLimits())
	_, err := c.Fetch(ctx, "", "token", "YV1UNKNOWN0000000", Odometer)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != KindNotFound {
		t.Errorf("unknown VIN: err = %v", err)
	}
}

func TestRetryParsing(t *testing.T) {
	if got := rateRetry("12", ""); got != 12*time.Second {
		t.Errorf("Retry-After: %v", got)
	}
	if got := rateRetry("", "Rate limit is exceeded. Try again in 7 seconds."); got != 7*time.Second {
		t.Errorf("message: %v", got)
	}
	if got := rateRetry("", "?"); got != defaultRateRetry {
		t.Errorf("default: %v", got)
	}
	if got := quotaRetry("?"); got != defaultQuotaRetry {
		t.Errorf("default quota: %v", got)
	}
}

func TestTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "key", srv.Client())
	if _, err := c.Fetch(context.Background(), "", "t", simVIN, Odometer); err == nil {
		t.Error("invalid JSON accepted")
	}
	if _, err := c.Vehicles(context.Background(), "", "t"); err == nil {
		t.Error("invalid list accepted")
	}

	srv.Close()
	if _, err := c.Fetch(context.Background(), "", "t", simVIN, Odometer); err == nil {
		t.Error("server stopped: expected error")
	}

	e500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer e500.Close()
	_, err := NewClient(e500.URL, "key", e500.Client()).Fetch(context.Background(), "", "t", simVIN, Odometer)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != KindOther || apiErr.Message != "boom" {
		t.Errorf("500: %v", err)
	}
}

// TestKeys: a call names its application key, or takes the instance's; without either,
// nothing is sent. A key the gateway refuses is told apart from a refused token.
func TestKeys(t *testing.T) {
	ctx := context.Background()
	limits := volvoapi.DefaultLimits()
	limits.Keys = []string{"key", "own-key"}
	c, _ := newSim(t, limits)
	if !c.HasKey() {
		t.Error("the instance's key is not known")
	}
	if _, err := c.Fetch(ctx, "own-key", "token", simVIN, Odometer); err != nil {
		t.Errorf("own key: %v", err)
	}
	for _, key := range []string{"", "own-key"} {
		if _, err := c.Vehicles(ctx, key, "token"); err != nil {
			t.Errorf("vehicles with key %q: %v", key, err)
		}
	}
	_, err := c.Fetch(ctx, "revoked-key", "token", simVIN, Odometer)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != KindKeyRefused || !apiErr.KeyRefused() || apiErr.Status != 401 {
		t.Errorf("revoked key: %v", err)
	}
	if strings.Contains(err.Error(), "revoked-key") {
		t.Errorf("the key is in the error: %v", err)
	}

	none := NewClient("http://127.0.0.1:1", "", http.DefaultClient)
	if _, err := none.Fetch(ctx, "", "token", simVIN, Odometer); !errors.Is(err, ErrNoKey) || none.HasKey() {
		t.Errorf("no key: %v", err)
	}
}

func TestKeyRefusedMessages(t *testing.T) {
	for _, tt := range []struct {
		status int
		body   string
		want   Kind
	}{
		{401, `{"statusCode":401,"message":"Access denied due to invalid subscription key. Make sure to provide a valid key for an active subscription."}`, KindKeyRefused},
		{401, `{"statusCode":401,"message":"Access denied due to missing subscription key."}`, KindKeyRefused},
		{401, `{"statusCode":401,"message":"Access denied due to invalid VCC-API-KEY."}`, KindKeyRefused},
		// Observed on the real API.
		{401, `{"status":401,"error":{"message":"Access denied due to invalid VCC-API-KEY. Make sure to provide a valid key for an active application."}}`, KindKeyRefused},
		{401, `{"status":401,"error":{"message":"Access denied due to missing header VCC-API-KEY. Make sure to provide a valid key for an active application."}}`, KindKeyRefused},
		{403, `{"statusCode":403,"message":"Invalid API key."}`, KindKeyRefused},
		{401, `{"error":{"message":"UNAUTHORIZED","description":"Access token expired"}}`, KindUnauthorized},
		{403, `{"statusCode":403,"message":"Out of call volume quota. Quota will be replenished in 01:00:00."}`, KindQuota},
		{403, `{"error":{"message":"FORBIDDEN","description":"Not allowed"}}`, KindOther},
	} {
		e := newAPIError("x", &http.Response{StatusCode: tt.status, Header: http.Header{}}, []byte(tt.body))
		if e.Kind != tt.want {
			t.Errorf("%d %s: kind %v, want %v", tt.status, tt.body, e.Kind, tt.want)
		}
	}
}

// TestKeyRefusedRetried: a key Volvo refuses now and then is tried again, up to
// keyAttempts calls; one refused every time is refused; other errors are not retried.
func TestKeyRefusedRetried(t *testing.T) {
	const refusal = `{"status":401,"error":{"message":"Access denied due to invalid VCC-API-KEY. Make sure to provide a valid key for an active application."}}`
	for name, tt := range map[string]struct {
		refused, status int // calls refused first, then the status answered
		wantCalls       int
		wantErr         bool
		wantKind        Kind
	}{
		"accepted at the last attempt": {keyAttempts - 1, http.StatusOK, keyAttempts, false, 0},
		"refused every time":           {keyAttempts, http.StatusOK, keyAttempts, true, KindKeyRefused},
		"token refused":                {0, http.StatusUnauthorized, 1, true, KindUnauthorized},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				switch {
				case calls <= tt.refused:
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(refusal))
				case tt.status != http.StatusOK:
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(`{"error":{"message":"UNAUTHORIZED"}}`))
				default:
					_, _ = w.Write([]byte(`{"data":[{"vin":"VIN1"}]}`))
				}
			}))
			t.Cleanup(srv.Close)
			vins, err := NewClient(srv.URL, "key", srv.Client()).Vehicles(context.Background(), "", "token")
			var apiErr *APIError
			if !tt.wantErr && (err != nil || len(vins) != 1) || tt.wantErr && (!errors.As(err, &apiErr) || apiErr.Kind != tt.wantKind) {
				t.Errorf("Vehicles = %v, %v", vins, err)
			}
			if calls != tt.wantCalls {
				t.Errorf("%d calls, want %d", calls, tt.wantCalls)
			}
		})
	}
}
