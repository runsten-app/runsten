package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// Settings reads and writes the account's currency and places. Every method is
// restricted to the account: another account's place is not found.
type Settings interface {
	Currency(ctx context.Context, accountID string) (core.Value[core.Currency], error)
	// SetCurrency fails with ErrCurrencyInUse when it would change the currency while a
	// tariff or an entered cost exists.
	SetCurrency(ctx context.Context, accountID string, c core.Currency) error
	// Places are in the order they were created.
	Places(ctx context.Context, accountID string) ([]core.Place, error)
	Place(ctx context.Context, accountID, placeID string) (core.Place, bool, error)
	// CreatePlace and ReplacePlace fail with ErrWithoutPositionTaken when another place
	// takes the charges without a position; CreatePlace with ErrTooManyPlaces when the
	// account has MaxPlaces already; ReplacePlace and DeletePlace with ErrNotFound.
	CreatePlace(ctx context.Context, accountID string, p core.Place) (string, error)
	ReplacePlace(ctx context.Context, accountID string, p core.Place) error
	DeletePlace(ctx context.Context, accountID, placeID string) error
}

// The limits of a place, given to the front end by GET /settings (limitsJSON). huma's
// tags cannot name a constant: TestLimitsMatchTheSpec checks that the schema's bounds
// are these.
//
// Two kinds: rules of the domain (a radius under GPS precision misses the charges at
// home; the decimals are the store's precision), and safety caps, far above any real
// use, that bound what one account may make every read compute or the database keep
// (the hosted offer takes the writes of many accounts). A cap is no statement about
// the domain: raise it when a real use meets it.
const (
	maxPlaceNameChars = 60 // cap
	// minRadiusM is about the precision of a parked car's position.
	minRadiusM = 20
	maxRadiusM = 1000 // cap
	// defaultRadiusM is the radius a new place is offered, as its example: the API
	// requires one.
	defaultRadiusM    = 100
	maxPowerKW        = 400 // cap: about the most powerful DC chargers today
	maxTariffVersions = 50  // cap: 25 years of prices changing twice a year
	maxPriceWindows   = 24  // cap: a real tariff has one to four
	// maxPricePerKWh is a cap in any accepted currency: a kWh costs about 0.3 in euros,
	// 30 in Icelandic krónur, 300 in Korean won; 10 000 leaves room for the currencies
	// with the smallest units, within the store's numeric(10,5).
	maxPricePerKWh = 10000
	// maxPriceDecimals is the precision of a price per kWh: the store's numeric(10,5).
	// The prices of the windows, kept in JSON, would not be rounded: the same rule for
	// both.
	maxPriceDecimals = 5
)

// currencyCodeJSON is an accepted ISO 4217 code.
type currencyCodeJSON string

// Schema lists the accepted codes.
func (currencyCodeJSON) Schema(huma.Registry) *huma.Schema {
	var codes []currencyCodeJSON
	for _, c := range core.Currencies() {
		codes = append(codes, currencyCodeJSON(c.Code))
	}
	return enumSchema(codes...)
}

type currencyJSON struct {
	Code        currencyCodeJSON `json:"code" doc:"ISO 4217."`
	MinorDigits int              `json:"minor_digits" minimum:"0" doc:"The digits of the minor unit: amounts in _minor fields are integers of it (cents for EUR, 2)."`
}

type radiusLimitsJSON struct {
	Min     float64 `json:"min" doc:"The smallest radius accepted, in metres."`
	Max     float64 `json:"max" doc:"The largest radius accepted, in metres."`
	Default float64 `json:"default" doc:"The radius a new place is offered, in metres: the API has no default, a place always has its own."`
}

type priceLimitsJSON struct {
	Max      float64 `json:"max" doc:"The highest price per kWh accepted, in major units of the currency; the lowest is 0."`
	Decimals int     `json:"decimals" minimum:"0" doc:"The decimals a price per kWh may have, at most."`
}

