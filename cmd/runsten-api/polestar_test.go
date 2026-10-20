package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"runsten/internal/api"
	"runsten/internal/platform/clock"
	"runsten/internal/polestar"
	"runsten/internal/simulator/polestarapi"
	"runsten/internal/simulator/vehicle"
)

type psSource string

func (s psSource) VIN() string                     { return string(s) }
func (psSource) ReportAt(time.Time) vehicle.Report { return vehicle.Report{} }

func TestPolestarVehicles(t *testing.T) {
	const vin = "LPSVSEDE5RL000001"
	clk := clock.NewManual(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	srv := httptest.NewServer(polestarapi.NewHandler([]polestarapi.Source{psSource(vin)}, clk, polestarapi.DevCredential(),
		polestarapi.DefaultLimits(), polestarapi.DefaultBehavior()))
	defer srv.Close()
	p := polestarVehicles{polestar.NewClient(srv.URL+polestarapi.Prefix, srv.Client())}
	cred := api.PolestarCredential(polestarapi.DevCredential())
	vins, err := p.PolestarVehicles(context.Background(), cred)
	if err != nil || !slices.Equal(vins, []string{vin}) {
		t.Errorf("vehicles %v, %v", vins, err)
	}
	cred.ClientSecret = "not-the-secret"
	_, err = p.PolestarVehicles(context.Background(), cred)
	var r interface{ CredentialRefused() bool }
	if !errors.As(err, &r) || !r.CredentialRefused() {
		t.Errorf("a credential refused: %v", err)
	}
}
