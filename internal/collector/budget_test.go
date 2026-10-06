package collector

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
	"runsten/internal/volvo"
)

// TestBudgetBound: calling whenever the budget allows, the calls of any 24 hours stay
// below the quota less its reserve, the first burst included.
func TestBudgetBound(t *testing.T) {
	start := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	b := newBudget(Quota{Daily: 1000}, start)
	var calls []time.Time
	for now := start; now.Before(start.Add(72 * time.Hour)); now = now.Add(10 * time.Second) {
		for {
			if ok, _ := b.allows(Target{AccountID: "a"}, volvo.APIEnergy, now); !ok {
				break
			}
			b.spend(Target{AccountID: "a"}, volvo.APIEnergy, now)
			calls = append(calls, now)
		}
	}
	if first := len(calls) - countFrom(calls, start.Add(time.Second)); first != 37 {
		t.Errorf("%d calls at once, expected the burst: 37 (an hour of 900 a day)", first)
	}
	for i, from := range calls {
		if n := countFrom(calls[i:], from) - countFrom(calls[i:], from.Add(24*time.Hour)); n > 900 {
			t.Fatalf("%d calls in the 24 hours from %v, more than 900", n, from)
		}
	}
	if n := countFrom(calls, start.Add(48*time.Hour)); n < 860 {
		t.Errorf("%d calls on the third day, the budget is not spent", n)
	}
}

// countFrom counts the calls at from or later.
func countFrom(calls []time.Time, from time.Time) int {
	n := 0
	for _, c := range calls {
		if !c.Before(from) {
			n++
		}
	}
	return n
}

// TestBudgetPerAPI: each API has its own budget; the retry of a rejected token, spent
// without asking, may run it below zero.
func TestBudgetPerAPI(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	b := newBudget(Quota{Daily: 27}, now) // a burst of one call
	if ok, changed := b.allows(Target{AccountID: "a"}, volvo.APILocation, now); !ok || changed {
		t.Fatalf("allows = %v, %v; want the first call, and no change", ok, changed)
	}
	b.spend(Target{AccountID: "a"}, volvo.APILocation, now)
	b.spend(Target{AccountID: "a"}, volvo.APILocation, now)
	if ok, changed := b.allows(Target{AccountID: "a"}, volvo.APILocation, now); ok || !changed {
		t.Errorf("allows = %v, %v; want the budget spent, a change", ok, changed)
	}
	if ok, _ := b.allows(Target{AccountID: "a"}, volvo.APIEnergy, now); !ok {
		t.Error("the energy API has its own budget")
	}
	now = now.Add(time.Hour) // under one call accrued: still below zero
	if ok, changed := b.allows(Target{AccountID: "a"}, volvo.APILocation, now); ok || changed {
		t.Errorf("allows = %v, %v; want still spent, no change", ok, changed)
	}
	now = now.Add(3 * time.Hour)
	if ok, changed := b.allows(Target{AccountID: "a"}, volvo.APILocation, now); !ok || !changed {
		t.Errorf("allows = %v, %v; want the budget back, a change", ok, changed)
	}
}

// TestBudgetOrder: the accounts that called the least lately go first; past calls
// fade, and ties keep their order.
func TestBudgetOrder(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	b := newBudget(DefaultQuota(), now)
	for range 10 {
		b.spend(Target{AccountID: "heavy"}, volvo.APIConnectedVehicle, now)
	}
	now = now.Add(usageHalfLife)
	for range 6 {
		b.spend(Target{AccountID: "light"}, volvo.APIEnergy, now)
	}
	targets := []Target{
		{AccountID: "heavy", VehicleID: "h1"},
		{AccountID: "light", VehicleID: "l1"},
		{AccountID: "idle", VehicleID: "i1"},
		{AccountID: "heavy", VehicleID: "h2"},
	}
	b.order(targets, now)
	var got []string
	for _, t := range targets {
		got = append(got, t.VehicleID)
	}
	if want := "i1 h1 h2 l1"; strings.Join(got, " ") != want { // heavy: 10 halved to 5 < 6
		t.Errorf("order = %s, want %s", strings.Join(got, " "), want)
	}
	b.order(targets, now.Add(30*24*time.Hour))
	if got := targets[0].VehicleID + targets[1].VehicleID; got != "i1h1" {
		t.Errorf("after a month, the usages faded to nothing: order kept, got %s first", got)
	}
}