type enteredCostLimitsJSON struct {
	AmountMinorMax int64   `json:"amount_minor_max" minimum:"0" doc:"The highest amount_minor accepted; the lowest is 0."`
	EnergyKWhMax   float64 `json:"energy_kwh_max" doc:"The highest energy_kwh accepted; it must be above 0."`
	NoteChars      int     `json:"note_chars" minimum:"0" doc:"The characters of a note, at most."`
}

// limitsJSON are the limits the writes are checked against, the same values: a form may
// tell what is wrong before sending. A single bound of a field is named after the field
// and the bound (max_power_kw_max); several bounds of a field are an object named after
// it (radius_m).
type limitsJSON struct {
	Places            int                   `json:"places" minimum:"0" doc:"The places of an account, at most: a safety cap, far above use."`
	PlaceNameChars    int                   `json:"place_name_chars" minimum:"0" doc:"The characters of a place's name, at most; it has at least one."`
	RadiusM           radiusLimitsJSON      `json:"radius_m" doc:"The bounds of a place's radius_m, included."`
	MaxPowerKWMax     float64               `json:"max_power_kw_max" doc:"The highest max_power_kw of a place accepted; it must be above 0."`
	VersionsPerPlace  int                   `json:"versions_per_place" minimum:"0" doc:"The versions of a place's tariff, at most; it has at least one."`
	WindowsPerVersion int                   `json:"windows_per_version" minimum:"0" doc:"The windows of a version of a tariff, at most."`
	PricePerKWh       priceLimitsJSON       `json:"price_per_kwh" doc:"The bounds of a price per kWh, of a version or a window."`
	EnteredCost       enteredCostLimitsJSON `json:"entered_cost" doc:"The bounds of the body of PUT /vehicles/{vehicle}/charges/{id}/cost."`
}

type defaultEfficiencyJSON struct {
	AC float64 `json:"ac" doc:"For the AC charges, and those of unknown type."`
	DC float64 `json:"dc" doc:"For the DC charges."`
}

type settingsJSON struct {
	Currency          null[currencyJSON]    `json:"currency" doc:"The account's currency: every amount and price is in it, without conversion. null until it is set: no cost is computed without it."`
	Currencies        []currencyJSON        `json:"currencies" nullable:"false" doc:"The accepted currencies, by code."`
	Limits            limitsJSON            `json:"limits" doc:"The limits of the places, their tariffs and the entered costs: those the writes are checked against. Besides the rules of the domain (a radius of at least 20 m, 5 decimals), they are safety caps far above any real use, which bound what an account may make each read compute."`
	DefaultEfficiency defaultEfficiencyJSON `json:"default_efficiency" doc:"The share of the energy drawn from the grid that reaches the battery, assumed for the cost of a charge at a place without its own efficiency."`
}

// limits are those of the writes: the constants the schema's tags repeat.
var limits = limitsJSON{
	Places: MaxPlaces, PlaceNameChars: maxPlaceNameChars,
	RadiusM:       radiusLimitsJSON{Min: minRadiusM, Max: maxRadiusM, Default: defaultRadiusM},
	MaxPowerKWMax: maxPowerKW, VersionsPerPlace: maxTariffVersions, WindowsPerVersion: maxPriceWindows,
	PricePerKWh: priceLimitsJSON{Max: maxPricePerKWh, Decimals: maxPriceDecimals},
	EnteredCost: enteredCostLimitsJSON{AmountMinorMax: maxAmountMinor, EnergyKWhMax: maxEnergyKWh, NoteChars: maxNoteChars},
}

type settingsUpdateJSON struct {
	Currency currencyCodeJSON `json:"currency" doc:"The account's currency. Once a tariff or an entered cost exists, it can no longer change: their amounts have no unit of their own. Setting the same one again is accepted."`
}

type settingsInput struct {
	Body settingsUpdateJSON
}

