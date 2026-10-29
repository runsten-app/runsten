package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// batteryInput reads the whole history of a vehicle in a time zone.
type batteryInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	TZ      string `query:"tz" default:"UTC" doc:"The IANA time zone in which the months are split, changes of daylight saving time included." example:"Europe/Paris"`
}

// estimateSourceJSON says what a capacity estimate was measured on.
type estimateSourceJSON string

// Schema lists the sources of an estimate.
func (estimateSourceJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.EstimatePower, core.EstimateBilled, core.EstimateRated)
}

// ratedEfficiencyJSON is the energy a vehicle counts per km of its rated range.
type ratedEfficiencyJSON struct {
	KWhPerKm float64 `json:"kwh_per_km" exclusiveMinimum:"0" doc:"The median, over the charges it is measured on, of the energy the vehicle measured into its battery over the rated range the charge added, in kWh per km."`
	Charges  int     `json:"charges" minimum:"1" doc:"How many charges it is measured on: observed, of 10 minutes or more, ended at 95 % at most, with a measured energy and a rated range that rose."`
}

// capacityReferenceJSON is the capacity the estimates are compared with, bounded by
// and divided into cycles: the same one the energies of the events rest on.
type capacityReferenceJSON struct {
	CapacityKWh float64            `json:"capacity_kwh" exclusiveMinimum:"0" doc:"The net capacity of the vehicle's variant, or else the capacity the vendor reports."`
	Source      capacitySourceJSON `json:"source" doc:"catalog_net: the net capacity of the vehicle's variant in Runsten's catalog, an assumption (the state of charge running from 0 to 100 % over the usable capacity). api: the capacity the vendor reports, when the catalog does not know the variant or its net capacity."`
}

// capacityQuartilesJSON summarizes a set of estimates by their median and quartiles:
// the middle half is the uncertainty of the median.
type capacityQuartilesJSON struct {
	CapacityKWh float64 `json:"capacity_kwh" doc:"The median, in kWh."`
	Q1KWh       float64 `json:"q1_kwh" doc:"The first quartile."`
	Q3KWh       float64 `json:"q3_kwh" doc:"The third quartile."`
	Estimates   int     `json:"estimates" minimum:"0" doc:"How many estimates these figures summarize."`
}

// capacityChangeJSON is the evolution of the estimate over the history the trend holds.
type capacityChangeJSON struct {
	Since      time.Time `json:"since" pattern:"Z$" doc:"The date of the first estimate: the evolution is measured from the data at hand, not from when the vehicle was new."`
	InitialKWh float64   `json:"initial_kwh" doc:"The median of the first 20 estimates."`
	ChangePct  int       `json:"change_pct" doc:"The current capacity over the initial one, minus one, in whole percent: signed and never clamped. A ratio of two medians of the same method, in which a constant bias (a charging efficiency, the side of the power) cancels."`
}

// capacityEstimateJSON is one estimate of the battery capacity, from one charge.
type capacityEstimateJSON struct {
	Charge      string               `json:"charge" doc:"The charge the estimate comes from, by its ID (GET /vehicles/{vehicle}/charges/{charge})." example:"2026-09-28T18:30:00.000000Z"`
	At          time.Time            `json:"at" pattern:"Z$" doc:"When the charge ended at the latest: the date the estimate is plotted at."`
	OdometerKm  *float64             `json:"odometer_km" doc:"The charge's odometer, which the estimate is plotted against too; null when no reading of it ended the charge."`
	Source      estimateSourceJSON   `json:"source" doc:"power: the integral of the charging power over the charge's retained span, over the SoC change of the same readings. billed: the energy billed for the charge, entered from its receipt, over the SoC change of the whole charge. rated: the rated range at the end of the charge brought back to 100 % of the usable level, times the vehicle's rated efficiency (a Tesla's). The estimates of one charge are separate points, never averaged."`
	Type        null[chargeTypeJSON] `json:"type" doc:"The charge's type; null when unknown."`
	SpanSoC     float64              `json:"span_soc" doc:"The SoC the estimate divides by, in points: the change over the span for the power source, over the whole charge for the billed one; the usable level at the end of the charge for the rated one."`
	CapacityKWh float64              `json:"capacity_kwh" doc:"The estimate itself."`
}

