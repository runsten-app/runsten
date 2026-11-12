package skodaapi

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"runsten/internal/simulator/vehicle"
)

// simulated are the parts the simulator renders, by include value, with the error type
// that tells one unavailable. The others (fuelStatus, auxiliaryHeating,
// activeVentilation, chargingProfiles) are an electric car's unsupported ones here:
// absent, and told in errors only when asked for, as the API does. operations, which
// the contract gives no unsupported error, is an empty list: no command is simulated.
var simulated = map[string]string{
	"status":          "VEHICLE_STATUS_UNAVAILABLE",
	"odometer":        "ODOMETER_UNAVAILABLE",
	"parkingPosition": "PARKING_POSITION_UNAVAILABLE",
	"airConditioning": "AIR_CONDITIONING_UNAVAILABLE",
	"charging":        "CHARGING_UNAVAILABLE",
}

// unsupported are the error types of the parts an electric car of the simulator does not
// support.
var unsupported = map[string]string{
	"fuelStatus":        "FUEL_STATUS_UNSUPPORTED",
	"auxiliaryHeating":  "AUXILIARY_HEATING_UNSUPPORTED",
	"activeVentilation": "ACTIVE_VENTILATION_UNSUPPORTED",
	"chargingProfiles":  "CHARGING_PROFILES_UNSUPPORTED",
}

// response is a vehicle's response: the parts asked for, each dated from its last send,
// and the errors of those it could not give.
func response(vin string, r vehicle.Report, parts map[string]bool, explicit bool,
	unavailable map[string]bool, b Behavior, now time.Time,
) map[string]any {
	active := r.Status.Driving || r.Energy.Charging == vehicle.Charging
	v := map[string]any{"vin": vin}
	errs := []map[string]string{}
	if parts["info"] {
		// The model's name, as the API gives it when the owner did not name the car.
		v["name"] = r.Spec.Model
		v["licensePlate"] = "1SIM001"
		v["renderUrl"] = "https://example.invalid/render/" + vin + ".png"
	}
	render := map[string]func() map[string]any{
		"status":          func() map[string]any { return status(r, sendAt(b, r.StatusAt, active, now)) },
		"odometer":        func() map[string]any { return odometer(r, sendAt(b, r.StatusAt, active, now)) },
		"parkingPosition": func() map[string]any { return parkingPosition(r) },
		"airConditioning": func() map[string]any { return airConditioning(r, sendAt(b, r.StatusAt, active, now)) },
		"charging":        func() map[string]any { return charging(r, sendAt(b, r.EnergyAt, active, now)) },
	}
	for _, p := range Parts() {
		if !parts[p] {
			continue
		}
		if errType, ok := simulated[p]; ok {
			if unavailable[p] || unavailable[""] {
				errs = append(errs, map[string]string{"type": errType, "description": "Could not be retrieved."})
				continue
			}
			v[p] = render[p]()
		} else if p == "operations" {
			v[p] = []any{}
		} else if errType, ok := unsupported[p]; ok && explicit {
			errs = append(errs, map[string]string{"type": errType, "description": "Not supported by the vehicle."})
		}
	}
	return map[string]any{"vehicle": v, "errors": errs}
}

func status(r vehicle.Report, at time.Time) map[string]any {
	s := r.Status
	opening := func(names ...string) string {
		for _, n := range names {
			if s.IsOpen(n) {
				return "OPEN"
			}
		}
		return "CLOSED"
	}
	doors := opening("front_left_door", "front_right_door", "rear_left_door", "rear_right_door")
	trunk, bonnet := opening("tailgate"), opening("hood")
	locked, doorsLocked := "NO", "NO"
	switch {
	case doors == "OPEN" || bonnet == "OPEN":
		doorsLocked = "OPENED"
	case trunk == "OPEN":
		doorsLocked = "TRUNK_OPENED"
	case s.Locked:
		locked, doorsLocked = "YES", "YES"
	}
	return map[string]any{
		"overall": map[string]any{
			"doorsLocked": doorsLocked, "locked": locked, "doors": doors,
			"windows": opening("front_left_window", "front_right_window", "rear_left_window", "rear_right_window"),
			"lights":  "OFF",
		},
		"detail": map[string]any{
			"sunroof": opening("sunroof"), "trunk": trunk, "bonnet": bonnet,
		},
		"carCapturedTimestamp": timestamp(at),
	}
}

func odometer(r vehicle.Report, at time.Time) map[string]any {
	return map[string]any{"mileageInKm": int64(r.Status.OdometerKm), "carCapturedTimestamp": timestamp(at)}
}

