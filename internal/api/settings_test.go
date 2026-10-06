package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// memSettings is an in-memory Settings, by account, refusing like the store.
type memSettings struct {
	mu       sync.Mutex
	currency map[string]core.Currency
	places   map[string][]core.Place
	next     int
	writes   int // calls of the methods that write
	err      error
}

func newMemSettings() *memSettings {
	return &memSettings{currency: map[string]core.Currency{}, places: map[string][]core.Place{}}
}

func placeID(n int) string { return fmt.Sprintf("5e1f0000-0000-4000-8000-%012d", n) }

func (m *memSettings) Currency(_ context.Context, acc string) (core.Value[core.Currency], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.currency[acc]
	return core.Value[core.Currency]{V: c, OK: ok}, m.err
}

func (m *memSettings) SetCurrency(_ context.Context, acc string, c core.Currency) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	if m.err != nil {
		return m.err
	}
	if cur, ok := m.currency[acc]; ok && cur != c &&
		slices.ContainsFunc(m.places[acc], func(p core.Place) bool { return len(p.Tariff) > 0 }) {
		return ErrCurrencyInUse
	}
	m.currency[acc] = c
	return nil
}

func (m *memSettings) Places(_ context.Context, acc string) ([]core.Place, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.places[acc]), m.err
}

func (m *memSettings) Place(_ context.Context, acc, id string) (core.Place, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.places[acc], func(p core.Place) bool { return p.ID == id })
	if i < 0 {
		return core.Place{}, false, m.err
	}
	return m.places[acc][i], true, m.err
}

// withoutPositionTaken tells whether another place than id takes the charges without a
// position.
func (m *memSettings) withoutPositionTaken(acc, id string) bool {
	return slices.ContainsFunc(m.places[acc], func(p core.Place) bool { return p.WithoutPosition && p.ID != id })
}

func (m *memSettings) CreatePlace(_ context.Context, acc string, p core.Place) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	switch {
	case m.err != nil:
		return "", m.err
	case p.WithoutPosition && m.withoutPositionTaken(acc, ""):
		return "", ErrWithoutPositionTaken
	case len(m.places[acc]) >= MaxPlaces:
		return "", ErrTooManyPlaces
	}
	m.next++
	p.ID = placeID(m.next)
	m.places[acc] = append(m.places[acc], p)
	return p.ID, nil
}

func (m *memSettings) ReplacePlace(_ context.Context, acc string, p core.Place) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	i := slices.IndexFunc(m.places[acc], func(q core.Place) bool { return q.ID == p.ID })
	switch {
	case m.err != nil:
		return m.err
	case i < 0:
		return ErrNotFound
	case p.WithoutPosition && m.withoutPositionTaken(acc, p.ID):
		return ErrWithoutPositionTaken
	}
	m.places[acc][i] = p
	return nil
}

func (m *memSettings) DeletePlace(_ context.Context, acc, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	i := slices.IndexFunc(m.places[acc], func(p core.Place) bool { return p.ID == id })
	switch {
	case m.err != nil:
		return m.err
	case i < 0:
		return ErrNotFound
	}
	m.places[acc] = slices.Delete(m.places[acc], i, i+1)
	return nil
}

// homeName and the position of homeBody must never reach a log or an error.
const homeName = "Maison de Tante Agathe"

// homeBody is a place with two versions of its tariff: off-peak hours at night, then
// seasonal ones and a weekend price.
var homeBody = `{"name":"` + homeName + `","position":{"lat":45.764,"lon":4.8357},"radius_m":100,` +
	`"time_zone":"Europe/Paris","without_position":true,"max_power_kw":11,"efficiency":null,"tariff":[` +
	`{"valid_from":"2026-02-01","price_per_kwh":0.2516,"windows":[` +
	`{"days":["mon","tue","wed","thu","fri","sat","sun"],"from":"22:00","to":"06:00","months":[],"price_per_kwh":0.1828}]},` +
	`{"valid_from":"2026-08-01","price_per_kwh":0.2142,"windows":[` +
	`{"days":["mon","tue","wed","thu","fri","sat","sun"],"from":"23:00","to":"07:00","months":[11,12,1,2,3],"price_per_kwh":0.1589},` +
	`{"days":["mon","tue","wed","thu","fri","sat","sun"],"from":"02:00","to":"07:00","months":[4,5,6,7,8,9,10],"price_per_kwh":0.1589},` +
	`{"days":["sat","sun"],"from":"00:00","to":"00:00","months":[],"price_per_kwh":0.19}]}]}`

