package aximoteapi

import "time"

// Default is the account the simulator serves: an electric car with the VIN vin, and a
// petrol one Aximote knows no VIN of. The electric car's history ends a month before
// base, the simulator's start, so that it stays before the first reading of its
// vehicle, in an earlier month (neither "this month" nor "since yesterday" holds it):
// two trips, one of them with its track, and a trip still unended; a charge with its
// cost and its battery energies, one in progress, and a fuel stop, which the importer
// leaves (a plug-in hybrid's). Fictitious places (Lyon).
func Default(base time.Time, vin string) Data {
	day := base.UTC().Truncate(24*time.Hour).AddDate(0, -1, -3)
	at := func(days, h, m int) time.Time {
		return day.AddDate(0, 0, days).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}
	ptr := func(t time.Time) *time.Time { return &t }
	lyon := func(lat, lon float64, locality string) *Point {
		return &Point{Latitude: lat, Longitude: lon, Locality: &locality, Country: new("France")}
	}
	home, work := lyon(45.7640, 4.8357, "Lyon"), lyon(45.7797, 4.9270, "Villeurbanne")
	return Data{
		Vehicles: []Vehicle{
			{
				ID: "veh-ex30", Name: new("My EX30"), Make: new("Volvo"), Model: new("EX30"), Year: new(2024), VIN: &vin,
				FuelType: []string{"ELECTRIC"}, BatteryCapacityKWh: new(64.0), OdometerKm: new(8150.0),
			},
			{
				ID: "veh-v60", Name: new("Old V60"), Make: new("Volvo"), Model: new("V60"), Year: new(2016),
				FuelType: []string{"PETROL"},
			},
		},
		Trips: []Trip{
			{
				ID: "trip-1001", VehicleID: "veh-ex30", StartTime: at(0, 8, 0), EndTime: ptr(at(0, 8, 35)), DurationSec: new(2100),
				DistanceKm: new(24.5), EnergyConsumedKWh: new(5.1), StartLocation: home, EndLocation: work, Tags: []string{},
				Track: [][2]float64{{4.8357, 45.7640}, {4.8600, 45.7700}, {4.9000, 45.7760}, {4.9270, 45.7797}},
			},
			{
				ID: "trip-1002", VehicleID: "veh-ex30", StartTime: at(0, 17, 30), EndTime: ptr(at(0, 18, 10)), DurationSec: new(2400),
				DistanceKm: new(25.1), EnergyConsumedKWh: new(5.6), StartLocation: work, EndLocation: home, Tags: []string{"commute"},
			},
			// Still driving, as Aximote saw it last: no end, left out.
			{ID: "trip-1003", VehicleID: "veh-ex30", StartTime: at(1, 9, 0), StartLocation: home, Tags: []string{}},
			{
				ID: "trip-2001", VehicleID: "veh-v60", StartTime: at(0, 10, 0), EndTime: ptr(at(0, 10, 20)),
				DistanceKm: new(12.0), StartLocation: home, EndLocation: work, Tags: []string{},
			},
		},
		Refuels: []Refuel{
			{
				ID: "charge-501", VehicleID: "veh-ex30", Type: "CHARGE", StartTime: at(0, 19, 0), EndTime: ptr(at(0, 21, 0)),
				StartEnergyKWh: new(41.6), EndEnergyKWh: new(57.6), EnergyChargedKWh: new(16.8), AvgChargingKW: new(8.4),
				CostAmount: new(4.37), CostCurrency: new("EUR"), Location: home,
				ResolvedStartTime: ptr(at(0, 19, 5)), ResolvedEndTime: ptr(at(0, 20, 50)), WindowSource: new("CHARGING"),
			},
			{ID: "charge-502", VehicleID: "veh-ex30", Type: "CHARGE", StartTime: at(1, 20, 0), IsActive: true, Location: home},
			{
				ID: "fuel-503", VehicleID: "veh-ex30", Type: "FUEL", StartTime: at(1, 12, 0), EndTime: ptr(at(1, 12, 10)),
				CostAmount: new(30.0), CostCurrency: new("EUR"),
			},
		},
	}
}
