package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// csvLines joins the lines of a CSV file, as RFC 4180 ends them.
func csvLines(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

const (
	tripsCSVHeader = "id,reconstructed,start_after,start_before,end_after,end_before,distance_km,start_odometer_km," +
		"end_odometer_km,start_soc_pct,end_soc_pct,start_range_km,end_range_km,energy_kwh,capacity_kwh,capacity_source," +
		"start_lat,start_lon,start_place,start_address,end_lat,end_lon,end_place,end_address,vehicle_trip_meter_km," +
		"vehicle_consumption_kwh_per_100km"
	tripMorning = "2026-09-28T07:01:00.000000Z,false,2026-09-28T06:55:00Z,2026-09-28T07:01:00Z,2026-09-28T07:39:00Z," +
		"2026-09-28T07:40:00Z,32,12400,12432,62,54.5,248,218,6,80,api,45.764,4.8357,Maison de Tante Agathe,,45.7797,4.927,,,32.2,18.1"
	tripReconstructed = "2026-09-28T13:00:00.000000Z,true,2026-09-28T12:00:00Z,2026-09-28T13:00:00Z,2026-09-28T12:00:00Z," +
		"2026-09-28T13:00:00Z,8,12432,12440,,,,,,,,,,,,,,,,,"
	tripEvening = "2026-09-28T16:41:00.000000Z,false,2026-09-28T16:35:00Z,2026-09-28T16:41:00Z,2026-09-28T17:24:00Z," +
		"2026-09-28T17:25:00Z,33,12440,12473,,,,,,75,catalog_net,45.7797,4.927,,,45.764,4.8357,Maison de Tante Agathe,,,"

	chargesCSVHeader = "id,reconstructed,start_after,start_before,end_after,end_before,type,start_soc_pct,end_soc_pct," +
		"target_soc_pct,energy_soc_kwh,energy_power_kwh,capacity_kwh,capacity_source,lat,lon,place,address,cost_currency," +
		"cost_min,cost_max,cost_source,cost_energy_kwh,cost_efficiency,cost_note"
	chargeNight = "2026-09-28T04:00:00.000000Z,true,2026-09-28T01:00:00Z,2026-09-28T04:00:00Z,2026-09-28T01:00:00Z," +
		"2026-09-28T04:00:00Z,,40,62,,17.6,,80,api,,,,,,,,,,,"
	chargeEvening = "2026-09-28T18:30:00.000000Z,false,2026-09-28T18:20:00Z,2026-09-28T18:30:00Z,2026-09-28T21:55:00Z," +
		"2026-09-28T21:56:00Z,AC,47,80,80,26.4,25.3,80,api,45.764,4.8357,Maison de Tante Agathe,,EUR,5.24,5.79,tariff,30,0.88,"
	chargeEveningNoCost = "2026-09-28T18:30:00.000000Z,false,2026-09-28T18:20:00Z,2026-09-28T18:30:00Z,2026-09-28T21:55:00Z," +
		"2026-09-28T21:56:00Z,AC,47,80,80,26.4,25.3,80,api,45.764,4.8357,Maison de Tante Agathe,,,,,,,,"
)

// TestCSV: the CSV files hold the items of the lists, every one of the period, oldest
// first, under the same limits.
func TestCSV(t *testing.T) {
	e, c := readerEnv(t)
	base := e.api.URL + "/api/v1/vehicles/" + car

	resp, body := get(t, noFollow(), base+"/trips.csv", c)
	if resp.status != http.StatusOK || resp.header.Get("Content-Type") != "text/csv; charset=utf-8" ||
		resp.header.Get("Content-Disposition") != `attachment; filename="runsten-trips-2026-09-28.csv"` ||
		resp.header.Get("Cache-Control") != "no-store" {
		t.Errorf("trips: %d %v", resp.status, resp.header)
	}
	if want := csvLines(tripsCSVHeader, tripMorning, tripReconstructed, tripEvening); body != want {
		t.Errorf("trips:\n%s\nwant:\n%s", body, want)
	}
	resp, body = get(t, noFollow(), base+"/charges.csv", c)
	if resp.status != http.StatusOK || resp.header.Get("Content-Disposition") != `attachment; filename="runsten-charges-2026-09-28.csv"` {
		t.Errorf("charges: %d %v", resp.status, resp.header)
	}
	if want := csvLines(chargesCSVHeader, chargeNight, chargeEvening); body != want {
		t.Errorf("charges:\n%s\nwant:\n%s", body, want)
	}

	for query, want := range map[string]string{
		// As the lists: an event that may have happened at least partly in the period.
		"?from=2026-09-28T12:30:00Z":                                csvLines(tripsCSVHeader, tripReconstructed, tripEvening),
		"?to=2026-09-28T12:00:00Z":                                  csvLines(tripsCSVHeader, tripMorning),
		"?from=2026-09-28T14:00:00Z&to=2026-09-28T16:00:00Z":        csvLines(tripsCSVHeader),
		"?from=2026-09-28T10:00:00%2B02:00&to=2026-09-29T00:00:00Z": csvLines(tripsCSVHeader, tripReconstructed, tripEvening),
	} {
		if resp, body := get(t, noFollow(), base+"/trips.csv"+query, c); resp.status != http.StatusOK || body != want {
			t.Errorf("trips%s: %d\n%s\nwant:\n%s", query, resp.status, body, want)
		}
	}

	t.Run("limits", func(t *testing.T) {
		l := &historyLimit{account: account, from: h(12, 0), noCosts: true}
		e.server.Limits = l
		defer func() { e.server.Limits = nil }()
		if _, body := get(t, noFollow(), base+"/trips.csv", c); body != csvLines(tripsCSVHeader, tripReconstructed, tripEvening) {
			t.Errorf("trips before the history: %s", body)
		}
		if _, body := get(t, noFollow(), base+"/trips.csv?to=2026-09-28T11:00:00Z", c); body != csvLines(tripsCSVHeader) {
			t.Errorf("a period before the history: %s", body)
		}
		if _, body := get(t, noFollow(), base+"/charges.csv", c); body != csvLines(chargesCSVHeader, chargeEveningNoCost) {
			t.Errorf("charges without costs: %s", body)
		}
		l.noCSV = true
		for _, kind := range []string{"trips", "charges"} {
			resp, body := get(t, noFollow(), base+"/"+kind+".csv", c)
			if resp.status != http.StatusForbidden {
				t.Errorf("%s without CSV: %d", kind, resp.status)
			}
			wantError(t, body, codeFeatureUnavailable)
		}
		if resp, _ := get(t, noFollow(), base+"/trips.csv", e.session(t, "other")); resp.status != http.StatusNotFound {
			t.Errorf("another account, unlimited, asking for this one's vehicle: %d", resp.status)
		}
		l.noCSV, l.err = false, errors.New("boom")
		resp, body := get(t, noFollow(), base+"/trips.csv", c)
		if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
			t.Errorf("limits not read: %d %s", resp.status, body)
		}
	})

	for _, kind := range []string{"trips", "charges"} {
		u := base + "/" + kind + ".csv"
		resp, body := get(t, noFollow(), u+"?from=2026-09-29T00:00:00Z&to=2026-09-28T00:00:00Z", c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s, to before from: %d", kind, resp.status)
		}
		wantError(t, body, codeInvalidParameter)
		if resp, _ := get(t, noFollow(), u+"?from=yesterday", c); resp.status != http.StatusBadRequest {
			t.Errorf("%s, from not a time: %d", kind, resp.status)
		}
		if resp, _ := get(t, noFollow(), u); resp.status != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d", kind, resp.status)
		}
		resp, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+foreign+"/"+kind+".csv", c)
		if resp.status != http.StatusNotFound {
			t.Errorf("%s of another account's vehicle: %d", kind, resp.status)
		}
		wantError(t, body, codeNotFound)
	}

	e.reader.err = errors.New("boom")
	for _, kind := range []string{"trips", "charges"} {
		resp, body := get(t, noFollow(), base+"/"+kind+".csv", c)
		if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
			t.Errorf("%s not read: %d %s", kind, resp.status, body)
		}
	}
}

// TestCSVCells: a text a spreadsheet would run as a formula is kept a text, and amounts
// are written with the currency's digits.
func TestCSVCells(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "Maison": "Maison", "=HYPERLINK(\"x\")": "'=HYPERLINK(\"x\")", "+33": "'+33", "-1": "'-1",
		"@SUM(A1)": "'@SUM(A1)", "\tx": "'\tx", "a=b": "a=b",
	} {
		if got := cell(in); got != want {
			t.Errorf("cell(%q) = %q, want %q", in, got, want)
		}
	}
	for _, tc := range []struct {
		minor  int64
		digits int
		want   string
	}{
		{524, 2, "5.24"}, {5, 2, "0.05"}, {0, 2, "0.00"}, {100, 2, "1.00"}, {1500, 0, "1500"}, {7, 3, "0.007"}, {12345, 3, "12.345"},
	} {
		if got := major(tc.minor, tc.digits); got != tc.want {
			t.Errorf("major(%d, %d) = %q, want %q", tc.minor, tc.digits, got, tc.want)
		}
	}
}
