package tesla

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/teslaapi"
	"runsten/internal/simulator/vehicle"
)

// sim is the client against the simulator on the model3 scenario, at its start.
type sim struct {
	*Client
	clk *clock.Manual
	srv *httptest.Server
}

func newSim(t *testing.T) *sim {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/model3.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	s, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	srv := httptest.NewServer(teslaapi.NewHandler([]teslaapi.Source{s}, clk, teslaapi.DefaultBehavior()))
	t.Cleanup(srv.Close)
	c := NewClient(Hosts{API: srv.URL, Auth: srv.URL + teslaapi.AuthPrefix}, srv.Client())
	return &sim{Client: c, clk: clk, srv: srv}
}

// at moves the simulated clock to the scenario's start plus d.
func (s *sim) at(d time.Duration) {
	start := time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	s.clk.Advance(start.Add(d).Sub(s.clk.Now()))
}

func (s *sim) control(t *testing.T, path string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.srv.URL+teslaapi.ControlPrefix+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func kindOf(err error) Kind {
	var e *APIError
	if !errors.As(err, &e) {
		return -1
	}
	return e.Kind
}

func TestSimulatorDay(t *testing.T) {
	ctx := context.Background()
	s := newSim(t)
	tok, err := s.Refresh(ctx, teslaapi.DevRefreshToken())
	if err != nil {
		t.Fatal(err)
	}
	if tok.Refresh == teslaapi.DevRefreshToken() || tok.ExpiresIn != 8*time.Hour {
		t.Errorf("tokens: rotated %v, expires in %v", tok.Refresh != teslaapi.DevRefreshToken(), tok.ExpiresIn)
	}
	products, err := s.Products(ctx, tok.Access)
	if err != nil || len(products) != 1 || products[0].VIN != "LRW3E7EK1RC000001" {
		t.Fatalf("products: %v %v", products, err)
	}
	id := products[0].ID

	read := func() core.Snapshot {
		t.Helper()
		raw, err := s.VehicleData(ctx, tok.Access, id)
		if err != nil {
			t.Fatalf("at %v: %v", s.clk.Now(), err)
		}
		snap, err := Normalize(VehicleData, raw)
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	state := func() State {
		t.Helper()
		raw, err := s.Vehicle(ctx, tok.Access, id)
		if err != nil {
			t.Fatal(err)
		}
		st, err := StateOf(raw)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}

	// Parked at home, as the scenario starts.
	snap := read()
	if snap.Brand.V != "tesla" || snap.Family.V != "Model 3" || snap.ModelYear.V != 2024 || snap.SoC.V != 58 ||
		math.Abs(snap.OdometerKm.V-18200) > 1e-3 || snap.Engine.V != core.EngineStopped || snap.Connection.V != core.Disconnected {
		t.Errorf("parked: %+v", snap)
	}

	// Left alone, it falls asleep: vehicle_data answers 408, its state offline.
	s.at(15 * time.Minute)
	if st := state(); st != Offline {
		t.Errorf("15 min later: %s, want offline", st)
	}
	// The vehicle's state, stored, tells it the snapshot's way.
	if raw, err := s.Vehicle(ctx, tok.Access, id); err != nil {
		t.Fatal(err)
	} else if snap, err := Normalize(VehicleState, raw); err != nil || snap.Sleep != core.Some(core.Offline, time.Time{}) {
		t.Errorf("15 min later: sleep %+v, %v", snap.Sleep, err)
	}
	if _, err := s.VehicleData(ctx, tok.Access, id); kindOf(err) != KindUnavailable {
		t.Errorf("asleep: %v, want KindUnavailable", err)
	}

	// Mid-drive to work, 7 of 14 km.
	s.at(2*time.Hour + 15*time.Minute)
	if st := state(); st != Online {
		t.Errorf("driving: %s, want online", st)
	}
	snap = read()
	if snap.Engine.V != core.EngineRunning || math.Abs(snap.OdometerKm.V-18207) > 0.01 {
		t.Errorf("driving: engine %v, odometer %v", snap.Engine.V, snap.OdometerKm.V)
	}

	// Charging at home, after the drive back and an hour parked; the access token
	// expired meanwhile, refreshed with the rotated refresh token.
	s.at(13*time.Hour + 30*time.Minute)
	if _, err := s.Products(ctx, tok.Access); kindOf(err) != KindUnauthorized {
		t.Errorf("8 h later: %v, want KindUnauthorized", err)
	}
	if tok, err = s.Refresh(ctx, tok.Refresh); err != nil {
		t.Fatal(err)
	}
	snap = read()
	if snap.Charging.V != core.ChargingActive || snap.ChargeType.V != core.AC || snap.PowerW.V != 11000 ||
		snap.TargetSoC.V != 80 || snap.Connection.V != core.Connected {
		t.Errorf("charging: %+v", snap)
	}
}

func TestSimulatorErrors(t *testing.T) {
	ctx := context.Background()
	s := newSim(t)
	tok, err := s.Refresh(ctx, teslaapi.DevRefreshToken())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(ctx, teslaapi.DevRefreshToken()); kindOf(err) != KindTokenRefused {
		t.Errorf("a rotated refresh token: %v, want KindTokenRefused", err)
	}

	s.control(t, "rate-limit?for=2m")
	_, err = s.Products(ctx, tok.Access)
	var e *APIError
	if !errors.As(err, &e) || e.Kind != KindRateLimited || e.RetryIn != 2*time.Minute {
		t.Errorf("rate limited: %v", err)
	}
	s.control(t, "rate-limit?for=0s")

	s.control(t, "owner-api?closed=true")
	if _, err := s.Products(ctx, tok.Access); kindOf(err) != KindOwnerAPIClosed {
		t.Errorf("closed: %v, want KindOwnerAPIClosed", err)
	}
	s.control(t, "owner-api?closed=false")

	s.control(t, "revoke")
	if _, err := s.Refresh(ctx, tok.Refresh); kindOf(err) != KindTokenRefused {
		t.Errorf("revoked: %v, want KindTokenRefused", err)
	}

	s.clk.Advance(8 * time.Hour)
	if _, err := s.Products(ctx, tok.Access); kindOf(err) != KindUnauthorized {
		t.Errorf("expired: %v, want KindUnauthorized", err)
	}
}
