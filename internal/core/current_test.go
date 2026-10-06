package core

import (
	"testing"
	"time"
)

func details(kwh float64, family string, year int, electric bool) Snapshot {
	return Snapshot{
		Covers: FieldCapacity | FieldModel, CapacityKWh: Some(kwh, time.Time{}),
		Family: Some(family, time.Time{}), ModelYear: Some(year, time.Time{}), BatteryElectric: Some(electric, time.Time{}),
	}
}

func TestLatest(t *testing.T) {
	for _, tt := range []struct {
		name    string
		records []Record
		check   func(t *testing.T, c Current)
	}{
		{
			name: "no record",
			check: func(t *testing.T, c Current) {
				if c.Snapshot.Covers != 0 || !c.CheckedAt().IsZero() {
					t.Errorf("current = %+v, checked at %s", c.Snapshot, c.CheckedAt())
				}
				if f, k := c.Read(FieldSoC); !f.IsZero() || !k.IsZero() {
					t.Errorf("SoC read at %s, %s", f, k)
				}
			},
		},
		{
			name: "latest value of each field, with its own reading times",
			records: []Record{
				rec(30, 50, nrg(70, ChargingIdle)),
				rec(0, 20, nrg(60, ChargingActive)), // unsorted, older
				rec(10, 40, odo(12400)),
				rec(5, 5, loc(home, m(4))),
			},
			check: func(t *testing.T, c Current) {
				s := c.Snapshot
				if s.SoC.V != 70 || s.Charging.V != ChargingIdle || s.OdometerKm.V != 12400 || s.Position.V != home ||
					!s.Position.At.Equal(m(4)) {
					t.Errorf("snapshot = %+v", s)
				}
				for _, w := range []struct {
					f                Field
					fetched, checked time.Time
				}{
					{FieldSoC, m(30), m(50)},
					{FieldOdometer, m(10), m(40)},
					{FieldPosition, m(5), m(5)},
				} {
					if f, k := c.Read(w.f); !f.Equal(w.fetched) || !k.Equal(w.checked) {
						t.Errorf("field %d read at %s, %s; want %s, %s", w.f, f, k, w.fetched, w.checked)
					}
				}
				if !c.CheckedAt().Equal(m(50)) {
					t.Errorf("checked at %s", c.CheckedAt())
				}
			},
		},
		{
			name: "a covered but absent value replaces the previous one",
			records: []Record{
				rec(0, 0, loc(home, m(0))),
				rec(10, 10, Snapshot{Covers: FieldPosition}),
			},
			check: func(t *testing.T, c Current) {
				if c.Snapshot.Position.OK {
					t.Errorf("position = %+v", c.Snapshot.Position)
				}
			},
		},
		{
			name: "the model is kept like the capacity",
			records: []Record{
				rec(0, 0, details(69, "EX30", 2024, true)),
				rec(10, 10, nrg(70, ChargingIdle)),
			},
			check: func(t *testing.T, c Current) {
				s := c.Snapshot
				if s.CapacityKWh.V != 69 || s.Family.V != "EX30" || s.ModelYear.V != 2024 || !s.BatteryElectric.OK ||
					!s.BatteryElectric.V {
					t.Errorf("snapshot = %+v", s)
				}
				if f, k := c.Read(FieldModel); !f.Equal(m(0)) || !k.Equal(m(0)) {
					t.Errorf("model read at %s, %s", f, k)
				}
			},
		},
		{
			name: "a details reading without the model clears it",
			records: []Record{
				rec(0, 0, details(69, "EX30", 2024, true)),
				rec(10, 10, Snapshot{Covers: FieldCapacity | FieldModel}),
			},
			check: func(t *testing.T, c Current) {
				if s := c.Snapshot; s.CapacityKWh.OK || s.Family.OK || s.ModelYear.OK || s.BatteryElectric.OK {
					t.Errorf("snapshot = %+v", s)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) { tt.check(t, Latest(tt.records)) })
	}
}
