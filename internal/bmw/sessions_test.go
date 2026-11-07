package bmw

import (
	"reflect"
	"testing"
	"time"

	"runsten/internal/core"
)

func TestNormalizeSessions(t *testing.T) {
	raw := []byte(`{"data": [
		{"startTime": 1790000000, "endTime": 1790010800, "displayedStartSoc": 41, "displayedSoc": 80,
		 "energyConsumedFromPowerGridKwh": 34.25, "mileage": 12345, "mileageUnits": "MileageUnits.KM",
		 "chargingLocation": {"mapMatchedLatitude": 50.85, "mapMatchedLongitude": 4.35}},
		{"startTime": "1789900000000", "endTime": "1789903600000", "displayedStartSoc": "20",
		 "energyConsumedFromPowerGridKwh": "5.5", "mileage": 100, "mileageUnits": "MileageUnits.MILES",
		 "chargingLocation": {"mapMatchedLatitude": 0, "mapMatchedLongitude": 0}},
		{"startTime": 1789800000, "endTime": 1789700000, "energyConsumedFromPowerGridKwh": 1},
		{"endTime": 1789700000, "energyConsumedFromPowerGridKwh": 1},
		{"startTime": 1789600000, "endTime": 1789600000, "energyConsumedFromPowerGridKwh": -2, "displayedSoc": 120}
	]}`)
	got, err := NormalizeSessions(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := func(f float64) core.Value[float64] { return core.Value[float64]{V: f, OK: true} }
	want := []core.ChargeSession{
		{
			Start: time.Unix(1790000000, 0).UTC(), End: time.Unix(1790010800, 0).UTC(),
			EnergyGridKWh: v(34.25), StartSoC: v(41), EndSoC: v(80), OdometerKm: v(12345),
			Position: core.Value[core.Position]{V: core.Position{Lat: 50.85, Lon: 4.35}, OK: true},
		},
		{
			// Milliseconds, strings, miles; a position at 0, 0 is none.
			Start: time.Unix(1789900000, 0).UTC(), End: time.Unix(1789903600, 0).UTC(),
			EnergyGridKWh: v(5.5), StartSoC: v(20), OdometerKm: v(160.9344),
		},
		// Ending before it starts, and without a start: left out. A negative energy and a
		// state of charge above 100 are unknown.
		{Start: time.Unix(1789600000, 0).UTC(), End: time.Unix(1789600000, 0).UTC()},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%+v\nwant\n%+v", got, want)
	}
}

func TestNormalizeSessionsKinds(t *testing.T) {
	for _, tt := range []struct {
		name    string
		kind    core.Kind
		raw     []byte
		wantErr bool
	}{
		{"another source's response", core.Kind{Source: "volvo", Endpoint: string(ChargingHistory)}, nil, true},
		{"another endpoint reads no session", core.Kind{Source: Source, Endpoint: string(BasicData)}, []byte(`{}`), false},
		{"an unreadable response", core.Kind{Source: Source, Endpoint: string(ChargingHistory)}, []byte(`[`), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalizer{}.NormalizeSessions(tt.kind, tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != nil {
				t.Errorf("sessions = %v, want none", got)
			}
		})
	}
}
