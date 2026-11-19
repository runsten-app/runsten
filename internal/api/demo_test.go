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

const (
	demoVIN       = "YV12ZEL000DEM0001"
	demoVehicleID = "7c1f6b86-3b0e-4f56-9a35-0d6f5a1e2d01"
)

// memDemo keeps the account's demo connection in memory, and shows its vehicle to the
// reader as the store would.
type memDemo struct {
	mu        sync.Mutex
	reader    *memReader
	set       time.Time
	pending   bool // set, its history not written yet
	err       error
	deleteErr error // DeleteDemoConnection fails, the connection stays
}

func (m *memDemo) DemoConnection(context.Context, string) (CredentialConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set.IsZero() {
		return CredentialConnection{}, m.err
	}
	c := CredentialConnection{ID: "demo", Last4: "demo", SetAt: m.set}
	if m.pending {
		c.RefusedAt = m.set
	}
	return c, m.err
}

func (m *memDemo) SetDemoConnection(_ context.Context, account string, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.set, m.pending = at, true
	if !slices.ContainsFunc(m.reader.vehicles[account], func(v Vehicle) bool { return v.Provider == "demo" }) {
		m.reader.vehicles[account] = append(m.reader.vehicles[account], Vehicle{ID: demoVehicleID, VIN: demoVIN, Provider: "demo"})
	}
	return "demo", nil
}

func (m *memDemo) DemoReady(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = false
	return m.err
}

func (m *memDemo) DeleteDemoConnection(_ context.Context, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.set, m.pending = time.Time{}, false
	m.reader.vehicles[account] = slices.DeleteFunc(m.reader.vehicles[account], func(v Vehicle) bool { return v.Provider == "demo" })
	return m.err
}

// fakeDemoVehicle records the histories written.
type fakeDemoVehicle struct {
	mu     sync.Mutex
	calls  []demoBackfill
	err    error
	locked func() bool // whether the account's lock is held
}

type demoBackfill struct {
	account, vehicle string
	to               time.Time
	net              core.NetCapacity
	locked           bool
}

func (f *fakeDemoVehicle) VIN() string { return demoVIN }

func (f *fakeDemoVehicle) Backfill(_ context.Context, account, vehicle string, to time.Time, net core.NetCapacity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, demoBackfill{account, vehicle, to, net, f.locked != nil && f.locked()})
	return f.err
}