// TestBudgetKeepsUnderQuota: told the simulator's quota of 150 calls a day, the
// collector spreads it over the day instead of running out in the morning: the API
// never refuses a call, and the state is still read in the evening.
func TestBudgetKeepsUnderQuota(t *testing.T) {
	client, clk := simulatorFor(t, "quota-exhausted.yaml")
	api := &countingAPI{API: client, clk: clk}
	rec := &recorder{}
	st := &memStore{targets: []Target{target()}}
	st.targets[0].VIN = "YV1SMLT0000XH0001"
	playDay(t, New(api, st, fixedToken{}, clk, DefaultIntervals(), Quota{Daily: 150}, quiet, rec, nil), clk)

	for _, c := range rec.calls {
		if c.Status == 403 {
			t.Fatalf("%s refused at %v: %s", c.Endpoint, c.At, c.Err)
		}
	}
	byAPI := map[string]int{}
	var lastEngine time.Time
	for ep, calls := range api.calls {
		byAPI[ep.API()] += len(calls)
		if ep == volvo.EngineStatus {
			lastEngine = calls[len(calls)-1]
		}
	}
	t.Logf("calls per API: %v", byAPI)
	// The day played lasts 16 hours: a burst of 5.6 calls, then 135 less the burst a day.
	if n := byAPI[volvo.APIConnectedVehicle]; n > 92 || n < 85 {
		t.Errorf("Connected Vehicle: %d calls, expected about 91", n)
	}
	if evening := clk.Now().Truncate(24 * time.Hour).Add(20 * time.Hour); lastEngine.Before(evening) {
		t.Errorf("engine status last read at %v, expected in the evening", lastEngine)
	}
}

// parkedAllDay is a vehicle of another account that stays parked the whole day.
const parkedAllDay = `
vin: YV1SMLT0000PK0003
start: 2026-09-28T05:00:00Z
vehicle:
  profile: bev-generic
  model: EX-SIM
  modelYear: 2026
  batteryKWh: 80
  consumptionKWhPer100km: 18
  soc: 70
  targetSoc: 80
  odometerKm: 8000
  place: home
places:
  home: { lat: 45.7400, lon: 4.8400 }
steps:
  - park: { duration: 20h }
`

// TestBudgetFairness: two accounts share a budget their demand exceeds. The one that
// asks less, parked all day, gets all its calls, as with no budget; the commuter, first
// in the list and driving, gets what remains.
func TestBudgetFairness(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/commute.yaml")
	if err != nil {
		t.Fatal(err)
	}
	commute, parked := parseScenario(t, string(data)), parseScenario(t, parkedAllDay)
	cv := func(q Quota) (commuter, other int) {
		e := startFleet(t, commute, parked)
		rec := &recorder{}
		st := &memStore{targets: fleetOfTwo(commute, parked)}
		playDay(t, New(e.client, st, fixedToken{}, e.clk, DefaultIntervals(), q, quiet, rec, nil), e.clk)
		for _, c := range rec.calls {
			if c.Endpoint.API() != volvo.APIConnectedVehicle {
				continue
			}
			if c.AccountID == "commuter" {
				commuter++
			} else {
				other++
			}
		}
		return commuter, other
	}
	freeCommuter, freeOther := cv(DefaultQuota())
	commuter, other := cv(Quota{Daily: 1000})
	t.Logf("Connected Vehicle calls: commuter %d of %d, parked %d of %d", commuter, freeCommuter, other, freeOther)
	if other != freeOther {
		t.Errorf("parked account: %d calls, want %d, as with no budget", other, freeOther)
	}
	if commuter >= freeCommuter {
		t.Errorf("commuter: %d calls, want fewer than the %d it asks", commuter, freeCommuter)
	}
	if total := commuter + other; total > 612 { // 16 hours: a burst of 37.5, then 862.5 a day
		t.Errorf("%d calls, above the budget of 612", total)
	}
}

// fleetOfTwo returns the targets of the two vehicles, each of its own account, the
// commuter first.
func fleetOfTwo(commute, parked *scenario.Scenario) []Target {
	return []Target{
		{AccountID: "commuter", VehicleID: "commuter", VIN: commute.VIN, ConnectionID: "c1"},
		{AccountID: "parked", VehicleID: "parked", VIN: parked.VIN, ConnectionID: "c2"},
	}
}

// TestBudgetRestore: a budget replayed from the calls saved knows what was spent: a
// restart right after the burst does not spend it again, and the accounts keep their
// order.
func TestBudgetRestore(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 10, 0, 0, time.UTC)
	b := newBudget(Quota{Daily: 1000}, t0) // a burst of 37.5
	for range 37 {
		b.spend(Target{AccountID: "heavy"}, volvo.APIEnergy, t0)
	}
	b.spend(Target{AccountID: "light"}, volvo.APIEnergy, t0)
	b.spend(Target{AccountID: "light"}, volvo.APILocation, t0.Add(-3*time.Hour))
	saved := b.unsavedCalls()
	if len(saved) != 3 || len(b.unsavedCalls()) != 0 {
		t.Fatalf("unsaved calls = %+v, then not forgotten", saved)
	}

	now := t0.Add(5 * time.Minute)
	r := newBudget(Quota{Daily: 1000}, now)
	r.restore(saved, nil, now)
	if ok, _ := r.allows(Target{AccountID: "light"}, volvo.APIEnergy, now); ok {
		t.Errorf("energy: %.1f tokens after a restart, the burst spent before is spent again", r.tokens[unit{api: volvo.APIEnergy}])
	}
	if ok, _ := r.allows(Target{AccountID: "light"}, volvo.APILocation, now); !ok {
		t.Error("location: one call three hours ago, the budget should allow more")
	}
	targets := []Target{{AccountID: "heavy"}, {AccountID: "light"}}
	r.order(targets, now)
	if targets[0].AccountID != "light" {
		t.Errorf("order after a restart: %s first, want light", targets[0].AccountID)
	}
}

