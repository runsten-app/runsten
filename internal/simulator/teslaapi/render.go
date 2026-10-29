package teslaapi

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"runsten/internal/simulator/vehicle"
)

// template is a real vehicle_data of a Model Y, anonymised: the response's shape, and the
// values the simulation does not change (climate, settings, configuration, tyres).
//
//go:embed vehicle_data.json
var template []byte

const kmPerMile = 1.609344

// renderVehicleData fills the template with the car's state at now: the Owner API gives
// distances in miles and pressures in bar, whatever the car displays.
func renderVehicleData(c *car, r vehicle.Report, st vehicle.State, now time.Time) (map[string]any, error) {
	var top struct {
		Response map[string]any `json:"response"`
	}
	dec := json.NewDecoder(bytes.NewReader(template))
	dec.UseNumber() // the template's numbers written back as they are
	if err := dec.Decode(&top); err != nil {
		return nil, fmt.Errorf("vehicle_data template: %w", err)
	}
	v := top.Response
	ms := now.UnixMilli()
	block := func(name string) map[string]any {
		b := v[name].(map[string]any) //nolint:forcetypeassert // the template's
		b["timestamp"] = ms
		return b
	}
	for k, val := range summary(c, true) {
		v[k] = val
	}
	delete(v, "display_name") // not in vehicle_data
	delete(v, "option_codes")
	v["user_id"] = userID

	renderCharge(block("charge_state"), r.Spec, st, now)
	renderDrive(block("drive_state"), r.Spec, st, now)

	vs := block("vehicle_state")
	vs["odometer"] = round(st.OdometerKm/kmPerMile, 6)
	vs["locked"] = st.Locked
	vs["is_user_present"] = st.Driving
	// The sensors report while the car rolls: last seen at the end of the last drive.
	seen := r.StatusAt.Unix()
	if st.Driving {
		seen = now.Unix()
	}
	for _, w := range []string{"fl", "fr", "rl", "rr"} {
		vs["tpms_last_seen_pressure_time_"+w] = seen
	}

	cfg := block("vehicle_config")
	if t := carType(r.Spec.Model); t != "" {
		cfg["car_type"] = t
	}
	if r.Spec.Trim != "" {
		cfg["trim_badging"] = r.Spec.Trim
	} else {
		delete(cfg, "trim_badging")
	}

	block("climate_state")["is_climate_on"] = st.Driving
	block("gui_settings")
	return v, nil
}

// carType is vehicle_config.car_type of a model: Model 3 → model3. Empty for a model
// that is not a Tesla's, which leaves the template's.
func carType(model string) string {
	m := strings.ToLower(strings.ReplaceAll(model, " ", ""))
	if !strings.HasPrefix(m, "model") && m != "cybertruck" {
		return ""
	}
	return m
}

func renderCharge(cs map[string]any, spec vehicle.Spec, st vehicle.State, now time.Time) {
	usable := spec.UsableKWhAt(now)
	miles := func(kwh float64) float64 { return kwh / spec.ConsumptionKWhPer100km * 100 / kmPerMile }
	// The usable level a point below the SoC, as a real car read it; the rated range a
	// fixed conversion of it, which the rated capacity estimate rests on.
	level := max(0, st.SoC-1)
	rangeMi := round(miles(level/100*usable), 2)
	cs["battery_level"] = int(st.SoC)
	cs["usable_battery_level"] = int(level)
	cs["battery_range"] = rangeMi
	cs["ideal_battery_range"] = rangeMi
	cs["charge_limit_soc"] = int(st.TargetSoC)
	cs["charge_energy_added"] = round(st.ChargeAddedKWh, 2)
	cs["charge_miles_added_rated"] = round(miles(st.ChargeAddedKWh), 1)
	cs["charge_miles_added_ideal"] = round(miles(st.ChargeAddedKWh), 1)

	state, color, cable, latch := "Disconnected", "Off", "<invalid>", "Disengaged"
	if st.PluggedIn {
		cable, latch = "IEC", "Engaged"
		switch st.Charging {
		case vehicle.Charging:
			state, color = "Charging", "FlashingGreen"
		case vehicle.Done:
			state, color = "Complete", "Green"
		default:
			state, color = "Stopped", "Blue"
		}
	}
	cs["charging_state"], cs["charge_port_color"], cs["conn_charge_cable"], cs["charge_port_latch"] = state, color, cable, latch
	cs["charge_port_door_open"] = st.PluggedIn

	// Assumed from TeslaMate: on DC, the AC side's voltage and current stay near zero,
	// and the power is charger_power's.
	kw, volts, amps, phases, rate, hours := 0, 0, 0, any(nil), 0.0, 0.0
	if st.PluggedIn {
		volts = 2 // observed on the car plugged in, not charging
	}
	charging := st.Charging == vehicle.Charging && st.ChargingPowerW > 0
	if charging {
		kw = int(math.Round(st.ChargingPowerW / 1000))
		rate = round(miles(st.ChargingPowerW/1000), 1)
		hours = (st.TargetSoC - st.SoC) / 100 * usable / (st.ChargingPowerW / 1000)
		if st.ChargeType == vehicle.AC {
			n := 1
			if st.ChargingPowerW > 7400 {
				n = 3
			}
			volts, phases = 230, n
			amps = int(math.Round(st.ChargingPowerW / float64(230*n)))
		}
	}
	dc := charging && st.ChargeType == vehicle.DC
	cs["fast_charger_present"] = dc
	if dc {
		cs["fast_charger_type"], cs["fast_charger_brand"] = "Combo", "<invalid>" // assumed: a CCS charger
	}
	cs["charger_power"], cs["charger_voltage"], cs["charger_actual_current"], cs["charger_phases"] = kw, volts, amps, phases
	cs["charge_amps"], cs["charge_current_request"] = max(amps, 13), max(amps, 13)
	cs["charge_rate"] = rate
	cs["time_to_full_charge"] = round(hours, 2)
	cs["minutes_to_full_charge"] = int(math.Ceil(hours * 60))
}

func renderDrive(ds map[string]any, spec vehicle.Spec, st vehicle.State, now time.Time) {
	p := st.Position
	ds["latitude"], ds["longitude"] = p.Lat, p.Lon
	ds["native_latitude"], ds["native_longitude"] = p.Lat, p.Lon
	ds["heading"] = int(math.Round(p.Heading)) % 360
	ds["gps_as_of"] = now.Unix()
	ds["shift_state"], ds["speed"], ds["power"] = nil, nil, 0
	switch {
	case st.Driving:
		ds["shift_state"] = "D"
		ds["speed"] = int(math.Round(st.SpeedKmh / kmPerMile))
		ds["power"] = int(math.Round(st.SpeedKmh * spec.ConsumptionKWhPer100km / 100))
	case st.Charging == vehicle.Charging:
		ds["power"] = -int(math.Round(st.ChargingPowerW / 1000)) // negative: into the battery
	}
}

// round rounds x to n decimals.
func round(x float64, n int) float64 {
	p := math.Pow10(n)
	return math.Round(x*p) / p
}
