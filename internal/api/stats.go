package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// statsInput selects a period and its split.
type statsInput struct {
	Vehicle string    `path:"vehicle" doc:"The vehicle ID."`
	From    time.Time `query:"from" doc:"The start of the period [from, to). Without it, the period starts with the vehicle's first event." example:"2026-09-01T00:00:00+02:00"`
	To      time.Time `query:"to" doc:"The end of the period, excluded; it must come after from. Without it, now." example:"2026-10-01T00:00:00+02:00"`
	TZ      string    `query:"tz" default:"UTC" doc:"The IANA time zone in which days, weeks and months are split, changes of daylight saving time included." example:"Europe/Paris"`
	Bucket  string    `query:"bucket" enum:"day,week,month" doc:"Splits the period into intervals: days, ISO weeks (from Monday) or months. Without it, the totals only."`
}

// bucketName is the split of a period.
type bucketName string

// Schema lists the splits.
func (bucketName) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.BucketDay, core.BucketWeek, core.BucketMonth)
}

// spanJSON is a sum of durations known by bounds, in seconds: the minimum rounded down,
// the maximum up, so that the true total always lies within.
type spanJSON struct {
	Min int64 `json:"min" minimum:"0"`
	Max int64 `json:"max" minimum:"0"`
}

func spanOf(s core.Span) spanJSON {
	return spanJSON{Min: int64(s.Min / time.Second), Max: int64((s.Max + time.Second - 1) / time.Second)}
}

type tripStatsJSON struct {
	Count              int      `json:"count" minimum:"0" doc:"The trips that started in the period or interval, reconstructed ones included."`
	Reconstructed      int      `json:"reconstructed" minimum:"0"`
	DistanceKm         float64  `json:"distance_km" minimum:"0" doc:"The sum of the known distances; 0 without trips."`
	DistanceUnknown    int      `json:"distance_unknown" minimum:"0" doc:"The trips whose distance is unknown, left out of distance_km."`
	DrivingTimeS       spanJSON `json:"driving_time_s" doc:"The sum of the durations of the observed trips: every total their bounds allow lies within [min, max]."`
	DrivingTimeUnknown int      `json:"driving_time_unknown" minimum:"0" doc:"The trips without a duration: the reconstructed ones."`
	EnergyKWh          float64  `json:"energy_kwh" doc:"The sum of the known energy estimates (ΔSoC × battery capacity)."`
	EnergyUnknown      int      `json:"energy_unknown" minimum:"0"`
	// An average: null over nothing, never 0.
	ConsumptionKWhPer100km *float64 `json:"consumption_kwh_per_100km" doc:"An estimate: the energy over the distance of the trips where both are known and the distance positive, reconstructed ones included. null without such a trip."`
}

// costSumJSON is a sum of costs known by bounds, in minor units of the account's
// currency: every total their bounds allow lies within.
type costSumJSON struct {
	MinMinor int64 `json:"min_minor" minimum:"0"`
	MaxMinor int64 `json:"max_minor" minimum:"0"`
}

type orphanedCostsJSON struct {
	Count       int   `json:"count" minimum:"0"`
	AmountMinor int64 `json:"amount_minor" minimum:"0" doc:"In minor units of the account's currency."`
}

type chargeTypeStatsJSON struct {
	Count            int         `json:"count" minimum:"0"`
	EnergySoCKWh     float64     `json:"energy_soc_kwh"`
	EnergySoCUnknown int         `json:"energy_soc_unknown" minimum:"0"`
	Cost             costSumJSON `json:"cost" doc:"The sum of the known costs of the charges of this type, as charges.cost."`
	CostUnknown      int         `json:"cost_unknown" minimum:"0"`
	CostEntered      int         `json:"cost_entered" minimum:"0"`
}

type chargesByTypeJSON struct {
	AC      chargeTypeStatsJSON `json:"ac"`
	DC      chargeTypeStatsJSON `json:"dc"`
	Unknown chargeTypeStatsJSON `json:"unknown" doc:"The charges whose type is unknown, as reconstructed ones."`
}

