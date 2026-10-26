package tesla

import (
	"fmt"
	"time"

	"runsten/internal/core"
)

// kPaPerBar converts the tyre pressures, in bar whatever units the car displays.
const kPaPerBar = 100

// NormalizeHealth converts a raw response of endpoint e into a health reading: the
// tyres, from vehicle_state. The Owner API tells neither the service nor the fluids nor
// the lights: they are left unknown.
func NormalizeHealth(e Endpoint, raw []byte) (core.Health, error) {
	if e != VehicleData {
		return core.Health{}, nil
	}
	v, err := parseVehicleData(raw)
	if err != nil {
		return core.Health{}, fmt.Errorf("normalize health %s: %w", e, err)
	}
	if v.Response == nil || v.Response.VehicleState == nil {
		return core.Health{}, nil
	}
	x := v.Response.VehicleState
	at := millis(x.Timestamp)
	tyres := map[core.Wheel]core.Tyre{}
	for w, t := range map[core.Wheel]struct {
		bar        *float64
		seen       *int64
		soft, hard *bool
	}{
		core.FrontLeft: {x.PressureFL, x.SeenFL, x.SoftFL, x.HardFL}, core.FrontRight: {x.PressureFR, x.SeenFR, x.SoftFR, x.HardFR},
		core.RearLeft: {x.PressureRL, x.SeenRL, x.SoftRL, x.HardRL}, core.RearRight: {x.PressureRR, x.SeenRR, x.SoftRR, x.HardRR},
	} {
		// A pressure is dated by when its sensor told it, which may be days old.
		seen := seconds(t.seen)
		if seen.IsZero() {
			seen = at
		}
		tyre := core.Tyre{PressureKPa: scaled(t.bar, kPaPerBar, seen), Warning: tyreWarning(t.soft, t.hard, at)}
		if tyre.PressureKPa.OK || tyre.Warning.OK {
			tyres[w] = tyre
		}
	}
	var h core.Health
	// A response without any tyre field does not report them: they stay unknown rather
	// than covered with nothing.
	if len(tyres) > 0 || x.RecommendedFront != nil || x.RecommendedRear != nil {
		h.Covers = core.HealthTyres
		h.Tyres = tyres
		h.RecommendedFrontKPa = scaled(x.RecommendedFront, kPaPerBar, at)
		h.RecommendedRearKPa = scaled(x.RecommendedRear, kPaPerBar, at)
	}
	return h, nil
}

// tyreWarning reads a tyre's two warnings. Assumption: the soft one is a low pressure,
// the hard one a very low pressure (or a puncture), as the car's own alerts tell them;
// neither seen set on a real car. Without both, unknown.
func tyreWarning(soft, hard *bool, at time.Time) core.Value[core.TyreWarning] {
	switch {
	case hard != nil && *hard:
		return core.Some(core.TyreVeryLow, at)
	case soft != nil && *soft:
		return core.Some(core.TyreLow, at)
	case hard != nil && soft != nil:
		return core.Some(core.TyreOK, at)
	}
	return core.Value[core.TyreWarning]{}
}
