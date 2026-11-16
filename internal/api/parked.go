package api

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

type parkedInput struct {
	Vehicle string    `path:"vehicle" doc:"The vehicle ID."`
	From    time.Time `query:"from" doc:"The start of the period [from, to). Without it, the period starts with the vehicle's first event." example:"2026-09-01T00:00:00+02:00"`
	To      time.Time `query:"to" doc:"The end of the period, excluded; it must come after from. Without it, now." example:"2026-10-01T00:00:00+02:00"`
}

// eventRefJSON names a trip or a charge of the lists.
type eventRefJSON struct {
	Kind string `json:"kind" enum:"trip,charge" doc:"A trip or a charge."`
	ID   string `json:"id" doc:"Its ID, as the lists give it (GET /trips/{id} or /charges/{id})."`
}

type parkedStopJSON struct {
	From        time.Time    `json:"from" doc:"The latest end of the event before: the stop's sure part starts there."`
	To          time.Time    `json:"to" doc:"The earliest start of the next event. Before from when the two events' bounds overlap."`
	TimeS       int64        `json:"time_s" minimum:"0" doc:"to − from, 0 when they overlap."`
	StartSoCPct *float64     `json:"start_soc_pct" doc:"The state of charge at the end of the event before; null when it did not tell it."`
	EndSoCPct   *float64     `json:"end_soc_pct" doc:"The state of charge at the start of the next event; null when it did not tell it."`
	SoCLossPct  *float64     `json:"soc_loss_pct" minimum:"0" doc:"The points lost, as the statistics count them: a rise below the noise threshold counts as 0; null without a state of charge at both ends, or for a rise that only a charge explains."`
	SoCLossKWh  *float64     `json:"soc_loss_kwh" minimum:"0" doc:"soc_loss_pct × the battery capacity of the event before, else after; null without one."`
	PowerW      *float64     `json:"power_w" minimum:"0" doc:"soc_loss_kwh over time_s, in W; null without both. An estimate: the state of charge has a resolution of one point, and a stop of a few hours may read one more or less."`
	SleepReadS  int64        `json:"sleep_read_s" minimum:"0" doc:"The part of time_s the vehicle's responses told whether it slept over, as in the statistics; 0 for a vehicle that does not tell it."`
	AsleepS     int64        `json:"asleep_s" minimum:"0" doc:"The part of sleep_read_s they told it asleep or out of reach."`
	AsleepShare *float64     `json:"asleep_share" minimum:"0" maximum:"1" doc:"asleep_s over sleep_read_s; null without sleep_read_s."`
	OutsideC    *float64     `json:"outside_temp_c" doc:"The mean of the outside temperatures of the observed events around it, as parked_by_temperature takes it; null without one."`
	Rated       bool         `json:"rated" doc:"The stop lasts at least min_stop_s: its loss counts in the statistics' soc_loss_pct_per_day and power_w."`
	Before      eventRefJSON `json:"before" doc:"The event before the stop."`
	After       eventRefJSON `json:"after" doc:"The event after it."`
}

type parkedJSON struct {
	From     time.Time        `json:"from" doc:"The start of the period: from, else the vehicle's first event; the history the account does not see is left out."`
	To       time.Time        `json:"to" doc:"The end of the period, excluded."`
	MinStopS int64            `json:"min_stop_s" minimum:"0" doc:"The time from which a stop counts in the rate of the loss while parked (rated)."`
	Items    []parkedStopJSON `json:"items" nullable:"false" doc:"The stops whose next event starts in the period, as the statistics count them (parked.intervals), newest first; but a stop after an event before the history the account sees, which is not shown."`
}

// getParked lists the parked intervals of a period, those the statistics sum.
func (s *Server) getParked(ctx context.Context, in *parkedInput) (*body[parkedJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	from, to := in.From.UTC(), in.To.UTC()
	if in.To.IsZero() {
		to = s.Clock.Now().UTC()
	}
	if !in.From.IsZero() && !from.Before(to) {
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "from must be before to")
	}
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	fromSet := !in.From.IsZero()
	if h := l.HistoryFrom; !h.IsZero() && (!fromSet || from.Before(h)) {
		from, fromSet = h, true
	}
	out := parkedJSON{From: from, To: to, MinStopS: int64(s.Params.MinParkedStop / time.Second), Items: []parkedStopJSON{}}
	if fromSet && !from.Before(to) {
		return respond(out), nil
	}
	p, err := s.Reader.PeriodEvents(ctx, sessionFrom(ctx).AccountID, v.ID, from, to)
	if err != nil {
		return nil, s.internal("events not read", err)
	}
	if !fromSet {
		out.From = firstStart(p.Trips, p.Charges, to)
	}
	sleep, err := s.sleepOf(ctx, v.ID, p, to)
	if err != nil {
		return nil, err
	}
	ref := func(r core.EventRef) eventRefJSON {
		if r.Charge {
			c := p.Charges[r.Index]
			return eventRefJSON{Kind: "charge", ID: eventOf(c.DetectedAt, c.Reconstructed, c.Start, c.End, c.Sources, c.Import).ID}
		}
		t := p.Trips[r.Index]
		return eventRefJSON{Kind: "trip", ID: eventOf(t.DetectedAt, t.Reconstructed, t.Start, t.End, t.Sources, t.Import).ID}
	}
	end := func(r core.EventRef) core.Bounds {
		if r.Charge {
			return p.Charges[r.Index].End
		}
		return p.Trips[r.Index].End
	}
	for _, st := range core.ParkedStops(p.Trips, p.Charges, sleep, out.From, to, s.Params) {
		// The event before the period opens its first stop, but one before the history
		// the account sees is not shown: neither its ID nor its end. The statistics count
		// that stop's loss, never the event.
		if l.hides(end(st.Before)) {
			continue
		}
		share := core.Value[float64]{}
		if st.SleepRead > 0 {
			share = core.Value[float64]{V: st.Asleep.Seconds() / st.SleepRead.Seconds(), OK: true}
		}
		out.Items = append(out.Items, parkedStopJSON{
			From: st.From, To: st.To, TimeS: int64(st.Time / time.Second),
			StartSoCPct: opt(st.StartSoC, same), EndSoCPct: opt(st.EndSoC, same),
			SoCLossPct: opt(st.LossPct, same), SoCLossKWh: opt(st.LossKWh, same), PowerW: opt(st.PowerW, same),
			SleepReadS: int64(st.SleepRead / time.Second), AsleepS: int64(st.Asleep / time.Second),
			AsleepShare: opt(share, same), OutsideC: opt(st.TempC, same), Rated: st.Rated,
			Before: ref(st.Before), After: ref(st.After),
		})
	}
	slices.Reverse(out.Items)
	return respond(out), nil
}

// registerParked registers the parked intervals of a vehicle.
func (s *Server) registerParked(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getVehicleParked", Method: http.MethodGet, Path: "/vehicles/{vehicle}/parked", Tags: []string{"stats"},
		Summary: "The stops of a vehicle over a period",
		Description: "The parked intervals between two consecutive events (trips and charges) whose second event starts in the " +
			"period, newest first: the detail of the statistics' `parked`, with the same rules, so that their sums agree. " +
			"Each tells its time, its state of charge at both ends and the loss, the power it stands for, the share of it " +
			"asleep, its outside temperature, and the events around it. The loss is an estimate from the state of charge.",
	}, "The stops.", map[int]string{
		http.StatusBadRequest:   "`invalid_parameter`: `from` or `to`; the message names the parameter.",
		http.StatusUnauthorized: errUnauthorized,
		http.StatusNotFound:     errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getParked)
}