type chargeStatsJSON struct {
	Count               int               `json:"count" minimum:"0" doc:"The charges that started in the period or interval, reconstructed ones included."`
	Reconstructed       int               `json:"reconstructed" minimum:"0"`
	EnergySoCKWh        float64           `json:"energy_soc_kwh" doc:"The sum of the known ΔSoC × battery capacity estimates, reconstructed charges included."`
	EnergySoCUnknown    int               `json:"energy_soc_unknown" minimum:"0"`
	EnergyPowerKWh      float64           `json:"energy_power_kwh" doc:"The sum of the known integrated power estimates, kept apart from energy_soc_kwh: observed charges only."`
	EnergyPowerUnknown  int               `json:"energy_power_unknown" minimum:"0"`
	ChargingTimeS       spanJSON          `json:"charging_time_s" doc:"The sum of the durations of the observed charges, as driving_time_s."`
	ChargingTimeUnknown int               `json:"charging_time_unknown" minimum:"0" doc:"The charges without a duration: the reconstructed ones."`
	Cost                costSumJSON       `json:"cost" doc:"The sum of the known costs of the charges, entered ones included: every total their bounds allow lies within [min_minor, max_minor]. {0, 0} over no charge, and without the account's currency."`
	CostUnknown         int               `json:"cost_unknown" minimum:"0" doc:"The charges whose cost is unknown, left out of cost: all of them without the account's currency."`
	CostEntered         int               `json:"cost_entered" minimum:"0" doc:"The charges whose cost was entered, counted in cost."`
	OrphanedCosts       orphanedCostsJSON `json:"orphaned_costs" doc:"The entered costs that no charge takes (GET /charge-costs/orphans) whose window starts in the period or interval, apart: they are in no cost, which would count twice the charge they were entered for, most often detected again with its tariff's cost."`
	ByType              chargesByTypeJSON `json:"by_type" doc:"The same figures by type of charge, but the orphaned costs: they have no type."`
}

type parkedStatsJSON struct {
	Intervals      int     `json:"intervals" minimum:"0" doc:"The intervals between two consecutive events (trips and charges), counted where the second one starts."`
	TimeS          int64   `json:"time_s" minimum:"0" doc:"Their sure duration, from the latest end of an event to the earliest start of the next."`
	SoCLossPct     float64 `json:"soc_loss_pct" minimum:"0" doc:"The state of charge lost while parked, in points. A rise below the noise threshold counts as 0."`
	SoCLossUnknown int     `json:"soc_loss_unknown" minimum:"0" doc:"The intervals whose loss is unknown: no state of charge at one end, or a rise that only a charge explains."`
	// An average: null over nothing.
	SoCLossPctPerDay *float64 `json:"soc_loss_pct_per_day" doc:"soc_loss_pct over the time of the intervals whose loss is known, per day. The state of charge has a resolution of one point: meaningful over weeks, not over a night. null without such time."`
}

// periodStatsJSON are the aggregates of a period.
type periodStatsJSON struct {
	Trips   tripStatsJSON   `json:"trips"`
	Charges chargeStatsJSON `json:"charges"`
	Parked  parkedStatsJSON `json:"parked"`
}

// bucketStatsJSON repeats the fields of periodStatsJSON: huma ignores the fields of an
// unexported embedded struct.
type bucketStatsJSON struct {
	Start   time.Time       `json:"start" pattern:"Z$" doc:"The start of the interval, in the requested time zone, written in UTC."`
	End     time.Time       `json:"end" pattern:"Z$" doc:"The end of the interval, excluded."`
	Trips   tripStatsJSON   `json:"trips"`
	Charges chargeStatsJSON `json:"charges"`
	Parked  parkedStatsJSON `json:"parked"`
}

type distanceBandJSON struct {
	MinKm float64  `json:"min_km" minimum:"0" doc:"The band holds the trips of at least min_km."`
	MaxKm *float64 `json:"max_km" doc:"And of less than max_km; null for the last band, which has no upper limit."`
	Count int      `json:"count" minimum:"0"`
	// An average: null over nothing.
	ConsumptionKWhPer100km *float64 `json:"consumption_kwh_per_100km" doc:"An estimate over the band's trips whose energy is known and distance positive, as trips.consumption_kwh_per_100km. null without such a trip."`
}

type tripsByDistanceJSON struct {
	Bands   []distanceBandJSON `json:"bands" nullable:"false" doc:"The bands, shortest first: less than 5 km, 5 to 20, 20 to 50, 50 to 100, and 100 km or more."`
	LeftOut int                `json:"left_out" minimum:"0" doc:"The trips in no band: the reconstructed ones, which may hold several, and those whose distance is unknown."`
}