// capacityExcludedJSON counts, by reason and in the filters' order, the candidates
// they set aside: each counts once, under the first that takes it.
type capacityExcludedJSON struct {
	Reconstructed int `json:"reconstructed" minimum:"0" doc:"The charge was never seen: neither its power nor its receipt is trustworthy."`
	NoPower       int `json:"no_power" minimum:"0" doc:"The vehicle reports no charging power."`
	HighSoC       int `json:"high_soc" minimum:"0" doc:"The charge, or its retained span, reached above the last SoC it keeps (95 %): the power goes into balancing the cells without raising the SoC."`
	SpanSoC       int `json:"span_soc" minimum:"0" doc:"The SoC change is too small (under 20 points): the integer SoC rounds the estimate too much."`
	PowerGap      int `json:"power_gap" minimum:"0" doc:"Two of the span's readings are too far apart (more than 3 minutes): a trapezoid invents the power curve."`
	LowPower      int `json:"low_power" minimum:"0" doc:"A slow charge (under 2 kW on average): its heating and 12 V system weigh in the energy."`
	Implausible   int `json:"implausible" minimum:"0" doc:"Far from the reference capacity (outside 0.6 to 1.15 times it): a false reading or a recalibration."`
	NoEfficiency  int `json:"no_efficiency" minimum:"0" doc:"A rated candidate, while no charge measures the vehicle's rated efficiency yet."`
	LowSoC        int `json:"low_soc" minimum:"0" doc:"A rated candidate whose usable level at the end of the charge is below 50 %: the integer level rounds the estimate too much."`
}

// rangeAtFullJSON is the displayed range at a full charge: the vehicle's own forecast
// of its consumption, kept apart from the estimates.
type rangeAtFullJSON struct {
	MedianKm float64 `json:"median_km" doc:"The median displayed range brought back to 100 % of SoC, in km. The vehicle's forecast follows the season and the driving, not the battery."`
	Readings int     `json:"readings" minimum:"0" doc:"How many trips it summarizes."`
}

// capacityMonthJSON is one calendar month of the trend: the estimates that ended in
// it, and the displayed range of the trips that started in it. A month without a
// value is present with none — a series shows a gap, never a zero.
type capacityMonthJSON struct {
	Start       time.Time                   `json:"start" pattern:"Z$" doc:"The start of the month, in the requested time zone, written in UTC."`
	End         time.Time                   `json:"end" pattern:"Z$" doc:"The end of the month, excluded."`
	Capacity    null[capacityQuartilesJSON] `json:"capacity" doc:"The estimates that ended in the month; null without one."`
	RangeAtFull null[rangeAtFullJSON]       `json:"range_at_full" doc:"The displayed range at a full charge of the trips that started in the month; null without one."`
}

// batteryJSON is what the battery page shows of a vehicle, computed from its whole
// history on each read.
type batteryJSON struct {
	TimeZone        string                      `json:"time_zone"`
	Basis           basisJSON                   `json:"basis" doc:"What current, change and the months' capacities rest on. all: every estimate, all sources together. rated: the rated estimates alone, once there are 5 of them: the vehicle's own count is more precise than the other sources, whose estimates stay in the series."`
	RatedEfficiency null[ratedEfficiencyJSON]   `json:"rated_efficiency" doc:"The vehicle's rated efficiency, the rated estimates rest on. null for a vehicle that tells no rated range or measures no energy (a Volvo, a Polestar), and until a charge measures it."`
	Reference       null[capacityReferenceJSON] `json:"reference" doc:"The capacity the estimates are compared with and bounded by. null when neither the variant's net capacity nor the vendor's is known, and then no estimate is set aside as implausible, no deviation is shown and no cycles either."`
	Current         null[capacityQuartilesJSON] `json:"current" doc:"The median of the latest 20 estimates of the basis, with their quartiles. null under 5 estimates: not enough points to say anything yet, the series still shows them."`
	DeviationPct    *int                        `json:"deviation_pct" doc:"The current capacity over the reference, minus one, in whole percent: signed and never clamped, so that a bias of the method shows as one instead of passing for a new battery. null when either capacity is."`
	Change          null[capacityChangeJSON]    `json:"change" doc:"The evolution of the estimate, from the median of the first 20 estimates of the basis. null while the history holds less than 12 months of it or fewer than 40 estimates."`
	Cycles          *float64                    `json:"cycles" doc:"The energy the charges put through the battery over the reference capacity: an estimate too. null without a reference."`
	Estimates       []capacityEstimateJSON      `json:"estimates" nullable:"false" doc:"The retained estimates, oldest first: one per source and charge, whatever the basis."`
	Excluded        capacityExcludedJSON        `json:"excluded" doc:"The candidates the filters set aside, by reason, in the order of the fields: the reader sees why there are few points."`
	Months          []capacityMonthJSON         `json:"months" nullable:"false" doc:"The calendar months of the history, empty ones included; [] without an estimate nor a trip."`
}

