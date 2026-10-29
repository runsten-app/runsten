package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// Charging gives the charge in progress of a vehicle: the derivation replayed from its
// cursor, nothing saved. net is the vehicle's net capacity, as the derivation's.
type Charging interface {
	InProgress(ctx context.Context, accountID, vehicleID string, net core.NetCapacity) (core.Value[core.ChargeInProgress], error)
}

// chargingNowJSON is the answer of GET /vehicles/{vehicle}/charging.
type chargingNowJSON struct {
	Charge null[chargeInProgressJSON] `json:"charge" doc:"The charge open at the vehicle's latest reading; null when none is."`
}

// chargeInProgressJSON is a charge not over yet: a charge of the list without its end,
// with its latest reading in its place.
type chargeInProgressJSON struct {
	ID                string               `json:"id" doc:"The ID the charge will have in the list once over: its detection time, to the microsecond. Treat it as opaque." example:"2026-09-28T18:30:00.000000Z"`
	DetectedAt        time.Time            `json:"detected_at" pattern:"Z$" doc:"The first reading that revealed the charge."`
	Sources           []string             `json:"sources" nullable:"false" minItems:"1" doc:"The sources of the charge's readings so far, sorted, as an event's." example:"[\"volvo\"]"`
	Start             boundsJSON           `json:"start" doc:"When the charge started."`
	ChargingAt        time.Time            `json:"charging_at" pattern:"Z$" doc:"The latest reading of the charge charging."`
	ReadAt            time.Time            `json:"read_at" pattern:"Z$" doc:"The latest reading of the vehicle's charge, which the values below are: later than charging_at while the charge is paused (the vehicle says it is scheduled, or in error), the cable still connected. Far behind the present, the readings stopped: the charge may have ended since, unseen."`
	Type              null[chargeTypeJSON] `json:"type"`
	StartSoCPct       *float64             `json:"start_soc_pct"`
	SoCPct            *float64             `json:"soc_pct" doc:"The state of charge at read_at, in %."`
	TargetSoCPct      *float64             `json:"target_soc_pct"`
	PowerW            *float64             `json:"power_w" doc:"The charging power at read_at, in W."`
	EnergySoCKWh      *float64             `json:"energy_soc_kwh" doc:"The energy added so far, one of two estimates: ΔSoC × capacity_kwh, from start_soc_pct to soc_pct."`
	EnergyPowerKWh    *float64             `json:"energy_power_kwh" doc:"The other estimate: the charging power integrated over the readings so far."`
	EnergyMeasuredKWh *float64             `json:"energy_measured_kwh" doc:"The energy the vehicle measured into its battery so far, from its own counter (a Tesla's); null for a vehicle without one."`
	CapacityFields
	Position null[positionJSON] `json:"position"`
	Place    null[placeRefJSON] `json:"place" doc:"As a charge's."`
	Address  *string            `json:"address" doc:"As a charge's."`
	Cost     null[costJSON]     `json:"cost" doc:"What the charge cost so far, from the tariff of its place, as a charge's up to read_at; null when unknown. A charge in progress takes no entered cost."`
}

func (s *Server) getCharging(ctx context.Context, in *vehicleInput) (*body[chargingNowJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	acc := sessionFrom(ctx).AccountID
	open, err := s.Charging.InProgress(ctx, acc, v.ID, s.netCapacity(v))
	if err != nil {
		return nil, s.internal("charge in progress not read", err)
	}
	if !open.OK {
		return respond(chargingNowJSON{}), nil
	}
	l, err := s.limitsOf(ctx, acc)
	if err != nil {
		return nil, err
	}
	places, err := s.Settings.Places(ctx, acc)
	if err != nil {
		return nil, s.internal("places not read", err)
	}
	c := open.V
	var cost core.Value[core.Cost]
	if !l.NoCosts {
		currency, err := s.Settings.Currency(ctx, acc)
		if err != nil {
			return nil, s.internal("currency not read", err)
		}
		charger, err := s.chargerOf(ctx, v)
		if err != nil {
			return nil, s.internal("vehicle model not read", err)
		}
		pricing := core.Pricing{Currency: currency, Places: places, OnboardChargerKW: charger, Params: s.CostParams}
		cost = pricing.ChargeCost(c.Charge, core.Value[core.EnteredCost]{})
	}
	place, _ := core.PlaceOf(c.Charge, places)
	ref := nullOf(core.Value[core.Place]{V: place, OK: place.ID != ""}, placeRefOf)
	book := s.addresses(ctx, places, c.Position)
	return respond(chargingNowJSON{Charge: known(chargeInProgressJSON{
		ID: c.DetectedAt.UTC().Format(idFormat), DetectedAt: c.DetectedAt, Sources: append([]string{}, c.Sources...),
		Start: boundsJSON(c.Start), ChargingAt: c.End.After, ReadAt: c.End.Before,
		Type:        nullOf(c.Type, text[core.ChargeType, chargeTypeJSON]),
		StartSoCPct: opt(c.StartSoC, same), SoCPct: opt(c.EndSoC, same), TargetSoCPct: opt(c.TargetSoC, same),
		PowerW:       opt(c.PowerW, same),
		EnergySoCKWh: opt(c.EnergySoCKWh, same), EnergyPowerKWh: opt(c.EnergyPowerKWh, same),
		EnergyMeasuredKWh: opt(c.EnergyMeasuredKWh, same),
		CapacityFields:    capacityOf(c.Capacity),
		Position:          nullOf(c.Position, positionOf),
		Place:             ref,
		Address:           addressOr(book.of(c.Position), ref, ""),
		Cost:              nullOf(cost, costOf),
	})}), nil
}

// netCapacity is the vehicle's net capacity as the derivation reads it: that of the
// variant in effect (catalog.Effective), from the details of each reading.
func (s *Server) netCapacity(v Vehicle) core.NetCapacity {
	return func(n core.Snapshot) (float64, bool) {
		variant, ok := s.Catalog.Effective(v.VariantID, catalogReading(n, v.VIN))
		if !ok || variant.NetKWh == nil {
			return 0, false
		}
		return *variant.NetKWh, true
	}
}

func (s *Server) registerCharging(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getChargeInProgress", Method: http.MethodGet, Path: "/vehicles/{vehicle}/charging", Tags: []string{"events"},
		Summary: "The charge in progress of a vehicle",
		Description: "The charge open at the vehicle's latest reading, as it stands: what it added so far, its power " +
			"and its cost. It is not an event: the list of charges, its cursors and the statistics never count it. " +
			"Once over, it is in the list under the same id, and this answers null. Read again every minute, as the " +
			"collector writes; read_at tells how fresh it is.",
	}, "The charge in progress, or null.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getCharging)
}
