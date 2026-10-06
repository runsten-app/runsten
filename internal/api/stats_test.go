package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// costStats are the cost fields of the charges, in all or of a type.
type costStats struct {
	Cost struct {
		MinMinor int64 `json:"min_minor"`
		MaxMinor int64 `json:"max_minor"`
	} `json:"cost"`
	CostUnknown int `json:"cost_unknown"`
	CostEntered int `json:"cost_entered"`
}

// placeStats are the fields of a group of charges by place the tests look into.
type placeStats struct {
	Count int `json:"count"`
	costStats
}

type chargeStats struct {
	Count int `json:"count"`
	costStats
	OrphanedCosts struct {
		Count       int   `json:"count"`
		AmountMinor int64 `json:"amount_minor"`
	} `json:"orphaned_costs"`
	ByType map[string]costStats `json:"by_type"`
}

// stats is the part of a statistics response the tests look into.
type stats struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Currency *struct {
		Code string `json:"code"`
	} `json:"currency"`
	Buckets []struct {
		Start   time.Time   `json:"start"`
		Charges chargeStats `json:"charges"`
	} `json:"buckets"`
	TripsByDistance struct {
		Bands []struct {
			Count int `json:"count"`
		} `json:"bands"`
		LeftOut int `json:"left_out"`
	} `json:"trips_by_distance"`
	ChargesBySoC struct {
		Start, End []int
		LeftOut    int `json:"left_out"`
	} `json:"charges_by_soc"`
	ChargesByPlace struct {
		Places []struct {
			Place struct {
				Name string `json:"name"`
			} `json:"place"`
			placeStats
		} `json:"places"`
		Outside    map[string]placeStats `json:"outside"`
		NoPosition placeStats            `json:"no_position"`
	} `json:"charges_by_place"`
	Totals struct {
		Trips struct {
			Count int `json:"count"`
		} `json:"trips"`
		Charges chargeStats `json:"charges"`
		Parked  struct {
			Intervals int `json:"intervals"`
		} `json:"parked"`
	} `json:"totals"`
}

