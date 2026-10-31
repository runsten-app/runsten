package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"runsten/internal/core"
)

// TestStateSoftware: a Tesla's version, and the update waiting for it.
func TestStateSoftware(t *testing.T) {
	e, c := readerEnv(t)
	snap := core.Snapshot{
		Covers: core.FieldSoftware, Software: core.Some("2026.38.3.1", h(4, 0)),
		SoftwareUpdate: core.Some(core.SoftwareUpdate{
			Status: core.UpdateWaitingWiFi, Version: "2026.38.3.2",
			DownloadPct: core.Some(0.0, time.Time{}), InstallPct: core.Some(1.0, time.Time{}),
			Duration: core.Some(45*time.Minute, time.Time{}), ScheduledAt: core.Some(h(23, 50), time.Time{}),
		}, h(4, 0)),
	}
	e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: snap}}
	_, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/state", c)
	golden(t, "state-software.json", body)
	var st struct {
		Software       json.RawMessage `json:"software"`
		SoftwareUpdate json.RawMessage `json:"software_update"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	if want := `{"value":"2026.38.3.1","reported_at":"2026-09-28T04:00:00Z","fetched_at":"2026-09-28T05:00:00Z","checked_at":"2026-09-28T05:30:00Z"}`; string(st.Software) != want {
		t.Errorf("software = %s\nwant       %s", st.Software, want)
	}
	if want := `{"value":{"status":"waiting_wifi","version":"2026.38.3.2","download_pct":0,"install_pct":1,"duration_s":2700,` +
		`"scheduled_at":"2026-09-28T23:50:00Z"},` +
		`"reported_at":"2026-09-28T04:00:00Z","fetched_at":"2026-09-28T05:00:00Z","checked_at":"2026-09-28T05:30:00Z"}`; string(st.SoftwareUpdate) != want {
		t.Errorf("software_update = %s\nwant              %s", st.SoftwareUpdate, want)
	}

	// No update waiting, its version and progress untold.
	snap.SoftwareUpdate = core.Some(core.SoftwareUpdate{Status: core.UpdateUnknown}, h(4, 0))
	e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: snap}}
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/state", c)
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	if want := `{"status":"unknown","version":null,"download_pct":null,"install_pct":null,"duration_s":null,"scheduled_at":null}`; !json.Valid(st.SoftwareUpdate) ||
		string(st.SoftwareUpdate[len(`{"value":`):len(`{"value":`)+len(want)]) != want {
		t.Errorf("software_update = %s", st.SoftwareUpdate)
	}
}

func TestSoftwareVersionsJSON(t *testing.T) {
	e, c := readerEnv(t)
	e.reader.versions = map[string][]core.SoftwareVersion{car: {
		{DetectedAt: h(1, 0), Version: "2026.32.1", Installed: core.Bounds{Before: h(1, 0)}},
		{DetectedAt: h(3, 0), Version: "2026.38.3.1", Installed: core.Bounds{After: h(2, 30), Before: h(3, 0)}},
		{
			DetectedAt: h(5, 0), Version: "2026.38.3.2", Installed: core.Bounds{After: h(4, 0), Before: h(5, 0)},
			InstallStarted: core.Bounds{After: h(3, 30), Before: h(3, 31)},
		},
	}}
	base := e.api.URL + "/api/v1/vehicles/" + car + "/software-versions"
	_, body := get(t, noFollow(), base, c)
	golden(t, "software-versions.json", body)

	var p struct {
		Items []struct {
			ID        string `json:"id"`
			Version   string `json:"version"`
			Installed struct {
				After *time.Time `json:"after"`
			} `json:"installed"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	read := func(target string) {
		t.Helper()
		resp, body := get(t, noFollow(), target, c)
		if resp.status != http.StatusOK {
			t.Fatalf("%s: %d %s", target, resp.status, body)
		}
		p.Items, p.NextCursor = nil, nil
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
	}
	read(base)
	if len(p.Items) != 3 || p.Items[0].Version != "2026.38.3.2" || p.Items[2].Installed.After != nil || p.NextCursor != nil {
		t.Errorf("all: %+v", p)
	}
	// Paged as the trips.
	read(base + "?limit=2")
	if len(p.Items) != 2 || p.NextCursor == nil {
		t.Fatalf("first page: %+v", p)
	}
	read(base + "?limit=2&cursor=" + *p.NextCursor)
	if len(p.Items) != 1 || p.Items[0].Version != "2026.32.1" || p.NextCursor != nil {
		t.Errorf("second page: %+v", p)
	}
	// A cursor not given by the API, a vehicle of no one.
	if resp, body := get(t, noFollow(), base+"?cursor=nope", c); resp.status != http.StatusBadRequest {
		t.Errorf("bad cursor: %d %s", resp.status, body)
	}
	if resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/00000000-0000-0000-0000-000000000000/software-versions", c); resp.status != http.StatusNotFound {
		t.Errorf("unknown vehicle: %d %s", resp.status, body)
	}
	// A vehicle that tells none: an empty page.
	e.reader.versions = nil
	read(base)
	if len(p.Items) != 0 {
		t.Errorf("none: %+v", p)
	}
}
