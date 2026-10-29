package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"runsten/internal/core"
)

func (m *memReader) InProgress(_ context.Context, _, vehicle string, net core.NetCapacity) (core.Value[core.ChargeInProgress], error) {
	m.net = net
	c, ok := m.open[vehicle]
	return core.Value[core.ChargeInProgress]{V: c, OK: ok}, m.err
}

// openCharge is the car's charge in progress at home: an AC charge read charging at
// 23:10, and once more at 23:12, paused.
func openCharge() core.ChargeInProgress {
	return core.ChargeInProgress{Charge: core.Charge{
		DetectedAt: h(22, 40), Start: core.Bounds{After: h(22, 35), Before: h(22, 40)},
		End: core.Bounds{After: h(23, 10), Before: h(23, 12)}, Sources: fromVolvo,
		Type: some(core.AC), StartSoC: some(40.0), EndSoC: some(52.0), TargetSoC: some(80.0),
		EnergySoCKWh: some(9.6), EnergyPowerKWh: some(9.1), Capacity: fromAPI, Position: some(home),
	}, PowerW: some(7400.0)}
}

// TestChargingJSON: the charge in progress, priced by its place's tariff up to its
// latest reading; null when none is.
func TestChargingJSON(t *testing.T) {
	e, c := readerEnv(t)
	e.reader.open = map[string]core.ChargeInProgress{car: openCharge()}
	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/charging", c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "charging.json", body)
	// The derivation's net capacity: the variant the XC40's details recognize.
	if kwh, ok := e.reader.net(details("XC40", 2021, 78.012, true)); !ok || kwh <= 60 || kwh >= 78.012 {
		t.Errorf("net capacity %v, %v", kwh, ok)
	}
	if _, ok := e.reader.net(details("XC40", 2021, 78.012, false)); ok {
		t.Error("a plug-in hybrid has a net capacity")
	}
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+parked+"/charging", c)
	golden(t, "charging-none.json", body)

	// Without the costs, no cost.
	e.server.Limits = &historyLimit{account: account, noCosts: true}
	defer func() { e.server.Limits = nil }()
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/charging", c)
	var got struct {
		Charge struct {
			Cost  json.RawMessage `json:"cost"`
			Place json.RawMessage `json:"place"`
		} `json:"charge"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Charge.Cost) != "null" || string(got.Charge.Place) == "null" {
		t.Errorf("without the costs: %s", body)
	}
}

func TestChargingErrors(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	if resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+foreign+"/charging", c); resp.status != http.StatusNotFound {
		t.Errorf("another account's vehicle: %d %s", resp.status, body)
	}
	e.server.Charging = failingCharging{}
	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/charging", c)
	if resp.status != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	wantError(t, body, "internal")
	if !strings.Contains(logs.String(), "charge in progress not read") {
		t.Errorf("not logged: %s", logs)
	}
}

type failingCharging struct{}

func (failingCharging) InProgress(context.Context, string, string, core.NetCapacity) (core.Value[core.ChargeInProgress], error) {
	return core.Value[core.ChargeInProgress]{}, errEvents
}
