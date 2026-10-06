package core

import (
	"math"
	"reflect"
	"testing"
	"time"
)

var (
	eur  = known(Currency{"EUR", 2})
	lyon = Position{Lat: 45.764, Lon: 4.8357}
)

// home is the place of the example of the ADR: the French off-peak tariff in Lyon.
func homePlace() Place {
	return Place{
		ID: "home", Name: "Home", Position: lyon, RadiusM: 100, Location: paris,
		Tariff: []TariffVersion{hphc("2026-08-01")},
	}
}

// fixture is the charge of internal/api/testdata/charge.json: 26.4 kWh by the SoC, AC,
// from 20:20 to 23:56 in Paris at the widest.
func fixture() Charge {
	return Charge{
		DetectedAt: utc("2026-09-28T18:30:00Z"),
		Start:      Bounds{utc("2026-09-28T18:20:00Z"), utc("2026-09-28T18:30:00Z")},
		End:        Bounds{utc("2026-09-28T21:55:00Z"), utc("2026-09-28T21:56:00Z")},
		Type:       known(AC), EnergySoCKWh: val(26.4), Position: known(lyon),
	}
}

// with applies changes to a copy.
func with[T any](v T, f func(*T)) T {
	f(&v)
	return v
}

func TestChargeCost(t *testing.T) {
	pricing := Pricing{Currency: eur, Places: []Place{homePlace()}, Params: DefaultCostParams()}
	tariff := func(lo, hi int64, kwh, eta float64) Value[Cost] {
		return known(Cost{
			Currency: eur.V, Min: lo, Max: hi, Source: CostTariff, Place: known(homePlace()),
			EnergyKWh: val(kwh), Efficiency: val(eta),
		})
	}
	night := func(c *Charge) {
		c.Start = Bounds{pt("2026-09-28 22:10"), pt("2026-09-28 22:20")}
		c.End = Bounds{pt("2026-09-29 02:00"), pt("2026-09-29 02:10")}
	}
	for _, tt := range []struct {
		name    string
		pricing Pricing
		charge  Charge
		entered Value[EnteredCost]
		want    Value[Cost]
		// unpriced: the cost is unknown for want of a price only.
		unpriced bool
	}{
		{
			// 30 kWh billed, all at the off-peak price or all at the full one.
			name: "example of the ADR, no power", pricing: pricing, charge: fixture(),
			want: tariff(476, 643, 30, 0.88),
		},
		{
			// 100 min at 11 kW hold 18.33 kWh, 116 min 21.27: at least 8.73 kWh at the
			// full price (5.2499 €, rounded down), at most 18.33 (5.7808 €, up).
			name:    "example of the ADR, 11 kW",
			pricing: with(pricing, func(p *Pricing) { p.Places = []Place{with(homePlace(), func(p *Place) { p.MaxPowerKW = val(11) })} }),
			charge:  fixture(),
			want: with(tariff(524, 579, 30, 0.88), func(c *Value[Cost]) {
				c.V.Place = known(with(homePlace(), func(p *Place) { p.MaxPowerKW = val(11) }))
			}),
		},
		{
			// 1 kW cannot deliver 30 kWh in 3 h 36: the power was underestimated.
			name:    "power too low: no limit",
			pricing: with(pricing, func(p *Pricing) { p.Places = []Place{with(homePlace(), func(p *Place) { p.MaxPowerKW = val(1) })} }),
			charge:  fixture(),
			want: with(tariff(476, 643, 30, 0.88), func(c *Value[Cost]) {
				c.V.Place = known(with(homePlace(), func(p *Place) { p.MaxPowerKW = val(1) }))
			}),
		},
		{
			name:    "the vehicle's onboard charger bounds an AC charge",
			pricing: with(pricing, func(p *Pricing) { p.OnboardChargerKW = val(11) }),
			charge:  fixture(),
			want:    tariff(524, 579, 30, 0.88),
		},
		{
			name: "the lower of the place's and the vehicle's power",
			pricing: with(pricing, func(p *Pricing) {
				p.OnboardChargerKW = val(22)
				p.Places = []Place{with(homePlace(), func(p *Place) { p.MaxPowerKW = val(11) })}
			}),
			charge: fixture(),
			want: with(tariff(524, 579, 30, 0.88), func(c *Value[Cost]) {
				c.V.Place = known(with(homePlace(), func(p *Place) { p.MaxPowerKW = val(11) }))
			}),
		},
		{
			// DC bypasses the onboard charger; 26.6 kWh / 0.95 = 28 kWh.
			name:    "DC: its efficiency, no onboard charger",
			pricing: with(pricing, func(p *Pricing) { p.OnboardChargerKW = val(11) }),
			charge:  with(fixture(), func(c *Charge) { c.Type, c.EnergySoCKWh = known(DC), val(26.6) }),
			want:    tariff(444, 600, 28, 0.95),
		},
		{
			// A 22 kW wallbox, a car that takes 11: the car's charger bounds the power.
			name: "the onboard charger below the place's power",
			pricing: with(pricing, func(p *Pricing) {
				p.OnboardChargerKW = val(11)
				p.Places = []Place{with(homePlace(), func(p *Place) { p.MaxPowerKW = val(22) })}
			}),
			charge: fixture(),
			want: with(tariff(524, 579, 30, 0.88), func(c *Value[Cost]) {
				c.V.Place = known(with(homePlace(), func(p *Place) { p.MaxPowerKW = val(22) }))
			}),
		},
		{
			name:    "unknown type: the AC efficiency",
			pricing: pricing,
			charge:  with(fixture(), func(c *Charge) { c.Type = Value[ChargeType]{} }),
			want:    tariff(476, 643, 30, 0.88),
		},
		{
			// Assumption: a charge of unknown type took place on an AC charger.
			name:    "unknown type: bounded by the onboard charger",
			pricing: with(pricing, func(p *Pricing) { p.OnboardChargerKW = val(11) }),
			charge:  with(fixture(), func(c *Charge) { c.Type = Value[ChargeType]{} }),
			want:    tariff(524, 579, 30, 0.88),
		},
		{
			// A domestic socket: 26.4 / 0.8 = 33 kWh, even for a DC charge.
			name: "the place's efficiency",
			pricing: with(pricing, func(p *Pricing) {
				p.Places = []Place{with(homePlace(), func(p *Place) { p.Efficiency = val(0.8) })}
			}),
			charge: with(fixture(), func(c *Charge) { c.Type = known(DC) }),
			want: known(Cost{
				Currency: eur.V, Min: 524, Max: 707, Source: CostTariff, EnergyKWh: val(33), Efficiency: val(0.8),
				Place: known(with(homePlace(), func(p *Place) { p.Efficiency = val(0.8) })),
			}),
		},
		{
			// Scheduled at night: one price, 4.767 €, rounded to the nearest.
			name: "within one window: min = max", pricing: pricing, charge: with(fixture(), night),
			want: tariff(477, 477, 30, 0.88),
		},
		{
			// 8.8 / 0.88 × 0.25 × 100 is 250.00000000000006 in floats.
			name: "an exact amount stays exact",
			pricing: with(pricing, func(p *Pricing) {
				p.Places = []Place{with(homePlace(), func(p *Place) { p.Tariff = []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.25}} })}
			}),
			charge: with(fixture(), func(c *Charge) { c.EnergySoCKWh = val(8.8) }),
			want: known(Cost{
				Currency: eur.V, Min: 250, Max: 250, Source: CostTariff, EnergyKWh: val(8.8 / 0.88), Efficiency: val(0.88),
				Place: known(with(homePlace(), func(p *Place) { p.Tariff = []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.25}} })),
			}),
		},
		{
			// 30 kWh at 30.5 and 32 ISK: 915 to 960 krónur, no minor unit.
			name: "a currency without minor units",
			pricing: with(pricing, func(p *Pricing) {
				p.Currency = known(Currency{"ISK", 0})
				p.Places = []Place{with(homePlace(), func(p *Place) {
					p.Tariff = []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 32, Windows: []PriceWindow{window("22:00-06:00", 30.5)}}}
				})}
			}),
			charge: fixture(),
			want: known(Cost{
				Currency: Currency{"ISK", 0}, Min: 915, Max: 960, Source: CostTariff, EnergyKWh: val(30), Efficiency: val(0.88),
				Place: known(with(homePlace(), func(p *Place) {
					p.Tariff = []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 32, Windows: []PriceWindow{window("22:00-06:00", 30.5)}}}
				})),
			}),
		},
		{
			// A reconstructed charge between two readings at the same time: the price of
			// that instant, 30 kWh at 0.1589.
			name: "a window of no duration",
			pricing: with(pricing, func(p *Pricing) {
				p.Places = []Place{with(homePlace(), func(p *Place) { p.MaxPowerKW = val(11) })}
			}),
			charge: with(fixture(), func(c *Charge) {
				c.Reconstructed, c.Type = true, Value[ChargeType]{}
				c.Start = Bounds{pt("2026-09-28 23:00"), pt("2026-09-28 23:00")}
				c.End = c.Start
			}),
			want: with(tariff(477, 477, 30, 0.88), func(c *Value[Cost]) {
				c.V.Place = known(with(homePlace(), func(p *Place) { p.MaxPowerKW = val(11) }))
			}),
		},
		{
			name: "a new version in the middle",
			pricing: with(pricing, func(p *Pricing) {
				p.Places = []Place{with(homePlace(), func(p *Place) {
					p.Tariff = append(p.Tariff, TariffVersion{ValidFrom: date("2026-09-29"), PricePerKWh: 0.3})
				})}
			}),
			// 22:10 to 02:10: off-peak until midnight, then 0.30.
			charge: with(fixture(), night),
			want: known(Cost{
				Currency: eur.V, Min: 476, Max: 900, Source: CostTariff, EnergyKWh: val(30), Efficiency: val(0.88),
				Place: known(with(homePlace(), func(p *Place) {
					p.Tariff = append(p.Tariff, TariffVersion{ValidFrom: date("2026-09-29"), PricePerKWh: 0.3})
				})),
			}),
		},
		{
			name:    "entered: replaces the tariff",
			pricing: pricing, charge: fixture(),
			entered: known(EnteredCost{ChargeDetectedAt: fixture().DetectedAt, AmountMinor: 1240, EnergyKWh: val(31.2), Note: "receipt"}),
			want: known(Cost{
				Currency: eur.V, Min: 1240, Max: 1240, Source: CostEntered, Place: known(homePlace()), EnergyKWh: val(31.2), Note: "receipt",
			}),
		},
		{
			name:    "entered: free is a true zero, without a place",
			pricing: with(pricing, func(p *Pricing) { p.Places = nil }), charge: fixture(),
			entered: known(EnteredCost{ChargeDetectedAt: fixture().DetectedAt}),
			want:    known(Cost{Currency: eur.V, Source: CostEntered}),
		},
		{
			name:    "unknown: no currency",
			pricing: with(pricing, func(p *Pricing) { p.Currency = Value[Currency]{} }), charge: fixture(),
			entered: known(EnteredCost{ChargeDetectedAt: fixture().DetectedAt, AmountMinor: 1240}),
		},
		{name: "unknown: no place", pricing: with(pricing, func(p *Pricing) { p.Places = nil }), charge: fixture()},
		{
			name: "unknown: no energy", pricing: pricing,
			charge: with(fixture(), func(c *Charge) { c.EnergySoCKWh = Value[float64]{} }),
		},
		{
			name: "unknown: before the first version", pricing: pricing,
			charge: with(fixture(), func(c *Charge) {
				c.Start = Bounds{pt("2026-07-31 22:00"), pt("2026-07-31 22:10")}
				c.End = Bounds{pt("2026-08-01 02:00"), pt("2026-08-01 02:10")}
			}),
			unpriced: true,
		},
		{
			name: "unknown: no version",
			pricing: with(pricing, func(p *Pricing) {
				p.Places = []Place{with(homePlace(), func(p *Place) { p.Tariff = nil })}
			}),
			charge:   fixture(),
			unpriced: true,
		},
		{
			// An entered cost gives it, whatever the tariff.
			name: "entered: before the first version", pricing: pricing,
			charge: with(fixture(), func(c *Charge) {
				c.Start = Bounds{pt("2026-07-31 22:00"), pt("2026-07-31 22:10")}
				c.End = Bounds{pt("2026-08-01 02:00"), pt("2026-08-01 02:10")}
			}),
			entered: known(EnteredCost{AmountMinor: 800}),
			want:    known(Cost{Currency: eur.V, Min: 800, Max: 800, Source: CostEntered, Place: known(homePlace())}),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pricing.ChargeCost(tt.charge, tt.entered)
			if got.OK && tt.want.OK && math.Abs(got.V.EnergyKWh.V-tt.want.V.EnergyKWh.V) < 1e-9 {
				got.V.EnergyKWh = tt.want.V.EnergyKWh // float rounding of the division
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cost = %+v\nwant   %+v", got, tt.want)
			}
			place, unpriced := tt.pricing.Unpriced(tt.charge, tt.entered)
			if unpriced != tt.unpriced || unpriced && place.ID != "home" {
				t.Errorf("unpriced = %v at %q, want %v", unpriced, place.ID, tt.unpriced)
			}
		})
	}
}

