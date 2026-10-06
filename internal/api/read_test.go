package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
)

var update = flag.Bool("update", false, "rewrite the golden files of testdata/")

// golden compares a JSON body, indented, with testdata/name: the files are the
// contract of the API. go test ./internal/api -update rewrites them.
func golden(t *testing.T, name, body string) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(body), "", "  "); err != nil {
		t.Fatalf("%s: invalid JSON %q: %v", name, body, err)
	}
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // test file
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("%s differs from the contract:\n%s\nwant:\n%s", name, buf.String(), want)
	}
}

// memReader is an in-memory Reader, States and ChargeCosts, filtering and paging like
// the store. The currency and places are those of its memSettings.
type memReader struct {
	vehicles map[string][]Vehicle // by account
	trips    map[string][]core.Trip
	charges  map[string][]core.Charge
	current  map[string][]core.Record
	entered  []EnteredCost
	settings *memSettings
	next     int // of the entered costs' IDs
	writes   int // calls of the methods that write the entered costs
	err      error
	costErr  error     // returned by the methods of ChargeCosts when set
	modelErr error     // returned by SetVehicleModel when set
	at       time.Time // last Current time
}

func newMemReader(settings *memSettings) *memReader {
	return &memReader{
		vehicles: map[string][]Vehicle{}, trips: map[string][]core.Trip{},
		charges: map[string][]core.Charge{}, current: map[string][]core.Record{}, settings: settings,
	}
}

func costID(n int) string { return fmt.Sprintf("c0570000-0000-4000-8000-%012d", n) }

// accountOf is the account of a vehicle.
func (m *memReader) accountOf(vehicle string) string {
	for acc, vs := range m.vehicles {
		if slices.ContainsFunc(vs, func(v Vehicle) bool { return v.ID == vehicle }) {
			return acc
		}
	}
	return ""
}

func (m *memReader) enteredOf(vehicle string) []core.EnteredCost {
	var out []core.EnteredCost
	for _, e := range m.entered {
		if e.VehicleID == vehicle {
			out = append(out, e.EnteredCost)
		}
	}
	return out
}

// priced gives charges with every other charge of the vehicle as deciding: attached
// over the whole history, as the store does with fewer.
func (m *memReader) priced(ctx context.Context, acc, vehicle string, charges []core.Charge) PricedCharges {
	var deciding []core.Charge
	for _, c := range m.charges[vehicle] {
		if !slices.ContainsFunc(charges, func(d core.Charge) bool { return d.DetectedAt.Equal(c.DetectedAt) }) {
			deciding = append(deciding, c)
		}
	}
	cur, _ := m.settings.Currency(ctx, acc)
	places, _ := m.settings.Places(ctx, acc)
	return PricedCharges{Charges: charges, Deciding: deciding, Entered: m.enteredOf(vehicle), Currency: cur, Places: places}
}

// attachedTo is the index in m.entered of the cost the charge takes, or -1.
func (m *memReader) attachedTo(vehicle string, c core.Charge) int {
	charges := m.charges[vehicle]
	i := slices.IndexFunc(charges, func(d core.Charge) bool { return d.DetectedAt.Equal(c.DetectedAt) })
	attached, _ := core.AttachCosts(charges, m.enteredOf(vehicle))
	if i < 0 || !attached[i].OK {
		return -1
	}
	return slices.IndexFunc(m.entered, func(e EnteredCost) bool {
		return e.VehicleID == vehicle && e.ChargeDetectedAt.Equal(attached[i].V.ChargeDetectedAt)
	})
}

func (m *memReader) chargeAt(vehicle string, at time.Time) (core.Charge, bool) {
	i := slices.IndexFunc(m.charges[vehicle], func(c core.Charge) bool { return c.DetectedAt.Equal(at) })
	if i < 0 {
		return core.Charge{}, false
	}
	return m.charges[vehicle][i], true
}

