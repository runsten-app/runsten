package skoda

import (
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
)

var (
	chargingAt = time.Date(2026, 11, 11, 18, 2, 0, 0, time.UTC)
	odometerAt = time.Date(2026, 11, 11, 17, 40, 0, 0, time.UTC)
)

// body is a response holding the vehicle's parts given, JSON objects by name.
func body(parts map[string]string) []byte {
	var b strings.Builder
	b.WriteString(`{"vehicle":{"vin":"TMBJC9NY0RF000001"`)
	names := make([]string, 0, len(parts))
	for n := range parts {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		b.WriteString(`,"` + n + `":` + parts[n])
	}
	b.WriteString(`},"errors":[]}`)
	return []byte(b.String())
}

func charge(status, settings string) string {
	return `{"isVehicleInSavedLocation":false,"status":` + status + `,"settings":` + settings +
		`,"carCapturedTimestamp":"2026-11-11T18:02:00Z"}`
}

func TestNormalizeCharging(t *testing.T) {
	tests := []struct {
		name  string
		raw   []byte
		check func(*testing.T, core.Snapshot)
	}{
		{
			name: "a fast charge",
			raw: body(map[string]string{"charging": charge(`{"chargePowerInKw":95,"remainingTimeToFullyChargedInMinutes":25,`+
				`"state":"CHARGING","chargeType":"DC","plugConnectionState":"CONNECTED",`+
				`"battery":{"remainingCruisingRangeInMeters":249000,"stateOfChargeInPercent":42}}`,
				`{"targetStateOfChargeInPercent":80,"maxChargeCurrentAcAmpere":16}`)}),
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				want := core.Snapshot{
					SoC: core.Some(42.0, chargingAt), RangeKm: core.Some(249.0, chargingAt),
					Charging: core.Some(core.ChargingActive, chargingAt), Connection: core.Some(core.Connected, chargingAt),
					ChargeType: core.Some(core.DC, chargingAt), PowerW: core.Some(95_000.0, chargingAt),
					TargetSoC: core.Some(80.0, chargingAt), CurrentLimitA: core.Some(16.0, chargingAt),
					TimeLeft: core.Some(core.TimeLeft{Duration: 25 * time.Minute, To: core.ToTarget}, chargingAt),
				}
				if s.SoC != want.SoC || s.RangeKm != want.RangeKm || s.Charging != want.Charging ||
					s.Connection != want.Connection || s.ChargeType != want.ChargeType || s.PowerW != want.PowerW ||
					s.TargetSoC != want.TargetSoC || s.CurrentLimitA != want.CurrentLimitA || s.TimeLeft != want.TimeLeft {
					t.Errorf("got %+v", s)
				}
				if s.Covers&core.FieldPower == 0 || s.Covers&core.FieldChargeDetails == 0 {
					t.Errorf("covers %b", s.Covers)
				}
			},
		},
		{
			name: "an omitted state, the cable plugged",
			raw: body(map[string]string{"charging": charge(
				`{"chargeType":"OFF","plugConnectionState":"CONNECTED","battery":{"stateOfChargeInPercent":70}}`, `{}`)}),
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				if s.Charging.OK || !s.Connection.OK || s.Connection.V != core.Connected {
					t.Errorf("charging %+v, connection %+v: unknown, plugged", s.Charging, s.Connection)
				}
				if s.ChargeType.V != core.NoCharge || s.PowerW.OK || s.TimeLeft.OK {
					t.Errorf("no charge: %+v %+v %+v", s.ChargeType, s.PowerW, s.TimeLeft)
				}
			},
		},
		{
			name: "CONNECT_CABLE before 1.1.0",
			raw:  body(map[string]string{"charging": charge(`{"state":"CONNECT_CABLE"}`, `{}`)}),
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				if s.Charging.V != core.ChargingIdle || s.Connection.V != core.Disconnected {
					t.Errorf("charging %+v, connection %+v", s.Charging, s.Connection)
				}
			},
		},
		{
			name: "an unknown state and type",
			raw:  body(map[string]string{"charging": charge(`{"state":"WARP_CHARGING","chargeType":"WIRELESS"}`, `{}`)}),
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				if s.Charging.OK || s.ChargeType.OK || s.Connection.OK {
					t.Errorf("unknown values read: %+v %+v %+v", s.Charging, s.ChargeType, s.Connection)
				}
				if s.Covers&core.FieldCharging == 0 {
					t.Error("the part is covered: its unknown values replace the previous ones")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Normalize(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, s)
		})
	}
}

