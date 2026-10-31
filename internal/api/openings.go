package api

import (
	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// openingJSON is whether a door, a window or a lid is open.
type openingJSON string

// Schema lists the openings' states.
func (openingJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.Closed, core.Ajar, core.Open)
}

type doorsJSON struct {
	Locked     *bool             `json:"locked" doc:"The central lock; null when the vehicle does not tell it."`
	FrontLeft  null[openingJSON] `json:"front_left" doc:"The front left door: closed, ajar (not fully shut) or open; null when the vehicle does not tell it."`
	FrontRight null[openingJSON] `json:"front_right" doc:"The front right door, as front_left."`
	RearLeft   null[openingJSON] `json:"rear_left" doc:"The rear left door, as front_left."`
	RearRight  null[openingJSON] `json:"rear_right" doc:"The rear right door, as front_left."`
	Hood       null[openingJSON] `json:"hood" doc:"The front lid, a front trunk's on an electric car, as front_left."`
	Tailgate   null[openingJSON] `json:"tailgate" doc:"The tailgate or rear trunk, as front_left."`
	TankLid    null[openingJSON] `json:"tank_lid" doc:"The fuel filler's lid, as front_left."`
	Sentry     *bool             `json:"sentry" doc:"The vehicle watches its surroundings with its cameras while parked (Tesla's Sentry Mode); null for a vehicle that does not tell it."`
}

type windowsJSON struct {
	FrontLeft  null[openingJSON] `json:"front_left" doc:"The front left window: closed, ajar (open a crack) or open; null when the vehicle does not tell it."`
	FrontRight null[openingJSON] `json:"front_right" doc:"The front right window, as front_left."`
	RearLeft   null[openingJSON] `json:"rear_left" doc:"The rear left window, as front_left."`
	RearRight  null[openingJSON] `json:"rear_right" doc:"The rear right window, as front_left."`
	Sunroof    null[openingJSON] `json:"sunroof" doc:"The sunroof, as front_left; null for a vehicle without one, or that does not tell it."`
}

func opening(v core.Value[core.Opening]) null[openingJSON] {
	return nullOf(v, text[core.Opening, openingJSON])
}

func doorsOf(d core.Doors) doorsJSON {
	return doorsJSON{
		Locked:    opt(d.Locked, same),
		FrontLeft: opening(d.FrontLeft), FrontRight: opening(d.FrontRight),
		RearLeft: opening(d.RearLeft), RearRight: opening(d.RearRight),
		Hood: opening(d.Hood), Tailgate: opening(d.Tailgate), TankLid: opening(d.TankLid),
		Sentry: opt(d.Sentry, same),
	}
}

func windowsOf(w core.Windows) windowsJSON {
	return windowsJSON{
		FrontLeft: opening(w.FrontLeft), FrontRight: opening(w.FrontRight),
		RearLeft: opening(w.RearLeft), RearRight: opening(w.RearRight), Sunroof: opening(w.Sunroof),
	}
}