// A tariff has no gap once its first version starts; other schedules (dynamic prices not
// yet published) will.
func TestCovers(t *testing.T) {
	h := func(n int) time.Time { return pt("2026-06-01 00:00").Add(time.Duration(n) * time.Hour) }
	for _, tt := range []struct {
		name string
		segs []PriceSegment
		want bool
	}{
		{"whole", []PriceSegment{{h(0), h(1), 1}, {h(1), h(3), 2}}, true},
		{"none", nil, false},
		{"late start", []PriceSegment{{h(1), h(3), 1}}, false},
		{"early end", []PriceSegment{{h(0), h(2), 1}}, false},
		{"a gap in the middle", []PriceSegment{{h(0), h(1), 1}, {h(2), h(3), 2}}, false},
	} {
		if got := covers(tt.segs, h(0), h(3)); got != tt.want {
			t.Errorf("%s: covers = %v", tt.name, got)
		}
	}
}

// A charge is the nearest place's whose circle holds it; without a position, the place
// marked for it, unless the charge is DC.
func TestPlaceOf(t *testing.T) {
	// 0.001° of latitude is about 111 m.
	north := func(p Position, deg float64) Position { return Position{p.Lat + deg, p.Lon} }
	edge := distanceM(lyon, north(lyon, 0.001))
	a := Place{ID: "a", Position: lyon, RadiusM: 200}
	b := Place{ID: "b", Position: north(lyon, 0.001), RadiusM: 200}
	c := Place{ID: "c", Position: north(lyon, 0.001), RadiusM: 200, WithoutPosition: true}
	d := Place{ID: "d", Position: lyon, RadiusM: 100, WithoutPosition: true}
	nowhere := Value[Position]{}
	for _, tt := range []struct {
		name     string
		position Value[Position]
		typ      Value[ChargeType]
		places   []Place
		want     string // place ID, "" for none
	}{
		{"nearest of two", known(north(lyon, 0.0004)), known(AC), []Place{a, b}, "a"},
		{"nearest of two, the other", known(north(lyon, 0.0006)), known(AC), []Place{a, b}, "b"},
		{"same distance: the first", known(north(lyon, 0.0005)), known(AC), []Place{b, a}, "b"},
		{"DC at a place", known(lyon), known(DC), []Place{a}, "a"},
		{"on the edge", known(north(lyon, 0.001)), known(AC), []Place{{ID: "e", Position: lyon, RadiusM: edge}}, "e"},
		{"just beyond", known(north(lyon, 0.001)), known(AC), []Place{{ID: "e", Position: lyon, RadiusM: edge - 0.01}}, ""},
		{"out of every place", known(north(lyon, 0.01)), known(AC), []Place{a, b}, ""},
		{"without position, AC", nowhere, known(AC), []Place{a, c}, "c"},
		{"without position, unknown type", nowhere, Value[ChargeType]{}, []Place{a, c}, "c"},
		{"without position, DC: never", nowhere, known(DC), []Place{a, c}, ""},
		{"without position, no place for it", nowhere, known(AC), []Place{a, b}, ""},
		{"without position, two places for it: the first", nowhere, known(AC), []Place{d, c}, "d"},
		// The mark only matters without a position.
		{"with a position, the marked place is a place", known(north(lyon, 0.0006)), known(AC), []Place{a, c}, "c"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PlaceOf(Charge{Position: tt.position, Type: tt.typ}, tt.places)
			if ok != (tt.want != "") || got.ID != tt.want {
				t.Errorf("place = %q (%v), want %q", got.ID, ok, tt.want)
			}
		})
	}
}

