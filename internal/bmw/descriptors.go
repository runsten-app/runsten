package bmw

import "slices"

// The CarData descriptors Runsten reads, named as BMW's telematics data catalogue names
// them (ADR 0045 §5). Which a car sends, and how often, is to see on a real one.
const (
	DescSoC = "vehicle.drivetrain.batteryManagement.header" // %
	// DescSoCDisplayed is the state of charge of the Neue Klasse, which may lack the
	// header: read when the stream pushes it, not asked for in the container.
	DescSoCDisplayed   = "vehicle.powertrain.electric.battery.stateOfCharge.displayed"
	DescTargetSoC      = "vehicle.powertrain.electric.battery.stateOfCharge.target"      // %
	DescChargingStatus = "vehicle.drivetrain.electricEngine.charging.status"             // CHARGINGACTIVE…
	DescHVStatus       = "vehicle.drivetrain.electricEngine.charging.hvStatus"           // CHARGING…
	DescChargingPower  = "vehicle.powertrain.electric.battery.charging.power"            // W
	DescChargingMethod = "vehicle.drivetrain.electricEngine.charging.method"             // AC_TYPE2PLUG…
	DescACAmpere       = "vehicle.drivetrain.electricEngine.charging.acAmpere"           // A
	DescACVoltage      = "vehicle.drivetrain.electricEngine.charging.acVoltage"          // V
	DescPhaseNumber    = "vehicle.drivetrain.electricEngine.charging.phaseNumber"        // 3-PHASES…
	DescTimeToFull     = "vehicle.drivetrain.electricEngine.charging.timeToFullyCharged" // min
	DescPortStatus     = "vehicle.body.chargingPort.status"                              // CONNECTED…
	DescPortDCStatus   = "vehicle.body.chargingPort.dcStatus"                            // CONNECTED…
	DescOdometer       = "vehicle.vehicle.travelledDistance"                             // km
	DescLatitude       = "vehicle.cabin.infotainment.navigation.currentLocation.latitude"
	DescLongitude      = "vehicle.cabin.infotainment.navigation.currentLocation.longitude"
	DescRange          = "vehicle.drivetrain.electricEngine.kombiRemainingElectricRange" // km
	DescIsMoving       = "vehicle.isMoving"                                              // ASN_isTrue…
	DescIgnition       = "vehicle.drivetrain.engine.isIgnitionOn"                        // ASN_isTrue…
	DescTyreFrontLeft  = "vehicle.chassis.axle.row1.wheel.left.tire.pressure"            // kPa
	DescTyreFrontRight = "vehicle.chassis.axle.row1.wheel.right.tire.pressure"
	DescTyreRearLeft   = "vehicle.chassis.axle.row2.wheel.left.tire.pressure"
	DescTyreRearRight  = "vehicle.chassis.axle.row2.wheel.right.tire.pressure"
)

// Descriptors returns the descriptors of Runsten's container, sorted: those the owner
// ticks in the portal for the stream too.
func Descriptors() []string {
	d := []string{
		DescSoC, DescTargetSoC, DescChargingStatus, DescHVStatus, DescChargingPower, DescChargingMethod,
		DescACAmpere, DescACVoltage, DescPhaseNumber, DescTimeToFull, DescPortStatus, DescPortDCStatus,
		DescOdometer, DescLatitude, DescLongitude, DescRange, DescIsMoving, DescIgnition,
		DescTyreFrontLeft, DescTyreFrontRight, DescTyreRearLeft, DescTyreRearRight,
	}
	slices.Sort(d)
	return d
}
