package derive

import (
	"context"
	"fmt"
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

// TestPolestarIDParkedReestimate: a parked Polestar read through the Polestar ID, its
// battery estimating its level again, 65 → 67 % in five minutes. The telematics tell the
// SoC without the cable, the gRPC's battery read right after tells the cable: the rise is
// no charge when that cable reads disconnected, as before it. A battery response
// unchanged since before the rise (no SoC in it) is an older reading, and without the
// gRPC (not granted) there is none: the rise stays a charge. Derived after each pass, as
// the collector does, and checked against a rebuild.
func TestPolestarIDParkedReestimate(t *testing.T) {
	telematics := func(soc int, at time.Time) string {
		return fmt.Sprintf(`{"data":{"carTelematicsV2":{"battery":[{"vin":"LPSYCZRE5RL000014",`+
			`"batteryChargeLevelPercentage":%d,"estimatedDistanceToEmptyKm":%d,"chargingStatusV2":"CHARGING_STATUS_V2_IDLE",`+
			`"timestamp":{"seconds":"%d","nanos":0}}],"odometer":[],"health":[]}}}`, soc, soc*4, at.Unix())
	}
	battery := func(soc int, cable string, at time.Time) string {
		level := ""
		if soc > 0 {
			level = fmt.Sprintf(`"batteryChargeLevelPercentage":%d,`, soc)
		}
		return fmt.Sprintf(`{"battery":{%s"chargerConnectionStatus":"CHARGER_CONNECTION_STATUS_%s",`+
			`"chargingStatus":"CHARGING_STATUS_IDLE","timestamp":{"seconds":"%d","nanos":0}}}`, level, cable, at.Unix())
	}
	tests := []struct {
		name string
		// battery is the gRPC's response of a pass with the SoC soc, "" when not read.
		battery func(soc int, at time.Time) string
		charges int
	}{
		{"the battery tells the SoC", func(soc int, at time.Time) string { return battery(soc, "DISCONNECTED", at) }, 0},
		{"the battery's response unchanged", func(_ int, at time.Time) string { return battery(0, "DISCONNECTED", at) }, 1},
		{"the battery connected", func(soc int, at time.Time) string { return battery(soc, "CONNECTED", at) }, 1},
		{"the battery not granted", func(int, time.Time) string { return "" }, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newMemStore()
			d := New(st, Sources(volvo.Normalizer{}, polestarid.Normalizer{}), core.DefaultParams(), idCatalog{}, quiet)
			save := func(ep polestarid.Endpoint, at time.Time, raw string) {
				t.Helper()
				hash, err := polestarid.Fingerprint([]byte(raw))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := st.SaveSnapshot(context.Background(), collector.Snapshot{
					AccountID: "a", VehicleID: "p", Source: polestarid.Source, Endpoint: collector.Endpoint(ep),
					FetchedAt: at, Payload: []byte(raw), Hash: hash,
				}); err != nil {
					t.Fatal(err)
				}
			}
			start := time.Date(2026, 11, 5, 22, 0, 0, 0, time.UTC)
			// A pass every five minutes, the telematics then the battery two seconds after.
			for i, soc := range []int{65, 65, 65, 67, 67, 67} {
				at := start.Add(time.Duration(i) * 5 * time.Minute)
				save(polestarid.Telematics, at, telematics(soc, at))
				if raw := tt.battery(soc, at.Add(2*time.Second)); raw != "" {
					save(polestarid.Battery, at.Add(2*time.Second), raw)
				}
				if err := d.Update(context.Background(), "a", "p"); err != nil {
					t.Fatal(err)
				}
			}
			if v := st.vehicle("p"); len(v.charges) != tt.charges {
				t.Errorf("%d charges, want %d: %+v", len(v.charges), tt.charges, v.charges)
			}
			checkRebuildOf(t, st, d, "p")
		})
	}
}
