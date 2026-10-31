package polestar

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/simulator/polestarapi"
)

const fixturesDir = "../../testdata/polestar-data-portal"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixturesDir, name)) //nolint:gosec // repository file
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const batteryFields = core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
	core.FieldChargeType | core.FieldPower | core.FieldConsumption

// TestNormalizeFixtures: the real responses of a Polestar 2, parked and unplugged.
func TestNormalizeFixtures(t *testing.T) {
	var none time.Time
	tests := []struct {
		d    Domain
		file string
		want core.Snapshot
	}{
		{Battery, "telemetry/battery.json", func() core.Snapshot {
			at := time.Unix(1789729847, 818764407).UTC()
			return core.Snapshot{
				Covers:     batteryFields | core.FieldModel,
				SoC:        core.Some(52.0, at),
				RangeKm:    core.Some(170.0, at),
				Connection: core.Some(core.Disconnected, at),
				Charging:   core.Some(core.ChargingIdle, at),
				ChargeType: core.Some(core.NoCharge, at),
				// Left out of the response: no charge runs, the power is 0.
				PowerW:                 core.Some(0.0, at),
				ConsumptionKWhPer100km: core.Some(20.1, at),
				Brand:                  core.Some(Brand, none),
				ModelYear:              core.Some(2023, none), // YSMTEST22PL000001: P
				Powertrain:             core.Some(core.Electric, none),
			}
		}()},
		{Odometer, "telemetry/odometer.json", core.Snapshot{
			Covers:      core.FieldOdometer | core.FieldTripMeter,
			OdometerKm:  core.Some(122952.704, time.Unix(1789729423, 41718424)),
			TripMeterKm: core.Some(136.3000030517578, time.Unix(1789729423, 41718424)),
		}},
		{Location, "telemetry/location.json", core.Snapshot{
			Covers:   core.FieldPosition,
			Position: core.Some(core.Position{Lat: 47.6062, Lon: -122.3321}, time.Unix(1789729422, 0)),
		}},
		{Availability, "telemetry/availability.json", core.Snapshot{
			Covers: core.FieldEngine,
			Engine: core.Some(core.EngineStopped, time.Unix(1789729843, 66550399)),
		}},
		{TargetSoC, "charging/target-soc.json", core.Snapshot{
			Covers:    core.FieldTargetSoC,
			TargetSoC: core.Some(80.0, time.Unix(1789729185, 785000000)),
		}},
		{Exterior, "telemetry/exterior.json", func() core.Snapshot {
			at := time.Unix(1789729816, 418289732).UTC()
			closed := core.Some(core.Closed, at)
			return core.Snapshot{
				Covers: core.FieldDoors | core.FieldWindows,
				Doors: core.Some(core.Doors{
					Locked:    core.Some(true, at),
					FrontLeft: closed, FrontRight: closed, RearLeft: closed, RearRight: closed,
					Hood: closed, Tailgate: closed, TankLid: closed,
				}, at),
				Windows: core.Some(core.Windows{FrontLeft: closed, FrontRight: closed, RearLeft: closed, RearRight: closed}, at),
			}
		}()},
		// The health is read apart (NormalizeHealth): nothing of a snapshot.
		{"health", "telemetry/health.json", core.Snapshot{}},
	}
	for _, tt := range tests {
		t.Run(string(tt.d), func(t *testing.T) {
			got, err := Normalize(tt.d, fixture(t, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestNormalizeAbsences: protobuf's JSON leaves zeros out; a missing field is absent,
// never zero, but the power out of an active charge.
func TestNormalizeAbsences(t *testing.T) {
	ts := `"timestamp":{"seconds":"1789729847","nanos":5}`
	at := time.Unix(1789729847, 5).UTC()
	tests := []struct {
		name string
		d    Domain
		data string
		want core.Snapshot
	}{
		{"empty battery", Battery, ``, core.Snapshot{Covers: batteryFields}},
		{
			"charging, power reported", Battery, ts + `,"chargingStatusV2":"CHARGING_STATUS_V2_CHARGING","chargingType":"CHARGING_TYPE_AC","chargingPowerWatts":7400`,
			core.Snapshot{
				Covers: batteryFields, Charging: core.Some(core.ChargingActive, at), ChargeType: core.Some(core.AC, at),
				PowerW: core.Some(7400.0, at),
			},
		},
		{
			// A model that does not report its power: unknown, never 0 W.
			"charging, power left out", Battery, ts + `,"chargingStatusV2":"CHARGING_STATUS_V2_SMART_CHARGING"`,
			core.Snapshot{Covers: batteryFields, Charging: core.Some(core.ChargingActive, at)},
		},
		{
			"done, power left out", Battery, ts + `,"chargingStatusV2":"CHARGING_STATUS_V2_DONE","chargerConnectionStatus":"CHARGER_CONNECTION_STATUS_CONNECTED"`,
			core.Snapshot{
				Covers: batteryFields, Charging: core.Some(core.ChargingDone, at), Connection: core.Some(core.Connected, at),
				PowerW: core.Some(0.0, at),
			},
		},
		{
			// The charging status unknown, whether a charge runs is too.
			"status unspecified, power left out", Battery, ts + `,"chargingStatusV2":"CHARGING_STATUS_V2_UNSPECIFIED"`,
			core.Snapshot{Covers: batteryFields},
		},
		{
			"unknown values", Battery, ts + `,"chargingStatusV2":"CHARGING_STATUS_V2_TELEPORTING","chargingType":"CHARGING_TYPE_WIRELESS",` +
				`"chargerConnectionStatus":"CHARGER_CONNECTION_STATUS_UNSPECIFIED","chargingStatus":"CHARGING_STATUS_CHARGING"`,
			core.Snapshot{Covers: batteryFields},
		},
		{
			"a value without its prefix", Battery, ts + `,"chargingStatusV2":"CHARGING","chargingType":"AC"`,
			core.Snapshot{Covers: batteryFields},
		},
		{
			// SoC and range at 0 are left out too: unknown rather than an invented zero.
			"zeros left out", Battery, ts + `,"chargingStatusV2":"CHARGING_STATUS_V2_IDLE"`,
			core.Snapshot{Covers: batteryFields, Charging: core.Some(core.ChargingIdle, at), PowerW: core.Some(0.0, at)},
		},
		{
			"no timestamp", Battery, `"batteryChargeLevelPercentage":40`,
			core.Snapshot{Covers: batteryFields, SoC: core.Some(40.0, time.Time{})},
		},
		{
			"unreadable timestamp", Battery, `"timestamp":{"seconds":"soon"},"batteryChargeLevelPercentage":40`,
			core.Snapshot{Covers: batteryFields, SoC: core.Some(40.0, time.Time{})},
		},
		{
			"nanos out of range", Battery, `"timestamp":{"seconds":"1789729847","nanos":1000000000},"batteryChargeLevelPercentage":40`,
			core.Snapshot{Covers: batteryFields, SoC: core.Some(40.0, time.Time{})},
		},
		{
			"seconds as a number, nanos left out", Battery, `"timestamp":{"seconds":1789729847},"batteryChargeLevelPercentage":40`,
			core.Snapshot{Covers: batteryFields, SoC: core.Some(40.0, time.Unix(1789729847, 0))},
		},
		{"empty odometer", Odometer, ``, core.Snapshot{Covers: core.FieldOdometer | core.FieldTripMeter}},
		{"no coordinate", Location, ts, core.Snapshot{Covers: core.FieldPosition}},
		{"half a coordinate", Location, ts + `,"coordinate":{"latitude":57.7}`, core.Snapshot{Covers: core.FieldPosition}},
		{
			"driving", Availability, ts + `,"usageMode":"USAGE_MODE_DRIVING"`,
			core.Snapshot{Covers: core.FieldEngine, Engine: core.Some(core.EngineRunning, at)},
		},
		// The engine left as it was: UNSPECIFIED, left out, or unknown.
		{"usage unspecified", Availability, ts + `,"usageMode":"USAGE_MODE_UNSPECIFIED"`, core.Snapshot{}},
		{"usage left out", Availability, ts, core.Snapshot{}},
		{"usage unknown", Availability, ts + `,"usageMode":"USAGE_MODE_FLYING"`, core.Snapshot{}},
		{"no target", TargetSoC, ts, core.Snapshot{Covers: core.FieldTargetSoC}},
		{"target left out", TargetSoC, ts + `,"targetSoc":{"chargeTargetLevelSettingType":"DAILY"}`, core.Snapshot{Covers: core.FieldTargetSoC}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.d, []byte(`{"data":{`+tt.data+`}}`))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestChargingStatuses(t *testing.T) {
	want := map[core.ChargingStatus][]string{
		core.ChargingActive:      {"CHARGING", "SMART_CHARGING", "CHARGING_TOWARDS_MIN_SOC", "CHARGING_IS_EN_ROUTE", "SMART_CHARGING_WILL_NOT_FINISH"},
		core.ChargingDone:        {"DONE", "CHARGE_LEVEL_IS_GOOD_TO_GO"},
		core.ChargingScheduled:   {"SCHEDULED", "SCHEDULED_CHARGING_CANNOT_COMPLETE", "SCHEDULED_CHARGING_WILL_COMPLETE", "SMART_CHARGING_PAUSED"},
		core.ChargingIdle:        {"IDLE"},
		core.ChargingDischarging: {"DISCHARGING", "DISCHARGING_V2H", "DISCHARGING_V2L"},
		core.ChargingError:       {"ERROR"},
	}
	n := 0
	for status, names := range want {
		for _, name := range names {
			n++
			s, err := Normalize(Battery, []byte(`{"data":{"chargingStatusV2":"CHARGING_STATUS_V2_`+name+`"}}`))
			if err != nil || !s.Charging.OK || s.Charging.V != status {
				t.Errorf("%s: %+v, %v; want %s", name, s.Charging, err, status)
			}
		}
	}
	// Every value of the specification but UNSPECIFIED.
	if n != len(chargingStatuses) || n != 16 {
		t.Errorf("%d values tested, %d mapped", n, len(chargingStatuses))
	}
}

func TestModelYear(t *testing.T) {
	for _, tt := range []struct {
		vin  string
		year int
		ok   bool
	}{
		{"LPSVSEDE5RL000001", 2024, true},
		{"YSMTEST22PL000001", 2023, true},
		{"LPSVSEDE5LL000001", 2020, true},
		{"LPSVSEDE5YL000001", 2030, true},
		{"LPSVSEDE51L000001", 2031, true},
		{"LPSVSEDE5AL000001", 2010, true},
		{"LPSVSEDE5UL000001", 0, false}, // never a year code
		{"LPSVSEDE50L000001", 0, false},
	} {
		s, err := Normalize(Battery, []byte(`{"data":{"vin":"`+tt.vin+`"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if s.ModelYear.OK != tt.ok || s.ModelYear.V != tt.year || !s.BatteryElectric() ||
			s.Family.OK || s.Brand != core.Some(Brand, time.Time{}) || s.Covers&core.FieldModel == 0 {
			t.Errorf("%s: %+v", tt.vin, s)
		}
	}
	// No VIN, or not one of 17 characters: the model is not covered.
	for _, data := range []string{`{}`, `{"vin":"LPS"}`} {
		s, err := Normalize(Battery, []byte(`{"data":`+data+`}`))
		if err != nil || s.Covers&core.FieldModel != 0 || s.Powertrain.OK {
			t.Errorf("%s: %+v, %v", data, s, err)
		}
	}
}

// TestNormalizeExterior: a door or window open or ajar, the car unlocked; what is left
// out (UNSPECIFIED) or unknown is absent, and a response without any of them covers them
// all the same, emptied.
func TestNormalizeExterior(t *testing.T) {
	at := time.Unix(1789729816, 0).UTC()
	s, err := Normalize(Exterior, []byte(`{"data": {"timestamp": {"seconds": "1789729816"},
		"centralLock": "LOCK_STATUS_UNLOCKED", "frontLeftDoor": "OPEN_STATUS_AJAR",
		"rearRightWindow": "OPEN_STATUS_OPEN", "tailgate": "OPEN_STATUS_SOMETHING", "alarm": "ALARM_STATUS_IDLE"}}`))
	want := core.Snapshot{
		Covers:  core.FieldDoors | core.FieldWindows,
		Doors:   core.Some(core.Doors{Locked: core.Some(false, at), FrontLeft: core.Some(core.Ajar, at)}, at),
		Windows: core.Some(core.Windows{RearRight: core.Some(core.Open, at)}, at),
	}
	if err != nil || !reflect.DeepEqual(s, want) {
		t.Errorf("got  %+v, %v\nwant %+v", s, err, want)
	}
	s, err = Normalize(Exterior, []byte(`{"data": {"timestamp": {"seconds": "1789729816"}, "centralLock": "LOCK_STATUS_UNSPECIFIED"}}`))
	if err != nil || !reflect.DeepEqual(s, core.Snapshot{Covers: core.FieldDoors | core.FieldWindows}) {
		t.Errorf("nothing told: %+v, %v", s, err)
	}
}

func TestNormalizeInvalidJSON(t *testing.T) {
	for _, d := range Domains() {
		_, err := Normalize(d, []byte("["))
		if d == Health {
			_, err = NormalizeHealth(d, []byte("["))
		}
		if err == nil {
			t.Errorf("%s: invalid JSON accepted", d)
		}
	}
}

func TestNormalizer(t *testing.T) {
	var n Normalizer
	// Neither the health nor the exterior is a reading of the derivation.
	if got := n.Kinds(); len(got) != len(Domains())-2 || got[0] != (core.Kind{Source: Source, Endpoint: string(Battery)}) ||
		slices.Contains(got, core.Kind{Source: Source, Endpoint: string(Health)}) ||
		slices.Contains(got, core.Kind{Source: Source, Endpoint: string(Exterior)}) {
		t.Errorf("kinds %v", got)
	}
	if got := n.StateKinds(); len(got) != 1 || got[0] != (core.Kind{Source: Source, Endpoint: "exterior"}) {
		t.Errorf("state kinds %v", got)
	}
	if got := n.HealthKinds(); len(got) != 1 || got[0] != (core.Kind{Source: Source, Endpoint: "health"}) {
		t.Errorf("health kinds %v", got)
	}
	if h, err := n.NormalizeHealth(core.Kind{Source: Source, Endpoint: "health"}, fixture(t, "telemetry/health.json")); err != nil ||
		!h.DaysToService.OK {
		t.Errorf("health: %+v, %v", h, err)
	}
	if _, err := n.NormalizeHealth(core.Kind{Source: "volvo", Endpoint: "health"}, nil); err == nil {
		t.Error("another source's health read")
	}
	if got := n.EnergyKinds(); len(got) != 1 || got[0] != (core.Kind{Source: Source, Endpoint: "battery"}) {
		t.Errorf("energy kinds %v", got)
	}
	odo := fixture(t, "telemetry/odometer.json")
	got, err := n.Normalize(core.Kind{Source: Source, Endpoint: "odometer"}, odo)
	if err != nil || !got.OdometerKm.OK || got.OdometerKm.V != 122952.704 {
		t.Errorf("odometer: %+v, %v", got.OdometerKm, err)
	}
	if _, err := n.Normalize(core.Kind{Source: "volvo", Endpoint: "odometer"}, odo); err == nil {
		t.Error("another source's response read")
	}
}

// TestNormalizeSimulator: the Polestar of demonstration, parked, driving, then on a DC
// charge, read through the client.
func TestNormalizeSimulator(t *testing.T) {
	c, clk := newSim(t, polestarapi.DefaultLimits())
	ctx := context.Background()
	account := devCredential().AccountID
	read := func(d Domain) core.Snapshot {
		t.Helper()
		raw, err := c.Fetch(ctx, token(ctx, t, c), account, simVIN, d)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Normalize(d, raw)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	b := read(Battery)
	if b.SoC.V != 64 || b.Charging.V != core.ChargingIdle || !b.PowerW.OK || b.PowerW.V != 0 ||
		b.ModelYear.V != 2024 || b.SoC.At.IsZero() {
		t.Errorf("parked: %+v", b)
	}
	if e := read(Availability); e.Engine.V != core.EngineStopped {
		t.Errorf("parked: engine %+v", e.Engine)
	}
	if ts := read(TargetSoC); ts.TargetSoC.V != 80 {
		t.Errorf("target %+v", ts.TargetSoC)
	}
	odo := read(Odometer).OdometerKm.V

	clk.Advance(2*time.Hour + 10*time.Minute) // on the way to work
	if e := read(Availability); !e.Engine.OK || e.Engine.V != core.EngineRunning {
		t.Errorf("driving: engine %+v", e.Engine)
	}

	clk.Advance(9*time.Hour + 30*time.Minute) // at the fast charger
	b = read(Battery)
	if b.Charging.V != core.ChargingActive || b.ChargeType.V != core.DC || b.Connection.V != core.Connected ||
		!b.PowerW.OK || b.PowerW.V <= 0 {
		t.Errorf("DC charge: %+v", b)
	}
	if e := read(Availability); e.Engine.V != core.EngineStopped {
		t.Errorf("charging: engine %+v", e.Engine)
	}
	if got := read(Odometer).OdometerKm.V; got-odo < 14.9 {
		t.Errorf("odometer %v, then %v: 15 km driven", odo, got)
	}
	if p := read(Location).Position; !p.OK || p.V.Lat != 57.7075 {
		t.Errorf("position %+v: the fast charger", p)
	}
}

func TestFingerprint(t *testing.T) {
	fp := func(raw []byte) []byte {
		t.Helper()
		f, err := Fingerprint(raw)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	battery := fixture(t, "telemetry/battery.json")
	reupload := strings.NewReplacer(
		`"seconds": "1789729847"`, `"seconds": "1789730447"`,
		`"metaReceivedAt": "2026-09-18T11:10:55.531Z"`, `"metaReceivedAt": "2026-09-18T11:20:55.531Z"`,
		`"metaEventId": "shardId-000000000000:0`, `"metaEventId": "shardId-000000000000:1`,
	)
	again := []byte(reupload.Replace(string(battery)))
	if bytes.Equal(again, battery) {
		t.Fatal("the fixture changed: the replacements no longer apply")
	}
	if !bytes.Equal(fp(battery), fp(again)) {
		t.Error("the same values uploaded again: different fingerprints")
	}
	if lower := []byte(strings.Replace(string(battery), `"batteryChargeLevelPercentage": 52`, `"batteryChargeLevelPercentage": 51`, 1)); bytes.Equal(fp(battery), fp(lower)) {
		t.Error("another SoC: same fingerprint")
	}
	// target-soc dates its value too, deeper.
	target := fixture(t, "charging/target-soc.json")
	moved := []byte(strings.ReplaceAll(string(target), `"seconds": "1789729185"`, `"seconds": "1789730000"`))
	if bytes.Equal(target, moved) || !bytes.Equal(fp(target), fp(moved)) {
		t.Error("target-soc: a nested timestamp changes the fingerprint")
	}
	if _, err := Fingerprint([]byte("{")); err == nil {
		t.Error("invalid JSON accepted")
	}
}

// TestFingerprintSimulator: a parked car uploads its battery again every 10 minutes,
// unchanged, which makes no new snapshot.
func TestFingerprintSimulator(t *testing.T) {
	c, clk := newSim(t, polestarapi.DefaultLimits())
	ctx := context.Background()
	tok, account := token(ctx, t, c), devCredential().AccountID
	first, err := c.Fetch(ctx, tok, account, simVIN, Battery)
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(25 * time.Minute)
	second, err := c.Fetch(ctx, tok, account, simVIN, Battery)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Fingerprint(first)
	b, _ := Fingerprint(second)
	if bytes.Equal(first, second) || !bytes.Equal(a, b) {
		t.Errorf("parked: responses equal %v, fingerprints equal %v", bytes.Equal(first, second), bytes.Equal(a, b))
	}
}

// TestUploadOf: the upload of the real responses, without the VIN nor any value.
func TestUploadOf(t *testing.T) {
	for _, tt := range []struct {
		name   string
		at     int64
		fields []string
	}{
		{"telemetry/location.json", 1789729422, []string{"altitude", "coordinate.latitude", "coordinate.longitude", "heading", "speed"}},
		{"charging/target-soc.json", 1789729185, []string{
			"id", "targetSoc.batteryChargeTargetLevel", "targetSoc.chargeTargetLevelSettingType", "targetSoc.id",
			"targetSoc.source", "targetSoc.updatedAt", "updatedAt",
		}},
	} {
		u, err := UploadOf(fixture(t, tt.name))
		if err != nil || u.At.Unix() != tt.at || u.ReceivedAt.IsZero() || u.ModelYear != 2023 || !slices.Equal(u.Fields, tt.fields) {
			t.Errorf("%s: %+v, %v", tt.name, u, err)
		}
	}
	u, err := UploadOf([]byte(`{"data":{"ampLimit":{"updatedAtTimestamp":{"seconds":"1"}}},"meta":{}}`))
	if err != nil || !u.At.IsZero() || !u.ReceivedAt.IsZero() || u.ModelYear != 0 || !slices.Equal(u.Fields, []string{"ampLimit.updatedAtTimestamp"}) {
		t.Errorf("no upload's keys: %+v, %v", u, err)
	}
	if _, err := UploadOf([]byte(`{"data":[]}`)); err == nil {
		t.Error("data not an object: no error")
	}
}