func (m *memReader) SetChargeCost(_ context.Context, acc, vehicle string, e core.EnteredCost) error {
	m.writes++
	c, ok := m.chargeAt(vehicle, e.ChargeDetectedAt)
	switch {
	case m.costErr != nil:
		return m.costErr
	case !ok || m.accountOf(vehicle) != acc:
		return ErrNotFound
	}
	e.WindowAfter, e.WindowBefore = c.Start.After, c.End.Before
	if i := m.attachedTo(vehicle, c); i >= 0 {
		m.entered[i].EnteredCost = e
		return nil
	}
	m.next++
	m.entered = append(m.entered, EnteredCost{ID: costID(m.next), VehicleID: vehicle, EnteredAt: t0, EnteredCost: e})
	return nil
}

func (m *memReader) DeleteChargeCost(_ context.Context, acc, vehicle string, at time.Time) error {
	m.writes++
	if m.costErr != nil {
		return m.costErr
	}
	c, ok := m.chargeAt(vehicle, at)
	if !ok || m.accountOf(vehicle) != acc {
		return ErrNotFound
	}
	i := m.attachedTo(vehicle, c)
	if i < 0 {
		return ErrNotFound
	}
	m.entered = slices.Delete(m.entered, i, i+1)
	return nil
}

func (m *memReader) orphans(acc string) []EnteredCost {
	var out []EnteredCost
	for _, v := range m.vehicles[acc] {
		_, orphans := core.AttachCosts(m.charges[v.ID], m.enteredOf(v.ID))
		for _, e := range m.entered {
			if e.VehicleID == v.ID && slices.Contains(orphans, e.EnteredCost) {
				out = append(out, e)
			}
		}
	}
	slices.SortFunc(out, func(a, b EnteredCost) int { return b.WindowAfter.Compare(a.WindowAfter) })
	return out
}

func (m *memReader) OrphanCosts(_ context.Context, acc string) ([]EnteredCost, error) {
	return m.orphans(acc), m.costErr
}

// costOf is the index in m.entered of the account's cost id, or -1.
func (m *memReader) costOf(acc, id string) int {
	return slices.IndexFunc(m.entered, func(e EnteredCost) bool { return e.ID == id && m.accountOf(e.VehicleID) == acc })
}

func (m *memReader) AttachEnteredCost(_ context.Context, acc, id, vehicle string, at time.Time) error {
	m.writes++
	if m.costErr != nil {
		return m.costErr
	}
	k := m.costOf(acc, id)
	c, ok := m.chargeAt(vehicle, at)
	if k < 0 || !ok || m.accountOf(vehicle) != acc {
		return ErrNotFound
	}
	if i := m.attachedTo(vehicle, c); i >= 0 && m.entered[i].ID != id {
		return ErrChargeHasCost
	}
	e := &m.entered[k]
	e.VehicleID, e.ChargeDetectedAt, e.WindowAfter, e.WindowBefore = vehicle, c.DetectedAt, c.Start.After, c.End.Before
	return nil
}

func (m *memReader) DeleteEnteredCost(_ context.Context, acc, id string) error {
	m.writes++
	if m.costErr != nil {
		return m.costErr
	}
	k := m.costOf(acc, id)
	if k < 0 {
		return ErrNotFound
	}
	m.entered = slices.Delete(m.entered, k, k+1)
	return nil
}

func (m *memReader) Vehicles(_ context.Context, acc string) ([]Vehicle, error) {
	return m.vehicles[acc], m.err
}

func (m *memReader) Vehicle(_ context.Context, acc, id string) (Vehicle, bool, error) {
	for _, v := range m.vehicles[acc] {
		if v.ID == id {
			return v, true, m.err
		}
	}
	return Vehicle{}, false, m.err
}

func pageEvents[E any](events []E, q EventQuery, bounds func(E) (core.Bounds, core.Bounds, time.Time)) []E {
	var out []E
	for _, e := range events {
		start, end, detected := bounds(e)
		switch {
		case !q.From.IsZero() && !end.Before.After(q.From):
		case !q.To.IsZero() && !start.After.Before(q.To):
		case q.After != nil && !start.After.Before(q.After.StartedAfter) &&
			(!start.After.Equal(q.After.StartedAfter) || !detected.Before(q.After.DetectedAt)):
		default:
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b E) int {
		sa, _, da := bounds(a)
		sb, _, db := bounds(b)
		if c := sb.After.Compare(sa.After); c != 0 {
			return c
		}
		return db.Compare(da)
	})
	if q.Limit == 0 {
		return out
	}
	return out[:min(len(out), q.Limit)]
}

