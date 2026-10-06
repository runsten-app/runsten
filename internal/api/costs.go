package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// MaxPlaces bounds the places of an account. A safety cap, not a rule of the domain: a
// few places are expected (home, work, a second home, the chargers one uses often), and
// each costs a distance per charge on every read. Well above use, so that no driver
// meets it.
const MaxPlaces = 100

// Errors of the writes of the settings, places and entered costs.
var (
	// ErrNotFound: no such place, charge or entered cost in the account.
	ErrNotFound = errors.New("not found")
	// ErrCurrencyInUse: the currency cannot change while a tariff or an entered cost
	// exists, whose amounts have no unit of their own.
	ErrCurrencyInUse = errors.New("the currency is in use")
	// ErrWithoutPositionTaken: another place already takes the charges without a position.
	ErrWithoutPositionTaken = errors.New("another place takes the charges without a position")
	// ErrTooManyPlaces: the account already has MaxPlaces places.
	ErrTooManyPlaces = errors.New("too many places")
	// ErrChargeHasCost: the charge already has an entered cost.
	ErrChargeHasCost = errors.New("the charge already has an entered cost")
)

// EnteredCost is an entered cost as stored, with what names it apart from its charge:
// an orphaned one is attached or deleted by its ID.
type EnteredCost struct {
	ID        string
	VehicleID string
	EnteredAt time.Time
	core.EnteredCost
}

// PricedCharges are charges read with what their costs depend on, from one snapshot
// of the database: a response never sees half of a rebuild, nor an entered cost
// without its charge.
type PricedCharges struct {
	Charges []core.Charge
	// Deciding are the other charges of the vehicle that decide which entered cost each
	// of Charges takes: attaching Entered to Charges and Deciding gives, for Charges,
	// the attachment over the vehicle's whole history. Its orphans, though, are not the
	// history's.
	Deciding []core.Charge
	Entered  []core.EnteredCost
	Currency core.Value[core.Currency]
	Places   []core.Place // in the order they were created
}

// Period are the events of a period, for its statistics, read like PricedCharges.
type Period struct {
	// Trips and PricedCharges.Charges are the events that started in the period, and
	// the latest one that started before it (a trip or a charge), in no particular
	// order.
	Trips []core.Trip
	PricedCharges
	// Orphans are the vehicle's entered costs that no charge takes, whenever entered.
	Orphans []core.EnteredCost
}

// ChargeCosts writes the entered costs of the account's charges. Every method is
// restricted to the account: another account's charge or entered cost is not found.
type ChargeCosts interface {
	// SetChargeCost enters the cost of the vehicle's charge detected at
	// e.ChargeDetectedAt, replacing the one the charge takes, even by overlap;
	// ErrNotFound without such a charge.
	SetChargeCost(ctx context.Context, accountID, vehicleID string, e core.EnteredCost) error
	// DeleteChargeCost deletes the entered cost the charge takes; ErrNotFound without
	// such a charge, or if it takes none.
	DeleteChargeCost(ctx context.Context, accountID, vehicleID string, detectedAt time.Time) error
	// OrphanCosts are the entered costs no charge takes, newest first.
	OrphanCosts(ctx context.Context, accountID string) ([]EnteredCost, error)
	// AttachEnteredCost attaches an entered cost to the charge; ErrNotFound without such
	// a cost or charge, ErrChargeHasCost if the charge takes another cost already.
	AttachEnteredCost(ctx context.Context, accountID, costID, vehicleID string, detectedAt time.Time) error
	// DeleteEnteredCost deletes an entered cost by its ID; ErrNotFound without it.
	DeleteEnteredCost(ctx context.Context, accountID, costID string) error
}

// attach enters the costs of p.Charges over the vehicle's whole history, Deciding
// included: attached[i] is the entered cost p.Charges[i] takes. The costs of the charges
// and the billed energies of the battery read the same one: one entered cost, one
// attachment, never two that could disagree.
func (s *Server) attach(p PricedCharges) []core.Value[core.EnteredCost] {
	attached, _ := core.AttachCosts(append(slices.Clip(p.Charges), p.Deciding...), p.Entered)
	return attached[:len(p.Charges)]
}

