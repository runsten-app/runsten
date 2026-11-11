package pleos

import (
	"testing"
	"time"

	"runsten/internal/core"
)

var sentAt = time.Date(2026, 11, 16, 22, 18, 41, 0, time.UTC)

// status is a response of the status endpoint, sent at sent, with the fields given
// (the tyres and the warnings of its data object).
func status(fields string) []byte {
	return []byte(`{"data":{"timestamp":"2026-11-16T22:18:41Z",` + fields + `},
		"meta":{"code":200}}`)
}

// TestNormalizeHealthTyres converts the raw pressure of each unit the API names into
// kPa (the assumptions of ADR 0049 §5, to check on a real car: psi as told, kpa in
// steps of five, bar in tenths) and leaves the invalid marker out.
func TestNormalizeHealthTyres(t *testing.T) {
	for _, test := range []struct {
		name  string
		left  string // the raw value of the front left, the other three held at 40
		unit  string
		want  float64
		valid bool
	}{
		{"psi", "40", "psi", 40 * kPaPerPSI, true},
		{"kpa", "50", "kpa", 250, true},
		{"bar", "25", "bar", 250, true},
		{"unnamed", "40", "", 40 * kPaPerPSI, true},
		{"invalid", "255", "psi", 0, false},
	} {
		h, err := NormalizeHealth(Status, status(`"tire":[{"left":`+test.left+`,"right":40,"unit":"`+test.unit+`"},
			{"left":40,"right":40,"unit":"`+test.unit+`"}]`))
		if err != nil {
			t.Fatal(err)
		}
		if h.Covers&core.HealthTyres == 0 {
			t.Fatalf("%s: no tyres: %+v", test.name, h)
		}
		// The invalid marker told nothing of its wheel: three tyres remain, the
		// rear's own axle converts regardless.
		if want := map[bool]int{true: 4, false: 3}[test.valid]; len(h.Tyres) != want {
			t.Errorf("%s: %d tyres, want %d", test.name, len(h.Tyres), want)
		}
		p := h.Tyres[core.FrontLeft].PressureKPa
		switch {
		case !test.valid && p.OK:
			t.Errorf("%s: the invalid marker told: %v", test.name, p.V)
		case test.valid && !p.OK:
			t.Errorf("%s: no pressure told", test.name)
		case test.valid && p.V != test.want:
			t.Errorf("%s: %v kPa, want %v", test.name, p.V, test.want)
		}
		if test.valid && p.OK && !p.At.Equal(sentAt) {
			t.Errorf("%s: dated %v, want %v", test.name, p.At, sentAt)
		}
		// The rear axle is its own row, its raw of 40 converted by the same unit as
		// the front's: the front left's invalid marker told nothing of it.
		rr := h.Tyres[core.RearRight].PressureKPa
		if !rr.OK {
			t.Errorf("%s: the rear's pressure told of nothing", test.name)
		} else if rr.V != pressure(40, test.unit) {
			t.Errorf("%s: the rear's %v kPa, want %v", test.name, rr.V, pressure(40, test.unit))
		}
	}
}

// TestNormalizeHealthWarnings reads the warnings a list holds, an entry absent from a
// received list warning of nothing, a missing list telling nothing.
func TestNormalizeHealthWarnings(t *testing.T) {
	h, err := NormalizeHealth(Status, status(`"tire":[{"left":40,"right":40,"unit":"psi"},
		{"left":40,"right":40,"unit":"psi"}],"warnings":["washFluid","tirePressureFrontLeft","frontHeadLamp"]`))
	if err != nil {
		t.Fatal(err)
	}
	if h.Covers&core.HealthTyres == 0 {
		t.Fatal("no tyres")
	}
	if w := h.Tyres[core.FrontLeft].Warning; !w.OK || w.V != core.TyreLow {
		t.Errorf("the front left warns: %v", w)
	}
	if w := h.Tyres[core.FrontRight].Warning; !w.OK || w.V != core.TyreOK {
		t.Errorf("the front right warns too: %v", w)
	}
	if !h.Checks[core.CheckWasherFluid].OK || !h.Checks[core.CheckWasherFluid].V {
		t.Error("the washer fluid warns of nothing")
	}
	if !h.Checks[core.CheckBrakeFluid].OK || h.Checks[core.CheckBrakeFluid].V {
		t.Error("a check absent from the list warns")
	}
	// headlamps: assumed both low beams.
	for _, c := range []core.Check{core.CheckLightLowBeamLeft, core.CheckLightLowBeamRight} {
		if !h.Checks[c].OK || !h.Checks[c].V {
			t.Errorf("the low beam does not warn: %v", c)
		}
	}

	// The whole car's, when the per-wheel monitoring is not supported (assumption:
	// it stands for all four).
	h, err = NormalizeHealth(Status, status(`"warnings":["tirePressureLow"]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range core.Wheels() {
		if warn := h.Tyres[w].Warning; !warn.OK || warn.V != core.TyreLow {
			t.Errorf("%s: %v", w, warn)
		}
	}
	if len(h.Tyres) != 4 {
		t.Errorf("the whole car's warning made %d tyres", len(h.Tyres))
	}
	// A list without a word for the tyres: all four are fine.
	h, err = NormalizeHealth(Status, status(`"tire":[{"left":40,"right":40},{"left":40,"right":40}],"warnings":[]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range core.Wheels() {
		if warn := h.Tyres[w].Warning; !warn.OK || warn.V != core.TyreOK {
			t.Errorf("%s: %v", w, warn)
		}
	}
	// A missing list tells nothing: the current warnings keep their value.
	h, err = NormalizeHealth(Status, status(`"tire":[{"left":40,"right":40,"unit":"psi"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if warn := h.Tyres[core.FrontLeft].Warning; warn.OK {
		t.Errorf("a missing list told the front left: %+v", warn)
	}
	// What the API names and Runsten does not read tells of nothing (lowFuel,
	// smartKeyBattery, engineOil and a word it may add later).
	h, err = NormalizeHealth(Status, status(`"warnings":["lowFuel","smartKeyBattery","engineOil","whatIsNext"]`))
	if err != nil {
		t.Fatal(err)
	}
	// The known checks stay none, and none warns.
	if len(h.Checks) != len(checkWords) {
		t.Errorf("the list made %d checks, want the %d known ones", len(h.Checks), len(checkWords))
	}
	for c, v := range h.Checks {
		if v.OK && v.V {
			t.Errorf("%s warns of nothing", c)
		}
	}
	// The service is left out: the API has no word of it.
	if h.Covers&core.HealthService != 0 {
		t.Error("the service told")
	}
}