func tripBounds(t core.Trip) (core.Bounds, core.Bounds, time.Time) {
	return t.Start, t.End, t.DetectedAt
}

func chargeBounds(c core.Charge) (core.Bounds, core.Bounds, time.Time) {
	return c.Start, c.End, c.DetectedAt
}

func (m *memReader) ListTrips(_ context.Context, _, vehicle string, q EventQuery) ([]core.Trip, error) {
	return pageEvents(m.trips[vehicle], q, tripBounds), m.err
}

func (m *memReader) FindTrip(_ context.Context, _, vehicle string, at time.Time) (core.Trip, bool, error) {
	for _, t := range m.trips[vehicle] {
		if t.DetectedAt.Equal(at) {
			return t, true, m.err
		}
	}
	return core.Trip{}, false, m.err
}

func (m *memReader) ListCharges(ctx context.Context, acc, vehicle string, q EventQuery) (PricedCharges, error) {
	return m.priced(ctx, acc, vehicle, pageEvents(m.charges[vehicle], q, chargeBounds)), m.err
}

func (m *memReader) FindCharge(ctx context.Context, acc, vehicle string, at time.Time) (PricedCharges, bool, error) {
	c, ok := m.chargeAt(vehicle, at)
	if !ok {
		return PricedCharges{}, false, m.err
	}
	return m.priced(ctx, acc, vehicle, []core.Charge{c}), true, m.err
}

func (m *memReader) ChargeHistory(ctx context.Context, acc string) ([]PricedCharges, error) {
	var out []PricedCharges
	for _, v := range m.vehicles[acc] {
		out = append(out, m.priced(ctx, acc, v.ID, m.charges[v.ID]))
	}
	return out, m.err
}

// PeriodEvents keeps, like the store, the events that started in [from, to), and the
// latest one of each kind before from: Summarize keeps the latest of both.
func (m *memReader) PeriodEvents(ctx context.Context, acc, vehicle string, from, to time.Time) (Period, error) {
	var orphans []core.EnteredCost
	for _, o := range m.orphans(acc) {
		if o.VehicleID == vehicle {
			orphans = append(orphans, o.EnteredCost)
		}
	}
	return Period{
		Trips:         inPeriod(m.trips[vehicle], from, to, tripBounds),
		PricedCharges: m.priced(ctx, acc, vehicle, inPeriod(m.charges[vehicle], from, to, chargeBounds)),
		Orphans:       orphans,
	}, m.err
}

func inPeriod[E any](events []E, from, to time.Time, bounds func(E) (core.Bounds, core.Bounds, time.Time)) []E {
	var out []E
	var prior []E
	var priorKey EventKey
	for _, e := range events {
		start, _, detected := bounds(e)
		switch {
		case !start.After.Before(to):
		case !start.After.Before(from):
			out = append(out, e)
		case prior == nil || start.After.After(priorKey.StartedAfter) ||
			start.After.Equal(priorKey.StartedAfter) && detected.After(priorKey.DetectedAt):
			prior, priorKey = []E{e}, EventKey{start.After, detected}
		}
	}
	return append(out, prior...)
}

func (m *memReader) Current(_ context.Context, _, vehicle string, at time.Time) (core.Current, error) {
	m.at = at
	return core.Latest(m.current[vehicle]), m.err
}

// Fixtures: a commute day of the vehicle car, whose connection is active; parked, a
// vehicle whose grant is lost; foreign, the vehicle of another account.
const (
	car     = "0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1a01"
	parked  = "0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1a02"
	foreign = "0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1a03"
	// chosen is an EX30 its details do not tell apart from the Twin Motor: its driver
	// chose the Single Motor Extended Range, with the standard charger.
	chosen = "0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1a04"
)

func h(hh, mm int) time.Time {
	return t0.Truncate(24 * time.Hour).Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute)
}

func some[T any](v T) core.Value[T] { return core.Value[T]{V: v, OK: true} }

var (
	home = core.Position{Lat: 45.764, Lon: 4.8357}
	work = core.Position{Lat: 45.7797, Lon: 4.927}
)

// fromAPI is the capacity of the fixtures' energies: 80 kWh, as the vendor reports.
var fromAPI = some(core.Capacity{KWh: 80, Source: core.CapacityAPI})

