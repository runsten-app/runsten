package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"runsten/internal/core"
)

// Health merges the vehicle's health records fetched at or before at, as the deriver.
func (m *memReader) Health(_ context.Context, _, vehicle string, at time.Time) (core.CurrentHealth, error) {
	var rs []core.HealthRecord
	for _, r := range m.health[vehicle] {
		if !r.FetchedAt.After(at) {
			rs = append(rs, r)
		}
	}
	return core.LatestHealth(rs), m.err
}

// HealthReadings returns the vehicle's health records read within [from, to].
func (m *memReader) HealthReadings(_ context.Context, _, vehicle string, from, to time.Time, limit int) ([]core.HealthRecord, error) {
	var out []core.HealthRecord
	for _, r := range m.health[vehicle] {
		if !r.CheckedAt.Before(from) && !r.FetchedAt.After(to) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b core.HealthRecord) int { return a.FetchedAt.Compare(b.FetchedAt) })
	return out[:min(limit, len(out))], m.err
}

type failingHealth struct{}

func (failingHealth) Health(context.Context, string, string, time.Time) (core.CurrentHealth, error) {
	return core.CurrentHealth{}, errEvents
}

func (failingHealth) HealthReadings(context.Context, string, string, time.Time, time.Time, int) ([]core.HealthRecord, error) {
	return nil, errEvents
}

// tyresAt is a response of the tyres fetched at f and read again at c, its rear left
// tyre at kPa, low below 250.
func tyresAt(f, c time.Time, kPa float64) core.HealthRecord {
	warning := core.TyreOK
	if kPa < 250 {
		warning = core.TyreLow
	}
	return core.HealthRecord{FetchedAt: f, CheckedAt: c, Health: core.Health{
		Covers: core.HealthTyres | core.HealthService,
		Tyres: map[core.Wheel]core.Tyre{
			core.FrontLeft: {PressureKPa: core.Some(282.0, f), Warning: core.Some(core.TyreOK, f)},
			// A pressure without the monitoring's warning.
			core.FrontRight: {PressureKPa: core.Some(280.0, f)},
			core.RearLeft:   {PressureKPa: core.Some(kPa, f), Warning: core.Some(warning, f)},
			// A wheel whose pressure is unknown.
			core.RearRight: {Warning: core.Some(core.TyreFault, f)},
		},
		RecommendedFrontKPa: core.Some(280.0, f), RecommendedRearKPa: core.Some(290.0, f),
		Service:       core.Some(core.ServiceSoon, f),
		DaysToService: core.Some(21.0, f), DistanceToServiceKm: core.Some(1200.0, f),
		Checks: map[core.Check]core.Value[bool]{
			core.CheckWasherFluid: core.Some(true, f), core.CheckBrakeFluid: core.Some(false, f),
			core.CheckLightBrakeLeft: core.Some(false, f), core.CheckOil: {},
		},
	}}
}

// healthFixtures: the car's tyres read in the afternoon, then nothing for more than 90
// minutes, then read again in the evening, the rear left tyre leaking.
func healthFixtures(m *memReader) {
	m.health = map[string][]core.HealthRecord{car: {
		// The distance of a later response: the service's reported_at is its time.
		{FetchedAt: h(18, 30), CheckedAt: h(18, 30), Health: core.Health{
			Covers: core.HealthService, Service: core.Some(core.ServiceSoon, h(18, 0)),
			DaysToService: core.Some(21.0, h(18, 0)), DistanceToServiceKm: core.Some(1200.0, h(18, 30)),
		}},
		tyresAt(h(18, 0), h(19, 0), 248),
		tyresAt(h(14, 0), h(15, 0), 270),
		// Warnings only: no pressure, out of the series.
		{FetchedAt: h(16, 0), CheckedAt: h(16, 0), Health: core.Health{
			Checks: map[core.Check]core.Value[bool]{core.CheckCoolant: core.Some(false, h(16, 0))},
		}},
	}}
}

func TestHealthJSON(t *testing.T) {
	e, c := readerEnv(t)
	healthFixtures(e.reader)
	e.clk.Advance(18 * time.Hour) // 23:00 UTC

	_, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/health", c)
	golden(t, "health.json", body)
	// A vehicle never read: nothing known.
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+parked+"/health", c)
	golden(t, "health-unknown.json", body)

	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+foreign+"/health", c)
	if resp.status != http.StatusNotFound {
		t.Errorf("foreign: %d", resp.status)
	}
	wantError(t, body, "not_found")

	e.server.Health = failingHealth{}
	defer func() { e.server.Health = e.reader }()
	resp, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/health", c)
	if resp.status != http.StatusInternalServerError {
		t.Errorf("failure: %d %s", resp.status, body)
	}
	wantError(t, body, "internal")
}

