package pleos

import (
	"encoding/json"
	"fmt"
	"time"

	"runsten/internal/core"
)

// NormalizeHealth converts a raw response of endpoint ep into a health reading; an
// endpoint that tells nothing of it gives an empty one.
//
// The status endpoint tells the tyres and the warnings: the tyres are raw values the
// API labels with their unit, and the warnings a list of everything that warns. An
// entry absent from a received list warns of nothing; a list the response does not
// hold tells nothing, so the reading replaces no check. The service is left out: the
// API has no word of it.
func NormalizeHealth(ep Endpoint, raw []byte) (core.Health, error) {
	if ep != Status {
		return core.Health{}, nil
	}
	var v struct {
		Data struct {
			Timestamp string `json:"timestamp"`
			Tire      [2]struct {
				Left  *float64 `json:"left"`
				Right *float64 `json:"right"`
				Unit  string   `json:"unit"`
			} `json:"tire"`
			Warnings []string `json:"warnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return core.Health{}, fmt.Errorf("normalize health %s: %w", ep, err)
	}
	warned := v.Data.Warnings != nil
	if len(v.Data.Tire) == 0 && !warned {
		return core.Health{}, nil
	}
	at := time.Time{}
	if t, err := time.Parse(time.RFC3339, v.Data.Timestamp); err == nil {
		at = t.UTC()
	}
	var h core.Health

	// The pressures, raw values of 0 to 254 (255 invalid), per axle with a side each,
	// their unit the API's words. Assumption (ADR 0049 §5), to check on a real car
	// (§10.9): psi is read as told, kpa holds steps of five, bar tenths. The
	// recommended pressures the API does not give.
	tyres := map[core.Wheel]core.Tyre{}
	for axle, row := range v.Data.Tire {
		wheels := []core.Wheel{core.FrontLeft, core.FrontRight}
		if axle == 1 {
			wheels = []core.Wheel{core.RearLeft, core.RearRight}
		}
		raws := []*float64{row.Left, row.Right}
		for i, w := range wheels {
			if raws[i] == nil || *raws[i] >= 255 {
				continue
			}
			t := tyres[w]
			t.PressureKPa = core.Some(pressure(*raws[i], row.Unit), at)
			tyres[w] = t
		}
	}

	// The words a warning list holds that tell of the tyres.
	tyreWords := map[string][]core.Wheel{
		"tirePressureFrontLeft":  {core.FrontLeft},
		"tirePressureFrontRight": {core.FrontRight},
		"tirePressureRearLeft":   {core.RearLeft},
		"tirePressureRearRight":  {core.RearRight},
		// The whole car's, when the per-wheel monitoring is not supported (assumption:
		// it stands for all four).
		"tirePressureLow": {core.FrontLeft, core.FrontRight, core.RearLeft, core.RearRight},
	}
	if warned {
		for w := range tyres {
			t := tyres[w]
			t.Warning = core.Some(core.TyreOK, at)
			tyres[w] = t
		}
		for _, word := range v.Data.Warnings {
			for _, w := range tyreWords[word] {
				t := tyres[w]
				t.Warning = core.Some(core.TyreLow, at)
				tyres[w] = t
			}
		}
	}
	if len(tyres) > 0 {
		h.Covers |= core.HealthTyres
		h.Tyres = tyres
	}

	// The words the doc lists that read of a check (a light, a fluid). Its words this
	// leaves out, lowFuel, smartKeyBattery and engineOil, tell nothing of a check it
	// maps (ADR 0049 §5); one it adds later is left out too, keeping the previous value
	// rather than taken for a warning of anything.
	if warned {
		h.Checks = map[core.Check]core.Value[bool]{}
		for _, c := range checkWords {
			h.Checks[c.check] = core.Some(false, at)
		}
		for _, word := range v.Data.Warnings {
			for _, c := range checkWords {
				if c.word != word {
					continue
				}
				// Assumption: frontHeadLamp covers both low beams; no side is told.
				if c.check == core.CheckLightLowBeamLeft {
					h.Checks[core.CheckLightLowBeamRight] = core.Some(true, at)
				}
				h.Checks[c.check] = core.Some(true, at)
			}
		}
	}
	return h, nil
}

// checkWord is a word of the warnings list, and the check it maps to.
type checkWord struct {
	word  string
	check core.Check
}

var checkWords = []checkWord{
	{"washFluid", core.CheckWasherFluid},
	{"brakeOil", core.CheckBrakeFluid},
	{"frontHeadLamp", core.CheckLightLowBeamLeft},
	{"frontLeftTurnSignal", core.CheckLightTurnFrontLeft},
	{"frontRightTurnSignal", core.CheckLightTurnFrontRight},
	{"rearLeftStopLamp", core.CheckLightBrakeLeft},
	{"rearRightStopLamp", core.CheckLightBrakeRight},
	{"rearLeftTurnSignal", core.CheckLightTurnRearLeft},
	{"rearRightTurnSignal", core.CheckLightTurnRearRight},
}

// kPaPerPSI converts a pressure in psi into kPa.
const kPaPerPSI = 6.894757

// pressure converts the raw value of a tyre in its unit into kPa (assumption,
// ADR 0049 §5): psi is read as told, kpa holds steps of five, bar tenths (ten kPa
// each).
func pressure(raw float64, unit string) float64 {
	switch unit {
	case "kpa":
		return raw * 5
	case "bar":
		return raw * 10
	}
	return raw * kPaPerPSI // psi, and a unit the API does not name
}