func fixtures(m *memReader) {
	m.vehicles[account] = []Vehicle{
		{ID: car, VIN: "YV1SMLT0000DT0001", AuthorizedAt: h(5, 0), RefreshedAt: h(6, 0), Collection: &Collection{
			PassedAt: h(7, 0), Mode: "charging", ReadAt: h(6, 59), NextAt: h(7, 1),
			Quota:   map[string]time.Time{"location": h(9, 0), "energy": h(8, 0)},
			Failure: CollectionFailure{At: h(6, 40), Endpoint: "location", Status: 403, Kind: "quota"},
		}},
		{
			ID: parked, VIN: "YV1SMLT0000DT0002", AuthorizedAt: h(5, 0), ReauthReason: "refresh token expired", ReauthAt: h(6, 30),
			Collection: &Collection{
				PassedAt: h(7, 0), Mode: "parked", ReadAt: h(6, 20), PausedUntil: h(7, 10), Quota: map[string]time.Time{},
				Failure: CollectionFailure{At: h(6, 30), Kind: "token"},
			},
		},
		{ID: chosen, VIN: "YV1SMLT0000DT0004", AuthorizedAt: h(5, 0), VariantID: "ex30-er-2024", ACMaxKW: some(11.0)},
	}
	m.vehicles["other"] = []Vehicle{{ID: foreign, VIN: "YV1SMLT0000DT0003"}}
	m.trips[car] = []core.Trip{
		{
			DetectedAt: h(7, 1), Start: core.Bounds{After: h(6, 55), Before: h(7, 1)}, End: core.Bounds{After: h(7, 39), Before: h(7, 40)},
			StartOdometerKm: some(12400.0), EndOdometerKm: some(12432.0), DistanceKm: some(32.0),
			StartSoC: some(62.0), EndSoC: some(54.5), StartRangeKm: some(248.0), EndRangeKm: some(218.0), EnergyKWh: some(6.0),
			Capacity: fromAPI,
			From:     some(home), To: some(work), TripMeterKm: some(32.2), ConsumptionKWhPer100km: some(18.1),
		},
		{
			// Revealed by the odometer after an outage: no position, no vehicle figures.
			DetectedAt: h(13, 0), Reconstructed: true,
			Start: core.Bounds{After: h(12, 0), Before: h(13, 0)}, End: core.Bounds{After: h(12, 0), Before: h(13, 0)},
			StartOdometerKm: some(12432.0), EndOdometerKm: some(12440.0), DistanceKm: some(8.0),
		},
		{
			DetectedAt: h(16, 41), Start: core.Bounds{After: h(16, 35), Before: h(16, 41)}, End: core.Bounds{After: h(17, 24), Before: h(17, 25)},
			// No SoC was read: the capacity is known, the energy is not.
			StartOdometerKm: some(12440.0), EndOdometerKm: some(12473.0), DistanceKm: some(33.0),
			From: some(work), To: some(home),
			Capacity: some(core.Capacity{KWh: 75, Source: core.CapacityCatalogNet}),
		},
	}
	m.charges[car] = []core.Charge{
		{
			DetectedAt: h(18, 30), Start: core.Bounds{After: h(18, 20), Before: h(18, 30)}, End: core.Bounds{After: h(21, 55), Before: h(21, 56)},
			Type: some(core.AC), StartSoC: some(47.0), EndSoC: some(80.0), TargetSoC: some(80.0),
			EnergySoCKWh: some(26.4), EnergyPowerKWh: some(25.3), Position: some(home),
			Capacity: fromAPI,
		},
		{
			DetectedAt: h(4, 0), Reconstructed: true,
			Start: core.Bounds{After: h(1, 0), Before: h(4, 0)}, End: core.Bounds{After: h(1, 0), Before: h(4, 0)},
			StartSoC: some(40.0), EndSoC: some(62.0), EnergySoCKWh: some(17.6),
			Capacity: fromAPI,
		},
	}
	nrg := core.Snapshot{
		Covers: core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection | core.FieldChargeType | core.FieldPower | core.FieldTargetSoC,
		SoC:    core.Some(64.0, h(19, 2)), RangeKm: core.Some(256.0, h(19, 2)),
		Charging: core.Some(core.ChargingActive, h(19, 2)), Connection: core.Some(core.Connected, h(19, 2)),
		ChargeType: core.Some(core.AC, h(19, 2)), PowerW: core.Some(7400.0, h(19, 2)), TargetSoC: core.Some(80.0, h(19, 2)),
	}
	m.current[car] = []core.Record{
		{FetchedAt: h(17, 26), CheckedAt: h(19, 0), Snapshot: core.Snapshot{Covers: core.FieldEngine, Engine: core.Some(core.EngineStopped, h(17, 25))}},
		{FetchedAt: h(19, 3), CheckedAt: h(19, 3), Snapshot: nrg},
		{FetchedAt: h(17, 30), CheckedAt: h(19, 0), Snapshot: core.Snapshot{Covers: core.FieldOdometer, OdometerKm: core.Some(12473.0, h(17, 25))}},
		{FetchedAt: h(17, 30), CheckedAt: h(18, 30), Snapshot: core.Snapshot{Covers: core.FieldPosition, Position: core.Some(home, h(17, 25))}},
		// Details come without a vehicle timestamp: an XC40 of 2021, whose capacity
		// recognizes its variant.
		{FetchedAt: h(5, 0), CheckedAt: h(5, 0), Snapshot: details("XC40", 2021, 78.012, true)},
	}
	m.current[chosen] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 0), Snapshot: details("EX30", 2024, 69.0, true)}}
}

