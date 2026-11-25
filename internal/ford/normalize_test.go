package ford

import (
	"os"
	"testing"
	"time"

	"runsten/internal/core"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name) //nolint:gosec // the test's own fixtures
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func ts(s string) time.Time {
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return at
}

// TestNormalizeParked: a car parked and unplugged, each value dated from its metric's
// own send. The 12 V battery's level is not the car's; the station's kind and the time
// left of a charge days old, and the charger's idle current, are not this moment's.
func TestNormalizeParked(t *testing.T) {
	s, err := Normalize(Telemetry, fixture(t, "telemetry.json"))
	if err != nil {
		t.Fatal(err)
	}
	sent := ts("2026-03-11T07:55:07Z")
	if !s.Engine.OK || s.Engine.V != core.EngineStopped || !s.Engine.At.Equal(ts("2026-03-11T07:55:12Z")) {
		t.Errorf("engine %+v", s.Engine)
	}
	if !s.SoC.OK || s.SoC.V != 64.5 || !s.SoC.At.Equal(sent) {
		t.Errorf("soc %+v, want the displayed level, never the 12 V battery's", s.SoC)
	}
	if !s.RangeKm.OK || s.RangeKm.V != 171 || !s.OdometerKm.OK || s.OdometerKm.V != 4804 {
		t.Errorf("range %+v, odometer %+v", s.RangeKm, s.OdometerKm)
	}
	if !s.Position.OK || s.Position.V != (core.Position{Lat: 45.764, Lon: 4.8357}) {
		t.Errorf("position %+v", s.Position)
	}
	if !s.TargetSoC.OK || s.TargetSoC.V != 80 || !s.OutsideTempC.OK || s.OutsideTempC.V != 7 {
		t.Errorf("target %+v, outside %+v", s.TargetSoC, s.OutsideTempC)
	}
	if !s.Charging.OK || s.Charging.V != core.ChargingIdle || !s.Connection.OK || s.Connection.V != core.Disconnected {
		t.Errorf("charging %+v, connection %+v", s.Charging, s.Connection)
	}
	// Covered and absent: a previous charge's values are cleared.
	for name, f := range map[string]core.Field{
		"charge type": core.FieldChargeType, "power": core.FieldPower, "charge details": core.FieldChargeDetails,
	} {
		if s.Covers&f == 0 {
			t.Errorf("%s not covered", name)
		}
	}
	if s.ChargeType.OK || s.PowerW.OK || s.CurrentA.OK || s.VoltageV.OK || s.TimeLeft.OK {
		t.Errorf("a charge's values while none runs: %+v %+v %+v %+v %+v", s.ChargeType, s.PowerW, s.CurrentA, s.VoltageV, s.TimeLeft)
	}
	// The capacity's meaning is not known: left in the raw response.
	if s.Covers&core.FieldCapacity != 0 || s.CapacityKWh.OK {
		t.Errorf("capacity %+v", s.CapacityKWh)
	}

	// The doors named by the driver's side are placed since all are closed; the lock is
	// the car's summary, not a door's.
	d := s.Doors.V
	if !s.Doors.OK || !d.Locked.OK || d.Locked.V {
		t.Errorf("lock %+v", d.Locked)
	}
	for name, door := range map[string]core.Value[core.Opening]{
		"front left": d.FrontLeft, "front right": d.FrontRight, "rear left": d.RearLeft, "rear right": d.RearRight,
		"tailgate": d.Tailgate, "hood": d.Hood,
	} {
		if !door.OK || door.V != core.Closed {
			t.Errorf("door %s: %+v", name, door)
		}
	}
	w := s.Windows.V
	if !s.Windows.OK || w.FrontLeft.V != core.Closed || w.FrontRight.V != core.Open || w.RearLeft.OK {
		t.Errorf("windows %+v", w)
	}
}