func TestNormalizeStates(t *testing.T) {
	for state, want := range map[string]core.ChargingStatus{
		"CHARGING": core.ChargingActive, "CONSERVING": core.ChargingDone, "READY_FOR_CHARGING": core.ChargingIdle,
		"CHARGING_INTERRUPTED": core.ChargingError, "DISCHARGING": core.ChargingDischarging,
	} {
		s, err := Normalize(body(map[string]string{"charging": charge(`{"state":"`+state+`"}`, `{}`)}))
		if err != nil {
			t.Fatal(err)
		}
		if s.Charging.V != want {
			t.Errorf("%s: %+v, want %s", state, s.Charging, want)
		}
	}
}

func TestNormalizeParts(t *testing.T) {
	tests := []struct {
		name  string
		parts map[string]string
		check func(*testing.T, core.Snapshot)
	}{
		{
			name:  "parked",
			parts: map[string]string{"parkingPosition": `{"state":"PARKED","gpsCoordinates":{"latitude":45.764,"longitude":4.8357},"formattedAddress":"Rue X 1, Lyon"}`},
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				if s.Engine.V != core.EngineStopped || s.Position.V != (core.Position{Lat: 45.764, Lon: 4.8357}) {
					t.Errorf("engine %+v, position %+v", s.Engine, s.Position)
				}
			},
		},
		{
			name:  "in motion",
			parts: map[string]string{"parkingPosition": `{"state":"IN_MOTION"}`},
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				if s.Engine.V != core.EngineRunning || s.Covers&core.FieldPosition != 0 {
					t.Errorf("engine %+v, position covered %v", s.Engine, s.Covers&core.FieldPosition != 0)
				}
			},
		},
		{
			name:  "parts of different ages",
			parts: map[string]string{"odometer": `{"mileageInKm":18420,"carCapturedTimestamp":"2026-11-11T17:40:00Z"}`, "charging": charge(`{"battery":{"stateOfChargeInPercent":42}}`, `{}`)},
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				if !s.OdometerKm.At.Equal(odometerAt) || !s.SoC.At.Equal(chargingAt) {
					t.Errorf("odometer at %v, SoC at %v", s.OdometerKm.At, s.SoC.At)
				}
			},
		},
		{
			name: "locked and closed",
			parts: map[string]string{"status": `{"overall":{"doorsLocked":"YES","locked":"YES","doors":"CLOSED","windows":"CLOSED","lights":"OFF"},` +
				`"detail":{"sunroof":"UNSUPPORTED","trunk":"CLOSED","bonnet":"CLOSED"},"carCapturedTimestamp":"2026-11-11T17:40:00Z"}`},
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				d := s.Doors.V
				if !d.Locked.V || d.FrontLeft.V != core.Closed || d.RearRight.V != core.Closed || d.Tailgate.V != core.Closed {
					t.Errorf("doors %+v", d)
				}
				if w := s.Windows.V; w.FrontLeft.V != core.Closed || w.Sunroof.OK {
					t.Errorf("windows %+v", w)
				}
			},
		},
		{
			name: "the trunk open",
			parts: map[string]string{"status": `{"overall":{"doorsLocked":"TRUNK_OPENED","locked":"UNKNOWN","doors":"CLOSED","windows":"OPEN","lights":"OFF"},` +
				`"detail":{"sunroof":"OPEN","trunk":"OPEN","bonnet":"CLOSED"}}`},
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				d := s.Doors.V
				if d.Locked.V || !d.Locked.OK || d.Tailgate.V != core.Open {
					t.Errorf("doors %+v", d)
				}
				if w := s.Windows.V; w.FrontLeft.OK || w.Sunroof.V != core.Open {
					t.Errorf("windows %+v: which side window is open is not told", w)
				}
			},
		},
		{
			name: "the climate",
			parts: map[string]string{"airConditioning": `{"state":"HEATING","targetTemperature":{"value":71.6,"unit":"FAHRENHEIT"},` +
				`"carCapturedTimestamp":"2026-11-11T17:40:00Z"}`},
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				if c := s.Climate.V; !c.On.V || c.SetpointC.V < 21.99 || c.SetpointC.V > 22.01 {
					t.Errorf("climate %+v", c)
				}
			},
		},
		{
			name:  "a part unavailable",
			parts: map[string]string{},
			check: func(t *testing.T, s core.Snapshot) {
				t.Helper()
				if s.Covers&(core.FieldSoC|core.FieldPosition|core.FieldOdometer|core.FieldDoors|core.FieldClimate) != 0 {
					t.Errorf("an absent part is covered: %b", s.Covers)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Normalize(body(tt.parts))
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, s)
		})
	}
}