// details is the snapshot of a details response.
func details(family string, year int, kwh float64, electric bool) core.Snapshot {
	return core.Snapshot{
		Covers: core.FieldCapacity | core.FieldModel, CapacityKWh: some(kwh),
		Family: some(family), ModelYear: some(year), BatteryElectric: some(electric),
	}
}

var eur, _ = core.CurrencyOf("EUR")

// homePlace holds the evening charge, whose window spans a change of price: peak hours
// until 22:00 in Paris, off-peak after. With its charger's power, the cost of the
// charge is an interval, 5.24 to 5.79 € (ADR example). The night charge, reconstructed,
// has no position and is at no place: its cost is unknown.
func homePlace(t *testing.T) core.Place {
	t.Helper()
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	return core.Place{
		ID: placeID(1), Name: homeName, Position: home, RadiusM: 100, Location: paris, MaxPowerKW: some(11.0),
		Tariff: []core.TariffVersion{{
			ValidFrom: core.Date{Year: 2026, Month: time.August, Day: 1}, PricePerKWh: 0.2142,
			Windows: []core.PriceWindow{{From: 22 * 60, To: 6 * 60, PricePerKWh: 0.1589}},
		}},
	}
}

func readerEnv(t *testing.T) (*env, *http.Cookie) {
	t.Helper()
	e := newEnv(t, 10)
	fixtures(e.reader)
	e.settings.currency[account] = eur
	e.settings.places[account] = []core.Place{homePlace(t)}
	e.settings.next = 1
	return e, e.session(t, "admin")
}

func TestVehiclesJSON(t *testing.T) {
	e, c := readerEnv(t)
	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles", c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "vehicles.json", body)
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+parked, c)
	golden(t, "vehicle-reauth-required.json", body)

	for _, id := range []string{foreign, "0B5C6C3E-3F0E-4A57-9D3B-2F4F9E8D1A09", "not-a-uuid", "0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1a0'"} {
		for _, suffix := range []string{"", "/state", "/trips", "/charges", "/trips/2026-09-28T07:01:00Z", "/stats", "/battery"} {
			resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+id+suffix, c)
			if resp.status != http.StatusNotFound {
				t.Errorf("%s%s: %d", id, suffix, resp.status)
			}
			wantError(t, body, "not_found")
		}
	}
	// No vehicle yet: an empty list, not null.
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles", e.session(t, "other"))
	if body != `{"items":[{"id":"`+foreign+`","vin":"YV1SMLT0000DT0003",`+
		`"model":{"family":null,"model_year":null,"variant":null,"variant_source":null,"ac_max_kw":null},"connection":{"status":"active","api_key":"instance","authorized_at":null,"refreshed_at":null,"reauth_at":null,"reauth_reason":null},`+
		`"collection":null}]}`+"\n" {
		t.Errorf("other account: %s", body)
	}
	e.reader.vehicles["other"] = nil
	if _, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles", e.session(t, "other")); body != `{"items":[]}`+"\n" {
		t.Errorf("no vehicle: %s", body)
	}
}