func referenceOf(c core.Capacity) capacityReferenceJSON {
	return capacityReferenceJSON{CapacityKWh: c.KWh, Source: capacitySourceJSON(c.Source)}
}

func quartilesOf(q core.CapacityQuartiles) capacityQuartilesJSON {
	return capacityQuartilesJSON{CapacityKWh: q.Median, Q1KWh: q.Q1, Q3KWh: q.Q3, Estimates: q.Count}
}

func changeOf(c core.CapacityChange) capacityChangeJSON {
	return capacityChangeJSON{Since: c.Since, InitialKWh: c.InitialKWh, ChangePct: c.ChangePct}
}

func estimateOf(e core.CapacityEstimate) capacityEstimateJSON {
	return capacityEstimateJSON{
		Charge: e.Charge.UTC().Format(idFormat), At: e.At, OdometerKm: opt(e.OdometerKm, same),
		Source: estimateSourceJSON(e.Source), Type: nullOf(e.Type, text[core.ChargeType, chargeTypeJSON]),
		SpanSoC: e.SpanSoC, CapacityKWh: e.CapacityKWh,
	}
}

func excludedOf(x core.CapacityExcluded) capacityExcludedJSON {
	return capacityExcludedJSON{
		Reconstructed: x.Reconstructed, NoPower: x.NoPower, HighSoC: x.HighSoC, SpanSoC: x.SpanSoC,
		PowerGap: x.PowerGap, LowPower: x.LowPower, Implausible: x.Implausible,
		NoEfficiency: x.NoEfficiency, LowSoC: x.LowSoC,
	}
}

func efficiencyOf(e core.RatedEfficiency) ratedEfficiencyJSON {
	return ratedEfficiencyJSON{KWhPerKm: e.KWhPerKm, Charges: e.Charges}
}

// basisJSON says what the current capacity rests on.
type basisJSON string

// Bases of the current capacity.
const (
	basisAll   basisJSON = "all"
	basisRated basisJSON = "rated"
)

// Schema lists the bases.
func (basisJSON) Schema(huma.Registry) *huma.Schema { return enumSchema(basisAll, basisRated) }

func rangeOf(r core.RangeMedian) rangeAtFullJSON {
	return rangeAtFullJSON{MedianKm: r.MedianKm, Readings: r.Readings}
}

func monthOf(m core.CapacityMonth) capacityMonthJSON {
	return capacityMonthJSON{
		Start: m.Start, End: m.End,
		Capacity: nullOf(m.Capacity, quartilesOf), RangeAtFull: nullOf(m.Range, rangeOf),
	}
}

// reference is the capacity the trend's estimates are compared with, bounded by and
// divided into cycles: the same one the energies of the events rest on, read as the
// derivation reads it — the net capacity of the variant in effect when the catalog
// knows it, else the one the vendor reports, else none. m is the model of the same
// details (effectiveModel).
func (s *Server) reference(m vehicleModel, n core.Snapshot) core.Value[core.Capacity] {
	if s.Params.Capacity == core.CapacityCatalogNet && m.Variant.OK && m.Variant.V.NetKWh != nil {
		return core.Value[core.Capacity]{V: core.Capacity{KWh: *m.Variant.V.NetKWh, Source: core.CapacityCatalogNet}, OK: true}
	}
	if !n.CapacityKWh.OK {
		return core.Value[core.Capacity]{}
	}
	return core.Value[core.Capacity]{V: core.Capacity{KWh: n.CapacityKWh.V, Source: core.CapacityAPI}, OK: true}
}

// billed lines up the billed energy of each of p.Charges: the energy entered with the
// cost the charge takes, when it has one, with the efficiency a tariff's cost would
// apply to that charge (Pricing.Efficiency). The attachment is the costs' (attach):
// one entered cost, one attachment, never two that could disagree.
func (s *Server) billed(p PricedCharges) []core.Value[core.BilledEnergy] {
	pricing := core.Pricing{Places: p.Places, Params: s.CostParams}
	attached := s.attach(p)
	out := make([]core.Value[core.BilledEnergy], len(p.Charges))
	for i, c := range p.Charges {
		e := attached[i]
		if !e.OK || !e.V.EnergyKWh.OK {
			continue
		}
		out[i] = core.Value[core.BilledEnergy]{
			V: core.BilledEnergy{EnergyKWh: e.V.EnergyKWh.V, Efficiency: pricing.Efficiency(c)}, OK: true,
		}
	}
	return out
}

