package ford

import (
	"fmt"
	"strings"
	"time"

	"runsten/internal/core"
)

// wheels maps vehicleWheel to the four tyres read; a van's twin rear wheels and the
// system's own entry are left out.
var wheels = map[string]core.Wheel{
	"FRONT_LEFT": core.FrontLeft, "FRONT_RIGHT": core.FrontRight,
	"REAR_LEFT": core.RearLeft, "REAR_RIGHT": core.RearRight,
}

// tyreWarnings maps tirePressureStatus: NORMAL, LOW, and ALERT, which the specification
// does not explain (assumption: a warning other than a low pressure).
var tyreWarnings = map[string]core.TyreWarning{
	"NORMAL": core.TyreOK, "LOW": core.TyreLow, "ALERT": core.TyreFault,
}

// tyre is an entry of tirePressure or tirePressureStatus. A pressure may carry no send
// time of its own.
type tyre struct {
	At    time.Time `json:"updateTime"`
	Value any       `json:"value"`
	Wheel string    `json:"vehicleWheel"`
	// The pressures recommended at the front and at the rear, on a pressure's entry.
	PlacardFront *float64 `json:"wheelPlacardFront"`
	PlacardRear  *float64 `json:"wheelPlacardRear"`
}

// NormalizeHealth reads the tyres of a telemetry response: their pressures, in kPa
// (assumption: the unit other clients take them in, which the values seen fit), the
// pressures the car recommends, and what its monitoring tells of each. The response
// tells no service; its warning lights are not read yet.
func NormalizeHealth(raw []byte) (core.Health, error) {
	m, err := metricsOf(raw)
	if err != nil {
		return core.Health{}, fmt.Errorf("normalize health: %w", err)
	}
	pressures := list[tyre](m, "tirePressure")
	statuses := list[tyre](m, "tirePressureStatus")
	var h core.Health
	if pressures == nil && statuses == nil {
		return h, nil
	}
	h.Covers |= core.HealthTyres
	h.Tyres = map[core.Wheel]core.Tyre{}
	for _, e := range pressures {
		if e.PlacardFront != nil && *e.PlacardFront > 0 {
			h.RecommendedFrontKPa = core.Some(*e.PlacardFront, e.At)
		}
		if e.PlacardRear != nil && *e.PlacardRear > 0 {
			h.RecommendedRearKPa = core.Some(*e.PlacardRear, e.At)
		}
		w, ok := wheels[e.Wheel]
		kpa, isNum := e.Value.(float64)
		if !ok || !isNum || kpa <= 0 {
			continue
		}
		t := h.Tyres[w]
		t.PressureKPa = core.Some(kpa, e.At)
		h.Tyres[w] = t
	}
	for _, e := range statuses {
		w, ok := wheels[e.Wheel]
		word, isWord := e.Value.(string)
		warning, known := tyreWarnings[strings.ToUpper(word)]
		if !ok || !isWord || !known {
			continue
		}
		t := h.Tyres[w]
		t.Warning = core.Some(warning, e.At)
		h.Tyres[w] = t
	}
	return h, nil
}