// costs gives the cost of each of p.Charges, a vehicle's, its entered costs attached as
// over the vehicle's whole history. charger is the vehicle's onboard charger (chargerOf):
// the account's places and tariffs, bounded by the vehicle's power.
func (s *Server) costs(p PricedCharges, charger core.Value[float64]) []core.Value[core.Cost] {
	pricing := core.Pricing{Currency: p.Currency, Places: p.Places, OnboardChargerKW: charger, Params: s.CostParams}
	attached := s.attach(p)
	out := make([]core.Value[core.Cost], len(p.Charges))
	for i, c := range p.Charges {
		out[i] = pricing.ChargeCost(c, attached[i])
	}
	return out
}

// chargesOf converts p.Charges, a vehicle's, in their order, with their places,
// addresses and costs.
// l's NoCosts leaves every cost unknown.
func (s *Server) chargesOf(ctx context.Context, p PricedCharges, charger core.Value[float64], l AccountLimits) []chargeJSON {
	costs := make([]core.Value[core.Cost], len(p.Charges))
	if !l.NoCosts {
		costs = s.costs(p, charger)
	}
	positions := make([]core.Value[core.Position], len(p.Charges))
	for i, c := range p.Charges {
		positions[i] = c.Position
	}
	book := s.addresses(ctx, p.Places, positions...)
	out := make([]chargeJSON, len(p.Charges))
	for i, c := range p.Charges {
		out[i] = chargeOf(c, p.Places, costs[i], book)
	}
	return out
}

type costSourceJSON string

// Schema lists the sources of a cost.
func (costSourceJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.CostTariff, core.CostEntered)
}

// placeRefJSON names a place of the account: the one of a charge, or of a trip's start
// or end.
type placeRefJSON struct {
	ID   string `json:"id" format:"uuid"`
	Name string `json:"name"`
}

type costJSON struct {
	Currency currencyCodeJSON `json:"currency" doc:"The account's currency, the unit of min_minor and max_minor."`
	MinMinor int64            `json:"min_minor" minimum:"0" doc:"The lowest amount the charge may have cost, in minor units of the currency (cents): the energy went through somewhere within [start.after, end.before], at prices that may change over it, so every amount the bounds allow lies within [min_minor, max_minor]. Equal to max_minor when the price is the same over the whole window, and for an entered cost."`
	MaxMinor int64            `json:"max_minor" minimum:"0" doc:"The highest amount the charge may have cost."`
	Source   costSourceJSON   `json:"source" doc:"tariff: estimated from the tariff of the charge's place. entered: the amount the user entered for the charge, which replaces the tariff's."`
	// The two estimates behind a tariff's cost are said as such.
	EnergyKWh  *float64 `json:"energy_kwh" doc:"The billed energy. For a tariff, an estimate: energy_soc_kwh (itself an estimate) over efficiency. For an entered cost, the energy entered with it, as read on the receipt; null if none was."`
	Efficiency *float64 `json:"efficiency" doc:"For a tariff, the assumed share of the energy drawn from the grid that reaches the battery: the place's, or else the default of the settings (default_efficiency: ac for AC and unknown types, dc for DC). null for an entered cost."`
	Note       *string  `json:"note" doc:"The note entered with the cost; null for a tariff, and without a note."`
}

func costOf(c core.Cost) costJSON {
	out := costJSON{
		Currency: currencyCodeJSON(c.Currency.Code), MinMinor: c.Min, MaxMinor: c.Max, Source: costSourceJSON(c.Source),
		EnergyKWh: opt(c.EnergyKWh, same), Efficiency: opt(c.Efficiency, same),
	}
	if c.Note != "" {
		out.Note = &c.Note
	}
	return out
}

func placeRefOf(p core.Place) placeRefJSON { return placeRefJSON{ID: p.ID, Name: p.Name} }

// Addresses gives the addresses of an account's positions, per cell, as a reverse
// geocoder found them, and asks for those not asked for yet: a later read has them. A
// cell whose geocoder knew no address, or not resolved yet, is left out.
type Addresses interface {
	Addresses(ctx context.Context, accountID string, cells []core.GeoCell) (map[core.GeoCell]string, error)
}

// addressBook holds the addresses of the positions of a response.
type addressBook map[core.GeoCell]string

