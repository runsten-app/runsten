package api

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"runsten/internal/core"
)

// purposeView is a trip's purpose, as the tests look into it.
type purposeView struct {
	ID            string  `json:"id"`
	Purpose       *string `json:"purpose"`
	PurposeOrigin *string `json:"purpose_origin"`
	PurposePlace  *struct {
		Name string `json:"name"`
	} `json:"purpose_place"`
}

// String is purpose/origin/place, - for null.
func (p purposeView) String() string {
	or := func(s *string) string {
		if s == nil {
			return "-"
		}
		return *s
	}
	place := "-"
	if p.PurposePlace != nil {
		place = p.PurposePlace.Name
	}
	return or(p.Purpose) + "/" + or(p.PurposeOrigin) + "/" + place
}

// TestTripPurpose: a trip's purpose is its mark by hand, else the rule of a place; the
// list and the CSV file keep the trips of one purpose; marks are written all or none.
func TestTripPurpose(t *testing.T) {
	e, c := readerEnv(t)
	base := e.api.URL + "/api/v1/vehicles/" + car
	const (
		morning       = "2026-09-28T07:01:00.000000Z" // home to work
		reconstructed = "2026-09-28T13:00:00.000000Z" // no position
		evening       = "2026-09-28T16:41:00.000000Z" // work to home
	)
	work := core.Place{
		ID: placeID(2), Name: "Work", Position: core.Position{Lat: 45.7797, Lon: 4.927}, RadiusM: 250,
		Rule: core.Value[core.PurposeRule]{V: core.PurposeRule{Purpose: core.PurposeCommute, Scope: core.ScopeEnd}, OK: true},
	}
	e.settings.places[account] = append(e.settings.places[account], work)

	list := func(t *testing.T, query string) (map[string]string, *string) {
		t.Helper()
		resp, body := get(t, noFollow(), base+"/trips"+query, c)
		if resp.status != http.StatusOK {
			t.Fatalf("trips%s: %d %s", query, resp.status, body)
		}
		var p struct {
			Items      []purposeView `json:"items"`
			NextCursor *string       `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, it := range p.Items {
			out[it.ID] = it.String()
		}
		return out, p.NextCursor
	}
	mark := func(t *testing.T, purpose string, ids ...string) (int, string) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"trips": ids, "purpose": purpose})
		resp, body := do(t, noFollow(), http.MethodPut, base+"/trip-marks", jsonType, string(b), c)
		return resp.status, body
	}
	want := func(t *testing.T, query string, w map[string]string) {
		t.Helper()
		if got, _ := list(t, query); !maps.Equal(got, w) {
			t.Errorf("trips%s = %v, want %v", query, got, w)
		}
	}

	t.Run("by rule", func(t *testing.T) {
		want(t, "", map[string]string{morning: "commute/rule/Work", reconstructed: "-/-/-", evening: "-/-/-"})
		_, body := get(t, noFollow(), base+"/trips/"+morning, c)
		var v purposeView
		if err := json.Unmarshal([]byte(body), &v); err != nil || v.String() != "commute/rule/Work" {
			t.Errorf("details: %s", body)
		}
	})

	t.Run("by hand", func(t *testing.T) {
		if status, body := mark(t, "business", morning, "2026-09-28T16:41:00Z"); status != http.StatusNoContent {
			t.Fatalf("mark: %d %s", status, body)
		}
		if status, body := mark(t, "none", evening); status != http.StatusNoContent {
			t.Fatalf("mark none: %d %s", status, body)
		}
		want(t, "", map[string]string{morning: "business/hand/-", reconstructed: "-/-/-", evening: "-/hand/-"})
		// Back to the rule.
		if status, body := mark(t, "rule", morning); status != http.StatusNoContent {
			t.Fatalf("back to the rule: %d %s", status, body)
		}
		want(t, "", map[string]string{morning: "commute/rule/Work", reconstructed: "-/-/-", evening: "-/hand/-"})
	})

	t.Run("filtered", func(t *testing.T) {
		want(t, "?purpose=commute", map[string]string{morning: "commute/rule/Work"})
		want(t, "?purpose=business", map[string]string{})
		want(t, "?purpose=none", map[string]string{reconstructed: "-/-/-", evening: "-/hand/-"})
		// A page at a time, newest first.
		first, next := list(t, "?purpose=none&limit=1")
		if !maps.Equal(first, map[string]string{evening: "-/hand/-"}) || next == nil {
			t.Fatalf("first page: %v, %v", first, next)
		}
		second, next := list(t, "?purpose=none&limit=1&cursor="+*next)
		if !maps.Equal(second, map[string]string{reconstructed: "-/-/-"}) || next != nil {
			t.Errorf("second page: %v, %v", second, next)
		}
		resp, body := get(t, noFollow(), base+"/trips?purpose=work", c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("unknown purpose: %d %s", resp.status, body)
		}
	})

	t.Run("in the CSV file", func(t *testing.T) {
		_, body := get(t, noFollow(), base+"/trips.csv?purpose=commute", c)
		lines := strings.Split(strings.TrimSpace(body), "\r\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[1], morning) || !strings.HasSuffix(lines[1], ",commute,rule") {
			t.Errorf("trips.csv?purpose=commute:\n%s", body)
		}
		_, body = get(t, noFollow(), base+"/trips.csv", c)
		if !strings.Contains(body, ",,hand\r\n") {
			t.Errorf("trips.csv, none by hand:\n%s", body)
		}
	})

	t.Run("refused", func(t *testing.T) {
		before := len(e.reader.marks)
		for _, tt := range []struct {
			name, body, contentType string
			status                  int
			code                    errorCode
		}{
			{"a trip of nowhere", `{"trips":["` + morning + `","2026-09-28T09:00:00Z"],"purpose":"personal"}`, jsonType, http.StatusNotFound, codeNotFound},
			{"not a trip ID", `{"trips":["yesterday"],"purpose":"personal"}`, jsonType, http.StatusNotFound, codeNotFound},
			{"an unknown purpose", `{"trips":["` + morning + `"],"purpose":"work"}`, jsonType, http.StatusBadRequest, codeInvalidBody},
			{"no trip", `{"trips":[],"purpose":"personal"}`, jsonType, http.StatusBadRequest, codeInvalidBody},
			{"not JSON", `trips=` + morning, "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
		} {
			resp, body := do(t, noFollow(), http.MethodPut, base+"/trip-marks", tt.contentType, tt.body, c)
			if resp.status != tt.status {
				t.Errorf("%s: %d %s", tt.name, resp.status, body)
			}
			wantError(t, body, tt.code)
		}
		resp, body := do(t, noFollow(), http.MethodPut, e.api.URL+"/api/v1/vehicles/"+foreign+"/trip-marks", jsonType,
			`{"trips":["`+morning+`"],"purpose":"personal"}`, c)
		if resp.status != http.StatusNotFound {
			t.Errorf("another account's vehicle: %d %s", resp.status, body)
		}
		if len(e.reader.marks) != before {
			t.Errorf("written anyway: %v", e.reader.marks)
		}
		e.reader.markErr = errors.New("boom")
		defer func() { e.reader.markErr = nil }()
		for _, tt := range []struct{ method, path, body string }{
			{http.MethodPut, "/trip-marks", `{"trips":["` + morning + `"],"purpose":"personal"}`},
			{http.MethodGet, "/trips", ""},
			{http.MethodGet, "/trips/" + morning, ""},
			{http.MethodGet, "/trips.csv", ""},
		} {
			ct := ""
			if tt.body != "" {
				ct = jsonType
			}
			resp, body := do(t, noFollow(), tt.method, base+tt.path, ct, tt.body, c)
			if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
				t.Errorf("%s %s: %d %s", tt.method, tt.path, resp.status, body)
			}
		}
	})

	t.Run("a place's rule", func(t *testing.T) {
		e.settings.places[account] = slices.DeleteFunc(e.settings.places[account], func(p core.Place) bool { return p.ID == work.ID })
		if status, body := mark(t, "rule", evening); status != http.StatusNoContent {
			t.Fatalf("back to the rule: %d %s", status, body)
		}
		// Either end: the way back too; and a place without a rule.
		work.Rule.V.Scope = core.ScopeEither
		e.settings.places[account] = append(e.settings.places[account], work)
		want(t, "", map[string]string{morning: "commute/rule/Work", reconstructed: "-/-/-", evening: "commute/rule/Work"})
	})
}

// TestPlaceRule: a place's rule goes and comes back with the place, both fields or none.
func TestPlaceRule(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/places"
	resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, workBody, c)
	if resp.status != http.StatusCreated || !strings.Contains(body, `"trip_purpose":"commute","trip_purpose_scope":"end"`) {
		t.Fatalf("create: %d %s", resp.status, body)
	}
	for _, half := range []string{
		strings.Replace(workBody, `"trip_purpose_scope":"end"`, `"trip_purpose_scope":null`, 1),
		strings.Replace(workBody, `"trip_purpose":"commute"`, `"trip_purpose":null`, 1),
	} {
		resp, body := do(t, noFollow(), http.MethodPost, u, jsonType, half, c)
		if resp.status != http.StatusBadRequest || !strings.Contains(body, "body.trip_purpose_scope") {
			t.Errorf("half a rule: %d %s", resp.status, body)
		}
	}
}
