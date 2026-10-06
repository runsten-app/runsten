package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
)

// The IDs of the fixtures' charges: in the evening at home, and at night, reconstructed.
const (
	evening = "2026-09-28T18:30:00.000000Z"
	night   = "2026-09-28T04:00:00.000000Z"
)

// costNote must never reach a log or an error.
const costNote = "Borne du parking, carte Chargemap"

const costBody = `{"amount_minor":850,"energy_kwh":20.1,"note":"` + costNote + `"}`

// ionity is an entered cost whose charge a rebuild lost: no charge on the 29th.
func ionity() EnteredCost {
	day := func(hh, mm int) time.Time { return h(hh, mm).AddDate(0, 0, 1) }
	return EnteredCost{
		ID: costID(9), VehicleID: car, EnteredAt: day(14, 2),
		EnteredCost: core.EnteredCost{
			ChargeDetectedAt: day(12, 5), WindowAfter: day(12, 0), WindowBefore: day(12, 40),
			AmountMinor: 1240, EnergyKWh: some(31.5), Note: "Ionity, carte Chargemap",
		},
	}
}

// chargeView is the part of a charge the tests look into.
type chargeView struct {
	ID    string `json:"id"`
	Place *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"place"`
	Cost *struct {
		Currency   string   `json:"currency"`
		MinMinor   int64    `json:"min_minor"`
		MaxMinor   int64    `json:"max_minor"`
		Source     string   `json:"source"`
		EnergyKWh  *float64 `json:"energy_kwh"`
		Efficiency *float64 `json:"efficiency"`
		Note       *string  `json:"note"`
	} `json:"cost"`
}

func readCharge(t *testing.T, body string) chargeView {
	t.Helper()
	var c chargeView
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	return c
}

// costIs tells whether the charge's cost is [lo, hi] from source.
func (c chargeView) costIs(source string, lo, hi int64) bool {
	return c.Cost != nil && c.Cost.Source == source && c.Cost.MinMinor == lo && c.Cost.MaxMinor == hi && c.Cost.Currency == "EUR"
}

func TestChargeCostsInTheLists(t *testing.T) {
	e, c := readerEnv(t)
	base := e.api.URL + "/api/v1/vehicles/" + car + "/charges"
	_, body := get(t, noFollow(), base+"/"+evening, c)
	ev := readCharge(t, body)
	// 30.0 kWh from the grid over 100 minutes of peak hours and 116 of off-peak ones.
	if !ev.costIs("tariff", 524, 579) || ev.Place == nil || ev.Place.Name != homeName || *ev.Cost.EnergyKWh != 26.4/0.88 ||
		*ev.Cost.Efficiency != 0.88 || ev.Cost.Note != nil {
		t.Errorf("evening charge: %s", body)
	}
	_, body = get(t, noFollow(), base+"/"+night, c)
	if n := readCharge(t, body); n.Cost != nil || n.Place != nil {
		t.Errorf("night charge, at no place: %s", body)
	}

	t.Run("without a currency, no cost, but the place", func(t *testing.T) {
		delete(e.settings.currency, account)
		defer func() { e.settings.currency[account] = eur }()
		_, body := get(t, noFollow(), base+"/"+evening, c)
		if ev := readCharge(t, body); ev.Cost != nil || ev.Place == nil {
			t.Errorf("evening charge: %s", body)
		}
	})

	// page reads the charges one page of one at a time, newest first.
	pages := func(t *testing.T) []chargeView {
		t.Helper()
		var out []chargeView
		cursor := ""
		for {
			_, body := get(t, noFollow(), base+"?limit=1"+cursor, c)
			var p struct {
				Items      []chargeView `json:"items"`
				NextCursor *string      `json:"next_cursor"`
			}
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatal(err)
			}
			out = append(out, p.Items...)
			if p.NextCursor == nil {
				return out
			}
			cursor = "&cursor=" + *p.NextCursor
		}
	}
	orphans := func(t *testing.T) string {
		t.Helper()
		_, body := get(t, noFollow(), e.api.URL+"/api/v1/charge-costs/orphans", c)
		return body
	}
	t.Run("across pages: a cost whose charge is on another page is not taken by this one's", func(t *testing.T) {
		// Entered for the evening charge before a rebuild widened its window over the
		// night: attached to it by its detection time, even from the page of the night
		// charge, which overlaps it.
		e.reader.entered = []EnteredCost{{ID: costID(1), VehicleID: car, EnteredCost: core.EnteredCost{
			ChargeDetectedAt: h(18, 30), WindowAfter: h(3, 0), WindowBefore: h(19, 0), AmountMinor: 111,
		}}}
		got := pages(t)
		if len(got) != 2 || !got[0].costIs("entered", 111, 111) || got[1].Cost != nil {
			t.Errorf("pages = %+v", got)
		}
		if body := orphans(t); body != `{"items":[]}`+"\n" {
			t.Errorf("orphans = %s", body)
		}
	})
	t.Run("across pages: a cost overlapping charges of two pages is taken by neither", func(t *testing.T) {
		e.reader.entered = []EnteredCost{{ID: costID(2), VehicleID: car, EnteredCost: core.EnteredCost{
			ChargeDetectedAt: h(12, 0), WindowAfter: h(3, 0), WindowBefore: h(19, 0), AmountMinor: 222,
		}}}
		got := pages(t)
		if len(got) != 2 || !got[0].costIs("tariff", 524, 579) || got[1].Cost != nil {
			t.Errorf("pages = %+v", got)
		}
		if body := orphans(t); !strings.Contains(body, costID(2)) {
			t.Errorf("orphans = %s", body)
		}
	})
}

func TestChargeCostJSON(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	base := e.api.URL + "/api/v1/vehicles/" + car + "/charges/"
	put := func(id, body string) (reply, string) {
		t.Helper()
		return do(t, noFollow(), http.MethodPut, base+id+"/cost", jsonType, body, c)
	}
	del := func(id string) (reply, string) {
		t.Helper()
		return do(t, noFollow(), http.MethodDelete, base+id+"/cost", "", "", c)
	}
	charge := func(id string) chargeView {
		t.Helper()
		_, body := get(t, noFollow(), base+id, c)
		return readCharge(t, body)
	}

	resp, body := put(night, costBody)
	if resp.status != http.StatusOK {
		t.Fatalf("put: %d %s", resp.status, body)
	}
	golden(t, "charge-cost.json", body)
	if _, got := get(t, noFollow(), base+night, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}

	t.Run("entered again: replaced, 0 is free", func(t *testing.T) {
		resp, body := put(night, `{"amount_minor":0,"energy_kwh":null,"note":""}`)
		n := readCharge(t, body)
		if resp.status != http.StatusOK || !n.costIs("entered", 0, 0) || n.Cost.EnergyKWh != nil || n.Cost.Note != nil ||
			n.Cost.Efficiency != nil || len(e.reader.entered) != 1 {
			t.Errorf("put: %d %s", resp.status, body)
		}
	})
	t.Run("on a charge with a tariff: replaced, and back once deleted", func(t *testing.T) {
		resp, body := put(evening, `{"amount_minor":600,"energy_kwh":30,"note":null}`)
		if ev := readCharge(t, body); resp.status != http.StatusOK || !ev.costIs("entered", 600, 600) || ev.Place == nil {
			t.Errorf("put: %d %s", resp.status, body)
		}
		if resp, body := del(evening); resp.status != http.StatusNoContent || body != "" {
			t.Errorf("delete: %d %q", resp.status, body)
		}
		if ev := charge(evening); !ev.costIs("tariff", 524, 579) {
			t.Errorf("after delete: %+v", ev.Cost)
		}
		resp, body = del(evening)
		if resp.status != http.StatusNotFound {
			t.Errorf("delete again: %d", resp.status)
		}
		wantError(t, body, codeNotFound)
	})
	t.Run("without a currency: accepted, unknown until it is set", func(t *testing.T) {
		delete(e.settings.currency, account)
		resp, body := put(night, costBody)
		if n := readCharge(t, body); resp.status != http.StatusOK || n.Cost != nil {
			t.Errorf("put: %d %s", resp.status, body)
		}
		e.settings.currency[account] = eur
		if n := charge(night); !n.costIs("entered", 850, 850) || *n.Cost.Note != costNote || *n.Cost.EnergyKWh != 20.1 {
			t.Errorf("once the currency is set: %+v", n.Cost)
		}
	})
	t.Run("invalid bodies", func(t *testing.T) {
		e.reader.writes = 0
		for _, tt := range []struct{ name, body, field string }{
			{"negative", `{"amount_minor":-1,"energy_kwh":null,"note":null}`, "body.amount_minor"},
			{"above 10⁹", `{"amount_minor":1000000001,"energy_kwh":null,"note":null}`, "body.amount_minor"},
			{"not an integer", `{"amount_minor":8.5,"energy_kwh":null,"note":null}`, "body.amount_minor"},
			{"a string", `{"amount_minor":"850","energy_kwh":null,"note":null}`, "body.amount_minor"},
			{"null amount", `{"amount_minor":null,"energy_kwh":null,"note":null}`, "body.amount_minor"},
			{"zero energy", `{"amount_minor":1,"energy_kwh":0,"note":null}`, "body.energy_kwh"},
			{"energy above 500", `{"amount_minor":1,"energy_kwh":500.1,"note":null}`, "body.energy_kwh"},
			{"note of 501 characters", `{"amount_minor":1,"energy_kwh":null,"note":"` + costNote + strings.Repeat("é", 501-len([]rune(costNote))) + `"}`, "body.note"},
			{"a field missing", `{"amount_minor":1,"energy_kwh":null}`, "body"},
			{"unknown field", `{"amount_minor":1,"energy_kwh":null,"note":null,"currency":"EUR"}`, "body"},
			{"not JSON", "{", "body"},
		} {
			resp, body := put(night, tt.body)
			if resp.status != http.StatusBadRequest || !strings.Contains(body, tt.field) || strings.Contains(body, "Chargemap") {
				t.Errorf("%s: %d %s", tt.name, resp.status, body)
			}
			wantError(t, body, codeInvalidBody)
		}
		for _, contentType := range []string{"", "text/plain"} {
			resp, body := do(t, noFollow(), http.MethodPut, base+night+"/cost", contentType, costBody, c)
			if resp.status != http.StatusUnsupportedMediaType {
				t.Errorf("Content-Type %q: %d", contentType, resp.status)
			}
			wantError(t, body, codeUnsupportedMediaType)
		}
		if e.reader.writes != 0 {
			t.Errorf("%d invalid bodies reached the store", e.reader.writes)
		}
		// The limits themselves are accepted.
		resp, body := put(night, `{"amount_minor":1000000000,"energy_kwh":500,"note":"`+strings.Repeat("é", 500)+`"}`)
		if n := readCharge(t, body); resp.status != http.StatusOK || !n.costIs("entered", 1e9, 1e9) {
			t.Errorf("limits: %d %s", resp.status, body)
		}
	})
	t.Run("not found", func(t *testing.T) {
		for _, u := range []string{
			e.api.URL + "/api/v1/vehicles/" + foreign + "/charges/" + evening,
			e.api.URL + "/api/v1/vehicles/not-a-uuid/charges/" + evening,
			base + "2026-09-28T18:31:00Z",
			base + "yesterday",
		} {
			resp, body := do(t, noFollow(), http.MethodPut, u+"/cost", jsonType, costBody, c)
			if resp.status != http.StatusNotFound {
				t.Errorf("PUT %s: %d", u, resp.status)
			}
			wantError(t, body, codeNotFound)
			resp, body = do(t, noFollow(), http.MethodDelete, u+"/cost", "", "", c)
			if resp.status != http.StatusNotFound {
				t.Errorf("DELETE %s: %d", u, resp.status)
			}
			wantError(t, body, codeNotFound)
		}
		// Another account's session, on this vehicle.
		other := e.session(t, "other")
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			if resp, _ := do(t, noFollow(), method, base+night+"/cost", jsonType, costBody, other); resp.status != http.StatusNotFound {
				t.Errorf("%s by another account: %d", method, resp.status)
			}
		}
		if n := charge(night); !n.costIs("entered", 1e9, 1e9) {
			t.Errorf("the owner's cost changed: %+v", n.Cost)
		}
	})
	if resp, _ := del(night); resp.status != http.StatusNoContent {
		t.Errorf("delete: %d", resp.status)
	}
	if n := charge(night); n.Cost != nil {
		t.Errorf("after delete: %+v", n.Cost)
	}
	if strings.Contains(logs.String(), "Chargemap") || strings.Contains(logs.String(), homeName) || strings.Contains(logs.String(), "45.76") {
		t.Errorf("a note or a place reached the log:\n%s", logs)
	}
}

func TestOrphansJSON(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	e.reader.entered = []EnteredCost{ionity(), {ID: costID(8), VehicleID: foreign, EnteredCost: core.EnteredCost{AmountMinor: 5}}}
	u := e.api.URL + "/api/v1/charge-costs/orphans"
	attach := func(id, body string) (reply, string) {
		t.Helper()
		return do(t, noFollow(), http.MethodPut, u+"/"+id+"/charge", jsonType, body, c)
	}
	to := func(vehicle, charge string) string { return `{"vehicle":"` + vehicle + `","charge":"` + charge + `"}` }

	resp, body := get(t, noFollow(), u, c)
	if resp.status != http.StatusOK {
		t.Fatalf("list: %d %s", resp.status, body)
	}
	golden(t, "orphans.json", body)
	// Another account's own, without its currency.
	if _, body := get(t, noFollow(), u, e.session(t, "other")); !strings.Contains(body, costID(8)) ||
		strings.Contains(body, costID(9)) || !strings.Contains(body, `"currency":null`) {
		t.Errorf("other account: %s", body)
	}

	t.Run("refused", func(t *testing.T) {
		for _, tt := range []struct {
			name, id, body string
			status         int
			code           errorCode
		}{
			{"another account's cost", costID(8), to(car, evening), http.StatusNotFound, codeNotFound},
			{"unknown cost", costID(7), to(car, evening), http.StatusNotFound, codeNotFound},
			{"malformed cost ID", "not-a-uuid", to(car, evening), http.StatusNotFound, codeNotFound},
			{"another account's vehicle", costID(9), to(foreign, evening), http.StatusNotFound, codeNotFound},
			{"malformed vehicle", costID(9), to("car", evening), http.StatusNotFound, codeNotFound},
			{"unknown charge", costID(9), to(car, "2026-09-28T18:31:00Z"), http.StatusNotFound, codeNotFound},
			{"malformed charge", costID(9), to(car, "yesterday"), http.StatusNotFound, codeNotFound},
			{"a field missing", costID(9), `{"vehicle":"` + car + `"}`, http.StatusBadRequest, codeInvalidBody},
			{"unknown field", costID(9), `{"vehicle":"` + car + `","charge":"` + evening + `","amount_minor":1}`, http.StatusBadRequest, codeInvalidBody},
			{"a number", costID(9), `{"vehicle":1,"charge":"` + evening + `"}`, http.StatusBadRequest, codeInvalidBody},
		} {
			resp, body := attach(tt.id, tt.body)
			if resp.status != tt.status {
				t.Errorf("%s: %d %s", tt.name, resp.status, body)
			}
			wantError(t, body, tt.code)
		}
		resp, body := do(t, noFollow(), http.MethodPut, u+"/"+costID(9)+"/charge", "", to(car, evening), c)
		if resp.status != http.StatusUnsupportedMediaType {
			t.Errorf("no content type: %d", resp.status)
		}
		wantError(t, body, codeUnsupportedMediaType)
	})
	t.Run("attach", func(t *testing.T) {
		resp, body := attach(costID(9), to(car, evening))
		ev := readCharge(t, body)
		if resp.status != http.StatusOK || ev.ID != evening || !ev.costIs("entered", 1240, 1240) || *ev.Cost.EnergyKWh != 31.5 {
			t.Errorf("attach: %d %s", resp.status, body)
		}
		if _, body := get(t, noFollow(), u, c); body != `{"items":[]}`+"\n" {
			t.Errorf("orphans after attaching: %s", body)
		}
		// Again, to the same charge: nothing changes.
		if resp, body := attach(costID(9), to(car, "2026-09-28T18:30:00Z")); resp.status != http.StatusOK {
			t.Errorf("attach again: %d %s", resp.status, body)
		}
		// Another cost, to the charge that takes this one.
		e.reader.entered = append(e.reader.entered, EnteredCost{ID: costID(3), VehicleID: car, EnteredCost: core.EnteredCost{
			ChargeDetectedAt: h(23, 0), WindowAfter: h(23, 0), WindowBefore: h(23, 30), AmountMinor: 3,
		}})
		resp, body = attach(costID(3), to(car, evening))
		if resp.status != http.StatusConflict {
			t.Errorf("attach to a charge with a cost: %d %s", resp.status, body)
		}
		wantError(t, body, codeChargeHasCost)
	})
	t.Run("delete", func(t *testing.T) {
		for _, tt := range []struct {
			id     string
			status int
		}{
			{costID(3), http.StatusNoContent},
			{costID(3), http.StatusNotFound},
			{costID(8), http.StatusNotFound}, // another account's
			{"not-a-uuid", http.StatusNotFound},
			{costID(9), http.StatusNoContent}, // one a charge takes
		} {
			resp, body := do(t, noFollow(), http.MethodDelete, u+"/"+tt.id, "", "", c)
			if resp.status != tt.status {
				t.Errorf("delete %s: %d %s", tt.id, resp.status, body)
			}
		}
		_, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/charges/"+evening, c)
		if ev := readCharge(t, body); !ev.costIs("tariff", 524, 579) || len(e.reader.entered) != 1 {
			t.Errorf("after deleting its cost: %s", body)
		}
	})
	if strings.Contains(logs.String(), "Chargemap") {
		t.Errorf("a note reached the log:\n%s", logs)
	}
}

func TestCostErrors(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	e.reader.entered = []EnteredCost{ionity()}
	e.reader.costErr = errors.New("boom")
	for _, tt := range []struct{ method, path, body string }{
		{http.MethodPut, "/vehicles/" + car + "/charges/" + night + "/cost", costBody},
		{http.MethodDelete, "/vehicles/" + car + "/charges/" + night + "/cost", ""},
		{http.MethodGet, "/charge-costs/orphans", ""},
		{http.MethodPut, "/charge-costs/orphans/" + costID(9) + "/charge", `{"vehicle":"` + car + `","charge":"` + evening + `"}`},
		{http.MethodDelete, "/charge-costs/orphans/" + costID(9), ""},
	} {
		contentType := ""
		if tt.body != "" {
			contentType = jsonType
		}
		resp, body := do(t, noFollow(), tt.method, e.api.URL+apiPrefix+tt.path, contentType, tt.body, c)
		if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
			t.Errorf("%s %s: %d %s", tt.method, tt.path, resp.status, body)
		}
		wantError(t, body, codeInternal)
	}
	e.reader.costErr = nil
	e.settings.err = errors.New("boom")
	if resp, _ := get(t, noFollow(), e.api.URL+"/api/v1/charge-costs/orphans", c); resp.status != http.StatusInternalServerError {
		t.Errorf("orphans, currency not read: %d", resp.status)
	}
	e.settings.err = nil
	// Entered, then the charge cannot be read back.
	e.server.Reader = &failingEvents{memReader: e.reader}
	if resp, _ := do(t, noFollow(), http.MethodPut, e.api.URL+apiPrefix+"/vehicles/"+car+"/charges/"+night+"/cost", jsonType, costBody, c); resp.status != http.StatusInternalServerError {
		t.Errorf("charge not read back: %d", resp.status)
	}
	if !strings.Contains(logs.String(), "boom") || strings.Contains(logs.String(), "Chargemap") {
		t.Errorf("log:\n%s", logs)
	}
}

func TestUnpricedCharges(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/places/" + placeID(1) + "/unpriced"
	unpriced := func(t *testing.T, want string) {
		t.Helper()
		resp, body := get(t, noFollow(), u, c)
		if resp.status != http.StatusOK || body != want+"\n" {
			t.Errorf("unpriced: %d %s, want %s", resp.status, body, want)
		}
	}
	// The evening charge is priced; the night one is at no place.
	unpriced(t, `{"charges":0,"first_day":null}`)

	// A tariff from October leaves the evening charge without a price, and one of the
	// night before, whose day in Paris is not UTC's.
	e.settings.places[account][0].Tariff[0].ValidFrom = core.Date{Year: 2026, Month: time.October, Day: 1}
	late := e.reader.charges[car][0] // 01:30 on the 27th in Paris, the 26th in UTC
	late.DetectedAt, late.Start = h(-25, 40), core.Bounds{After: h(-25, 30), Before: h(-25, 40)}
	late.End = core.Bounds{After: h(-23, 0), Before: h(-22, 55)}
	e.reader.charges[car] = append(e.reader.charges[car], late)
	_, body := get(t, noFollow(), u, c)
	golden(t, "place-unpriced.json", body)
	unpriced(t, `{"charges":2,"first_day":"2026-09-27"}`)

	t.Run("an entered cost gives the price", func(t *testing.T) {
		e.reader.entered = []EnteredCost{{ID: costID(1), VehicleID: car, EnteredCost: core.EnteredCost{
			ChargeDetectedAt: late.DetectedAt, WindowAfter: late.Start.After, WindowBefore: late.End.Before, AmountMinor: 500,
		}}}
		defer func() { e.reader.entered = nil }()
		unpriced(t, `{"charges":1,"first_day":"2026-09-28"}`)
	})
	t.Run("without a currency, no cost is to be had", func(t *testing.T) {
		delete(e.settings.currency, account)
		defer func() { e.settings.currency[account] = eur }()
		unpriced(t, `{"charges":0,"first_day":null}`)
	})
	t.Run("another place's charges are not counted", func(t *testing.T) {
		work := homePlace(t)
		work.ID, work.Position = placeID(2), core.Position{Lat: 45.75, Lon: 4.85}
		e.settings.places[account] = append(e.settings.places[account], work)
		defer func() { e.settings.places[account] = e.settings.places[account][:1] }()
		resp, body := get(t, noFollow(), e.api.URL+"/api/v1/places/"+placeID(2)+"/unpriced", c)
		if resp.status != http.StatusOK || body != `{"charges":0,"first_day":null}`+"\n" {
			t.Errorf("work: %d %s", resp.status, body)
		}
	})
	t.Run("another account's place is not found", func(t *testing.T) {
		other := e.session(t, "other")
		for _, id := range []string{placeID(1), "not-a-uuid"} {
			resp, body := get(t, noFollow(), e.api.URL+"/api/v1/places/"+id+"/unpriced", other)
			if resp.status != http.StatusNotFound {
				t.Errorf("%s: %d %s", id, resp.status, body)
			}
			wantError(t, body, codeNotFound)
		}
	})
	t.Run("errors", func(t *testing.T) {
		e.reader.err = errors.New("boom")
		resp, body := get(t, noFollow(), u, c)
		e.reader.err = nil
		if resp.status != http.StatusInternalServerError {
			t.Errorf("charges not read: %d %s", resp.status, body)
		}
		wantError(t, body, codeInternal)
	})
}
