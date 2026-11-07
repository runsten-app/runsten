package derive

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"runsten/internal/bmw"
	"runsten/internal/core"
	"runsten/internal/volvo"
)

func TestSessions(t *testing.T) {
	st := newMemStore()
	kind := core.Kind{Source: bmw.Source, Endpoint: string(bmw.ChargingHistory)}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	session := func(start int64, kwh string) string {
		return fmt.Sprintf(`{"startTime": %d, "endTime": %d, "energyConsumedFromPowerGridKwh": %s}`, start, start+3600, kwh)
	}
	v := st.vehicle("v")
	v.rows[kind] = []Snapshot{
		{Kind: kind, FetchedAt: day, CheckedAt: day, Payload: []byte(`{"data": [` + session(1790000000, "10") + `]}`)},
		// The next day's: the same session corrected, and a new one.
		{Kind: kind, FetchedAt: day.AddDate(0, 0, 1), CheckedAt: day.AddDate(0, 0, 1), Payload: []byte(`{"data": [` +
			session(1790100000, "7") + `, ` + session(1790000000, "11") + `]}`)},
	}
	d := New(st, Sources(volvo.Normalizer{}, bmw.Normalizer{}), core.DefaultParams(), nil, quiet)
	if _, err := d.Rebuild(context.Background(), "a", "v"); err != nil {
		t.Fatal(err)
	}
	got := v.sessions[bmw.Source]
	if len(got) != 2 {
		t.Fatalf("sessions: %+v", got)
	}
	for start, kwh := range map[int64]float64{1790000000: 11, 1790100000: 7} {
		if s := got[time.Unix(start, 0).UTC()]; !s.EnergyGridKWh.OK || s.EnergyGridKWh.V != kwh {
			t.Errorf("session at %d: %+v, want %v kWh", start, s, kwh)
		}
	}

	// A normalizer of no session records none.
	st2 := newMemStore()
	st2.vehicle("v").rows[kind] = v.rows[kind]
	if _, err := New(st2, volvo.Normalizer{}, core.DefaultParams(), nil, quiet).Rebuild(context.Background(), "a", "v"); err != nil {
		t.Fatal(err)
	}
	if st2.vehicle("v").sessions != nil {
		t.Error("sessions recorded without a SessionNormalizer")
	}
}

// TestSessionsFailFirst: sessions that cannot be saved leave the events and the cursor
// as they were, so that the next update reads the same responses again.
func TestSessionsFailFirst(t *testing.T) {
	boom := errors.New("boom")
	kind := core.Kind{Source: bmw.Source, Endpoint: string(bmw.ChargingHistory)}
	st := &scriptedStore{sessionsErr: boom, snaps: []Snapshot{{Kind: kind, Payload: []byte(`{"data": []}`)}}}
	d := New(st, Sources(volvo.Normalizer{}, bmw.Normalizer{}), core.DefaultParams(), nil, quiet)
	if err := d.Update(context.Background(), "a", "v"); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	if st.saved != nil {
		t.Error("events saved before the sessions")
	}
}