// addresses are those of the positions outside every place: a place's name says more,
// and the geocoder is not asked for it. Nil without a geocoder; an error of the store
// leaves the response without addresses, never fails it.
func (s *Server) addresses(ctx context.Context, places []core.Place, positions ...core.Value[core.Position]) addressBook {
	if s.Addresses == nil {
		return nil
	}
	seen := map[core.GeoCell]bool{}
	var cells []core.GeoCell
	for _, p := range positions {
		if !p.OK {
			continue
		}
		if _, at := core.PlaceAt(p.V, places); at {
			continue
		}
		if c := core.CellOf(p.V); !seen[c] {
			seen[c] = true
			cells = append(cells, c)
		}
	}
	if len(cells) == 0 {
		return nil
	}
	book, err := s.Addresses.Addresses(ctx, sessionFrom(ctx).AccountID, cells)
	if err != nil {
		s.Log.WarnContext(ctx, "addresses not read", "error", err)
		return nil
	}
	return book
}

// of is the address of a position, if it has one.
func (b addressBook) of(p core.Value[core.Position]) *string {
	if !p.OK {
		return nil
	}
	a, ok := b[core.CellOf(p.V)]
	if !ok {
		return nil
	}
	return &a
}

// placeAt is the place of a position, if it has one and a place holds it.
func placeAt(pos core.Value[core.Position], places []core.Place) null[placeRefJSON] {
	if !pos.OK {
		return null[placeRefJSON]{}
	}
	p, ok := core.PlaceAt(pos.V, places)
	return nullOf(core.Value[core.Place]{V: p, OK: ok}, placeRefOf)
}

// The limits of an entered cost, given by GET /settings (enteredCostLimitsJSON), and
// repeated in the tags of enteredCostJSON: TestLimitsMatchTheSpec checks both agree.
// Safety caps, far above a real charge (see the limits of a place).
const (
	maxAmountMinor = 1_000_000_000 // in minor units: 10 million euros, 1 billion won
	maxEnergyKWh   = 500           // more than the largest battery
	maxNoteChars   = 500
)

// enteredCostJSON is the body of an entered cost.
type enteredCostJSON struct {
	AmountMinor int64    `json:"amount_minor" minimum:"0" maximum:"1000000000" doc:"What was paid, in minor units of the account's currency (cents), taxes included. 0 is free: a true zero, not an unknown cost."`
	EnergyKWh   *float64 `json:"energy_kwh" exclusiveMinimum:"0" maximum:"500" doc:"The billed energy, as read on the receipt; null if not known."`
	Note        *string  `json:"note" maxLength:"500" doc:"Free text, such as the operator or the means of payment, shown to the user only. null or empty for none."`
}

type chargeCostInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	ID      string `path:"id" doc:"The charge ID. Any RFC 3339 form of its detection time is accepted too." example:"2026-09-28T18:30:00.000000Z"`
	Body    enteredCostJSON
}

type chargeCostDelete struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	ID      string `path:"id" doc:"The charge ID." example:"2026-09-28T18:30:00.000000Z"`
}

type orphanJSON struct {
	ID          string                 `json:"id" format:"uuid"`
	VehicleID   string                 `json:"vehicle_id" format:"uuid"`
	EnteredAt   time.Time              `json:"entered_at" pattern:"Z$" doc:"When the cost was entered, or last entered again."`
	Window      boundsJSON             `json:"window" doc:"The window [start.after, end.before] of its charge when the cost was entered. Since then, a rebuild of the derived data changed the charges: no charge is at its detection time, and none or several overlap this window."`
	AmountMinor int64                  `json:"amount_minor" minimum:"0" doc:"What was paid, in minor units of the account's currency."`
	Currency    null[currencyCodeJSON] `json:"currency" doc:"The account's currency; null until it is set."`
	EnergyKWh   *float64               `json:"energy_kwh" doc:"The billed energy entered with the cost; null if none was."`
	Note        *string                `json:"note" doc:"null without a note."`
}

type orphanListJSON struct {
	Items []orphanJSON `json:"items" nullable:"false"`
}

func orphanOf(e EnteredCost, c core.Value[core.Currency]) orphanJSON {
	out := orphanJSON{
		ID: e.ID, VehicleID: e.VehicleID, EnteredAt: e.EnteredAt,
		Window:      boundsJSON{After: e.WindowAfter, Before: e.WindowBefore},
		AmountMinor: e.AmountMinor, EnergyKWh: opt(e.EnergyKWh, same),
		Currency: nullOf(c, func(c core.Currency) currencyCodeJSON { return currencyCodeJSON(c.Code) }),
	}
	if e.Note != "" {
		out.Note = &e.Note
	}
	return out
}

