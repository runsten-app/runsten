package core

import "time"

// Bounds is the interval in which an instant lies. Polling only brackets transitions:
// the vehicle started (or stopped) somewhere between the last reading that showed the
// old state (After) and the first that showed the new one (Before).
type Bounds struct {
	After, Before time.Time
}

// Trip is a trip derived from snapshots: two points, never a track.
//
// An observed trip was seen driving: it started in Start and ended in End. A
// reconstructed trip was not seen at all (outage, stopped collector, exhausted quota):
// only the odometer revealed it, so it happened somewhere between Start.After and
// End.Before, and Start and End are that same interval.
type Trip struct {
	// DetectedAt is the time of the first reading that revealed the trip. It identifies
	// the trip for a given vehicle.
	DetectedAt    time.Time
	Reconstructed bool
	Start, End    Bounds

	StartOdometerKm, EndOdometerKm Value[float64]
	DistanceKm                     Value[float64]
	StartSoC, EndSoC               Value[float64]
	StartRangeKm, EndRangeKm       Value[float64]
	EnergyKWh                      Value[float64] // ΔSoC × Capacity
	// Capacity is the battery capacity EnergyKWh rests on: that of the readings as of the
	// first one showing the trip over (for a reconstructed trip, revealing it). A reading
	// that settles the trip later may be one an incremental derivation never replays.
	Capacity Value[Capacity]
	From, To Value[Position]

	// Vehicle's own trip figures at the end of the trip, for comparison: their exact
	// semantics are unknown, they are never used to derive anything.
	TripMeterKm            Value[float64]
	ConsumptionKWhPer100km Value[float64]
}

// PowerSpan is the stretch of a charge's power integral that a battery capacity
// estimate rests on: the energy and the SoC change are read at the same readings.
type PowerSpan struct {
	StartSoC  float64       // %, at the span's first reading
	EndSoC    float64       // %, at its last integrated reading
	EnergyKWh float64       // integral of the charging power between the two
	MaxGap    time.Duration // widest interval between two consecutive of its readings
	Duration  time.Duration // from its first to its last integrated reading
}

// Charge is a charging session derived from snapshots. A reconstructed charge was not
// seen charging: only a rise of the SoC revealed it.
type Charge struct {
	// DetectedAt is the time of the first reading that revealed the charge. It
	// identifies the charge for a given vehicle.
	DetectedAt    time.Time
	Reconstructed bool
	Start, End    Bounds

	Type                        Value[ChargeType]
	StartSoC, EndSoC, TargetSoC Value[float64]
	// Two estimates, kept apart: neither is a measurement (no energy meter is exposed).
	EnergySoCKWh   Value[float64] // ΔSoC × Capacity
	EnergyPowerKWh Value[float64] // integral of the charging power over the observed readings
	// Span is the stretch of that integral a battery capacity estimate rests on. Absent
	// when no two of the charge's power readings follow each other below MaxSpanSoC: a
	// reconstructed charge, or a vehicle that reports no charging power.
	Span Value[PowerSpan]
	// OdometerKm is the odometer known at the reading that ends the charge, taken as
	// Capacity is, never from a later reading.
	OdometerKm Value[float64]
	// Capacity is the battery capacity EnergySoCKWh rests on: that of the readings as of
	// the one that gave EndSoC or ended the charge.
	Capacity Value[Capacity]
	Position Value[Position]
}