type chargesBySoCJSON struct {
	Start   []int `json:"start" nullable:"false" minItems:"101" maxItems:"101" doc:"The charges by the state of charge they started at: start[20] is those that started at 20 %, rounded to the nearest point."`
	End     []int `json:"end" nullable:"false" minItems:"101" maxItems:"101" doc:"The charges by the state of charge they ended at, as start."`
	LeftOut int   `json:"left_out" minimum:"0" doc:"The charges in neither: the reconstructed ones, whose states of charge are readings before and after it, and those whose state of charge is unknown at one end."`
}

// PlaceStatsFields sum the charges of a place, or of a group outside every place.
// Exported, as EventFields, for placeChargesJSON to embed.
type PlaceStatsFields struct {
	Count            int         `json:"count" minimum:"0"`
	Reconstructed    int         `json:"reconstructed" minimum:"0" doc:"The reconstructed charges among them, counted with their energy."`
	EnergySoCKWh     float64     `json:"energy_soc_kwh" doc:"The sum of the known ΔSoC × battery capacity estimates, as charges.energy_soc_kwh."`
	EnergySoCUnknown int         `json:"energy_soc_unknown" minimum:"0"`
	Cost             costSumJSON `json:"cost" doc:"The sum of the known costs of these charges, as charges.cost. Outside every place, only entered costs are known."`
	CostUnknown      int         `json:"cost_unknown" minimum:"0"`
	CostEntered      int         `json:"cost_entered" minimum:"0"`
}

type placeChargesJSON struct {
	Place placeRefJSON `json:"place"`
	PlaceStatsFields
}

type outsideChargesJSON struct {
	AC      PlaceStatsFields `json:"ac"`
	DC      PlaceStatsFields `json:"dc"`
	Unknown PlaceStatsFields `json:"unknown" doc:"Those whose type is unknown, as reconstructed ones."`
}

type chargesByPlaceJSON struct {
	Places     []placeChargesJSON `json:"places" nullable:"false" doc:"The account's places that hold a charge of the period, in the order they were created."`
	Outside    outsideChargesJSON `json:"outside" doc:"The charges with a position outside every place, by type."`
	NoPosition PlaceStatsFields   `json:"no_position" doc:"The charges without a position that no place takes: the DC ones, or all of them without a place marked without_position."`
}

func tripsByDistanceOf(d core.TripsByDistance) tripsByDistanceJSON {
	out := tripsByDistanceJSON{Bands: make([]distanceBandJSON, len(d.Bands)), LeftOut: d.LeftOut}
	for i, b := range d.Bands {
		out.Bands[i] = distanceBandJSON{
			MinKm: b.MinKm, MaxKm: opt(b.MaxKm, same), Count: b.Count,
			ConsumptionKWhPer100km: opt(b.ConsumptionKWhPer100km, same),
		}
	}
	return out
}

func chargesBySoCOf(c core.ChargesBySoC) chargesBySoCJSON {
	return chargesBySoCJSON{Start: c.Start[:], End: c.End[:], LeftOut: c.LeftOut}
}

func placeStatsOf(s core.PlaceStats) PlaceStatsFields {
	return PlaceStatsFields{
		Count: s.Count, Reconstructed: s.Reconstructed, EnergySoCKWh: s.EnergySoCKWh, EnergySoCUnknown: s.EnergySoCUnknown,
		Cost: costSumOf(s.Cost), CostUnknown: s.Cost.Unknown, CostEntered: s.Cost.Entered,
	}
}

func chargesByPlaceOf(c core.ChargesByPlace) chargesByPlaceJSON {
	out := chargesByPlaceJSON{
		Places:     make([]placeChargesJSON, len(c.Places)),
		Outside:    outsideChargesJSON{AC: placeStatsOf(c.AC), DC: placeStatsOf(c.DC), Unknown: placeStatsOf(c.UnknownType)},
		NoPosition: placeStatsOf(c.NoPosition),
	}
	for i, p := range c.Places {
		out.Places[i] = placeChargesJSON{Place: placeRefOf(p.Place), PlaceStatsFields: placeStatsOf(p.PlaceStats)}
	}
	return out
}

