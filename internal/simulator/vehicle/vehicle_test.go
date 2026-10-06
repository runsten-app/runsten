package vehicle

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

var (
	home = Position{Lat: 45.7640, Lon: 4.8357}
	work = Position{Lat: 45.7797, Lon: 4.9270}
)

func testSpec() Spec {
	return Spec{Model: "EX-SIM", ModelYear: 2026, BatteryKWh: 80, ConsumptionKWhPer100km: 20, ChargingCurrentLimitA: 16}
}

// usableSpec returns testSpec with its SoC running over kwh.
func usableSpec(kwh float64) Spec {
	s := testSpec()
	s.UsableKWh = kwh
	return s
}

// fadedSpec returns a 64 kWh usable spec losing fade of its capacity a year, from t0.
func fadedSpec(fade float64, from time.Time) Spec {
	s := usableSpec(64)
	s.FadePerYear = fade
	s.FadeStart = from
	return s
}

func newVehicle(t *testing.T, soc float64) *Vehicle {
	t.Helper()
	v, err := New(testSpec(), State{SoC: soc, OdometerKm: 1000, Position: home, Locked: true}, DefaultUploadPolicy(), t0)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name   string
		spec   Spec
		state  State
		policy UploadPolicy
	}{
		{"zero battery", Spec{ConsumptionKWhPer100km: 20}, State{SoC: 50}, DefaultUploadPolicy()},
		{"zero consumption", Spec{BatteryKWh: 80}, State{SoC: 50}, DefaultUploadPolicy()},
		{"negative usable", usableSpec(-1), State{SoC: 50}, DefaultUploadPolicy()},
		{"usable above battery", usableSpec(81), State{SoC: 50}, DefaultUploadPolicy()},
		{"fade of a whole capacity a year", fadedSpec(1, t0), State{SoC: 50}, DefaultUploadPolicy()},
		{"negative fade", fadedSpec(-0.02, t0), State{SoC: 50}, DefaultUploadPolicy()},
		{"fade without a start", fadedSpec(0.02, time.Time{}), State{SoC: 50}, DefaultUploadPolicy()},
		{"negative SoC", testSpec(), State{SoC: -1}, DefaultUploadPolicy()},
		{"SoC > 100", testSpec(), State{SoC: 101}, DefaultUploadPolicy()},
		{"zero interval", testSpec(), State{SoC: 50}, UploadPolicy{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.spec, tt.state, tt.policy, t0); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNewReportsEverythingAtStart(t *testing.T) {
	v := newVehicle(t, 50)
	r := v.Report()
	for name, at := range map[string]time.Time{"energy": r.EnergyAt, "status": r.StatusAt, "location": r.LocationAt} {
		if !at.Equal(t0) {
			t.Errorf("%s uploaded at %v, want %v", name, at, t0)
		}
	}
	if v.State().TargetSoC != 90 {
		t.Errorf("default TargetSoC = %v, want 90", v.State().TargetSoC)
	}
}

func TestUsableKWhAt(t *testing.T) {
	faded := fadedSpec(0.02, t0) // 64 kWh usable, 2 % lost a year from t0
	year := 365.25 * 24 * time.Hour
	tests := []struct {
		name string
		spec Spec
		at   time.Time
		want float64
	}{
		{"unset: the whole reported battery", testSpec(), t0, 80},
		{"usable, no fade", usableSpec(64), t0.Add(year), 64},
		{"before the fade starts: the full capacity (a fleet parks the vehicle first)", faded, t0.Add(-time.Hour), 64},
		{"at the fade start: not faded yet", faded, t0, 64},
		{"half a simulated year", faded, t0.Add(year / 2), 64 * (1 - 0.01)},
		{"one simulated year", faded, t0.Add(year), 64 * (1 - 0.02)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.UsableKWhAt(tt.at); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("UsableKWhAt(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

func TestDriveConsumesEnergyAndReportsPeriodically(t *testing.T) {
	v := newVehicle(t, 50)
	v.StartDrive(t0)

	// 40 km at 20 kWh/100 km = 8 kWh = 10 % of an 80 kWh battery, in 1 km / 6 s steps.
	at := t0
	for range 40 {
		at = at.Add(6 * time.Second)
		v.Drive(at, 1)
	}
	s := v.State()
	if !near(s.SoC, 40) || !near(s.OdometerKm, 1040) {
		t.Fatalf("SoC = %v, odometer = %v, want 40 and 1040", s.SoC, s.OdometerKm)
	}

	r := v.Report()
	// Status uploaded every minute: departure at t0, then at t0+60s, t0+120s, t0+180s, t0+240s.
	if want := t0.Add(240 * time.Second); !r.StatusAt.Equal(want) {
		t.Errorf("StatusAt = %v, want %v", r.StatusAt, want)
	}
	// Energy uploaded on every whole % change: dropping below 41 % happens at km 37
	// (SoC 40.75), i.e. at t0+222s. The next 3 km stay at "40 %".
	if want := t0.Add(222 * time.Second); !r.EnergyAt.Equal(want) || int(r.Energy.SoC) != 40 {
		t.Errorf("energy uploaded at %v with SoC %v, want %v and 40", r.EnergyAt, r.Energy.SoC, want)
	}
	// The position is not uploaded during the trip.
	if !r.LocationAt.Equal(t0) {
		t.Errorf("LocationAt = %v, want %v (no upload while driving)", r.LocationAt, t0)
	}
}

func TestEndDriveReportsLocationAndTrip(t *testing.T) {
	v := newVehicle(t, 50)
	v.StartDrive(t0)
	v.Drive(t0.Add(time.Minute), 10)
	end := t0.Add(20 * time.Minute)
	v.EndDrive(end, work)

	r := v.Report()
	if !r.LocationAt.Equal(end) || r.Location.Lat != work.Lat {
		t.Fatalf("position uploaded %v at %v, want %v at %v", r.Location, r.LocationAt, work, end)
	}
	if h := r.Location.Heading; h < 60 || h > 80 {
		t.Errorf("heading = %v, want approximately 70 degrees (towards east-northeast)", h)
	}
	trip := r.Status.LastTrip
	if !near(trip.DistanceKm, 10) || trip.Duration != 20*time.Minute || !near(trip.EnergyKWh, 2) {
		t.Errorf("trip = %+v", trip)
	}
	if r.Status.TotalDriveTime != 20*time.Minute {
		t.Errorf("TotalDriveTime = %v, want 20m", r.Status.TotalDriveTime)
	}
	if !r.Status.Locked || r.Status.Driving {
		t.Errorf("after the trip: locked=%v, driving=%v", r.Status.Locked, r.Status.Driving)
	}
}

func TestDriveIgnoredWhenParked(t *testing.T) {
	v := newVehicle(t, 50)
	v.Drive(t0.Add(time.Minute), 10)
	v.EndDrive(t0.Add(2*time.Minute), work)
	if s := v.State(); s.OdometerKm != 1000 || s.Position != home {
		t.Fatalf("a parked vehicle must not move: %+v", s)
	}
}

// TestDriveRunsOverUsableCapacity: driving turns energy into SoC points over the
// capacity the SoC spans, not the nominal one.
func TestDriveRunsOverUsableCapacity(t *testing.T) {
	v, err := New(usableSpec(64), State{SoC: 50, OdometerKm: 1000, Position: home, Locked: true}, DefaultUploadPolicy(), t0)
	if err != nil {
		t.Fatal(err)
	}
	v.StartDrive(t0)
	// 32 km × 20 kWh/100 km = 6.4 kWh = 10 points over 64 kWh (8 over the nominal 80).
	v.Drive(t0.Add(time.Minute), 32)
	if soc := v.State().SoC; !near(soc, 40) {
		t.Errorf("SoC = %v, want 40", soc)
	}
}

func TestChargeUntilTarget(t *testing.T) {
	v := newVehicle(t, 50)
	v.PlugIn(t0, AC, 8000, 80)
	if s := v.State(); s.Charging != Charging || s.ChargingPowerW != 8000 {
		t.Fatalf("after plugging in: %+v", s)
	}

	// 8 kW for 3 h = 24 kWh = 30 %: the 80 % target is reached exactly.
	at := t0
	done := false
	for i := 0; i < 3*3600 && !done; i++ {
		at = at.Add(time.Second)
		done = v.Charge(at, time.Second)
	}
	if !done {
		t.Fatal("charging should have finished")
	}
	s := v.State()
	if s.SoC != 80 || s.Charging != Done || s.ChargingPowerW != 0 || !s.PluggedIn {
		t.Fatalf("end of charge: %+v", s)
	}
	r := v.Report()
	if !r.EnergyAt.Equal(at) || !r.StatusAt.Equal(at) {
		t.Errorf("end of charge uploaded at %v / %v, want %v", r.EnergyAt, r.StatusAt, at)
	}
	if !v.Charge(at.Add(time.Second), time.Second) {
		t.Error("Charge after the end must return true")
	}
}

func TestPlugInAboveTargetIsDoneImmediately(t *testing.T) {
	v := newVehicle(t, 85)
	v.PlugIn(t0, DC, 150000, 80)
	if s := v.State(); s.Charging != Done || s.ChargingPowerW != 0 {
		t.Fatalf("state = %+v, want Done with no power", s)
	}
}

// TestChargeRunsOverUsableCapacity: charging turns energy into SoC points over the
// capacity the SoC spans, not the nominal one.
func TestChargeRunsOverUsableCapacity(t *testing.T) {
	v, err := New(usableSpec(64), State{SoC: 50, OdometerKm: 1000, Position: home, Locked: true}, DefaultUploadPolicy(), t0)
	if err != nil {
		t.Fatal(err)
	}
	v.PlugIn(t0, AC, 8000, 90)
	if v.Charge(t0.Add(time.Hour), time.Hour) {
		t.Fatal("the charge should still be in progress")
	}
	// 8 kW for 1 h = 8 kWh = 12.5 points over the usable 64 kWh (10 over the nominal 80).
	if soc := v.State().SoC; !near(soc, 62.5) {
		t.Errorf("SoC = %v, want 62.5", soc)
	}
}

// TestChargeAfterAYearOfFade: after one simulated year of fadePerYear 0.02 the usable
// capacity has lost 2 %, and the same 8 kWh raise the SoC 1/(1-0.02) times as much as
// over the year-old battery (12.7551… points against 12.5).
func TestChargeAfterAYearOfFade(t *testing.T) {
	v, err := New(fadedSpec(0.02, t0), State{SoC: 50, OdometerKm: 1000, Position: home, Locked: true}, DefaultUploadPolicy(), t0)
	if err != nil {
		t.Fatal(err)
	}
	year := 365.25 * 24 * time.Hour
	v.PlugIn(t0.Add(year-time.Hour), AC, 8000, 90)
	if v.Charge(t0.Add(year), time.Hour) {
		t.Fatal("the charge should still be in progress")
	}
	want := 50 + 8/(64*(1-0.02))*100
	if soc := v.State().SoC; math.Abs(soc-want) > 1e-9 {
		t.Errorf("SoC = %v, want %v", soc, want)
	}
}

func TestStartDriveUnplugs(t *testing.T) {
	v := newVehicle(t, 50)
	v.PlugIn(t0, AC, 7400, 80)
	v.StartDrive(t0.Add(time.Hour))
	s := v.State()
	if s.PluggedIn || s.Charging != Idle || s.ChargeType != NoCharge || !s.Driving || s.Locked {
		t.Fatalf("state = %+v", s)
	}
}

func TestSoCIsClamped(t *testing.T) {
	v := newVehicle(t, 1)
	v.StartDrive(t0)
	v.Drive(t0.Add(time.Minute), 100)
	if soc := v.State().SoC; soc != 0 {
		t.Fatalf("SoC = %v, want 0", soc)
	}
}

func TestProfiles(t *testing.T) {
	tests := []struct {
		name  string
		power Availability
		limit Availability
	}{
		{"bev-generic", Supported, Supported},
		{"ex30-like", Supported, Unsupported},
		{"ex90-like", NotFound, Supported},
	}
	for _, tt := range tests {
		c, ok := Profile(tt.name)
		if !ok || c.ChargingPower != tt.power || c.ChargingCurrentLimit != tt.limit {
			t.Errorf("Profile(%q) = %+v, %v", tt.name, c, ok)
		}
	}
	if _, ok := Profile("unknown"); ok {
		t.Error("an unknown profile must not be found")
	}
}
