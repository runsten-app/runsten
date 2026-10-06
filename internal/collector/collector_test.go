package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

const simVIN = "YV1SMLT0000DT0001"

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// memStore is an in-memory Store, with the same deduplication as the real one.
type memStore struct {
	targets    []Target
	targetsErr error
	saved      []Snapshot
	last       map[string][]byte
	statuses   []Status
	statusErr  error
	calls      []Calls
	callsErr   error
	keys       map[string]string    // own application keys, by connection
	refused    map[string]time.Time // keys refused, by connection: the setAt refused
}

func (m *memStore) ConnectionKey(_ context.Context, _, connection string) (string, error) {
	return m.keys[connection], nil
}

func (m *memStore) SetKeyRefused(_ context.Context, _, connection string, setAt time.Time, refused bool) error {
	if m.refused == nil {
		m.refused = map[string]time.Time{}
	}
	if refused {
		m.refused[connection] = setAt
	} else {
		delete(m.refused, connection)
	}
	return nil
}

func (m *memStore) SaveCalls(_ context.Context, calls []Calls) error {
	if m.callsErr != nil {
		return m.callsErr
	}
	m.calls = append(m.calls, calls...)
	return nil
}

func (m *memStore) Calls(_ context.Context, since time.Time) ([]Calls, error) {
	var out []Calls
	for _, c := range m.calls {
		if !c.Hour.Before(since) {
			out = append(out, c)
		}
	}
	return out, m.callsErr
}

func (m *memStore) Targets(context.Context) ([]Target, error) { return m.targets, m.targetsErr }

func (m *memStore) SaveStatus(_ context.Context, s Status) error {
	if m.statusErr != nil {
		return m.statusErr
	}
	m.statuses = append(m.statuses, s)
	return nil
}

func (m *memStore) SaveSnapshot(_ context.Context, s Snapshot) (bool, error) {
	if m.last == nil {
		m.last = map[string][]byte{}
	}
	k := s.VehicleID + "|" + string(s.Endpoint)
	if bytes.Equal(m.last[k], s.Hash) {
		return false, nil
	}
	m.last[k] = s.Hash
	m.saved = append(m.saved, s)
	return true, nil
}

// countingAPI counts the calls per endpoint.
type countingAPI struct {
	API
	mu    sync.Mutex
	calls map[volvo.Endpoint][]time.Time
	clk   clock.Clock
}

func (c *countingAPI) Fetch(ctx context.Context, key, token, vin string, ep volvo.Endpoint) ([]byte, error) {
	c.mu.Lock()
	if c.calls == nil {
		c.calls = map[volvo.Endpoint][]time.Time{}
	}
	c.calls[ep] = append(c.calls[ep], c.clk.Now())
	c.mu.Unlock()
	return c.API.Fetch(ctx, key, token, vin, ep)
}

func simulator(t *testing.T) (*volvo.Client, *clock.Manual) {
	t.Helper()
	return simulatorFor(t, "commute.yaml")
}

// simulatorFor starts the simulator on a scenario from the repository, with its limits
// and failures (same combination as runsten-simulator, without environment overrides).
func simulatorFor(t *testing.T, file string) (*volvo.Client, *clock.Manual) {
	t.Helper()
	e := startSim(t, file, nil)
	return e.client, e.clk
}

// simEnv is a running simulator: Volvo API and Volvo ID.
type simEnv struct {
	client *volvo.Client
	auth   *volvo.AuthClient
	clk    *clock.Manual
	srv    *httptest.Server
	sc     *scenario.Scenario // the first scenario, whose api section applies
}

// startSim starts the simulator on a scenario from the repository, modified by edit.
func startSim(t *testing.T, file string, edit func(*scenario.Scenario)) simEnv {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../scenarios", file)) //nolint:gosec // repository file
	if err != nil {
		t.Fatal(err)
	}
	sc := parseScenario(t, string(data))
	if edit != nil {
		edit(sc)
	}
	return startFleet(t, sc)
}

