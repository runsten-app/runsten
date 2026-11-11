package pleosapi

import (
	"net/http"
	"testing"
	"time"

	"runsten/internal/simulator/vehicle"
)

// The faults are set on demand (ADR 0049 §3): the key revoked, a vehicle's consent
// withdrawn, an endpoint without data, the calls answered 429. None of them is
// published by Pleos: their bodies and headers are assumed, and the client honours
// Retry-After.

func fault(t *testing.T, s *server, path string) {
	t.Helper()
	status, out, _ := s.call(t, http.MethodPost, ControlPrefix+path, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("%s: HTTP %d: %v", path, status, out)
	}
}

func TestKeyRevoked(t *testing.T) {
	s := newServer(t, DefaultBehavior())
	tok := s.token(t, Kia)
	fault(t, s, "revoke")
	status, out, _ := s.call(t, http.MethodPost, Prefix+"v1/auth/personal-token",
		tokenBody(t, DevCredential().ClientID, DevCredential().ClientSecret),
		map[string]string{"Brand": Kia, "Content-Type": "application/json"})
	if status != http.StatusNotFound || errorCode(t, out) != "40404" {
		t.Errorf("revoked key: HTTP %d code %s", status, errorCode(t, out))
	}
	// The tokens issued before live on: the revoke hits the key, at the token's.
	if status, out, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/consent"); status != http.StatusOK {
		t.Errorf("the token of a revoked key: HTTP %d: %v", status, out)
	}
}

func TestConsentWithdrawn(t *testing.T) {
	s := newServer(t, DefaultBehavior())
	tok := s.token(t, Kia)
	fault(t, s, "withdraw?vin="+vin)
	status, out, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/"+vin+"/batteries")
	if status != http.StatusBadRequest || errorCode(t, out) != "5006" {
		t.Errorf("withdrawn: HTTP %d code %s", status, errorCode(t, out))
	}
	// The consent still lists the vehicle: the data alone answers its refusal.
	if status, _, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/consent"); status != http.StatusOK {
		t.Errorf("consent: HTTP %d", status)
	}
}

func TestNoData(t *testing.T) {
	s := newServer(t, DefaultBehavior())
	tok := s.token(t, Kia)
	fault(t, s, "no-data?endpoint=batteries")
	status, out, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/"+vin+"/batteries")
	if status != http.StatusNotFound || errorCode(t, out) != "4045" {
		t.Errorf("no data: HTTP %d code %s", status, errorCode(t, out))
	}
	// The other endpoints read on.
	if status, _, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/"+vin+"/driving"); status != http.StatusOK {
		t.Errorf("driving: HTTP %d", status)
	}
	// Without an endpoint, every one of them answers no data.
	fault(t, s, "no-data")
	if status, out, _ = s.get(t, tok, Kia, Prefix+"v1/vehicles/"+vin+"/driving"); status != http.StatusNotFound || errorCode(t, out) != "4045" {
		t.Errorf("no data everywhere: HTTP %d code %s", status, errorCode(t, out))
	}
}

func TestRateLimit(t *testing.T) {
	s := newServer(t, DefaultBehavior())
	tok := s.token(t, Kia)
	fault(t, s, "rate-limit?for=10m")
	status, out, header := s.get(t, tok, Kia, Prefix+"v1/vehicles/"+vin+"/batteries")
	if status != http.StatusTooManyRequests || errorCode(t, out) != "429" {
		t.Errorf("rate limit: HTTP %d code %s", status, errorCode(t, out))
	}
	if ra := header.Get("Retry-After"); ra != "600" {
		t.Errorf("Retry-After %q, want 600", ra)
	}
	// Past the wait, the calls answer again.
	s.clk.Advance(11 * time.Minute)
	if status, _, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/"+vin+"/batteries"); status != http.StatusOK {
		t.Errorf("after the wait: HTTP %d", status)
	}
}

// TestSendCadence checks what the response's timestamp says of the car's send: a change
// is dated from when it happened, and while the car drives or charges it sends again
// every SendEvery; parked, nothing but a change moves the time (assumption,
// ADR 0049 §10.1).
func TestSendCadence(t *testing.T) {
	s := newServer(t, Behavior{SendEvery: 5 * time.Minute, TokenTTL: time.Hour})
	tok := s.token(t, Kia)
	read := func() time.Time {
		t.Helper()
		_, out, _ := s.get(t, tok, Kia, Prefix+"v1/vehicles/"+vin+"/batteries")
		at, err := time.Parse("2006-01-02T15:04:05Z", obj(t, obj(t, out)["data"])["timestamp"].(string))
		if err != nil {
			t.Fatal(err)
		}
		return at
	}
	// Parked: the same send, read later.
	s.clk.Advance(29 * time.Minute)
	if at := read(); !at.Equal(t0) {
		t.Errorf("parked: sent at %v, want %v (no send without a change)", at, t0)
	}
	// Charging: a send every SendEvery, the group's last change behind it.
	rep := s.src.rep
	rep.Energy.Charging, rep.Energy.ChargeType = vehicle.Charging, vehicle.AC
	rep.Energy.PluggedIn, rep.Energy.ChargingPowerW, rep.Energy.TargetSoC = true, 11000, 80
	s.src.rep = rep
	s.clk.Advance(1 * time.Minute) // the change is dated from its instant
	if at := read(); !at.Equal(t0.Add(30 * time.Minute)) {
		t.Errorf("the change: sent at %v, want %v", at, t0.Add(30*time.Minute))
	}
	s.clk.Advance(4 * time.Minute)
	if at := read(); !at.Equal(t0.Add(30 * time.Minute)) {
		t.Errorf("before SendEvery: sent at %v, want %v", at, t0.Add(30*time.Minute))
	}
	s.clk.Advance(1 * time.Minute)
	if at := read(); !at.Equal(t0.Add(35 * time.Minute)) {
		t.Errorf("after SendEvery: sent at %v, want %v", at, t0.Add(35*time.Minute))
	}
}
