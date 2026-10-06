package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/catalog"
	"runsten/internal/core"
)

// VehicleModels writes what the user says of a vehicle's model (*store.Store).
type VehicleModels interface {
	// SetVehicleModel sets the chosen variant (empty: none, the recognition applies)
	// and the stated onboard charger; ErrNotFound without such a vehicle in the account.
	SetVehicleModel(ctx context.Context, accountID, vehicleID, variantID string, acMaxKW core.Value[float64]) error
}

type variantSourceJSON string

// Schema lists the sources of a variant.
func (variantSourceJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(variantDetected, variantChosen)
}

const (
	// variantDetected: recognized from the vehicle's details.
	variantDetected variantSourceJSON = "detected"
	// variantChosen: chosen by the user, which outranks the recognition.
	variantChosen variantSourceJSON = "chosen"
)

// VariantFields are the figures of a variant, from the catalog. Exported, so that huma
// reads the fields of the types that embed it.
type VariantFields struct {
	ID         string   `json:"id" doc:"Identifies the variant in the catalog; stable."`
	Name       string   `json:"name" doc:"A name to display, such as EX30 Single Motor Extended Range."`
	GrossKWh   float64  `json:"gross_kwh" doc:"Gross battery capacity."`
	NetKWh     *float64 `json:"net_kwh" doc:"Usable battery capacity, which the energy estimates rest on (capacity_source catalog_net); null when no source settles it, and they rest on the vendor's."`
	ACMaxKW    float64  `json:"ac_max_kw" doc:"Power of the standard onboard charger."`
	ACOptionKW *float64 `json:"ac_option_kw" doc:"Power of the optional onboard charger; null when the variant has none."`
	DCMaxKW    float64  `json:"dc_max_kw" doc:"Maximum DC charging power."`
}

// variantJSON keeps the name of the schema, Variant.
type variantJSON struct {
	VariantFields
}

type modelJSON struct {
	Family        *string                 `json:"family" doc:"The model as the vendor names it (XC40, EX30), without its powertrain or battery; null until the vehicle's details are read."`
	ModelYear     *int                    `json:"model_year" minimum:"1" doc:"The model year, as the vendor reports it; null until the vehicle's details are read."`
	Variant       null[variantJSON]       `json:"variant" doc:"The variant in effect, from Runsten's catalog: the one the user chose, else the one recognized, the only one that fits the family, model year, battery capacity and VIN the vendor reports. Its figures are the catalog's, from the manufacturer or else from public sources. null when none is chosen and none or several fit, for an unknown family, and for a vehicle that is not battery electric: hybrids are neither recognized nor offered."`
	VariantSource null[variantSourceJSON] `json:"variant_source" doc:"chosen: by the user (PUT /vehicles/{vehicle}/model); detected: recognized from the vehicle's details, recomputed on every read. A chosen variant the catalog no longer has, or of another family than the vehicle reports, is ignored: the recognition applies again. null without a variant."`
	ACMaxKW       *float64                `json:"ac_max_kw" doc:"The onboard charger the user stated, one of the variant's (its ac_max_kw or ac_option_kw). null when not stated, or when it is not one of the chargers of the variant in effect (ignored): the costs then take the more powerful, an upper bound."`
}

// vehicleModel is what the vehicle is taken to be: the family and year it reports, and
// the variant in effect with its onboard charger. effectiveModel is its only source: the
// JSON of the vehicle, the variants offered, the checks of a choice and the costs all
// read it.
type vehicleModel struct {
	Family    core.Value[string]
	ModelYear core.Value[int]
	// Match is the recognition from the details, whatever the user chose. Empty for a
	// vehicle the catalog does not offer variants for.
	Match catalog.Result
	// Variant is the variant in effect: the chosen one when it applies, else the
	// recognized one. Source is empty without one.
	Variant core.Value[catalog.Variant]
	Source  variantSourceJSON
	// StatedChargerKW is the user's charger when it is one of Variant's.
	StatedChargerKW core.Value[float64]
	// ChargerKW is the onboard charger in effect: the stated one, else the more powerful
	// of the variant's (§4.2 of ADR 0022: a bound that never narrows a cost wrongly).
	// Unknown without a variant.
	ChargerKW core.Value[float64]
}