// TestBudgetPerUser: with a quota per user, each account has its own budget.
func TestBudgetPerUser(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	b := newBudget(Quota{Daily: 27, PerUser: true}, now) // a burst of one call
	b.spend(Target{AccountID: "a"}, volvo.APIEnergy, now)
	if ok, _ := b.allows(Target{AccountID: "a"}, volvo.APIEnergy, now); ok {
		t.Error("account a spent its budget")
	}
	if ok, _ := b.allows(Target{AccountID: "b"}, volvo.APIEnergy, now); !ok {
		t.Error("account b has its own budget")
	}
}

// quotaOf refuses the energy calls of one VIN, as its user's quota is exhausted.
type quotaOf struct {
	scriptedAPI
	vin string
}

func (q *quotaOf) Fetch(ctx context.Context, key, token, vin string, ep volvo.Endpoint) ([]byte, error) {
	if vin == q.vin && ep == volvo.EnergyState {
		return nil, &volvo.APIError{Kind: volvo.KindQuota, Status: 403, RetryIn: 2 * time.Hour}
	}
	return q.scriptedAPI.Fetch(ctx, key, token, vin, ep)
}

// TestQuotaRefusalScope: an exhausted quota blocks the API for every account when the
// application has one quota, for its own account only when each user has one.
func TestQuotaRefusalScope(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	targets := []Target{
		{AccountID: "a", VehicleID: "va", VIN: "VIN-A", ConnectionID: "ca"},
		{AccountID: "b", VehicleID: "vb", VIN: "VIN-B", ConnectionID: "cb"},
	}
	for _, tt := range []struct {
		perUser bool
		want    int // energy calls of the second pass
	}{{false, 0}, {true, 1}} {
		clk := clock.NewManual(t0)
		api := &quotaOf{vin: "VIN-A"}
		st := &memStore{targets: slices.Clone(targets)}
		c := New(api, st, fixedToken{}, clk, DefaultIntervals(), Quota{Daily: 10_000, PerUser: tt.perUser}, quiet, nil, nil)
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		first := api.calls[volvo.EnergyState]
		clk.Advance(DefaultIntervals().Parked)
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := api.calls[volvo.EnergyState] - first; got != tt.want {
			t.Errorf("per user %v: %d energy calls after the refusal, want %d", tt.perUser, got, tt.want)
		}
		var quotaB bool
		for _, s := range st.statuses {
			if s.VehicleID == "vb" && !s.Quota[volvo.APIEnergy].IsZero() {
				quotaB = true
			}
		}
		if quotaB == tt.perUser {
			t.Errorf("per user %v: account b told of an exhausted quota: %v", tt.perUser, quotaB)
		}
	}
}

// TestCallsSaved: the calls of each pass are saved, kept when the save fails, and a
// restarted collector starts from them: its first pass waits for the budget the other
// spent.
func TestCallsSaved(t *testing.T) {
	client, clk := simulatorFor(t, "quota-exhausted.yaml")
	st := &memStore{targets: []Target{target()}}
	st.targets[0].VIN = "YV1SMLT0000XH0001"
	q := Quota{Daily: 150}
	c := New(client, st, fixedToken{}, clk, DefaultIntervals(), q, quiet, nil, nil)
	for range 4 * 60 * 4 { // four hours, a pass every 15 s
		if err := c.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(15 * time.Second)
	}
	cvSaved := func() int {
		n := 0
		for _, calls := range st.calls {
			if calls.API == volvo.APIConnectedVehicle {
				n += calls.N
			}
		}
		return n
	}
	saved := cvSaved()
	if saved < 20 {
		t.Fatalf("%d Connected Vehicle calls saved, expected the burst and four hours", saved)
	}

	// A failed save keeps the calls for the next pass.
	st.callsErr = errors.New("database down")
	clk.Advance(time.Hour)
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cvSaved() != saved {
		t.Fatal("calls saved while the database is down")
	}
	st.callsErr = nil
	if err := c.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cvSaved() <= saved {
		t.Error("the calls made while the database was down are lost")
	}

	st.callsErr = errors.New("database down")
	if err := New(client, st, fixedToken{}, clk, DefaultIntervals(), q, quiet, nil, nil).PollOnce(context.Background()); err == nil {
		t.Error("a collector that cannot read the calls saved polls anyway")
	}
	st.callsErr = nil
	rec := &recorder{}
	restarted := New(client, st, fixedToken{}, clk, DefaultIntervals(), q, quiet, rec, nil)
	if err := restarted.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range rec.calls {
		if call.Endpoint.API() == volvo.APIConnectedVehicle {
			t.Errorf("restarted collector called %s at once: the budget spent before is forgotten", call.Endpoint)
		}
	}
}
