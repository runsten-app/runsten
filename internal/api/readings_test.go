package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// memResponses are the stored responses of the tests, of the account's car.
type memResponses struct {
	items []Response
	err   error
	// failAfter fails the read with err after that many responses; 0: at once.
	failAfter int
	// the latest question's period
	from, to time.Time
}

func (m *memResponses) EachResponse(_ context.Context, accountID, vehicleID string, from, to time.Time, fn func(Response) error) error {
	m.from, m.to = from, to
	if m.err != nil && m.failAfter == 0 {
		return m.err
	}
	if accountID != account || vehicleID != car {
		return nil
	}
	n := 0
	for _, r := range m.items {
		if r.FetchedAt.Before(from) || !r.FetchedAt.Before(to) {
			continue
		}
		if err := fn(r); err != nil {
			return err
		}
		if n++; m.err != nil && n == m.failAfter {
			return m.err
		}
	}
	return nil
}

const readingsCSVHeader = "fetched_at,source,endpoint,field,value"

func readingsEnv(t *testing.T) (*env, *http.Cookie, *memResponses) {
	t.Helper()
	e, c := readerEnv(t)
	m := &memResponses{items: []Response{
		{
			Source: "volvo", Endpoint: "odometer", FetchedAt: h(7, 0).Add(123 * time.Millisecond),
			Payload: []byte(`{"data":{"odometer":{"timestamp":"2026-09-28T06:59:00Z","unit":"km","value":12400}},"operationId":"op-1"}`),
		},
		// Fields of every kind, and those that are secret or not the vehicle's.
		{Source: "tesla", Endpoint: "vehicle_data", FetchedAt: h(8, 0), Payload: []byte(`{"response":{` +
			`"id":1234567890123456,"id_s":"1234567890123456","user_id":1111111111,"vehicle_id":987,"tokens":["secret"],` +
			`"backseat_token":"secret","access_type":"OWNER","granular_access":{"hide_private":false},` +
			`"vin":"LRWYGCEK9MC000000","charge_state":{"battery_level":69,"charge_rate":80.50,"user_charge_enable_request":null},` +
			`"vehicle_state":{"vehicle_name":"=HYPERLINK(\"x\")","tpms":[2.9,{"rl":null}],"media":{},"seats":[],"locked":true,` +
			`"note":"a, \"b\""}}}`)},
		// A source no declaration covers: left out whole.
		{Source: "other", Endpoint: "x", FetchedAt: h(9, 0), Payload: []byte(`{"token":"secret"}`)},
		{Source: "polestar", Endpoint: "battery", FetchedAt: h(10, 0), Payload: []byte(`{"data":{"batteryChargeLevelPercentage":80}}`)},
	}}
	e.server.Responses = m
	e.server.PrivateFields = map[string][]string{
		"volvo":    {"operationId"},
		"polestar": {},
		"tesla": {
			"response.tokens", "response.backseat_token", "response.user_id", "response.vehicle_id",
			"response.id", "response.id_s", "response.access_type", "response.granular_access",
		},
	}
	return e, c, m
}

