package pleos

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/pleosapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

const simVIN = "KNAPC81ABPP000001"

// newSim starts the simulator on the EV6 of demonstration: this is the contract test
// between the client and the fake API. It also answers how its faults are classified.
func newSim(t *testing.T) (*Client, *clock.Manual, *pleosapi.Handler) {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/ev6.yaml") //nolint:gosec // the test's own scenario
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
	h := pleosapi.NewHandler([]pleosapi.Source{sim}, clk, pleosapi.DevCredential(), pleosapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+pleosapi.Prefix, srv.Client()), clk, h
}

func devCredential() Credential {
	c := pleosapi.DevCredential()
	return Credential{ClientID: c.ClientID, ClientSecret: c.ClientSecret}
}

func token(ctx context.Context, t *testing.T, c *Client) AccessToken {
	t.Helper()
	tok, err := c.Token(ctx, Kia, devCredential())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestTokenAndVehicles(t *testing.T) {
	c, clk, _ := newSim(t)
	ctx := context.Background()
	tok, err := c.Token(ctx, Kia, devCredential())
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value == "" {
		t.Fatal("no token")
	}
	// The token's JWT carries its end: read without verifying the signature.
	if want := clk.Now().Add(time.Hour); !tok.ExpiresAt.Equal(want) {
		t.Errorf("the token's end %v, want %v", tok.ExpiresAt, want)
	}
	vins, err := c.Vehicles(ctx, tok.Value, Kia)
	if err != nil {
		t.Fatal(err)
	}
	if len(vins) != 1 || vins[0] != simVIN {
		t.Fatalf("vins = %v", vins)
	}
	// A key of one brand reads no other's car: the Ioniq 5 of another handler is
	// another key's, consent above lists this one's alone.
	if _, err := c.Consent(ctx, tok.Value, Hyundai); err == nil {
		t.Error("the kia token reads hyundai cars")
	}
}

// TestTokenRefused: a wrong secret and a revoked key are the connection's refusal,
// whatever the API wrote (observed a 400, documented 404 "40404").
func TestTokenRefused(t *testing.T) {
	c, _, h := newSim(t)
	ctx := context.Background()
	_, err := c.Token(ctx, Kia, Credential{ClientID: pleosapi.DevClientID, ClientSecret: "wrong"})
	if !refused(t, err) {
		t.Errorf("wrong secret: %v", err)
	}
	h.RefuseKey()
	_, err = c.Token(ctx, Kia, devCredential())
	if !refused(t, err) {
		t.Errorf("revoked key: %v", err)
	}
}

// TestTokenUnavailable: the API's own failure tells nothing of the key, which stays.
func TestTokenUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	_, err := NewClient(srv.URL, srv.Client()).Token(context.Background(), Kia, devCredential())
	var e *APIError
	if !errors.As(err, &e) || e.Status != http.StatusServiceUnavailable {
		t.Fatalf("not the API's 503: %v", err)
	}
	if e.CredentialRefused() {
		t.Error("a 503 told as the key's refusal")
	}
}

// TestVehiclesWithoutVIN: an entry that names no vehicle is not listed.
func TestVehiclesWithoutVIN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"vehicles":[{"nickname":"none"},{"vin":null},{"vin":"` + simVIN + `"}]}}`))
	}))
	t.Cleanup(srv.Close)
	vins, err := NewClient(srv.URL, srv.Client()).Vehicles(context.Background(), "token", Kia)
	if err != nil {
		t.Fatal(err)
	}
	if len(vins) != 1 || vins[0] != simVIN {
		t.Errorf("vins = %q", vins)
	}
}

func refused(t *testing.T, err error) bool {
	t.Helper()
	var e *APIError
	return errors.As(err, &e) && e.Kind == KindCredentialRefused
}

// TestTakenPlace: a token asked for with the same key takes the previous one's place,
// with the 403 a real response carried (a number, not a string).
func TestTakenPlace(t *testing.T) {
	c, _, _ := newSim(t)
	ctx := context.Background()
	first := token(ctx, t, c)
	_ = token(ctx, t, c)
	_, err := c.Fetch(ctx, first.Value, Kia, simVIN, Batteries)
	var e *APIError
	if !errors.As(err, &e) {
		t.Fatalf("not the API's error: %v", err)
	}
	if e.Code != "403" || e.Kind != KindUnauthorized {
		t.Errorf("code %q kind %v", e.Code, e.Kind)
	}
}

// TestErrors tells each of the API's refusals by the kind the collector reacts to
// (ADR 0049 §3): the faults are set on demand, one response each.
func TestErrors(t *testing.T) {
	fetch := func(t *testing.T, c *Client, tok AccessToken) error {
		t.Helper()
		_, err := c.Fetch(context.Background(), tok.Value, Kia, simVIN, Batteries)
		return err
	}
	t.Run("expired", func(t *testing.T) {
		c, clk, _ := newSim(t)
		tok := token(context.Background(), t, c)
		clk.Advance(time.Hour + time.Minute)
		if kind(t, fetch(t, c, tok)) != KindUnauthorized {
			t.Error("an expired token is not the token's refusal")
		}
	})
	t.Run("no data", func(t *testing.T) {
		c, _, h := newSim(t)
		h.LeaveWithoutData("")
		tok := token(context.Background(), t, c)
		if kind(t, fetch(t, c, tok)) != KindNotAvailable {
			t.Error("no data is not told as such")
		}
	})
	t.Run("withdrawn", func(t *testing.T) {
		c, _, h := newSim(t)
		h.Withdraw("")
		tok := token(context.Background(), t, c)
		if kind(t, fetch(t, c, tok)) != KindUnshared {
			t.Error("a withdrawn consent is not the vehicle's refusal")
		}
	})
	t.Run("rate limited", func(t *testing.T) {
		c, _, h := newSim(t)
		h.RateLimit(10 * time.Minute)
		tok := token(context.Background(), t, c)
		var e *APIError
		if err := fetch(t, c, tok); !errors.As(err, &e) || e.Kind != KindRateLimited {
			t.Fatalf("not the limit's: %v", err)
		}
		// No 429 is documented: the wait is the source's, and Retry-After's when the
		// API gives it (the simulator does, ten minutes).
		if e.RetryIn != 10*time.Minute {
			t.Errorf("retry in %v", e.RetryIn)
		}
	})
}

func kind(t *testing.T, err error) Kind {
	t.Helper()
	var e *APIError
	if !errors.As(err, &e) {
		t.Fatalf("not the API's error: %v", err)
	}
	return e.Kind
}
