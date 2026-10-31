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
	core.FieldChargeType | core.FieldPower | core.FieldTargetSoC | core.FieldEnergyAdded

// TestNormalizeFixture: the real response of a Model Y, parked asleep a moment before,
// plugged to an AC station, its charge complete.
func TestNormalizeFixture(t *testing.T) {
	var none time.Time
	at := time.UnixMilli(1793032171549).UTC()
	want := core.Snapshot{
		Covers: chargeFields | core.FieldEngine | core.FieldPosition | core.FieldOdometer | core.FieldModel |
			core.FieldSoftware | core.FieldClimate | core.FieldSheet,
		Engine:     core.Some(core.EngineStopped, at), // shift_state null
		SoC:        core.Some(69.0, at),
		UsableSoC:  core.Some(68.0, at),
		RangeKm:    core.Some(miles(194.9), at),
		Charging:   core.Some(core.ChargingDone, at),
		Connection: core.Some(core.Connected, at),
		// fast_charger_type reads MCSingleWireCAN: no charge runs.
		ChargeType: core.Some(core.NoCharge, at),
		PowerW:     core.Some(0.0, at),
		TargetSoC:  core.Some(69.0, at),
		// The session's: it holds after the charge.
		EnergyAddedKWh: core.Some(5.9, at),
		OdometerKm:     core.Some(miles(39620.727469), at),
		// The latest fix, before the response.
		Position:   core.Some(core.Position{Lat: 50.8467, Lon: 4.3525}, time.Unix(1793032115, 0)),
		Brand:      core.Some(Brand, none),
		Family:     core.Some("Model Y", none),
		ModelYear:  core.Some(2021, none), // the VIN's M; efficiency_package says MY2020
		Trim:       core.Some("74d", none),
		Powertrain: core.Some(core.Electric, none),
		Software:   core.Some("2026.38.3.1", at),
		// Waiting for Wi-Fi to download the next.
		SoftwareUpdate: core.Some(core.SoftwareUpdate{
			Status: core.UpdateWaitingWiFi, Version: "2026.38.3.2",
			DownloadPct: core.Some(0.0, none), InstallPct: core.Some(1.0, none),
			Duration: core.Some(45*time.Minute, none),
		}, at),
		OutsideTempC: core.Some(16.0, at),
		CabinTempC:   core.Some(16.0, at),
		Climate: core.Some(core.Climate{
			On: core.Some(false, at), Preconditioning: core.Some(false, at), BatteryHeater: core.Some(false, at),
		}, at),
		// RoofColorGlass, Base and None as Tesla writes them; rhd false.
		Sheet: core.Some(core.Sheet{
			Name: "Demo", Colour: "Deep Blue", RoofColour: "Glass", Wheels: "HelixV220Dark", Steering: core.LeftHand,
		}, at),
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
			"charger_power": 120, "battery_level": 30, "usable_battery_level": 29, "charge_limit_soc": 80, "charge_energy_added": 12.34}`, core.Snapshot{
			Charging: core.Some(core.ChargingActive, at), Connection: core.Some(core.Connected, at),
			ChargeType: core.Some(core.DC, at), PowerW: core.Some(120000.0, at),
			SoC: core.Some(30.0, at), UsableSoC: core.Some(29.0, at), TargetSoC: core.Some(80.0, at), EnergyAddedKWh: core.Some(12.34, at),
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

// TestNormalizeClimate: the temperatures and what runs, each absent when null; a block
// without any covers the climate, absent.
func TestNormalizeClimate(t *testing.T) {
	at := time.UnixMilli(1793032171549).UTC()
	tests := []struct {
		body string
		want core.Snapshot
	}{
		{`"outside_temp": -3.5, "inside_temp": 19.5, "is_climate_on": true, "is_preconditioning": true, "battery_heater": true`, core.Snapshot{
			Covers: core.FieldClimate, OutsideTempC: core.Some(-3.5, at), CabinTempC: core.Some(19.5, at),
			Climate: core.Some(core.Climate{On: core.Some(true, at), Preconditioning: core.Some(true, at), BatteryHeater: core.Some(true, at)}, at),
		}},
		// Asleep a moment before, the car gives no temperature yet.
		{`"outside_temp": null, "inside_temp": null, "is_climate_on": false`, core.Snapshot{
			Covers: core.FieldClimate, Climate: core.Some(core.Climate{On: core.Some(false, at)}, at),
		}},
		{`"defrost_mode": 0`, core.Snapshot{Covers: core.FieldClimate}},
	}
	for _, tc := range tests {
		raw := []byte(`{"response": {"climate_state": {` + tc.body + `, "timestamp": 1793032171549}}}`)
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
			Covers: core.FieldModel | core.FieldSheet, Brand: core.Some(Brand, none), Family: tc.family, ModelYear: tc.year,
			Powertrain: core.Some(core.Electric, none),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s %s:\ngot  %+v\nwant %+v", tc.vin, tc.carType, got, want)
		}
	}
}

func TestNormalizeSheet(t *testing.T) {
	at := time.UnixMilli(1793032171549).UTC()
	for _, tc := range []struct {
		name, config, state string
		want                core.Value[core.Sheet]
	}{
		{
			"every value", `"exterior_color": "MidnightSilver", "roof_color": "RoofColorBlack", "wheel_type": "Induction20",
			"rhd": true, "performance_package": "Performance", "third_row_seats": "FuturisFoldFlat"`, `"vehicle_name": " Blixt "`,
			core.Some(core.Sheet{
				Name: "Blixt", Colour: "Midnight Silver", RoofColour: "Black", Wheels: "Induction20",
				Steering: core.RightHand, Performance: true, ThirdRow: true,
			}, at),
		},
		{
			"Tesla's words for none", `"exterior_color": "", "roof_color": "None", "wheel_type": "<invalid>",
			"performance_package": "Base", "third_row_seats": "None"`, `"vehicle_name": ""`,
			core.Value[core.Sheet]{},
		},
		{"nulls", `"exterior_color": null, "rhd": null`, `"vehicle_name": null`, core.Value[core.Sheet]{}},
		{"the name alone", ``, `"vehicle_name": "Blixt"`, core.Some(core.Sheet{Name: "Blixt"}, at)},
		{"without the state", `"rhd": false`, ``, core.Some(core.Sheet{Steering: core.LeftHand}, at)},
	} {
		raw := `{"response": {"vehicle_config": {` + tc.config + sep(tc.config) + `"timestamp": 1793032171549}`
		if tc.state != "" {
			raw += `, "vehicle_state": {` + tc.state + `}`
		}
		got, err := Normalize(VehicleData, []byte(raw+`}}`))
		if err != nil {
			t.Fatal(err)
		}
		if got.Covers&core.FieldSheet == 0 || !reflect.DeepEqual(got.Sheet, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got.Sheet, tc.want)
		}
	}
	// No configuration: no sheet, even with the name.
	got, err := Normalize(VehicleData, []byte(`{"response": {"vehicle_state": {"vehicle_name": "Blixt"}}}`))
	if err != nil || got.Covers&core.FieldSheet != 0 || got.Sheet.OK {
		t.Errorf("without the configuration: %+v, %v", got.Sheet, err)
	}
}

func sep(s string) string {
	if s == "" {
		return ""
	}
	return ", "
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

func TestNormalizeSoftware(t *testing.T) {
	at := time.UnixMilli(1793032171549).UTC()
	var none time.Time
	tests := []struct {
		name   string
		block  string
		covers bool
		want   core.Value[string]
		update core.Value[core.SoftwareUpdate]
	}{
		{"no version", `{"odometer": 1}`, false, core.Value[string]{}, core.Value[core.SoftwareUpdate]{}},
		{"an empty version", `{"car_version": " "}`, false, core.Value[string]{}, core.Value[core.SoftwareUpdate]{}},
		{
			"its build hash left out, no update", `{"car_version": "2025.32.3 abcdef123456",
				"software_update": {"status": "", "version": " ", "download_perc": 0, "install_perc": 1, "expected_duration_sec": 2700}}`,
			true, core.Some("2025.32.3", at),
			core.Value[core.SoftwareUpdate]{},
		},
		{
			"installing", `{"car_version": "2025.32.3 abcdef123456",
				"software_update": {"status": "installing", "version": "2025.38.1 0123456789ab", "download_perc": 100, "install_perc": 40}}`,
			true, core.Some("2025.32.3", at), core.Some(core.SoftwareUpdate{
				Status: core.UpdateInstalling, Version: "2025.38.1",
				DownloadPct: core.Some(100.0, none), InstallPct: core.Some(40.0, none),
			}, at),
		},
		{
			// As a Model Y told it, set to install at night.
			"scheduled", `{"car_version": "2026.38.3.1 7f5c3e1a2b4d",
				"software_update": {"status": "scheduled", "version": "2026.38.3.2", "install_perc": 10, "download_perc": 100,
					"scheduled_time_ms": 1793497800944, "expected_duration_sec": 1800}}`,
			true, core.Some("2026.38.3.1", at), core.Some(core.SoftwareUpdate{
				Status: core.UpdateScheduled, Version: "2026.38.3.2",
				DownloadPct: core.Some(100.0, none), InstallPct: core.Some(10.0, none),
				Duration: core.Some(30*time.Minute, none), ScheduledAt: core.Some(time.UnixMilli(1793497800944).UTC(), none),
			}, at),
		},
		{
			"a status not known", `{"car_version": "2025.32.3", "software_update": {"status": "paused"}}`,
			true, core.Some("2025.32.3", at), core.Some(core.SoftwareUpdate{Status: core.UpdateUnknown}, at),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"response": {"vehicle_state": ` + strings.TrimSuffix(tt.block, "}") + `, "timestamp": 1793032171549}}}`
			got, err := Normalize(VehicleData, []byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if got.Covers&core.FieldSoftware != 0 != tt.covers || got.Software != tt.want || !reflect.DeepEqual(got.SoftwareUpdate, tt.update) {
				t.Errorf("covers %v, software %+v, update %+v", got.Covers&core.FieldSoftware != 0, got.Software, got.SoftwareUpdate)
			}
		})
	}
}
