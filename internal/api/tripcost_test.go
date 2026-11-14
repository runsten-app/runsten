package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
)

// tripCostView is the part of a trip's cost the tests look into.
type tripCostView struct {
	MinMinor int64 `json:"min_minor"`
	MaxMinor int64 `json:"max_minor"`
	Per100km *struct {
		MinMinor int64 `json:"min_minor"`
		MaxMinor int64 `json:"max_minor"`
	} `json:"per_100km"`
	Price struct {
		Charges int `json:"charges"`
		LeftOut int `json:"left_out"`
	} `json:"price"`
}

// TestTripCost: a trip's energy at the price of the kWh of the charges of the 30 days
// before it, in the list, the details, the CSV file and the statistics; unknown without
// a priced charge, without a currency, and where the account's limits leave the costs
// out; no cost per 100 km for a plug-in hybrid.
func TestTripCost(t *testing.T) {
	e, c := readerEnv(t)
	base := e.api.URL + "/api/v1/vehicles/" + car
	const morning = "2026-09-28T07:01:00.000000Z"
	trip := func(t *testing.T) *tripCostView {
		t.Helper()
		_, body := get(t, noFollow(), base+"/trips/"+morning, c)
		var tr struct {
			Cost *tripCostView `json:"cost"`
		}
		if err := json.Unmarshal([]byte(body), &tr); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		return tr.Cost
	}
	// The night's charge, at no place, has no cost: nothing prices the morning's trip.
	if got := trip(t); got != nil {
		t.Fatalf("no priced charge: cost %+v", got)
	}

	// 8.50 € entered for the night's 17.6 kWh: 0.48295 € a kWh, 2.90 € the morning's
	// 6 kWh, 9.06 € over its 32 km. The evening's charge, after the trips, prices none.
	if resp, body := do(t, noFollow(), http.MethodPut, base+"/charges/"+night+"/cost", jsonType, costBody, c); resp.status != http.StatusOK {
		t.Fatalf("cost entered: %d %s", resp.status, body)
	}
	resp, body := get(t, noFollow(), base+"/trips/"+morning, c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "trip-cost.json", body)
	if got := trip(t); got == nil || got.MinMinor != 290 || got.MaxMinor != 290 || got.Per100km == nil ||
		got.Per100km.MinMinor != 906 || got.Price.Charges != 1 || got.Price.LeftOut != 0 {
		t.Errorf("trip's cost = %+v", got)
	}

	t.Run("the same in the list, whatever its period", func(t *testing.T) {
		for _, query := range []string{"", "?from=2026-09-28T06:00:00Z&to=2026-09-28T08:00:00Z"} {
			_, body := get(t, noFollow(), base+"/trips"+query, c)
			var p struct {
				Items []struct {
					ID   string        `json:"id"`
					Cost *tripCostView `json:"cost"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatal(err)
			}
			for _, it := range p.Items {
				// The evening's trip has no energy, the reconstructed one neither.
				if priced := it.Cost != nil; priced != (it.ID == morning) || priced && it.Cost.MaxMinor != 290 {
					t.Errorf("%s: trip %s cost %+v", query, it.ID, it.Cost)
				}
			}
		}
	})

	t.Run("in the CSV file", func(t *testing.T) {
		_, body := get(t, noFollow(), base+"/trips.csv", c)
		if !strings.Contains(body, ",EUR,2.90,2.90,9.06,9.06,0.48295,0.48295,1\r\n") {
			t.Errorf("trips.csv:\n%s", body)
		}
	})

	t.Run("in the statistics", func(t *testing.T) {
		var s struct {
			Totals struct {
				Trips struct {
					Cost struct {
						MinMinor int64 `json:"min_minor"`
					} `json:"cost"`
					CostUnknown  int `json:"cost_unknown"`
					CostPer100km *struct {
						MinMinor int64 `json:"min_minor"`
					} `json:"cost_per_100km"`
				} `json:"trips"`
			} `json:"totals"`
		}
		_, body := get(t, noFollow(), base+"/stats?from=2026-09-28T00:00:00Z&to=2026-09-29T00:00:00Z", c)
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Fatal(err)
		}
		tr := s.Totals.Trips
		if tr.Cost.MinMinor != 290 || tr.CostUnknown != 2 || tr.CostPer100km == nil || tr.CostPer100km.MinMinor != 906 {
			t.Errorf("stats' trips = %+v", tr)
		}
	})

	t.Run("a plug-in hybrid has no cost per 100 km", func(t *testing.T) {
		before := e.reader.current[car]
		defer func() { e.reader.current[car] = before }()
		e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 0), Snapshot: details("V60", 2027, 18.819, false)}}
		if got := trip(t); got == nil || got.MaxMinor != 290 || got.Per100km != nil {
			t.Errorf("hybrid's trip cost = %+v", got)
		}
		_, body := get(t, noFollow(), base+"/stats?from=2026-09-28T00:00:00Z&to=2026-09-29T00:00:00Z", c)
		if !strings.Contains(body, `"cost_per_100km":null`) {
			t.Errorf("hybrid's stats: %s", body)
		}
	})

	t.Run("without a currency, unknown", func(t *testing.T) {
		delete(e.settings.currency, account)
		defer func() { e.settings.currency[account] = eur }()
		if got := trip(t); got != nil {
			t.Errorf("cost %+v", got)
		}
	})

	t.Run("where the account's limits leave the costs out, unknown", func(t *testing.T) {
		e.server.Limits = &historyLimit{account: account, noCosts: true}
		defer func() { e.server.Limits = nil }()
		if got := trip(t); got != nil {
			t.Errorf("cost %+v", got)
		}
	})

	t.Run("the history the account does not see prices nothing", func(t *testing.T) {
		e.server.Limits = &historyLimit{account: account, from: h(6, 0)}
		defer func() { e.server.Limits = nil }()
		if got := trip(t); got != nil {
			t.Errorf("cost %+v", got)
		}
	})

	t.Run("the 30 days before the trip, not after", func(t *testing.T) {
		before := e.reader.trips[car]
		defer func() { e.reader.trips[car] = before }()
		late := before[0]
		late.DetectedAt = late.DetectedAt.Add(31 * 24 * time.Hour)
		late.Start.After, late.Start.Before = late.Start.After.Add(31*24*time.Hour), late.Start.Before.Add(31*24*time.Hour)
		late.End.After, late.End.Before = late.End.After.Add(31*24*time.Hour), late.End.Before.Add(31*24*time.Hour)
		e.reader.trips[car] = append([]core.Trip{late}, before...)
		_, body := get(t, noFollow(), base+"/trips/"+late.DetectedAt.Format(idFormat), c)
		if !strings.Contains(body, `"cost":null`) {
			t.Errorf("a month later: %s", body)
		}
	})
}