func TestNormalizeModel(t *testing.T) {
	tests := []struct {
		vin, name, family, sheet string
		year                     int
	}{
		{"TMBJC9NY0RF000001", "Enyaq", "Enyaq", "", 2024},
		{"TMBJC9NY0RF000001", "My Enyaq", "Enyaq", "My Enyaq", 2024},
		{"TMBGF7NY0PF047301", "Enyaq", "Enyaq", "", 2023}, // the name settles the family
		{"TMBGF7NY0PF047301", "Rosie", "Enyaq Coupé", "Rosie", 2023},
		{"TMBNC7NY5SF083963", "", "Elroq", "", 2025},
		{"TMBAB7NE1M0000001", "Octavia", "", "Octavia", 2021},
	}
	for _, tt := range tests {
		s, err := Normalize([]byte(`{"vehicle":{"vin":"` + tt.vin + `","name":"` + tt.name + `"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if s.Family.V != tt.family || s.Sheet.V.Name != tt.sheet || s.ModelYear.V != tt.year || s.Brand.V != Brand {
			t.Errorf("%s %q: family %q, sheet %q, year %d, brand %q", tt.vin, tt.name, s.Family.V, s.Sheet.V.Name, s.ModelYear.V, s.Brand.V)
		}
		if electric := s.Powertrain.OK && s.Powertrain.V == core.Electric; electric != (tt.family != "") {
			t.Errorf("%s: powertrain %+v", tt.vin, s.Powertrain)
		}
	}
}

func TestNormalizer(t *testing.T) {
	n := Normalizer{}
	if _, err := n.Normalize(core.Kind{Source: "pleos", Endpoint: "vehicle"}, nil); err == nil {
		t.Error("another source read")
	}
	if _, err := n.Normalize(core.Kind{Source: Source, Endpoint: "status"}, nil); err == nil {
		t.Error("another endpoint read")
	}
	if _, err := n.Normalize(core.Kind{Source: Source, Endpoint: string(Vehicle)}, []byte(`{"errors":[]}`)); err == nil {
		t.Error("a response without its vehicle read")
	}
	if _, err := n.NormalizeHealth(core.Kind{Source: Source, Endpoint: string(Vehicle)}, nil); err == nil {
		t.Error("a health read")
	}
	if len(n.Kinds()) != 1 || len(n.EnergyKinds()) != 1 || n.StateKinds() != nil || n.HealthKinds() != nil || n.SleepKinds() != nil {
		t.Error("kinds")
	}
}

func TestFingerprintAndUpload(t *testing.T) {
	a := []byte(`{"vehicle":{"vin":"TMBJC9NY0RF000001","renderUrl":"https://x/1","odometer":{"mileageInKm":1,"carCapturedTimestamp":"2026-11-11T17:40:00Z"}}}`)
	b := []byte(`{"vehicle":{"vin":"TMBJC9NY0RF000001","renderUrl":"https://x/2","odometer":{"mileageInKm":1,"carCapturedTimestamp":"2026-11-11T17:40:00Z"}}}`)
	c := []byte(`{"vehicle":{"vin":"TMBJC9NY0RF000001","renderUrl":"https://x/2","odometer":{"mileageInKm":1,"carCapturedTimestamp":"2026-11-11T18:40:00Z"}}}`)
	fa, _ := Fingerprint(a)
	fb, _ := Fingerprint(b)
	fc, _ := Fingerprint(c)
	if string(fa) != string(fb) {
		t.Error("the picture's link changes the fingerprint")
	}
	if string(fb) == string(fc) {
		t.Error("a new send of the same values is the same fingerprint")
	}
	if _, err := Fingerprint([]byte("{")); err == nil {
		t.Error("invalid JSON fingerprinted")
	}
	u, err := UploadOf(c)
	if err != nil {
		t.Fatal(err)
	}
	if !u.At.Equal(time.Date(2026, 11, 11, 18, 40, 0, 0, time.UTC)) || u.ModelYear != 2024 ||
		!slices.Equal(u.Fields, []string{"odometer.mileageInKm", "vin"}) {
		t.Errorf("upload %+v", u)
	}
	if _, err := UploadOf([]byte("[")); err == nil {
		t.Error("invalid JSON read")
	}
	if len(PrivateFields()) != 3 {
		t.Error("private fields")
	}
}
