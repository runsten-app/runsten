package polestarid

import (
	"testing"
	"time"

	"runsten/internal/core"
)

func TestChargingStatuses(t *testing.T) {
	for _, tt := range []struct {
		status string
		want   core.ChargingStatus
		ok     bool
	}{
		{"CHARGING_STATUS_V2_CHARGING", core.ChargingActive, true},
		{"CHARGING_STATUS_V2_IDLE", core.ChargingIdle, true},
		{"CHARGING_STATUS_V2_DONE", core.ChargingDone, true},
		{"CHARGING_STATUS_V2_SOMETHING_NEW", "", false},
		{"", "", false},
	} {
		raw := `{"data":{"carTelematicsV2":{"battery":[{"vin":"LPSYCZRE5RL000014","chargingStatusV2":"` + tt.status +
			`","batteryChargeLevelPercentage":50,"timestamp":{"seconds":"1790571600","nanos":0}}],"odometer":[],"health":[]}}}`
		s, err := Normalize(Telematics, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if s.Charging.OK != tt.ok || s.Charging.V != tt.want {
			t.Errorf("%q: %+v, want %q %v", tt.status, s.Charging, tt.want, tt.ok)
		}
		if s.Covers&core.FieldCharging == 0 {
			t.Errorf("%q: the charging not covered: an unknown status leaves it unknown, not the last one", tt.status)
		}
	}
}

func TestATelematicsWithoutBatteryCoversOnlyTheOdometer(t *testing.T) {
	raw := `{"data":{"carTelematicsV2":{"battery":[],"odometer":[{"vin":"LPSYCZRE5RL000014","odometerMeters":1234567,"timestamp":{"seconds":1790571600}}],"health":[]}}}`
	s, err := Normalize(Telematics, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if s.Covers != core.FieldOdometer || s.OdometerKm.V != 1234.567 || !s.OdometerKm.At.Equal(time.Unix(1790571600, 0).UTC()) {
		t.Errorf("%+v", s)
	}
}

func TestFamilies(t *testing.T) {
	for name, want := range map[string]string{"Polestar4": "Polestar 4", "Polestar 2": "Polestar 2", "Polestar3": "Polestar 3", "Something": ""} {
		raw := `{"data":{"getConsumerCarsV2":[{"vin":"LPSYCZRE5RL000014","modelName":"` + name + `","modelYear":"2024"}]}}`
		s, err := Normalize(Cars, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if s.Family.V != want || s.Family.OK != (want != "") {
			t.Errorf("%q: family %+v, want %q", name, s.Family, want)
		}
	}
}

func TestTheFingerprintLeavesTheTimestampsOut(t *testing.T) {
	a := `{"data":{"carTelematicsV2":{"battery":[{"batteryChargeLevelPercentage":50,"timestamp":{"seconds":"1"}}]}}}`
	b := `{"data":{"carTelematicsV2":{"battery":[{"batteryChargeLevelPercentage":50,"timestamp":{"seconds":"2"}}]}}}`
	c := `{"data":{"carTelematicsV2":{"battery":[{"batteryChargeLevelPercentage":51,"timestamp":{"seconds":"2"}}]}}}`
	fa, _ := Fingerprint([]byte(a))
	fb, _ := Fingerprint([]byte(b))
	fc, _ := Fingerprint([]byte(c))
	if string(fa) != string(fb) || string(fb) == string(fc) {
		t.Error("the fingerprint must ignore the timestamps, and only them")
	}
}