// TestNormalizeCharging: a charge running, its power the charger's voltage times its
// current; RUN is a car's word for an ignition on; a door ajar on the driver's side is
// not placed, the doors named by their place are.
func TestNormalizeCharging(t *testing.T) {
	s, err := Normalize(Telemetry, fixture(t, "telemetry-charging.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Engine.OK || s.Engine.V != core.EngineRunning {
		t.Errorf("engine %+v", s.Engine)
	}
	if !s.Charging.OK || s.Charging.V != core.ChargingActive || s.Connection.V != core.Connected {
		t.Errorf("charging %+v, connection %+v", s.Charging, s.Connection)
	}
	if !s.ChargeType.OK || s.ChargeType.V != core.AC {
		t.Errorf("charge type %+v", s.ChargeType)
	}
	if !s.PowerW.OK || s.PowerW.V != 10500 || !s.PowerW.At.Equal(ts("2026-03-11T19:10:00Z")) {
		t.Errorf("power %+v", s.PowerW)
	}
	if s.CurrentA.V != 30 || s.VoltageV.V != 350 {
		t.Errorf("current %+v, voltage %+v", s.CurrentA, s.VoltageV)
	}
	if !s.TimeLeft.OK || s.TimeLeft.V != (core.TimeLeft{Duration: 95 * time.Minute, To: core.ToFull}) {
		t.Errorf("time left %+v", s.TimeLeft)
	}
	d := s.Doors.V
	if !d.Locked.OK || !d.Locked.V || d.FrontLeft.OK || d.FrontRight.OK || d.RearRight.OK {
		t.Errorf("doors %+v", d)
	}
	if !d.RearLeft.OK || d.RearLeft.V != core.Closed {
		t.Errorf("rear left %+v", d.RearLeft)
	}
	// What the response lacks keeps its previous value.
	if s.Covers&(core.FieldPosition|core.FieldRange|core.FieldWindows|core.FieldClimate|core.FieldTargetSoC) != 0 {
		t.Errorf("covers %b", s.Covers)
	}
}

// TestNormalizeTolerant: a metric of another shape, or a word this does not know, is
// left out, never the whole response; a position without a fix is unknown.
func TestNormalizeTolerant(t *testing.T) {
	raw := []byte(`{"metrics": {
		"odometer": {"updateTime": "2026-03-11T07:55:07Z", "value": "many"},
		"ignitionStatus": {"updateTime": "2026-03-11T07:55:07Z", "value": "UNRECOGNIZED"},
		"xevBatteryChargeDisplayStatus": {"value": "SOMETHING_NEW"},
		"xevBatteryStateOfCharge": {"updateTime": "2026-03-11T07:55:07Z", "value": 40},
		"position": {"updateTime": "2026-03-11T07:55:07Z", "value": {"location": {"lat": 0, "lon": 0}, "gpsDimension": "NONE"}},
		"doorStatus": "none"
	}}`)
	s, err := Normalize(Telemetry, raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.OdometerKm.OK || s.Covers&core.FieldOdometer != 0 || s.Covers&core.FieldEngine != 0 {
		t.Errorf("odometer %+v, covers %b", s.OdometerKm, s.Covers)
	}
	if s.Covers&core.FieldCharging == 0 || s.Charging.OK {
		t.Errorf("charging %+v: an unknown word is unknown", s.Charging)
	}
	if !s.SoC.OK || s.SoC.V != 40 {
		t.Errorf("soc %+v", s.SoC)
	}
	if s.Covers&core.FieldPosition == 0 || s.Position.OK {
		t.Errorf("position %+v: without a fix, unknown", s.Position)
	}
	if _, err := Normalize(Telemetry, []byte(`{"error": 500}`)); err == nil {
		t.Error("a response without metrics normalized")
	}
	if _, err := Normalize("wallbox", []byte(`{}`)); err == nil {
		t.Error("an unknown endpoint normalized")
	}
}

// TestNormalizeLegacyNames: the metrics' former names are read when the response lacks
// the others.
func TestNormalizeLegacyNames(t *testing.T) {
	s, err := Normalize(Telemetry, []byte(`{"metrics": {
		"evBatteryChargeDisplayStatus": {"updateTime": "2026-03-11T07:55:07Z", "value": "FAST_CHARGING"},
		"evPlugChargerStatus": {"updateTime": "2026-03-11T07:55:07Z", "value": "CONNECTED"},
		"ambientTemp": {"updateTime": "2026-03-11T07:55:07Z", "value": -3.5}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Charging.V != core.ChargingActive || s.Connection.V != core.Connected || s.OutsideTempC.V != -3.5 {
		t.Errorf("%+v %+v %+v", s.Charging, s.Connection, s.OutsideTempC)
	}
}

// TestNormalizeGarage: the car of the grant, as an object, in a list or under vehicles;
// its year a string or a number.
func TestNormalizeGarage(t *testing.T) {
	object := `{"vin": "WF0TESTPUMA000001", "nickName": "Bluebird", "color": "AGATE BLACK", "modelName": "Puma",
		"modelYear": "2025", "make": "Ford", "engineType": "BEV", "vehicleAuthorizationIndicator": 1}`
	for name, raw := range map[string]string{
		"object":   object,
		"list":     "[" + object + "]",
		"vehicles": `{"vehicles": [{"vin": "WF0TESTPUMA000001", "nickName": "Bluebird", "color": "AGATE BLACK", "modelName": "Puma", "modelYear": 2025, "make": "Ford", "engineType": "BEV"}]}`,
	} {
		s, err := Normalize(Garage, []byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if s.Brand.V != Brand || s.Family.V != "Puma" || s.ModelYear.V != 2025 || !s.BatteryElectric() {
			t.Errorf("%s: model %+v %+v %+v %+v", name, s.Brand, s.Family, s.ModelYear, s.Powertrain)
		}
		if s.Sheet.V.Name != "Bluebird" || s.Sheet.V.Colour != "AGATE BLACK" {
			t.Errorf("%s: sheet %+v", name, s.Sheet)
		}
		v, err := VehicleOf([]byte(raw))
		if err != nil || v.VIN != "WF0TESTPUMA000001" {
			t.Errorf("%s: vehicle %+v, %v", name, v, err)
		}
	}
	s, err := Normalize(Garage, []byte(`{"vin": "5LMTESTLNC0000001", "make": "Lincoln", "modelName": "Corsair", "engineType": "ICE"}`))
	if err != nil || s.Brand.OK || s.Powertrain.OK || s.ModelYear.OK {
		t.Errorf("a Lincoln: %+v, %v", s, err)
	}
	if _, err := VehicleOf([]byte(`{"vehicles": []}`)); err == nil {
		t.Error("an empty garage gave a vehicle")
	}
	if _, err := VehicleOf([]byte(`{"vin": "short"}`)); err == nil {
		t.Error("a garage without a VIN gave a vehicle")
	}
}

// TestNormalizeHealth: the four tyres' pressures and warnings, the recommended
// pressures; a van's twin wheel and a status the system does not support are left out.
func TestNormalizeHealth(t *testing.T) {
	h, err := NormalizeHealth(fixture(t, "telemetry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if h.Covers&core.HealthTyres == 0 || len(h.Tyres) != 4 {
		t.Fatalf("health %+v", h)
	}
	if fl := h.Tyres[core.FrontLeft]; fl.PressureKPa.V != 253 || fl.Warning.V != core.TyreOK {
		t.Errorf("front left %+v", fl)
	}
	if rl := h.Tyres[core.RearLeft]; rl.PressureKPa.V != 250 || rl.Warning.V != core.TyreLow {
		t.Errorf("rear left %+v", rl)
	}
	if rr := h.Tyres[core.RearRight]; rr.PressureKPa.V != 248 || rr.Warning.OK {
		t.Errorf("rear right %+v", rr)
	}
	if h.RecommendedFrontKPa.V != 250 || h.RecommendedRearKPa.V != 240 {
		t.Errorf("recommended %+v %+v", h.RecommendedFrontKPa, h.RecommendedRearKPa)
	}
	// A car that tells no tyre covers none.
	if h, err := NormalizeHealth(fixture(t, "telemetry-charging.json")); err != nil || h.Covers != 0 {
		t.Errorf("health %+v, %v", h, err)
	}
	n := Normalizer{}
	if _, err := n.NormalizeHealth(core.Kind{Source: Source, Endpoint: string(Garage)}, []byte(`{}`)); err == nil {
		t.Error("the garage gave a health")
	}
}

// TestFingerprintAndUpload: the same sends give the same fingerprint, a new send of the
// same values another; the upload is the latest send, with the metrics' names.
func TestFingerprintAndUpload(t *testing.T) {
	a := []byte(`{"updateTime":"2026-03-11T08:00:00Z","vin":"WF0TESTPUMA000001","metrics":{"odometer":{"updateTime":"2026-03-11T07:55:07Z","value":4804},"speed":{"updateTime":"2026-03-11T08:01:00Z","value":0}}}`)
	b := []byte(`{"metrics":{"speed":{"value":0,"updateTime":"2026-03-11T08:01:00Z"},"odometer":{"value":4804,"updateTime":"2026-03-11T07:55:07Z"}},"vin":"WF0TESTPUMA000001","updateTime":"2026-03-11T08:00:00Z"}`)
	c := []byte(`{"updateTime":"2026-03-11T08:05:00Z","vin":"WF0TESTPUMA000001","metrics":{"odometer":{"updateTime":"2026-03-11T08:05:00Z","value":4804},"speed":{"updateTime":"2026-03-11T08:01:00Z","value":0}}}`)
	fa, _ := Fingerprint(a)
	fb, _ := Fingerprint(b)
	fc, _ := Fingerprint(c)
	if string(fa) != string(fb) || string(fa) == string(fc) {
		t.Error("fingerprints: the same sends must match, a new send must not")
	}
	u, err := UploadOf(Telemetry, a)
	if err != nil || !u.At.Equal(ts("2026-03-11T08:01:00Z")) || len(u.Fields) != 2 || u.Fields[0] != "odometer" {
		t.Errorf("upload %+v, %v", u, err)
	}
	if u, err := UploadOf(Garage, []byte(`{"vin":"WF0TESTPUMA000001"}`)); err != nil || !u.At.IsZero() {
		t.Errorf("garage upload %+v, %v", u, err)
	}
}
