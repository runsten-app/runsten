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
			Covers:          core.FieldCapacity | core.FieldModel,
			CapacityKWh:     core.Some(78.0, time.Time{}),
			Family:          core.Some("V60 II", time.Time{}),
			ModelYear:       core.Some(2019, time.Time{}),
			BatteryElectric: core.Some(false, time.Time{}),
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
		{Doors, "cv-doors.json", core.Snapshot{}},
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
				SoC:     core.Some(0.0, ts("2026-09-28T06:00:00Z")), // a real 0 %, not an absence
				RangeKm: core.Some(160.9344, ts("2026-09-28T06:00:00Z")),
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
				BatteryElectric: core.Some(true, none),
			},
		},
		{
			"electric", `"modelYear":2024,"fuelType":"ELECTRIC","descriptions":{"model":"XC40"}`,
			core.Snapshot{Family: core.Some("XC40", none), ModelYear: core.Some(2024, none), BatteryElectric: core.Some(true, none)},
		},
		{"hybrid", `"fuelType":"PETROL/ELECTRIC"`, core.Snapshot{BatteryElectric: core.Some(false, none)}},
		{"diesel", `"fuelType":"DIESEL"`, core.Snapshot{BatteryElectric: core.Some(false, none)}},
		{"unknown fuel", `"fuelType":"HYDROGEN"`, core.Snapshot{BatteryElectric: core.Some(false, none)}},
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