type statsJSON struct {
	From     time.Time          `json:"from" pattern:"Z$" doc:"The start of the period: the requested one, or the start of the first event. Equal to to when the vehicle has no event."`
	To       time.Time          `json:"to" pattern:"Z$"`
	TimeZone string             `json:"time_zone"`
	Bucket   null[bucketName]   `json:"bucket" doc:"null without a split."`
	Currency null[currencyJSON] `json:"currency" doc:"The account's currency, the unit of the _minor fields; null until it is set, and then no cost is known."`
	Totals   periodStatsJSON    `json:"totals"`
	Buckets  []bucketStatsJSON  `json:"buckets" nullable:"false" doc:"The intervals, oldest first, empty ones included; [] without a split. The first starts at the start of the day, week or month of from, the last ends at the end of the one of the last instant before to."`
	// Over the period only: by interval, they would repeat the bands hundreds of times.
	TripsByDistance tripsByDistanceJSON `json:"trips_by_distance" doc:"How the observed trips of the period spread by distance, with or without a split."`
	ChargesBySoC    chargesBySoCJSON    `json:"charges_by_soc" doc:"How the observed charges of the period spread by the state of charge they started and ended at."`
	ChargesByPlace  chargesByPlaceJSON  `json:"charges_by_place" doc:"The charges of the period, reconstructed ones included, by the place their cost takes (charges.place), then outside every place by type, then without a position: each charge in one group."`
}

func tripStatsOf(s core.TripStats) tripStatsJSON {
	return tripStatsJSON{
		Count: s.Count, Reconstructed: s.Reconstructed,
		DistanceKm: s.DistanceKm, DistanceUnknown: s.DistanceUnknown,
		DrivingTimeS: spanOf(s.DrivingTime), DrivingTimeUnknown: s.DrivingTimeUnknown,
		EnergyKWh: s.EnergyKWh, EnergyUnknown: s.EnergyUnknown,
		ConsumptionKWhPer100km: opt(s.ConsumptionKWhPer100km, same),
	}
}

func costSumOf(s core.CostSum) costSumJSON { return costSumJSON{MinMinor: s.Min, MaxMinor: s.Max} }

func chargeTypeStatsOf(s core.ChargeTypeStats) chargeTypeStatsJSON {
	return chargeTypeStatsJSON{
		Count: s.Count, EnergySoCKWh: s.EnergySoCKWh, EnergySoCUnknown: s.EnergySoCUnknown,
		Cost: costSumOf(s.Cost), CostUnknown: s.Cost.Unknown, CostEntered: s.Cost.Entered,
	}
}

func chargeStatsOf(s core.ChargeStats) chargeStatsJSON {
	return chargeStatsJSON{
		Count: s.Count, Reconstructed: s.Reconstructed,
		EnergySoCKWh: s.EnergySoCKWh, EnergySoCUnknown: s.EnergySoCUnknown,
		EnergyPowerKWh: s.EnergyPowerKWh, EnergyPowerUnknown: s.EnergyPowerUnknown,
		ChargingTimeS: spanOf(s.ChargingTime), ChargingTimeUnknown: s.ChargingTimeUnknown,
		Cost: costSumOf(s.Cost), CostUnknown: s.Cost.Unknown, CostEntered: s.Cost.Entered,
		OrphanedCosts: orphanedCostsJSON{Count: s.Orphaned.Count, AmountMinor: s.Orphaned.AmountMinor},
		ByType: chargesByTypeJSON{
			AC: chargeTypeStatsOf(s.AC), DC: chargeTypeStatsOf(s.DC), Unknown: chargeTypeStatsOf(s.UnknownType),
		},
	}
}

func parkedStatsOf(s core.ParkedStats) parkedStatsJSON {
	return parkedStatsJSON{
		Intervals: s.Intervals, TimeS: int64(s.Time / time.Second),
		SoCLossPct: s.SoCLossPct, SoCLossUnknown: s.SoCLossUnknown,
		SoCLossPctPerDay: opt(s.SoCLossPctPerDay, same),
	}
}

func periodStatsOf(s core.Stats) periodStatsJSON {
	return periodStatsJSON{Trips: tripStatsOf(s.Trips), Charges: chargeStatsOf(s.Charges), Parked: parkedStatsOf(s.Parked)}
}

// loadZone loads an IANA time zone. Local is refused: the server's own zone means
// nothing to the client. So is the empty name, which time.LoadLocation reads as UTC.
func loadZone(name string) (*time.Location, bool) {
	if name == "Local" || name == "" {
		return nil, false
	}
	loc, err := time.LoadLocation(name)
	return loc, err == nil
}

// location loads the time zone of a query.
func location(name string) (*time.Location, error) {
	loc, ok := loadZone(name)
	if !ok {
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "tz: unknown time zone")
	}
	return loc, nil
}