// offered reports whether the catalog offers variants for what the details say: a family
// read, of a battery-electric vehicle. Hybrids are left out, recognized or chosen alike:
// their charges are not derived correctly.
func offered(n core.Snapshot) bool {
	return n.Family.OK && n.BatteryElectric.OK && n.BatteryElectric.V
}

// effectiveModel decides the vehicle's model from its latest details n and what the user
// wrote (v.VariantID, v.ACMaxKW). The choice outranks the recognition while it holds: a
// variant the catalog still has, of the family read. The charger stated holds while it is
// one of the variant's in effect. Pure: it reads nothing else.
func effectiveModel(c *catalog.Catalog, n core.Snapshot, v Vehicle) vehicleModel {
	m := vehicleModel{Family: n.Family, ModelYear: n.ModelYear}
	if !offered(n) {
		return m
	}
	// Unknown year or capacity: zero, which Match does not filter on.
	m.Match = c.Match(n.Family.V, n.ModelYear.V, n.CapacityKWh.V, v.VIN)
	// The rule of catalog.Effective, which the derivation's capacities follow, told apart
	// for the source.
	if chosen, ok := c.Chosen(v.VariantID, catalogReading(n, v.VIN)); ok {
		m.Variant, m.Source = core.Value[catalog.Variant]{V: chosen, OK: true}, variantChosen
	} else if m.Match.Recognized {
		m.Variant, m.Source = core.Value[catalog.Variant]{V: m.Match.Candidates[0], OK: true}, variantDetected
	} else {
		return m
	}
	chargers := chargersOf(m.Variant.V)
	if v.ACMaxKW.OK && slices.Contains(chargers, v.ACMaxKW.V) {
		m.StatedChargerKW = v.ACMaxKW
		m.ChargerKW = v.ACMaxKW
	} else {
		m.ChargerKW = core.Value[float64]{V: slices.Max(chargers), OK: true}
	}
	return m
}

// catalogReading is what the catalog's rules read of the details. Unknown year or capacity:
// zero, which they do not filter on.
func catalogReading(n core.Snapshot, vin string) catalog.Reading {
	return catalog.Reading{
		Family: n.Family.V, ModelYear: n.ModelYear.V, CapacityKWh: n.CapacityKWh.V, VIN: vin,
		BatteryElectric: n.BatteryElectric.OK && n.BatteryElectric.V,
	}
}

// chargersOf lists the onboard chargers a variant may have: the standard one, and the
// optional one if any.
func chargersOf(v catalog.Variant) []float64 {
	if v.ACOptionKW == nil {
		return []float64{v.ACMaxKW}
	}
	return []float64{v.ACMaxKW, *v.ACOptionKW}
}

func modelOf(m vehicleModel) modelJSON {
	out := modelJSON{
		Family: opt(m.Family, same), ModelYear: opt(m.ModelYear, same),
		Variant: nullOf(m.Variant, variantOf), ACMaxKW: opt(m.StatedChargerKW, same),
	}
	if m.Source != "" {
		out.VariantSource = known(m.Source)
	}
	return out
}

func variantFieldsOf(v catalog.Variant) VariantFields {
	return VariantFields{
		ID: v.ID, Name: v.Name, GrossKWh: v.GrossKWh, NetKWh: v.NetKWh,
		ACMaxKW: v.ACMaxKW, ACOptionKW: v.ACOptionKW, DCMaxKW: v.DCMaxKW,
	}
}

func variantOf(v catalog.Variant) variantJSON { return variantJSON{variantFieldsOf(v)} }

// chargerOf reads the vehicle's current state for its onboard charger in effect, which
// bounds the costs of its AC charges; unknown without a variant. The error is the store's,
// for the caller to report.
func (s *Server) chargerOf(ctx context.Context, v Vehicle) (core.Value[float64], error) {
	c, err := s.States.Current(ctx, sessionFrom(ctx).AccountID, v.ID, s.Clock.Now())
	if err != nil {
		return core.Value[float64]{}, err //nolint:wrapcheck // reported by the caller, never shown
	}
	return effectiveModel(s.Catalog, c.Snapshot, v).ChargerKW, nil
}

