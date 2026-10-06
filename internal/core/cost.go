package core

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// CostParams are the assumptions of the cost of a charge. The vehicle reports no energy
// meter: the cost is billed on the grid side, the energy is known on the battery side
// (ΔSoC × capacity), and a charging efficiency bridges the two.
type CostParams struct {
	// EfficiencyAC is the share of the grid energy that reaches the battery on an AC
	// charger. Assumption: 0.88, from ADAC's 2026 wallbox measurements at 11 kW (0.93 to
	// 0.95 from 10 to 90 %), Green NCAP's EX30 (0.91) and ADAC Ecotest's EX30 (0.86) and
	// XC40 (0.83) from 0 to 100 %. A domestic socket loses more: a place can override it.
	EfficiencyAC float64
	// EfficiencyDC is the share of the energy metered by a DC charger that reaches the
	// battery. Assumption: 0.95, from ADAC's 2025 measurements (1 to 4 % lost with a warm
	// battery, 6 to 10 % with a cold one).
	EfficiencyDC float64
}

// DefaultCostParams returns the assumptions used without explicit configuration.
func DefaultCostParams() CostParams {
	return CostParams{EfficiencyAC: 0.88, EfficiencyDC: 0.95}
}

// Currency is an ISO 4217 currency, with its number of minor digits.
type Currency struct {
	Code        string
	MinorDigits int
}

// currencies are those accepted: their ISO 4217 minor digits are those that browsers
// format (CLDR), so that an amount reads the same on both sides.
var currencies = []Currency{
	{"CHF", 2},
	{"CZK", 2},
	{"DKK", 2},
	{"EUR", 2},
	{"GBP", 2},
	{"ISK", 0},
	{"NOK", 2},
	{"PLN", 2},
	{"SEK", 2},
	{"USD", 2},
}

// Currencies returns the accepted currencies, sorted by code.
func Currencies() []Currency { return slices.Clone(currencies) }

// CurrencyOf returns the accepted currency of code.
func CurrencyOf(code string) (Currency, bool) {
	i := slices.IndexFunc(currencies, func(c Currency) bool { return c.Code == code })
	if i < 0 {
		return Currency{}, false
	}
	return currencies[i], true
}

// Place is a named circle, with the tariff of the charges it holds.
type Place struct {
	ID, Name string
	Position Position
	RadiusM  float64
	Location *time.Location // zone of the tariff's days and windows; UTC if nil
	// WithoutPosition makes the place take the AC charges and those of unknown type that
	// have no position.
	WithoutPosition bool
	// MaxPowerKW is the charger's power, which bounds the energy of each price segment.
	MaxPowerKW Value[float64]
	// Efficiency replaces the default of CostParams for the charges of the place.
	Efficiency Value[float64]
	Tariff     []TariffVersion
}

// Prices is the price schedule of the place.
func (p Place) Prices() PriceSchedule { return Tariff{Location: p.Location, Versions: p.Tariff} }

// known is a present value, read at an unknown time.
func known[T any](v T) Value[T] { return Value[T]{V: v, OK: true} }

// isDC tells whether the charge is known to be DC.
func isDC(c Charge) bool { return c.Type.OK && c.Type.V == DC }

// earthRadiusM is the mean radius of the Earth (IUGG).
const earthRadiusM = 6_371_008.8

