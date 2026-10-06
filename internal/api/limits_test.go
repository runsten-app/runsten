package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// historyLimit hides the events that ended before from, for the account it limits.
type historyLimit struct {
	account          string
	from             time.Time
	noCosts, noStats bool
	noCSV            bool
	unread           []string
	err              error
	asOf             time.Time // the time of the latest question
}

func (l *historyLimit) Limits(_ context.Context, accountID string, at time.Time) (AccountLimits, error) {
	l.asOf = at
	if accountID != l.account {
		return AccountLimits{}, l.err
	}
	return AccountLimits{HistoryFrom: l.from, NoCosts: l.noCosts, NoStats: l.noStats, NoCSV: l.noCSV, UnreadVehicles: l.unread}, l.err
}

// TestLimits: the session tells the account's limits; the trips and charges that ended
// before its history are neither listed, nor found, nor counted, and a period before
// it is empty, never an error. Another account sees all its history.
func TestLimits(t *testing.T) {
	e, c := readerEnv(t)
	from := h(12, 0).In(time.FixedZone("CEST", 2*3600))
	l := &historyLimit{account: account, from: from}
	e.server.Limits = l
	base := e.api.URL + "/api/v1/"

	var sess struct {
		Limits *struct {
			HistoryFrom string `json:"history_from"`
		} `json:"limits"`
	}
	resp, body := get(t, noFollow(), base+"session", c)
	if err := json.Unmarshal([]byte(body), &sess); err != nil || resp.status != http.StatusOK || sess.Limits == nil ||
		sess.Limits.HistoryFrom != "2026-09-28T12:00:00Z" {
		t.Errorf("session: %d %s", resp.status, body)
	}
	if !l.asOf.Equal(e.clk.Now()) {
		t.Errorf("limits asked as of %v, want the clock's %v", l.asOf, e.clk.Now())
	}
	if _, body := get(t, noFollow(), base+"session", e.session(t, "other")); !json.Valid([]byte(body)) ||
		!strings.Contains(body, `"limits":null`) {
		t.Errorf("another account's session: %s", body)
	}

	ids := func(t *testing.T, target string) []string {
		t.Helper()
		resp, body := get(t, noFollow(), target, c)
		var p struct {
			Items []struct {
				DetectedAt time.Time `json:"detected_at"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil || resp.status != http.StatusOK {
			t.Fatalf("%s: %d %s", target, resp.status, body)
		}
		var out []string
		for _, it := range p.Items {
			out = append(out, it.DetectedAt.Format("15:04"))
		}
		return out
	}
	trips := base + "vehicles/" + car + "/trips"
	for query, want := range map[string][]string{
		"":                                   {"16:41", "13:00"},
		"?from=2026-09-28T00:00:00Z":         {"16:41", "13:00"},
		"?from=2026-09-28T16:00:00Z":         {"16:41"},
		"?to=2026-09-28T12:00:00Z":           nil,
		"?to=2026-09-28T11:00:00Z":           nil,
		"?to=2026-09-28T14:00:00Z&limit=200": {"13:00"},
	} {
		if got := ids(t, trips+query); !slices.Equal(got, want) {
			t.Errorf("trips%s: %v, want %v", query, got, want)
		}
	}
	for id, status := range map[string]int{"2026-09-28T07:01:00Z": http.StatusNotFound, "2026-09-28T13:00:00Z": http.StatusOK} {
		if resp, body := get(t, noFollow(), trips+"/"+id, c); resp.status != status {
			t.Errorf("trip %s: %d %s, want %d", id, resp.status, body, status)
		}
	}
	if resp, body := get(t, noFollow(), base+"vehicles/"+car+"/charges/2026-09-28T18:30:00.000000Z", c); resp.status != http.StatusOK {
		t.Errorf("a charge after the limit: %d %s", resp.status, body)
	}
	l.from = h(22, 0) // after the evening charge ended, at 21:56
	if got := ids(t, base+"vehicles/"+car+"/charges"); len(got) != 0 {
		t.Errorf("charges ended before the limit: %v", got)
	}
	if resp, _ := get(t, noFollow(), base+"vehicles/"+car+"/charges/2026-09-28T18:30:00.000000Z", c); resp.status != http.StatusNotFound {
		t.Errorf("a charge before the limit: %d", resp.status)
	}

	t.Run("statistics from the limit on", func(t *testing.T) {
		l.from = from
		e.clk.Advance(18 * time.Hour)
		var s struct {
			From   time.Time `json:"from"`
			Totals struct {
				Trips struct {
					Count int `json:"count"`
				} `json:"trips"`
			} `json:"totals"`
		}
		for _, query := range []string{"", "?from=2026-09-28T00:00:00Z"} {
			resp, body := get(t, noFollow(), base+"vehicles/"+car+"/stats"+query, c)
			if err := json.Unmarshal([]byte(body), &s); err != nil || resp.status != http.StatusOK {
				t.Fatalf("stats%s: %d %s", query, resp.status, body)
			}
			if !s.From.Equal(from) || s.Totals.Trips.Count != 2 {
				t.Errorf("stats%s: from %v, %d trips; want from %v, 2 trips", query, s.From, s.Totals.Trips.Count, from)
			}
		}
		resp, body := get(t, noFollow(), base+"vehicles/"+car+"/stats?to=2026-09-28T11:00:00Z", c)
		if err := json.Unmarshal([]byte(body), &s); err != nil || resp.status != http.StatusOK || s.Totals.Trips.Count != 0 {
			t.Errorf("stats before the limit: %d %s", resp.status, body)
		}
	})

	t.Run("costs and statistics left out", func(t *testing.T) {
		l.from, l.noCosts, l.noStats = time.Time{}, true, true
		charge := base + "vehicles/" + car + "/charges/2026-09-28T18:30:00.000000Z"
		resp, body := get(t, noFollow(), base+"session", c)
		if resp.status != http.StatusOK || !strings.Contains(body, `"limits":{"history_from":null,"unavailable":["costs","stats"],"unread_vehicles":[]}`) {
			t.Errorf("session: %s", body)
		}
		if _, body := get(t, noFollow(), charge, c); !strings.Contains(body, `"cost":null`) {
			t.Errorf("a charge's cost: %s", body)
		}
		if _, body := get(t, noFollow(), base+"vehicles/"+car+"/charges", c); strings.Contains(body, `"min_minor"`) {
			t.Errorf("the charges' costs: %s", body)
		}
		var s struct {
			Currency *struct{} `json:"currency"`
		}
		resp, body = get(t, noFollow(), base+"vehicles/"+car+"/stats", c)
		if err := json.Unmarshal([]byte(body), &s); err != nil || resp.status != http.StatusOK || s.Currency != nil ||
			strings.Contains(body, `"min_minor":5`) {
			t.Errorf("stats without costs: %d %s", resp.status, body)
		}
		refused := func(method, target, body string) {
			t.Helper()
			resp, got := do(t, noFollow(), method, target, jsonType, body, c)
			if resp.status != http.StatusForbidden {
				t.Errorf("%s %s: %d %s", method, target, resp.status, got)
			}
			wantError(t, got, "feature_unavailable")
		}
		refused(http.MethodGet, base+"vehicles/"+car+"/stats?bucket=day", "")
		refused(http.MethodPut, charge+"/cost", costBody)
		refused(http.MethodDelete, charge+"/cost", "")
		refused(http.MethodGet, base+"charge-costs/orphans", "")
		refused(http.MethodDelete, base+"charge-costs/orphans/"+costID(1), "")
		refused(http.MethodPut, base+"charge-costs/orphans/"+costID(1)+"/charge", `{"vehicle":"`+car+`","charge":"2026-09-28T18:30:00Z"}`)
		refused(http.MethodGet, base+"places/"+homePlace(t).ID+"/unpriced", "")
		l.noCosts, l.noStats = false, false
	})

	t.Run("vehicles left unread", func(t *testing.T) {
		l.from, l.unread = time.Time{}, []string{car}
		resp, body := get(t, noFollow(), base+"session", c)
		if resp.status != http.StatusOK ||
			!strings.Contains(body, `"limits":{"history_from":null,"unavailable":[],"unread_vehicles":["`+car+`"]}`) {
			t.Errorf("session: %s", body)
		}
		// What was read of it stays shown.
		if resp, body := get(t, noFollow(), trips, c); resp.status != http.StatusOK || !strings.Contains(body, `"items":[{`) {
			t.Errorf("trips of an unread vehicle: %d %s", resp.status, body)
		}
		l.unread = nil
	})

	t.Run("an unreadable limit fails", func(t *testing.T) {
		l.err = errors.New("database down")
		for _, target := range []string{base + "session", trips, trips + "/2026-09-28T13:00:00Z", base + "vehicles/" + car + "/stats"} {
			if resp, body := get(t, noFollow(), target, c); resp.status != http.StatusInternalServerError {
				t.Errorf("%s: %d %s", target, resp.status, body)
			}
		}
	})
}