func currencyOf(c core.Currency) currencyJSON {
	return currencyJSON{Code: currencyCodeJSON(c.Code), MinorDigits: c.MinorDigits}
}

func (s *Server) settingsOf(c core.Value[core.Currency]) settingsJSON {
	out := settingsJSON{
		Currency: nullOf(c, currencyOf), Currencies: []currencyJSON{}, Limits: limits,
		DefaultEfficiency: defaultEfficiencyJSON{AC: s.CostParams.EfficiencyAC, DC: s.CostParams.EfficiencyDC},
	}
	for _, c := range core.Currencies() {
		out.Currencies = append(out.Currencies, currencyOf(c))
	}
	return out
}

// weekdayJSON is a day of the week, by name: no convention of the first day to agree on.
type weekdayJSON string

var weekdays = [...]weekdayJSON{"sun", "mon", "tue", "wed", "thu", "fri", "sat"} // by time.Weekday

// Schema lists the days, from Monday.
func (weekdayJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(weekdays[1], weekdays[2], weekdays[3], weekdays[4], weekdays[5], weekdays[6], weekdays[0])
}

// monthJSON is a month of the year, 1 for January.
type monthJSON int

// Schema bounds the months.
func (monthJSON) Schema(huma.Registry) *huma.Schema {
	first, last := 1.0, 12.0
	return &huma.Schema{Type: huma.TypeInteger, Minimum: &first, Maximum: &last}
}

type priceWindowJSON struct {
	Days        []weekdayJSON `json:"days" minItems:"1" uniqueItems:"true" nullable:"false" doc:"The days the window opens on: a window across midnight belongs to the day it starts (mon 22:00–06:00 covers the night from Monday to Tuesday)."`
	From        string        `json:"from" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"Local time, in the place's time zone, from which the window applies." example:"22:00"`
	To          string        `json:"to" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"Local time at which the window ends, excluded. Before from, the window crosses midnight; equal to from, it lasts 24 hours from from (00:00–00:00 on sat and sun: a weekend price)." example:"06:00"`
	Months      []monthJSON   `json:"months" uniqueItems:"true" nullable:"false" doc:"The months of the instant, 1 to 12, in which the window applies; [] for every month. A season changes at midnight: a window across midnight on the last day of a month is split there."`
	PricePerKWh float64       `json:"price_per_kwh" minimum:"0" maximum:"10000" doc:"In major units of the account's currency per kWh, taxes included, with at most 5 decimals." example:"0.1589"`
}

type tariffVersionJSON struct {
	ValidFrom   string            `json:"valid_from" format:"date" doc:"The day from which the version applies, from midnight in the place's time zone, until the next version. A new price is a new version: the costs of past charges stay those of their version." example:"2026-08-01"`
	PricePerKWh float64           `json:"price_per_kwh" minimum:"0" maximum:"10000" doc:"The price outside every window, in major units of the account's currency per kWh, taxes included, with at most 5 decimals." example:"0.2142"`
	Windows     []priceWindowJSON `json:"windows" maxItems:"24" nullable:"false" doc:"Times of the week with their own price, tried in order: the last window that holds an instant gives its price. [] for a single price."`
}

// PlaceFields are the fields of a place but its ID: the body of a write. Exported, so
// that huma reads the fields of placeJSON, which embeds it.
type PlaceFields struct {
	Name            string              `json:"name" minLength:"1" maxLength:"60" doc:"Shown to the user only: never in the logs nor the errors."`
	Position        positionJSON        `json:"position" doc:"The center of the place."`
	RadiusM         float64             `json:"radius_m" minimum:"20" maximum:"1000" doc:"A charge, or the start or end of a trip, whose position is within this distance, in metres, is at the place; the nearest place wins." example:"100"`
	TimeZone        string              `json:"time_zone" minLength:"1" doc:"The IANA time zone of the tariff's days and windows." example:"Europe/Paris"`
	WithoutPosition bool                `json:"without_position" doc:"The place also takes the AC charges, and those of unknown type, that have no position. At most one place of the account."`
	MaxPowerKW      *float64            `json:"max_power_kw" exclusiveMinimum:"0" maximum:"400" doc:"The power of the place's charger: it narrows the cost of a charge across several prices. null when unknown."`
	Efficiency      *float64            `json:"efficiency" exclusiveMinimum:"0" maximum:"1" doc:"The share of the energy drawn from the grid that reaches the battery, in place of the default of the settings (default_efficiency). null for the default."`
	Tariff          []tariffVersionJSON `json:"tariff" minItems:"1" maxItems:"50" nullable:"false" doc:"The versions of the tariff, one per valid_from, oldest first in a response. At least one: a place gives a price to its charges."`
}

