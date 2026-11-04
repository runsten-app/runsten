package bmwapi

import (
	"math"
	"sort"
	"time"

	"runsten/internal/simulator/vehicle"
)

// The CarData descriptors the simulator gives, named as BMW's telematics data catalogue
// names them. Their units are the catalogue's; how BMW writes a unit in a
// response is assumed.
const (
	DescSoC            = "vehicle.drivetrain.batteryManagement.header"
	DescTargetSoC      = "vehicle.powertrain.electric.battery.stateOfCharge.target"
	DescChargingStatus = "vehicle.drivetrain.electricEngine.charging.status"
	DescHVStatus       = "vehicle.drivetrain.electricEngine.charging.hvStatus"
	DescChargingPower  = "vehicle.powertrain.electric.battery.charging.power"
	DescChargingMethod = "vehicle.drivetrain.electricEngine.charging.method"
	DescACAmpere       = "vehicle.drivetrain.electricEngine.charging.acAmpere"
	DescACVoltage      = "vehicle.drivetrain.electricEngine.charging.acVoltage"
	DescPhaseNumber    = "vehicle.drivetrain.electricEngine.charging.phaseNumber"
	DescTimeToFull     = "vehicle.drivetrain.electricEngine.charging.timeToFullyCharged"
	DescPortStatus     = "vehicle.body.chargingPort.status"
	DescPortDCStatus   = "vehicle.body.chargingPort.dcStatus"
	DescOdometer       = "vehicle.vehicle.travelledDistance"
	DescLatitude       = "vehicle.cabin.infotainment.navigation.currentLocation.latitude"
	DescLongitude      = "vehicle.cabin.infotainment.navigation.currentLocation.longitude"
	DescHeading        = "vehicle.cabin.infotainment.navigation.currentLocation.heading"
	DescRange          = "vehicle.drivetrain.electricEngine.kombiRemainingElectricRange"
	DescIsMoving       = "vehicle.isMoving"
	DescIgnition       = "vehicle.drivetrain.engine.isIgnitionOn"
	DescTyreFrontLeft  = "vehicle.chassis.axle.row1.wheel.left.tire.pressure"
	DescTyreFrontRight = "vehicle.chassis.axle.row1.wheel.right.tire.pressure"
	DescTyreRearLeft   = "vehicle.chassis.axle.row2.wheel.left.tire.pressure"
	DescTyreRearRight  = "vehicle.chassis.axle.row2.wheel.right.tire.pressure"
)

// Descriptors returns every descriptor the simulator gives, sorted: those the owner ticks
// in the portal for the stream, and puts in a container for the API.
func Descriptors() []string {
	d := []string{
		DescSoC, DescTargetSoC, DescChargingStatus, DescHVStatus, DescChargingPower, DescChargingMethod,
		DescACAmpere, DescACVoltage, DescPhaseNumber, DescTimeToFull, DescPortStatus, DescPortDCStatus,
		DescOdometer, DescLatitude, DescLongitude, DescHeading, DescRange, DescIsMoving, DescIgnition,
		DescTyreFrontLeft, DescTyreFrontRight, DescTyreRearLeft, DescTyreRearRight,
	}
	sort.Strings(d)
	return d
}

// value is a descriptor's value as CarData gives it: a number, a string or a boolean,
// with its unit.
type value struct {
	v    any
	unit string
}

// throttled are the descriptors a driving car sends at most every Behavior.MovingEvery:
// those that change all along a drive. The others are sent at each change.
var throttled = map[string]bool{
	DescSoC: true, DescOdometer: true, DescLatitude: true, DescLongitude: true, DescHeading: true, DescRange: true,
}

// asn is CarData's boolean: ASN_isTrue or ASN_isFalse.
func asn(b bool) string {
	if b {
		return "ASN_isTrue"
	}
	return "ASN_isFalse"
}

// render returns the descriptors' values for the car's state at now.
func render(spec vehicle.Spec, st vehicle.State, now time.Time) map[string]value {
	v := map[string]value{
		DescSoC:       {round(st.SoC, 1), "%"},
		DescTargetSoC: {int(st.TargetSoC), "%"},
		DescOdometer:  {int(math.Floor(st.OdometerKm)), "km"},
		DescLatitude:  {round(st.Position.Lat, 6), "degrees"},
		DescLongitude: {round(st.Position.Lon, 6), "degrees"},
		DescHeading:   {int(math.Round(st.Position.Heading)) % 360, "degrees"},
		DescRange:     {int(math.Round(st.SoC / 100 * spec.UsableKWhAt(now) / spec.ConsumptionKWhPer100km * 100)), "km"},
		DescIsMoving:  {asn(st.Driving), ""},
		DescIgnition:  {asn(st.Driving), ""},
		// The pressures do not change in the simulation: BMW's row1 is the front axle.
		DescTyreFrontLeft: {250, "kPa"}, DescTyreFrontRight: {250, "kPa"},
		DescTyreRearLeft: {270, "kPa"}, DescTyreRearRight: {270, "kPa"},
	}

	port, dcPort := "DISCONNECTED", "DISCONNECTED"
	status, hv, method := "NOCHARGING", "NOT_CHARGING", "NOCHARGING"
	if st.PluggedIn {
		port = "CONNECTED"
		if st.ChargeType == vehicle.DC {
			dcPort = "CONNECTED"
		}
		switch st.Charging {
		case vehicle.Charging:
			status, hv = "CHARGINGACTIVE", "CHARGING"
			// AC_TYPE2PLUG is the catalogue's; its DC value is not listed:
			// DC is assumed.
			method = "AC_TYPE2PLUG"
			if st.ChargeType == vehicle.DC {
				method = "DC"
			}
		case vehicle.Done:
			status, hv = "CHARGINGENDED", "FINISHED_FULLY_CHARGED"
			if st.SoC < st.TargetSoC {
				hv = "FINISHED_NOT_FULL"
			}
		default:
			status, hv = "NOCHARGING", "WAITING_FOR_CHARGING"
		}
	}
	v[DescPortStatus], v[DescPortDCStatus] = value{port, ""}, value{dcPort, ""}
	v[DescChargingStatus], v[DescHVStatus], v[DescChargingMethod] = value{status, ""}, value{hv, ""}, value{method, ""}

	power, amps, volts, phases, minutes := 0, 0, 0, "NO_CHARGING", 0
	if st.Charging == vehicle.Charging && st.ChargingPowerW > 0 {
		power = int(math.Round(st.ChargingPowerW))
		minutes = int(math.Ceil((st.TargetSoC - st.SoC) / 100 * spec.UsableKWhAt(now) / (st.ChargingPowerW / 1000) * 60))
		if st.ChargeType == vehicle.AC {
			n := 1
			if st.ChargingPowerW > 7400 {
				n = 3
			}
			volts, phases = 230, []string{"", "1-PHASES", "2-PHASES", "3-PHASES"}[n]
			amps = int(math.Round(st.ChargingPowerW / float64(230*n)))
		}
	}
	v[DescChargingPower] = value{power, "W"}
	v[DescACAmpere], v[DescACVoltage], v[DescPhaseNumber] = value{amps, "A"}, value{volts, "V"}, value{phases, ""}
	v[DescTimeToFull] = value{max(minutes, 0), "min"}
	return v
}

// round rounds x to n decimals.
func round(x float64, n int) float64 {
	p := math.Pow10(n)
	return math.Round(x*p) / p
}
