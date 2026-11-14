package renault

import (
	"testing"
	"time"

	"runsten/internal/core"
)

// The fixtures are renault-api's (MIT), real responses anonymised.

const zoe40Charging = `{"data":{"type":"Car","id":"VF1AAAAA555777999","attributes":{
	"timestamp":"2020-01-12T21:40:16Z","batteryLevel":60,"batteryTemperature":20,"batteryAutonomy":141,
	"batteryCapacity":0,"batteryAvailableEnergy":31,"plugStatus":1,"chargingStatus":1.0,
	"chargingRemainingTime":145,"chargingInstantaneousPower":27.0}}}`

const r5Parked = `{"data":{"id":"VYSP0000000000000","attributes":{"timestamp":"2026-05-01T06:07:54Z",
	"batteryLevel":73,"batteryAutonomy":261,"V2L_SystemStatusDisplay":0,"plugStatus":0,"chargingStatus":0.0,
	"chargingRemainingTime":3,"chargingRemainingTimeLastUpdateDateTime":"2026-04-30T16:16:10Z"}}}`

const zoe40Offset = `{"data":{"type":"Car","id":"VF1AAAAA555777999","attributes":{
	"timestamp":"2020-11-17T09:06:48+01:00","batteryLevel":50,"batteryAutonomy":128,"batteryCapacity":0,
	"batteryAvailableEnergy":0,"plugStatus":0,"chargingStatus":-1.0}}}`

