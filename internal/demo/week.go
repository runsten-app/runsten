package demo

import (
	"fmt"
	"math"
	"time"

	"runsten/internal/simulator/scenario"
)

// The vehicle: a Volvo EX30 Single Motor Extended Range of 2024, a variant the catalog
// recognizes (its motor code, EL, is at the VIN's 6th and 7th characters), on the
// simulator's EX30 profile. Fictitious VIN.
const (
	// VIN is the demo vehicle's, the same for every account: a vehicle is keyed by its
	// account and its VIN.
	VIN = "YV12ZEL000DEM0001"

	brand       = "volvo"
	family      = "EX30"
	modelYear   = 2024
	capacityKWh = 69.0 // what Volvo's API reports of the battery: its gross capacity
	usableKWh   = 64.0 // what the state of charge runs over
	consumption = 17.5 // kWh per 100 km
	chargeSoC   = 80.0 // the home charge's target: the week starts from it, and ends on it

	firstOdometerKm = 12000.0 // the odometer at the origin
)

// Origin is the start of the first week of the scenario: a Monday, 00:00 UTC. Every later
// week starts a whole number of weeks after it and runs the same week again.
var Origin = time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)

// Week is how long the scenario lasts before it starts again.
const Week = 7 * 24 * time.Hour

// Days of the week, as offsets from its Monday 00:00 UTC.
const (
	mon = time.Duration(iota) * 24 * time.Hour
	tue
	wed
	thu
	fri
	sat
	sun
)

// place is a named location of the week: around Ghent, with a destination in the
// Ardennes for the weekend. Real-looking, generic coordinates.
type place struct{ lat, lon float64 }

var places = map[string]place{
	"home":   {51.0522, 3.7250},
	"work":   {51.1005, 3.6860},
	"shop":   {51.0631, 3.7403},
	"gym":    {51.0498, 3.7098},
	"family": {51.1860, 3.5600},
	"dinant": {50.2611, 4.9124},
}

// step is one thing of the week, in order: a drive or a charge. A drive starts at its own time
// (the vehicle stays parked until then); a charge starts as the previous step ends, a driver
// plugging in on arriving.
type step struct {
	at       time.Duration // a drive's start, from the Monday
	to       string        // a drive's destination: a step with one is a drive
	km       float64
	duration time.Duration // a drive's
	powerKW  float64       // a charge's, until untilSoC
	untilSoC float64
	dc       bool
}

func hm(h, m int) time.Duration { return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute }

func drive(at time.Duration, to string, km float64, d time.Duration) step {
	return step{at: at, to: to, km: km, duration: d}
}

// plugHome is the charge at home, on a wall box, up to the usual target.
func plugHome() step { return step{powerKW: 11, untilSoC: chargeSoC} }

func commuteOut(day time.Duration) step { return drive(day+hm(6, 40), "work", 14, 28*time.Minute) }

func commuteBack(day time.Duration) step { return drive(day+hm(15, 45), "home", 14, 32*time.Minute) }

// timetable is the week, in UTC: five commutes, a stop at the shop on Wednesday and at the
// gym on Thursday, a long Saturday to the Ardennes with a fast charge on arriving (taper
// included: the vehicle is plugged in as it arrives, so that the collector, which reads a
// parked vehicle every ten minutes and a charging one every minute, sees the charge from its
// start), a visit on Sunday; charged at home each evening but Friday's.
func timetable() []step {
	return []step{
		plugHome(), // plugged in and charged since Sunday: the week starts as the last one ended
		commuteOut(mon), commuteBack(mon), plugHome(),
		commuteOut(tue), commuteBack(tue), plugHome(),
		commuteOut(wed),
		drive(wed+hm(15, 45), "shop", 7, 18*time.Minute),
		drive(wed+hm(16, 40), "home", 6, 14*time.Minute), plugHome(),
		commuteOut(thu), commuteBack(thu), plugHome(),
		drive(thu+hm(17, 30), "gym", 7, 18*time.Minute),
		drive(thu+hm(19, 0), "home", 7, 18*time.Minute), plugHome(),
		commuteOut(fri), commuteBack(fri),
		drive(sat+hm(6, 30), "dinant", 165, 2*time.Hour+5*time.Minute),
		{powerKW: 125, untilSoC: 50, dc: true},
		{powerKW: 90, untilSoC: 65, dc: true},
		{powerKW: 50, untilSoC: chargeSoC, dc: true},
		drive(sat+hm(9, 30), "home", 165, 2*time.Hour+5*time.Minute), plugHome(),
		drive(sun+hm(9, 30), "family", 22, 30*time.Minute),
		drive(sun+hm(15, 0), "home", 22, 30*time.Minute), plugHome(),
	}
}

// leg is a stretch of the week the vehicle spends driving or standing, for its position.
type leg struct {
	from, until  time.Duration // offsets from the Monday
	origin, dest place
	driving      bool
}

