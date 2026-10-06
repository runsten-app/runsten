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

// Readings reads the records of the energy state, as the store's endpoint does: those
// reporting the state of charge.
func (m *memReader) Readings(_ context.Context, _, vehicle string, from, to time.Time, limit int) ([]core.Record, error) {
	var out []core.Record
	for _, r := range m.current[vehicle] {
		if r.Snapshot.Covers&core.FieldSoC != 0 && !r.CheckedAt.Before(from) && !r.FetchedAt.After(to) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b core.Record) int { return a.FetchedAt.Compare(b.FetchedAt) })
	return out[:min(limit, len(out))], m.err
}

type failingReadings struct{}

func (failingReadings) Readings(context.Context, string, string, time.Time, time.Time, int) ([]core.Record, error) {
	return nil, errEvents
}

// energyAt is a response of the energy state fetched at f and read again at c; power
// and range are absent when negative.
func energyAt(f, c time.Time, soc, powerW, rangeKm float64) core.Record {
	s := core.Snapshot{
		Covers: core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldPower,
		SoC:    core.Some(soc, f), Charging: core.Some(core.ChargingIdle, f),
	}
	if powerW >= 0 {
		s.PowerW = core.Some(powerW, f)
		if powerW > 0 {
			s.Charging = core.Some(core.ChargingActive, f)
		}
	}
	if rangeKm >= 0 {
		s.RangeKm = core.Some(rangeKm, f)
	}
	return core.Record{FetchedAt: f, CheckedAt: c, Snapshot: s}
}

// seriesFixtures adds, before the fixtures' latest energy state (19:03), a reading in
// the afternoon, then nothing for more than 90 minutes, then the car parked before its
// evening charge, read again until 18:20, and the charge's first readings, one without
// a range.
func seriesFixtures(m *memReader) {
	m.current[car] = append(m.current[car],
		energyAt(h(18, 40), h(18, 40), 50, 7350, -1),
		energyAt(h(15, 0), h(15, 0), 50, 0, 200),
		energyAt(h(17, 26), h(18, 20), 47, 0, 188),
		energyAt(h(18, 30), h(18, 30), 48, 7400, 192),
	)
}

type series struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Runs []struct {
		Readings []struct {
			At     time.Time `json:"at"`
			SoCPct *float64  `json:"soc_pct"`
		} `json:"readings"`
	} `json:"runs"`
}

func TestSeriesJSON(t *testing.T) {
	e, c := readerEnv(t)
	seriesFixtures(e.reader)
	e.clk.Advance(18 * time.Hour) // 23:00 UTC
	base := e.api.URL + "/api/v1/vehicles/" + car + "/series"
	window := func(from, to time.Time) string {
		v := url.Values{"from": {from.Format(time.RFC3339)}}
		if !to.IsZero() {
			v.Set("to", to.Format(time.RFC3339))
		}
		return base + "?" + v.Encode()
	}
	read := func(t *testing.T, target string) series {
		t.Helper()
		resp, body := get(t, noFollow(), target, c)
		if resp.status != http.StatusOK {
			t.Fatalf("%s: status %d: %s", target, resp.status, body)
		}
		var s series
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// From 14:00 to 19:00: the afternoon's reading alone, then the evening's run, which
	// stops before the 19:03 reading.
	_, body := get(t, noFollow(), window(h(14, 0), h(19, 0)), c)
	golden(t, "series.json", body)

	t.Run("until now", func(t *testing.T) {
		s := read(t, window(h(18, 25), time.Time{}))
		last := s.Runs[len(s.Runs)-1].Readings
		if !s.To.Equal(e.clk.Now()) || len(s.Runs) != 1 || !last[len(last)-1].At.Equal(h(19, 3)) {
			t.Errorf("until now: %+v", s)
		}
	})
	t.Run("the history the account sees", func(t *testing.T) {
		e.server.Limits = &historyLimit{account: account, from: h(18, 0)}
		defer func() { e.server.Limits = nil }()
		// The response read from 17:26 to 18:20 gives its value at the limit.
		s := read(t, window(h(14, 0), h(19, 0)))
		if first := s.Runs[0].Readings[0]; !s.From.Equal(h(18, 0)) || len(s.Runs) != 1 || !first.At.Equal(h(18, 0)) ||
			first.SoCPct == nil || *first.SoCPct != 47 {
			t.Errorf("from the limit: %+v", s)
		}
		// A window wholly before it is empty, never an error.
		if s := read(t, window(h(14, 0), h(16, 0))); len(s.Runs) != 0 || !s.From.Equal(h(16, 0)) {
			t.Errorf("before the limit: %+v", s)
		}
	})
	t.Run("nothing read", func(t *testing.T) {
		if s := read(t, window(h(19, 10), h(20, 0))); s.Runs == nil || len(s.Runs) != 0 {
			t.Errorf("no reading: %+v", s)
		}
	})
	t.Run("refused", func(t *testing.T) {
		for _, target := range []string{
			base,                       // no from
			window(h(19, 0), h(19, 0)), // empty
			window(h(19, 0), h(14, 0)), // backwards
			window(h(0, 0), h(0, 0).AddDate(0, 0, 8)), // over 7 days
			base + "?from=yesterday",
		} {
			resp, body := get(t, noFollow(), target, c)
			if resp.status != http.StatusBadRequest {
				t.Errorf("%s: %d %s", target, resp.status, body)
			}
			wantError(t, body, "invalid_parameter")
		}
		// Seven days exactly are not too many.
		if resp, body := get(t, noFollow(), window(h(0, 0), h(0, 0).AddDate(0, 0, 7)), c); resp.status != http.StatusOK {
			t.Errorf("7 days: %d %s", resp.status, body)
		}
	})
	t.Run("too many responses", func(t *testing.T) {
		many := make([]core.Record, maxSeriesResponses+1)
		for i := range many {
			at := h(0, 0).Add(time.Duration(i) * 4 * time.Second)
			many[i] = energyAt(at, at, 50, 0, 200)
		}
		e.reader.current[chosen] = many
		u := e.api.URL + "/api/v1/vehicles/" + chosen + "/series?from=2026-09-28T00:00:00Z&to=2026-09-29T00:00:00Z"
		resp, body := get(t, noFollow(), u, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%d responses: %d", len(many), resp.status)
		}
		wantError(t, body, "invalid_parameter")
		e.reader.current[chosen] = many[:maxSeriesResponses]
		if resp, _ := get(t, noFollow(), u, c); resp.status != http.StatusOK {
			t.Errorf("%d responses: %d", maxSeriesResponses, resp.status)
		}
	})
	t.Run("another account's vehicle", func(t *testing.T) {
		u := e.api.URL + "/api/v1/vehicles/" + foreign + "/series?from=2026-09-28T00:00:00Z"
		resp, body := get(t, noFollow(), u, c)
		if resp.status != http.StatusNotFound {
			t.Errorf("foreign: %d", resp.status)
		}
		wantError(t, body, "not_found")
	})
	t.Run("store failure", func(t *testing.T) {
		e.server.Readings = failingReadings{}
		defer func() { e.server.Readings = e.reader }()
		resp, body := get(t, noFollow(), window(h(14, 0), h(19, 0)), c)
		if resp.status != http.StatusInternalServerError {
			t.Errorf("failure: %d %s", resp.status, body)
		}
		wantError(t, body, "internal")
	})
}
