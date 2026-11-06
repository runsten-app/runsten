package bmw

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"runsten/internal/core"
)

func mustNormalize(t *testing.T, e Endpoint, raw string) core.Snapshot {
	t.Helper()
	s, err := Normalize(e, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNormalizeStringsAndNumbers(t *testing.T) {
	// The API writes every value as a string, the stream as JSON values: both read.
	for _, raw := range []string{
		`{"telematicData":{"vehicle.drivetrain.batteryManagement.header":{"value":"71.5","unit":"%","timestamp":"2026-10-01T08:00:00.000Z"},
		  "vehicle.vehicle.travelledDistance":{"value":"12034","unit":"km","timestamp":"2026-10-01T08:00:00Z"}}}`,
		`{"telematicData":{"vehicle.drivetrain.batteryManagement.header":{"value":71.5,"unit":"%","timestamp":"2026-10-01T08:00:00.000Z"},
		  "vehicle.vehicle.travelledDistance":{"value":12034,"unit":"km","timestamp":"2026-10-01T08:00:00Z"}}}`,
	} {
		s := mustNormalize(t, Stream, raw)
		at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
		if s.SoC != core.Some(71.5, at) || s.OdometerKm != core.Some(12034.0, at) {
			t.Errorf("%s: %+v", raw, s)
		}
		// Only what the response holds is covered.
		if s.Covers != core.FieldSoC|core.FieldOdometer {
			t.Errorf("covers %b", s.Covers)
		}
	}
}

func TestNormalizeSoCDisplayed(t *testing.T) {
	s := mustNormalize(t, Stream, `{"telematicData":{
		"vehicle.powertrain.electric.battery.stateOfCharge.displayed":{"value":55,"timestamp":"2026-10-01T08:00:00Z"}}}`)
	if s.SoC.V != 55 || s.Covers&core.FieldSoC == 0 {
		t.Errorf("Neue Klasse's SoC: %+v", s.SoC)
	}
}

func TestNormalizePosition(t *testing.T) {
	both := `{"telematicData":{
		"vehicle.cabin.infotainment.navigation.currentLocation.latitude":{"value":50.85,"timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.cabin.infotainment.navigation.currentLocation.longitude":{"value":4.35,"timestamp":"2026-10-01T08:00:00.000Z"}}}`
	if s := mustNormalize(t, Stream, both); s.Position.V != (core.Position{Lat: 50.85, Lon: 4.35}) {
		t.Errorf("same time: %+v", s.Position)
	}
	// A latitude of a newer message than the longitude: no position yet.
	apart := `{"telematicData":{
		"vehicle.cabin.infotainment.navigation.currentLocation.latitude":{"value":50.86,"timestamp":"2026-10-01T08:00:30Z"},
		"vehicle.cabin.infotainment.navigation.currentLocation.longitude":{"value":4.35,"timestamp":"2026-10-01T08:00:00Z"}}}`
	if s := mustNormalize(t, Stream, apart); s.Covers&core.FieldPosition != 0 {
		t.Errorf("apart: %+v", s.Position)
	}
}

func TestNormalizeEngine(t *testing.T) {
	for _, c := range []struct {
		ignition, moving string
		want             core.EngineState
		covered          bool
	}{
		{"ASN_isTrue", "", core.EngineRunning, true},
		{"ASN_isFalse", "", core.EngineStopped, true},
		{"ASN_isFalse", "ASN_isTrue", core.EngineRunning, true},
		{"", "ASN_isTrue", core.EngineRunning, true},
		{"", "ASN_isFalse", "", false}, // not moving: the ignition may be on
		{"ASN_isUnknown", "", "", false},
	} {
		raw := `{"telematicData":{`
		if c.ignition != "" {
			raw += `"vehicle.drivetrain.engine.isIgnitionOn":{"value":"` + c.ignition + `","timestamp":"2026-10-01T08:00:00Z"}`
		}
		if c.moving != "" {
			if c.ignition != "" {
				raw += ","
			}
			raw += `"vehicle.isMoving":{"value":"` + c.moving + `","timestamp":"2026-10-01T08:00:05Z"}`
		}
		raw += `}}`
		s := mustNormalize(t, Stream, raw)
		if covered := s.Covers&core.FieldEngine != 0; covered != c.covered || s.Engine.V != c.want {
			t.Errorf("%s %s: %+v covered %v", c.ignition, c.moving, s.Engine, covered)
		}
	}
}

func TestNormalizeCharging(t *testing.T) {
	charge := func(status, method, dc string) string {
		return `{"telematicData":{
			"vehicle.drivetrain.electricEngine.charging.status":{"value":"` + status + `","timestamp":"2026-10-01T08:00:00Z"},
			"vehicle.drivetrain.electricEngine.charging.method":{"value":"` + method + `","timestamp":"2026-10-01T08:00:00Z"},
			"vehicle.body.chargingPort.dcStatus":{"value":"` + dc + `","timestamp":"2026-10-01T08:00:00Z"},
			"vehicle.body.chargingPort.status":{"value":"CONNECTED","timestamp":"2026-10-01T08:00:00Z"}}}`
	}
	for _, c := range []struct {
		status, method, dc string
		want               core.ChargingStatus
		typ                core.ChargeType
	}{
		{"CHARGINGACTIVE", "AC_TYPE2PLUG", "DISCONNECTED", core.ChargingActive, core.AC},
		{"CHARGINGACTIVE", "DC", "CONNECTED", core.ChargingActive, core.DC},
		{"INITIALIZATION", "NOCHARGING", "CONNECTED", core.ChargingActive, core.DC},
		{"CHARGINGENDED", "NOCHARGING", "DISCONNECTED", core.ChargingDone, core.NoCharge},
		{"CHARGINGPAUSED", "AC_TYPE2PLUG", "DISCONNECTED", core.ChargingIdle, core.NoCharge},
		{"CHARGINGERROR", "", "", core.ChargingError, core.NoCharge},
	} {
		s := mustNormalize(t, Stream, charge(c.status, c.method, c.dc))
		if s.Charging.V != c.want || s.ChargeType.V != c.typ || s.Connection.V != core.Connected {
			t.Errorf("%s %s %s: %v %v %v", c.status, c.method, c.dc, s.Charging, s.ChargeType, s.Connection)
		}
	}
	// Without charging.status, hvStatus tells.
	s := mustNormalize(t, Stream, `{"telematicData":{
		"vehicle.drivetrain.electricEngine.charging.hvStatus":{"value":"FINISHED_FULLY_CHARGED","timestamp":"2026-10-01T08:00:00Z"}}}`)
	if s.Charging.V != core.ChargingDone {
		t.Errorf("hvStatus: %+v", s.Charging)
	}
	// An unknown word covers nothing.
	s = mustNormalize(t, Stream, `{"telematicData":{
		"vehicle.drivetrain.electricEngine.charging.status":{"value":"SOMETHING","timestamp":"2026-10-01T08:00:00Z"}}}`)
	if s.Covers&core.FieldCharging != 0 {
		t.Errorf("unknown status covered: %+v", s.Charging)
	}
}

func TestNormalizeChargeDetails(t *testing.T) {
	s := mustNormalize(t, Stream, `{"telematicData":{
		"vehicle.drivetrain.electricEngine.charging.acAmpere":{"value":"16","unit":"A","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.drivetrain.electricEngine.charging.acVoltage":{"value":"230","unit":"V","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.drivetrain.electricEngine.charging.phaseNumber":{"value":"3-PHASES","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.drivetrain.electricEngine.charging.timeToFullyCharged":{"value":"95","unit":"min","timestamp":"2026-10-01T08:00:00Z"}}}`)
	if s.CurrentA.V != 16 || s.VoltageV.V != 230 || s.Phases.V != 3 ||
		s.TimeLeft.V != (core.TimeLeft{Duration: 95 * time.Minute, To: core.ToTarget}) {
		t.Errorf("details: %+v %+v %+v %+v", s.CurrentA, s.VoltageV, s.Phases, s.TimeLeft)
	}
	// Not charging: no voltage, nothing of the charger.
	s = mustNormalize(t, Stream, `{"telematicData":{
		"vehicle.drivetrain.electricEngine.charging.acVoltage":{"value":0,"timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.drivetrain.electricEngine.charging.phaseNumber":{"value":"NO_CHARGING","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.drivetrain.electricEngine.charging.timeToFullyCharged":{"value":0,"timestamp":"2026-10-01T08:00:00Z"}}}`)
	if s.Covers&core.FieldChargeDetails == 0 || s.VoltageV.OK || s.Phases.OK || s.TimeLeft.OK {
		t.Errorf("idle details: %+v", s)
	}
}

func TestNormalizeBasicData(t *testing.T) {
	s := mustNormalize(t, BasicData, `{"vin":"WMW11DJ0X02T00001","brand":"MINI","modelName":"Cooper SE","propulsionType":"BEV",
		"constructionDate":"2024-08-12T00:00:00.000Z"}`)
	if s.Brand.V != "mini" || s.Family.V != "Cooper" || s.Trim.V != "se" || s.ModelYear.V != 2025 || !s.BatteryElectric() {
		t.Errorf("mini: %+v", s)
	}
	s = mustNormalize(t, BasicData, `{"brand":"BMW","modelName":"X5 xDrive50e","propulsionType":"PHEV","constructionDate":"2024-02-01T00:00:00Z"}`)
	if !s.PlugInHybrid() || s.ModelYear.V != 2024 {
		t.Errorf("phev: %+v", s)
	}
	// Nothing of the sheet nor the capacity told: covered, unknown.
	if s.Covers&(core.FieldCapacity|core.FieldSheet) != core.FieldCapacity|core.FieldSheet || s.CapacityKWh.OK || s.Sheet.OK {
		t.Errorf("phev's sheet: %+v", s)
	}
}

func TestNormalizeBasicDataSheet(t *testing.T) {
	// The OpenAPI's brand of the i models, BMWi; the capacity a string, or a number.
	for _, capacity := range []string{`"83.9"`, `83.9`} {
		s := mustNormalize(t, BasicData, `{"brand":"BMWi","modelName":"i4 eDrive40","propulsionType":"BEV",
			"steering":"LL","colourCode":"C4P","colourCodeRaw":"C4P1","hvsMaxEnergyAbsolute":`+capacity+`}`)
		if s.Brand.V != "bmw" {
			t.Errorf("brand: %+v", s.Brand)
		}
		if s.CapacityKWh != core.Some(83.9, time.Time{}) {
			t.Errorf("capacity %s: %+v", capacity, s.CapacityKWh)
		}
		if s.Sheet.V != (core.Sheet{Colour: "C4P", Steering: core.LeftHand}) {
			t.Errorf("sheet: %+v", s.Sheet)
		}
	}
	s := mustNormalize(t, BasicData, `{"brand":"MINI","steering":"RL","colourCodeRaw":"B71","hvsMaxEnergyAbsolute":"0"}`)
	if s.Sheet.V != (core.Sheet{Colour: "B71", Steering: core.RightHand}) || s.CapacityKWh.OK {
		t.Errorf("right hand, raw colour, no capacity: %+v", s)
	}
}

func TestFingerprintIgnoresTimestamps(t *testing.T) {
	a, err := Fingerprint([]byte(`{"telematicData":{"k":{"value":"1","timestamp":"2026-10-01T08:00:00Z"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Fingerprint([]byte(`{"telematicData":{"k":{"value":"1","timestamp":"2026-10-01T09:00:00Z"}}}`))
	c, _ := Fingerprint([]byte(`{"telematicData":{"k":{"value":"2","timestamp":"2026-10-01T08:00:00Z"}}}`))
	if !bytes.Equal(a, b) || bytes.Equal(a, c) {
		t.Error("fingerprint")
	}

	// The same coordinates, apart then of the same timestamp: the position becomes
	// readable, which is news.
	pos := func(latAt, lonAt string) []byte {
		f, err := Fingerprint([]byte(`{"telematicData":{
			"vehicle.cabin.infotainment.navigation.currentLocation.latitude":{"value":50.85,"timestamp":"` + latAt + `"},
			"vehicle.cabin.infotainment.navigation.currentLocation.longitude":{"value":4.35,"timestamp":"` + lonAt + `"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	apart, together := pos("2026-10-01T08:00:30Z", "2026-10-01T08:00:00Z"), pos("2026-10-01T08:00:30Z", "2026-10-01T08:00:30.000Z")
	if bytes.Equal(apart, together) {
		t.Error("a position paired since has the fingerprint of one apart")
	}
	if !bytes.Equal(together, pos("2026-10-01T09:00:00Z", "2026-10-01T09:00:00Z")) {
		t.Error("a position paired again, unchanged, is news")
	}
}

func TestUploadOf(t *testing.T) {
	u, err := UploadOf(Stream, []byte(`{"telematicData":{
		"vehicle.vehicle.travelledDistance":{"value":1,"timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.drivetrain.batteryManagement.header":{"value":1,"timestamp":"2026-10-01T08:05:00Z"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !u.At.Equal(time.Date(2026, 10, 1, 8, 5, 0, 0, time.UTC)) ||
		!slices.Equal(u.Fields, []string{DescSoC, DescOdometer}) {
		t.Errorf("upload: %+v", u)
	}
}

func TestDecodePicture(t *testing.T) {
	for raw, ok := range map[string]bool{
		`{"type":"image/png","image":"iVBORw0KGgo="}`: true,
		`{"type":"text/html","image":"PGh0bWw+"}`:     false,
		`{"type":"image/png","image":""}`:             false,
		`not json`:                                    false,
	} {
		if _, err := DecodePicture([]byte(raw)); (err == nil) != ok {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestNormalizeDoorsAndWindows(t *testing.T) {
	at := func(sec int) time.Time { return time.Date(2026, 10, 1, 8, 0, sec, 0, time.UTC) }
	s := mustNormalize(t, Stream, `{"telematicData":{
		"vehicle.cabin.door.row1.driver.isOpen":{"value":"CLOSED","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.cabin.door.row1.passenger.isOpen":{"value":"OPEN","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.cabin.door.row2.driver.isOpen":{"value":false,"timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.cabin.door.row2.passenger.isOpen":{"value":"INVALID","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.body.hood.isOpen":{"value":"CLOSED","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.body.trunk.isOpen":{"value":"INTERMEDIATE","timestamp":"2026-10-01T08:00:10Z"},
		"vehicle.cabin.door.lock.status":{"value":"SECURED","timestamp":"2026-10-01T08:00:05Z"},
		"vehicle.cabin.window.row1.driver.status":{"value":"INTERMEDIATE","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.cabin.window.row2.passenger.status":{"value":"CLOSED","timestamp":"2026-10-01T08:00:00Z"},
		"vehicle.cabin.sunroof.status":{"value":"OPEN","timestamp":"2026-10-01T08:00:20Z"}}}`)
	if s.Covers != core.FieldDoors|core.FieldWindows {
		t.Errorf("covers %b", s.Covers)
	}
	wantDoors := core.Doors{
		Locked:    core.Some(true, at(5)),
		FrontLeft: core.Some(core.Closed, at(0)), FrontRight: core.Some(core.Open, at(0)),
		RearLeft: core.Some(core.Closed, at(0)), // a boolean read too
		Hood:     core.Some(core.Closed, at(0)), Tailgate: core.Some(core.Ajar, at(10)),
	}
	if s.Doors != core.Some(wantDoors, at(10)) {
		t.Errorf("doors %+v", s.Doors)
	}
	wantWindows := core.Windows{
		FrontLeft: core.Some(core.Ajar, at(0)), RearRight: core.Some(core.Closed, at(0)),
		Sunroof: core.Some(core.Open, at(20)),
	}
	if s.Windows != core.Some(wantWindows, at(20)) {
		t.Errorf("windows %+v", s.Windows)
	}
	if !s.Open() {
		t.Error("not open")
	}

	for _, c := range []struct {
		word   string
		locked core.Value[bool]
	}{
		{"LOCKED", core.Some(true, at(0))},
		{"UNLOCKED", core.Some(false, at(0))},
		{"SELECTIVE-LOCKED", core.Some(false, at(0))}, // the driver's door unlocked
		{"UNKNOWN", core.Value[bool]{}},
	} {
		s := mustNormalize(t, Stream, `{"telematicData":{
			"vehicle.cabin.door.lock.status":{"value":"`+c.word+`","timestamp":"2026-10-01T08:00:00Z"}}}`)
		if s.Covers != core.FieldDoors || s.Doors.V.Locked != c.locked {
			t.Errorf("%s: %+v covers %b", c.word, s.Doors, s.Covers)
		}
		if c.word == "UNKNOWN" && s.Doors.OK {
			t.Errorf("%s: doors told %+v", c.word, s.Doors)
		}
	}

	// Without one of them, neither is covered: the previous holds.
	if s := mustNormalize(t, Stream, `{"telematicData":{}}`); s.Covers&(core.FieldDoors|core.FieldWindows) != 0 {
		t.Errorf("covers %b", s.Covers)
	}
}

func TestNormalizePreconditioning(t *testing.T) {
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		word string
		want core.Value[core.Climate]
	}{
		{"heating", core.Some(core.Climate{Preconditioning: core.Some(true, at)}, at)},
		{"COOLING", core.Some(core.Climate{Preconditioning: core.Some(true, at)}, at)},
		{"ventilation", core.Some(core.Climate{Preconditioning: core.Some(true, at)}, at)},
		{"standby", core.Some(core.Climate{Preconditioning: core.Some(false, at)}, at)},
		{"inactive", core.Some(core.Climate{Preconditioning: core.Some(false, at)}, at)},
		{"", core.Value[core.Climate]{}},
	} {
		s := mustNormalize(t, Stream, `{"telematicData":{
			"vehicle.vehicle.preConditioning.activity":{"value":"`+c.word+`","timestamp":"2026-10-01T08:00:00Z"}}}`)
		if s.Covers != core.FieldClimate || s.Climate != c.want {
			t.Errorf("%q: %+v covers %b", c.word, s.Climate, s.Covers)
		}
		// CarData tells no temperature.
		if s.OutsideTempC.OK || s.CabinTempC.OK {
			t.Errorf("%q: temperatures %+v %+v", c.word, s.OutsideTempC, s.CabinTempC)
		}
	}
}

func TestContainerName(t *testing.T) {
	n := ContainerName()
	if len(n) != len("runsten-")+8 || !isRunstenContainer(n) || !isRunstenContainer("runsten") {
		t.Errorf("name %q", n)
	}
	if isRunstenContainer("runstenx") || isRunstenContainer("evcc") {
		t.Error("another's container taken for Runsten's")
	}
}
