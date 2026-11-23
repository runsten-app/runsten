package core

import "time"

// The rules that tell a vehicle's state wants its owner: pure functions of the readings,
// which the notifications and MQTT's binary sensors share. An unknown value never says
// yes: without a state of charge read there is no low battery, without the cable read no
// night without a charge.

// BatteryLow tells whether the vehicle is parked with its state of charge strictly under
// threshold (in %). Parked: its engine read stopped, and no charge in progress; a vehicle
// that drives or charges is not low in this sense. Known not low when the state of charge
// is at or above the threshold, whatever else is known; unknown when it is not read, or
// when it is under the threshold and the vehicle neither tells its engine, nor whether it
// charges (a cable read disconnected tells that it does not).
func BatteryLow(s Snapshot, threshold float64) Value[bool] {
	if !s.SoC.OK {
		return Value[bool]{}
	}
	if s.SoC.V >= threshold {
		return known(false)
	}
	if !s.Engine.OK {
		return Value[bool]{}
	}
	if s.Engine.V != EngineStopped {
		return known(false)
	}
	switch {
	case s.Charging.OK:
		return known(s.Charging.V != ChargingActive)
	case s.Connection.OK && s.Connection.V == Disconnected:
		return known(true)
	}
	return Value[bool]{}
}

// LowSince returns when the episode in progress of a state of charge under threshold
// began: the first reading under it after the latest at or above it, whether the vehicle
// drove, charged or stood. Zero when the latest reading of the state of charge is not
// under it, or none is. A reading without a state of charge changes nothing. An episode
// that began before the first record given began at its reading, which a store that keeps
// a value once, from its first reading, gives at the instant it was first read.
func LowSince(records []Record, threshold float64) time.Time {
	var (
		st    state
		since time.Time
	)
	for _, p := range points(records) {
		st.apply(p)
		switch soc := st.snap.SoC; {
		case !soc.OK:
		case soc.V >= threshold:
			since = time.Time{}
		case since.IsZero():
			since = p.at
		}
	}
	return since
}

// The night of NotCharged, in the local time of the place the vehicle stands at.
const (
	// NightFrom and NightUntil bound the evening in which the cable must be read plugged
	// in at a place: from 18:00 to 02:00 the next day.
	NightFrom  = 18 * time.Hour
	NightUntil = 2 * time.Hour
	// MorningAt is when the night is judged, in the morning of the next day.
	MorningAt = 7 * time.Hour
	// nightStale is how old the last reading may be at the morning for the night to be
	// judged: a collector that was not running tells nothing of it.
	nightStale = 2 * time.Hour
	// fullMargin is how far below its limit the state of charge may stand for the vehicle to
	// be taken as full: it is read in whole points.
	fullMargin = 1
)

// NightCheck is the judgement of a night: the vehicle plugged in at a place in the
// evening, and whether its state of charge rose before the morning.
type NightCheck struct {
	// Date is the local date of the morning, at midnight, in the place's zone: the night's
	// identity.
	Date  time.Time
	Place Place
	// Morning is the instant it is judged: 07:00 on Date, local.
	Morning time.Time
	// PluggedAt is the first reading of the cable plugged in at the place in the evening.
	PluggedAt time.Time
	// EveningSoC is the state of charge then.
	EveningSoC float64
	// NotCharged: the vehicle was plugged in with room left to charge, its state of charge
	// never rose until the morning, no charge was seen delivering, and the cable is still
	// connected. False for a night without a plug-in, a charge, or a vehicle already full.
	NotCharged bool
}

// DateKey is the night's date as 2026-11-23, its key once per night.
func (n NightCheck) DateKey() string { return n.Date.Format(time.DateOnly) }

// Standing tells whether the vehicle is still as the night judged it at at: before the
// evening that ends the day, the cable still connected, no charge in progress and its
// state of charge not above the evening's. Unknown when the cable or the state of charge
// is not read.
func (n NightCheck) Standing(c Current, at time.Time) Value[bool] {
	if !n.NotCharged {
		return known(false)
	}
	s := c.Snapshot
	if !s.Connection.OK || !s.SoC.OK {
		return Value[bool]{}
	}
	if !at.Before(n.Date.Add(NightFrom)) {
		return known(false)
	}
	charging := s.Charging.OK && s.Charging.V == ChargingActive
	return known(s.Connection.V == Connected && !charging && s.SoC.V <= n.EveningSoC)
}

