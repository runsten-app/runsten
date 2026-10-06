package scenario

import (
	"math"
	"strings"
	"testing"
	"time"

	"runsten/internal/simulator/vehicle"
)

// variant returns commute with its VIN, its start or its api section replaced.
func variant(t *testing.T, vin, startAt, api string) *Scenario {
	t.Helper()
	src := strings.Replace(commute, "YV1SMLT0000DT0001", vin, 1)
	src = strings.Replace(src, "2026-09-28T05:00:00Z", startAt, 1)
	sc, err := Parse([]byte(src + api))
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestNewFleetRejects(t *testing.T) {
	tests := []struct {
		name    string
		scs     func(t *testing.T) []*Scenario
		wantErr string
	}{
		{"no scenario", func(*testing.T) []*Scenario { return nil }, "no scenario"},
		{"same VIN twice", func(t *testing.T) []*Scenario {
			return []*Scenario{
				variant(t, "YV1SMLT0000DT0001", "2026-09-28T05:00:00Z", ""),
				variant(t, "YV1SMLT0000DT0002", "2026-09-28T05:00:00Z", ""),
				variant(t, "YV1SMLT0000DT0001", "2026-09-28T06:00:00Z", ""),
			}
		}, `scenarios 1 and 3 have the same vin "YV1SMLT0000DT0001"`},
		{"api section after the first", func(t *testing.T) []*Scenario {
			return []*Scenario{
				variant(t, "YV1SMLT0000DT0001", "2026-09-28T05:00:00Z", ""),
				variant(t, "YV1SMLT0000DT0002", "2026-09-28T05:00:00Z", "api:\n  dailyQuota: 150\n"),
			}
		}, "scenario 2 (vin YV1SMLT0000DT0002): only the first scenario may have an api section"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewFleet(tt.scs(t), vehicle.DefaultUploadPolicy())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestInvalidSpecCarriesTheScenario: the vehicle's spec is validated when the fleet
// builds it, the error naming the scenario whose vehicle is invalid.
func TestInvalidSpecCarriesTheScenario(t *testing.T) {
	bad := variant(t, "YV1SMLT0000DT0002", "2026-09-28T05:00:00Z", "")
	bad.Vehicle.UsableKWh = 100 // above batteryKWh
	_, err := NewFleet([]*Scenario{
		variant(t, "YV1SMLT0000DT0001", "2026-09-28T05:00:00Z", ""),
		bad,
	}, vehicle.DefaultUploadPolicy())
	if err == nil ||
		!strings.Contains(err.Error(), "scenario 2 (vin YV1SMLT0000DT0002)") ||
		!strings.Contains(err.Error(), "usableKWh must be in [0, batteryKWh]") {
		t.Errorf("error = %v, want the scenario's name and the field", err)
	}
}

// TestFadeStartsAtTheOwnScenario: the fleet creates every vehicle at the earliest
// start, but each one's fade counts from its own start: the late vehicle, half a year
// later, has barely faded.
func TestFadeStartsAtTheOwnScenario(t *testing.T) {
	lateAt := start.Add(365.25 * 24 * time.Hour / 2)
	late := variant(t, "YV1SMLT0000DT0002", lateAt.Format(time.RFC3339), "")
	late.Vehicle.UsableKWh, late.Vehicle.FadePerYear, late.Vehicle.SoC = 64, 0.5, 50
	late.Steps = []Step{
		{Park: &Park{Duration: time.Hour}},
		{Charge: &Charge{Type: "AC", PowerKW: 8, UntilSoC: 90}},
	}
	f, err := NewFleet([]*Scenario{
		variant(t, "YV1SMLT0000DT0001", "2026-09-28T05:00:00Z", ""),
		late,
	}, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !f.Start().Equal(start) {
		t.Fatalf("fleet start %v, want the earliest, %v", f.Start(), start)
	}

	// One hour of charging has put in 8 kWh: ~62.5 % over the 64 kWh barely faded.
	// Counted from the fleet's start, half a year at 50 % a year would leave 48 kWh
	// and a SoC of 66.67.
	sim := f.Simulations()[1]
	sim.ReportAt(lateAt.Add(2 * time.Hour))
	if soc := sim.State().SoC; math.Abs(soc-62.5) > 0.01 {
		t.Errorf("SoC = %v, want ~62.5 (the fade counts from the vehicle's own start)", soc)
	}
}

func TestFleetOfOneMatchesSimulation(t *testing.T) {
	sc := variant(t, "YV1SMLT0000DT0001", "2026-09-28T05:00:00Z", "api:\n  dailyQuota: 150\n")
	f, err := NewFleet([]*Scenario{sc}, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	alone := mustSimulation(t, commute)
	if !f.Start().Equal(start) || len(f.Simulations()) != 1 {
		t.Fatalf("start %v, %d simulations", f.Start(), len(f.Simulations()))
	}
	for _, at := range []time.Time{start, start.Add(90 * time.Minute), start.Add(5 * time.Hour)} {
		if got, want := f.Simulations()[0].ReportAt(at), alone.ReportAt(at); got != want {
			t.Errorf("at %v: %+v, want %+v", at, got, want)
		}
	}
}

// TestFleetStartsAtTheEarliest: the clock starts at the earliest start; the vehicle of
// the later scenario stays parked, in its initial state, until its own start, then plays
// its steps from there.
func TestFleetStartsAtTheEarliest(t *testing.T) {
	later := start.Add(3 * time.Hour)
	f, err := NewFleet([]*Scenario{
		variant(t, "YV1SMLT0000DT0002", later.Format(time.RFC3339), ""),
		variant(t, "YV1SMLT0000DT0001", start.Format(time.RFC3339), ""),
	}, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !f.Start().Equal(start) {
		t.Fatalf("fleet start %v, want the earliest, %v", f.Start(), start)
	}
	late, early := f.Simulations()[0], f.Simulations()[1]
	if late.VIN() != "YV1SMLT0000DT0002" || early.VIN() != "YV1SMLT0000DT0001" {
		t.Fatalf("simulations not in the order of the scenarios: %s, %s", late.VIN(), early.VIN())
	}

	// Commute: parked 1 h, then drives 40 min.
	tests := []struct {
		at           time.Time
		earlyDriving bool
		lateDriving  bool
		lateStep     int
	}{
		{start, false, false, 0},
		{start.Add(90 * time.Minute), true, false, 0},
		{later.Add(30 * time.Minute), false, false, 0},
		{later.Add(90 * time.Minute), false, true, 1},
	}
	for _, tt := range tests {
		if got := early.ReportAt(tt.at).Status.Driving; got != tt.earlyDriving {
			t.Errorf("%v: early vehicle driving = %v", tt.at, got)
		}
		rep := late.ReportAt(tt.at)
		if rep.Status.Driving != tt.lateDriving {
			t.Errorf("%v: late vehicle driving = %v", tt.at, rep.Status.Driving)
		}
		if rep.StatusAt.After(tt.at) {
			t.Errorf("%v: late vehicle's reading stamped in the future, %v", tt.at, rep.StatusAt)
		}
		sts := f.StatusAt(tt.at)
		if len(sts) != 2 || sts[0].VIN != late.VIN() || sts[0].Step != tt.lateStep || !sts[0].Now.Equal(tt.at) {
			t.Errorf("%v: status %+v", tt.at, sts)
		}
	}
	late.ReportAt(later.Add(2 * time.Hour))
	if st := late.State(); math.Abs(st.OdometerKm-1040) > 1e-6 {
		t.Errorf("late vehicle's odometer %v after its trip, want 1040", st.OdometerKm)
	}
}
