package collector

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
	"runsten/internal/volvo"
)

// ownKey is a target reading with its connection's own application key.
func ownKey(account, vehicle, vin, connection string, setAt time.Time) Target {
	return Target{AccountID: account, VehicleID: vehicle, VIN: vin, ConnectionID: connection, KeySetAt: setAt}
}

var keySet = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// TestBudgetPerKey: a connection with its own key has its own budget, apart from the
// instance's and from another connection's, and does not weigh in the order of the
// accounts sharing the instance's.
func TestBudgetPerKey(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	b := newBudget(Quota{Daily: 27}, now) // a burst of one call
	a := ownKey("a", "va", "VIN-A", "ca", keySet)
	bb := ownKey("b", "vb", "VIN-B", "cb", keySet)
	shared := Target{AccountID: "c", VehicleID: "vc", ConnectionID: "cc"}
	b.spend(a, volvo.APIEnergy, now)
	if ok, _ := b.allows(a, volvo.APIEnergy, now); ok {
		t.Error("a spent its key's budget")
	}
	for _, other := range []Target{bb, shared} {
		if ok, _ := b.allows(other, volvo.APIEnergy, now); !ok {
			t.Errorf("%s: blocked by the budget of a's key", other.AccountID)
		}
	}
	if b.usage["a"] != 0 {
		t.Errorf("a's own calls weigh %.1f among the accounts of the instance's key", b.usage["a"])
	}
	b.spend(shared, volvo.APIEnergy, now)
	if ok, _ := b.allows(Target{AccountID: "d"}, volvo.APIEnergy, now); ok {
		t.Error("the instance's key is shared: d should wait after c")
	}
	if ok, _ := b.allows(bb, volvo.APIEnergy, now); !ok {
		t.Error("b's own key: blocked by the instance's budget")
	}

	// A restart counts each account's saved calls on the key its connection has now.
	saved := b.unsavedCalls()
	r := newBudget(Quota{Daily: 27}, now)
	r.restore(saved, []Target{a, bb, shared}, now)
	if ok, _ := r.allows(a, volvo.APIEnergy, now); ok {
		t.Error("after a restart, a's key has its budget again")
	}
	if ok, _ := r.allows(bb, volvo.APIEnergy, now); !ok {
		t.Error("after a restart, b's key is charged with another's calls")
	}
	if ok, _ := r.allows(shared, volvo.APIEnergy, now); ok {
		t.Error("after a restart, the instance's key has its budget again")
	}
	if r.usage["a"] != 0 || r.usage["c"] == 0 {
		t.Errorf("usages after a restart: %v, want c's only", r.usage)
	}
}