type placeJSON struct {
	ID string `json:"id" format:"uuid"`
	PlaceFields
}

type unpricedChargesJSON struct {
	Charges  int     `json:"charges" minimum:"0" doc:"The charges the place takes whose cost is unknown for want of a price only: part of their window lies before the first version of the tariff. A charge with an entered cost, or without energy_soc_kwh, is not counted: a price would not change it. 0 without the account's currency."`
	FirstDay *string `json:"first_day" format:"date" doc:"The day, in the place's time zone, when the earliest of them may have started (its start.after): a first version valid from this day gives every one of them a price. null when charges is 0."`
}

type placeListJSON struct {
	Items []placeJSON `json:"items" nullable:"false"`
}

type placeBody struct {
	Body PlaceFields
}

// placeInput is the {place} of the path. Like a vehicle, a malformed ID is not found.
type placeInput struct {
	Place string `path:"place" doc:"The place ID."`
}

type placeUpdate struct {
	Place string `path:"place" doc:"The place ID."`
	Body  PlaceFields
}

func clockOf(minutes int) string { return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60) }

// minutesOf reads a time of clockPattern.
func minutesOf(s string) int {
	h, m, _ := strings.Cut(s, ":")
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	return hh*60 + mm
}

func placeOf(p core.Place) placeJSON {
	loc := p.Location
	if loc == nil {
		loc = time.UTC
	}
	out := placeJSON{ID: p.ID, PlaceFields: PlaceFields{
		Name: p.Name, Position: positionOf(p.Position), RadiusM: p.RadiusM, TimeZone: loc.String(),
		WithoutPosition: p.WithoutPosition, MaxPowerKW: opt(p.MaxPowerKW, same), Efficiency: opt(p.Efficiency, same),
		Tariff: []tariffVersionJSON{},
	}}
	versions := slices.SortedFunc(slices.Values(p.Tariff), func(a, b core.TariffVersion) int {
		return cmp.Or(cmp.Compare(a.ValidFrom.Year, b.ValidFrom.Year), cmp.Compare(a.ValidFrom.Month, b.ValidFrom.Month),
			cmp.Compare(a.ValidFrom.Day, b.ValidFrom.Day))
	})
	for _, v := range versions {
		j := tariffVersionJSON{
			ValidFrom:   fmt.Sprintf("%04d-%02d-%02d", v.ValidFrom.Year, v.ValidFrom.Month, v.ValidFrom.Day),
			PricePerKWh: v.PricePerKWh, Windows: []priceWindowJSON{},
		}
		for _, w := range v.Windows {
			jw := priceWindowJSON{From: clockOf(w.From), To: clockOf(w.To), PricePerKWh: w.PricePerKWh, Days: []weekdayJSON{}, Months: []monthJSON{}}
			// core reads no days as every day: the API always lists them.
			days := w.Days
			if len(days) == 0 {
				days = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}
			}
			for _, d := range days {
				jw.Days = append(jw.Days, weekdays[d])
			}
			for _, m := range w.Months {
				jw.Months = append(jw.Months, monthJSON(m))
			}
			j.Windows = append(j.Windows, jw)
		}
		out.Tariff = append(out.Tariff, j)
	}
	return out
}