// workBody is a place with a single price: free charging at work.
const workBody = `{"name":"Work","position":{"lat":45.7797,"lon":4.927},"radius_m":250,"time_zone":"Europe/Paris",` +
	`"without_position":false,"max_power_kw":null,"efficiency":0.9,"tariff":[{"valid_from":"2026-01-01","price_per_kwh":0,"windows":[]}]}`

const jsonType = "application/json"

// logged is a server log kept in memory.
func logged(e *env) *bytes.Buffer {
	var buf bytes.Buffer
	e.server.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	return &buf
}

// withoutID is a place without its ID, its keys sorted: what was written.
func withoutID(t *testing.T, body string) string {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	delete(p, "id")
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSettingsJSON(t *testing.T) {
	e := newEnv(t, 10)
	c := e.session(t, "admin")
	u := e.api.URL + "/api/v1/settings"

	_, body := get(t, noFollow(), u, c)
	golden(t, "settings-unset.json", body)

	resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, `{"currency":"EUR"}`, c)
	if resp.status != http.StatusOK {
		t.Fatalf("put: %d %s", resp.status, body)
	}
	golden(t, "settings.json", body)
	if _, got := get(t, noFollow(), u, c); got != body {
		t.Errorf("GET after PUT = %s, want %s", got, body)
	}
	// Another account has its own.
	if _, got := get(t, noFollow(), u, e.session(t, "other")); !strings.HasPrefix(got, `{"currency":null,`) {
		t.Errorf("other account: %s", got)
	}

	for _, tt := range []struct {
		name, contentType, body string
		status                  int
		code                    errorCode
	}{
		{"not accepted", jsonType, `{"currency":"XYZ"}`, http.StatusBadRequest, codeInvalidBody},
		{"lowercase", jsonType, `{"currency":"eur"}`, http.StatusBadRequest, codeInvalidBody},
		{"missing", jsonType, `{}`, http.StatusBadRequest, codeInvalidBody},
		{"null", jsonType, `{"currency":null}`, http.StatusBadRequest, codeInvalidBody},
		{"unknown field", jsonType, `{"currency":"EUR","locale":"fr"}`, http.StatusBadRequest, codeInvalidBody},
		{"no content type", "", `{"currency":"SEK"}`, http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
		{"form", "application/x-www-form-urlencoded", "currency=SEK", http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
	} {
		resp, body := do(t, noFollow(), http.MethodPut, u, tt.contentType, tt.body, c)
		if resp.status != tt.status {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		wantError(t, body, tt.code)
	}

	// A tariff gives the amounts their unit: the currency can no longer change.
	if resp, body := do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/places", jsonType, workBody, c); resp.status != http.StatusCreated {
		t.Fatalf("place: %d %s", resp.status, body)
	}
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, `{"currency":"SEK"}`, c)
	if resp.status != http.StatusConflict {
		t.Errorf("change in use: %d %s", resp.status, body)
	}
	wantError(t, body, codeCurrencyInUse)
	if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, `{"currency":"EUR"}`, c); resp.status != http.StatusOK {
		t.Errorf("same currency: %d %s", resp.status, body)
	}
}

