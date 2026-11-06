package bmw

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"
)

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

	// The doors and the lids: OPEN or CLOSED, the catalogue's words for a type it calls
	// boolean (a boolean read too). The catalogue names row1's driver door the front left
	// one; on a right-hand drive car it may be the right (assumed: as the catalogue says).
	DescDoorFrontLeft  = "vehicle.cabin.door.row1.driver.isOpen"
	DescDoorFrontRight = "vehicle.cabin.door.row1.passenger.isOpen"
	DescDoorRearLeft   = "vehicle.cabin.door.row2.driver.isOpen"
	DescDoorRearRight  = "vehicle.cabin.door.row2.passenger.isOpen"
	DescHood           = "vehicle.body.hood.isOpen"
	DescTrunk          = "vehicle.body.trunk.isOpen"      // CLOSED, OPEN, INTERMEDIATE
	DescLock           = "vehicle.cabin.door.lock.status" // LOCKED, SECURED, UNLOCKED, SELECTIVE-LOCKED
	// The windows and the sunroof: CLOSED, INTERMEDIATE or OPEN.
	DescWindowFrontLeft  = "vehicle.cabin.window.row1.driver.status"
	DescWindowFrontRight = "vehicle.cabin.window.row1.passenger.status"
	DescWindowRearLeft   = "vehicle.cabin.window.row2.driver.status"
	DescWindowRearRight  = "vehicle.cabin.window.row2.passenger.status"
	DescSunroof          = "vehicle.cabin.sunroof.status"
	// DescPreconditioning is the preconditioning's activity: standby, heating, cooling,
	// ventilation or inactive.
	DescPreconditioning = "vehicle.vehicle.preConditioning.activity"
)

// Descriptors returns the descriptors of Runsten's container, sorted: those the owner
// ticks in the portal for the stream too.
func Descriptors() []string {
	d := []string{
		DescSoC, DescTargetSoC, DescChargingStatus, DescHVStatus, DescChargingPower, DescChargingMethod,
		DescACAmpere, DescACVoltage, DescPhaseNumber, DescTimeToFull, DescPortStatus, DescPortDCStatus,
		DescOdometer, DescLatitude, DescLongitude, DescRange, DescIsMoving, DescIgnition,
		DescTyreFrontLeft, DescTyreFrontRight, DescTyreRearLeft, DescTyreRearRight,
		DescDoorFrontLeft, DescDoorFrontRight, DescDoorRearLeft, DescDoorRearRight, DescHood, DescTrunk, DescLock,
		DescWindowFrontLeft, DescWindowFrontRight, DescWindowRearLeft, DescWindowRearRight, DescSunroof,
		DescPreconditioning,
	}
	slices.Sort(d)
	return d
}

// containerPrefix begins the name of every container Runsten creates; the first ones
// were named so alone. containerPurpose is the purpose they are all created with.
const (
	containerPrefix  = "runsten"
	containerPurpose = "Runsten, a data logger of the owner's vehicles"
)

// runstenName matches the names Runsten gives its containers: runsten, the first ones,
// then runsten- and 8 hexadecimal digits.
var runstenName = regexp.MustCompile(`^runsten(-[0-9a-f]{8})?$`)

// ContainerName is the name of Runsten's container: its prefix and 8 hexadecimal digits
// of its descriptors' hash. A container cannot be changed once created (ADR 0045 §5):
// a release that reads more descriptors names a new one, which the collector creates in
// place of the previous.
func ContainerName() string {
	sum := sha256.Sum256([]byte(strings.Join(Descriptors(), "\n")))
	return containerPrefix + "-" + hex.EncodeToString(sum[:4])
}

// isRunstenContainer tells a container Runsten created, of any release: a name of its
// and its purpose, so that a container another client named alike is never taken.
func isRunstenContainer(ct Container) bool {
	return runstenName.MatchString(ct.Name) && ct.Purpose == containerPurpose
}
