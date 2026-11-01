package publish

import (
	"time"

	"runsten/internal/core"
)

// The lock's payloads.
const (
	Locked   = "locked"
	Unlocked = "unlocked"
)

// LeftOpenAfter is how long the vehicle stands parked and open (core.OpenSince) before it
// is said left open: long enough to carry the shopping in.
const LeftOpenAfter = 10 * time.Minute

// part is the lock, a door, a lid, a window or Sentry Mode, as published: its topic, its
// Home Assistant entity, and its value in a snapshot, unknown when it does not tell it.
type part struct {
	topic, name, deviceClass, template string
	value                              func(core.Snapshot) core.Value[string]
}

// openTemplate is on for a door, a lid or a window open or ajar.
const openTemplate = "{{ 'None' if value == '' else ('OFF' if value == '" + string(core.Closed) + "' else 'ON') }}"

func parts() []part {
	door := func(topic, name, class string, get func(core.Doors) core.Value[core.Opening]) part {
		return part{topic, name, class, openTemplate, func(s core.Snapshot) core.Value[string] {
			return opening(s.Doors, get)
		}}
	}
	window := func(topic, name string, get func(core.Windows) core.Value[core.Opening]) part {
		return part{topic, name, "window", openTemplate, func(s core.Snapshot) core.Value[string] {
			return opening(s.Windows, get)
		}}
	}
	return []part{
		{"locked", "Lock", "lock", onOff(Unlocked, Locked), func(s core.Snapshot) core.Value[string] {
			return word(s.Doors, func(d core.Doors) core.Value[bool] { return d.Locked }, Locked, Unlocked)
		}},
		door("door_front_left", "Door front left", "door", func(d core.Doors) core.Value[core.Opening] { return d.FrontLeft }),
		door("door_front_right", "Door front right", "door", func(d core.Doors) core.Value[core.Opening] { return d.FrontRight }),
		door("door_rear_left", "Door rear left", "door", func(d core.Doors) core.Value[core.Opening] { return d.RearLeft }),
		door("door_rear_right", "Door rear right", "door", func(d core.Doors) core.Value[core.Opening] { return d.RearRight }),
		door("hood", "Hood", "opening", func(d core.Doors) core.Value[core.Opening] { return d.Hood }),
		door("tailgate", "Tailgate", "opening", func(d core.Doors) core.Value[core.Opening] { return d.Tailgate }),
		door("tank_lid", "Tank lid", "opening", func(d core.Doors) core.Value[core.Opening] { return d.TankLid }),
		window("window_front_left", "Window front left", func(w core.Windows) core.Value[core.Opening] { return w.FrontLeft }),
		window("window_front_right", "Window front right", func(w core.Windows) core.Value[core.Opening] { return w.FrontRight }),
		window("window_rear_left", "Window rear left", func(w core.Windows) core.Value[core.Opening] { return w.RearLeft }),
		window("window_rear_right", "Window rear right", func(w core.Windows) core.Value[core.Opening] { return w.RearRight }),
		window("sunroof", "Sunroof", func(w core.Windows) core.Value[core.Opening] { return w.Sunroof }),
		{"sentry", "Sentry Mode", "", onOff(On, Off), func(s core.Snapshot) core.Value[string] {
			return word(s.Doors, func(d core.Doors) core.Value[bool] { return d.Sentry }, On, Off)
		}},
	}
}

func opening[T any](v core.Value[T], get func(T) core.Value[core.Opening]) core.Value[string] {
	if !v.OK {
		return core.Value[string]{}
	}
	o := get(v.V)
	return core.Value[string]{V: string(o.V), OK: o.OK}
}

func word(d core.Value[core.Doors], get func(core.Doors) core.Value[bool], on, off string) core.Value[string] {
	if !d.OK {
		return core.Value[string]{}
	}
	b := get(d.V)
	if b.V {
		return core.Value[string]{V: on, OK: b.OK}
	}
	return core.Value[string]{V: off, OK: b.OK}
}

// OpeningSet is a set of the lock, the doors, the windows and Sentry Mode, by their
// topic; a bit each, so that a Model stays comparable.
type OpeningSet uint16

// Has tells whether the set holds the topic's.
func (o OpeningSet) Has(topic string) bool {
	for i, p := range parts() {
		if p.topic == topic {
			return o&(1<<i) != 0
		}
	}
	return false
}

// Openings is the set of the lock, the doors, the windows and Sentry Mode the vehicle
// tells, for Model.Openings.
func Openings(s core.Snapshot) OpeningSet {
	var told OpeningSet
	for i, p := range parts() {
		if p.value(s).OK {
			told |= 1 << i
		}
	}
	return told
}

// openingsReadAt is when the doors and windows were last read, the older of the two: a
// warning says how old what it rests on is. Empty when neither was read.
func openingsReadAt(c core.Current) string {
	var at time.Time
	for _, f := range []core.Field{core.FieldDoors, core.FieldWindows} {
		if _, checked := c.Read(f); !checked.IsZero() && (at.IsZero() || checked.Before(at)) {
			at = checked
		}
	}
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// LeftOpen tells whether the vehicle is left open at at, standing parked and open since
// since (core.OpenSince, zero: not): for LeftOpenAfter at least. Unknown when c tells
// neither its doors nor its windows.
func LeftOpen(c core.Current, since, at time.Time) core.Value[bool] {
	if c.Snapshot.Covers&(core.FieldDoors|core.FieldWindows) == 0 {
		return core.Value[bool]{}
	}
	return core.Value[bool]{V: !since.IsZero() && !since.After(at.Add(-LeftOpenAfter)), OK: true}
}

// LeftOpenState is the message of LeftOpen, after State's: on or off, empty when unknown.
func LeftOpenState(b Broker, vehicleID string, v core.Value[bool]) Message {
	return Message{Topic: b.TopicPrefix + "/vehicles/" + vehicleID + "/left_open", Payload: text(v, onOffWord), Retained: true}
}

func onOffWord(b bool) string {
	if b {
		return On
	}
	return Off
}