func TestVehicleModel(t *testing.T) {
	vin := "YV1EL3AV0R2000001" // motor code EL: the Single Motor of the EX30
	tests := []struct {
		name    string
		vin     string
		details core.Snapshot
		want    string
	}{
		{
			"recognized", vin, details("EX30", 2024, 69.0, true),
			`{"family":"EX30","model_year":2024,"variant":{"id":"ex30-er-2024","name":"EX30 Single Motor Extended Range",` +
				`"gross_kwh":69,"net_kwh":64,"ac_max_kw":11,"ac_option_kw":22,"dc_max_kw":153},"variant_source":"detected","ac_max_kw":null}`,
		},
		{
			"several candidates", "YV1SMLT0000DT0001", details("EX30", 2024, 69.0, true),
			`{"family":"EX30","model_year":2024,"variant":null,"variant_source":null,"ac_max_kw":null}`,
		},
		{
			"unknown family", vin, details("EX-SIM", 2026, 80, true),
			`{"family":"EX-SIM","model_year":2026,"variant":null,"variant_source":null,"ac_max_kw":null}`,
		},
		{
			// It would be recognized, were it battery electric.
			"hybrid", vin, details("EX30", 2024, 69.0, false),
			`{"family":"EX30","model_year":2024,"variant":null,"variant_source":null,"ac_max_kw":null}`,
		},
		{
			"fuel type unknown", vin,
			core.Snapshot{Covers: core.FieldCapacity | core.FieldModel, CapacityKWh: some(69.0), Family: some("EX30")},
			`{"family":"EX30","model_year":null,"variant":null,"variant_source":null,"ac_max_kw":null}`,
		},
		{
			"year and capacity unknown", "",
			core.Snapshot{Covers: core.FieldModel, Family: some("EX90"), BatteryElectric: some(true)},
			`{"family":"EX90","model_year":null,"variant":null,"variant_source":null,"ac_max_kw":null}`,
		},
		{"details never read", vin, core.Snapshot{}, `{"family":null,"model_year":null,"variant":null,"variant_source":null,"ac_max_kw":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, c := readerEnv(t)
			e.reader.vehicles[account][0].VIN = tt.vin
			e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 0), Snapshot: tt.details}}
			resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car, c)
			if resp.status != http.StatusOK {
				t.Fatalf("status %d: %s", resp.status, body)
			}
			var v struct {
				Model json.RawMessage `json:"model"`
			}
			if err := json.Unmarshal([]byte(body), &v); err != nil {
				t.Fatal(err)
			}
			if string(v.Model) != tt.want {
				t.Errorf("model = %s\nwant    %s", v.Model, tt.want)
			}
		})
	}
}

func TestStateJSON(t *testing.T) {
	e, c := readerEnv(t)
	e.clk.Advance(14 * time.Hour)
	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/state", c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "state.json", body)
	if !e.reader.at.Equal(e.clk.Now()) {
		t.Errorf("state read as of %s", e.reader.at)
	}
	// Never read: every value is null, the connection is still known.
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+parked+"/state", c)
	golden(t, "state-unknown.json", body)
}

func TestTripsJSON(t *testing.T) {
	e, c := readerEnv(t)
	base := e.api.URL + "/api/v1/vehicles/" + car + "/trips"

	resp, body := get(t, noFollow(), base+"?limit=2", c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "trips-page-1.json", body)
	var p struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil || p.NextCursor == nil {
		t.Fatalf("page 1: %v", err)
	}
	_, body = get(t, noFollow(), base+"?limit=2&cursor="+*p.NextCursor, c)
	golden(t, "trips-page-2.json", body)

	_, body = get(t, noFollow(), base+"/2026-09-28T07:01:00Z", c)
	golden(t, "trip.json", body)
	// The ID, to the microsecond, finds the trip too.
	if _, again := get(t, noFollow(), base+"/"+p.Items[0].ID, c); again == body || !strings.Contains(again, `"id":"2026-09-28T16:41:00.000000Z"`) {
		t.Errorf("by ID: %s", again)
	}
	for _, id := range []string{"2026-09-28T07:02:00Z", "yesterday"} {
		resp, body := get(t, noFollow(), base+"/"+id, c)
		if resp.status != http.StatusNotFound {
			t.Errorf("%s: %d", id, resp.status)
		}
		wantError(t, body, "not_found")
	}

	t.Run("period: the events that may overlap it", func(t *testing.T) {
		for _, tt := range []struct {
			query string
			ids   []string
		}{
			{"", []string{"16:41", "13:00", "07:01"}},
			{"?from=2026-09-28T12:30:00Z&to=2026-09-28T16:00:00Z", []string{"13:00"}},
			{"?from=2026-09-28T07:40:00Z", []string{"16:41", "13:00"}},                 // ended before 07:40: out
			{"?from=2026-09-28T07:39:30%2B00:00", []string{"16:41", "13:00", "07:01"}}, // may still be driving
			{"?to=2026-09-28T06:55:00Z", nil},                                          // started after 06:55: out
			{"?to=2026-09-28T06:56:00Z", []string{"07:01"}},
		} {
			_, body := get(t, noFollow(), base+tt.query, c)
			var p struct {
				Items []struct {
					DetectedAt time.Time `json:"detected_at"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, it := range p.Items {
				ids = append(ids, it.DetectedAt.Format("15:04"))
			}
			if !slices.Equal(ids, tt.ids) {
				t.Errorf("%q: %v, want %v", tt.query, ids, tt.ids)
			}
		}
	})
	t.Run("invalid parameters", func(t *testing.T) {
		for query, param := range map[string]string{
			"?limit=0": "limit", "?limit=201": "limit", "?limit=ten": "limit",
			"?from=yesterday": "from", "?to=2026-09-28": "to",
			"?from=2026-09-28T12:00:00Z&to=2026-09-28T12:00:00Z": "from must be before to",
			"?cursor=!!":               "cursor",
			"?cursor=bm90LWEtY3Vyc29y": "cursor", // "not-a-cursor"
			"?cursor=MTIuYWI":          "cursor", // "12.ab"
		} {
			resp, body := get(t, noFollow(), base+query, c)
			if resp.status != http.StatusBadRequest || !strings.Contains(body, param) {
				t.Errorf("%s: %d %s", query, resp.status, body)
			}
			wantError(t, body, "invalid_parameter")
		}
	})
}

