// Package demo is a vehicle that needs no car and no maker's account: the instance
// simulates it, one week of driving and charging (commutes, errands, a long Saturday with
// a fast charge, home charging on most evenings) repeated from a fixed origin, on the
// simulator's vehicle model.
//
// The vehicle's state at an instant depends on that instant alone, whatever restarted
// meanwhile, so that the history written when it is connected and the readings that follow
// agree: each week starts from the same state (plugged in at home, charged to the usual
// target), and the odometer grows by what a week drives. A Cursor reads it, forward in
// time; the readings are small JSON documents of this package's own, which the Normalizer
// turns into core.Snapshot as it does for the other sources' responses.
//
// It is the one package outside the simulator that uses its pure packages (vehicle and
// scenario), never the providers' handlers.
package demo

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

// Source names the demo among the sources of readings, and the provider of its
// connections.
const Source = "demo"

// Endpoint is what the collector reads of the vehicle: the one document.
type Endpoint string

// Vehicle is the one endpoint.
const Vehicle Endpoint = "vehicle"

// The paces the vehicle is read at, which the collector's source and the history written
// at connection share: often while it drives or charges, seldom while it is parked.
const (
	ActiveEvery = time.Minute
	ParkedEvery = 10 * time.Minute
)

// HistoryDays is how far back the history written at connection goes.
const HistoryDays = 28