// holdsLock has the fake tell whether the account's lock is held when it writes a history.
func (e *env) holdsLock() {
	e.demoVehicle.locked = func() bool {
		e.store.mu.Lock()
		defer e.store.mu.Unlock()
		return e.store.locked
	}
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
	e.holdsLock()
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
	if call.account != account || call.vehicle != demoVehicleID || !call.to.Equal(e.clk.Now()) || call.net == nil || !call.locked || e.demo.pending {
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
	// The vehicle has no credential, set or refused, nor key.
	if _, list := get(t, noFollow(), e.api.URL+apiPrefix+"/vehicles", c); !strings.Contains(list, `"connection":{"provider":"demo","status":"active","api_key":null,"credential":null,`) {
		t.Errorf("the demo vehicle's connection: %s", list)
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

// TestDemoFailures: each failure leaves the account as it can be started again from: a
// history that cannot be written leaves nothing behind, or, should the cleanup fail too, a
// connection still pending, which the user is not shown; a vehicle over the account's cap is
// refused; a store that fails is an internal error.
func TestDemoFailures(t *testing.T) {
	for _, tt := range []struct {
		name      string
		setup     func(*env)
		status    int
		code      string
		wantSet   bool // a connection is left
		wantCalls int  // histories written
	}{
		{"history fails", func(e *env) { e.demoVehicle.err = errors.New("disk full") }, http.StatusInternalServerError, "internal", false, 1},
		{"history and cleanup fail", func(e *env) {
			e.demoVehicle.err, e.demo.deleteErr = errors.New("disk full"), errors.New("no database")
		}, http.StatusInternalServerError, "internal", true, 1},
		{"over the cap", func(e *env) {
			e.server.MaxVehicles = 1
			e.reader.vehicles[account] = []Vehicle{{ID: "car", VIN: "YV1AAAAAAAAAAAAA1", Provider: "volvo"}}
		}, http.StatusConflict, "too_many_vehicles", false, 0},
		{"store down", func(e *env) { e.demo.err = errors.New("no database") }, http.StatusInternalServerError, "internal", false, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, 10)
			e.server.Flags = &accountFlags{}
			c := e.session(t, "admin")
			u := e.api.URL + apiPrefix + "/connection"
			tt.setup(e)
			resp, body := do(t, noFollow(), http.MethodPut, u+"/demo", "", "", c)
			if resp.status != tt.status || !strings.Contains(body, `"`+tt.code+`"`) || strings.Contains(body, "disk full") || strings.Contains(body, "no database") {
				t.Errorf("PUT: %d %s", resp.status, body)
			}
			if got := !e.demo.set.IsZero(); got != tt.wantSet || len(e.demoVehicle.calls) != tt.wantCalls {
				t.Errorf("connection left: %v, histories: %d, want %v and %d", got, len(e.demoVehicle.calls), tt.wantSet, tt.wantCalls)
			}
			if tt.wantSet && !e.demo.pending {
				t.Error("the connection left is not pending")
			}
			// Whatever is left, the user is shown no demo vehicle connected.
			e.demo.err = nil
			if _, body = get(t, noFollow(), u, c); !strings.Contains(body, `"demo":null`) {
				t.Errorf("shown: %s", body)
			}
		})
	}
}

// TestDemoDeleteStoreDown: a store that fails is an internal error for DELETE too.
func TestDemoDeleteStoreDown(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	e.demo.deleteErr = errors.New("no database")
	if resp, _ := do(t, noFollow(), http.MethodDelete, e.api.URL+apiPrefix+"/connection/demo", "", "", c); resp.status != http.StatusInternalServerError {
		t.Errorf("DELETE with the store down: %d", resp.status)
	}
}

// TestDemoRecovers: a connection left pending, the cleanup after a failed history having
// failed too, is not taken for connected: the next PUT deletes it and writes the history
// again, once, and a PUT after that changes nothing.
func TestDemoRecovers(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection"
	e.demoVehicle.err, e.demo.deleteErr = errors.New("disk full"), errors.New("no database")
	if resp, _ := do(t, noFollow(), http.MethodPut, u+"/demo", "", "", c); resp.status != http.StatusInternalServerError {
		t.Fatalf("first PUT: %d", resp.status)
	}
	e.demoVehicle.err, e.demo.deleteErr = nil, nil
	for i := range 2 {
		resp, body := do(t, noFollow(), http.MethodPut, u+"/demo", "", "", c)
		if resp.status != http.StatusOK || !strings.Contains(body, `"demo":{"set_at"`) {
			t.Fatalf("PUT %d: %d %s", i+2, resp.status, body)
		}
	}
	if len(e.demoVehicle.calls) != 2 || e.demo.pending || len(e.reader.vehicles[account]) != 1 {
		t.Errorf("%d histories, pending %v, vehicles %v", len(e.demoVehicle.calls), e.demo.pending, e.reader.vehicles[account])
	}
}

// TestDemoVehicleDeleted: the demo vehicle deleted by itself, its connection kept, is
// connected again by a PUT, with its history.
func TestDemoVehicleDeleted(t *testing.T) {
	e := newEnv(t, 10)
	e.server.Flags = &accountFlags{}
	c := e.session(t, "admin")
	u := e.api.URL + apiPrefix + "/connection/demo"
	if resp, _ := do(t, noFollow(), http.MethodPut, u, "", "", c); resp.status != http.StatusOK {
		t.Fatalf("PUT: %d", resp.status)
	}
	e.reader.vehicles[account] = nil
	if resp, _ := do(t, noFollow(), http.MethodPut, u, "", "", c); resp.status != http.StatusOK || len(e.demoVehicle.calls) != 2 || len(e.reader.vehicles[account]) != 1 {
		t.Errorf("PUT again: %d, %d histories, vehicles %v", resp.status, len(e.demoVehicle.calls), e.reader.vehicles[account])
	}
}
