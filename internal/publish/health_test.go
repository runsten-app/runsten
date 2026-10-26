package publish

import (
	"testing"
	"time"

	"runsten/internal/core"
)

func TestHealthAggregates(t *testing.T) {
	at := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	tyres := func(ws ...core.TyreWarning) core.CurrentHealth {
		h := core.CurrentHealth{Health: core.Health{Covers: core.HealthTyres, Tyres: map[core.Wheel]core.Tyre{}}}
		for i, w := range ws {
			h.Tyres[core.Wheels()[i]] = core.Tyre{Warning: core.Some(w, at)}
		}
		return h
	}
	lights := func(vs ...core.Value[bool]) core.CurrentHealth {
		h := core.CurrentHealth{Health: core.Health{Checks: map[core.Check]core.Value[bool]{
			core.CheckOil: core.Some(true, at), // not a light
		}}}
		for i, v := range vs {
			h.Checks[core.Checks()[5+i]] = v
		}
		return h
	}
	for _, tt := range []struct {
		name         string
		h            core.CurrentHealth
		tyre, lights string
	}{
		{"nothing known", core.CurrentHealth{}, "", ""},
		{"four tyres fine", tyres(core.TyreOK, core.TyreOK, core.TyreOK, core.TyreOK), "ok", ""},
		// Three known fine, one unknown: not fine.
		{"a tyre unknown", tyres(core.TyreOK, core.TyreOK, core.TyreOK), "", ""},
		// A known fault is told whatever the others.
		{"a tyre low, others unknown", tyres(core.TyreOK, core.TyreLow), "low", ""},
		{"the worst", tyres(core.TyreHigh, core.TyreVeryLow, core.TyreFault, core.TyreLow), "very_low", ""},
		{"lights fine", lights(core.Some(false, at), core.Some(false, at)), "", "ok"},
		{"a light unknown", lights(core.Some(false, at), core.Value[bool]{}), "", ""},
		{"a light fails", lights(core.Value[bool]{}, core.Some(true, at)), "", "warning"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tyreWarning(tt.h); got != tt.tyre {
				t.Errorf("tyres %q, want %q", got, tt.tyre)
			}
			if got := lightsWarning(tt.h); got != tt.lights {
				t.Errorf("lights %q, want %q", got, tt.lights)
			}
		})
	}
	// The fluids one by one: unknown is empty.
	msgs := Health(broker(true), vehicle, lights(core.Some(false, at)))
	got := map[string]string{}
	for _, m := range msgs {
		got[m.Topic[len("runsten/vehicles/"+vehicle+"/"):]] = m.Payload
	}
	if got["warning_oil"] != "warning" || got["warning_coolant"] != "" || got["warning_lights"] != "ok" || len(msgs) != 15 {
		t.Errorf("%d messages: %v", len(msgs), got)
	}
}
