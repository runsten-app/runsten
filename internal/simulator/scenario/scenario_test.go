package scenario

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"runsten/internal/simulator/vehicle"
)

const commute = `
vin: YV1SMLT0000DT0001
start: 2026-09-28T05:00:00Z
vehicle:
  profile: bev-generic
  batteryKWh: 80
  consumptionKWhPer100km: 20
  soc: 60
  odometerKm: 1000
  place: home
places:
  home: { lat: 45.7640, lon: 4.8357 }
  work: { lat: 45.7797, lon: 4.9270 }
steps:
  - park: { duration: 1h }
  - drive: { to: work, distanceKm: 40, duration: 40m }
  - park: { duration: 1h }
  - charge: { type: AC, powerKW: 8, untilSoc: 80 }
`

var start = time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)

func mustSimulation(t *testing.T, src string) *Simulation {
	t.Helper()
	sc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	sim, err := NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return sim
}

func TestParseRejectsInvalidScenarios(t *testing.T) {
	tests := []struct {
		name    string
		replace [2]string
		wantErr string
	}{
		{"vin with I", [2]string{"YV1SMLT0000DT0001", "YV1SIMT0000DT0001"}, "vin"},
		{"unknown profile", [2]string{"bev-generic", "tesla"}, "unknown profile"},
		{"unknown starting place", [2]string{"place: home", "place: moon"}, "starting place"},
		{"unknown destination", [2]string{"to: work", "to: moon"}, "drive.to"},
		{"zero duration", [2]string{"duration: 1h }\n  - drive", "duration: 0s }\n  - drive"}, "park.duration"},
		{"zero distance", [2]string{"distanceKm: 40", "distanceKm: 0"}, "drive.distanceKm"},
		{"invalid charge type", [2]string{"type: AC", "type: XX"}, "charge.type"},
		{"zero power", [2]string{"powerKW: 8", "powerKW: 0"}, "charge.powerKW"},
		{"target out of bounds", [2]string{"untilSoc: 80", "untilSoc: 120"}, "charge.untilSoc"},
		{"double step", [2]string{"- park: { duration: 1h }\n  - drive", "- park: { duration: 1h }\n    charge: { type: AC, powerKW: 1, untilSoc: 50 }\n  - drive"}, "exactly one"},
		{"unknown field", [2]string{"soc: 60", "soc: 60\n  color: red"}, "color"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(commute, tt.replace[0], tt.replace[1], 1)
			if src == commute {
				t.Fatalf("replacement had no effect: %q", tt.replace[0])
			}
			_, err := Parse([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseAPI(t *testing.T) {
	sc, err := Parse([]byte(commute + `api:
  dailyQuota: 0
  tokenTTL: 30m
  errorRate: 0.1
  latency: 200ms
  outages:
    - { after: 1h, duration: 40m }
`))
	if err != nil {
		t.Fatal(err)
	}
	a := sc.API
	if a.DailyQuota == nil || *a.DailyQuota != 0 || a.PerMinute != nil || a.TokenTTL != 30*time.Minute ||
		a.ErrorRate != 0.1 || a.Latency != 200*time.Millisecond ||
		len(a.Outages) != 1 || a.Outages[0] != (Outage{After: time.Hour, Duration: 40 * time.Minute}) {
		t.Errorf("api = %+v", a)
	}

	for name, bad := range map[string]string{
		"negative quota":          "api: { dailyQuota: -1 }",
		"rate out of bounds":      "api: { errorRate: 1.5 }",
		"negative latency":        "api: { latency: -1s }",
		"outage without duration": "api: { outages: [ { after: 1h } ] }",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(commute + bad + "\n")); err == nil || !strings.Contains(err.Error(), "api.") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestParseRejectsEmptySteps(t *testing.T) {
	head, _, _ := strings.Cut(commute, "steps:")
	src := head + "steps: []\n"
	if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "no steps") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseVehicleUsableAndFade(t *testing.T) {
	// Absent: both zero, the vehicle's defaults (the SoC spans batteryKWh, no fade).
	sc, err := Parse([]byte(commute))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Vehicle.UsableKWh != 0 || sc.Vehicle.FadePerYear != 0 {
		t.Fatalf("usableKWh = %v, fadePerYear = %v, want the defaults 0 and 0",
			sc.Vehicle.UsableKWh, sc.Vehicle.FadePerYear)
	}

	src := strings.Replace(commute, "batteryKWh: 80", "batteryKWh: 80\n  usableKWh: 64\n  fadePerYear: 0.02", 1)
	sc, err = Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Vehicle.UsableKWh != 64 || sc.Vehicle.FadePerYear != 0.02 {
		t.Errorf("usableKWh = %v, fadePerYear = %v, want 64 and 0.02",
			sc.Vehicle.UsableKWh, sc.Vehicle.FadePerYear)
	}
}

func TestCommuteDay(t *testing.T) {
	sim := mustSimulation(t, commute)

	// During the initial parking, nothing moves and nothing is uploaded.
	r := sim.ReportAt(start.Add(30 * time.Minute))
	if !r.StatusAt.Equal(start) || r.Status.Driving {
		t.Fatalf("parked: %+v at %v", r.Status, r.StatusAt)
	}

	// Mid-trip: the vehicle is driving, the odometer was uploaded recently, the position was not.
	mid := start.Add(time.Hour + 20*time.Minute)
	r = sim.ReportAt(mid)
	if !r.Status.Driving {
		t.Fatal("vehicle should be driving mid-trip")
	}
	if age := mid.Sub(r.StatusAt); age > time.Minute {
		t.Errorf("status uploaded %v ago, want <= 1 min", age)
	}
	if !r.LocationAt.Equal(start) {
		t.Errorf("position uploaded at %v, want %v (no upload while driving)", r.LocationAt, start)
	}
	if got := r.Status.OdometerKm; math.Abs(got-1020) > 1 {
		t.Errorf("odometer uploaded = %v, want ~1020", got)
	}

	// Trip ends at 05:00 + 1h40.
	arrival := start.Add(time.Hour + 40*time.Minute)
	r = sim.ReportAt(arrival.Add(time.Minute))
	if !r.LocationAt.Equal(arrival) || r.Location.Lat != 45.7797 {
		t.Fatalf("position %v uploaded at %v, want work at %v", r.Location, r.LocationAt, arrival)
	}
	if trip := r.Status.LastTrip; math.Abs(trip.DistanceKm-40) > 1e-6 || trip.Duration != 40*time.Minute {
		t.Errorf("last trip = %+v", trip)
	}
	// 40 km × 20 kWh/100 km = 8 kWh = 10 % of 80 kWh.
	if soc := sim.State().SoC; math.Abs(soc-50) > 1e-6 {
		t.Errorf("SoC after trip = %v, want 50", soc)
	}

	// Charge: plugged in at 05:00 + 2h40, 8 kW, from 50 to 80 % = 24 kWh = 3 h.
	plug := arrival.Add(time.Hour)
	st := sim.StatusAt(plug.Add(time.Hour))
	if st.Step != 3 || st.Finished {
		t.Fatalf("step = %+v, want charge in progress", st)
	}
	r = sim.ReportAt(plug.Add(time.Hour))
	if r.Energy.Charging != vehicle.Charging || !r.Energy.PluggedIn {
		t.Errorf("charging: %+v", r.Energy)
	}

	end := plug.Add(3 * time.Hour)
	st = sim.StatusAt(end.Add(time.Hour))
	if !st.Finished || st.Step != 4 || st.Steps != 4 {
		t.Fatalf("end: %+v", st)
	}
	r = sim.ReportAt(end.Add(time.Hour))
	if r.Energy.Charging != vehicle.Done || r.Energy.SoC != 80 {
		t.Errorf("end of charge: %+v", r.Energy)
	}
	if d := r.EnergyAt.Sub(end); d < 0 || d > 2*time.Second {
		t.Errorf("end of charge uploaded at %v, want ~%v", r.EnergyAt, end)
	}
}

func TestReportAtIsMonotonic(t *testing.T) {
	sim := mustSimulation(t, commute)
	later := start.Add(3 * time.Hour)
	a := sim.ReportAt(later)
	b := sim.ReportAt(start) // a past time does not move the simulation backwards
	if !a.StatusAt.Equal(b.StatusAt) || a.Status.OdometerKm != b.Status.OdometerKm {
		t.Fatalf("simulation went backwards: %v then %v", a.StatusAt, b.StatusAt)
	}
}

func TestSmallStepsEqualOneBigStep(t *testing.T) {
	a := mustSimulation(t, commute)
	b := mustSimulation(t, commute)
	end := start.Add(8 * time.Hour)
	for at := start; at.Before(end); at = at.Add(7 * time.Minute) {
		a.ReportAt(at)
	}
	ra, rb := a.ReportAt(end), b.ReportAt(end)
	if math.Abs(ra.Energy.SoC-rb.Energy.SoC) > 1e-6 || ra.Status.OdometerKm != rb.Status.OdometerKm {
		t.Fatalf("result depends on polling rate: %+v vs %+v", ra.Energy, rb.Energy)
	}
}

func TestShippedScenariosAreValid(t *testing.T) {
	files, err := filepath.Glob("../../../scenarios/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no scenario found (%v)", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f) //nolint:gosec // path comes from a glob over the repository
			if err != nil {
				t.Fatal(err)
			}
			sim := mustSimulation(t, string(data))
			if st := sim.StatusAt(sim.sc.Start.Add(72 * time.Hour)); !st.Finished {
				t.Errorf("scenario not finished after 72h: %+v", st)
			}
			if sim.VIN() == "" {
				t.Error("empty VIN")
			}
		})
	}
}