// Reading is the vehicle's state at an instant, as the collector stores it. Its values are
// rounded as a vehicle would report them, so that a parked vehicle gives the same reading
// again.
type Reading struct {
	At          time.Time `json:"at"`
	Brand       string    `json:"brand"`
	Family      string    `json:"family"`
	ModelYear   int       `json:"model_year"`
	Name        string    `json:"name"`
	CapacityKWh float64   `json:"capacity_kwh"`

	SoC          float64 `json:"soc"`
	RangeKm      float64 `json:"range_km"`
	TargetSoC    float64 `json:"target_soc"`
	OdometerKm   float64 `json:"odometer_km"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	OutsideTempC float64 `json:"outside_temp_c"`

	Driving bool `json:"driving"`
	Plugged bool `json:"plugged"`
	// Charging is charging, done or idle; ChargeType ac, dc or empty.
	Charging   string  `json:"charging"`
	ChargeType string  `json:"charge_type"`
	PowerKW    float64 `json:"power_kw"`
}

// Active reports whether the vehicle drives or charges: it is then read more often.
func (r Reading) Active() bool { return r.Driving || r.Charging == "charging" }

// Demo is the compiled week. It is immutable: any number of Cursors read it.
type Demo struct {
	plan plan
}

// New compiles the week and checks that the vehicle, with its places and steps, is a
// scenario the simulator accepts.
func New() (*Demo, error) {
	p, err := compile(timetable())
	if err != nil {
		return nil, fmt.Errorf("demo week: %w", err)
	}
	d := &Demo{plan: p}
	if err := d.scenario(0).Validate(); err != nil {
		return nil, fmt.Errorf("demo week: %w", err)
	}
	return d, nil
}

// WeekKm is what the vehicle drives in a week.
func (d *Demo) WeekKm() float64 { return d.plan.km }

// scenario is the week with index week (from Origin) as a scenario of its own: it starts
// at the week's Monday, with the odometer of the weeks before it.
func (d *Demo) scenario(week int64) *scenario.Scenario {
	pl := make(map[string]scenario.Place, len(places()))
	for name, p := range places() {
		pl[name] = scenario.Place{Lat: p.lat, Lon: p.lon}
	}
	return &scenario.Scenario{
		VIN: VIN, Start: Origin().Add(time.Duration(week)*Week - tick),
		Vehicle: scenario.VehicleConfig{
			Profile: "ex30-like", Model: family, ModelYear: modelYear, BatteryKWh: capacityKWh, UsableKWh: usableKWh,
			ConsumptionKWhPer100km: consumption, SoC: chargeSoC, TargetSoC: chargeSoC,
			OdometerKm: firstOdometerKm + float64(week)*d.plan.km, Place: "home",
		},
		Places: pl,
		Steps:  d.plan.steps,
	}
}

// Cursor reads the vehicle at instants that mostly go forward: it keeps the simulation of
// the week it reads, advancing it, and starts it over from the week's Monday when an
// instant is before the last one read or in another week. Not safe for concurrent use.
type Cursor struct {
	d    *Demo
	week int64
	sim  *scenario.Simulation
	last time.Time
}

// Cursor returns a new Cursor.
func (d *Demo) Cursor() *Cursor { return &Cursor{d: d} }

// ReadingAt returns the vehicle's state at t. An instant before Origin reads as Origin.
func (c *Cursor) ReadingAt(t time.Time) (Reading, error) {
	t = t.UTC()
	if t.Before(Origin()) {
		t = Origin()
	}
	// The simulation is read at whole seconds: its ticks then always fall on the same
	// instants, whatever instants were read before (a read in between would shift the end of a
	// charge, and the drives after it, by under a second). At the very second a drive ends and
	// a charge starts, the vehicle would be parked and unplugged for the length of a tick, and
	// the collector, which reads a parked vehicle every ten minutes and a charging one every
	// minute, would not see the charge for ten: it is read a tick on, plugged in.
	at := t.Truncate(time.Second)
	week := int64(t.Sub(Origin()) / Week)
	if c.sim == nil || week != c.week || at.Before(c.last) {
		sim, err := scenario.NewSimulation(c.d.scenario(week), vehicle.DefaultUploadPolicy())
		if err != nil {
			return Reading{}, fmt.Errorf("demo simulation: %w", err)
		}
		c.sim, c.week = sim, week
	}
	off := t.Sub(Origin().Add(time.Duration(week) * Week))
	simAt := at
	if slices.Contains(c.d.plan.plugAt, off.Truncate(time.Second)) {
		simAt = at.Add(tick)
	}
	c.last = simAt
	st := c.sim.StateAt(simAt)
	lat, lon := c.d.plan.legAt(off).at(off)
	r := Reading{
		At: t, Brand: brand, Family: family, ModelYear: modelYear, Name: "Demo " + family, CapacityKWh: capacityKWh,
		SoC:          round(st.SoC, 1),
		RangeKm:      math.Round(st.SoC / 100 * usableKWh / consumption * 100),
		TargetSoC:    chargeSoC, // the owner's limit: the steps of a fast charge's taper are not
		OdometerKm:   round(st.OdometerKm, 1),
		Latitude:     round(lat, 5),
		Longitude:    round(lon, 5),
		OutsideTempC: outsideTempC(t),
		Driving:      st.Driving,
		Plugged:      st.PluggedIn,
		PowerKW:      round(st.ChargingPowerW/1000, 1),
	}
	switch st.Charging {
	case vehicle.Charging:
		r.Charging = "charging"
	case vehicle.Done:
		r.Charging = "done"
	default:
		r.Charging = "idle"
	}
	switch st.ChargeType {
	case vehicle.AC:
		r.ChargeType = "ac"
	case vehicle.DC:
		r.ChargeType = "dc"
	}
	return r, nil
}

// outsideTempC is a plausible temperature, in whole °C, for a temperate climate: the
// season, the hour and a few days' weather, all pure functions of t.
func outsideTempC(t time.Time) float64 {
	season := 11 + 8*math.Sin(2*math.Pi*float64(t.YearDay()-110)/365.25)
	hour := float64(t.Hour()) + float64(t.Minute())/60
	day := 4 * math.Sin(2*math.Pi*(hour-9)/24)
	weather := 2.5 * math.Sin(2*math.Pi*float64(t.Unix()/86400)/5.3)
	return math.Round(season + day + weather)
}

func round(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}

// Marshal is the stored form of a reading.
func Marshal(r Reading) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("demo reading: %w", err)
	}
	return b, nil
}

// Parse reads a stored reading.
func Parse(raw []byte) (Reading, error) {
	var r Reading
	if err := json.Unmarshal(raw, &r); err != nil {
		return Reading{}, fmt.Errorf("demo reading: %w", err)
	}
	return r, nil
}

// Sample is a reading as stored: its instant, its document and its fingerprint.
type Sample struct {
	At      time.Time
	Payload []byte
	Hash    []byte
}

// History reads the vehicle from from to to at the paces the collector would have: every
// ActiveEvery while it drives or charges, every ParkedEvery otherwise.
func (d *Demo) History(from, to time.Time) ([]Sample, error) {
	var out []Sample
	c := d.Cursor()
	for t := from.UTC(); !t.After(to); {
		r, err := c.ReadingAt(t)
		if err != nil {
			return nil, err
		}
		raw, err := Marshal(r)
		if err != nil {
			return nil, err
		}
		hash, err := Fingerprint(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, Sample{At: t, Payload: raw, Hash: hash})
		if r.Active() {
			t = t.Add(ActiveEvery)
		} else {
			t = t.Add(ParkedEvery)
		}
	}
	return out, nil
}