// keysSim starts the simulator on the commuter and the parked vehicle, one Volvo ID,
// with limits: each key its own quota.
func keysSim(t *testing.T, limits volvoapi.Limits) (*volvo.Client, *clock.Manual, *scenario.Scenario, *scenario.Scenario) {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/commute.yaml")
	if err != nil {
		t.Fatal(err)
	}
	commute, parked := parseScenario(t, string(data)), parseScenario(t, parkedAllDay)
	fleet, err := scenario.NewFleet([]*scenario.Scenario{commute, parked}, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var srcs []volvoapi.Source
	for _, sim := range fleet.Simulations() {
		srcs = append(srcs, sim)
	}
	clk := clock.NewManual(fleet.Start())
	srv := httptest.NewServer(volvoapi.NewHandler(srcs, clk, limits, volvoapi.Faults{}, volvoapi.DefaultOAuth()))
	t.Cleanup(srv.Close)
	return volvo.NewClient(srv.URL, "instance-key", srv.Client()), clk, commute, parked
}

// TestQuotaPerKeyEndToEnd: two accounts, each with its own key, against a simulator
// that counts the quota per key. The commuter exhausts its key's quota; the parked
// vehicle of the other account reads all day, never refused. With the instance's key
// shared, the commuter's calls would exhaust it for both.
func TestQuotaPerKeyEndToEnd(t *testing.T) {
	limits := volvoapi.Limits{DailyQuota: 320, Keys: []string{"key-a", "key-b", "instance-key"}}
	play := func(own bool) (commuterRefused, parkedRefused int, st *memStore) {
		client, clk, commute, parked := keysSim(t, limits)
		st = &memStore{targets: fleetOfTwo(commute, parked)}
		if own {
			st.targets[0].KeySetAt, st.targets[1].KeySetAt = keySet, keySet
			st.keys = map[string]string{"c1": "key-a", "c2": "key-b"}
		}
		q := DefaultQuota()
		q.NoInstanceKey = own // with the instance's key, the accounts' keys are not used
		rec := &recorder{}
		playDay(t, New(client, st, fixedToken{}, clk, DefaultIntervals(), q, quiet, rec, nil), clk)
		for _, c := range rec.calls {
			if c.Status != 403 {
				continue
			}
			if c.AccountID == "commuter" {
				commuterRefused++
			} else {
				parkedRefused++
			}
		}
		return commuterRefused, parkedRefused, st
	}

	commuter, parked, st := play(true)
	if commuter == 0 {
		t.Fatal("the commuter never exhausted its key's quota: the test proves nothing")
	}
	if parked != 0 {
		t.Errorf("parked account refused %d times: the commuter's key exhausted its quota", parked)
	}
	for _, s := range st.statuses {
		if s.AccountID == "parked" && len(s.Quota) > 0 {
			t.Errorf("parked account told of an exhausted quota: %v", s.Quota)
		}
	}

	if _, parked, _ := play(false); parked == 0 {
		t.Error("the instance's key shared: the parked account should run out with the commuter")
	}
}

// keyTokens is a TokenSource that counts its refreshes and keep-alives.
type keyTokens struct{ refreshes, keepAlives int }

func (*keyTokens) Token(context.Context, string, string) (string, error) { return "token", nil }

func (k *keyTokens) Refresh(context.Context, string, string, string) (string, error) {
	k.refreshes++
	return "token", nil
}

func (k *keyTokens) KeepAlive(context.Context) error {
	k.keepAlives++
	return nil
}

// TestKeyRefused: a refused key stops the reads of its vehicles, not of the others, and
// is not taken for a refused token: no refresh, no re-authentication, the keep-alive
// goes on. The connection's own key is marked refused, until the user sets another;
// the instance's is tried again after keyRetry. The key never shows in the logs.
func TestKeyRefused(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	refused := &volvo.APIError{Status: 401, Kind: volvo.KindKeyRefused, Message: "Access denied due to invalid VCC-API-KEY."}

	t.Run("own key", func(t *testing.T) {
		clk := clock.NewManual(t0)
		api := &keyedAPI{refuse: map[string]error{"bad-key": refused}}
		st := &memStore{
			targets: []Target{
				ownKey("a", "va", "VIN-A", "ca", keySet),
				ownKey("a", "va2", "VIN-A2", "ca", keySet), // the same connection
				ownKey("b", "vb", "VIN-B", "cb", keySet),
			},
			keys: map[string]string{"ca": "bad-key", "cb": "key-b"},
		}
		var logs bytes.Buffer
		tokens := &keyTokens{}
		hosted := Quota{Daily: 10_000, NoInstanceKey: true}
		c := New(api, st, tokens, clk, DefaultIntervals(), hosted, slog.New(slog.NewJSONHandler(&logs, nil)), nil, nil)
		for range 3 {
			if err := c.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			// The store says what the collector saved, as the real one would.
			st.targets[0].KeyRefused = !st.refused["ca"].IsZero()
			st.targets[1].KeyRefused = st.targets[0].KeyRefused
			clk.Advance(DefaultIntervals().Parked)
		}
		if n := api.calls["bad-key"]; n != 1 {
			t.Errorf("%d calls with the refused key, want 1", n)
		}
		if api.calls["key-b"] < 3 {
			t.Errorf("%d calls with b's key: b's reads stopped", api.calls["key-b"])
		}
		if !st.refused["ca"].Equal(keySet) {
			t.Errorf("refused keys saved: %v", st.refused)
		}
		if tokens.refreshes != 0 || tokens.keepAlives != 3 {
			t.Errorf("%d refreshes, %d keep-alives: want none, and one a pass", tokens.refreshes, tokens.keepAlives)
		}
		last := map[string]Status{}
		for _, s := range st.statuses {
			last[s.VehicleID] = s
		}
		if s := last["va"]; s.Failure.Kind != FailKeyRefused || !s.NextAt.IsZero() {
			t.Errorf("va: failure %+v, next %v; want key_refused, nothing due", s.Failure, s.NextAt)
		}
		if s := last["vb"]; s.Failure.Kind != "" || s.NextAt.IsZero() {
			t.Errorf("vb: %+v", s)
		}
		if strings.Contains(logs.String(), "bad-key") {
			t.Errorf("the key is in the logs: %s", logs.String())
		}
		if n := strings.Count(logs.String(), "application key refused"); n != 2 {
			t.Errorf("%d log lines of the refusal, want one when refused, one when skipped", n)
		}

		// A new key: read again.
		st.targets[0].KeySetAt, st.targets[0].KeyRefused = keySet.Add(time.Hour), false
		st.targets[1].KeySetAt, st.targets[1].KeyRefused = keySet.Add(time.Hour), false
		st.keys["ca"] = "good-key"
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if api.calls["good-key"] == 0 {
			t.Error("the new key is not used")
		}
	})

	t.Run("instance key", func(t *testing.T) {
		clk := clock.NewManual(t0)
		api := &keyedAPI{refuse: map[string]error{"": refused}}
		st := &memStore{targets: []Target{target()}}
		c := New(api, st, &keyTokens{}, clk, DefaultIntervals(), DefaultQuota(), quiet, nil, nil)
		for range 6 { // an hour
			_ = c.PollOnce(context.Background())
			clk.Advance(DefaultIntervals().Parked)
		}
		if n := api.calls[""]; n != 1 {
			t.Errorf("%d calls within keyRetry, want 1", n)
		}
		if len(st.refused) != 0 {
			t.Errorf("the instance's key marked on a connection: %v", st.refused)
		}
		if s := st.statuses[len(st.statuses)-1]; s.Failure.Kind != FailKeyRefused || s.NextAt.Before(t0.Add(keyRetry)) {
			t.Errorf("status %+v: want key_refused, due after keyRetry", s)
		}
		clk.Advance(t0.Add(keyRetry).Sub(clk.Now()))
		_ = c.PollOnce(context.Background())
		if n := api.calls[""]; n != 2 {
			t.Errorf("%d calls after keyRetry, want the key tried again", n)
		}
	})
}

// TestNoInstanceKey: without the instance's key, a connection without its own is not
// read, nothing is due for it, and it is logged once; one with its own key is read.
// With the instance's key (self-hosting), it reads every connection: an account's key
// left in the database is not used.
func TestNoInstanceKey(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	targets := []Target{
		{AccountID: "a", VehicleID: "va", VIN: "VIN-A", ConnectionID: "ca"},
		ownKey("b", "vb", "VIN-B", "cb", keySet),
	}
	for _, tt := range []struct {
		name          string
		q             Quota
		instance, own int // calls with the instance's key and with key-b, in two passes
	}{
		{"hosted", Quota{Daily: 10_000, NoInstanceKey: true}, 0, 14},
		{"self-hosted", DefaultQuota(), 28, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clk := clock.NewManual(t0)
			api := &keyedAPI{}
			st := &memStore{targets: targets, keys: map[string]string{"cb": "key-b"}}
			var logs bytes.Buffer
			c := New(api, st, &keyTokens{}, clk, DefaultIntervals(), tt.q, slog.New(slog.NewJSONHandler(&logs, nil)), nil, nil)
			for range 2 {
				if err := c.PollOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				clk.Advance(time.Minute)
			}
			if api.calls[""] != tt.instance || api.calls["key-b"] != tt.own {
				t.Errorf("calls by key: %v, want %d with the instance's, %d with key-b", api.calls, tt.instance, tt.own)
			}
			last := map[string]Status{}
			for _, s := range st.statuses {
				last[s.VehicleID] = s
			}
			if got := last["va"].NextAt.IsZero(); got != tt.q.NoInstanceKey {
				t.Errorf("va: nothing due %v, want %v", got, tt.q.NoInstanceKey)
			}
			n := strings.Count(logs.String(), "without an application key")
			if want := map[bool]int{true: 1, false: 0}[tt.q.NoInstanceKey]; n != want {
				t.Errorf("%d log lines of the missing key, want %d", n, want)
			}
		})
	}
}

// keyedAPI counts the calls per application key (empty: the instance's), and refuses
// those of some keys.
type keyedAPI struct {
	refuse map[string]error
	calls  map[string]int
}

func (k *keyedAPI) Fetch(_ context.Context, key, _, _ string, _ volvo.Endpoint) ([]byte, error) {
	if k.calls == nil {
		k.calls = map[string]int{}
	}
	k.calls[key]++
	if err := k.refuse[key]; err != nil {
		return nil, err
	}
	return []byte(`{}`), nil
}
