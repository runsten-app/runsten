package pleos

import (
	"testing"
	"time"

	"runsten/internal/core"
)

var at = time.Date(2026, 11, 16, 22, 18, 41, 0, time.UTC)

// raw answers a response whose data object holds the given fields, sent at at: the
// API's envelope with its meta half, which the fingerprint leaves out. The fields are
// the contents of the data object, the car's send first.
func raw(fields string) []byte {
	return []byte(`{"data":{"timestamp":"2026-11-16T22:18:41Z",` + fields + `},
		"meta":{"code":200,"message":"Success","requestId":"835bdbd4","timestamp":"2026-11-17T07:14:00Z"}}`)
}

// sent answers consent's response, which is no send of the car's: no timestamp, the
// meta half at its place.
func unsent(fields string) []byte {
	return []byte(`{"data":{` + fields + `},
		"meta":{"code":200,"message":"Success","requestId":"835bdbd4","timestamp":"2026-11-17T07:14:00Z"}}`)
}

func TestNormalizeBatteries(t *testing.T) {
	s, err := Normalize(Batteries, raw(`"charge":{"plugin":"connected","status":"fastCharging","charging":true,
		"stateOfCharge":42.5,"batteryCapacity":256320,"targetStateOfCharge":{"standard":80,"quick":100},
		"remainTime":54}`))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "the plug", s.Covers&core.FieldConnection != 0 && s.Connection.V == core.Connected && s.Connection.At.Equal(at))
	check(t, "the charge", s.Covers&core.FieldCharging != 0 && s.Charging.V == core.ChargingActive && s.Charging.At.Equal(at))
	check(t, "the charge's type", s.Covers&core.FieldChargeType != 0 && s.ChargeType.V == core.DC)
	check(t, "the state of charge", s.Covers&core.FieldSoC != 0 && s.SoC.V == 42.5 && s.SoC.At.Equal(at))
	check(t, "the target", s.Covers&core.FieldTargetSoC != 0 && s.TargetSoC.V == 80)
	check(t, "the capacity (kJ, assumption)", s.Covers&core.FieldCapacity != 0 && s.CapacityKWh.V == 256320.0/3600)
	check(t, "the time left (to the target, assumption)",
		s.Covers&core.FieldChargeDetails != 0 && s.TimeLeft.V.Duration == 54*time.Minute && s.TimeLeft.V.To == core.ToTarget)
	// No power, current nor voltage: a charge keeps the energy of its SoC alone.
	if s.Covers&core.FieldPower != 0 || s.Covers&core.FieldSoC != 0 && s.UsableSoC.OK {
		t.Errorf("the domain tells a power: %+v", s)
	}
}