// TestReadingsCSV: every value of the responses of the period, one per row, as the
// provider gave it, their private fields and the sources without a declaration left out.
func TestReadingsCSV(t *testing.T) {
	e, c, m := readingsEnv(t)
	u := e.api.URL + "/api/v1/vehicles/" + car + "/readings.csv"
	day := "?from=2026-09-28T00:00:00Z&to=2026-09-29T00:00:00Z"

	resp, body := get(t, noFollow(), u+day, c)
	if resp.status != http.StatusOK || resp.header.Get("Content-Type") != "text/csv; charset=utf-8" ||
		resp.header.Get("Content-Disposition") != `attachment; filename="runsten-readings-2026-09-28.csv"` ||
		resp.header.Get("Cache-Control") != "no-store" {
		t.Errorf("readings: %d %v", resp.status, resp.header)
	}
	want := csvLines(readingsCSVHeader,
		"2026-09-28T07:00:00Z,volvo,odometer,data.odometer.timestamp,2026-09-28T06:59:00Z",
		"2026-09-28T07:00:00Z,volvo,odometer,data.odometer.unit,km",
		"2026-09-28T07:00:00Z,volvo,odometer,data.odometer.value,12400",
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.charge_state.battery_level,69",
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.charge_state.charge_rate,80.50",
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.charge_state.user_charge_enable_request,",
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.locked,true",
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.media,{}",
		`2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.note,"a, ""b"""`,
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.seats,[]",
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.tpms[0],2.9",
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.tpms[1].rl,",
		`2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.vehicle_name,"'=HYPERLINK(""x"")"`,
		"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vin,LRWYGCEK9MC000000",
		"2026-09-28T10:00:00Z,polestar,battery,data.batteryChargeLevelPercentage,80",
	)
	if body != want {
		t.Errorf("readings:\n%s\nwant:\n%s", body, want)
	}
	if strings.Contains(body, "secret") || strings.Contains(body, "1111111111") || strings.Contains(body, "op-1") {
		t.Errorf("a private field: %s", body)
	}

	// The period: [from, to), in any offset.
	if _, body := get(t, noFollow(), u+"?from=2026-09-28T09:30:00%2B02:00&to=2026-09-28T10:00:00Z", c); body !=
		csvLines(readingsCSVHeader, "2026-09-28T08:00:00Z,tesla,vehicle_data,response.charge_state.battery_level,69",
			"2026-09-28T08:00:00Z,tesla,vehicle_data,response.charge_state.charge_rate,80.50",
			"2026-09-28T08:00:00Z,tesla,vehicle_data,response.charge_state.user_charge_enable_request,",
			"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.locked,true",
			"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.media,{}",
			`2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.note,"a, ""b"""`,
			"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.seats,[]",
			"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.tpms[0],2.9",
			"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.tpms[1].rl,",
			`2026-09-28T08:00:00Z,tesla,vehicle_data,response.vehicle_state.vehicle_name,"'=HYPERLINK(""x"")"`,
			"2026-09-28T08:00:00Z,tesla,vehicle_data,response.vin,LRWYGCEK9MC000000") {
		t.Errorf("an hour: %s", body)
	}
	if !m.from.Equal(h(7, 30)) || !m.to.Equal(h(10, 0)) {
		t.Errorf("asked [%v, %v)", m.from, m.to)
	}
	month := "?from=2026-08-27T23:00:00Z&to=2026-09-28T00:00:00Z" // a month with a day of 25 hours
	if resp, body := get(t, noFollow(), u+month, c); resp.status != http.StatusOK || body != csvLines(readingsCSVHeader) {
		t.Errorf("31 days without a response: %d %s", resp.status, body)
	}

	t.Run("limits", func(t *testing.T) {
		l := &historyLimit{account: account, from: h(9, 0)}
		e.server.Limits = l
		defer func() { e.server.Limits = nil }()
		if _, body := get(t, noFollow(), u+day, c); body != csvLines(readingsCSVHeader,
			"2026-09-28T10:00:00Z,polestar,battery,data.batteryChargeLevelPercentage,80") {
			t.Errorf("before the history: %s", body)
		}
		m.from = time.Time{}
		if _, body := get(t, noFollow(), u+"?from=2026-09-28T00:00:00Z&to=2026-09-28T09:00:00Z", c); body != csvLines(readingsCSVHeader) || !m.from.IsZero() {
			t.Errorf("a period before the history: %s, asked from %v", body, m.from)
		}
		l.noCSV = true
		resp, body := get(t, noFollow(), u+day, c)
		if resp.status != http.StatusForbidden {
			t.Errorf("without CSV: %d", resp.status)
		}
		wantError(t, body, codeFeatureUnavailable)
	})

	for query, msg := range map[string]string{
		"":                           "to is required",
		"?from=2026-09-28T00:00:00Z": "to is required",
		"?to=2026-09-28T00:00:00Z":   "from is required",
		"?from=yesterday&to=2026-09-28T00:00:00Z":            "not a time",
		"?from=2026-09-29T00:00:00Z&to=2026-09-28T00:00:00Z": "to before from",
		"?from=2026-09-28T00:00:00Z&to=2026-09-28T00:00:00Z": "an empty period",
		"?from=2026-08-27T22:59:59Z&to=2026-09-28T00:00:00Z": "more than 31 days",
	} {
		resp, body := get(t, noFollow(), u+query, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: %d", msg, resp.status)
		}
		wantError(t, body, codeInvalidParameter)
	}
	if resp, _ := get(t, noFollow(), u+day); resp.status != http.StatusUnauthorized {
		t.Errorf("without a session: %d", resp.status)
	}
	resp, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+foreign+"/readings.csv"+day, c)
	if resp.status != http.StatusNotFound {
		t.Errorf("another account's vehicle: %d", resp.status)
	}
	wantError(t, body, codeNotFound)

	m.err = errors.New("boom")
	resp, body = get(t, noFollow(), u+day, c)
	if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") || resp.header.Get("Content-Disposition") != "" {
		t.Errorf("not read: %d %v %s", resp.status, resp.header, body)
	}
	wantError(t, body, codeInternal)
	// A payload that is no JSON fails the same way.
	m.err, m.items = nil, []Response{{Source: "volvo", Endpoint: "odometer", FetchedAt: h(7, 0), Payload: []byte(`{`)}}
	if resp, _ := get(t, noFollow(), u+day, c); resp.status != http.StatusInternalServerError {
		t.Errorf("a payload not read: %d", resp.status)
	}
}

// TestReadingsCutShort: a failure after the first bytes cannot change the status: the
// file is cut short. The contract's check is left out, as the export's.
func TestReadingsCutShort(t *testing.T) {
	e, _, m := readingsEnv(t)
	// Enough rows to fill the buffer before the failure.
	m.items = nil
	for i := range 2000 {
		m.items = append(m.items, Response{
			Source: "polestar", Endpoint: "battery", FetchedAt: h(7, 0).Add(time.Duration(i) * time.Second),
			Payload: []byte(`{"data":{"batteryChargeLevelPercentage":` + strconv.Itoa(i%100) + `}}`),
		})
	}
	m.err, m.failAfter = errors.New("boom"), 1999
	tok, _, _ := e.sessions.Login(t.Context(), "admin", password, "test")
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"/api/v1/vehicles/"+car+"/readings.csv?from=2026-09-28T00:00:00Z&to=2026-09-29T00:00:00Z", http.NoBody)
	req.AddCookie(reqCookie("runsten_session", tok))
	rec := httptest.NewRecorder()
	e.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), readingsCSVHeader+"\r\n") ||
		strings.Count(rec.Body.String(), "\r\n") >= 2000 {
		t.Errorf("cut short: %d, %d lines", rec.Code, strings.Count(rec.Body.String(), "\r\n"))
	}
}

// TestLeaves: the paths of a document's leaves, an array at its root included.
func TestLeaves(t *testing.T) {
	var got []string
	err := leaves([]byte(`[{"a":{"b":1}},"x",[]]`), []string{"[0].a.b"}, func(field, value string) error {
		got = append(got, field+"="+value)
		return nil
	})
	if err != nil || strings.Join(got, " ") != "[1]=x [2]=[]" {
		t.Errorf("leaves: %v, %v", got, err)
	}
}
