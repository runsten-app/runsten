package api

import (
	"encoding/json"
	"testing"

	"runsten/internal/core"
)

// TestStateClimate: a Tesla's temperatures and climate, each value a reading; a value the
// source leaves out is null, as is every one for a source without them.
func TestStateClimate(t *testing.T) {
	e, c := readerEnv(t)
	snap := core.Snapshot{
		Covers: core.FieldClimate, OutsideTempC: core.Some(-3.5, h(4, 0)), CabinTempC: core.Some(19.5, h(4, 0)),
		Climate: core.Some(core.Climate{
			On: core.Some(true, h(4, 0)), Preconditioning: core.Some(true, h(4, 0)),
		}, h(4, 0)),
	}
	e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: snap}}
	_, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/state", c)
	golden(t, "state-climate.json", body)
	var st struct {
		Climate map[string]json.RawMessage `json:"climate"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	reading := func(v string) string {
		return `{"value":` + v + `,"reported_at":"2026-09-28T04:00:00Z","fetched_at":"2026-09-28T05:00:00Z","checked_at":"2026-09-28T05:30:00Z"}`
	}
	for k, want := range map[string]string{
		"outside_temp_c": reading("-3.5"), "cabin_temp_c": reading("19.5"),
		"on": reading("true"), "preconditioning": reading("true"), "battery_heater": "null",
	} {
		if got := string(st.Climate[k]); got != want {
			t.Errorf("%s = %s, want %s", k, got, want)
		}
	}

	// A source without a climate.
	e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: core.Snapshot{
		Covers: core.FieldOdometer, OdometerKm: core.Some(12400.0, h(4, 0)),
	}}}
	_, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/state", c)
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	for k, v := range st.Climate {
		if string(v) != "null" {
			t.Errorf("%s = %s, want null", k, v)
		}
	}
}