func TestStatsJSON(t *testing.T) {
	e, c := readerEnv(t)
	e.clk.Advance(18 * time.Hour) // 23:00 UTC, after the last event
	base := e.api.URL + "/api/v1/vehicles/"
	query := func(q ...string) string {
		v := url.Values{}
		for i := 0; i < len(q); i += 2 {
			v.Set(q[i], q[i+1])
		}
		return "?" + v.Encode()
	}
	read := func(t *testing.T, target string) stats {
		t.Helper()
		resp, body := get(t, noFollow(), target, c)
		if resp.status != http.StatusOK {
			t.Fatalf("%s: status %d: %s", target, resp.status, body)
		}
		var s stats
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// Without from nor to: from the first event until now, totals only, in UTC. The
	// evening charge costs 5.24 to 5.79 €, the night one is unknown.
	_, body := get(t, noFollow(), base+car+"/stats", c)
	golden(t, "stats-totals.json", body)

	// The night charge's cost is entered, and a cost whose charge was lost lies on the
	// 29th: apart, in its interval.
	if resp, body := do(t, noFollow(), http.MethodPut, base+car+"/charges/"+night+"/cost", jsonType, costBody, c); resp.status != http.StatusOK {
		t.Fatalf("enter cost: %d %s", resp.status, body)
	}
	e.reader.entered = append(e.reader.entered, ionity())
	// The day of the fixtures and the next two, by day in Paris: the last two are empty.
	byDay := base + car + "/stats" + query(
		"from", "2026-09-28T00:00:00+02:00", "to", "2026-10-01T00:00:00+02:00", "tz", "Europe/Paris", "bucket", "day")
	resp, body := get(t, noFollow(), byDay, c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "stats.json", body)
	// No event: every interval is there, at zero.
	_, body = get(t, noFollow(), base+parked+"/stats"+query(
		"from", "2026-09-01T00:00:00Z", "to", "2026-09-15T00:00:00Z", "bucket", "week"), c)
	golden(t, "stats-empty.json", body)

	t.Run("costs: sums, unknown, entered, and the orphans apart", func(t *testing.T) {
		s := read(t, byDay)
		ch := s.Totals.Charges
		if s.Currency == nil || s.Currency.Code != "EUR" || ch.Cost.MinMinor != 524+850 || ch.Cost.MaxMinor != 579+850 ||
			ch.CostUnknown != 0 || ch.CostEntered != 1 || ch.OrphanedCosts.Count != 1 || ch.OrphanedCosts.AmountMinor != 1240 {
			t.Errorf("totals = %+v", ch)
		}
		if ac, unknown := ch.ByType["ac"], ch.ByType["unknown"]; ac.Cost.MinMinor != 524 || ac.Cost.MaxMinor != 579 ||
			ac.CostEntered != 0 || unknown.Cost.MinMinor != 850 || unknown.CostEntered != 1 {
			t.Errorf("by type = %+v", ch.ByType)
		}
		// The totals are the sums of the intervals; the orphan lies on the 29th.
		var sum chargeStats
		for _, b := range s.Buckets {
			sum.Cost.MinMinor += b.Charges.Cost.MinMinor
			sum.Cost.MaxMinor += b.Charges.Cost.MaxMinor
			sum.CostUnknown += b.Charges.CostUnknown
			sum.CostEntered += b.Charges.CostEntered
			sum.OrphanedCosts.Count += b.Charges.OrphanedCosts.Count
			sum.OrphanedCosts.AmountMinor += b.Charges.OrphanedCosts.AmountMinor
		}
		sum.Count, sum.ByType = ch.Count, ch.ByType
		if !reflect.DeepEqual(sum, ch) || s.Buckets[1].Charges.OrphanedCosts.Count != 1 {
			t.Errorf("sum of the intervals %+v, totals %+v", sum, ch)
		}
		// Outside the period, the orphan is not counted.
		if s := read(t, base+car+"/stats"+query("to", "2026-09-29T00:00:00Z")); s.Totals.Charges.OrphanedCosts.Count != 0 {
			t.Errorf("orphans before the 29th = %+v", s.Totals.Charges.OrphanedCosts)
		}
	})
	t.Run("without a currency, every cost is unknown", func(t *testing.T) {
		delete(e.settings.currency, account)
		defer func() { e.settings.currency[account] = eur }()
		s := read(t, byDay)
		ch := s.Totals.Charges
		if s.Currency != nil || ch.Cost.MinMinor != 0 || ch.Cost.MaxMinor != 0 || ch.CostUnknown != 2 || ch.CostEntered != 0 {
			t.Errorf("stats without currency = %+v %+v", s.Currency, ch)
		}
	})

	t.Run("no event and no from: an empty period until now", func(t *testing.T) {
		s := read(t, base+parked+"/stats"+query("bucket", "day"))
		if !s.From.Equal(e.clk.Now()) || !s.To.Equal(e.clk.Now()) || len(s.Buckets) != 0 ||
			len(s.TripsByDistance.Bands) != 5 || len(s.ChargesBySoC.Start) != 101 {
			t.Errorf("stats = %+v", s)
		}
	})
	t.Run("the distributions count each event of the period once", func(t *testing.T) {
		// From 05:00, as below: three trips and one charge.
		s := read(t, base+car+"/stats"+query("from", "2026-09-28T05:00:00Z"))
		trips, starts, ends := s.TripsByDistance.LeftOut, s.ChargesBySoC.LeftOut, s.ChargesBySoC.LeftOut
		for _, b := range s.TripsByDistance.Bands {
			trips += b.Count
		}
		for i := range s.ChargesBySoC.Start {
			starts += s.ChargesBySoC.Start[i]
			ends += s.ChargesBySoC.End[i]
		}
		if trips != 3 || starts != 1 || ends != 1 {
			t.Errorf("trips %d, charges %d and %d: %+v %+v", trips, starts, ends, s.TripsByDistance, s.ChargesBySoC)
		}
	})
	t.Run("each charge of the period is in one group of places", func(t *testing.T) {
		// The whole day: the reconstructed charge of the night, without a position, and
		// the evening charge, at the place.
		s := read(t, base+car+"/stats")
		p := s.ChargesByPlace
		n := p.NoPosition.Count
		for _, g := range p.Outside {
			n += g.Count
		}
		if len(p.Places) != 1 || p.Places[0].Place.Name != "Maison de Tante Agathe" || p.Places[0].Count != 1 ||
			p.Places[0].Cost.MinMinor != 524 || p.NoPosition.Count != 1 || n+p.Places[0].Count != s.Totals.Charges.Count {
			t.Errorf("by place = %+v, totals %+v", p, s.Totals.Charges)
		}
	})
	t.Run("the event before from opens the first parked interval", func(t *testing.T) {
		// From 05:00: the reconstructed charge of the night is before, the three trips
		// and the evening charge in the period.
		s := read(t, base+car+"/stats"+query("from", "2026-09-28T05:00:00Z"))
		if s.Totals.Trips.Count != 3 || s.Totals.Charges.Count != 1 || s.Totals.Parked.Intervals != 4 {
			t.Errorf("totals = %+v", s.Totals)
		}
	})
	t.Run("to defaults to now", func(t *testing.T) {
		e, c := readerEnv(t)
		e.clk.Advance(7 * time.Hour) // 12:00: the night charge and the morning trip
		_, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/stats", c)
		var s stats
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Fatal(err)
		}
		if !s.To.Equal(h(12, 0)) || !s.From.Equal(h(1, 0)) || s.Totals.Trips.Count != 1 || s.Totals.Charges.Count != 1 {
			t.Errorf("stats = %+v", s)
		}
	})
	t.Run("invalid parameters", func(t *testing.T) {
		for q, param := range map[string]string{
			query("tz", "Mars/Olympus_Mons"): "tz",
			query("tz", "../../etc/passwd"):  "tz",
			query("tz", "Local"):             "tz",
			query("bucket", "year"):          "bucket",
			query("from", "yesterday"):       "from",
			query("from", "2026-09-28T12:00:00Z", "to", "2026-09-28T12:00:00Z"):                   "from must be before to",
			query("from", "2026-09-29T00:00:00Z"):                                                 "from must be before to", // after now
			query("from", "2025-01-01T00:00:00Z", "bucket", "day"):                                "bucket",
			query("from", "2019-01-01T00:00:00Z", "to", "2026-09-01T00:00:00Z", "bucket", "week"): "400 intervals",
		} {
			resp, body := get(t, noFollow(), base+car+"/stats"+q, c)
			if resp.status != http.StatusBadRequest || !strings.Contains(body, param) {
				t.Errorf("%s: %d %s", q, resp.status, body)
			}
			wantError(t, body, "invalid_parameter")
		}
	})
}