func TestChargesJSON(t *testing.T) {
	e, c := readerEnv(t)
	base := e.api.URL + "/api/v1/vehicles/" + car + "/charges"
	resp, body := get(t, noFollow(), base, c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "charges.json", body)
	_, body = get(t, noFollow(), base+"/2026-09-28T18:30:00.000000Z", c)
	golden(t, "charge.json", body)
	for _, id := range []string{"2026-09-28T18:31:00Z", "yesterday"} {
		if resp, body := get(t, noFollow(), base+"/"+id, c); resp.status != http.StatusNotFound {
			t.Errorf("charge %s: %d %s", id, resp.status, body)
		}
	}
	if resp, body := get(t, noFollow(), base+"?limit=0", c); resp.status != http.StatusBadRequest {
		t.Errorf("invalid limit: %d %s", resp.status, body)
	}
	// Another vehicle's events are not listed.
	if _, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+parked+"/charges", c); body != `{"items":[],"next_cursor":null}`+"\n" {
		t.Errorf("parked: %s", body)
	}
}

// TestOnboardCharger: the vehicle's onboard charger bounds the power of its AC charges,
// where the place says none. The fixtures' XC40 takes 11 kW: the evening charge costs
// what a charger of 11 kW at the place would give.
func TestOnboardCharger(t *testing.T) {
	tests := []struct {
		name     string
		vin      string
		details  core.Snapshot
		min, max int64
	}{
		{"an XC40 of 11 kW", "", details("XC40", 2021, 78.012, true), 524, 579},
		// An EX30 may have the 22 kW option: nothing says it does not, and 22 kW could
		// have charged either way.
		{"an EX30, whose option is 22 kW", "YV1EL3AV0R2000001", details("EX30", 2024, 69.0, true), 476, 643},
		{"no variant recognized", "", details("EX-SIM", 2026, 80, true), 476, 643},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, c := readerEnv(t)
			place := homePlace(t)
			place.MaxPowerKW = core.Value[float64]{}
			e.settings.places[account] = []core.Place{place}
			if tt.vin != "" {
				e.reader.vehicles[account][0].VIN = tt.vin
			}
			e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 0), Snapshot: tt.details}}
			base := e.api.URL + "/api/v1/vehicles/" + car
			_, body := get(t, noFollow(), base+"/charges/2026-09-28T18:30:00.000000Z", c)
			if tt.min == 524 {
				golden(t, "charge-onboard-charger.json", body)
			}
			// The list and the statistics bound it alike.
			_, list := get(t, noFollow(), base+"/charges", c)
			_, stats := get(t, noFollow(), base+"/stats?from=2026-09-28T00:00:00Z&to=2026-09-29T00:00:00Z", c)
			for _, got := range []string{body, list, stats} {
				if want := fmt.Sprintf(`"min_minor":%d,"max_minor":%d`, tt.min, tt.max); !strings.Contains(got, want) {
					t.Errorf("want %s in %s", want, got)
				}
			}
		})
	}
}

