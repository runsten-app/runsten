package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/core"
)

const demoVIN = "YV12ZEL000DEM0001"

// memDemo keeps the account's demo connection in memory, and shows its vehicle to the
// reader as the store would.
type memDemo struct {
	mu     sync.Mutex
	reader *memReader
	set    time.Time
	err    error
}

func (m *memDemo) DemoConnection(context.Context, string) (CredentialConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set.IsZero() {
		return CredentialConnection{}, m.err
	}
	return CredentialConnection{ID: "demo", Last4: "demo", SetAt: m.set}, m.err
}

func (m *memDemo) SetDemoConnection(_ context.Context, account string, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.set = at
	m.reader.vehicles[account] = append(m.reader.vehicles[account], Vehicle{ID: "demo-vehicle", VIN: demoVIN, Provider: "demo"})
	return "demo", nil
}

func (m *memDemo) DeleteDemoConnection(_ context.Context, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.set = time.Time{}
	m.reader.vehicles[account] = slices.DeleteFunc(m.reader.vehicles[account], func(v Vehicle) bool { return v.Provider == "demo" })
	return m.err
}

// fakeDemoVehicle records the histories written.
type fakeDemoVehicle struct {
	mu    sync.Mutex
	calls []demoBackfill
	err   error
}

type demoBackfill struct {
	account, vehicle string
	to               time.Time
	net              core.NetCapacity
}

func (f *fakeDemoVehicle) VIN() string { return demoVIN }

func (f *fakeDemoVehicle) Backfill(_ context.Context, account, vehicle string, to time.Time, net core.NetCapacity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, demoBackfill{account, vehicle, to, net})
	return f.err
}

// TestDemoOff: the demo vehicle is off unless the flag is on for the account: nothing is
// enrolled nor written, and it may be disconnected all the same.
func TestDemoOff(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/demo"
	resp, body := do(t, noFollow(), http.MethodPut, u, "", "", c)
	if resp.status != http.StatusForbidden || !strings.Contains(body, `"feature_unavailable"`) {
		t.Fatalf("PUT with the flag off: %d %s", resp.status, body)
	}
	if len(e.store.vehicles) != 0 || len(e.demoVehicle.calls) != 0 || !e.demo.set.IsZero() {
		t.Errorf("enrolled %v, written %v, set %v", e.store.vehicles, e.demoVehicle.calls, e.demo.set)
	}
	if resp, body = do(t, noFollow(), http.MethodDelete, u, "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	// The flag is the account's: another account's flag does not turn it on.
	e.server.Flags = &accountFlags{off: account}
	if resp, _ = do(t, noFollow(), http.MethodPut, u, "", "", c); resp.status != http.StatusForbidden {
		t.Errorf("PUT with the flag off for the account: %d", resp.status)
	}
}

// TestDemoConnection: connecting enrolls the demo vehicle and writes its history up to
// now, with the net capacity of its variant; connecting again writes nothing more;
// disconnecting deletes it.
func TestDemoConnection(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	// The body, if any, is ignored: an empty object is as good as none.
	resp, body := do(t, noFollow(), http.MethodPut, u+"/demo", jsonType, `{}`, c)
	if resp.status != http.StatusOK {
		t.Fatalf("PUT: %d %s", resp.status, body)
	}
	golden(t, "connection-demo.json", body)
	if !slices.Equal(e.store.vehicles, []string{demoVIN}) || !e.demo.set.Equal(e.clk.Now()) {
		t.Errorf("enrolled %v, set at %v", e.store.vehicles, e.demo.set)
	}
	if len(e.demoVehicle.calls) != 1 {
		t.Fatalf("%d histories written", len(e.demoVehicle.calls))
	}
	call := e.demoVehicle.calls[0]
	if call.account != account || call.vehicle != "demo-vehicle" || !call.to.Equal(e.clk.Now()) || call.net == nil {
		t.Errorf("history: %+v", call)
	}
	// The variant is the catalog's: the EX30 Extended Range's 64 kWh net.
	if kwh, ok := call.net(core.Snapshot{
		Brand: core.Some("volvo", time.Time{}), Family: core.Some("EX30", time.Time{}), ModelYear: core.Some(2024, time.Time{}),
		CapacityKWh: core.Some(69.0, time.Time{}), Powertrain: core.Some(core.Electric, time.Time{}),
	}); !ok || kwh != 64 {
		t.Errorf("net capacity %v, %v", kwh, ok)
	}
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}
	if resp, body = do(t, noFollow(), http.MethodPut, u+"/demo", "", "", c); resp.status != http.StatusOK || len(e.demoVehicle.calls) != 1 {
		t.Errorf("PUT again: %d %s, %d histories", resp.status, body, len(e.demoVehicle.calls))
	}
	if resp, body = do(t, noFollow(), http.MethodDelete, u+"/demo", "", "", c); resp.status != http.StatusNoContent || body != "" {
		t.Errorf("DELETE: %d %q", resp.status, body)
	}
	if _, body = get(t, noFollow(), u, c); !strings.Contains(body, `"demo":null`) || len(e.reader.vehicles[account]) != 0 {
		t.Errorf("after DELETE: %s, vehicles %v", body, e.reader.vehicles[account])
	}
}

// TestDemoFailures: a history that cannot be written leaves nothing behind, a demo vehicle
// that would exceed the account's cap is refused, and a store that fails is an internal
// error.
func TestDemoFailures(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/demo"
	e.demoVehicle.err = errors.New("disk full")
	resp, body := do(t, noFollow(), http.MethodPut, u, "", "", c)
	if resp.status != http.StatusInternalServerError || strings.Contains(body, "disk full") {
		t.Errorf("PUT with a history that fails: %d %s", resp.status, body)
	}
	if !e.demo.set.IsZero() || len(e.reader.vehicles[account]) != 0 {
		t.Errorf("left behind: set %v, vehicles %v", e.demo.set, e.reader.vehicles[account])
	}
	e.demoVehicle.err = nil

	e.server.MaxVehicles = 1
	e.reader.vehicles[account] = []Vehicle{{ID: "car", VIN: "YV1AAAAAAAAAAAAA1", Provider: "volvo"}}
	if resp, body = do(t, noFollow(), http.MethodPut, u, "", "", c); resp.status != http.StatusConflict || !strings.Contains(body, `"too_many_vehicles"`) {
		t.Errorf("PUT over the cap: %d %s", resp.status, body)
	}
	e.server.MaxVehicles = 0

	e.demo.err = errors.New("no database")
	if resp, _ = do(t, noFollow(), http.MethodPut, u, "", "", c); resp.status != http.StatusInternalServerError {
		t.Errorf("PUT with the store down: %d", resp.status)
	}
	if resp, _ = do(t, noFollow(), http.MethodDelete, u, "", "", c); resp.status != http.StatusInternalServerError {
		t.Errorf("DELETE with the store down: %d", resp.status)
	}
}