// getStats sums the events of a period: an event counts once, in the interval that
// holds its start.after.
func (s *Server) getStats(ctx context.Context, in *statsInput) (*body[statsJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	loc, err := location(in.TZ)
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
	// The history the account does not see is not counted either: the period starts
	// with it at the earliest.
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	if l.NoStats && in.Bucket != "" {
		return nil, apiError(http.StatusForbidden, codeFeatureUnavailable, "the account's offer does not include the statistics by interval")
	}
	fromSet := !in.From.IsZero()
	if h := l.HistoryFrom; !h.IsZero() && (!fromSet || from.Before(h)) {
		from, fromSet = h, true
	}
	var p Period
	if !fromSet || from.Before(to) {
		if p, err = s.Reader.PeriodEvents(ctx, sessionFrom(ctx).AccountID, v.ID, from, to); err != nil {
			return nil, s.internal("events not read", err)
		}
	}
	if !fromSet {
		from = firstStart(p.Trips, p.Charges, to)
	}
	if l.NoCosts { // no currency: no cost, as before the account chose one
		p.Currency, p.Orphans = core.Value[core.Currency]{}, nil
	}
	out := statsJSON{
		From: from, To: to, TimeZone: loc.String(), Currency: nullOf(p.Currency, currencyOf), Buckets: []bucketStatsJSON{},
	}
	if in.Bucket != "" {
		out.Bucket = known(bucketName(in.Bucket))
	}
	if !from.Before(to) { // no event, and no from
		out.Totals = periodStatsOf(core.Stats{})
		out.TripsByDistance = tripsByDistanceOf(core.NoTripsByDistance())
		out.ChargesBySoC = chargesBySoCOf(core.ChargesBySoC{})
		out.ChargesByPlace = chargesByPlaceOf(core.NoChargesByPlace())
		return respond(out), nil
	}
	charger, err := s.chargerOf(ctx, v)
	if err != nil {
		return nil, s.internal("vehicle model not read", err)
	}
	costs := core.Costs{Charges: make([]core.Value[core.Cost], len(p.Charges)), Orphans: p.Orphans}
	if !l.NoCosts {
		costs.Charges = s.costs(p.PricedCharges, charger)
	}
	sum, err := core.Summarize(p.Trips, p.Charges, costs, p.Places, from, to, loc, core.Bucket(in.Bucket), s.Params)
	switch {
	case errors.Is(err, core.ErrTooManyBuckets):
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "bucket: the period would have more than 400 intervals")
	case err != nil:
		return nil, s.internal("stats not computed", err)
	}
	out.Totals = periodStatsOf(sum.Totals)
	out.TripsByDistance, out.ChargesBySoC = tripsByDistanceOf(sum.TripsByDistance), chargesBySoCOf(sum.ChargesBySoC)
	out.ChargesByPlace = chargesByPlaceOf(sum.ChargesByPlace)
	for _, b := range sum.Buckets {
		out.Buckets = append(out.Buckets, bucketStatsJSON{
			Start: b.Start, End: b.End,
			Trips: tripStatsOf(b.Trips), Charges: chargeStatsOf(b.Charges), Parked: parkedStatsOf(b.Parked),
		})
	}
	return respond(out), nil
}

// firstStart is the earliest start of the events, or to without any.
func firstStart(trips []core.Trip, charges []core.Charge, to time.Time) time.Time {
	first := to
	for _, t := range trips {
		first = minTime(first, t.Start.After)
	}
	for _, c := range charges {
		first = minTime(first, c.Start.After)
	}
	return first
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// registerStats registers the statistics of a vehicle.
func (s *Server) registerStats(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getVehicleStats", Method: http.MethodGet, Path: "/vehicles/{vehicle}/stats", Tags: []string{"stats"},
		Summary: "The statistics of a vehicle over a period",
		Description: "Totals of the trips, charges and parked intervals of the period, and, with `bucket`, the same " +
			"figures by day, week or month. An event counts once, in the interval that holds its `start.after`: " +
			"unlike the lists, which keep the events that may overlap the period, an event across a limit counts " +
			"only where it started. A sum covers the known values, and its `_unknown` companion counts the events " +
			"that lacked one; a sum over no event is 0, an average `null`. Energies, consumption, costs and the " +
			"loss of the state of charge are estimates. At most 400 intervals.",
	}, "The statistics.", map[int]string{
		http.StatusBadRequest: "`invalid_parameter`: `from`, `to`, `tz` or `bucket`, or more than 400 intervals; " +
			"the message names the parameter.",
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: "`feature_unavailable`: `bucket`, while the " +
			"account's offer leaves out the statistics (`limits.unavailable` of the session).",
		http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getStats)
}
