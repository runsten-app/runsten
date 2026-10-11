package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"runsten/internal/core"
)

// importFixtures add to the commute day a trip and a charge imported from Aximote the day
// before, before the first reading: the trip ends outside every place, where Aximote
// names the street, and comes with its track; the charge, at home, cost what Aximote
// says.
func importFixtures(m *memReader) {
	imp := func(id, from, to string) core.Value[core.Import] {
		return some(core.Import{Source: "aximote", SourceID: id, ImportedAt: h(4, 30), StartAddress: from, EndAddress: to})
	}
	at := func(hh, mm int) core.Bounds { return core.Bounds{After: h(hh-24, mm), Before: h(hh-24, mm)} }
	m.trips[car] = append(m.trips[car], core.Trip{
		DetectedAt: h(4, 30), Start: at(17, 2), End: at(17, 41), Sources: []string{"aximote"},
		Import:          imp("trip-8812", "Boulevard Vivier-Merle, Lyon", "Rue de la République, Lyon"),
		StartOdometerKm: some(12368.0), EndOdometerKm: some(12400.0), DistanceKm: some(32.0),
		StartSoC: some(81.0), EndSoC: some(74.0), EnergyKWh: some(5.2),
		From: some(work), To: some(core.Position{Lat: 45.75, Lon: 4.85}),
	})
	m.tracks = map[string][][2]float64{"aximote:trip-8812": {{work.Lon, work.Lat}, {4.8421, 45.7563}, {4.85, 45.75}}}
	m.charges[car] = append(m.charges[car], core.Charge{
		DetectedAt: h(4, 30), Start: at(19, 10), End: at(21, 40), Sources: []string{"aximote"},
		Import: imp("charge-311", "Rue Mercière, Lyon", ""),
		Type:   some(core.AC), StartSoC: some(74.0), EndSoC: some(90.0),
		EnergyPowerKWh: some(10.6), EnergySoCKWh: some(12.8), Capacity: fromAPI, AveragePowerKW: some(4.2),
		PaidCost: some(core.Amount{Currency: "EUR", Minor: 233}), Position: some(home),
	})
}