// invalidBody is the 400 of a body huma accepted but the rules below refuse. The message
// names the field, never its value: a name or a position tells where the user lives.
func invalidBody(field, reason string) error {
	return apiError(http.StatusBadRequest, codeInvalidBody, "validation failed: body."+field+": "+reason)
}

// decimals counts the decimals of p as written: the shortest form that reads back as p,
// exact for the prices below maxPricePerKWh with 5 decimals (10 significant digits, within
// a float64's 15).
func decimals(p float64) int {
	_, frac, _ := strings.Cut(strconv.FormatFloat(p, 'f', -1, 64), ".")
	return len(frac)
}

var tooManyDecimals = fmt.Sprintf("more than %d decimals", maxPriceDecimals)

// corePlace checks what huma's schema does not, and converts the body.
func corePlace(id string, f PlaceFields) (core.Place, error) {
	loc, ok := loadZone(f.TimeZone)
	if !ok {
		return core.Place{}, invalidBody("time_zone", "unknown time zone")
	}
	p := core.Place{
		ID: id, Name: f.Name, Position: core.Position{Lat: f.Position.Lat, Lon: f.Position.Lon}, RadiusM: f.RadiusM,
		Location: loc, WithoutPosition: f.WithoutPosition,
	}
	if f.MaxPowerKW != nil {
		p.MaxPowerKW = core.Value[float64]{V: *f.MaxPowerKW, OK: true}
	}
	if f.Efficiency != nil {
		p.Efficiency = core.Value[float64]{V: *f.Efficiency, OK: true}
	}
	var dates []core.Date
	for i, v := range f.Tariff {
		at := fmt.Sprintf("tariff[%d]", i)
		d, err := time.Parse(time.DateOnly, v.ValidFrom)
		if err != nil { // huma checks the format: never
			return core.Place{}, invalidBody(at+".valid_from", "not a date")
		}
		date := core.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()}
		if slices.Contains(dates, date) {
			return core.Place{}, invalidBody(at+".valid_from", "the same day as another version")
		}
		dates = append(dates, date)
		if decimals(v.PricePerKWh) > maxPriceDecimals {
			return core.Place{}, invalidBody(at+".price_per_kwh", tooManyDecimals)
		}
		version := core.TariffVersion{ValidFrom: date, PricePerKWh: v.PricePerKWh}
		for j, w := range v.Windows {
			if decimals(w.PricePerKWh) > maxPriceDecimals {
				return core.Place{}, invalidBody(fmt.Sprintf("%s.windows[%d].price_per_kwh", at, j), tooManyDecimals)
			}
			cw := core.PriceWindow{From: minutesOf(w.From), To: minutesOf(w.To), PricePerKWh: w.PricePerKWh}
			for _, day := range w.Days {
				cw.Days = append(cw.Days, time.Weekday(slices.Index(weekdays[:], day)))
			}
			for _, m := range w.Months {
				cw.Months = append(cw.Months, time.Month(m))
			}
			version.Windows = append(version.Windows, cw)
		}
		p.Tariff = append(p.Tariff, version)
	}
	return p, nil
}

// settingsError maps the refusals of the store to the contract.
func (s *Server) settingsError(msg string, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return apiError(http.StatusNotFound, codeNotFound, "no such place")
	case errors.Is(err, ErrWithoutPositionTaken):
		return apiError(http.StatusConflict, codeWithoutPositionTaken, "another place already takes the charges without a position")
	case errors.Is(err, ErrCurrencyInUse):
		return apiError(http.StatusConflict, codeCurrencyInUse, "the currency cannot change while a tariff or an entered cost exists")
	case errors.Is(err, ErrTooManyPlaces):
		return apiError(http.StatusConflict, codeTooManyPlaces, fmt.Sprintf("the account already has %d places", MaxPlaces))
	}
	return s.internal(msg, err)
}

