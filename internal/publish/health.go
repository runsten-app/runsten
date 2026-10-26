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

// tyreWarning is the worst warning of the tyres: a known one whatever the others, ok
// only when the four are known, else empty (unknown), never fine for want of a reading.
func tyreWarning(h core.CurrentHealth) string {
	worst, known := core.TyreOK, 0
	for _, w := range core.Wheels() {
		if t := h.Tyres[w].Warning; t.OK {
			known++
			if tyreSeverity[t.V] > tyreSeverity[worst] {
				worst = t.V
			}
		}
	}
	if worst == core.TyreOK && known < len(core.Wheels()) {
		return ""
	}
	return string(worst)
}

// lightsWarning tells whether a light fails: ok only when every light the vehicle
// reports is known, else empty (unknown). Which lights a model has, the vehicle tells.
func lightsWarning(h core.CurrentHealth) string {
	reported, unknown := false, false
	for c, v := range h.Checks {
		if !c.Light() {
			continue
		}
		if v.OK && v.V {
			return warningOn
		}
		reported, unknown = true, unknown || !v.OK
	}
	if !reported || unknown {
		return ""
	}
	return warningOff
}
