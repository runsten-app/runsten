package notify

import (
	"strconv"
	"time"

	"runsten/internal/core"
	"runsten/internal/publish"
)

// The events of a vehicle's state, which no reading of a trip or a charge tells but the
// state watched over time: the rules are core's (BatteryLow, NotCharged) and publish's
// (LeftOpen), the same that MQTT's binary sensors state. Each is keyed by its episode or
// its night, so that it is told once at most, and never built from a reading old enough
// to be a past one, nor from a value the vehicle did not tell. They rest on stored
// readings only: no imported or reconstructed event gives one.

// EpisodeLookBack is how far back an episode of a low battery is looked for: past
// Retention, the key of its delivery may notify again.
const EpisodeLookBack = Retention

// OpeningsFresh is how old the reading of the doors and the windows may be for a vehicle
// to be told left open: a Volvo and a Polestar tell them once an hour.
const OpeningsFresh = 2 * time.Hour

// readSince tells whether each field was last read at or after from.
func readSince(c core.Current, from time.Time, fields ...core.Field) bool {
	for _, f := range fields {
		if _, checked := c.Read(f); checked.Before(from) {
			return false
		}
	}
	return true
}

// openingFields are the fields of the doors and the windows the vehicle tells.
func openingFields(s core.Snapshot) []core.Field {
	var out []core.Field
	for _, f := range []core.Field{core.FieldDoors, core.FieldWindows} {
		if s.Covers&f != 0 {
			out = append(out, f)
		}
	}
	return out
}

// NotChargedWithin is how long after the morning's check a night not charged is still
// told: the check runs every few minutes, and a collector restarted at that time must
// not tell the night after the owner left.
const NotChargedWithin = 2 * time.Hour

// BatteryLowEvent is the event of a vehicle parked with its state of charge under
// threshold (a percentage), since the episode began at since (core.LowSince); false when
// it is not, or its state of charge was last read more than Fresh before now. Its key is
// the episode's start: a charge to the threshold or above, then a new fall, is a new one.
func BatteryLowEvent(accountID, vehicleID string, c core.Current, threshold int, since, now time.Time) (Event, bool) {
	if since.IsZero() {
		return Event{}, false
	}
	if low := core.BatteryLow(c.Snapshot, float64(threshold)); !low.OK || !low.V {
		return Event{}, false
	}
	// Every reading the rule rests on is recent: the state of charge, the engine, and
	// the charge, else the cable (read disconnected, it tells the vehicle does not charge).
	s := c.Snapshot
	needed := []core.Field{core.FieldSoC, core.FieldEngine}
	if s.Charging.OK {
		needed = append(needed, core.FieldCharging)
	} else {
		needed = append(needed, core.FieldConnection)
	}
	if !readSince(c, now.Add(-Fresh), needed...) {
		return Event{}, false
	}
	return Event{
		Kind: BatteryLow, AccountID: accountID, VehicleID: vehicleID, At: since, Key: "low@" + stamp(since),
		Args: map[string]string{
			"soc":       strconv.FormatFloat(c.Snapshot.SoC.V, 'f', 0, 64),
			"threshold": strconv.Itoa(threshold),
		},
	}, true
}

// LeftOpenEvent is the event of a vehicle left open (publish.LeftOpen) since since
// (core.OpenSince); false when it is not, or the vehicle tells neither locks nor windows.
// Its key is the episode's start: closed, then opened again, is a new one.
func LeftOpenEvent(accountID, vehicleID string, c core.Current, since, now time.Time) (Event, bool) {
	if open := publish.LeftOpen(c, since, now); !open.OK || !open.V {
		return Event{}, false
	}
	// A value read long ago is unknown: no episode rests on it. The doors and windows of
	// some makers are told once an hour, so theirs may be older.
	if !readSince(c, now.Add(-Fresh), core.FieldEngine) || !readSince(c, now.Add(-OpeningsFresh), openingFields(c.Snapshot)...) {
		return Event{}, false
	}
	return Event{
		Kind: LeftOpen, AccountID: accountID, VehicleID: vehicleID, At: since, Key: "open@" + stamp(since),
		Args: map[string]string{"since": stamp(since)},
	}, true
}

// NotChargedEvent is the event of a night judged not charged (core.NotCharged), told from
// the morning's check to NotChargedWithin after it; false otherwise. Its key is the night's
// local date: once a night, whenever the check runs.
func NotChargedEvent(accountID, vehicleID string, n core.Value[core.NightCheck], now time.Time) (Event, bool) {
	if !n.OK || !n.V.NotCharged || now.Before(n.V.Morning) || now.After(n.V.Morning.Add(NotChargedWithin)) {
		return Event{}, false
	}
	return Event{
		Kind: NotCharged, AccountID: accountID, VehicleID: vehicleID, At: n.V.Morning, Key: n.V.DateKey(),
		Args: map[string]string{
			"soc":        strconv.FormatFloat(n.V.EveningSoC, 'f', 0, 64),
			"plugged_at": stamp(n.V.PluggedAt),
		},
	}, true
}

// MorningCheckDue tells whether it is, at some place, between 07:00 and NotChargedWithin
// after, on the place's clock: the time a night not charged is told (core.NotCharged).
func MorningCheckDue(places []core.Place, now time.Time) bool {
	for _, p := range places {
		loc := p.Location
		if loc == nil {
			loc = time.UTC
		}
		l := now.In(loc)
		// 07:00 on the wall clock, which a change of daylight saving time that day moves
		// no more than the clock.
		morning := time.Date(l.Year(), l.Month(), l.Day(), int(core.MorningAt/time.Hour), int(core.MorningAt%time.Hour/time.Minute), 0, 0, loc)
		if !l.Before(morning) && !l.After(morning.Add(NotChargedWithin)) {
			return true
		}
	}
	return false
}