func parseScenario(t *testing.T, src string) *scenario.Scenario {
	t.Helper()
	sc, err := scenario.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// startFleet starts the simulator on several scenarios, one vehicle each, under one
// Volvo ID (same combination as runsten-simulator, without environment overrides).
func startFleet(t *testing.T, scs ...*scenario.Scenario) simEnv {
	t.Helper()
	fleet, err := scenario.NewFleet(scs, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var srcs []volvoapi.Source
	for _, sim := range fleet.Simulations() {
		srcs = append(srcs, sim)
	}
	sc := scs[0]
	limits := volvoapi.DefaultLimits()
	if q := sc.API.DailyQuota; q != nil {
		limits.DailyQuota = *q
	}
	var faults volvoapi.Faults
	for _, o := range sc.API.Outages {
		faults.Outages = append(faults.Outages, volvoapi.Outage{From: sc.Start.Add(o.After), To: sc.Start.Add(o.After + o.Duration)})
	}
	auth := volvoapi.DefaultOAuth()
	for dst, v := range map[*time.Duration]time.Duration{
		&auth.AccessTokenTTL: sc.API.AccessTokenTTL, &auth.RefreshTokenTTL: sc.API.RefreshTokenTTL, &auth.GrantTTL: sc.API.GrantTTL,
	} {
		if v > 0 {
			*dst = v
		}
	}
	clk := clock.NewManual(fleet.Start())
	srv := httptest.NewServer(volvoapi.NewHandler(srcs, clk, limits, faults, auth))
	t.Cleanup(srv.Close)
	return simEnv{
		client: volvo.NewClient(srv.URL, "key", srv.Client()),
		auth: volvo.NewAuthClient(volvo.AuthConfig{
			BaseURL: srv.URL, ClientID: auth.ClientID, ClientSecret: auth.ClientSecret,
			RedirectURI: auth.RedirectURI, Scopes: volvo.DefaultScopes(),
		}, srv.Client()),
		clk: clk, srv: srv, sc: sc,
	}
}

func target() Target {
	return Target{AccountID: "a", VehicleID: "v", VIN: simVIN, ConnectionID: "c"}
}

// fixedToken is a TokenSource with a token that never expires.
type fixedToken struct{}

func (fixedToken) Token(context.Context, string, string) (string, error) { return "token", nil }

func (fixedToken) Refresh(context.Context, string, string, string) (string, error) {
	return "token", nil
}
func (fixedToken) KeepAlive(context.Context) error { return nil }

func lonOf(t *testing.T, raw []byte) float64 {
	t.Helper()
	var v struct {
		Data struct {
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || len(v.Data.Geometry.Coordinates) < 2 {
		t.Fatalf("unreadable location: %s", raw)
	}
	return v.Data.Geometry.Coordinates[0]
}

// TestDayScenario plays the home-work commute day: departure at 07:00, arrival at work
// at 07:40, return trip from 16:40 to 17:25, charging from 18:25.
func TestDayScenario(t *testing.T) {
	client, clk := simulator(t)
	api := &countingAPI{API: client, clk: clk}
	st := &memStore{targets: []Target{target()}}
	c := New(api, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	start := clk.Now()
	at := func(h, m int) time.Time {
		return start.Truncate(24 * time.Hour).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}

	for clk.Now().Before(at(21, 0)) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}

	t.Run("driving detected within less than one parked interval", func(t *testing.T) {
		var during int
		for _, ts := range api.calls[volvo.EngineStatus] {
			if !ts.Before(at(7, 0)) && ts.Before(at(7, 40)) {
				during++
			}
		}
		// 40 min of driving, detected within 10 min, then one call per minute.
		t.Logf("engine-status during the trip: %d calls", during)
		if during < 30 {
			t.Errorf("engine-status during the outbound trip: %d calls", during)
		}
	})

	t.Run("position read on arrival", func(t *testing.T) {
		var arrivals []time.Time
		for _, s := range st.saved {
			if s.Endpoint == volvo.Location && lonOf(t, s.Payload) == 4.927 {
				arrivals = append(arrivals, s.FetchedAt)
			}
		}
		t.Logf("work position read at %v", arrivals)
		if len(arrivals) != 1 {
			t.Fatalf("stored \"work\" positions: %v", arrivals)
		}
		if a := arrivals[0]; a.Before(at(7, 40)) || a.After(at(7, 43)) {
			t.Errorf("work position read at %v, expected shortly after 07:40", a)
		}
	})

	t.Run("charging tracked at the active interval", func(t *testing.T) {
		var during int
		for _, ts := range api.calls[volvo.EnergyState] {
			if !ts.Before(at(18, 40)) && ts.Before(at(19, 40)) {
				during++
			}
		}
		t.Logf("energy-state during charging: %d calls", during)
		if during < 55 {
			t.Errorf("energy-state during charging: %d calls in 1 h", during)
		}
	})

	t.Run("deduplication and budget", func(t *testing.T) {
		var calls int
		for _, ts := range api.calls {
			calls += len(ts)
		}
		t.Logf("%d calls, %d snapshots stored", calls, len(st.saved))
		// 16 simulated hours: far below the 10,000 calls per day and per API.
		if calls > 1500 {
			t.Errorf("%d calls", calls)
		}
		if len(st.saved) >= calls {
			t.Errorf("%d snapshots stored for %d calls: no deduplication", len(st.saved), calls)
		}
		var details int
		for _, s := range st.saved {
			if s.Endpoint == volvo.Details {
				details++
			}
		}
		if details != 1 {
			t.Errorf("details stored %d times", details)
		}
	})
}

// playDay polls the simulator every 15 s until 21:00.
func playDay(t *testing.T, c *Collector, clk *clock.Manual) {
	t.Helper()
	end := clk.Now().Truncate(24 * time.Hour).Add(21 * time.Hour)
	for clk.Now().Before(end) {
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
}

// TestMissedTrip: the API is down for the whole trip. Driving is never observed, but
// the odometer and location read afterwards still record the trip.
func TestMissedTrip(t *testing.T) {
	client, clk := simulatorFor(t, "missed-trip.yaml")
	st := &memStore{targets: []Target{target()}}
	st.targets[0].VIN = "YV1SMLT0000TM0001"
	c := New(client, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	day := clk.Now().Truncate(24 * time.Hour)
	playDay(t, c, clk)

	var odometers []string
	var workSeen time.Time
	for _, s := range st.saved {
		switch s.Endpoint {
		case volvo.EngineStatus:
			if e, _ := volvo.ParseEngineStatus(s.Payload); e == "RUNNING" {
				t.Errorf("driving observed at %v despite the outage", s.FetchedAt)
			}
		case volvo.Odometer:
			odometers = append(odometers, string(s.Payload))
		case volvo.Location:
			if lonOf(t, s.Payload) == 4.927 && workSeen.IsZero() {
				workSeen = s.FetchedAt
			}
		}
	}
	if len(odometers) != 2 {
		t.Errorf("stored odometers: %d, want 2 (before and after the trip)", len(odometers))
	}
	if workSeen.IsZero() || workSeen.After(day.Add(9*time.Hour)) {
		t.Errorf("work position read at %v, expected before 09:00", workSeen)
	}
}

// TestQuotaExhausted: once the quota is refused, the collector stops calling the
// affected API until the announced delay (midnight UTC), instead of retrying.
func TestQuotaExhausted(t *testing.T) {
	client, clk := simulatorFor(t, "quota-exhausted.yaml")
	api := &countingAPI{API: client, clk: clk}
	st := &memStore{targets: []Target{target()}}
	st.targets[0].VIN = "YV1SMLT0000XH0001"
	playDay(t, New(api, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil), clk)

	byAPI := map[string]int{}
	for ep, calls := range api.calls {
		byAPI[ep.API()] += len(calls)
	}
	t.Logf("calls per API: %v", byAPI)
	for name, n := range byAPI {
		if n > 151 { // 150 accepted + 1 refused
			t.Errorf("%s: %d calls, the collector keeps retrying after the quota refusal", name, n)
		}
	}
	if byAPI[volvo.APIConnectedVehicle] != 151 {
		t.Errorf("Connected Vehicle: %d calls, quota not reached?", byAPI[volvo.APIConnectedVehicle])
	}
}

// charger is a second vehicle of the commute day's account: it stays at home, a few
// streets away, and charges on AC from 06:00 to about 11:25 (40 kWh at 7.4 kW).
const charger = `
vin: YV1SMLT0000CH0002
start: 2026-09-28T05:00:00Z
vehicle:
  profile: bev-generic
  model: EX-SIM
  modelYear: 2026
  batteryKWh: 80
  consumptionKWhPer100km: 18
  chargingCurrentLimitA: 16
  soc: 30
  targetSoc: 80
  odometerKm: 3000
  place: home
places:
  home: { lat: 45.7500, lon: 4.8500 }
steps:
  - park: { duration: 1h }
  - charge: { type: AC, powerKW: 7.4, untilSoc: 80 }
`

// twoVehicles returns the targets of two vehicles of one account, under one connection.
func twoVehicles(first, second *scenario.Scenario) []Target {
	return []Target{
		{AccountID: "a", VehicleID: "commuter", VIN: first.VIN, ConnectionID: "c"},
		{AccountID: "a", VehicleID: "charger", VIN: second.VIN, ConnectionID: "c"},
	}
}

// callsIn counts the calls of rec to endpoint ep of vin, in [from, to).
func callsIn(rec *recorder, vin string, ep volvo.Endpoint, from, to time.Time) int {
	n := 0
	for _, c := range rec.calls {
		if c.VIN == vin && c.Endpoint == ep && !c.At.Before(from) && c.At.Before(to) {
			n++
		}
	}
	return n
}

// TestTwoVehicles plays the commute day on one vehicle and a morning charge on another,
// of the same account and connection: each keeps its own mode and intervals, and its
// own snapshots.
func TestTwoVehicles(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/commute.yaml")
	if err != nil {
		t.Fatal(err)
	}
	commute, other := parseScenario(t, string(data)), parseScenario(t, charger)
	e := startFleet(t, commute, other)
	rec := &recorder{}
	st := &memStore{targets: twoVehicles(commute, other)}
	c := New(e.client, st, fixedToken{}, e.clk, DefaultIntervals(), DefaultQuota(), quiet, rec, nil)
	day := e.clk.Now().Truncate(24 * time.Hour)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	playDay(t, c, e.clk)

	t.Run("each vehicle in its own mode", func(t *testing.T) {
		tests := []struct {
			at                time.Time
			commuter, charger string
		}{
			{at(6, 30), "parked", "charging"},
			{at(7, 20), "driving", "charging"},
			{at(9, 0), "parked", "charging"},
			{at(13, 0), "parked", "parked"},
			{at(17, 0), "driving", "parked"},
			{at(19, 0), "charging", "parked"},
		}
		for _, tt := range tests {
			mode := map[string]string{}
			for _, s := range rec.states {
				if !s.At.After(tt.at) {
					mode[s.VehicleID] = s.Mode
				}
			}
			if mode["commuter"] != tt.commuter || mode["charger"] != tt.charger {
				t.Errorf("%v: modes %v, want commuter %s, charger %s", tt.at.Format("15:04"), mode, tt.commuter, tt.charger)
			}
		}
	})

	t.Run("each vehicle at its own intervals", func(t *testing.T) {
		tests := []struct {
			name     string
			vin      string
			ep       volvo.Endpoint
			from, to time.Time
			min, max int
		}{
			// 07:10-07:40: the commuter drives (engine every minute), the charger charges
			// (energy every minute, engine every parked interval).
			{"commuter's engine while driving", commute.VIN, volvo.EngineStatus, at(7, 10), at(7, 40), 28, 31},
			{"charger's engine while charging", other.VIN, volvo.EngineStatus, at(7, 10), at(7, 40), 2, 4},
			{"charger's energy while charging", other.VIN, volvo.EnergyState, at(7, 10), at(7, 40), 28, 31},
			// 13:00-16:00: both parked.
			{"commuter's engine parked", commute.VIN, volvo.EngineStatus, at(13, 0), at(16, 0), 17, 19},
			{"charger's engine parked", other.VIN, volvo.EngineStatus, at(13, 0), at(16, 0), 17, 19},
			{"charger's energy parked", other.VIN, volvo.EnergyState, at(13, 0), at(16, 0), 17, 19},
		}
		for _, tt := range tests {
			if n := callsIn(rec, tt.vin, tt.ep, tt.from, tt.to); n < tt.min || n > tt.max {
				t.Errorf("%s: %d calls, want %d to %d", tt.name, n, tt.min, tt.max)
			}
		}
	})

	t.Run("snapshots stored per vehicle", func(t *testing.T) {
		byVehicle := map[string]map[volvo.Endpoint]int{"commuter": {}, "charger": {}}
		for _, s := range st.saved {
			counts, ok := byVehicle[s.VehicleID]
			if !ok || s.AccountID != "a" {
				t.Fatalf("snapshot of an unknown vehicle: %+v", s)
			}
			counts[s.Endpoint]++
			switch s.Endpoint {
			case volvo.Details:
				var d struct {
					Data struct {
						VIN string `json:"vin"`
					} `json:"data"`
				}
				if err := json.Unmarshal(s.Payload, &d); err != nil {
					t.Fatal(err)
				}
				if want := map[string]string{"commuter": commute.VIN, "charger": other.VIN}[s.VehicleID]; d.Data.VIN != want {
					t.Errorf("details of %s: vin %s, want %s", s.VehicleID, d.Data.VIN, want)
				}
			case volvo.Location:
				if s.VehicleID == "charger" && lonOf(t, s.Payload) != 4.85 {
					t.Errorf("charger's location at %v: another vehicle's position", s.FetchedAt)
				}
			}
		}
		// The commuter's odometer moves with each trip, the charger's never does; the
		// charger's energy state changes with each percent charged.
		if n := byVehicle["commuter"][volvo.Odometer]; n < 3 {
			t.Errorf("commuter's odometers stored: %d, want one per trip at least", n)
		}
		if n := byVehicle["charger"][volvo.Odometer]; n != 1 {
			t.Errorf("charger's odometers stored: %d, want 1", n)
		}
		if n := byVehicle["charger"][volvo.EnergyState]; n < 50 {
			t.Errorf("charger's energy states stored: %d, want one per percent charged", n)
		}
		for id, counts := range byVehicle {
			if counts[volvo.Details] != 1 {
				t.Errorf("%s: details stored %d times", id, counts[volvo.Details])
			}
		}
	})
}

// TestTwoVehiclesShareTheQuota: the simulator counts the quota per API and application
// key, over every vehicle (an assumption, see volvoapi.Limits). The collector blocks an
// API for every vehicle once one of them is refused, until the announced delay.
func TestTwoVehiclesShareTheQuota(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/quota-exhausted.yaml")
	if err != nil {
		t.Fatal(err)
	}
	commute, other := parseScenario(t, string(data)), parseScenario(t, charger)
	e := startFleet(t, commute, other)
	rec := &recorder{}
	st := &memStore{targets: twoVehicles(commute, other)}
	playDay(t, New(e.client, st, fixedToken{}, e.clk, DefaultIntervals(), DefaultQuota(), quiet, rec, nil), e.clk)

	calls := map[string]int{}
	refused := map[string]Call{} // first refusal per API
	for _, c := range rec.calls {
		name := c.Endpoint.API()
		if r, ok := refused[name]; ok {
			t.Errorf("%s called for %s at %v, after the quota refusal for %s at %v",
				name, c.VIN, c.At.Format("15:04:05"), r.VIN, r.At.Format("15:04:05"))
			continue
		}
		calls[name]++
		if c.Status == 403 {
			refused[name] = c
		}
	}
	t.Logf("calls per API: %v", calls)
	if _, ok := refused[volvo.APIConnectedVehicle]; !ok || calls[volvo.APIConnectedVehicle] != 151 {
		t.Errorf("Connected Vehicle: %d calls, want 150 accepted for both vehicles and 1 refused", calls[volvo.APIConnectedVehicle])
	}
	for _, vin := range []string{commute.VIN, other.VIN} {
		if n := callsIn(rec, vin, volvo.EngineStatus, e.sc.Start, refused[volvo.APIConnectedVehicle].At); n == 0 {
			t.Errorf("%s: no engine-status call before the quota ran out", vin)
		}
	}
}

// scriptedAPI returns an error for some endpoints, an empty JSON otherwise.
type scriptedAPI struct {
	errs  map[volvo.Endpoint]error
	calls map[volvo.Endpoint]int
}

func (s *scriptedAPI) Fetch(_ context.Context, _, _, _ string, ep volvo.Endpoint) ([]byte, error) {
	if s.calls == nil {
		s.calls = map[volvo.Endpoint]int{}
	}
	s.calls[ep]++
	if err := s.errs[ep]; err != nil {
		return nil, err
	}
	return []byte(`{}`), nil
}

func TestFailures(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	iv := DefaultIntervals()

	t.Run("quota exhausted: the API is suspended, the others continue", func(t *testing.T) {
		clk := clock.NewManual(t0)
		api := &scriptedAPI{errs: map[volvo.Endpoint]error{
			volvo.EnergyState: &volvo.APIError{Kind: volvo.KindQuota, RetryIn: 2 * time.Hour},
		}}
		c := New(api, &memStore{targets: []Target{target()}}, fixedToken{}, clk, iv, DefaultQuota(), quiet, nil, nil)
		for range 12 { // an hour and a half
			_ = c.PollOnce(context.Background())
			clk.Advance(iv.Parked)
		}
		if api.calls[volvo.EnergyState] != 1 {
			t.Errorf("energy-state called %d times during the suspension", api.calls[volvo.EnergyState])
		}
		if api.calls[volvo.EngineStatus] < 10 {
			t.Errorf("engine-status called %d times", api.calls[volvo.EngineStatus])
		}
	})

	for _, kind := range []volvo.Kind{volvo.KindRateLimited, volvo.KindUnauthorized} {
		t.Run("account suspended", func(t *testing.T) {
			clk := clock.NewManual(t0)
			api := &scriptedAPI{errs: map[volvo.Endpoint]error{
				volvo.EngineStatus: &volvo.APIError{Kind: kind, RetryIn: time.Minute},
			}}
			c := New(api, &memStore{targets: []Target{target()}}, fixedToken{}, clk, iv, DefaultQuota(), quiet, nil, nil)
			_ = c.PollOnce(context.Background())
			if n := len(api.calls); n != 1 {
				t.Errorf("kind %v: %d endpoints called after the error", kind, n)
			}
		})
	}

	t.Run("ordinary error: retried at the normal interval", func(t *testing.T) {
		clk := clock.NewManual(t0)
		api := &scriptedAPI{errs: map[volvo.Endpoint]error{
			volvo.EngineStatus: errors.New("network"),
			volvo.Odometer:     &volvo.APIError{Kind: volvo.KindOther, Status: 500},
			volvo.Location:     &volvo.APIError{Kind: volvo.KindNotFound, Status: 404},
		}}
		c := New(api, &memStore{targets: []Target{target()}}, fixedToken{}, clk, iv, DefaultQuota(), quiet, nil, nil)
		_ = c.PollOnce(context.Background())
		clk.Advance(iv.Parked)
		_ = c.PollOnce(context.Background())
		if api.calls[volvo.EngineStatus] != 2 || api.calls[volvo.Doors] != 1 {
			t.Errorf("calls: %v", api.calls)
		}
	})

	t.Run("optional endpoint missing: no failure, tried again at the details' pace", func(t *testing.T) {
		clk := clock.NewManual(t0)
		api := &scriptedAPI{errs: map[volvo.Endpoint]error{
			volvo.Brakes: &volvo.APIError{Kind: volvo.KindOther, Status: 403}, // scope not granted
			volvo.Fuel:   &volvo.APIError{Kind: volvo.KindNotFound, Status: 404},
			volvo.Engine: &volvo.APIError{Kind: volvo.KindOther, Status: 500}, // an ordinary failure
		}}
		var logs bytes.Buffer
		store := &memStore{targets: []Target{target()}}
		c := New(api, store, fixedToken{}, clk, iv, DefaultQuota(), slog.New(slog.NewTextHandler(&logs, nil)), nil, nil)
		for range 25 * 6 { // 25 hours
			_ = c.PollOnce(context.Background())
			clk.Advance(iv.Parked)
		}
		if api.calls[volvo.Brakes] != 2 || api.calls[volvo.Fuel] != 2 || api.calls[volvo.Engine] != 25 {
			t.Errorf("calls: %v", api.calls)
		}
		for _, s := range store.statuses {
			if f := s.Failure; f.Endpoint != "" && f.Endpoint != volvo.Engine {
				t.Fatalf("failure told: %+v", f)
			}
		}
		if n := strings.Count(logs.String(), "optional endpoint unavailable"); n != 2 {
			t.Errorf("logged %d times:\n%s", n, &logs)
		}

		delete(api.errs, volvo.Brakes) // the scope granted
		clk.Advance(24 * time.Hour)
		_ = c.PollOnce(context.Background())
		clk.Advance(iv.Rare)
		_ = c.PollOnce(context.Background())
		if api.calls[volvo.Brakes] != 4 {
			t.Errorf("brakes called %d times once answered", api.calls[volvo.Brakes])
		}
	})
}

// recorder is an Observer that keeps everything.
type recorder struct {
	calls  []Call
	states []VehicleState
}

func (r *recorder) Called(c Call)           { r.calls = append(r.calls, c) }
func (r *recorder) Observed(v VehicleState) { r.states = append(r.states, v) }

func TestObserver(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	api := &scriptedAPI{errs: map[volvo.Endpoint]error{
		volvo.Doors:   &volvo.APIError{Kind: volvo.KindOther, Status: 500, Message: "boom"},
		volvo.Windows: errors.New("network"),
	}}
	rec := &recorder{}
	c := New(api, &memStore{targets: []Target{target()}}, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, rec, nil)
	_ = c.PollOnce(context.Background())
	clk.Advance(DefaultIntervals().Parked)
	_ = c.PollOnce(context.Background())

	byEp := map[volvo.Endpoint][]Call{}
	for _, call := range rec.calls {
		if call.VIN != simVIN || call.VehicleID != "v" {
			t.Fatalf("misattributed call: %+v", call)
		}
		byEp[call.Endpoint] = append(byEp[call.Endpoint], call)
	}
	if es := byEp[volvo.EngineStatus]; len(es) != 2 || !es[0].Stored || es[1].Stored || es[0].Status != 200 {
		t.Errorf("engine-status: expected stored then duplicate, %+v", es)
	}
	if d := byEp[volvo.Doors][0]; d.Status != 500 || d.Err == "" || d.Payload != nil {
		t.Errorf("doors: %+v", d)
	}
	if w := byEp[volvo.Windows][0]; w.Status != 0 || w.Err == "" {
		t.Errorf("windows: %+v", w)
	}
	if len(rec.states) != 2 || rec.states[1].Mode != "parked" || len(rec.states[1].Next) != len(endpoints()) {
		t.Errorf("states: %+v", rec.states)
	}
}

type failingStore struct{ memStore }

func (failingStore) SaveSnapshot(context.Context, Snapshot) (bool, error) {
	return false, errors.New("database unavailable")
}

func TestStoreErrors(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	api := &scriptedAPI{}
	st := &failingStore{memStore{targets: []Target{target()}}}
	if err := New(api, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil).PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != len(endpoints()) {
		t.Errorf("a storage failure must not stop the pass: %v", api.calls)
	}
}

func TestVehicleRemoved(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	st := &memStore{targets: []Target{target()}}
	c := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	_ = c.PollOnce(context.Background())
	st.targets = nil
	_ = c.PollOnce(context.Background())
	if len(c.vehicles) != 0 {
		t.Error("state of a removed vehicle kept")
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	api := &scriptedAPI{}
	c := New(api, &memStore{targets: []Target{target()}}, fixedToken{}, clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)), DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	done := make(chan error)
	var passes atomic.Int32
	go func() { done <- c.Run(ctx, time.Millisecond, func() { passes.Add(1) }) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(api.calls) == 0 || passes.Load() == 0 {
		t.Errorf("%d calls, %d passes", len(api.calls), passes.Load())
	}
}

func TestRunReportsOnlySuccessfulPasses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st := &memStore{targetsErr: errors.New("database down")}
	c := New(&scriptedAPI{}, st, fixedToken{}, clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)), DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	done := make(chan error)
	var passes atomic.Int32
	go func() { done <- c.Run(ctx, time.Millisecond, func() { passes.Add(1) }) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if passes.Load() != 0 {
		t.Errorf("%d passes reported while the vehicles could not be listed", passes.Load())
	}
}

// fakeDeriver records the vehicles it is asked to update.
type fakeDeriver struct {
	updates []string
	err     error
}

func (f *fakeDeriver) Update(_ context.Context, accountID, vehicleID string) error {
	f.updates = append(f.updates, accountID+"/"+vehicleID)
	return f.err
}

func TestDeriverCalledAfterPass(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	der := &fakeDeriver{err: errors.New("database unavailable")}
	c := New(&scriptedAPI{}, &memStore{targets: []Target{target()}}, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, der)
	for range 2 { // the second pass has nothing due
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(der.updates) != 1 || der.updates[0] != "a/v" {
		t.Errorf("updates = %v, want one after the first pass", der.updates)
	}

	// Nothing saved (storage down): nothing to derive.
	der = &fakeDeriver{}
	st := &failingStore{memStore{targets: []Target{target()}}}
	if err := New(&scriptedAPI{}, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, der).PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(der.updates) != 0 {
		t.Errorf("updates = %v after a pass that saved nothing", der.updates)
	}
}

// fakePublisher records what it is asked, and fails as told.
type fakePublisher struct {
	vehicles  []map[string][]string
	syncs     []map[string]bool
	published []string
	err       error
}

func (f *fakePublisher) Sync(_ context.Context, vehicles map[string][]string, withheld map[string]bool) error {
	f.vehicles = append(f.vehicles, vehicles)
	f.syncs = append(f.syncs, withheld)
	return f.err
}

func (f *fakePublisher) Publish(_ context.Context, accountID, vehicleID string) error {
	f.published = append(f.published, accountID+"/"+vehicleID)
	return f.err
}

// TestPublisher: each pass synchronizes the publisher with every account's vehicles, those
// the policy leaves unread included, and the accounts it withholds; a vehicle whose pass
// saved a snapshot is published, even when its derivation failed, unless its account is
// withheld; a publisher that fails stops nothing.
func TestPublisher(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC))
	other := Target{AccountID: "b", VehicleID: "x", VIN: simVIN, ConnectionID: "d"}
	der := &fakeDeriver{err: errors.New("database unavailable")}
	unread := target()
	unread.VehicleID = "w"
	c := New(&scriptedAPI{}, &memStore{targets: []Target{target(), unread, other}}, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, der)
	pub := &fakePublisher{err: errors.New("broker of account a: unreachable")}
	c.SetPublisher(pub)
	c.SetPolicy(&readsPolicy{reads: map[string]Reads{"a": {Only: []string{"v"}}, "b": {NoPublish: true}}})
	for range 2 { // the second pass has nothing due
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(pub.syncs) != 2 || len(pub.syncs[0]) != 1 || !pub.syncs[0]["b"] {
		t.Errorf("syncs = %v, want two, b withheld", pub.syncs)
	}
	if v := pub.vehicles[0]; len(v) != 2 || !slices.Equal(v["a"], []string{"v", "w"}) || !slices.Equal(v["b"], []string{"x"}) {
		t.Errorf("vehicles = %v, want a's v and w, b's x", v)
	}
	if len(pub.published) != 1 || pub.published[0] != "a/v" {
		t.Errorf("published = %v, want a/v once, b withheld", pub.published)
	}
	if len(der.updates) != 2 {
		t.Errorf("updates = %v: the publisher's failure stopped the derivation", der.updates)
	}
}

// readsPolicy reads the accounts it lists as it says, or fails.
type readsPolicy struct {
	reads map[string]Reads
	err   error
}

func (p *readsPolicy) Reads(context.Context, time.Time) (map[string]Reads, error) {
	return p.reads, p.err
}

// TestPolicy: an account the policy limits is read at its pace, whatever the mode, and
// the policy's failure keeps the pace it gave; lifted, the adaptive reading comes back
// at the next pass.
func TestPolicy(t *testing.T) {
	client, clk := simulator(t)
	api := &countingAPI{API: client, clk: clk}
	st := &memStore{targets: []Target{target()}}
	c := New(api, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	p := &readsPolicy{reads: map[string]Reads{"a": {Every: time.Hour}, "other": {Every: time.Minute}}}
	c.SetPolicy(p)
	day := clk.Now().Truncate(24 * time.Hour)
	poll := func(until time.Time) {
		for clk.Now().Before(until) {
			if err := c.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			clk.Advance(15 * time.Second)
		}
	}
	calls := func(ep volvo.Endpoint, from, to time.Time) int {
		n := 0
		for _, ts := range api.calls[ep] {
			if !ts.Before(from) && ts.Before(to) {
				n++
			}
		}
		return n
	}

	// The outbound trip, from 07:00 to 07:40, and its arrival, read at most hourly.
	poll(day.Add(8 * time.Hour))
	p.err = errors.New("database down")
	lifted := day.Add(12*time.Hour + 30*time.Minute)
	poll(lifted)
	start := api.calls[volvo.EngineStatus][0]
	if start.Minute() == 30 {
		t.Fatalf("the first call at %v: the hourly calls are due when the policy is lifted", start)
	}
	for _, ep := range []volvo.Endpoint{volvo.EngineStatus, volvo.EnergyState, volvo.Odometer, volvo.Location} {
		if n, most := calls(ep, start, lifted), int(lifted.Sub(start)/time.Hour)+1; n > most {
			t.Errorf("%s: %d calls since %v, at most %d hourly", ep, n, start, most)
		}
	}
	if n := calls(volvo.Details, start, lifted); n != 1 {
		t.Errorf("details: %d calls, want 1 a day", n)
	}

	// Lifted before the return trip, from 16:40: the parked pace at once, then driving
	// followed every minute.
	p.reads, p.err = nil, nil
	poll(lifted.Add(11 * time.Minute))
	if n := calls(volvo.EngineStatus, lifted, lifted.Add(11*time.Minute)); n == 0 {
		t.Error("engine-status not read within a parked interval once the policy was lifted")
	}
	poll(day.Add(17 * time.Hour))
	if n := calls(volvo.EngineStatus, day.Add(16*time.Hour+50*time.Minute), day.Add(17*time.Hour)); n < 9 {
		t.Errorf("engine-status during the return trip: %d calls, want one a minute", n)
	}
}

// readStore records the vehicles whose readings are saved, changed or not.
type readStore struct {
	*memStore
	read map[string]bool
}

func (r *readStore) SaveSnapshot(ctx context.Context, s Snapshot) (bool, error) {
	if r.read == nil {
		r.read = map[string]bool{}
	}
	r.read[s.VehicleID] = true
	return r.memStore.SaveSnapshot(ctx, s)
}

// TestPolicyOnly: the vehicles of an account the policy leaves out are not read, those
// of other accounts are; let in again, they are read at the next pass.
func TestPolicyOnly(t *testing.T) {
	client, clk := simulator(t)
	other := Target{AccountID: "b", VehicleID: "x", VIN: simVIN, ConnectionID: "d"}
	second := target()
	second.VehicleID = "w"
	st := &readStore{memStore: &memStore{targets: []Target{target(), second, other}}}
	c := New(client, st, fixedToken{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
	p := &readsPolicy{reads: map[string]Reads{"a": {Only: []string{"v"}}}}
	c.SetPolicy(p)
	read := func() map[string]bool {
		got := st.read
		st.read = nil
		return got
	}

	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := read(); !got["v"] || got["w"] || !got["x"] {
		t.Errorf("vehicles read = %v, want v and x, not w", got)
	}
	for _, s := range st.statuses {
		if s.VehicleID == "w" {
			t.Errorf("a status written for w, which is not read: %+v", s)
		}
	}

	p.reads = map[string]Reads{"a": {Only: []string{}}}
	clk.Advance(time.Hour)
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := read(); got["v"] || got["w"] {
		t.Errorf("vehicles read = %v, want none of a's under an empty Only", got)
	}

	p.reads = nil
	clk.Advance(time.Minute)
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := read(); !got["v"] || !got["w"] {
		t.Errorf("vehicles read = %v once the policy is lifted, want v and w", got)
	}
}