func (s *Server) getSettings(ctx context.Context, _ *struct{}) (*body[settingsJSON], error) {
	c, err := s.Settings.Currency(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, s.internal("currency not read", err)
	}
	return respond(s.settingsOf(c)), nil
}

func (s *Server) putSettings(ctx context.Context, in *settingsInput) (*body[settingsJSON], error) {
	c, ok := core.CurrencyOf(string(in.Body.Currency))
	if !ok { // huma checks the enumeration: never
		return nil, invalidBody("currency", "not an accepted currency")
	}
	if err := s.Settings.SetCurrency(ctx, sessionFrom(ctx).AccountID, c); err != nil {
		return nil, s.settingsError("currency not set", err)
	}
	return respond(s.settingsOf(core.Value[core.Currency]{V: c, OK: true})), nil
}

func (s *Server) listPlaces(ctx context.Context, _ *struct{}) (*body[placeListJSON], error) {
	ps, err := s.Settings.Places(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, s.internal("places not read", err)
	}
	out := placeListJSON{Items: []placeJSON{}}
	for _, p := range ps {
		out.Items = append(out.Items, placeOf(p))
	}
	return respond(out), nil
}

type placeCreated struct {
	Body placeJSON
}

func (s *Server) createPlace(ctx context.Context, in *placeBody) (*placeCreated, error) {
	p, err := corePlace("", in.Body)
	if err != nil {
		return nil, err
	}
	if p.ID, err = s.Settings.CreatePlace(ctx, sessionFrom(ctx).AccountID, p); err != nil {
		return nil, s.settingsError("place not created", err)
	}
	return &placeCreated{Body: placeOf(p)}, nil
}

func (s *Server) getPlace(ctx context.Context, in *placeInput) (*body[placeJSON], error) {
	if !uuidPattern.MatchString(in.Place) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such place")
	}
	p, found, err := s.Settings.Place(ctx, sessionFrom(ctx).AccountID, in.Place)
	switch {
	case err != nil:
		return nil, s.internal("place not read", err)
	case !found:
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such place")
	}
	return respond(placeOf(p)), nil
}

// getUnpriced counts the place's charges that a tariff from an earlier day would price,
// over the account's whole history: a new place's tariff starts today, after the
// charges that made the user create it.
func (s *Server) getUnpriced(ctx context.Context, in *placeInput) (*body[unpricedChargesJSON], error) {
	if !uuidPattern.MatchString(in.Place) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such place")
	}
	acc := sessionFrom(ctx).AccountID
	_, found, err := s.Settings.Place(ctx, acc, in.Place)
	switch {
	case err != nil:
		return nil, s.internal("place not read", err)
	case !found:
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such place")
	}
	history, err := s.Reader.ChargeHistory(ctx, acc)
	if err != nil {
		return nil, s.internal("charges not read", err)
	}
	var out unpricedChargesJSON
	var first time.Time
	var loc *time.Location
	for _, h := range history {
		// Without the vehicle's charger: whether a tariff covers a charge does not depend on
		// its power.
		pricing := core.Pricing{Currency: h.Currency, Places: h.Places, Params: s.CostParams}
		attached, _ := core.AttachCosts(h.Charges, h.Entered)
		for i, c := range h.Charges {
			place, ok := pricing.Unpriced(c, attached[i])
			if !ok || place.ID != in.Place {
				continue
			}
			out.Charges++
			if out.Charges == 1 || c.Start.After.Before(first) {
				first, loc = c.Start.After, place.Location
			}
		}
	}
	if out.Charges > 0 {
		if loc == nil {
			loc = time.UTC
		}
		day := first.In(loc).Format(time.DateOnly)
		out.FirstDay = &day
	}
	return respond(out), nil
}

func (s *Server) putPlace(ctx context.Context, in *placeUpdate) (*body[placeJSON], error) {
	if !uuidPattern.MatchString(in.Place) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such place")
	}
	p, err := corePlace(in.Place, in.Body)
	if err != nil {
		return nil, err
	}
	if err := s.Settings.ReplacePlace(ctx, sessionFrom(ctx).AccountID, p); err != nil {
		return nil, s.settingsError("place not replaced", err)
	}
	return respond(placeOf(p)), nil
}