func normalize(t *testing.T, ep Endpoint, raw string) core.Snapshot {
	t.Helper()
	s, err := Normalize(ep, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestBatteryCharging(t *testing.T) {
	s := normalize(t, BatteryStatus, zoe40Charging)
	when := at("2020-01-12T21:40:16Z")
	if !s.SoC.OK || s.SoC.V != 60 || !s.SoC.At.Equal(when) {
		t.Errorf("SoC = %+v", s.SoC)
	}
	if !s.RangeKm.OK || s.RangeKm.V != 141 {
		t.Errorf("range = %+v", s.RangeKm)
	}
	if s.Connection.V != core.Connected || s.Charging.V != core.ChargingActive {
		t.Errorf("connection %+v, charging %+v", s.Connection, s.Charging)
	}
	if !s.TimeLeft.OK || s.TimeLeft.V.Duration != 145*time.Minute {
		t.Errorf("time left = %+v", s.TimeLeft)
	}
	// Without the model, the power is the one the car would accept: never read.
	if s.PowerW.OK || s.Covers&core.FieldPower != 0 {
		t.Errorf("power read without its model: %+v", s.PowerW)
	}
	// batteryAvailableEnergy is the energy left, batteryCapacity always 0: no capacity.
	if s.CapacityKWh.OK {
		t.Errorf("capacity = %+v", s.CapacityKWh)
	}
}

func TestBatteryPowerOnZoePhase1(t *testing.T) {
	raw, err := WithModel([]byte(zoe40Charging), "X101VE")
	if err != nil {
		t.Fatal(err)
	}
	s := normalize(t, BatteryStatus, string(raw))
	if !s.PowerW.OK || s.PowerW.V != 27 || s.Covers&core.FieldPower == 0 {
		t.Errorf("power on a Zoe phase 1 = %+v", s.PowerW)
	}
	// Another model: the admissible power, not read.
	raw, _ = WithModel([]byte(zoe40Charging), "R5E1VE")
	if s := normalize(t, BatteryStatus, string(raw)); s.PowerW.OK {
		t.Errorf("power on a Renault 5 = %+v", s.PowerW)
	}
	// The charge stopped: covered and absent, the last one not kept.
	raw, _ = WithModel([]byte(zoe40Offset), "X101VE")
	if s := normalize(t, BatteryStatus, string(raw)); s.PowerW.OK || s.Covers&core.FieldPower == 0 {
		t.Errorf("power once stopped = %+v, covered %v", s.PowerW, s.Covers&core.FieldPower != 0)
	}
}

// TestWithModelNull: a response that is not an object is refused, not a panic.
func TestWithModelNull(t *testing.T) {
	if _, err := WithModel([]byte("null"), "R5E1VE"); err == nil {
		t.Error("null accepted")
	}
}

func TestBatteryParked(t *testing.T) {
	s := normalize(t, BatteryStatus, r5Parked)
	if s.Charging.V != core.ChargingIdle || s.Connection.V != core.Disconnected {
		t.Errorf("charging %+v, connection %+v", s.Charging, s.Connection)
	}
	// A time left kept from a charge ended ten hours before: not read.
	if s.TimeLeft.OK {
		t.Errorf("time left = %+v", s.TimeLeft)
	}
	s = normalize(t, BatteryStatus, zoe40Offset)
	if !s.SoC.At.Equal(at("2020-11-17T08:06:48Z")) {
		t.Errorf("the offset not read: %v", s.SoC.At)
	}
	if s.Charging.V != core.ChargingIdle {
		t.Errorf("-1.0 = %+v", s.Charging)
	}
}

func TestPlugThreeIsUnknown(t *testing.T) {
	s := normalize(t, BatteryStatus, `{"data":{"attributes":{"timestamp":"2025-04-28T05:27:07Z",
		"batteryLevel":96,"batteryAutonomy":182,"plugStatus":3,"chargingStatus":0.3,"chargingRemainingTime":15}}}`)
	if s.Connection.OK {
		t.Errorf("plugStatus 3 read as %v", s.Connection.V)
	}
	if s.Covers&core.FieldConnection == 0 {
		t.Error("the connection not covered: it would keep a value the car no longer tells")
	}
	if s.Charging.V != core.ChargingIdle {
		t.Errorf("0.3 = %+v", s.Charging)
	}
}

func TestCockpit(t *testing.T) {
	s := normalize(t, Cockpit, `{"data":{"type":"Car","id":"VF1AAAAA555777999","attributes":{"totalMileage":49114.27}}}`)
	if !s.OdometerKm.OK || s.OdometerKm.V != 49114.27 || !s.OdometerKm.At.IsZero() {
		t.Errorf("odometer = %+v", s.OdometerKm)
	}
	s = normalize(t, Cockpit, `{"data":{"id":"VYSP0000000000000","attributes":{"fuelQuantity":0.0,"totalMileage":1253,"timestamp":"2026-05-01T06:05:24Z"}}}`)
	if s.OdometerKm.V != 1253 || !s.OdometerKm.At.Equal(at("2026-05-01T06:05:24Z")) {
		t.Errorf("odometer = %+v", s.OdometerKm)
	}
}

func TestLocation(t *testing.T) {
	s := normalize(t, Location, `{"data":{"id":"VYSP0000000000000","attributes":{"lastUpdateTime":"2026-05-01T06:05:24Z","gpsLatitude":48.8584,"gpsLongitude":2.2945}}}`)
	if !s.Position.OK || s.Position.V != (core.Position{Lat: 48.8584, Lon: 2.2945}) {
		t.Errorf("position = %+v", s.Position)
	}
	// 91/181: no fix, not covered.
	s = normalize(t, Location, `{"data":{"attributes":{"lastUpdateTime":"2026-09-06T10:00:00Z","gpsLatitude":91,"gpsLongitude":181}}}`)
	if s.Covers != 0 || s.Position.OK {
		t.Errorf("no fix read: %+v", s)
	}
}

func TestHVAC(t *testing.T) {
	s := normalize(t, HVACStatus, `{"data":{"type":"Car","id":"VF1AAAAA555777999","attributes":{"externalTemperature":8.0,"hvacStatus":"off"}}}`)
	if !s.OutsideTempC.OK || s.OutsideTempC.V != 8 {
		t.Errorf("outside = %+v", s.OutsideTempC)
	}
	if !s.Climate.OK || !s.Climate.V.On.OK || s.Climate.V.On.V {
		t.Errorf("climate = %+v", s.Climate)
	}
	s = normalize(t, HVACStatus, `{"data":{"id":"VF1AAAA0000000000","attributes":{"internalTemperature":15.0,"hvacStatus":"on","socThreshold":15.0,"lastUpdateTime":"2025-03-24T18:57:22Z"}}}`)
	if !s.CabinTempC.OK || s.CabinTempC.V != 15 || !s.Climate.V.On.V {
		t.Errorf("cabin %+v, climate %+v", s.CabinTempC, s.Climate)
	}
}

func TestDetails(t *testing.T) {
	s := normalize(t, Details, `{"vin":"VF1AAAAA555777999","registrationNumber":"AB-123-CD","radioCode":"1234",
		"brand":{"label":"RENAULT"},"model":{"code":"R5E1VE","label":"RENAULT 5"},"energy":{"code":"ELEC"}}`)
	if s.Brand.V != "renault" || s.Family.V != "Renault 5 E-Tech" || s.Powertrain.V != core.Electric {
		t.Errorf("brand %+v, family %+v, powertrain %+v", s.Brand, s.Family, s.Powertrain)
	}
	s = normalize(t, Details, `{"brand":{"label":"DACIA"},"model":{"code":"XBG9ZZ","label":"SPRING NEW"},"energy":{"code":"ELECX"}}`)
	if s.Brand.V != "dacia" || s.Family.V != "SPRING NEW" {
		t.Errorf("an unknown code: brand %+v, family %+v", s.Brand, s.Family)
	}
	s = normalize(t, Details, `{"brand":{"label":"RENAULT"},"model":{"code":"XJA1VP"},"energy":{"code":"ESS"}}`)
	if s.Powertrain.V != core.OtherPowertrain {
		t.Errorf("a petrol Clio = %+v", s.Powertrain)
	}
}

func TestNoAttributes(t *testing.T) {
	s := normalize(t, BatteryStatus, `{"data":{"type":"Car","id":"VF1AAAAA555777999"}}`)
	if s.Covers != 0 {
		t.Errorf("covers = %b", s.Covers)
	}
	if _, err := Normalize(BatteryStatus, []byte(`{}`)); err == nil {
		t.Error("no data: no error")
	}
}

func TestFingerprintKeepsTheSend(t *testing.T) {
	a, _ := Fingerprint([]byte(r5Parked))
	b, _ := Fingerprint([]byte(`{"data":{"id":"VYSP0000000000000","attributes":{"timestamp":"2026-05-01T06:17:54Z",
	"batteryLevel":73,"batteryAutonomy":261,"V2L_SystemStatusDisplay":0,"plugStatus":0,"chargingStatus":0.0,
	"chargingRemainingTime":3,"chargingRemainingTimeLastUpdateDateTime":"2026-04-30T16:16:10Z"}}}`))
	if string(a) == string(b) {
		t.Error("a new send of the same values is a new reading")
	}
	c, _ := Fingerprint([]byte(`{"vin":"X","assets":[{"url":"a"}]}`))
	d, _ := Fingerprint([]byte(`{"vin":"X","assets":[{"url":"b"}]}`))
	if string(c) != string(d) {
		t.Error("the pictures' links change the fingerprint")
	}
}

func TestUpload(t *testing.T) {
	u, err := UploadOf([]byte(r5Parked))
	if err != nil {
		t.Fatal(err)
	}
	if !u.At.Equal(at("2026-05-01T06:07:54Z")) || len(u.Fields) != 8 || u.Fields[0] != "V2L_SystemStatusDisplay" {
		t.Errorf("upload = %+v", u)
	}
}

func TestServes(t *testing.T) {
	if Serves("X101VE", Location) || !Serves("X101VE", BatteryStatus) || !Serves("UNKNOWN", Location) {
		t.Error("the model map")
	}
}