// parkingPosition tells IN_MOTION while the car drives, its position once parked: the
// contract dates no parking position.
func parkingPosition(r vehicle.Report) map[string]any {
	if r.Status.Driving {
		return map[string]any{"state": "IN_MOTION"}
	}
	p := r.Location
	return map[string]any{
		"state":            "PARKED",
		"gpsCoordinates":   map[string]any{"latitude": p.Lat, "longitude": p.Lon},
		"formattedAddress": "Simulated Street 1, 69001 Lyon, France",
	}
}

// airConditioning runs while the car drives (assumption: the cabin blows), off parked.
func airConditioning(r vehicle.Report, at time.Time) map[string]any {
	state := "OFF"
	if r.Status.Driving {
		state = "HEATING"
	}
	return map[string]any{
		"state":                               state,
		"targetTemperature":                   map[string]any{"value": 21.0, "unit": "CELSIUS"},
		"airConditioningWithoutExternalPower": true,
		"airConditioningAtUnlock":             false,
		"windowHeating":                       map[string]any{"enabled": false, "front": "OFF", "rear": "OFF"},
		"carCapturedTimestamp":                timestamp(at),
	}
}

// charging maps the car's charge to the API's derived state: CHARGING, CONSERVING once
// the target is reached plugged in, READY_FOR_CHARGING plugged without a charge,
// CONNECT_CABLE unplugged.
func charging(r vehicle.Report, at time.Time) map[string]any {
	e := r.Energy
	state, chargeType, plug, lock := "CONNECT_CABLE", "OFF", "DISCONNECTED", "UNLOCKED"
	if e.PluggedIn {
		plug, lock, state = "CONNECTED", "LOCKED", "READY_FOR_CHARGING"
		switch e.Charging {
		case vehicle.Charging:
			state = "CHARGING"
			chargeType = "AC"
			if e.ChargeType == vehicle.DC {
				chargeType = "DC"
			}
		case vehicle.Done:
			state = "CONSERVING"
		case vehicle.Idle:
		}
	}
	powerKW, minutes, rate := 0.0, 0, 0.0
	if e.Charging == vehicle.Charging && e.ChargingPowerW > 0 {
		powerKW = math.Round(e.ChargingPowerW/100) / 10
		kwh := (e.TargetSoC - e.SoC) / 100 * r.Spec.UsableKWhAt(at)
		minutes = max(0, int(math.Ceil(kwh/powerKW*60)))
		if r.Spec.ConsumptionKWhPer100km > 0 {
			rate = math.Round(powerKW / r.Spec.ConsumptionKWhPer100km * 100)
		}
	}
	return map[string]any{
		"isVehicleInSavedLocation": false,
		"status": map[string]any{
			"chargingRateInKilometersPerHour":      rate,
			"chargePowerInKw":                      powerKW,
			"remainingTimeToFullyChargedInMinutes": minutes,
			"state":                                state,
			"chargeType":                           chargeType,
			"plugConnectionState":                  plug,
			"plugLockState":                        lock,
			"battery": map[string]any{
				"remainingCruisingRangeInMeters": rangeMeters(r.Spec, at, e.SoC),
				"stateOfChargeInPercent":         int(math.Round(e.SoC)),
			},
		},
		"settings": map[string]any{
			"targetStateOfChargeInPercent": int(e.TargetSoC),
			"preferredChargeMode":          "MANUAL",
			"availableChargeModes":         []string{"MANUAL", "TIMER"},
			"chargingCareMode":             "DEACTIVATED",
			"autoUnlockPlugWhenCharged":    "OFF",
			"maxChargeCurrentAc":           "MAXIMUM",
			"maxChargeCurrentAcAmpere":     32,
		},
		"carCapturedTimestamp": timestamp(at),
	}
}

// rangeMeters is the range the car forecasts at the SoC, over the usable capacity, in
// meters.
func rangeMeters(s vehicle.Spec, at time.Time, soc float64) int {
	if s.ConsumptionKWhPer100km <= 0 {
		return 0
	}
	return int(soc / 100 * s.UsableKWhAt(at) / s.ConsumptionKWhPer100km * 100 * 1000)
}

// sendAt is the instant a car is taken to have last sent a part it uploaded at at: at a
// change, and every SendEvery while it drives or charges (assumption).
func sendAt(b Behavior, at time.Time, active bool, now time.Time) time.Time {
	if !active || b.SendEvery <= 0 || now.Before(at) {
		return at
	}
	return at.Add(now.Sub(at) / b.SendEvery * b.SendEvery)
}

// timestamp writes a time as the API's: ISO 8601, UTC, to the second.
func timestamp(at time.Time) string { return at.UTC().Format(time.RFC3339) }

// writeProblem answers with an RFC 9457 problem.
func writeProblem(w http.ResponseWriter, status int, typ, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{ // the header is already sent: nothing more to do
		"type": typ, "title": title, "status": status, "detail": detail,
		"instance": "/api/v1/vehicles/" + strconv.Itoa(status),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}