func (s *Server) deletePlace(ctx context.Context, in *placeInput) (*struct{}, error) {
	if !uuidPattern.MatchString(in.Place) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such place")
	}
	if err := s.Settings.DeletePlace(ctx, sessionFrom(ctx).AccountID, in.Place); err != nil {
		return nil, s.settingsError("place not deleted", err)
	}
	return &struct{}{}, nil
}

// registerSettings registers the account's settings and places.
func (s *Server) registerSettings(api huma.API) {
	const (
		errPlace   = "`not_found`: no such place in the account. Another account's place is not found either."
		errWithout = "`without_position_taken`: another place already takes the charges without a position."
	)
	read := map[int]string{http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal}
	write := writeErrors
	jsonBody := huma.Middlewares{s.requireJSON}

	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getSettings", Method: http.MethodGet, Path: "/settings", Tags: []string{"settings"},
		Summary: "The account's settings",
	}, "The settings, and the accepted currencies.", read), s.getSettings)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putSettings", Method: http.MethodPut, Path: "/settings", Tags: []string{"settings"},
		Summary: "Set the account's currency", Middlewares: jsonBody,
	}, "The settings.", write(map[int]string{
		http.StatusConflict: "`currency_in_use`: another currency is set, and a tariff or an entered cost exists.",
	})), s.putSettings)

	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listPlaces", Method: http.MethodGet, Path: "/places", Tags: []string{"settings"},
		Summary:     "The account's places",
		Description: "In the order they were created, which decides between two places at the same distance of a charge.",
	}, "Every place of the account, with its tariff.", read), s.listPlaces)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "createPlace", Method: http.MethodPost, Path: "/places", Tags: []string{"settings"},
		Summary: "Create a place", Description: fmt.Sprintf("At most %d places per account.", MaxPlaces),
		DefaultStatus: http.StatusCreated, Middlewares: jsonBody,
	}, "The place, with its ID.", write(map[int]string{
		http.StatusConflict: errWithout + fmt.Sprintf(" `too_many_places`: the account already has %d places.", MaxPlaces),
	})), s.createPlace)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getPlace", Method: http.MethodGet, Path: "/places/{place}", Tags: []string{"settings"},
		Summary: "A place",
	}, "The place, with its tariff.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errPlace, http.StatusInternalServerError: errInternal,
	}), s.getPlace)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getUnpricedCharges", Method: http.MethodGet, Path: "/places/{place}/unpriced", Tags: []string{"settings"},
		Summary:     "The place's charges without a price",
		Description: "Those a tariff from an earlier day would give a cost: its versions apply from their day on, never before the first. Computed over the account's whole history, on each request.",
		Middlewares: huma.Middlewares{s.requireCosts},
	}, "The count of the charges, and the day from which a tariff would price them all.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errFeature, http.StatusNotFound: errPlace,
		http.StatusInternalServerError: errInternal,
	}), s.getUnpriced)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putPlace", Method: http.MethodPut, Path: "/places/{place}", Tags: []string{"settings"},
		Summary:     "Replace a place",
		Description: "The whole place, every version of its tariff included, replaced at once: a version left out is deleted. Correcting a version changes the costs of the past charges it covers.",
		Middlewares: jsonBody,
	}, "The place.", write(map[int]string{http.StatusNotFound: errPlace, http.StatusConflict: errWithout})), s.putPlace)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deletePlace", Method: http.MethodDelete, Path: "/places/{place}", Tags: []string{"settings"},
		Summary: "Delete a place", Description: "With its tariff: the costs of its charges become unknown.",
		DefaultStatus: http.StatusNoContent,
	}, "Deleted.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin, http.StatusNotFound: errPlace,
		http.StatusInternalServerError: errInternal,
	}), s.deletePlace)
}