// NotCharged judges the latest night whose morning is at or before now, at each place
// the vehicle stood plugged in: the evening is 18:00 to 02:00 in the place's zone, the
// morning 07:00. The vehicle is not charged when:
//
//   - its cable is read connected at the place at some reading of the evening, its state of
//     charge and its limit known, and the state of charge more than a point under the
//     limit (a full vehicle has nothing to charge);
//   - from then to the morning no reading shows the state of charge above that of the
//     evening, nor a charge in progress;
//   - at the morning the cable is still read connected: a vehicle gone, or unplugged, has
//     been seen by its owner.
//
// Unknown (not OK) when the readings do not cover the night: the last one is more than two
// hours before the morning, or the cable is never read, or no place is given. A night with
// no plug-in at a place is known, and not a problem. Readings after the morning are
// ignored. Of several places, the one where the vehicle is not charged is returned, else
// the latest morning.
func NotCharged(records []Record, places []Place, now time.Time) Value[NightCheck] {
	ps := points(records)
	var best Value[NightCheck]
	for _, pl := range places {
		v := judgeNight(ps, places, pl, lastMorning(now, zone(pl)))
		switch {
		case !v.OK:
		case !best.OK, v.V.NotCharged && !best.V.NotCharged,
			v.V.NotCharged == best.V.NotCharged && v.V.Morning.After(best.V.Morning):
			best = v
		}
	}
	return best
}

func zone(p Place) *time.Location {
	if p.Location == nil {
		return time.UTC
	}
	return p.Location
}

// lastMorning is the local date, at midnight, of the latest morning at or before now.
func lastMorning(now time.Time, loc *time.Location) time.Time {
	l := now.In(loc)
	d := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	if l.Before(clock(d, MorningAt)) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// clock is the time of day offset on the wall clock of the local date d, which a change of
// daylight saving time that day does not move.
func clock(d time.Time, offset time.Duration) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), int(offset/time.Hour), int(offset%time.Hour/time.Minute), 0, 0, d.Location())
}

// judgeNight judges the night that ends on the morning of date, for the place pl.
func judgeNight(ps []point, places []Place, pl Place, date time.Time) Value[NightCheck] {
	from, until, morning := clock(date.AddDate(0, 0, -1), NightFrom), clock(date, NightUntil), clock(date, MorningAt)
	out := NightCheck{Date: date, Place: pl, Morning: morning}
	type plug struct {
		at          time.Time
		soc, target float64
	}
	var (
		st                      state
		last                    time.Time
		pluggedIn               *plug
		risen, delivered, cable bool
	)
	eligible := func(at time.Time) {
		s := st.snap
		if pluggedIn != nil || !s.Connection.OK || s.Connection.V != Connected || !s.Position.OK || !s.SoC.OK || !s.TargetSoC.OK {
			return
		}
		if here, ok := PlaceAt(s.Position.V, places); !ok || here.ID != pl.ID || here.Name != pl.Name {
			return
		}
		pluggedIn = &plug{at: at, soc: s.SoC.V, target: s.TargetSoC.V}
	}
	// The state at the start of the evening, before the first reading after it: a cable
	// plugged in earlier and left stands in it.
	atStart := false
	for _, p := range ps {
		if !atStart && p.at.After(from) {
			eligible(from)
			atStart = true
		}
		if p.at.After(morning) {
			break
		}
		st.apply(p)
		last = p.at
		cable = cable || st.snap.Connection.OK
		if pluggedIn != nil && p.at.After(pluggedIn.at) {
			risen = risen || st.snap.SoC.OK && st.snap.SoC.V > pluggedIn.soc
			delivered = delivered || st.snap.Charging.OK && st.snap.Charging.V == ChargingActive
		}
		if !p.at.Before(from) && p.at.Before(until) {
			eligible(p.at)
		}
	}
	if !atStart {
		eligible(from)
	}
	if !cable || last.Before(morning.Add(-nightStale)) {
		return Value[NightCheck]{}
	}
	if pluggedIn == nil {
		return known(out)
	}
	out.PluggedAt, out.EveningSoC = pluggedIn.at, pluggedIn.soc
	if pluggedIn.soc >= pluggedIn.target-fullMargin {
		return known(out)
	}
	if !st.snap.Connection.OK {
		return Value[NightCheck]{}
	}
	out.NotCharged = st.snap.Connection.V == Connected && !risen && !delivered
	return known(out)
}