// distanceM is the great-circle distance between a and b (haversine).
func distanceM(a, b Position) float64 {
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	dLat, dLon := rad(b.Lat-a.Lat), rad(b.Lon-a.Lon)
	h := math.Pow(math.Sin(dLat/2), 2) + math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Pow(math.Sin(dLon/2), 2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// PlaceOf is the place of a charge: the nearest one whose circle holds its position, its
// edge included. A charge without a position takes the place marked WithoutPosition,
// unless it is DC: a DC charge is never at home by default. The API allows one such
// place; given several, and between places at the same distance, the first in places
// wins, so callers pass them in a stable order.
func PlaceOf(c Charge, places []Place) (Place, bool) {
	i := placeIndex(c, places)
	if i < 0 {
		return Place{}, false
	}
	return places[i], true
}

// placeIndex is the index of PlaceOf's place in places, or -1.
func placeIndex(c Charge, places []Place) int {
	if !c.Position.OK {
		if isDC(c) {
			return -1
		}
		return slices.IndexFunc(places, func(p Place) bool { return p.WithoutPosition })
	}
	return placeAtIndex(c.Position.V, places)
}

// PlaceAt is the place of a position: the nearest one whose circle holds it, its edge
// included; between places at the same distance, the first in places.
func PlaceAt(pos Position, places []Place) (Place, bool) {
	i := placeAtIndex(pos, places)
	if i < 0 {
		return Place{}, false
	}
	return places[i], true
}

func placeAtIndex(pos Position, places []Place) int {
	best := -1
	var bestM float64
	for i, p := range places {
		if d := distanceM(pos, p.Position); d <= p.RadiusM && (best < 0 || d < bestM) {
			best, bestM = i, d
		}
	}
	return best
}

// EnteredCost is the cost of a charge entered by the user. It is not derived: it names
// its charge by DetectedAt, and keeps the charge's window [Start.After, End.Before] at
// the time it was entered, to find the charge again should a rebuild move DetectedAt.
type EnteredCost struct {
	ChargeDetectedAt          time.Time
	WindowAfter, WindowBefore time.Time
	AmountMinor               int64          // what was paid, in minor units; 0 is free
	EnergyKWh                 Value[float64] // billed energy, from the receipt
	Note                      string
}

// CostSource tells where a cost comes from.
type CostSource string

// Cost sources.
const (
	CostTariff  CostSource = "tariff"
	CostEntered CostSource = "entered"
)

// Cost is the cost of a charge: every amount its bounds allow lies in [Min, Max], in
// minor units of Currency. An entered cost has Min = Max.
type Cost struct {
	Currency Currency
	Min, Max int64
	Source   CostSource
	Place    Value[Place] // the place of the charge
	// EnergyKWh is the billed energy: estimated from the tariff, as read on the receipt
	// for an entered cost (unknown if not given).
	EnergyKWh Value[float64]
	// Efficiency is the one applied to estimate EnergyKWh; unknown for an entered cost.
	Efficiency Value[float64]
	Note       string
}

// Pricing is what the costs of a vehicle's charges depend on, besides the charges and
// the entered costs.
type Pricing struct {
	Currency Value[Currency] // without it, no cost is known
	Places   []Place
	// OnboardChargerKW is the vehicle's AC charging power, which bounds the AC charges
	// with the place's: a Pricing is a vehicle's. Unknown without its variant.
	OnboardChargerKW Value[float64]
	Params           CostParams
}

// Costs are the costs of a list of charges: Charges[i] is the cost of the i-th charge,
// unknown when absent. Orphans are the entered costs that no charge takes.
type Costs struct {
	Charges []Value[Cost]
	Orphans []EnteredCost
}

// Costs attaches the entered costs to the charges, and gives the cost of each charge.
func (pr Pricing) Costs(charges []Charge, entered []EnteredCost) Costs {
	attached, orphans := AttachCosts(charges, entered)
	out := Costs{Charges: make([]Value[Cost], len(charges)), Orphans: orphans}
	for i, c := range charges {
		out.Charges[i] = pr.ChargeCost(c, attached[i])
	}
	return out
}

// minorEpsilon absorbs the float error of an amount in minor units before rounding: an
// exact 5.00 may come out as 499.9999999.
const minorEpsilon = 1e-6

// ChargeCost is the cost of a charge. An entered cost replaces the tariff's. Otherwise
// the billed energy is EnergySoCKWh over the efficiency, somewhere in [Start.After,
// End.Before]: the minimum fills the cheapest price segments first, the maximum the
// dearest, each segment holding at most the maximum power times its duration. Without a
// maximum power, or if the segments cannot hold the energy at that power (the power was
// underestimated), there is no such limit.
//
// A window of no duration, as a reconstructed charge between two readings at the same
// time may have, takes the price at that instant.
//
// The cost is unknown without a currency, without a place (and no entered cost),
// without EnergySoCKWh, or when a part of the window has no price.
func (pr Pricing) ChargeCost(c Charge, entered Value[EnteredCost]) Value[Cost] {
	if !pr.Currency.OK {
		return Value[Cost]{}
	}
	place, atPlace := PlaceOf(c, pr.Places)
	cost := Cost{Currency: pr.Currency.V}
	if atPlace {
		cost.Place = known(place)
	}
	if entered.OK {
		e := entered.V
		cost.Min, cost.Max, cost.Source = e.AmountMinor, e.AmountMinor, CostEntered
		cost.EnergyKWh, cost.Note = e.EnergyKWh, e.Note
		return known(cost)
	}
	if !atPlace || !c.EnergySoCKWh.OK {
		return Value[Cost]{}
	}
	eta := pr.efficiency(c, place)
	energy := c.EnergySoCKWh.V / eta

	from, to := span(c)
	segs := place.Prices().Segments(from, to)
	if !covers(segs, from, to) {
		return Value[Cost]{}
	}
	lo, hi := allocate(segs, energy, pr.maxPower(c, place))
	cost.Min, cost.Max = round(lo, hi, pr.Currency.V.MinorDigits)
	cost.Source, cost.EnergyKWh, cost.Efficiency = CostTariff, known(energy), known(eta)
	return known(cost)
}

// Unpriced tells whether the cost of a charge is unknown for want of a price only: it is
// at a place, has neither an entered cost nor an unknown energy, and part of its window
// has no price, before the first version of the tariff. It gives the place, whose tariff
// from an earlier day would give the charge a cost.
func (pr Pricing) Unpriced(c Charge, entered Value[EnteredCost]) (Place, bool) {
	if !pr.Currency.OK || entered.OK || !c.EnergySoCKWh.OK {
		return Place{}, false
	}
	place, ok := PlaceOf(c, pr.Places)
	if !ok {
		return Place{}, false
	}
	from, to := span(c)
	return place, !covers(place.Prices().Segments(from, to), from, to)
}

// span is the time a charge's energy went through: [Start.After, End.Before], or an
// instant for a window of no duration.
func span(c Charge) (from, to time.Time) {
	from, to = c.Start.After, c.End.Before
	if !to.After(from) {
		to = from.Add(time.Nanosecond)
	}
	return from, to
}

// efficiency is the place's, or the default of the charge's type. Assumption: a charge
// of unknown type (a reconstructed one) took place at home, on an AC charger.
func (pr Pricing) efficiency(c Charge, place Place) float64 {
	switch {
	case place.Efficiency.OK:
		return place.Efficiency.V
	case isDC(c):
		return pr.Params.EfficiencyDC
	default:
		return pr.Params.EfficiencyAC
	}
}

// Efficiency is the charging efficiency of a charge: the place's when it is at one that
// states one, else the default of its type. The billed capacity estimate rests on the
// same one a tariff's cost applies to that charge.
func (pr Pricing) Efficiency(c Charge) float64 {
	place, _ := PlaceOf(c, pr.Places)
	return pr.efficiency(c, place)
}

// maxPower is the lowest known of the place's power and, unless the charge is DC (which
// bypasses it), the vehicle's onboard charger.
func (pr Pricing) maxPower(c Charge, place Place) Value[float64] {
	p := place.MaxPowerKW
	if o := pr.OnboardChargerKW; o.OK && !isDC(c) && (!p.OK || o.V < p.V) {
		p = o
	}
	return p
}

// round gives [lo, hi] in minor units: lo rounded down and hi up, so that every amount
// the bounds allow lies within, as the durations. A single amount (one price over the
// window) is rounded to the nearest: its rounding is not an uncertainty of the time.
func round(lo, hi float64, digits int) (lower, upper int64) {
	scale := math.Pow10(digits)
	lo, hi = lo*scale, hi*scale
	if hi-lo < minorEpsilon {
		n := int64(math.Round(lo))
		return n, n
	}
	return int64(math.Floor(lo + minorEpsilon)), int64(math.Ceil(hi - minorEpsilon))
}

// covers tells whether segs cover [from, to) without a gap.
func covers(segs []PriceSegment, from, to time.Time) bool {
	at := from
	for _, s := range segs {
		if !s.From.Equal(at) {
			return false
		}
		at = s.To
	}
	return len(segs) > 0 && at.Equal(to)
}

// allocate gives the lowest and highest amounts of energy over segs. Filling the
// cheapest segments first is exact for the minimum: one total, a cap per segment.
func allocate(segs []PriceSegment, energy float64, powerKW Value[float64]) (lo, hi float64) {
	caps := make([]float64, len(segs))
	var total float64
	for i, s := range segs {
		caps[i] = powerKW.V * s.To.Sub(s.From).Hours()
		total += caps[i]
	}
	capped := powerKW.OK && total >= energy
	order := make([]int, len(segs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(segs[a].PricePerKWh, segs[b].PricePerKWh) })
	fill := func(order []int) float64 {
		left, amount := energy, 0.0
		for _, i := range order {
			if left <= 0 {
				break
			}
			e := left
			if capped {
				e = min(left, caps[i])
			}
			amount += e * segs[i].PricePerKWh
			left -= e
		}
		return amount
	}
	lo = fill(order)
	slices.Reverse(order)
	return lo, fill(order)
}

// AttachCosts finds the charge of each entered cost: the charge detected at the same
// time; otherwise the only charge whose window overlaps the one kept (a change of the
// detection moved DetectedAt), sharing more than an end. attached[i] is the entered cost
// of charges[i].
//
// A charge takes at most one entered cost. An exact match comes first, and a charge so
// taken is no candidate for an overlap. Two entered costs that point to the same charge
// otherwise are both orphaned, as are those that overlap no charge or several: the user
// settles them, nothing is attached by guess.
func AttachCosts(charges []Charge, entered []EnteredCost) (attached []Value[EnteredCost], orphans []EnteredCost) {
	attached = make([]Value[EnteredCost], len(charges))
	var pending []EnteredCost
	for _, e := range entered {
		i := slices.IndexFunc(charges, func(c Charge) bool { return c.DetectedAt.Equal(e.ChargeDetectedAt) })
		if i >= 0 && !attached[i].OK {
			attached[i] = known(e)
			continue
		}
		pending = append(pending, e)
	}

	target := make([]int, len(pending)) // the charge each pending cost overlaps alone, or -1
	claims := make([]int, len(charges))
	for j, e := range pending {
		target[j] = -1
		for i, c := range charges {
			overlaps := c.Start.After.Before(e.WindowBefore) && e.WindowAfter.Before(c.End.Before)
			if attached[i].OK || !overlaps {
				continue
			}
			if target[j] != -1 {
				target[j] = -2 // several
				break
			}
			target[j] = i
		}
		if target[j] >= 0 {
			claims[target[j]]++
		}
	}
	for j, e := range pending {
		if i := target[j]; i >= 0 && claims[i] == 1 {
			attached[i] = known(e)
		} else {
			orphans = append(orphans, e)
		}
	}
	return attached, orphans
}