func TestPlacesJSON(t *testing.T) {
	e := newEnv(t, 10)
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + "/api/v1/places"

	if _, body := get(t, noFollow(), u, c); body != `{"items":[]}`+"\n" {
		t.Errorf("no place: %s", body)
	}
	resp, body := do(t, noFollow(), http.MethodPost, u, "application/json; charset=utf-8", homeBody, c)
	if resp.status != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.status, body)
	}
	golden(t, "place.json", body)
	// What was written is what is read: days, times, months and decimals.
	if got := withoutID(t, body); got != withoutID(t, homeBody) {
		t.Errorf("created %s\nwant    %s", got, withoutID(t, homeBody))
	}
	home := u + "/" + placeID(1)
	if _, got := get(t, noFollow(), home, c); got != body {
		t.Errorf("GET = %s, want %s", got, body)
	}
	if resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, workBody, c); resp.status != http.StatusCreated {
		t.Fatalf("create work: %d %s", resp.status, body)
	}
	_, body = get(t, noFollow(), u, c)
	golden(t, "places.json", body)

	t.Run("replace", func(t *testing.T) {
		// Versions in any order, a window whose days are listed out of order: the
		// versions come back oldest first, the days as written.
		moved := `{"name":"Home","position":{"lat":45.7641,"lon":4.8358},"radius_m":20,"time_zone":"Europe/Stockholm",` +
			`"without_position":false,"max_power_kw":null,"efficiency":0.8,"tariff":[` +
			`{"valid_from":"2026-09-01","price_per_kwh":1.23456,"windows":[]},` +
			`{"valid_from":"2025-12-31","price_per_kwh":100,"windows":[{"days":["sun","mon"],"from":"23:59","to":"00:00","months":[12],"price_per_kwh":0.00001}]}]}`
		want := `{"name":"Home","position":{"lat":45.7641,"lon":4.8358},"radius_m":20,"time_zone":"Europe/Stockholm",` +
			`"without_position":false,"max_power_kw":null,"efficiency":0.8,"tariff":[` +
			`{"valid_from":"2025-12-31","price_per_kwh":100,"windows":[{"days":["sun","mon"],"from":"23:59","to":"00:00","months":[12],"price_per_kwh":0.00001}]},` +
			`{"valid_from":"2026-09-01","price_per_kwh":1.23456,"windows":[]}]}`
		resp, body := do(t, noFollow(), http.MethodPut, home, jsonType, moved, c)
		if resp.status != http.StatusOK {
			t.Fatalf("replace: %d %s", resp.status, body)
		}
		if got := withoutID(t, body); got != withoutID(t, want) {
			t.Errorf("replaced %s\nwant     %s", got, withoutID(t, want))
		}
		if _, got := get(t, noFollow(), home, c); got != body {
			t.Errorf("GET = %s, want %s", got, body)
		}
		if !strings.Contains(body, `"id":"`+placeID(1)+`"`) {
			t.Errorf("ID changed: %s", body)
		}
		// The body's without_position is taken back.
		if resp, body := do(t, noFollow(), http.MethodPut, home, jsonType, homeBody, c); resp.status != http.StatusOK {
			t.Errorf("replace back: %d %s", resp.status, body)
		}
	})
	t.Run("one place without position", func(t *testing.T) {
		second := strings.Replace(workBody, `"without_position":false`, `"without_position":true`, 1)
		resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, second, c)
		if resp.status != http.StatusConflict {
			t.Errorf("create: %d %s", resp.status, body)
		}
		wantError(t, body, codeWithoutPositionTaken)
		resp, body = do(t, noFollow(), http.MethodPut, u+"/"+placeID(2), jsonType, second, c)
		if resp.status != http.StatusConflict {
			t.Errorf("replace: %d %s", resp.status, body)
		}
		wantError(t, body, codeWithoutPositionTaken)
	})
	t.Run("another account's places are not found", func(t *testing.T) {
		other := e.session(t, "other")
		if _, body := get(t, noFollow(), u, other); body != `{"items":[]}`+"\n" {
			t.Errorf("list: %s", body)
		}
		for _, id := range []string{placeID(1), placeID(9), "not-a-uuid", "5e1f0000-0000-4000-8000-00000000000'"} {
			for _, tt := range []struct{ method, contentType, body string }{
				{http.MethodGet, "", ""}, {http.MethodPut, jsonType, workBody}, {http.MethodDelete, "", ""},
			} {
				resp, body := do(t, noFollow(), tt.method, u+"/"+id, tt.contentType, tt.body, other)
				if resp.status != http.StatusNotFound {
					t.Errorf("%s %s: %d %s", tt.method, id, resp.status, body)
				}
				wantError(t, body, codeNotFound)
			}
		}
		if _, body := get(t, noFollow(), home, c); !strings.Contains(body, homeName) {
			t.Errorf("the owner's place changed: %s", body)
		}
	})
	t.Run("delete", func(t *testing.T) {
		resp, body := do(t, noFollow(), http.MethodDelete, u+"/"+placeID(2), "", "", c)
		if resp.status != http.StatusNoContent || body != "" {
			t.Errorf("delete: %d %q", resp.status, body)
		}
		for _, method := range []string{http.MethodGet, http.MethodDelete} {
			resp, body := do(t, noFollow(), method, u+"/"+placeID(2), "", "", c)
			if resp.status != http.StatusNotFound {
				t.Errorf("%s after delete: %d", method, resp.status)
			}
			wantError(t, body, codeNotFound)
		}
	})
	t.Run("at most 20 places", func(t *testing.T) {
		for range MaxPlaces - 1 {
			if resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, workBody, c); resp.status != http.StatusCreated {
				t.Fatalf("create: %d %s", resp.status, body)
			}
		}
		resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, workBody, c)
		if resp.status != http.StatusConflict {
			t.Errorf("21st place: %d %s", resp.status, body)
		}
		wantError(t, body, codeTooManyPlaces)
	})
	if strings.Contains(logs.String(), homeName) || strings.Contains(logs.String(), "45.76") {
		t.Errorf("a place reached the log:\n%s", logs)
	}
}