type orphanInput struct {
	ID string `path:"id" doc:"The entered cost's ID."`
}

type attachJSON struct {
	// Not declared as UUIDs: like those of the path, a malformed ID is not found.
	Vehicle string `json:"vehicle" doc:"The ID of the vehicle of the charge."`
	Charge  string `json:"charge" doc:"The ID of the charge, which takes the cost from now on." example:"2026-09-28T18:30:00.000000Z"`
}

type attachInput struct {
	ID   string `path:"id" doc:"The entered cost's ID."`
	Body attachJSON
}

// costError maps the refusals of the store to the contract.
func (s *Server) costError(msg, notFound string, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return apiError(http.StatusNotFound, codeNotFound, notFound)
	case errors.Is(err, ErrChargeHasCost):
		return apiError(http.StatusConflict, codeChargeHasCost, "the charge already has an entered cost")
	}
	return s.internal(msg, err)
}

// chargeAt answers the vehicle's charge detected at at, with its cost.
func (s *Server) chargeAt(ctx context.Context, v Vehicle, at time.Time) (*body[chargeJSON], error) {
	charger, err := s.chargerOf(ctx, v)
	if err != nil {
		return nil, s.internal("vehicle model not read", err)
	}
	p, found, err := s.Reader.FindCharge(ctx, sessionFrom(ctx).AccountID, v.ID, at)
	switch {
	case err != nil:
		return nil, s.internal("charge not read", err)
	case !found:
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such charge")
	}
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	if l.hides(p.Charges[0].End) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such charge")
	}
	return respond(s.chargesOf(ctx, p, charger, l)[0]), nil
}

func (s *Server) putChargeCost(ctx context.Context, in *chargeCostInput) (*body[chargeJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	at, err := eventTime(in.ID, "charge")
	if err != nil {
		return nil, err
	}
	e := core.EnteredCost{ChargeDetectedAt: at, AmountMinor: in.Body.AmountMinor}
	if in.Body.EnergyKWh != nil {
		e.EnergyKWh = core.Value[float64]{V: *in.Body.EnergyKWh, OK: true}
	}
	if in.Body.Note != nil {
		e.Note = *in.Body.Note
	}
	if err := s.Costs.SetChargeCost(ctx, sessionFrom(ctx).AccountID, v.ID, e); err != nil {
		return nil, s.costError("cost not entered", "no such charge", err)
	}
	return s.chargeAt(ctx, v, at)
}

func (s *Server) deleteChargeCost(ctx context.Context, in *chargeCostDelete) (*struct{}, error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	at, err := eventTime(in.ID, "charge")
	if err != nil {
		return nil, err
	}
	if err := s.Costs.DeleteChargeCost(ctx, sessionFrom(ctx).AccountID, v.ID, at); err != nil {
		return nil, s.costError("cost not deleted", "no such charge, or it has no entered cost", err)
	}
	return &struct{}{}, nil
}

func (s *Server) listOrphans(ctx context.Context, _ *struct{}) (*body[orphanListJSON], error) {
	acc := sessionFrom(ctx).AccountID
	// Two reads: the currency may only be set meanwhile, not changed (ErrCurrencyInUse).
	c, err := s.Settings.Currency(ctx, acc)
	if err != nil {
		return nil, s.internal("currency not read", err)
	}
	orphans, err := s.Costs.OrphanCosts(ctx, acc)
	if err != nil {
		return nil, s.internal("orphaned costs not read", err)
	}
	out := orphanListJSON{Items: []orphanJSON{}}
	for _, o := range orphans {
		out.Items = append(out.Items, orphanOf(o, c))
	}
	return respond(out), nil
}

func (s *Server) attachOrphan(ctx context.Context, in *attachInput) (*body[chargeJSON], error) {
	if !uuidPattern.MatchString(in.ID) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such entered cost")
	}
	v, err := s.accountVehicle(ctx, in.Body.Vehicle)
	if err != nil {
		return nil, err
	}
	at, err := eventTime(in.Body.Charge, "charge")
	if err != nil {
		return nil, err
	}
	if err := s.Costs.AttachEnteredCost(ctx, sessionFrom(ctx).AccountID, in.ID, v.ID, at); err != nil {
		return nil, s.costError("cost not attached", "no such entered cost or charge", err)
	}
	return s.chargeAt(ctx, v, at)
}