func TestImportedEventsJSON(t *testing.T) {
	e, c := readerEnv(t)
	importFixtures(e.reader)
	base := e.api.URL + "/api/v1/vehicles/" + car

	resp, body := get(t, noFollow(), base+"/trips/aximote:trip-8812", c)
	if resp.status != http.StatusOK {
		t.Fatalf("%d %s", resp.status, body)
	}
	golden(t, "trip-imported.json", body)
	// As the web interface sends it, the colon encoded.
	if resp, encoded := get(t, noFollow(), base+"/trips/aximote%3Atrip-8812", c); resp.status != http.StatusOK || encoded != body {
		t.Errorf("encoded ID: %d %s", resp.status, encoded)
	}
	_, body = get(t, noFollow(), base+"/charges/aximote:charge-311", c)
	golden(t, "charge-imported.json", body)
	ch := readCharge(t, body)
	if !ch.costIs("imported", 233, 233) || ch.Place == nil || ch.Address != nil {
		t.Errorf("imported charge: %s", body)
	}

	t.Run("in another currency, the tariff's cost", func(t *testing.T) {
		i := slices.IndexFunc(e.reader.charges[car], func(c core.Charge) bool { return c.Import.OK })
		e.reader.charges[car][i].PaidCost = some(core.Amount{Currency: "SEK", Minor: 2650})
		defer func() { e.reader.charges[car][i].PaidCost = some(core.Amount{Currency: "EUR", Minor: 233}) }()
		_, body := get(t, noFollow(), base+"/charges/aximote:charge-311", c)
		if ch := readCharge(t, body); ch.Cost == nil || ch.Cost.Source != "tariff" {
			t.Errorf("imported charge: %s", body)
		}
	})

	for _, path := range []string{
		"/trips/aximote:charge-311",  // a charge, not a trip
		"/charges/aximote:trip-8812", // a trip
		"/trips/aximote:nothing", "/trips/Aximote:trip-8812", "/trips/aximote:", "/trips/:trip-8812",
		"/charges/aximote:a%2Fb",
	} {
		if resp, body := get(t, noFollow(), base+path, c); resp.status != http.StatusNotFound {
			t.Errorf("%s: %d %s", path, resp.status, body)
		}
	}

	t.Run("its track, on demand", func(t *testing.T) {
		resp, body := get(t, noFollow(), base+"/trips/aximote%3Atrip-8812/track", c)
		if resp.status != http.StatusOK {
			t.Fatalf("%d %s", resp.status, body)
		}
		golden(t, "trip-track.json", body)
		for _, path := range []string{
			"/trips/2026-09-28T07:01:00.000000Z/track", // read by Runsten: its two ends only
			"/trips/aximote:charge-311/track",          // a charge
			"/trips/aximote:nothing/track", "/trips/aximote:/track",
		} {
			if resp, body := get(t, noFollow(), base+path, c); resp.status != http.StatusNotFound {
				t.Errorf("%s: %d %s", path, resp.status, body)
			}
		}

		// Hidden with its trip, before the history limit.
		e.server.Limits = &historyLimit{account: account, from: h(0, 0)}
		resp, body = get(t, noFollow(), base+"/trips/aximote:trip-8812/track", c)
		e.server.Limits = nil
		if resp.status != http.StatusNotFound {
			t.Errorf("a track before the history limit: %d %s", resp.status, body)
		}

		line := e.reader.tracks["aximote:trip-8812"]
		delete(e.reader.tracks, "aximote:trip-8812")
		defer func() { e.reader.tracks["aximote:trip-8812"] = line }()
		resp, body = get(t, noFollow(), base+"/trips/aximote:trip-8812/track", c)
		if resp.status != http.StatusNotFound {
			t.Errorf("a trip without a track: %d %s", resp.status, body)
		}
	})

	t.Run("listed with the derived events, paged across them", func(t *testing.T) {
		var ids []string
		cursor := ""
		for {
			_, body := get(t, noFollow(), base+"/trips?limit=1"+cursor, c)
			var p struct {
				Items      []EventFields `json:"items"`
				NextCursor *string       `json:"next_cursor"`
			}
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatal(err)
			}
			for _, i := range p.Items {
				ids = append(ids, i.ID)
				if i.Imported != (i.ID == "aximote:trip-8812") || len(i.Sources) != 1 {
					t.Errorf("trip %+v", i)
				}
			}
			if p.NextCursor == nil {
				break
			}
			cursor = "&cursor=" + *p.NextCursor
		}
		if len(ids) != 4 || ids[3] != "aximote:trip-8812" {
			t.Errorf("trips %q", ids)
		}
	})

	t.Run("in the statistics", func(t *testing.T) {
		_, body := get(t, noFollow(), base+"/stats?from=2026-09-27T00:00:00Z&to=2026-09-29T00:00:00Z", c)
		var s struct {
			Totals struct {
				Trips   struct{ Count int } `json:"trips"`
				Charges struct {
					Count int
					Cost  struct {
						MinMinor int64 `json:"min_minor"`
					} `json:"cost"`
				} `json:"charges"`
			} `json:"totals"`
		}
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Fatal(err)
		}
		// The evening charge's 5.24 € at least, and the imported 2.33 €.
		if s.Totals.Trips.Count != 4 || s.Totals.Charges.Count != 3 || s.Totals.Charges.Cost.MinMinor != 524+233 {
			t.Errorf("totals %+v", s.Totals)
		}
	})

	t.Run("no entered cost", func(t *testing.T) {
		resp, body := do(t, noFollow(), http.MethodPut, base+"/charges/aximote:charge-311/cost", jsonType, `{"amount_minor":100,"energy_kwh":null,"note":null}`, c)
		if resp.status != http.StatusConflict {
			t.Fatalf("put: %d %s", resp.status, body)
		}
		wantError(t, body, codeImportedCharge)
		e.reader.entered = []EnteredCost{ionity()}
		resp, body = do(t, noFollow(), http.MethodPut, e.api.URL+"/api/v1/charge-costs/orphans/"+costID(9)+"/charge", jsonType,
			`{"vehicle":"`+car+`","charge":"aximote:charge-311"}`, c)
		if resp.status != http.StatusConflict {
			t.Fatalf("attach: %d %s", resp.status, body)
		}
		wantError(t, body, codeImportedCharge)
		if resp, body := do(t, noFollow(), http.MethodDelete, base+"/charges/aximote:charge-311/cost", "", "", c); resp.status != http.StatusNotFound {
			t.Errorf("delete: %d %s", resp.status, body)
		}
		if e.reader.writes != 0 {
			t.Errorf("%d writes", e.reader.writes)
		}
	})

	t.Run("as CSV", func(t *testing.T) {
		_, body := get(t, noFollow(), base+"/charges.csv", c)
		lines := strings.Split(strings.TrimSpace(body), "\r\n")
		if !strings.HasSuffix(lines[0], ",average_power_kw,imported,sources") || len(lines) != 4 ||
			!strings.HasPrefix(lines[1], "aximote:charge-311,false,") || !strings.HasSuffix(lines[1], ",4.2,true,aximote") ||
			!strings.HasSuffix(lines[2], ",,false,volvo") {
			t.Errorf("charges.csv:\n%s", body)
		}
	})
}

func TestImportsJSON(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	importFixtures(e.reader)
	u := e.api.URL + "/api/v1/vehicles/" + car + "/imports"

	resp, body := get(t, noFollow(), u, c)
	if resp.status != http.StatusOK {
		t.Fatalf("%d %s", resp.status, body)
	}
	golden(t, "imports.json", body)
	if _, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+chosen+"/imports", c); strings.TrimSpace(body) != `{"items":[]}` {
		t.Errorf("without imports: %s", body)
	}
	if resp, _ := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+foreign+"/imports", c); resp.status != http.StatusNotFound {
		t.Errorf("another account's vehicle: %d", resp.status)
	}

	for _, source := range []string{"volvo_csv", "Aximote", "a-b"} {
		if resp, body := do(t, noFollow(), http.MethodDelete, u+"/"+source, "", "", c); resp.status != http.StatusNotFound {
			t.Errorf("%s: %d %s", source, resp.status, body)
		}
	}
	resp, body = do(t, noFollow(), http.MethodDelete, u+"/aximote", "", "", c)
	if resp.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", resp.status, body)
	}
	if !strings.Contains(logs.String(), "import deleted") {
		t.Errorf("not logged:\n%s", logs)
	}
	if _, body := get(t, noFollow(), u, c); strings.TrimSpace(body) != `{"items":[]}` {
		t.Errorf("after the deletion: %s", body)
	}
	if len(e.reader.trips[car]) != 3 || len(e.reader.charges[car]) != 2 {
		t.Errorf("derived events deleted: %d trips, %d charges", len(e.reader.trips[car]), len(e.reader.charges[car]))
	}

	e.reader.err = errEvents
	for _, req := range [][2]string{{http.MethodGet, u}, {http.MethodDelete, u + "/aximote"}} {
		if resp, body := do(t, noFollow(), req[0], req[1], "", "", c); resp.status != http.StatusInternalServerError {
			t.Errorf("%s: %d %s", req[0], resp.status, body)
		}
	}
}