func TestReaderErrors(t *testing.T) {
	e, c := readerEnv(t)
	e.reader.err = errors.New("boom")
	for _, path := range []string{
		"/api/v1/vehicles", "/api/v1/vehicles/" + car, "/api/v1/vehicles/" + car + "/state",
	} {
		resp, body := get(t, noFollow(), e.api.URL+path, c)
		if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
			t.Errorf("%s: %d %s", path, resp.status, body)
		}
		wantError(t, body, "internal")
	}
	// The vehicle check passes, then the event read fails.
	e.server.Reader = &failingEvents{memReader: e.reader}
	e.reader.err = nil
	for _, path := range []string{"/trips", "/charges", "/trips/2026-09-28T07:01:00Z", "/charges/2026-09-28T18:30:00Z", "/stats", "/battery"} {
		resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+path, c)
		if resp.status != http.StatusInternalServerError {
			t.Errorf("%s: %d %s", path, resp.status, body)
		}
	}
	e.server.Reader = e.reader
	e.server.States = failingStates{}
	for _, path := range []string{
		"", "/" + car, "/" + car + "/state",
		// The onboard charger bounds the costs, and the state holds the details the
		// battery's reference capacity reads.
		"/" + car + "/charges", "/" + car + "/charges/2026-09-28T18:30:00Z", "/" + car + "/stats", "/" + car + "/battery",
	} {
		if resp, _ := get(t, noFollow(), e.api.URL+"/api/v1/vehicles"+path, c); resp.status != http.StatusInternalServerError {
			t.Errorf("state of %q: %d", path, resp.status)
		}
	}
	e.sessions.err = errors.New("boom")
	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles", c)
	if resp.status != http.StatusInternalServerError {
		t.Errorf("session check: %d", resp.status)
	}
	wantError(t, body, "internal")
}

type failingEvents struct{ *memReader }

var errEvents = errors.New("events")

func (failingEvents) ListTrips(context.Context, string, string, EventQuery) ([]core.Trip, error) {
	return nil, errEvents
}

func (failingEvents) FindTrip(context.Context, string, string, time.Time) (core.Trip, bool, error) {
	return core.Trip{}, false, errEvents
}

func (failingEvents) ListCharges(context.Context, string, string, EventQuery) (PricedCharges, error) {
	return PricedCharges{}, errEvents
}

func (failingEvents) PeriodEvents(context.Context, string, string, time.Time, time.Time) (Period, error) {
	return Period{}, errEvents
}

func (failingEvents) FindCharge(context.Context, string, string, time.Time) (PricedCharges, bool, error) {
	return PricedCharges{}, false, errEvents
}

type failingStates struct{}

func (failingStates) Current(context.Context, string, string, time.Time) (core.Current, error) {
	return core.Current{}, errEvents
}

func TestCursor(t *testing.T) {
	k := EventKey{StartedAfter: h(6, 55).Add(123 * time.Microsecond), DetectedAt: h(7, 1)}
	got, err := decodeCursor(encodeCursor(k))
	if err != nil || !got.StartedAfter.Equal(k.StartedAfter) || !got.DetectedAt.Equal(k.DetectedAt) {
		t.Errorf("round trip: %+v, %v", got, err)
	}
}