// at is the position at offset off of the leg: the place it stands at, or a point of its
// way, a straight line bent a little so that a track does not look drawn with a ruler.
func (l leg) at(off time.Duration) (lat, lon float64) {
	if !l.driving {
		return l.origin.lat, l.origin.lon
	}
	f := min(max(float64(off-l.from)/float64(l.until-l.from), 0), 1)
	dLat, dLon := l.dest.lat-l.origin.lat, l.dest.lon-l.origin.lon
	bend := 0.06 * math.Sin(math.Pi*f)
	return l.origin.lat + dLat*f - dLon*bend, l.origin.lon + dLon*f + dLat*bend
}

// plan is the week compiled: the scenario's steps, the legs of the vehicle's position, and
// what a week adds to the odometer.
type plan struct {
	steps []scenario.Step
	legs  []leg
	km    float64
	// plugAt are the offsets at which a drive or a charge ends and a charge starts at once (the
	// fast charge's steps follow each other): the vehicle is plugged in, and the new power
	// reached, a tick later.
	plugAt []time.Duration
}

// tick is the scenario's step: a charge already at its target takes one, and the week's first,
// which has the vehicle plugged in, starts one before the Monday so that it is at the Monday.
const tick = time.Second

// compile turns the timetable into steps with exact timing: the parks between them are
// worked out from the time each drive starts and from how long each charge takes, which
// follows from its energy and its power (the vehicle's own arithmetic: its charge ends on
// the tick that reaches the target). It tracks the state of charge to know that energy.
func compile(w []step) (plan, error) {
	var (
		p       plan
		cursor  = -tick // where the last step ended: the first charge starts a tick before the Monday
		soc     = chargeSoC
		here    = "home"
		since   time.Duration // since when the vehicle stands at here
		chained bool
	)
	for _, s := range w {
		if s.to == "" { // a charge
			if chained {
				p.plugAt = append(p.plugAt, cursor)
			}
			chained = true
			d, after := chargeTicks(soc, s)
			soc = after
			typ := "AC"
			if s.dc {
				typ = "DC"
			}
			p.steps = append(p.steps, scenario.Step{Charge: &scenario.Charge{Type: typ, PowerKW: s.powerKW, UntilSoC: s.untilSoC}})
			cursor += d
			continue
		}
		if s.at < cursor {
			return plan{}, fmt.Errorf("the drive to %s at %v starts before the previous step ends, at %v", s.to, s.at, cursor)
		}
		if s.at > cursor {
			p.steps = append(p.steps, scenario.Step{Park: &scenario.Park{Duration: s.at - cursor}})
		}
		p.legs = append(p.legs,
			leg{from: since, until: s.at, origin: places[here], dest: places[here]},
			leg{from: s.at, until: s.at + s.duration, origin: places[here], dest: places[s.to], driving: true})
		p.steps = append(p.steps, scenario.Step{Drive: &scenario.Drive{To: s.to, DistanceKm: s.km, Duration: s.duration}})
		soc = driveTicks(soc, s)
		chained = true
		if soc < 5 {
			return plan{}, fmt.Errorf("the drive to %s at %v leaves %.1f %% of charge", s.to, s.at, soc)
		}
		p.km += s.km
		here, cursor, since = s.to, s.at+s.duration, s.at+s.duration
	}
	if here != "home" || soc != chargeSoC {
		return plan{}, fmt.Errorf("the week ends at %s with %.1f %%, and must end where and as it starts", here, soc)
	}
	if cursor >= Week {
		return plan{}, fmt.Errorf("the week's steps last %v, over a week", cursor)
	}
	p.steps = append(p.steps, scenario.Step{Park: &scenario.Park{Duration: Week - cursor}})
	p.legs = append(p.legs, leg{from: since, until: Week, origin: places[here], dest: places[here]})
	return p, nil
}

// legAt returns the leg holding offset off.
func (p plan) legAt(off time.Duration) leg {
	for _, l := range p.legs {
		if off < l.until {
			return l
		}
	}
	return p.legs[len(p.legs)-1]
}

// chargeTicks is how long a charge lasts from soc, and the state of charge it ends on, by the
// vehicle's own arithmetic, tick by tick: it ends on the tick that reaches the target, and one
// already there lasts a tick. Estimating it would be off by a tick now and then, and the
// drives after it, which start a fixed time after it ends, with it.
func chargeTicks(soc float64, s step) (time.Duration, float64) {
	if soc >= s.untilSoC {
		return tick, soc
	}
	var d time.Duration
	for {
		d += tick
		kwh := s.powerKW * 1000 / 1000 * tick.Hours()
		next := soc + kwh/usableKWh*100
		if next >= s.untilSoC {
			return d, s.untilSoC
		}
		soc = next
	}
}

// driveTicks is the state of charge after a drive, tick by tick as the vehicle consumes it.
func driveTicks(soc float64, s step) float64 {
	kmPerTick := s.km * float64(tick) / float64(s.duration)
	for range int(s.duration / tick) {
		soc = max(0, min(100, soc-kmPerTick*consumption/100/usableKWh*100))
	}
	return soc
}
