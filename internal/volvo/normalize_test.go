package volvo

import (
	"reflect"
	"testing"
	"time"

	"runsten/internal/core"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNormalizeFixtures(t *testing.T) {
	energyAt := ts("2025-03-04T10:14:08Z")
	tests := []struct {
		ep   Endpoint
		file string
		want core.Snapshot
	}{
		{EngineStatus, "cv-engine-status.json", core.Snapshot{
			Covers: core.FieldEngine,
			Engine: core.Some(core.EngineStopped, ts("2026-09-25T15:37:22.385600145Z")),
		}},
		{Odometer, "cv-odometer.json", core.Snapshot{
			Covers:     core.FieldOdometer,
			OdometerKm: core.Some(42.0, ts("2026-09-25T15:37:22.078633774Z")),
		}},
		{Location, "location.json", core.Snapshot{
			Covers:   core.FieldPosition,
			Position: core.Some(core.Position{Lat: 57.68877034998428, Lon: 11.968302141174869}, ts("2026-09-25T15:40:12.045762046Z")),
		}},
		{Statistics, "cv-statistics.json", core.Snapshot{
			Covers:                 core.FieldTripMeter | core.FieldConsumption,
			TripMeterKm:            core.Some(420.0, ts("2026-09-25T15:37:22.226Z")),
			ConsumptionKWhPer100km: core.Some(2.4732, ts("2026-09-25T15:37:22.226Z")),
		}},
		// The Demo car, a diesel V60 II: its capacity is read, but it is no battery
		// electric vehicle, which keeps its variant from being recognized.
		{Details, "cv-vehicle.json", core.Snapshot{
			Covers:      core.FieldCapacity | core.FieldModel,
			CapacityKWh: core.Some(78.0, time.Time{}),
			Family:      core.Some("V60 II", time.Time{}),
			ModelYear:   core.Some(2019, time.Time{}),
			Powertrain:  core.Some(core.OtherPowertrain, time.Time{}),
		}},
		{EnergyState, "energy-v2-state.json", core.Snapshot{
			Covers: core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
				core.FieldChargeType | core.FieldPower | core.FieldTargetSoC,
			SoC:        core.Some(50.0, energyAt),
			RangeKm:    core.Some(180.0, energyAt),
			Charging:   core.Some(core.ChargingIdle, energyAt),
			Connection: core.Some(core.Connected, energyAt),
			ChargeType: core.Some(core.AC, energyAt),
			PowerW:     core.Some(1600.0, energyAt),
			TargetSoC:  core.Some(85.0, energyAt),
		}},
		// The Demo car's, locked, a window open.
		{Doors, "cv-doors.json", func() core.Snapshot {
			at := ts("2026-09-25T15:37:22.985858324Z")
			closed := core.Some(core.Closed, at)
			return core.Snapshot{Covers: core.FieldDoors, Doors: core.Some(core.Doors{
				Locked:    core.Some(true, at),
				FrontLeft: closed, FrontRight: closed, RearLeft: closed, RearRight: closed,
				Hood: closed, Tailgate: closed, TankLid: closed,
			}, at)}
		}()},
		{Windows, "cv-windows.json", func() core.Snapshot {
			at := ts("2026-09-25T15:37:23.105928815Z")
			closed := core.Some(core.Closed, at)
			return core.Snapshot{Covers: core.FieldWindows, Windows: core.Some(core.Windows{
				FrontLeft: core.Some(core.Open, at), FrontRight: closed, RearLeft: closed, RearRight: closed,
				Sunroof: closed,
			}, at)}
		}()},
		{Tyres, "cv-tyres.json", core.Snapshot{}},
	}
	for _, tt := range tests {
		t.Run(string(tt.ep), func(t *testing.T) {
			got, err := Normalize(tt.ep, fixture(t, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// Absent, null, in error, in an unknown unit or with an unknown value: absent, never
// zero.
func TestNormalizeAbsences(t *testing.T) {
	tests := []struct {
		name string
		ep   Endpoint
		raw  string
		want core.Snapshot
	}{
		{
			"null value", Odometer, `{"data":{"odometer":{"timestamp":"x","unit":"km","value":null}}}`,
			core.Snapshot{Covers: core.FieldOdometer},
		},
		{"missing key", Odometer, `{"data":{}}`, core.Snapshot{Covers: core.FieldOdometer}},
		{
			"unknown unit", Odometer, `{"data":{"odometer":{"timestamp":"x","unit":"furlong","value":3}}}`,
			core.Snapshot{Covers: core.FieldOdometer},
		},
		{
			"null unit", Odometer, `{"data":{"odometer":{"timestamp":"x","unit":null,"value":3}}}`,
			core.Snapshot{Covers: core.FieldOdometer},
		},
		{
			"miles, unreadable timestamp", Odometer, `{"data":{"odometer":{"timestamp":"x","unit":"mi","value":10}}}`,
			core.Snapshot{Covers: core.FieldOdometer, OdometerKm: core.Some(16.09344, time.Time{})},
		},
		{
			"unknown engine state", EngineStatus, `{"data":{"engineStatus":{"timestamp":"x","unit":null,"value":"IDLING"}}}`,
			core.Snapshot{Covers: core.FieldEngine},
		},
		{
			"running", EngineStatus, `{"data":{"engineStatus":{"timestamp":"x","unit":null,"value":"RUNNING"}}}`,
			core.Snapshot{Covers: core.FieldEngine, Engine: core.Some(core.EngineRunning, time.Time{})},
		},
		{
			"energy errors", EnergyState, `{
			"batteryChargeLevel":{"status":"OK","value":0,"unit":"percentage","updatedAt":"2026-09-28T06:00:00Z"},
			"electricRange":{"status":"OK","value":100,"unit":"miles","updatedAt":"2026-09-28T06:00:00Z"},
			"chargingStatus":{"status":"OK","value":"UNPLUGGED_SOMEHOW","updatedAt":"2026-09-28T06:00:00Z"},
			"chargingType":{"status":"OK","value":"NONE","updatedAt":"2026-09-28T06:00:00Z"},
			"chargingPower":{"status":"ERROR","code":"PROPERTY_NOT_FOUND","message":"Property not found"},
			"chargingCurrentLimit":{"status":"ERROR","code":"NOT_SUPPORTED","message":"x"},
			"targetBatteryChargeLevel":{"status":"OK","value":null,"unit":"percentage","updatedAt":"2026-09-28T06:00:00Z"}}`,
			core.Snapshot{
				Covers: core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
					core.FieldChargeType | core.FieldPower | core.FieldTargetSoC,
				SoC:        core.Some(0.0, ts("2026-09-28T06:00:00Z")), // a real 0 %, not an absence
				RangeKm:    core.Some(160.9344, ts("2026-09-28T06:00:00Z")),
				ChargeType: core.Some(core.NoCharge, ts("2026-09-28T06:00:00Z")), // told, not unknown
			},
		},
		{
			"no coordinates", Location, `{"data":{"geometry":{"coordinates":[null,45]},"properties":{}}}`,
			core.Snapshot{Covers: core.FieldPosition},
		},
		{"no details", Details, `{"data":{"batteryCapacityKWH":null}}`, core.Snapshot{Covers: core.FieldCapacity | core.FieldModel}},
		{
			"kW", EnergyState, `{"chargingPower":{"status":"OK","value":7.4,"unit":"kW","updatedAt":"x"}}`,
			core.Snapshot{
				Covers: core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
					core.FieldChargeType | core.FieldPower | core.FieldTargetSoC,
				PowerW: core.Some(7400.0, time.Time{}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.ep, []byte(tt.raw))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestNormalizeInvalidJSON(t *testing.T) {
	for _, ep := range NormalizedEndpoints() {
		if _, err := Normalize(ep, []byte("[")); err == nil {
			t.Errorf("%s: invalid JSON accepted", ep)
		}
	}
}

func TestNormalizeDetails(t *testing.T) {
	var none time.Time
	tests := []struct {
		name, data string
		want       core.Snapshot
	}{
		{
			"EX30", `"batteryCapacityKWH":69.0,"modelYear":2024,"fuelType":"NONE","descriptions":{"model":"EX30"}`,
			core.Snapshot{
				CapacityKWh: core.Some(69.0, none), Family: core.Some("EX30", none), ModelYear: core.Some(2024, none),
				Powertrain: core.Some(core.Electric, none),
			},
		},
		{
			"electric", `"modelYear":2024,"fuelType":"ELECTRIC","descriptions":{"model":"XC40"}`,
			core.Snapshot{Family: core.Some("XC40", none), ModelYear: core.Some(2024, none), Powertrain: core.Some(core.Electric, none)},
		},
		{"plug-in hybrid", `"fuelType":"PETROL/ELECTRIC"`, core.Snapshot{Powertrain: core.Some(core.PlugInHybrid, none)}},
		{"diesel plug-in hybrid", `"fuelType":"DIESEL/ELECTRIC"`, core.Snapshot{Powertrain: core.Some(core.PlugInHybrid, none)}},
		{"two fuels, no electricity", `"fuelType":"PETROL/CNG"`, core.Snapshot{Powertrain: core.Some(core.OtherPowertrain, none)}},
		{"diesel", `"fuelType":"DIESEL"`, core.Snapshot{Powertrain: core.Some(core.OtherPowertrain, none)}},
		{"unknown fuel", `"fuelType":"HYDROGEN"`, core.Snapshot{Powertrain: core.Some(core.OtherPowertrain, none)}},
		{"combustion capacity", `"batteryCapacityKWH":0.0`, core.Snapshot{}},
		{"missing", ``, core.Snapshot{}},
		{"null", `"modelYear":null,"fuelType":null,"descriptions":{"model":null}`, core.Snapshot{}},
		{"empty", `"fuelType":"","descriptions":{"model":""}`, core.Snapshot{}},
		{"no descriptions", `"descriptions":null`, core.Snapshot{}},
		{"unreadable", `"modelYear":"2024","fuelType":1,"descriptions":{"model":40}`, core.Snapshot{}},
		{"invalid year", `"modelYear":0`, core.Snapshot{}},
		{"fractional year", `"modelYear":2024.5`, core.Snapshot{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(Details, []byte(`{"data":{`+tt.data+`}}`))
			if err != nil {
				t.Fatal(err)
			}
			tt.want.Covers = core.FieldCapacity | core.FieldModel
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestNormalizeOpenings: an UNSPECIFIED or unknown value is absent, the latest timestamp
// dates the response, and one that tells nothing covers its field all the same, emptied.
func TestNormalizeOpenings(t *testing.T) {
	early, late := ts("2026-10-11T10:00:00Z"), ts("2026-10-11T10:05:00Z")
	s, err := Normalize(Doors, []byte(`{"data": {
		"centralLock": {"value": "UNLOCKED", "timestamp": "2026-10-11T10:00:00Z"},
		"frontLeftDoor": {"value": "AJAR", "timestamp": "2026-10-11T10:05:00Z"},
		"tailgate": {"value": "UNSPECIFIED", "timestamp": "2026-10-11T10:06:00Z"},
		"hood": {"value": "HALF", "timestamp": "2026-10-11T10:06:00Z"}}}`))
	want := core.Snapshot{Covers: core.FieldDoors, Doors: core.Some(core.Doors{
		Locked: core.Some(false, early), FrontLeft: core.Some(core.Ajar, late),
	}, late)}
	if err != nil || !reflect.DeepEqual(s, want) {
		t.Errorf("doors: got  %+v, %v\nwant %+v", s, err, want)
	}
	s, err = Normalize(Doors, []byte(`{"data": {"centralLock": {"value": "UNSPECIFIED"}}}`))
	if err != nil || !reflect.DeepEqual(s, core.Snapshot{Covers: core.FieldDoors}) {
		t.Errorf("doors unknown: %+v, %v", s, err)
	}
	s, err = Normalize(Windows, []byte(`{"data": {"sunroof": {"value": "AJAR", "timestamp": "2026-10-11T10:00:00Z"}}}`))
	if err != nil || !reflect.DeepEqual(s, core.Snapshot{Covers: core.FieldWindows, Windows: core.Some(core.Windows{Sunroof: core.Some(core.Ajar, early)}, early)}) {
		t.Errorf("windows: %+v, %v", s, err)
	}
	for _, ep := range []Endpoint{Doors, Windows} {
		if _, err := Normalize(ep, []byte("[")); err == nil {
			t.Errorf("%s: invalid JSON accepted", ep)
		}
	}
}

func TestNormalizer(t *testing.T) {
	var n Normalizer
	if got := n.Kinds(); len(got) != len(NormalizedEndpoints()) || got[0] != (core.Kind{Source: Source, Endpoint: string(EngineStatus)}) {
		t.Errorf("kinds %v", got)
	}
	if got := n.EnergyKinds(); len(got) != 1 || got[0].Endpoint != string(EnergyState) {
		t.Errorf("energy kinds %v", got)
	}
	// The doors and windows are no readings of the derivation.
	if got := n.StateKinds(); len(got) != 2 || got[0].Endpoint != string(Doors) || got[1].Endpoint != string(Windows) {
		t.Errorf("state kinds %v", got)
	}
	odo := []byte(`{"data":{"odometer":{"value":1234,"unit":"km","timestamp":"2026-10-11T10:00:00Z"}}}`)
	got, err := n.Normalize(core.Kind{Source: Source, Endpoint: string(Odometer)}, odo)
	if err != nil || !got.OdometerKm.OK || got.OdometerKm.V != 1234 {
		t.Errorf("odometer: %+v, %v", got.OdometerKm, err)
	}
	if _, err := n.Normalize(core.Kind{Source: "phone", Endpoint: string(Odometer)}, odo); err == nil {
		t.Error("another source's response read")
	}
}