func (s *Server) deleteOrphan(ctx context.Context, in *orphanInput) (*struct{}, error) {
	if !uuidPattern.MatchString(in.ID) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such entered cost")
	}
	if err := s.Costs.DeleteEnteredCost(ctx, sessionFrom(ctx).AccountID, in.ID); err != nil {
		return nil, s.costError("cost not deleted", "no such entered cost", err)
	}
	return &struct{}{}, nil
}

// registerCosts registers the entered costs of the charges.
func (s *Server) registerCosts(api huma.API) {
	const (
		errCharge = "`not_found`: no such vehicle in the account, or no such charge for it."
		errOrphan = "`not_found`: no such entered cost in the account, or no such vehicle or charge."
	)
	// The costs are the account's offer's: its limits may leave them out (403, as a
	// request from another origin).
	costs := huma.Middlewares{s.requireCosts}
	jsonBody := huma.Middlewares{s.requireJSON, s.requireCosts}
	const forbidden = errCrossOrigin + " " + errFeature
	write := func(more map[int]string) map[int]string {
		errs := writeErrors(more)
		errs[http.StatusForbidden] = forbidden
		return errs
	}
	deleted := func(notFound string) map[int]string {
		return map[int]string{
			http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: forbidden, http.StatusNotFound: notFound,
			http.StatusInternalServerError: errInternal,
		}
	}

	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putChargeCost", Method: http.MethodPut, Path: "/vehicles/{vehicle}/charges/{id}/cost", Tags: []string{"costs"},
		Summary: "Enter the cost of a charge",
		Description: "What was paid for the charge, as on a receipt: it replaces the cost of the tariff, if any. Entered " +
			"again, it replaces the previous one. It is not derived data: a rebuild keeps it, and finds its charge again " +
			"by its detection time or else by its window; when it cannot, the cost is listed in `GET /charge-costs/orphans`. " +
			"Accepted without a currency, as a tariff is: the charge's `cost` stays `null` until the account's currency " +
			"is set, which gives the amount its unit.",
		Middlewares: jsonBody,
	}, "The charge, with its entered cost.", write(map[int]string{http.StatusNotFound: errCharge})), s.putChargeCost)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteChargeCost", Method: http.MethodDelete, Path: "/vehicles/{vehicle}/charges/{id}/cost", Tags: []string{"costs"},
		Summary: "Delete the entered cost of a charge", Description: "The charge's cost is that of its tariff again, if any.",
		DefaultStatus: http.StatusNoContent, Middlewares: costs,
	}, "Deleted.", deleted("`not_found`: no such vehicle in the account, no such charge for it, or the charge has no entered cost.")),
		s.deleteChargeCost)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listOrphanedCosts", Method: http.MethodGet, Path: "/charge-costs/orphans", Tags: []string{"costs"},
		Summary: "The entered costs no charge takes",
		Description: "After a rebuild of the derived data changed the charges, an entered cost whose charge is neither " +
			"at the same detection time nor the only one overlapping its window: never lost, never attached by guess. " +
			"Attach each to a charge, or delete it. They count in no cost total: the statistics show them apart. Newest " +
			"first, by `window.after`.",
		Middlewares: costs,
	}, "Every orphaned cost of the account.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errFeature, http.StatusInternalServerError: errInternal,
	}), s.listOrphans)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "attachOrphanedCost", Method: http.MethodPut, Path: "/charge-costs/orphans/{id}/charge", Tags: []string{"costs"},
		Summary: "Attach an entered cost to a charge",
		Description: "The charge, of any vehicle of the account, takes the cost: it gets the charge's detection time and " +
			"window. An entered cost that a charge takes can be moved too.",
		Middlewares: jsonBody,
	}, "The charge, with the cost.", write(map[int]string{
		http.StatusNotFound: errOrphan,
		http.StatusConflict: "`charge_has_cost`: the charge already takes another entered cost; delete that one first.",
	})), s.attachOrphan)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteOrphanedCost", Method: http.MethodDelete, Path: "/charge-costs/orphans/{id}", Tags: []string{"costs"},
		Summary: "Delete an entered cost", Description: "An orphaned one most often; one that a charge takes is deleted too.",
		DefaultStatus: http.StatusNoContent, Middlewares: costs,
	}, "Deleted.", deleted("`not_found`: no such entered cost in the account.")), s.deleteOrphan)
}