// edit applies change to the decoded homeBody and encodes it again.
func edit(t *testing.T, change func(p map[string]any)) string {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(homeBody), &p); err != nil {
		t.Fatal(err)
	}
	change(p)
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func version(p map[string]any, i int) map[string]any { return p["tariff"].([]any)[i].(map[string]any) }

func window(p map[string]any, i, j int) map[string]any {
	return version(p, i)["windows"].([]any)[j].(map[string]any)
}

// TestPlaceValidation checks every rule of a place's body, before the store: one case
// per rule, each a 400 that names the field and never echoes the name or the position.
func TestPlaceValidation(t *testing.T) {
	e := newEnv(t, 10)
	logs := logged(e)
	c := e.session(t, "admin")
	u := e.api.URL + "/api/v1/places"
	if resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, homeBody, c); resp.status != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.status, body)
	}
	e.settings.writes = 0

	windows := func(n int) []any {
		var ws []any
		for range n {
			ws = append(ws, map[string]any{"days": []string{"mon"}, "from": "22:00", "to": "06:00", "months": []int{}, "price_per_kwh": 0.1})
		}
		return ws
	}
	versions := func(n int) []any {
		var vs []any
		for i := range n {
			vs = append(vs, map[string]any{"valid_from": t0.AddDate(0, 0, i).Format(time.DateOnly), "price_per_kwh": 0.2, "windows": []any{}})
		}
		return vs
	}
	for _, tt := range []struct {
		name   string
		change func(p map[string]any)
		field  string
	}{
		{"empty name", func(p map[string]any) { p["name"] = "" }, "body.name"},
		{"name of 61 characters", func(p map[string]any) { p["name"] = homeName + strings.Repeat("é", 61-len([]rune(homeName))) }, "body.name"},
		{"latitude", func(p map[string]any) { p["position"] = map[string]any{"lat": 90.5, "lon": 4.8357} }, "body.position.lat"},
		{"longitude", func(p map[string]any) { p["position"] = map[string]any{"lat": 45.764, "lon": -180.5} }, "body.position.lon"},
		{"radius below 20 m", func(p map[string]any) { p["radius_m"] = 19.9 }, "body.radius_m"},
		{"radius above 1 km", func(p map[string]any) { p["radius_m"] = 1001 }, "body.radius_m"},
		{"unknown time zone", func(p map[string]any) { p["time_zone"] = "Europe/Lyon" }, "body.time_zone"},
		{"server's time zone", func(p map[string]any) { p["time_zone"] = "Local" }, "body.time_zone"},
		{"empty time zone", func(p map[string]any) { p["time_zone"] = "" }, "body.time_zone"},
		{"zero power", func(p map[string]any) { p["max_power_kw"] = 0 }, "body.max_power_kw"},
		{"power above 400 kW", func(p map[string]any) { p["max_power_kw"] = 401 }, "body.max_power_kw"},
		{"zero efficiency", func(p map[string]any) { p["efficiency"] = 0 }, "body.efficiency"},
		{"efficiency above 1", func(p map[string]any) { p["efficiency"] = 1.01 }, "body.efficiency"},
		{"no tariff", func(p map[string]any) { p["tariff"] = []any{} }, "body.tariff"},
		{"51 versions", func(p map[string]any) { p["tariff"] = versions(51) }, "body.tariff"},
		{"two versions the same day", func(p map[string]any) { version(p, 1)["valid_from"] = "2026-02-01" }, "body.tariff[1].valid_from"},
		{"not a date", func(p map[string]any) { version(p, 0)["valid_from"] = "2026-02-30" }, "body.tariff[0].valid_from"},
		{"negative price", func(p map[string]any) { version(p, 0)["price_per_kwh"] = -0.1 }, "body.tariff[0].price_per_kwh"},
		{"price above the cap", func(p map[string]any) { version(p, 0)["price_per_kwh"] = 10000.00001 }, "body.tariff[0].price_per_kwh"},
		{"6 decimals", func(p map[string]any) { version(p, 0)["price_per_kwh"] = 0.214201 }, "body.tariff[0].price_per_kwh"},
		{"6 decimals in a window", func(p map[string]any) { window(p, 1, 2)["price_per_kwh"] = 0.000001 }, "body.tariff[1].windows[2].price_per_kwh"},
		{"25 windows", func(p map[string]any) { version(p, 0)["windows"] = windows(25) }, "body.tariff[0].windows"},
		{"no day", func(p map[string]any) { window(p, 0, 0)["days"] = []string{} }, "body.tariff[0].windows[0].days"},
		{"a day twice", func(p map[string]any) { window(p, 0, 0)["days"] = []string{"mon", "mon"} }, "body.tariff[0].windows[0].days"},
		{"a day by number", func(p map[string]any) { window(p, 0, 0)["days"] = []int{1} }, "body.tariff[0].windows[0].days[0]"},
		{"a month twice", func(p map[string]any) { window(p, 1, 0)["months"] = []int{11, 11} }, "body.tariff[1].windows[0].months"},
		{"month 13", func(p map[string]any) { window(p, 1, 0)["months"] = []int{13} }, "body.tariff[1].windows[0].months[0]"},
		{"24:00", func(p map[string]any) { window(p, 0, 0)["to"] = "24:00" }, "body.tariff[0].windows[0].to"},
		{"minutes as a number", func(p map[string]any) { window(p, 0, 0)["from"] = 1320 }, "body.tariff[0].windows[0].from"},
		{"no minutes", func(p map[string]any) { window(p, 0, 0)["from"] = "22" }, "body.tariff[0].windows[0].from"},
		{"days null", func(p map[string]any) { window(p, 0, 0)["days"] = nil }, "body.tariff[0].windows[0].days"},
		{"a field missing", func(p map[string]any) { delete(p, "efficiency") }, "body"},
		{"an ID", func(p map[string]any) { p["id"] = placeID(1) }, "body"},
	} {
		body := edit(t, tt.change)
		for _, req := range []struct{ method, u string }{{http.MethodPost, u}, {http.MethodPut, u + "/" + placeID(1)}} {
			resp, got := do(t, noFollow(), req.method, req.u, jsonType, body, c)
			if resp.status != http.StatusBadRequest || !strings.Contains(got, tt.field) {
				t.Errorf("%s, %s: %d %s", tt.name, req.method, resp.status, got)
			}
			wantError(t, got, codeInvalidBody)
			if strings.Contains(got, "Agathe") || strings.Contains(got, "45.76") || strings.Contains(got, "4.83") {
				t.Errorf("%s, %s: the error echoes the place: %s", tt.name, req.method, got)
			}
		}
	}
	for _, tt := range []struct{ name, contentType, body string }{
		{"not JSON", jsonType, "{"},
		{"trailing data", jsonType, homeBody + " {}"},
		{"too large", jsonType, `{"name":"` + strings.Repeat("a", 1<<20) + `"}`},
	} {
		resp, body := do(t, noFollow(), http.MethodPost, u, tt.contentType, tt.body, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		wantError(t, body, codeInvalidBody)
	}
	for _, req := range []struct{ method, u string }{{http.MethodPost, u}, {http.MethodPut, u + "/" + placeID(1)}} {
		for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
			resp, body := do(t, noFollow(), req.method, req.u, contentType, homeBody, c)
			if resp.status != http.StatusUnsupportedMediaType {
				t.Errorf("%s, Content-Type %q: %d", req.method, contentType, resp.status)
			}
			wantError(t, body, codeUnsupportedMediaType)
		}
	}
	if e.settings.writes != 0 {
		t.Errorf("%d invalid bodies reached the store", e.settings.writes)
	}
	// The limits themselves are accepted.
	limits := edit(t, func(p map[string]any) {
		p["name"] = strings.Repeat("é", 60)
		p["position"] = map[string]any{"lat": -90, "lon": 180}
		p["radius_m"], p["max_power_kw"], p["efficiency"] = 1000, 400, 1
		p["time_zone"] = "UTC"
		vs := versions(50)
		vs[0].(map[string]any)["windows"] = windows(24)
		vs[1].(map[string]any)["price_per_kwh"] = 100
		p["tariff"] = vs
	})
	if resp, body := do(t, noFollow(), http.MethodPut, u+"/"+placeID(1), jsonType, limits, c); resp.status != http.StatusOK {
		t.Errorf("limits: %d %s", resp.status, body)
	}
	if strings.Contains(logs.String(), "Agathe") {
		t.Errorf("a place reached the log:\n%s", logs)
	}
}

