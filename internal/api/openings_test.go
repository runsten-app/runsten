package api

import (
	"encoding/json"
	"testing"

	"runsten/internal/core"
)

// TestStateOpenings: the doors and windows of a vehicle unlocked, watching, a door ajar
// and a window open, each told by its own response; what the vehicle does not tell is
// null.
func TestStateOpenings(t *testing.T) {
	e, c := readerEnv(t)
	closed := core.Some(core.Closed, h(3, 0))
	doors := core.Snapshot{Covers: core.FieldDoors, Doors: core.Some(core.Doors{
		Locked:    core.Some(false, h(3, 0)),
		FrontLeft: core.Some(core.Ajar, h(3, 0)), FrontRight: closed, RearLeft: closed, RearRight: closed,
		Hood: closed, Tailgate: closed, Sentry: core.Some(true, h(3, 0)),
	}, h(3, 0))}
	windows := core.Snapshot{Covers: core.FieldWindows, Windows: core.Some(core.Windows{
		FrontLeft: closed, FrontRight: closed, RearLeft: core.Some(core.Open, h(3, 0)), RearRight: closed,
	}, h(3, 0))}
	e.reader.current[car] = []core.Record{
		{FetchedAt: h(4, 0), CheckedAt: h(5, 0), Snapshot: doors},
		{FetchedAt: h(4, 30), CheckedAt: h(5, 30), Snapshot: windows},
	}
	_, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/state", c)
	golden(t, "state-openings.json", body)
	var st struct {
		Doors   json.RawMessage `json:"doors"`
		Windows json.RawMessage `json:"windows"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	if want := `{"value":{"locked":false,"front_left":"ajar","front_right":"closed","rear_left":"closed","rear_right":"closed",` +
		`"hood":"closed","tailgate":"closed","tank_lid":null,"sentry":true},` +
		`"reported_at":"2026-09-28T03:00:00Z","fetched_at":"2026-09-28T04:00:00Z","checked_at":"2026-09-28T05:00:00Z"}`; string(st.Doors) != want {
		t.Errorf("doors = %s\nwant    %s", st.Doors, want)
	}
	if want := `{"value":{"front_left":"closed","front_right":"closed","rear_left":"open","rear_right":"closed","sunroof":null},` +
		`"reported_at":"2026-09-28T03:00:00Z","fetched_at":"2026-09-28T04:30:00Z","checked_at":"2026-09-28T05:30:00Z"}`; string(st.Windows) != want {
		t.Errorf("windows = %s\nwant      %s", st.Windows, want)
	}
}
