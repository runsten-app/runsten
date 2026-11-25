package fordapi

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strconv"
	"time"

	"runsten/internal/simulator/vehicle"
)

// sparse are the models reported to tell much less than the others: no speed, no
// temperature, no charger current nor voltage, no tyre pressure.
var sparse = map[string]bool{"Explorer": true, "Capri": true}

// garageOf is the car as the garage tells it: one object, the grant's car.
func garageOf(vin string, r vehicle.Report) map[string]any {
	engine := "BEV"
	if r.Spec.PlugInHybrid {
		engine = "PHEV"
	}
	return map[string]any{
		"vin": vin, "nickName": r.Spec.Model, "vehicleType": strconv.Itoa(r.Spec.ModelYear) + " " + r.Spec.Model,
		"color": "AGATE BLACK", "modelName": r.Spec.Model, "modelCode": "SIM1",
		"modelYear": strconv.Itoa(r.Spec.ModelYear), "tcuEnabled": 1, "make": "Ford", "ngSdnManaged": 1,
		"vehicleAuthorizationIndicator": 1, "engineType": engine,
	}
}

// telemetryOf is the car's state: each metric with the time the car last sent it. The
// position is the one of the last trip's end, as a car that sends it when its ignition
// goes off.
func telemetryOf(vin string, r vehicle.Report, b Behavior, now time.Time) map[string]any {
	active := r.Status.Driving || r.Energy.Charging == vehicle.Charging
	statusAt := sendAt(b, r.StatusAt, active, now)
	energyAt := sendAt(b, r.EnergyAt, active, now)
	rich := !sparse[r.Spec.Model]
	e, s := r.Energy, r.Status

	m := map[string]any{}
	set := func(name string, at time.Time, v any) {
		m[name] = map[string]any{"updateTime": timestamp(at), "value": v}
	}

	ignition := "OFF"
	if s.Driving {
		ignition = "ON"
	}
	set("ignitionStatus", statusAt, ignition)
	// Whole kilometres, as a car was seen to send.
	set("odometer", statusAt, math.Floor(s.OdometerKm))
	set("position", r.LocationAt, map[string]any{
		"location":     map[string]any{"lat": r.Location.Lat, "lon": r.Location.Lon, "alt": 0.0},
		"gpsDimension": "DIM_3D",
	})
	set("heading", r.LocationAt, map[string]any{"heading": r.Location.Heading, "uncertainty": 0.0, "detectionType": "HEADING_RAW_GNSS"})

	usable := r.Spec.UsableKWhAt(now)
	set("xevBatteryStateOfCharge", energyAt, math.Round(e.SoC*2)/2)
	// The actual level, below the displayed one by the buffers the car hides.
	set("xevBatteryActualStateOfCharge", energyAt, math.Round((5+e.SoC*0.9)*100)/100)
	set("xevBatteryEnergyRemaining", energyAt, math.Round(e.SoC/100*usable*10)/10)
	set("xevBatteryCapacity", energyAt, r.Spec.BatteryKWh)
	if c := r.Spec.ConsumptionKWhPer100km; c > 0 {
		set("xevBatteryRange", energyAt, math.Round(e.SoC/100*usable/c*100))
	}

	plug, display := "DISCONNECTED", "NOT_READY"
	if e.PluggedIn {
		plug, display = "CONNECTED", "STOPPED"
		switch e.Charging {
		case vehicle.Charging:
			display = "IN_PROGRESS"
		case vehicle.Done:
			display = "COMPLETED"
		case vehicle.Idle:
		}
	}
	set("xevPlugChargerStatus", energyAt, plug)
	set("xevBatteryChargeDisplayStatus", energyAt, display)
	charging := e.Charging == vehicle.Charging && e.ChargingPowerW > 0
	if charging {
		station := "AC_BASIC"
		if e.ChargeType == vehicle.DC {
			station = "DC_FAST"
		}
		set("xevChargeStationPowerType", energyAt, station)
		kwh := (100 - e.SoC) / 100 * usable
		set("xevBatteryTimeToFullCharge", energyAt, math.Ceil(kwh/(e.ChargingPowerW/1000)*60))
	}
	if rich {
		// The charger's output, on the battery's side: its voltage idles at a few volts.
		volts, amps := 5.0, 0.0
		if charging {
			volts = 350.0
			amps = math.Round(e.ChargingPowerW/volts*10) / 10
		}
		set("xevBatteryChargerVoltageOutput", energyAt, volts)
		set("xevBatteryChargerCurrentOutput", energyAt, amps)
		set("speed", statusAt, s.SpeedKmh)
		set("outsideTemperature", statusAt, 12.0)
		set("xevBatteryTemperature", energyAt, 18.0)
		m["tirePressure"] = []map[string]any{
			{"value": 250.0, "vehicleWheel": "FRONT_LEFT", "wheelPlacardFront": 250.0, "wheelPlacardRear": 240.0},
			{"value": 248.0, "vehicleWheel": "FRONT_RIGHT"},
			{"value": 241.0, "vehicleWheel": "REAR_LEFT"},
			{"value": 240.0, "vehicleWheel": "REAR_RIGHT"},
		}
		status := []map[string]any{}
		for _, wheel := range []string{"FRONT_LEFT", "FRONT_RIGHT", "REAR_LEFT", "REAR_RIGHT"} {
			status = append(status, map[string]any{"updateTime": timestamp(statusAt), "value": "NORMAL", "vehicleWheel": wheel})
		}
		m["tirePressureStatus"] = status
	}
	m["configurations"] = map[string]any{
		"xevBatteryTargetStateOfChargeSetting": map[string]any{"updateTime": timestamp(energyAt), "value": e.TargetSoC},
	}

	door := func(name string) string {
		if s.IsOpen(name) {
			return "AJAR"
		}
		return "CLOSED"
	}
	doors, all := []map[string]any{}, "CLOSED"
	for _, d := range [][2]string{
		{"FRONT_LEFT", "front_left_door"},
		{"FRONT_RIGHT", "front_right_door"},
		{"REAR_LEFT", "rear_left_door"},
		{"REAR_RIGHT", "rear_right_door"},
		{"TAILGATE", "tailgate"},
	} {
		st := door(d[1])
		if st == "AJAR" {
			all = "AJAR"
		}
		doors = append(doors, map[string]any{"updateTime": timestamp(statusAt), "value": st, "vehicleDoor": d[0]})
	}
	m["doorStatus"] = append(doors, map[string]any{"updateTime": timestamp(statusAt), "value": all, "vehicleDoor": "ALL_DOORS"})
	set("hoodStatus", statusAt, door("hood"))
	lock := "UNLOCKED"
	if s.Locked {
		lock = "LOCKED"
	}
	m["doorLockStatus"] = []map[string]any{{"updateTime": timestamp(statusAt), "value": lock, "vehicleDoor": "ALL_DOORS"}}
	windows := []map[string]any{}
	for _, w := range [][2]string{
		{"FRONT_LEFT", "front_left_window"},
		{"FRONT_RIGHT", "front_right_window"},
		{"REAR_LEFT", "rear_left_window"},
		{"REAR_RIGHT", "rear_right_window"},
	} {
		upper := 0.0
		if s.IsOpen(w[1]) {
			upper = 100.0
		}
		windows = append(windows, map[string]any{
			"updateTime": timestamp(statusAt), "vehicleWindow": w[0],
			"value": map[string]any{"doubleRange": map[string]any{"lowerBound": 0.0, "upperBound": upper}},
		})
	}
	m["windowStatus"] = windows

	latest := statusAt
	for _, at := range []time.Time{energyAt, r.LocationAt} {
		if at.After(latest) {
			latest = at
		}
	}
	return map[string]any{"updateTime": timestamp(latest), "vehicleId": vehicleID(vin), "vin": vin, "metrics": m}
}

// vehicleID is the car's identifier at Ford, a UUID: here derived from its VIN.
func vehicleID(vin string) string {
	sum := sha256.Sum256([]byte(vin))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// sendAt is the instant a car is taken to have last sent a metric it uploaded at at: at
// a change, and every SendEvery while it drives or charges (assumption).
func sendAt(b Behavior, at time.Time, active bool, now time.Time) time.Time {
	if !active || b.SendEvery <= 0 || now.Before(at) {
		return at
	}
	return at.Add(now.Sub(at) / b.SendEvery * b.SendEvery)
}

// timestamp writes a time as the API's: ISO 8601, UTC, to the second.
func timestamp(at time.Time) string { return at.UTC().Format(time.RFC3339) }