func TestSettingsErrors(t *testing.T) {
	e := newEnv(t, 10)
	logs := logged(e)
	c := e.session(t, "admin")
	e.settings.err = errors.New("boom " + homeName)
	for _, tt := range []struct{ method, path, body string }{
		{http.MethodGet, "/settings", ""},
		{http.MethodPut, "/settings", `{"currency":"EUR"}`},
		{http.MethodGet, "/places", ""},
		{http.MethodPost, "/places", homeBody},
		{http.MethodGet, "/places/" + placeID(1), ""},
		{http.MethodGet, "/places/" + placeID(1) + "/unpriced", ""},
		{http.MethodPut, "/places/" + placeID(1), homeBody},
		{http.MethodDelete, "/places/" + placeID(1), ""},
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
	// The cause is logged: here the fake store's error, which a real one never words
	// with a place's name.
	if !strings.Contains(logs.String(), "boom") {
		t.Errorf("the cause is not logged:\n%s", logs)
	}
}

// TestPlaceOfDefaults checks what core allows and the API does not write: no zone is
// UTC, and a window without days opens every day.
func TestPlaceOfDefaults(t *testing.T) {
	p := placeOf(core.Place{ID: placeID(1), Tariff: []core.TariffVersion{{
		ValidFrom: core.Date{Year: 2026, Month: 1, Day: 1}, Windows: []core.PriceWindow{{From: 60, To: 90}},
	}}})
	w := p.Tariff[0].Windows[0]
	if p.TimeZone != "UTC" || len(w.Days) != 7 || w.Days[0] != "mon" || w.Days[6] != "sun" || w.From != "01:00" || w.To != "01:30" {
		t.Errorf("placeOf = %+v", p)
	}
}

// TestLimitsMatchTheSpec checks that the bounds of the schema, written in huma's tags,
// are the limits GET /settings gives: a limit changed on one side only fails here.
func TestLimitsMatchTheSpec(t *testing.T) {
	schemas := New(Config{Log: quiet}).api.OpenAPI().Components.Schemas.Map()
	prop := func(schema, name string) *huma.Schema {
		t.Helper()
		s, ok := schemas[schema]
		if !ok {
			t.Fatalf("no schema %s", schema)
		}
		p, ok := s.Properties[name]
		if !ok {
			t.Fatalf("no property %s.%s", schema, name)
		}
		return p
	}
	num := func(p *float64) float64 {
		if p == nil {
			return -1
		}
		return *p
	}
	count := func(p *int) float64 {
		if p == nil {
			return -1
		}
		return float64(*p)
	}
	example := func(s *huma.Schema) float64 {
		if len(s.Examples) != 1 {
			return -1
		}
		v, _ := s.Examples[0].(float64)
		return v
	}
	decimals := fmt.Sprintf("with at most %d decimals.", maxPriceDecimals)
	for _, tt := range []struct {
		what      string
		got, want float64
	}{
		{"PlaceFields.name maxLength", count(prop("PlaceFields", "name").MaxLength), float64(limits.PlaceNameChars)},
		{"PlaceFields.radius_m minimum", num(prop("PlaceFields", "radius_m").Minimum), limits.RadiusM.Min},
		{"PlaceFields.radius_m maximum", num(prop("PlaceFields", "radius_m").Maximum), limits.RadiusM.Max},
		{"PlaceFields.radius_m example", example(prop("PlaceFields", "radius_m")), limits.RadiusM.Default},
		{"PlaceFields.max_power_kw maximum", num(prop("PlaceFields", "max_power_kw").Maximum), limits.MaxPowerKWMax},
		{"PlaceFields.tariff maxItems", count(prop("PlaceFields", "tariff").MaxItems), float64(limits.VersionsPerPlace)},
		{"TariffVersion.windows maxItems", count(prop("TariffVersion", "windows").MaxItems), float64(limits.WindowsPerVersion)},
		{"TariffVersion.price_per_kwh maximum", num(prop("TariffVersion", "price_per_kwh").Maximum), limits.PricePerKWh.Max},
		{"PriceWindow.price_per_kwh maximum", num(prop("PriceWindow", "price_per_kwh").Maximum), limits.PricePerKWh.Max},
		{"EnteredCost.amount_minor maximum", num(prop("EnteredCost", "amount_minor").Maximum), float64(limits.EnteredCost.AmountMinorMax)},
		{"EnteredCost.energy_kwh maximum", num(prop("EnteredCost", "energy_kwh").Maximum), limits.EnteredCost.EnergyKWhMax},
		{"EnteredCost.note maxLength", count(prop("EnteredCost", "note").MaxLength), float64(limits.EnteredCost.NoteChars)},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.what, tt.got, tt.want)
		}
	}
	for _, s := range []string{"TariffVersion", "PriceWindow"} {
		if d := prop(s, "price_per_kwh").Description; !strings.HasSuffix(d, decimals) {
			t.Errorf("%s.price_per_kwh: %q does not end with %q", s, d, decimals)
		}
	}
	if limits.Places != MaxPlaces {
		t.Errorf("places = %d, want MaxPlaces (%d)", limits.Places, MaxPlaces)
	}
}

// TestDefaultEfficiencyIsTheConfig checks that the settings give the efficiencies the
// costs are computed with, not a copy of the defaults.
func TestDefaultEfficiencyIsTheConfig(t *testing.T) {
	s := New(Config{Log: quiet, CostParams: core.CostParams{EfficiencyAC: 0.8, EfficiencyDC: 0.9}})
	got := s.settingsOf(core.Value[core.Currency]{}).DefaultEfficiency
	if got != (defaultEfficiencyJSON{AC: 0.8, DC: 0.9}) {
		t.Errorf("default_efficiency = %+v", got)
	}
}