// getBattery reads the whole history of the vehicle's charges and trips and computes
// what the battery page shows: the capacity is not read over a chosen period.
func (s *Server) getBattery(ctx context.Context, in *batteryInput) (*body[batteryJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	loc, err := location(in.TZ)
	if err != nil {
		return nil, err
	}
	// From nothing until now: the whole history, from one coherent read.
	p, err := s.Reader.PeriodEvents(ctx, sessionFrom(ctx).AccountID, v.ID, time.Time{}, s.Clock.Now().UTC())
	if err != nil {
		return nil, s.internal("events not read", err)
	}
	n, err := s.detailsOf(ctx, v)
	if err != nil {
		return nil, err
	}
	// The efficiencies are the costs': a grid-side power estimate and a charge's cost
	// never rest on two different ones.
	cp := s.CapacityParams
	cp.CostParams = s.CostParams
	t, err := core.CapacityTrend(p.Charges, s.billed(p.PricedCharges), p.Trips, s.reference(effectiveModel(s.Catalog, n, v), n), s.Params, cp, loc)
	switch {
	case errors.Is(err, core.ErrTooManyBuckets):
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "more than 400 months of history")
	case err != nil:
		return nil, s.internal("battery trend not computed", err)
	}
	basis := basisAll
	if t.Rated {
		basis = basisRated
	}
	out := batteryJSON{
		TimeZone: loc.String(), Basis: basis, RatedEfficiency: nullOf(t.Efficiency, efficiencyOf), Reference: nullOf(t.Reference, referenceOf), Current: nullOf(t.Current, quartilesOf),
		DeviationPct: opt(t.DeviationPct, same), Change: nullOf(t.Change, changeOf), Cycles: opt(t.Cycles, same),
		Estimates: []capacityEstimateJSON{}, Excluded: excludedOf(t.Excluded), Months: []capacityMonthJSON{},
	}
	for _, e := range t.Estimates {
		out.Estimates = append(out.Estimates, estimateOf(e))
	}
	for _, m := range t.Months {
		out.Months = append(out.Months, monthOf(m))
	}
	return respond(out), nil
}

// registerBattery registers the estimated battery capacity of a vehicle.
func (s *Server) registerBattery(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getVehicleBattery", Method: http.MethodGet, Path: "/vehicles/{vehicle}/battery", Tags: []string{"battery"},
		Summary: "The estimated battery capacity of a vehicle",
		Description: "Over the whole history of the vehicle, one capacity estimate per source and charge and their " +
			"aggregates: no period is chosen. A power estimate is the integral of the charging power over the charge's " +
			"retained span, over the SoC change of the same readings; a billed one is the energy entered from the " +
			"charge's receipt, over the SoC change of the whole charge. Neither is a measurement: the vehicle exposes " +
			"no energy meter, and the integer SoC rounds a 20-point span by ±5 %. A rated one, for a vehicle that tells " +
			"a rated range and measures the energy it charges (a Tesla), is the rated range at the end of the charge " +
			"brought back to 100 % of the usable level, times the vehicle's rated efficiency (`rated_efficiency`), as " +
			"TeslaMate reads it: the battery management system's own count. `current` is the median of the latest " +
			"20 estimates of the `basis`, every source together, or the rated estimates alone once there are 5; `deviation_pct` compares it with the reference capacity, signed and " +
			"never clamped, so that a bias of the method shows as one instead of passing for a new battery. `change` " +
			"waits for 12 months of history and twice 20 estimates: a ratio of two medians of the same method, in " +
			"which a constant bias cancels, and the trend says more than the level. `cycles` is an estimate too, and " +
			"`excluded` counts the candidates the filters set aside, by reason, so that the reader sees why there are " +
			"few points. `range_at_full` is the vehicle's own forecast of its consumption, which follows the season " +
			"and the driving: shown apart, and never a measure of the battery. The months are split in the requested " +
			"time zone, empty ones included; at most 400 of them.",
	}, "The battery capacity trend.", map[int]string{
		http.StatusBadRequest: "`invalid_parameter`: `tz`, or more than 400 months of history; the message names the " +
			"parameter.",
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getBattery)
}