func TestDistance(t *testing.T) {
	paris := Position{Lat: 48.8566, Lon: 2.3522}
	if d := distanceM(paris, lyon); math.Abs(d-391_500) > 1_000 {
		t.Errorf("Paris to Lyon = %.0f m, want about 391.5 km", d)
	}
	if d := distanceM(lyon, lyon); d != 0 {
		t.Errorf("distance to itself = %g", d)
	}
}

func TestAttachCosts(t *testing.T) {
	h := func(n int) time.Time { return pt("2026-06-01 00:00").Add(time.Duration(n) * time.Hour) }
	// charge i spans hours [3i, 3i+2]; entered costs keep a window.
	charge := func(i int) Charge {
		return Charge{DetectedAt: h(3 * i), Start: Bounds{h(3 * i), h(3 * i)}, End: Bounds{h(3*i + 2), h(3*i + 2)}}
	}
	entry := func(detected time.Time, after, before time.Time, amount int64) EnteredCost {
		return EnteredCost{ChargeDetectedAt: detected, WindowAfter: after, WindowBefore: before, AmountMinor: amount}
	}
	charges := []Charge{charge(0), charge(1), charge(2)}
	moved := h(100) // a DetectedAt no charge has any more
	for _, tt := range []struct {
		name        string
		entered     []EnteredCost
		want        []int64 // amount attached to each charge, -1 for none
		wantOrphans []int64
	}{
		{name: "none", want: []int64{-1, -1, -1}},
		{
			name:    "same detection time",
			entered: []EnteredCost{entry(h(3), h(50), h(51), 100)}, // the window does not matter
			want:    []int64{-1, 100, -1},
		},
		{
			name:    "the only charge the window overlaps",
			entered: []EnteredCost{entry(moved, h(4), h(5), 200)},
			want:    []int64{-1, 200, -1},
		},
		{
			// Charge 0 ends at 2 h, charge 1 starts at 3 h: a window from 2 to 3 touches
			// both and overlaps none.
			name:        "touching is not overlapping",
			entered:     []EnteredCost{entry(moved, h(2), h(3), 300)},
			want:        []int64{-1, -1, -1},
			wantOrphans: []int64{300},
		},
		{
			name:        "no overlap: orphaned",
			entered:     []EnteredCost{entry(moved, h(20), h(21), 400)},
			want:        []int64{-1, -1, -1},
			wantOrphans: []int64{400},
		},
		{
			name:        "several overlaps: orphaned",
			entered:     []EnteredCost{entry(moved, h(1), h(4), 500)},
			want:        []int64{-1, -1, -1},
			wantOrphans: []int64{500},
		},
		{
			name:        "two by overlap for one charge: both orphaned",
			entered:     []EnteredCost{entry(moved, h(3), h(4), 600), entry(moved.Add(time.Hour), h(4), h(5), 700)},
			want:        []int64{-1, -1, -1},
			wantOrphans: []int64{600, 700},
		},
		{
			// The charge taken by its detection time is no candidate: the other entered
			// cost overlaps charge 2 alone.
			name:    "exact match first, then overlap among the others",
			entered: []EnteredCost{entry(moved, h(4), h(7), 800), entry(h(3), h(3), h(5), 900)},
			want:    []int64{-1, 900, 800},
		},
		{
			name:        "exact match first, the overlapping one orphaned",
			entered:     []EnteredCost{entry(moved, h(4), h(5), 800), entry(h(3), h(3), h(5), 900)},
			want:        []int64{-1, 900, -1},
			wantOrphans: []int64{800},
		},
		{
			// Impossible in the store, whose key is the detection time.
			name:        "the same detection time twice: the first",
			entered:     []EnteredCost{entry(h(0), h(0), h(2), 1), entry(h(0), h(0), h(2), 2)},
			want:        []int64{1, -1, -1},
			wantOrphans: []int64{2},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			attached, orphans := AttachCosts(charges, tt.entered)
			got := make([]int64, len(attached))
			for i, a := range attached {
				got[i] = -1
				if a.OK {
					got[i] = a.V.AmountMinor
				}
			}
			var gotOrphans []int64
			for _, o := range orphans {
				gotOrphans = append(gotOrphans, o.AmountMinor)
			}
			if !reflect.DeepEqual(got, tt.want) || !reflect.DeepEqual(gotOrphans, tt.wantOrphans) {
				t.Errorf("attached %v, orphans %v; want %v and %v", got, gotOrphans, tt.want, tt.wantOrphans)
			}
		})
	}
}

