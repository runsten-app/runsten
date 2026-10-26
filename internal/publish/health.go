package publish

import "runsten/internal/core"

// The words of the warnings' topics.
const (
	warningOn  = "warning"
	warningOff = "ok"
)

// Health is the vehicle's health, one retained message per value after State's, in the
// order of the contract; an unknown value is an empty message, as in State. The tyres'
// warning and the lights' are the worst of their parts: a vehicle reports many lights,
// whose details the web interface gives.
func Health(b Broker, vehicleID string, h core.CurrentHealth) []Message {
	base := b.TopicPrefix + "/vehicles/" + vehicleID + "/"
	type value struct{ name, payload string }
	var values []value
	for _, w := range core.Wheels() {
		values = append(values, value{"tyre_pressure_" + string(w) + "_kpa", text(h.Tyres[w].PressureKPa, number)})
	}
	values = append(values, []value{
		{"tyre_warning", tyreWarning(h)},
		{"service", text(h.Service, str[core.ServiceState])},
		{"service_days", text(h.DaysToService, number)},
		{"service_months", text(h.MonthsToService, number)},
		{"service_distance_km", text(h.DistanceToServiceKm, number)},
	}...)
	for _, c := range warningChecks {
		values = append(values, value{"warning_" + string(c), warning(h.Checks[c])})
	}
	values = append(values, value{"warning_lights", lightsWarning(h)})
	msgs := make([]Message, len(values))
	for i, v := range values {
		msgs[i] = Message{Topic: base + v.name, Payload: v.payload, Retained: true}
	}
	return msgs
}

// warningChecks are the checks published one by one: the fluids and the battery.
var warningChecks = []core.Check{
	core.CheckBrakeFluid, core.CheckCoolant, core.CheckOil, core.CheckWasherFluid, core.CheckLowVoltageBattery,
}

func warning(v core.Value[bool]) string {
	if !v.OK {
		return ""
	}
	if v.V {
		return warningOn
	}
	return warningOff
}

// tyreSeverity orders the tyres' warnings, the worst last.
var tyreSeverity = map[core.TyreWarning]int{
	core.TyreOK: 0, core.TyreHigh: 1, core.TyreFault: 2, core.TyreLow: 3, core.TyreVeryLow: 4,
}

// tyreWarning is the worst warning of the tyres; empty when none is known.
func tyreWarning(h core.CurrentHealth) string {
	worst, known := core.TyreOK, false
	for _, t := range h.Tyres {
		if t.Warning.OK {
			known = true
			if tyreSeverity[t.Warning.V] > tyreSeverity[worst] {
				worst = t.Warning.V
			}
		}
	}
	if !known {
		return ""
	}
	return string(worst)
}

// lightsWarning tells whether a light fails; empty when none is known.
func lightsWarning(h core.CurrentHealth) string {
	out := ""
	for c, v := range h.Checks {
		if c.Light() && v.OK {
			if v.V {
				return warningOn
			}
			out = warningOff
		}
	}
	return out
}