type tyreSeries struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Runs []struct {
		Readings []struct {
			At          time.Time `json:"at"`
			RearLeftKPa *float64  `json:"rear_left_kpa"`
		} `json:"readings"`
	} `json:"runs"`
}

func TestTyresJSON(t *testing.T) {
	e, c := readerEnv(t)
	healthFixtures(e.reader)
	e.clk.Advance(18 * time.Hour) // 23:00 UTC
	base := e.api.URL + "/api/v1/vehicles/" + car + "/health/tyres"
	window := func(from, to time.Time) string {
		v := url.Values{"from": {from.Format(time.RFC3339)}}
		if !to.IsZero() {
			v.Set("to", to.Format(time.RFC3339))
		}
		return base + "?" + v.Encode()
	}
	read := func(t *testing.T, target string) tyreSeries {
		t.Helper()
		resp, body := get(t, noFollow(), target, c)
		if resp.status != http.StatusOK {
			t.Fatalf("%s: status %d: %s", target, resp.status, body)
		}
		var s tyreSeries
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	_, body := get(t, noFollow(), window(h(12, 0), time.Time{}), c)
	golden(t, "tyres.json", body)

	t.Run("the history the account sees", func(t *testing.T) {
		e.server.Limits = &historyLimit{account: account, from: h(14, 30)}
		defer func() { e.server.Limits = nil }()
		s := read(t, window(h(12, 0), h(20, 0)))
		if first := s.Runs[0].Readings[0]; !s.From.Equal(h(14, 30)) || len(s.Runs) != 2 || !first.At.Equal(h(14, 30)) ||
			first.RearLeftKPa == nil || *first.RearLeftKPa != 270 {
			t.Errorf("from the limit: %+v", s)
		}
		if s := read(t, window(h(12, 0), h(14, 0))); len(s.Runs) != 0 || !s.From.Equal(h(14, 0)) {
			t.Errorf("before the limit: %+v", s)
		}
	})
	t.Run("nothing read", func(t *testing.T) {
		if s := read(t, window(h(20, 0), h(21, 0))); s.Runs == nil || len(s.Runs) != 0 {
			t.Errorf("no reading: %+v", s)
		}
	})
	t.Run("refused", func(t *testing.T) {
		for _, target := range []string{
			base,
			window(h(19, 0), h(19, 0)),
			window(h(19, 0), h(14, 0)),
			window(h(0, 0), h(0, 0).AddDate(0, 0, 367)),
			base + "?from=yesterday",
		} {
			resp, body := get(t, noFollow(), target, c)
			if resp.status != http.StatusBadRequest {
				t.Errorf("%s: %d %s", target, resp.status, body)
			}
			wantError(t, body, "invalid_parameter")
		}
	})
	t.Run("too many responses", func(t *testing.T) {
		many := make([]core.HealthRecord, maxHealthResponses+1)
		for i := range many {
			at := h(0, 0).Add(time.Duration(i) * 4 * time.Second)
			many[i] = tyresAt(at, at, 280)
		}
		e.reader.health[chosen] = many
		u := e.api.URL + "/api/v1/vehicles/" + chosen + "/health/tyres?from=2026-09-28T00:00:00Z&to=2026-09-29T00:00:00Z"
		resp, body := get(t, noFollow(), u, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%d responses: %d", len(many), resp.status)
		}
		wantError(t, body, "invalid_parameter")
		e.reader.health[chosen] = many[:maxHealthResponses]
		if resp, _ := get(t, noFollow(), u, c); resp.status != http.StatusOK {
			t.Errorf("%d responses: %d", maxHealthResponses, resp.status)
		}
	})
	t.Run("another account's vehicle", func(t *testing.T) {
		resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+foreign+"/health/tyres?from=2026-09-28T00:00:00Z", c)
		if resp.status != http.StatusNotFound {
			t.Errorf("foreign: %d", resp.status)
		}
		wantError(t, body, "not_found")
	})
	t.Run("store failure", func(t *testing.T) {
		e.server.Health = failingHealth{}
		defer func() { e.server.Health = e.reader }()
		resp, body := get(t, noFollow(), window(h(14, 0), h(19, 0)), c)
		if resp.status != http.StatusInternalServerError {
			t.Errorf("failure: %d %s", resp.status, body)
		}
		wantError(t, body, "internal")
	})
}