func TestPricingCosts(t *testing.T) {
	pricing := Pricing{Currency: eur, Places: []Place{homePlace()}, Params: DefaultCostParams()}
	c0 := fixture()
	c1 := with(fixture(), func(c *Charge) {
		c.DetectedAt = c.DetectedAt.Add(24 * time.Hour)
		c.Start.After, c.End.Before = c.Start.After.Add(24*time.Hour), c.End.Before.Add(24*time.Hour)
	})
	orphan := EnteredCost{ChargeDetectedAt: c0.DetectedAt.Add(time.Minute), WindowAfter: pt("2026-01-01 10:00"), WindowBefore: pt("2026-01-01 11:00"), AmountMinor: 5}
	got := pricing.Costs([]Charge{c0, c1}, []EnteredCost{{ChargeDetectedAt: c1.DetectedAt, AmountMinor: 1000}, orphan})
	if len(got.Charges) != 2 || got.Charges[0].V.Source != CostTariff || got.Charges[0].V.Min != 476 ||
		got.Charges[1].V.Source != CostEntered || got.Charges[1].V.Min != 1000 {
		t.Errorf("costs = %+v", got.Charges)
	}
	if !reflect.DeepEqual(got.Orphans, []EnteredCost{orphan}) {
		t.Errorf("orphans = %+v", got.Orphans)
	}
}

func TestCurrencies(t *testing.T) {
	for _, code := range []string{"EUR", "GBP", "SEK", "NOK", "DKK", "CHF", "PLN", "CZK", "USD"} {
		if c, ok := CurrencyOf(code); !ok || c.MinorDigits != 2 {
			t.Errorf("%s = %+v, %v", code, c, ok)
		}
	}
	if c, ok := CurrencyOf("ISK"); !ok || c.MinorDigits != 0 {
		t.Errorf("ISK = %+v, %v", c, ok)
	}
	if _, ok := CurrencyOf("XXX"); ok {
		t.Error("XXX accepted")
	}
	all := Currencies()
	for i := 1; i < len(all); i++ {
		if all[i-1].Code >= all[i].Code {
			t.Errorf("not sorted: %s before %s", all[i-1].Code, all[i].Code)
		}
	}
	all[0].Code = "changed"
	if Currencies()[0].Code == "changed" {
		t.Error("Currencies returns the list itself")
	}
}
