package tesla

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
)

// miles and bar convert at run time, as the normalizer does: a constant expression would
// round otherwise.
func miles(v float64) float64 { return v * kmPerMile }

func bar(v float64) float64 { return v * kPaPerBar }

const chargeFields = core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
	core.FieldChargeType | core.FieldPower | core.FieldTargetSoC

// TestNormalizeFixture: the real response of a Model Y, parked asleep a moment before,
// plugged to an AC station, its charge complete.
func TestNormalizeFixture(t *testing.T) {
	var none time.Time
	at := time.UnixMilli(1793032171549).UTC()
	want := core.Snapshot{
		Covers:     chargeFields | core.FieldEngine | core.FieldPosition | core.FieldOdometer | core.FieldModel | core.FieldSleep,
		Engine:     core.Some(core.EngineStopped, at), // shift_state null
		SoC:        core.Some(69.0, at),
		RangeKm:    core.Some(miles(194.9), at),
		Charging:   core.Some(core.ChargingDone, at),
		Connection: core.Some(core.Connected, at),
		// fast_charger_type reads MCSingleWireCAN: no charge runs.
		ChargeType: core.Some(core.NoCharge, at),
		PowerW:     core.Some(0.0, at),
		TargetSoC:  core.Some(69.0, at),
		OdometerKm: core.Some(miles(39620.727469), at),
		// The latest fix, before the response.
		Position:        core.Some(core.Position{Lat: 50.8467, Lon: 4.3525}, time.Unix(1793032115, 0)),
		Brand:           core.Some(Brand, none),
		Family:          core.Some("Model Y", none),
		ModelYear:       core.Some(2021, none), // the VIN's M; efficiency_package says MY2020
		Trim:            core.Some("74d", none),
		BatteryElectric: core.Some(true, none),
		Sleep:           core.Some(core.Awake, none),
	}
	n := Normalizer{}
	got, err := n.Normalize(core.Kind{Source: Source, Endpoint: string(VehicleData)}, fixture(t, "vehicle_data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// The vehicle's state tells whether it sleeps, and nothing else.
	got, err = n.Normalize(core.Kind{Source: Source, Endpoint: string(VehicleState)}, fixture(t, "vehicle.json"))
	if want := (core.Snapshot{Covers: core.FieldSleep, Sleep: core.Some(core.Awake, none)}); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("vehicle: %+v, %v", got, err)
	}
	// Nor is the 408's body, were it stored.
	if got, err := Normalize(VehicleData, fixture(t, "vehicle_data-408.json")); err != nil || got.Covers != 0 {
		t.Errorf("408: %+v, %v", got, err)
	}
	if _, err := n.Normalize(core.Kind{Source: "volvo", Endpoint: string(VehicleData)}, nil); err == nil {
		t.Error("another source: no error")
	}
	if _, err := Normalize(VehicleData, []byte(`{`)); err == nil {
		t.Error("invalid JSON: no error")
	}
}

// TestNormalizeSleep: the vehicle's state, each word Tesla gives; an unknown one covers
// the state, absent.
func TestNormalizeSleep(t *testing.T) {
	for state, want := range map[string]core.Value[core.SleepState]{
		"online":  core.Some(core.Awake, time.Time{}),
		"asleep":  core.Some(core.Asleep, time.Time{}),
		"offline": core.Some(core.Offline, time.Time{}),
		"waking":  {},
	} {
		got, err := Normalize(VehicleState, []byte(`{"response":{"vin":"LRWYGCEK9MC000000","state":"`+state+`"}}`))
		if err != nil || got.Covers != core.FieldSleep || got.Sleep != want {
			t.Errorf("%s: %+v, %v", state, got, err)
		}
	}
}

func TestNormalizerKinds(t *testing.T) {
	n := Normalizer{}
	if got, want := n.Kinds(), []core.Kind{{Source: Source, Endpoint: "vehicle"}, {Source: Source, Endpoint: "vehicle_data"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("kinds: %v", got)
	}
	want := []core.Kind{{Source: Source, Endpoint: "vehicle_data"}}
	for name, got := range map[string][]core.Kind{"energy": n.EnergyKinds(), "health": n.HealthKinds()} {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestNormalizeCharge(t *testing.T) {
	at := time.UnixMilli(1793032171549).UTC()
	tests := []struct {
		name string
		body string
		want core.Snapshot
	}{
		{"DC", `{"charging_state": "Charging", "fast_charger_present": true, "fast_charger_type": "Supercharger",
			"charger_power": 120, "battery_level": 30, "charge_limit_soc": 80}`, core.Snapshot{
			Charging: core.Some(core.ChargingActive, at), Connection: core.Some(core.Connected, at),
			ChargeType: core.Some(core.DC, at), PowerW: core.Some(120000.0, at),
			SoC: core.Some(30.0, at), TargetSoC: core.Some(80.0, at),
		}},
		{
			"AC starting", `{"charging_state": "Starting", "fast_charger_present": false, "fast_charger_type": "MCSingleWireCAN"}`,
			core.Snapshot{
				Charging: core.Some(core.ChargingActive, at), Connection: core.Some(core.Connected, at),
				ChargeType: core.Some(core.AC, at),
			},
		},
		{"charging, no word of the charger", `{"charging_state": "Charging"}`, core.Snapshot{
			Charging: core.Some(core.ChargingActive, at), Connection: core.Some(core.Connected, at),
		}},
		{"unplugged", `{"charging_state": "Disconnected", "charger_power": 0}`, core.Snapshot{
			Charging: core.Some(core.ChargingIdle, at), Connection: core.Some(core.Disconnected, at),
			ChargeType: core.Some(core.NoCharge, at), PowerW: core.Some(0.0, at),
		}},
		{"no power", `{"charging_state": "NoPower"}`, core.Snapshot{
			Charging: core.Some(core.ChargingIdle, at), Connection: core.Some(core.Connected, at),
			ChargeType: core.Some(core.NoCharge, at),
		}},
		{"stopped", `{"charging_state": "Stopped"}`, core.Snapshot{
			Charging: core.Some(core.ChargingIdle, at), Connection: core.Some(core.Connected, at),
			ChargeType: core.Some(core.NoCharge, at),
		}},
		{"an unknown word", `{"charging_state": "Calibrating", "battery_level": 50}`, core.Snapshot{SoC: core.Some(50.0, at)}},
		{"nothing", `{"charging_state": null}`, core.Snapshot{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"response": {"charge_state": ` + tc.body[:len(tc.body)-1] + `, "timestamp": 1793032171549}}}`)
			got, err := Normalize(VehicleData, raw)
			if err != nil {
				t.Fatal(err)
			}
			tc.want.Covers = chargeFields
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestNormalizeDrive(t *testing.T) {
	at := time.UnixMilli(1793032171549).UTC()
	tests := []struct {
		body string
		want core.Snapshot
	}{
		{`"shift_state": "D", "latitude": 50.1, "longitude": 4.2`, core.Snapshot{
			Covers: core.FieldEngine | core.FieldPosition,
			Engine: core.Some(core.EngineRunning, at),
			// Without gps_as_of, the block's time.
			Position: core.Some(core.Position{Lat: 50.1, Lon: 4.2}, at),
		}},
		{`"shift_state": "R"`, core.Snapshot{Covers: core.FieldEngine | core.FieldPosition, Engine: core.Some(core.EngineRunning, at)}},
		{`"shift_state": "N", "latitude": 50.1`, core.Snapshot{Covers: core.FieldEngine | core.FieldPosition, Engine: core.Some(core.EngineRunning, at)}},
		{`"shift_state": "P"`, core.Snapshot{Covers: core.FieldEngine | core.FieldPosition, Engine: core.Some(core.EngineStopped, at)}},
		// An unknown gear leaves the engine as it was.
		{`"shift_state": "X"`, core.Snapshot{Covers: core.FieldPosition}},
	}
	for _, tc := range tests {
		raw := []byte(`{"response": {"drive_state": {` + tc.body + `, "timestamp": 1793032171549}}}`)
		got, err := Normalize(VehicleData, raw)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\ngot  %+v\nwant %+v", tc.body, got, tc.want)
		}
	}
}

func TestNormalizeModel(t *testing.T) {
	var none time.Time
	for _, tc := range []struct {
		vin, carType string
		family       core.Value[string]
		year         core.Value[int]
	}{
		{"5YJ3E1EA7KF000001", `"model3"`, core.Some("Model 3", none), core.Some(2019, none)},
		{"5YJSA1E2XPF000001", `"models"`, core.Some("Model S", none), core.Some(2023, none)},
		{"7SAXCBE6XPF000001", `"modelx"`, core.Some("Model X", none), core.Some(2023, none)},
		{"7G2CEHED3RA000001", `"cybertruck"`, core.Some("Cybertruck", none), core.Some(2024, none)},
		// The brand stands in for a family not known, and a VIN not readable gives no year.
		{"", `"roadster"`, core.Value[string]{}, core.Value[int]{}},
		{"5YJ3E1EA7_F000001", `null`, core.Value[string]{}, core.Value[int]{}},
	} {
		raw := []byte(`{"response": {"vin": "` + tc.vin + `", "vehicle_config": {"car_type": ` + tc.carType + `}}}`)
		got, err := Normalize(VehicleData, raw)
		if err != nil {
			t.Fatal(err)
		}
		want := core.Snapshot{
			Covers: core.FieldModel, Brand: core.Some(Brand, none), Family: tc.family, ModelYear: tc.year,
			BatteryElectric: core.Some(true, none),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s %s:\ngot  %+v\nwant %+v", tc.vin, tc.carType, got, want)
		}
	}
}

func TestFingerprint(t *testing.T) {
	raw := fixture(t, "vehicle_data.json")
	a, err := Fingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	// A read later of the same values: new timestamps.
	later := bytes.ReplaceAll(raw, []byte("1793032171549"), []byte("1793032231549"))
	if b, err := Fingerprint(later); err != nil || !bytes.Equal(a, b) {
		t.Errorf("same values, another fingerprint: %v", err)
	}
	moved := bytes.Replace(raw, []byte(`"battery_level": 69`), []byte(`"battery_level": 68`), 1)
	if b, err := Fingerprint(moved); err != nil || bytes.Equal(a, b) {
		t.Errorf("another value, the same fingerprint: %v", err)
	}
	// The vehicle's state counts.
	on, _ := Fingerprint(fixture(t, "vehicle.json"))
	off, _ := Fingerprint(bytes.Replace(fixture(t, "vehicle.json"), []byte(`"online"`), []byte(`"offline"`), 1))
	if bytes.Equal(on, off) {
		t.Error("online and offline alike")
	}
	if _, err := Fingerprint([]byte(`{`)); err == nil {
		t.Error("invalid JSON: no error")
	}
}

func TestUploadOf(t *testing.T) {
	u, err := UploadOf(fixture(t, "vehicle_data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.UnixMilli(1793032171549).UTC(); !u.At.Equal(want) {
		t.Errorf("at %v, want %v", u.At, want)
	}
	if u.ModelYear == 0 {
		t.Error("no model year")
	}
	if !slices.Contains(u.Fields, "charge_state.battery_level") || !slices.IsSorted(u.Fields) {
		t.Errorf("fields %v", u.Fields)
	}
	for _, f := range u.Fields {
		if f == "vin" || strings.HasSuffix(f, "timestamp") {
			t.Errorf("field %s", f)
		}
	}
	// The vehicle's state: no block, so no time.
	if u, err := UploadOf(fixture(t, "vehicle.json")); err != nil || !u.At.IsZero() || !slices.Contains(u.Fields, "state") {
		t.Errorf("vehicle: %+v, %v", u, err)
	}
	if _, err := UploadOf([]byte(`{`)); err == nil {
		t.Error("invalid JSON: no error")
	}
}
