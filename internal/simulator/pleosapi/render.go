package pleosapi

import (
	"math"
	"strconv"
	"time"

	"runsten/internal/simulator/vehicle"
)

// dataset is the data of one status endpoint of one send of a car: what it uploaded of
// its group, dated from when it sent it. A group's send happens at each change, and
// every SendEvery while the car drives or charges; parked, the timestamp of a read may
// be hours old, as on the real API.
func dataset(name string, b Behavior, r vehicle.Report, active bool, now time.Time) map[string]any {
	switch name {
	case "batteries":
		at := sendAt(b, r.EnergyAt, active, now)
		return batteries(r, at)
	case "powertrains":
		return powertrains(r, sendAt(b, r.EnergyAt, active, now))
	case "locations":
		return locations(r, r.LocationAt)
	case "driving":
		return driving(r, sendAt(b, r.StatusAt, active, now))
	case "status":
		return status(r, sendAt(b, r.StatusAt, active, now))
	}
	return nil
}

func batteries(r vehicle.Report, at time.Time) map[string]any {
	e := r.Energy
	plugin := "disconnected"
	if e.PluggedIn {
		plugin = "connected"
	}
	charge := "notCharging"
	t := e.ChargeType
	switch e.Charging {
	case vehicle.Charging:
		charge = "charging" // "slow charging", the current an AC one draws
		if t == vehicle.DC {
			charge = "fastCharging"
		}
	case vehicle.Done:
		charge = "notCharging"
	case vehicle.Idle:
		charge = "notCharging"
	}
	minutes := 0
	if e.Charging == vehicle.Charging && e.ChargingPowerW > 0 {
		kwh := (e.TargetSoC - e.SoC) / 100 * r.Spec.UsableKWhAt(at)
		minutes = int(math.Ceil(kwh / (e.ChargingPowerW / 1000) * 60))
	}
	return map[string]any{
		"charge": map[string]any{
			"plugin":              plugin,
			"status":              charge,
			"charging":            e.Charging == vehicle.Charging,
			"stateOfCharge":       e.SoC,
			"batteryCapacity":     int(r.Spec.UsableKWhAt(at) * 3600), // kJ (assumption, ADR 0049 §5)
			"targetStateOfCharge": map[string]any{"standard": int(e.TargetSoC), "quick": int(e.TargetSoC)},
			"remainTime":          minutes,
		},
		"timestamp": timestamp(at),
	}
}

// batteries[].remainRatio reads whole percents on the doc's example; SoC is finer: kept
// as it is, the readings' resolution being untested on a real car.
func powertrains(r vehicle.Report, at time.Time) map[string]any {
	soc := r.Energy.SoC
	return map[string]any{
		"engine":    map[string]any{"fuelLevel": nil}, // null: the doc's answer for an EV
		"batteries": []map[string]any{{"type": "main", "remainRatio": soc}},
		"distanceToEmpties": []map[string]any{{
			"type": "EV", "value": strconv.Itoa(electricRange(r.Spec, at, soc)), "unit": "km",
		}},
		"timestamp": timestamp(at),
	}
}

// electricRange is the range the car forecasts at the SoC's instant, over the usable
// capacity the SoC runs over. The API gives it as a string, 1 to 4095.
func electricRange(s vehicle.Spec, at time.Time, soc float64) int {
	if s.ConsumptionKWhPer100km <= 0 {
		return 1
	}
	return max(1, int(soc/100*s.UsableKWhAt(at)/s.ConsumptionKWhPer100km*100))
}

func locations(r vehicle.Report, at time.Time) map[string]any {
	p := r.Location
	return map[string]any{
		"location": map[string]any{
			"latitude":  p.Lat,
			"longitude": p.Lon,
			"altitude":  10,
			"heading":   int(p.Heading) % 360,
			"speed":     map[string]any{"value": int(r.Status.SpeedKmh), "unit": "kph"},
			"type":      0, // WGS84; Bessel and CoordShift are not read
		},
		"timestamp": timestamp(at.Truncate(time.Second)),
	}
}

func driving(r vehicle.Report, at time.Time) map[string]any {
	s := r.Status
	on := "off"
	if s.Driving {
		on = "on"
	}
	// Sleep mode while charging is one of the unknowns (ADR 0049 §10.2): assumed off,
	// the car talking to the charger.
	sleep := "on"
	if s.Driving || r.Energy.Charging == vehicle.Charging {
		sleep = "off"
	}
	details := []map[string]string{}
	for _, t := range []string{"ign1", "ign3", "acc"} {
		details = append(details, map[string]string{"type": t, "status": on}) // assumed in step
	}
	return map[string]any{
		"ignition":     map[string]any{"status": on, "ignitionDetails": details},
		"sleepMode":    sleep,
		"odometer":     map[string]any{"value": s.OdometerKm, "unit": "km"},
		"transmission": map[string]any{"parkingPosition": !s.Driving},
		"timestamp":    timestamp(at),
	}
}

// doors maps the car's states to the API's rows: the first the front, the second the
// rear, each side one value (the driver's side is not told). The tyres are raw psi, as
// the doc's example gives them.
func status(r vehicle.Report, at time.Time) map[string]any {
	s := r.Status
	door := func(name string) string {
		if s.IsOpen(name) {
			return "open"
		}
		if s.Locked {
			return "locked"
		}
		return "closed"
	}
	closed := func(name string) string {
		if s.IsOpen(name) {
			return "open"
		}
		return "closed"
	}
	climate := "off"
	if s.Driving || r.Energy.Charging == vehicle.Charging {
		climate = "on" // assumed: the cabin blows while driving, the pack warms charging
	}
	seats := []map[string]any{}
	for range 2 {
		seats = append(seats, map[string]any{
			"left":  map[string]string{"heat": "off", "ventilation": "off"},
			"right": map[string]string{"heat": "off", "ventilation": "off"},
		})
	}
	return map[string]any{
		"doors": []map[string]any{
			{"left": door("front_left_door"), "right": door("front_right_door")},
			{"left": door("rear_left_door"), "right": door("rear_right_door")},
		},
		"hood":     closed("hood"),
		"trunk":    closed("tailgate"),
		"sunroof":  closed("sunroof"),
		"tire":     []map[string]any{{"left": 40, "right": 40, "unit": "psi"}, {"left": 40, "right": 40, "unit": "psi"}},
		"warnings": []string{},
		"climateControl": map[string]any{
			"mode": "auto", "status": climate, "speed": -1,
			"temperature": map[string]any{"value": 17, "unit": "C"},
		},
		"windshield":        map[string]string{"defrost": "off"},
		"steeringWheelHeat": "off",
		"seats":             seats,
		// outsideTemperature is left out: no electric car reports it, says the
		// compatibility matrix.
		"timestamp": timestamp(at),
	}
}

// sendAt is the instant a car is taken to have last sent a group it uploaded at at: at
// a change, and every SendEvery while it drives or charges (assumption, ADR 0049 §10.1).
func sendAt(b Behavior, at time.Time, active bool, now time.Time) time.Time {
	if !active || b.SendEvery <= 0 || now.Before(at) {
		return at
	}
	return at.Add(now.Sub(at) / b.SendEvery * b.SendEvery)
}
