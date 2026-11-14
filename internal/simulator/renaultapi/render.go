package renaultapi

import (
	"math"
	"strings"
	"time"

	"runsten/internal/simulator/vehicle"
)

// unserved are the endpoints a model does not serve, by its code (renault-api's map):
// they answer err.tech.501, as the Zoe phase 1's and the Mégane IV's location does.
var unserved = map[string][]string{
	"X101VE": {"location"},
	"XFB2BI": {"location"},
}

func served(code, endpoint string) bool {
	for _, e := range unserved[code] {
		if e == endpoint {
			return false
		}
	}
	return true
}

// zoePhase1 is the model whose charging power is in watts, and whose charging status
// has no other values than 0.0, 1.0 and -1.0.
const zoePhase1 = "X101VE"

// electricOnly are the models whose energy code is ELEC; the others, ELECX.
var electricOnly = map[string]bool{"X101VE": true, "X102VE": true, "X071VE": true, "XBG1VE": true, "XBG2VE": true}

// link is an entry of the vehicles' list: the vehicle and its details.
func (h *Handler) link(vin string, now time.Time) map[string]any {
	src := h.byVIN[vin]
	rep := src.ReportAt(now)
	return map[string]any{
		"brand": brandLabel(src), "vin": vin, "status": "ACTIVE", "linkType": "OWNER",
		"garageBrand": strings.ToLower(src.Brand()), "mileage": int(rep.Status.OdometerKm), "mileageUnit": "km",
		"mileageDate": rep.StatusAt.UTC().Format("2006-01-02"), "vehicleDetails": detailsOf(src, now),
	}
}

func brandLabel(s Source) string { return strings.ToUpper(s.Brand()) }

// detailsOf is the vehicle's details: its brand, its model's code and name, its energy
// and its picture.
func detailsOf(src Source, now time.Time) map[string]any {
	rep := src.ReportAt(now)
	code := rep.Spec.Trim
	energy := "ELECX"
	if electricOnly[code] {
		energy = "ELEC"
	}
	vin := src.VIN()
	return map[string]any{
		"vin": vin, "registrationNumber": "SIM-" + vin[len(vin)-3:],
		"brand":            map[string]any{"label": brandLabel(src)},
		"model":            map[string]any{"code": code, "label": strings.ToUpper(rep.Spec.Model)},
		"energy":           map[string]any{"code": energy},
		"engineEnergyType": "ELEC", "electrical": true,
		"assets": []map[string]any{{
			"assetType": "PICTURE", "viewpoint": "mybrand_2",
			"renditions": []map[string]any{{"resolutionType": "ONE_MYRENAULT_LARGE", "url": "https://example.invalid/render/" + vin + ".png"}},
		}},
	}
}

// render writes the response of a car-adapter read, in the JSON:API shape of the
// fixtures, each part dated from the car's last send.
func (h *Handler) render(vin, endpoint string, r vehicle.Report, now time.Time) map[string]any {
	active := r.Status.Driving || r.Energy.Charging == vehicle.Charging
	var attrs map[string]any
	switch endpoint {
	case "battery-status":
		attrs = h.battery(vin, r, sendAt(h.behavior, r.EnergyAt, active, now))
	case "cockpit":
		attrs = map[string]any{
			"fuelQuantity": 0.0, "totalMileage": math.Round(r.Status.OdometerKm*100) / 100,
			"timestamp": timestamp(sendAt(h.behavior, r.StatusAt, active, now)),
		}
	case "location":
		attrs = map[string]any{
			"gpsLatitude": r.Location.Lat, "gpsLongitude": r.Location.Lon,
			"lastUpdateTime": timestamp(r.LocationAt),
		}
	case "hvac-status":
		state := "off"
		if r.Status.Driving {
			state = "on" // assumption: the cabin blows while the car drives
		}
		attrs = map[string]any{
			"hvacStatus": state, "externalTemperature": 12.0, "internalTemperature": 20.0, "socThreshold": 15.0,
			"lastUpdateTime": timestamp(sendAt(h.behavior, r.StatusAt, active, now)),
		}
	}
	return map[string]any{"data": map[string]any{"type": "Car", "id": vin, "attributes": attrs}}
}

// battery is the charge: chargingStatus is a float (1.0 charging, 0.2 ended, 0.0 not),
// the time left in minutes while charging; chargingInstantaneousPower in watts on a Zoe
// phase 1, in kilowatts elsewhere (where the real one is the power the car would
// accept, taken here for the charge's).
func (h *Handler) battery(_ string, r vehicle.Report, at time.Time) map[string]any {
	e := r.Energy
	usable := r.Spec.UsableKWhAt(at)
	plug, status := 0, 0.0
	if e.PluggedIn {
		plug = 1
		switch e.Charging {
		case vehicle.Charging:
			status = 1.0
		case vehicle.Done:
			status = 0.2
		case vehicle.Idle:
		}
	}
	a := map[string]any{
		"timestamp": timestamp(at), "batteryLevel": int(math.Round(e.SoC)), "batteryAutonomy": rangeKm(r.Spec, at, e.SoC),
		"batteryCapacity": 0, "batteryAvailableEnergy": math.Round(e.SoC/100*usable*10) / 10, "batteryTemperature": 20,
		"plugStatus": plug, "chargingStatus": status,
	}
	if status == 1.0 && e.ChargingPowerW > 0 {
		kwh := (e.TargetSoC - e.SoC) / 100 * usable
		a["chargingRemainingTime"] = max(0, int(math.Ceil(kwh/(e.ChargingPowerW/1000)*60)))
		a["chargingInstantaneousPower"] = e.ChargingPowerW / 1000
		if r.Spec.Trim == zoePhase1 {
			a["chargingInstantaneousPower"] = e.ChargingPowerW
		}
	}
	return a
}

// rangeKm is the range the car forecasts at the SoC, over the usable capacity.
func rangeKm(s vehicle.Spec, at time.Time, soc float64) int {
	if s.ConsumptionKWhPer100km <= 0 {
		return 0
	}
	return int(soc / 100 * s.UsableKWhAt(at) / s.ConsumptionKWhPer100km * 100)
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