// TestNormalizeBatteriesWords tells each word of charge.status apart (ADR 0049 §5).
func TestNormalizeBatteriesWords(t *testing.T) {
	for _, test := range []struct {
		status string
		want   core.ChargingStatus
	}{
		{"charging", core.ChargingActive},
		{"fastCharging", core.ChargingActive},
		{"wirelessCharging", core.ChargingActive},
		{"reservedCharging", core.ChargingScheduled},
		{"notCharging", core.ChargingIdle},
		{"v2lStop", core.ChargingIdle},
		{"v2lOperating", core.ChargingDischarging},
		{"v2xOperating", core.ChargingDischarging},
	} {
		s, err := Normalize(Batteries, raw(`"charge":{"status":"`+test.status+`","charging":false}`))
		if err != nil {
			t.Fatal(err)
		}
		if !s.Charging.OK || s.Charging.V != test.want {
			t.Errorf("%s: %v, want %v", test.status, s.Charging, test.want)
		}
		if test.want != core.ChargingActive && s.ChargeType.OK {
			t.Errorf("%s: the type of no active charge", test.status)
		}
	}
	// The type of the two charges that draw: AC the slow, DC the fast; wireless has
	// none read.
	for _, test := range []struct {
		status string
		want   core.ChargeType
	}{
		{"charging", core.AC},
		{"fastCharging", core.DC},
		{"wirelessCharging", core.NoCharge},
	} {
		s, err := Normalize(Batteries, raw(`"charge":{"status":"`+test.status+`","charging":true}`))
		if err != nil {
			t.Fatal(err)
		}
		if s.Charging.V != core.ChargingActive {
			t.Fatalf("%s: not charging", test.status)
		}
		if s.ChargeType.OK && s.ChargeType.V != test.want {
			t.Errorf("%s: %v, want %v", test.status, s.ChargeType.V, test.want)
		}
	}
	// charge.charging in support when the status tells nothing.
	s, err := Normalize(Batteries, raw(`"charge":{"charging":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Charging.OK || s.Charging.V != core.ChargingActive {
		t.Errorf("the bool in support: %v", s.Charging)
	}
	s, err = Normalize(Batteries, raw(`"charge":{"charging":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Charging.OK || s.Charging.V != core.ChargingIdle {
		t.Errorf("the bool at rest: %v", s.Charging)
	}
}

// TestNormalizeBatteriesAbsent: null and invalid tell nothing, never zero.
func TestNormalizeBatteriesAbsent(t *testing.T) {
	s, err := Normalize(Batteries, raw(`"charge":{"plugin":"invalid","stateOfCharge":null,
		"targetStateOfCharge":{"standard":null},"batteryCapacity":null,"remainTime":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.SoC.OK || s.TargetSoC.OK || s.CapacityKWh.OK || s.TimeLeft.OK || s.Connection.OK {
		t.Errorf("absent values told: %+v", s)
	}
	// A capacity of zero: the API tells it of no model, several of which answer none.
	// A time left of zero: what a car that does not charge tells.
	s, err = Normalize(Batteries, raw(`"charge":{"batteryCapacity":0,"remainTime":0}`)) //nolint:revive
	if err != nil {
		t.Fatal(err)
	}
	if s.CapacityKWh.OK {
		t.Error("the capacity of no model told")
	}
	if s.TimeLeft.OK {
		t.Error("a time left told of a car that does not charge")
	}
}

func TestNormalizePowertrains(t *testing.T) {
	s, err := Normalize(Powertrains, raw(`"engine":{"fuelLevel":null},
		"batteries":[{"type":"main","remainRatio":42.5}],
		"distanceToEmpties":[{"type":"EV","value":"261","unit":"km"}]`))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "the range", s.Covers == core.FieldRange && s.RangeKm.V == 261 && s.RangeKm.At.Equal(at))
	// The remainRatio is not the state of charge: the batteries tell it.
	if s.SoC.OK {
		t.Error("the powertrains told the state of charge")
	}
}

// TestNormalizeRangeUnits converts the range as its label says, the value a string;
// the words that tell nothing leave it absent.
func TestNormalizeRangeUnits(t *testing.T) {
	for _, test := range []struct {
		value, unit string
		want        float64
	}{
		{"261", "km", 261},
		{"163", "miles", 163 * 1.609344},
		{"26100", "meter", 26.1},
		{"857000", "feet", 857000 * 0.0003048},
	} {
		s, err := Normalize(Powertrains, raw(`"distanceToEmpties":[{"type":"EV","value":"`+test.value+`","unit":"`+test.unit+`"}]`))
		if err != nil {
			t.Fatal(err)
		}
		if !s.RangeKm.OK || s.RangeKm.V != test.want {
			t.Errorf("%s: %v, want %v", test.value+" "+test.unit, s.RangeKm.V, test.want)
		}
	}
	for _, test := range []string{
		`{"type":"EV","value":"invalid","unit":"km"}`,
		`{"type":"EV","value":"-","unit":"km"}`,
		`{"type":"EV","value":"261","unit":"invalid"}`,
		`{"type":"invalid","value":"261","unit":"km"}`,
		`{"type":"ICE","value":"261","unit":"km"}`,
	} {
		s, err := Normalize(Powertrains, raw(`"distanceToEmpties":[`+test+`]`))
		if err != nil {
			t.Fatal(err)
		}
		if s.RangeKm.OK {
			t.Errorf("%s: the range told", test)
		}
	}
	// The value is a number too, as some responses may write it.
	s, err := Normalize(Powertrains, raw(`"distanceToEmpties":[{"type":"EV","value":261,"unit":"km"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if !s.RangeKm.OK || s.RangeKm.V != 261 {
		t.Errorf("a numeric value: %v", s.RangeKm)
	}
}

func TestNormalizeDriving(t *testing.T) {
	s, err := Normalize(Driving, raw(`"ignition":{"status":"on"},"sleepMode":"off",
		"odometer":{"value":23065.5,"unit":"km"},"transmission":{"parkingPosition":false}`))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "the engine", s.Covers&core.FieldEngine != 0 && s.Engine.V == core.EngineRunning && s.Engine.At.Equal(at))
	check(t, "the odometer", s.Covers&core.FieldOdometer != 0 && s.OdometerKm.V == 23065.5 && s.OdometerKm.At.Equal(at))
	check(t, "awake", s.Covers&core.FieldSleep != 0 && s.Sleep.V == core.Awake)
	// Assumption (ADR 0049 §10.6): the mile label is trusted, whatever a British car
	// is said to put under it.
	s, err = Normalize(Driving, raw(`"odometer":{"value":5177.5,"unit":"mile"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := 5177.5
	want *= 1.609344
	if !s.OdometerKm.OK || s.OdometerKm.V != want {
		t.Errorf("the odometer in miles: %v, want %v", s.OdometerKm.V, want)
	}
	// invalid tells nothing.
	s, err = Normalize(Driving, raw(`"ignition":{"status":"invalid"},"sleepMode":"invalid",
		"odometer":{"value":"invalid","unit":"km"}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Engine.OK || s.Sleep.OK || s.OdometerKm.OK {
		t.Errorf("invalid told: %+v", s)
	}
}

func TestNormalizeLocations(t *testing.T) {
	s, err := Normalize(Locations, raw(`"location":{"latitude":48.8566,"longitude":2.3522,
		"altitude":10,"heading":270,"speed":{"value":0,"unit":"kph"},"type":0}`))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "the position", s.Covers == core.FieldPosition &&
		s.Position.V == (core.Position{Lat: 48.8566, Lon: 2.3522}) && s.Position.At.Equal(at))
	// The coordinate systems of Korea are not read: Bessel and CoordShift, and a word
	// the doc does not list.
	for _, typ := range []string{`1`, `2`, `"invalid"`} {
		s, err = Normalize(Locations, raw(`"location":{"latitude":48.8566,"longitude":2.3522,"type":`+typ+`}`))
		if err != nil {
			t.Fatal(err)
		}
		if s.Position.OK || s.Covers != 0 {
			t.Errorf("type %s: the position taken, or the previous one replaced (covers %v)", typ, s.Covers)
		}
	}
}

func TestNormalizeStatusDoors(t *testing.T) {
	s, err := Normalize(Status, raw(`"doors":[{"left":"locked","right":"locked"},{"left":"locked","right":"locked"}],
		"hood":"closed","trunk":"closed","sunroof":"closed","warnings":[]`))
	if err != nil {
		t.Fatal(err)
	}
	d := s.Doors.V
	check(t, "closed", d.FrontLeft.V == core.Closed && d.RearRight.V == core.Closed)
	check(t, "locked (every door's word)", d.Locked.OK && d.Locked.V)
	check(t, "the lids", d.Hood.V == core.Closed && d.Tailgate.V == core.Closed)
	if !s.Windows.OK || s.Windows.V.Sunroof.V != core.Closed {
		t.Errorf("the sunroof: %v", s.Windows)
	}
	// One door open, one closed (not locked): the lock falls.
	s, err = Normalize(Status, raw(`"doors":[{"left":"open","right":"locked"},{"left":"closed","right":"locked"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if d = s.Doors.V; d.FrontLeft.V != core.Open || d.RearLeft.V != core.Closed {
		t.Errorf("the open doors: %+v", d)
	} else if d.Locked.OK && d.Locked.V {
		t.Error("the car is locked with a door open")
	}
	// The words of the front alone, a two-door car's.
	s, err = Normalize(Status, raw(`"doors":[{"left":"locked","right":"locked"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if d = s.Doors.V; !d.Locked.OK || !d.Locked.V {
		t.Error("two doors locked do not lock the car")
	}
	// invalid tells nothing, of the doors and of the lids.
	s, err = Normalize(Status, raw(`"doors":[{"left":"invalid","right":"invalid"}],"hood":"invalid","sunroof":"invalid"`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Doors.V.FrontLeft.OK || s.Doors.V.Hood.OK || s.Windows.V.Sunroof.OK {
		t.Errorf("invalid told: %+v", s.Doors.V)
	}
}

// TestNormalizeStatusClimate reads the climate and the temperature the doc example
// gives, its unit the only one (assumption).
func TestNormalizeStatusClimate(t *testing.T) {
	s, err := Normalize(Status, raw(`"climateControl":{"mode":"auto","status":"on","speed":-1,
		"temperature":{"value":17,"unit":"C"}},"outsideTemperature":{"value":30,"unit":"C"}`))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "the climate runs", s.Covers&core.FieldClimate != 0 && s.Climate.V.On.V)
	check(t, "the setpoint", s.Climate.V.SetpointC.V == 17)
	check(t, "the outside", s.OutsideTempC.V == 30 && s.OutsideTempC.At.Equal(at))
	// Another unit is not read.
	s, err = Normalize(Status, raw(`"outsideTemperature":{"value":86,"unit":"F"}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.OutsideTempC.OK {
		t.Error("a Fahrenheit temperature told in degrees")
	}
}

func TestNormalizeConsent(t *testing.T) {
	s, err := Normalize(Consent, unsent(`"vehicles":[{"vin":"KNAPC81ABPP000001",
		"nickname":"My Car","name":"CV(CV)","type":"EV","sellname":"EV6"}]`))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "the powertrain", s.Covers&core.FieldModel != 0 && s.Powertrain.V == core.Electric)
	check(t, "the family", s.Family.V == "EV6")
	check(t, "the model year at the VIN's tenth", s.ModelYear.V == 2023)
	check(t, "the owner's name", s.Covers&core.FieldSheet != 0 && s.Sheet.V.Name == "My Car")
	// The brand is the connection's provider, which the normalizer does not know.
	if s.Brand.OK {
		t.Error("the brand told by the response")
	}
	// The other powertrains.
	for _, test := range []struct {
		typ  string
		want core.Powertrain
	}{
		{"PE", core.PlugInHybrid},
		{"GN", core.OtherPowertrain},
		{"UNKNOWN", core.OtherPowertrain},
	} {
		s, err = Normalize(Consent, unsent(`"vehicles":[{"vin":"KNAPC81ABPP000001","type":"`+test.typ+`"}]`))
		if err != nil {
			t.Fatal(err)
		}
		if s.Powertrain.V != test.want {
			t.Errorf("%s: %v, want %v", test.typ, s.Powertrain.V, test.want)
		}
	}
	// The entry of the vehicle read, not the list's first: another one may come
	// before it (the source keeps each vehicle's own).
	s, err = Normalize(Consent, unsent(`"vehicles":[{"vin":"OTHERV0000000000X","type":"GN"},
		{"vin":"KNAPC81ABPP000001","type":"EV","sellname":"EV6"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Powertrain.V != core.Electric || s.Family.V != "EV6" {
		t.Errorf("the wrong entry read: %+v", s)
	}
}

// TestFingerprint leaves the envelope and the send times out: the same values sent
// again bring nothing new.
func TestFingerprint(t *testing.T) {
	a, err := Fingerprint(raw(`"charge":{"stateOfCharge":42.5}`))
	if err != nil {
		t.Fatal(err)
	}
	// The same values, sent again three hours later.
	b, err := Fingerprint([]byte(`{"data":{"timestamp":"2026-11-17T01:18:41Z","charge":{"stateOfCharge":42.5}},
		"meta":{"code":200,"message":"Success"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("fingerprint of the same values differs")
	}
	c, err := Fingerprint(raw(`"charge":{"stateOfCharge":43}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(c) {
		t.Error("a new value brings nothing")
	}
}

// TestUploadOf reads what a response tells of the car's send, for the logs: no value,
// no model year (the responses hold neither VIN nor model).
func TestUploadOf(t *testing.T) {
	u, err := UploadOf(raw(`"charge":{"plugin":"connected","stateOfCharge":42.5}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"charge.plugin", "charge.stateOfCharge"}
	if len(u.Fields) != len(want) || u.Fields[0] != want[0] || u.Fields[1] != want[1] || !u.At.Equal(at) {
		t.Errorf("fields %v, sent at %v", u.Fields, u.At)
	}
}

func check(t *testing.T, what string, ok bool) {
	t.Helper()
	if !ok {
		t.Errorf("%s: not told", what)
	}
}
