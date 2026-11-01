package api

import (
	"encoding/json"
	"testing"
	"time"

	"runsten/internal/core"
)

// TestStateChargeDetails: a charge's current, voltage, phases, limit, time left and
// charger, each a reading; a value the source leaves out is null, as is every one for a
// source without them.
func TestStateChargeDetails(t *testing.T) {
	e, c := readerEnv(t)
	snap := core.Snapshot{
		Covers:   core.FieldChargeDetails,
		CurrentA: core.Some(16.0, h(4, 0)), VoltageV: core.Some(229.0, h(4, 0)), Phases: core.Some(3, h(4, 0)),
		CurrentLimitA: core.Some(16.0, h(4, 0)),
		TimeLeft:      core.Some(core.TimeLeft{Duration: 95 * time.Minute, To: core.ToTarget}, h(4, 0)),
		Charger:       core.Some(core.Charger{Cable: "IEC"}, h(4, 0)),
	}
	e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: snap}}
	_, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/state", c)
	golden(t, "state-charge-details.json", body)
	var st struct {
		Charging map[string]json.RawMessage `json:"charging"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	reading := func(v string) string {
		return `{"value":` + v + `,"reported_at":"2026-09-28T04:00:00Z","fetched_at":"2026-09-28T05:00:00Z","checked_at":"2026-09-28T05:30:00Z"}`
	}
	for k, want := range map[string]string{
		"current_a": reading("16"), "voltage_v": reading("229"), "phases": reading("3"), "current_limit_a": reading("16"),
		"time_left": reading(`{"duration_s":5700,"to":"target"}`),
		"charger":   reading(`{"brand":null,"type":null,"cable":"IEC"}`),
	} {
		if got := string(st.Charging[k]); got != want {
			t.Errorf("%s = %s, want %s", k, got, want)
		}
	}

	// A source without them.
	e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: core.Snapshot{
		Covers: core.FieldOdometer, OdometerKm: core.Some(12400.0, h(4, 0)),
	}}}
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/state", c)
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"current_a", "voltage_v", "phases", "current_limit_a", "time_left", "charger"} {
		if v := string(st.Charging[k]); v != "null" {
			t.Errorf("%s = %s, want null", k, v)
		}
	}
}
