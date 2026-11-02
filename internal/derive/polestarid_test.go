package derive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/collector/polestaridsource"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/polestarid"
	"runsten/internal/simulator/polestaridapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/volvo"
)

// idCredential is the Polestar ID's credential of the simulator, its refresh token kept
// as it rotates.
type idCredential struct{ refresh string }

func (c *idCredential) PolestarIDCredential(context.Context, string, string) (polestaridsource.Credential, error) {
	return polestaridsource.Credential{Refresh: c.refresh}, nil
}

func (c *idCredential) RotatePolestarIDToken(_ context.Context, _, _ string, _ time.Time, token string) error {
	c.refresh = token
	return nil
}

// idCatalog knows the Polestar 4 of polestar4-id: 94 kWh net.
type idCatalog struct{}

func (idCatalog) NetCapacity(context.Context, string, string) (core.NetCapacity, error) {
	return func(n core.Snapshot) (float64, bool) { return 94, n.Family.V == "Polestar 4" }, nil
}

// TestPolestarIDDay: the Polestar 4 of polestar4-id read through the Polestar ID, the
// GraphQL and the gRPC, derived after each pass: its evening AC charge at home is
// measured (the cable, the type, the power read every minute), not inferred from the
// state of charge alone. The incremental derivation matches a rebuild.
func TestPolestarIDDay(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/polestar4-id.yaml")
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
	srv := httptest.NewUnstartedServer(polestaridapi.NewHandler([]polestaridapi.Source{sim}, clk, polestaridapi.DefaultBehavior()))
	srv.Config.Protocols = new(http.Protocols)
	srv.Config.Protocols.SetHTTP1(true)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Start()
	t.Cleanup(srv.Close)

	st := newMemStore(collector.Target{
		AccountID: "a", VehicleID: "p", VIN: sim.VIN(), ConnectionID: "pc", Provider: polestarid.Source, CredentialSetAt: sc.Start,
	})
	d := New(st, Sources(volvo.Normalizer{}, polestarid.Normalizer{}), core.DefaultParams(), idCatalog{}, quiet)
	c := collector.New(volvo.NewClient(srv.URL, "key", srv.Client()), st, fixedToken{}, clk, collector.DefaultIntervals(),
		collector.DefaultQuota(), quiet, nil, d)
	client := polestarid.NewClient(polestarid.Hosts{
		Auth: srv.URL + strings.TrimSuffix(polestaridapi.Prefix, "/"), API: srv.URL + polestaridapi.APIPath,
		Discovery: srv.URL + polestaridapi.DiscoveryPath, PCCS: srv.URL,
	}, srv.Client())
	c.AddSource(polestaridsource.New(client, &idCredential{refresh: polestaridapi.DevRefreshToken()}), st)
	day := sc.Start.Truncate(24 * time.Hour)
	for clk.Now().Before(day.Add(21 * time.Hour)) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}

	v := st.vehicle("p")
	if len(v.charges) != 1 {
		t.Fatalf("%d charges, want 1: %+v", len(v.charges), v.charges)
	}
	ch := v.charges[0]
	start := day.Add(16*time.Hour + 45*time.Minute)
	if ch.Reconstructed || ch.Type.V != core.AC || !slices.Equal(ch.Sources, []string{polestarid.Source}) {
		t.Errorf("charge: %+v, reconstructed %v, sources %v", ch.Type, ch.Reconstructed, ch.Sources)
	}
	// The status read every minute while charging, every 5 parked.
	if !within(start, ch.Start.After, ch.Start.Before) || ch.Start.Before.Sub(ch.Start.After) > 5*time.Minute || !near(ch.EndSoC, 90, 1) {
		t.Errorf("charge: start %+v, SoC %+v → %+v", ch.Start, ch.StartSoC, ch.EndSoC)
	}
	// The energy of the power, 11 kW read every minute, and of the SoC on 94 kWh, read in
	// whole points: two apart.
	soc := (ch.EndSoC.V - ch.StartSoC.V) * 0.94
	if !ch.EnergyPowerKWh.OK || !near(ch.EnergyPowerKWh, soc, 2) || !near(ch.EnergySoCKWh, soc, 0.5) {
		t.Errorf("charge: energy of the power %+v, of the SoC %+v, want about %.1f", ch.EnergyPowerKWh, ch.EnergySoCKWh, soc)
	}
	if !ch.TargetSoC.OK || ch.TargetSoC.V != 90 || !ch.MaxCurrentA.OK {
		t.Errorf("charge: target %+v, current %+v", ch.TargetSoC, ch.MaxCurrentA)
	}
	checkRebuildOf(t, st, d, "p")
}
