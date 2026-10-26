package core

import (
	"reflect"
	"testing"
	"time"
)

func TestLatestHealth(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	tyres := func(at time.Time, kPa float64) Health {
		return Health{
			Covers:             HealthTyres,
			Tyres:              map[Wheel]Tyre{FrontLeft: {PressureKPa: Some(kPa, at), Warning: Some(TyreOK, at)}},
			RecommendedRearKPa: Some(290.0, at),
		}
	}
	records := []HealthRecord{
		// Out of order: the latest of each part wins.
		{FetchedAt: t0.Add(2 * time.Hour), CheckedAt: t0.Add(3 * time.Hour), Health: tyres(t0.Add(2*time.Hour), 270)},
		{FetchedAt: t0, CheckedAt: t0, Health: tyres(t0, 280)},
		{FetchedAt: t0, CheckedAt: t0.Add(time.Hour), Health: Health{
			Covers:  HealthService,
			Service: Some(ServiceSoon, t0), DaysToService: Some(20.0, t0),
			Checks: map[Check]Value[bool]{CheckOil: Some(true, t0), CheckCoolant: Some(false, t0)},
		}},
		// A later response of the checks replaces those it reports only.
		{FetchedAt: t0.Add(time.Hour), CheckedAt: t0.Add(time.Hour), Health: Health{
			Checks: map[Check]Value[bool]{CheckOil: Some(false, t0.Add(time.Hour))},
		}},
	}
	got := LatestHealth(records)
	want := CurrentHealth{
		Health: Health{
			Covers:             HealthTyres | HealthService,
			Tyres:              tyres(t0.Add(2*time.Hour), 270).Tyres,
			RecommendedRearKPa: Some(290.0, t0.Add(2*time.Hour)),
			Service:            Some(ServiceSoon, t0), DaysToService: Some(20.0, t0),
			Checks: map[Check]Value[bool]{CheckOil: Some(false, t0.Add(time.Hour)), CheckCoolant: Some(false, t0)},
		},
		CheckedAt: t0.Add(3 * time.Hour),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if !got.Pressures() || (Health{Covers: HealthTyres, Tyres: map[Wheel]Tyre{RearLeft: {Warning: Some(TyreLow, t0)}}}).Pressures() {
		t.Error("pressures")
	}
	if c := LatestHealth(nil); c.Covers != 0 || !c.CheckedAt.IsZero() || c.Checks != nil {
		t.Errorf("none: %+v", c)
	}
}

func TestTyreRuns(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	rec := func(fetched, checked time.Duration, kPa float64) HealthRecord {
		at := t0.Add(fetched)
		return HealthRecord{FetchedAt: at, CheckedAt: t0.Add(checked), Health: Health{
			Covers: HealthTyres, Tyres: map[Wheel]Tyre{RearLeft: {PressureKPa: Some(kPa, at)}},
			RecommendedRearKPa: Some(280.0, at),
		}}
	}
	records := []HealthRecord{
		rec(3*time.Hour, 3*time.Hour, 260),
		rec(0, time.Hour, 270),
		// Warnings only: no pressure, left out.
		{FetchedAt: t0.Add(30 * time.Minute), CheckedAt: t0.Add(30 * time.Minute), Health: Health{
			Covers: HealthTyres, Tyres: map[Wheel]Tyre{RearLeft: {Warning: Some(TyreLow, t0)}},
		}},
		{FetchedAt: t0, CheckedAt: t0, Health: Health{Covers: HealthService}},
	}
	runs := TyreRuns(records, t0.Add(30*time.Minute), t0.Add(4*time.Hour), 90*time.Minute)
	if len(runs) != 2 || len(runs[0]) != 2 || len(runs[1]) != 1 {
		t.Fatalf("runs %+v", runs)
	}
	// The first record holds across from: its value at that edge.
	if r := runs[0][0]; !r.At.Equal(t0.Add(30*time.Minute)) || r.PressureKPa[RearLeft].V != 270 || r.RecommendedRearKPa.V != 280 {
		t.Errorf("first %+v", r)
	}
	if r := runs[1][0]; !r.At.Equal(t0.Add(3*time.Hour)) || r.PressureKPa[RearLeft].V != 260 {
		t.Errorf("second run %+v", r)
	}
	if runs := TyreRuns(records, t0.Add(5*time.Hour), t0.Add(6*time.Hour), time.Hour); runs != nil {
		t.Errorf("outside: %+v", runs)
	}
}

func TestChecks(t *testing.T) {
	seen := map[Check]bool{}
	lights := 0
	for _, c := range Checks() {
		if seen[c] {
			t.Errorf("%s twice", c)
		}
		seen[c] = true
		if c.Light() {
			lights++
		}
	}
	if lights != len(Checks())-5 || CheckOil.Light() || !CheckLightHazard.Light() {
		t.Errorf("%d lights", lights)
	}
	if !FrontLeft.Front() || RearRight.Front() || len(Wheels()) != 4 {
		t.Error("wheels")
	}
}