// detailsOf reads the vehicle's current state for its model: its latest details, on
// every read, so that a correction of the catalog shows without a migration.
func (s *Server) detailsOf(ctx context.Context, v Vehicle) (core.Snapshot, error) {
	c, err := s.States.Current(ctx, sessionFrom(ctx).AccountID, v.ID, s.Clock.Now())
	if err != nil {
		return core.Snapshot{}, s.internal("vehicle model not read", err)
	}
	return c.Snapshot, nil
}

// vehicleOf reads the vehicle's current state for its model.
func (s *Server) vehicleOf(ctx context.Context, v Vehicle) (vehicleJSON, error) {
	n, err := s.detailsOf(ctx, v)
	if err != nil {
		return vehicleJSON{}, err
	}
	m := effectiveModel(s.Catalog, n, v)
	return vehicleJSON{ID: v.ID, VIN: v.VIN, Model: modelOf(m), Connection: s.connectionOf(v), Collection: collectionOf(v)}, nil
}

type sourceLevelJSON string

// Schema lists the levels of a source.
func (sourceLevelJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(sourceLevelJSON(catalog.Manufacturer), sourceLevelJSON(catalog.Secondary))
}

// figureLevelsJSON tells, figure by figure, where the catalog has it from.
type figureLevelsJSON struct {
	GrossKWh   sourceLevelJSON       `json:"gross_kwh"`
	NetKWh     null[sourceLevelJSON] `json:"net_kwh" doc:"null when net_kwh is."`
	ACMaxKW    sourceLevelJSON       `json:"ac_max_kw"`
	ACOptionKW null[sourceLevelJSON] `json:"ac_option_kw" doc:"null when ac_option_kw is."`
	DCMaxKW    sourceLevelJSON       `json:"dc_max_kw"`
}

type modelYearsJSON struct {
	From int  `json:"from" minimum:"1"`
	To   *int `json:"to" minimum:"1" doc:"null while the variant is on sale."`
}

type variantOptionJSON struct {
	VariantFields
	ModelYears modelYearsJSON   `json:"model_years" doc:"The model years of the variant, included: two variants of the same name differ by them."`
	Candidate  bool             `json:"candidate" doc:"The vehicle's details fit the variant, and not every variant of the family: the only candidate is the recognized variant. false for all when the details tell none apart, or fit none."`
	Levels     figureLevelsJSON `json:"levels" doc:"Where each figure comes from: manufacturer, published by the maker; secondary, from public sources (press, encyclopedias), cross-checked."`
}

type variantListJSON struct {
	Items []variantOptionJSON `json:"items" nullable:"false"`
}

type vehicleModelUpdateJSON struct {
	VariantID *string  `json:"variant_id" minLength:"1" doc:"The ID of a variant of the vehicle's family, from GET /vehicles/{vehicle}/variants: it outranks the recognition. null to go back to the recognition."`
	ACMaxKW   *float64 `json:"ac_max_kw" exclusiveMinimum:"0" doc:"The vehicle's onboard charger, one of the variant's in effect after the write (its ac_max_kw or ac_option_kw). null when unknown: the costs take the more powerful."`
}

type vehicleModelInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	Body    vehicleModelUpdateJSON
}

func yearOrNil(y int) *int {
	if y == 0 {
		return nil
	}
	return &y
}

func levelOf(v catalog.Variant, f catalog.Figure) sourceLevelJSON { return sourceLevelJSON(v.Level(f)) }

func optionalLevel(v catalog.Variant, f catalog.Figure) null[sourceLevelJSON] {
	if v.Level(f) == "" {
		return null[sourceLevelJSON]{}
	}
	return known(levelOf(v, f))
}

