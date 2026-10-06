package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// Readings reads the energy states a vehicle gave over a window.
type Readings interface {
	// Readings returns the records of the vehicle's energy state read within [from, to],
	// oldest first, at most limit of them.
	Readings(ctx context.Context, accountID, vehicleID string, from, to time.Time, limit int) ([]core.Record, error)
}

const (
	// maxSeriesWindow bounds the window of a series: the last seven days, or a charge.
	maxSeriesWindow = 7 * 24 * time.Hour
	// maxSeriesResponses bounds the responses of a window: a week read every minute is
	// 10,080, so only a collector reading faster than it ever does reaches it.
	maxSeriesResponses = 20_000
	// seriesGap is the longest time between two responses that a run joins: longer than
	// the slowest regular read, one an hour (assumption: an account read every hour, no
	// slower). Beyond it, nothing was read in between, and the line breaks.
	seriesGap = 90 * time.Minute
)

type seriesInput struct {
	Vehicle string    `path:"vehicle" doc:"The vehicle ID."`
	From    time.Time `query:"from" required:"true" doc:"The start of the window [from, to], included." example:"2026-09-28T18:20:00Z"`
	To      time.Time `query:"to" doc:"The end of the window, included; it must come after from, at most 7 days later. Without it, now." example:"2026-09-28T21:56:00Z"`
}

// seriesReadingJSON is the energy state as one response gave it, at one instant.
type seriesReadingJSON struct {
	At     time.Time `json:"at" pattern:"Z$" doc:"When it was read: the response's fetched_at, or the latest time it was read again unchanged; a response read before from or again after to gives its value at that edge, which it held all along."`
	SoCPct *float64  `json:"soc_pct" doc:"State of charge, in %; null when the response lacked it."`
	PowerW *float64  `json:"power_w" doc:"Charging power, in W; null when the response lacked it."`
	// The vehicle's own forecast, as in the state.
	RangeKm *float64 `json:"range_km" doc:"Remaining electric range; null when the response lacked it."`
}

// seriesRunJSON is readings with nothing missed between them.
type seriesRunJSON struct {
	Readings []seriesReadingJSON `json:"readings" nullable:"false" minItems:"1" doc:"Oldest first, at most 90 minutes between two responses: a line joins them."`
}

type seriesJSON struct {
	From time.Time `json:"from" pattern:"Z$" doc:"The start of the window: the one asked for, or the start of the history the account sees when later."`
	To   time.Time `json:"to" pattern:"Z$"`
	// Runs rather than nulls between the readings: a gap has no time of its own.
	Runs []seriesRunJSON `json:"runs" nullable:"false" doc:"The readings, oldest first, in runs: a new run starts where more than 90 minutes separate two responses, nothing being read in between. A line joins the readings of a run and breaks between two runs. [] when nothing was read in the window."`
}

func seriesReadingOf(r core.Reading) seriesReadingJSON {
	return seriesReadingJSON{At: r.At, SoCPct: opt(r.SoC, same), PowerW: opt(r.PowerW, same), RangeKm: opt(r.RangeKm, same)}
}

// getSeries reads the energy states of a window: the readings themselves, never a value
// between two of them.
func (s *Server) getSeries(ctx context.Context, in *seriesInput) (*body[seriesJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	from, to := in.From.UTC(), in.To.UTC()
	if in.To.IsZero() {
		to = s.Clock.Now().UTC()
	}
	switch {
	case !from.Before(to):
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "from must be before to")
	case to.Sub(from) > maxSeriesWindow:
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "to: at most 7 days after from")
	}
	// The history the account does not see is not read either.
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	from = later(from, l.HistoryFrom)
	out := seriesJSON{From: from, To: to, Runs: []seriesRunJSON{}}
	if !from.Before(to) {
		out.From = to
		return respond(out), nil
	}
	records, err := s.Readings.Readings(ctx, sessionFrom(ctx).AccountID, v.ID, from, to, maxSeriesResponses+1)
	if err != nil {
		return nil, s.internal("readings not read", err)
	}
	if len(records) > maxSeriesResponses {
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter,
			"from: more than "+strconv.Itoa(maxSeriesResponses)+" responses in the window, ask a shorter one")
	}
	for _, run := range core.Runs(records, from, to, seriesGap) {
		readings := make([]seriesReadingJSON, len(run))
		for i, r := range run {
			readings[i] = seriesReadingOf(r)
		}
		out.Runs = append(out.Runs, seriesRunJSON{Readings: readings})
	}
	return respond(out), nil
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// registerSeries registers the readings of a vehicle over a window.
func (s *Server) registerSeries(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getVehicleSeries", Method: http.MethodGet, Path: "/vehicles/{vehicle}/series", Tags: []string{"vehicles"},
		Summary: "The energy states of a vehicle over a window",
		Description: "The state of charge, charging power and range as the vehicle gave them within `[from, to]`, " +
			"both included: a charge's bounds are reading times. A stored response is two readings, when it was " +
			"fetched and the latest time it was read again unchanged; nothing is known between two responses, and " +
			"nothing is interpolated. The readings come in runs, a new one where more than 90 minutes separate two " +
			"responses. How close they are follows how often the vehicle was read: every minute while it drives or " +
			"charges, less often parked, or every hour for an account read so. At most 7 days, and 20,000 " +
			"responses; the history the account does not see is left out, as from the lists.",
	}, "The readings.", map[int]string{
		http.StatusBadRequest: "`invalid_parameter`: `from` or `to`, a window over 7 days, or more than 20,000 " +
			"responses in it; the message names the parameter.",
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getSeries)
}
