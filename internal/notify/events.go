package notify

import (
	"strconv"
	"time"

	"runsten/internal/core"
	"runsten/internal/publish"
)

// Connection is what the credential's kinds read of a connection to a maker.
type Connection struct {
	ID       string
	Provider string
	// ExpiresAt is when its credential expires at the latest; zero: it has none that
	// expires.
	ExpiresAt time.Time
	// RefusedAt is when the maker refused its credential; zero: not refused.
	RefusedAt time.Time
	// ReauthAt is when its grant was lost (a refresh refused): it must be authorized
	// again; zero: not lost.
	ReauthAt time.Time
}

// CredentialEvents are the account's connections' credentials that need the user: one
// refused or whose grant was lost, else one that expires within publish.RenewAhead, or
// expired. Each is keyed by its connection and the instant, so that a credential given
// again, which expires later, is told of again.
func CredentialEvents(accountID string, conns []Connection, now time.Time) []Event {
	var out []Event
	for _, c := range conns {
		args := map[string]string{"provider": c.Provider}
		switch lost := later(c.RefusedAt, c.ReauthAt); {
		case !lost.IsZero():
			out = append(out, Event{
				Kind: CredentialRefused, AccountID: accountID, At: lost, Key: c.ID + "@" + stamp(lost), Args: args,
			})
		case !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt.Add(-publish.RenewAhead)):
			args["expires_at"] = c.ExpiresAt.UTC().Format(time.RFC3339)
			out = append(out, Event{
				Kind: CredentialExpiring, AccountID: accountID, At: now, Key: c.ID + "@" + stamp(c.ExpiresAt), Args: args,
			})
		}
	}
	return out
}

// SoCMargin is how far below its limit a charge may end and still have reached it: a
// state of charge is read in whole points, and a vehicle stops at its limit or one
// under.
const SoCMargin = 1

// VehicleEvents are the events of a derivation of the vehicle: a charge started, a
// charge finished or interrupted, a trip ended. Only those observed (never a
// reconstructed or imported event, which no one saw happen) whose end, or start for a
// charge in progress, is within Fresh of now. plugged is the cable as last read: a charge
// that stopped below its limit is interrupted only while it is known connected.
func VehicleEvents(accountID, vehicleID string, res core.Result, plugged core.Value[core.Connection], now time.Time) []Event {
	fresh := func(t time.Time) bool { return !t.Before(now.Add(-Fresh)) }
	event := func(k Kind, detected, at time.Time, args map[string]string) Event {
		return Event{Kind: k, AccountID: accountID, VehicleID: vehicleID, At: at, Key: EventID(detected), Args: args}
	}
	var out []Event
	if res.InProgress.OK {
		c := res.InProgress.V.Charge
		if !c.Reconstructed && fresh(c.Start.Before) {
			out = append(out, event(ChargeStarted, c.DetectedAt, c.Start.Before, chargeArgs(c)))
		}
	}
	for _, c := range res.Charges {
		if c.Reconstructed || c.Import.OK || !fresh(c.End.Before) || !c.EndSoC.OK || !c.TargetSoC.OK {
			continue
		}
		switch {
		case c.EndSoC.V >= c.TargetSoC.V-SoCMargin:
			out = append(out, event(ChargeFinished, c.DetectedAt, c.End.Before, chargeArgs(c)))
		case plugged.OK && plugged.V == core.Connected:
			out = append(out, event(ChargeInterrupted, c.DetectedAt, c.End.Before, chargeArgs(c)))
		}
	}
	for _, t := range FreshTrips(res, now) {
		out = append(out, event(TripEnded, t.DetectedAt, t.End.Before, TripArgs(t)))
	}
	return out
}

// FreshTrips are the trips of a derivation that VehicleEvents tells of.
func FreshTrips(res core.Result, now time.Time) []core.Trip {
	var out []core.Trip
	for _, t := range res.Trips {
		if !t.Reconstructed && !t.Import.OK && !t.End.Before.Before(now.Add(-Fresh)) {
			out = append(out, t)
		}
	}
	return out
}

// TripToClassifyEvent is the event of a trip that ended without a purpose.
func TripToClassifyEvent(accountID, vehicleID string, t core.Trip) Event {
	return Event{
		Kind: TripToClassify, AccountID: accountID, VehicleID: vehicleID, At: t.End.Before,
		Key: EventID(t.DetectedAt), Args: TripArgs(t),
	}
}

// EventID is the ID of a derived event, as the API gives it: its detection time to the
// microsecond, in UTC.
func EventID(detected time.Time) string {
	return detected.UTC().Format("2006-01-02T15:04:05.000000Z")
}

// chargeArgs are a charge's figures: its ID, its states of charge and its limit, those
// known.
func chargeArgs(c core.Charge) map[string]string {
	args := map[string]string{"charge": EventID(c.DetectedAt)}
	figure(args, "start_soc", c.StartSoC, 0)
	figure(args, "end_soc", c.EndSoC, 0)
	figure(args, "target_soc", c.TargetSoC, 0)
	return args
}

// TripArgs are a trip's figures: its ID, its distance and energy, those known.
func TripArgs(t core.Trip) map[string]string {
	args := map[string]string{"trip": EventID(t.DetectedAt)}
	figure(args, "distance_km", t.DistanceKm, 1)
	figure(args, "energy_kwh", t.EnergyKWh, 1)
	return args
}

// figure sets args[name] to v with digits decimals, when v is known.
func figure(args map[string]string, name string, v core.Value[float64], digits int) {
	if v.OK {
		args[name] = strconv.FormatFloat(v.V, 'f', digits, 64)
	}
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