// variantsOf lists the variants the user may choose from: the recognition's candidates
// first, then the rest of the family, each in the order of the catalog.
func (s *Server) variantsOf(m vehicleModel) variantListJSON {
	out := variantListJSON{Items: []variantOptionJSON{}}
	if !m.Family.OK || len(m.Match.Candidates) == 0 {
		return out
	}
	family := s.Catalog.Family(m.Family.V)
	// Match gives the whole family when nothing fits: then, as when everything does, the
	// details narrow nothing, and no variant is a candidate.
	narrows := m.Match.Recognized || len(m.Match.Candidates) < len(family)
	isCandidate := func(v catalog.Variant) bool {
		return narrows && slices.ContainsFunc(m.Match.Candidates, func(c catalog.Variant) bool { return c.ID == v.ID })
	}
	for _, first := range []bool{true, false} {
		for _, v := range family {
			if isCandidate(v) != first {
				continue
			}
			out.Items = append(out.Items, variantOptionJSON{
				VariantFields: variantFieldsOf(v), ModelYears: modelYearsJSON{From: v.Years.From, To: yearOrNil(v.Years.To)},
				Candidate: first,
				Levels: figureLevelsJSON{
					GrossKWh: levelOf(v, catalog.FigureGrossKWh), NetKWh: optionalLevel(v, catalog.FigureNetKWh),
					ACMaxKW: levelOf(v, catalog.FigureACMaxKW), ACOptionKW: optionalLevel(v, catalog.FigureACOptionKW),
					DCMaxKW: levelOf(v, catalog.FigureDCMaxKW),
				},
			})
		}
	}
	return out
}

func (s *Server) listVariants(ctx context.Context, in *vehicleInput) (*body[variantListJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	n, err := s.detailsOf(ctx, v)
	if err != nil {
		return nil, err
	}
	return respond(s.variantsOf(effectiveModel(s.Catalog, n, v))), nil
}

// putModel writes the user's choice, checked against the details as they are: a choice
// that no longer holds later is ignored on reading (effectiveModel), not refused.
func (s *Server) putModel(ctx context.Context, in *vehicleModelInput) (*body[vehicleJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	n, err := s.detailsOf(ctx, v)
	if err != nil {
		return nil, err
	}
	v.VariantID, v.ACMaxKW = "", core.Value[float64]{}
	if id := in.Body.VariantID; id != nil {
		chosen, ok := s.Catalog.Variant(*id)
		switch {
		case !ok:
			return nil, invalidBody("variant_id", "unknown variant")
		case !offered(n) || chosen.Family != n.Family.V:
			return nil, invalidBody("variant_id", "not a variant of the vehicle's family")
		}
		v.VariantID = *id
	}
	if kw := in.Body.ACMaxKW; kw != nil {
		v.ACMaxKW = core.Value[float64]{V: *kw, OK: true}
	}
	// The model after the write, from the same details: its charger must be the one
	// stated.
	after := effectiveModel(s.Catalog, n, v)
	if v.ACMaxKW.OK && !after.StatedChargerKW.OK {
		return nil, invalidBody("ac_max_kw", "not an onboard charger of the vehicle's variant")
	}
	if err := s.Models.SetVehicleModel(ctx, sessionFrom(ctx).AccountID, v.ID, v.VariantID, v.ACMaxKW); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, apiError(http.StatusNotFound, codeNotFound, "no such vehicle")
		}
		return nil, s.internal("vehicle model not written", err)
	}
	return respond(vehicleJSON{ID: v.ID, VIN: v.VIN, Model: modelOf(after), Connection: s.connectionOf(v)}), nil
}

// registerModel registers the variants offered for a vehicle, and the user's choice.
func (s *Server) registerModel(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listVariants", Method: http.MethodGet, Path: "/vehicles/{vehicle}/variants", Tags: []string{"vehicles"},
		Summary: "The variants a vehicle may be",
		Description: "The variants of the catalog of the family the vehicle reports: the candidates of the recognition " +
			"first, then the rest of the family, whatever the model years. Empty until the details are read, for a " +
			"family the catalog does not have, and for a vehicle that is not battery electric.",
	}, "The variants, candidates first.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.listVariants)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putVehicleModel", Method: http.MethodPut, Path: "/vehicles/{vehicle}/model", Tags: []string{"vehicles"},
		Summary: "Choose a vehicle's variant and onboard charger",
		Description: "The choice outranks the recognition, and shows wherever the model does. The onboard charger " +
			"bounds the costs of AC charges at once. A new variant has the vehicle's trips and charges derived again " +
			"on its net capacity, by the collector's next pass: their energies change then. The choice is checked " +
			"against the vehicle's details as they are, and a choice that no longer holds later (a variant the " +
			"catalog no longer has) is ignored on reading.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, "The vehicle, with its model.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " Also: a `variant_id` the catalog does not have, or of another family than " +
			"the vehicle's; an `ac_max_kw` that is not a charger of the variant in effect after the write, or without one.",
		http.StatusNotFound: errVehicle,
	})), s.putModel)
}
