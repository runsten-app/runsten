package core

import (
	"reflect"
	"testing"
	"time"
)

func TestTripCosts(t *testing.T) {
	home := Place{ID: "home", Name: "Home", Position: lyon, RadiusM: 100}
	pricing := Pricing{Currency: eur, Places: []Place{home}, Params: DefaultCostParams()}
	start := utc("2026-10-07T07:00:00Z")
	day := func(n int) time.Time { return start.Add(-time.Duration(n) * 24 * time.Hour) }
	// charge started n days before the trip, with energy kWh into the battery.
	charge := func(n int, kwh float64, f func(*Charge)) Charge {
		c := Charge{
			DetectedAt: day(n), Start: Bounds{day(n), day(n)}, End: Bounds{day(n).Add(time.Hour), day(n).Add(time.Hour)},
			Type: known(AC), StartSoC: val(40), EndSoC: val(80), EnergySoCKWh: val(kwh), Position: known(lyon),
		}
		if f != nil {
			f(&c)
		}
		return c
	}
	dc := func(c *Charge) { c.Type, c.Position = known(DC), known(Position{Lat: 48.85, Lon: 2.35}) }
	cost := func(lo, hi int64) Value[Cost] { return known(Cost{Currency: eur.V, Min: lo, Max: hi}) }
	trip := Trip{
		DetectedAt: start, Start: Bounds{start, start}, End: Bounds{start.Add(time.Hour), start.Add(time.Hour)},
		DistanceKm: val(112), EnergyKWh: val(21.6),
	}
	tripCost := func(lo, hi int64, per100 Value[MinorRange], p KWhPrice) Value[TripCost] {
		p.Currency, p.From, p.To = eur.V, start.Add(-TripPriceWindow), start
		return known(TripCost{Currency: eur.V, Min: lo, Max: hi, Per100km: per100, Price: p})
	}
	for _, tt := range []struct {
		name    string
		pricing Pricing
		trip    Trip
		charges []Charge
		costs   []Value[Cost]
		want    Value[TripCost]
	}{
		{
			// 40 kWh at home for 5.00 €, 10 kWh on DC for 5.00 €: 0.20 € a kWh, 4.32 € the
			// trip's 21.6 kWh, 3.86 € over its 112 km.
			name:    "home and fast charges",
			pricing: pricing, trip: trip,
			charges: []Charge{charge(10, 40, nil), charge(3, 10, dc)},
			costs:   []Value[Cost]{cost(500, 500), cost(500, 500)},
			want: tripCost(432, 432, known(MinorRange{386, 386}), KWhPrice{
				Min: 0.2, Max: 0.2, EnergyKWh: 50, Charges: 2,
				Shares: []PriceShare{
					{Place: known(home), Type: "", EnergyKWh: 40, Min: 5, Max: 5},
					{Type: DC, EnergyKWh: 10, Min: 5, Max: 5},
				},
			}),
		},
		{
			// 0.10 to 0.15 € a kWh: 2.16 to 3.24 €.
			name:    "a range",
			pricing: pricing, trip: trip,
			charges: []Charge{charge(1, 20, nil)},
			costs:   []Value[Cost]{cost(200, 300)},
			want: tripCost(216, 324, known(MinorRange{192, 290}), KWhPrice{
				Min: 0.1, Max: 0.15, EnergyKWh: 20, Charges: 1,
				Shares: []PriceShare{{Place: known(home), EnergyKWh: 20, Min: 2, Max: 3}},
			}),
		},
		{
			// The 31st day before, the trip's own start and after it are out of the window;
			// the 30th day before is in.
			name:    "the window",
			pricing: pricing, trip: trip,
			charges: []Charge{charge(31, 10, nil), charge(30, 10, nil), charge(0, 10, nil), charge(-1, 10, nil)},
			costs:   []Value[Cost]{cost(900, 900), cost(300, 300), cost(900, 900), cost(900, 900)},
			want: tripCost(648, 648, known(MinorRange{579, 579}), KWhPrice{
				Min: 0.3, Max: 0.3, EnergyKWh: 10, Charges: 1,
				Shares: []PriceShare{{Place: known(home), EnergyKWh: 10, Min: 3, Max: 3}},
			}),
		},
		{
			// A charge without a cost, and one without an energy, are left out and counted.
			name:    "charges left out",
			pricing: pricing, trip: trip,
			charges: []Charge{
				charge(5, 10, nil), charge(4, 30, nil),
				charge(3, 0, func(c *Charge) { c.EnergySoCKWh = Value[float64]{} }),
			},
			costs: []Value[Cost]{cost(200, 200), {}, cost(100, 100)},
			want: tripCost(432, 432, known(MinorRange{386, 386}), KWhPrice{
				Min: 0.2, Max: 0.2, EnergyKWh: 10, Charges: 1, LeftOut: 2,
				Shares: []PriceShare{{Place: known(home), EnergyKWh: 10, Min: 2, Max: 2}},
			}),
		},
		{
			// An entered cost of 0 is a free kWh, not an unknown one.
			name:    "free charges",
			pricing: pricing, trip: trip,
			charges: []Charge{charge(2, 30, nil)},
			costs:   []Value[Cost]{cost(0, 0)},
			want: tripCost(0, 0, known(MinorRange{0, 0}), KWhPrice{
				EnergyKWh: 30, Charges: 1,
				Shares: []PriceShare{{Place: known(home), EnergyKWh: 30}},
			}),
		},
		{
			// Elsewhere, a charge of unknown type counts as AC, as its efficiency.
			name:    "AC elsewhere",
			pricing: pricing, trip: trip,
			charges: []Charge{charge(2, 10, func(c *Charge) { c.Type, c.Position = Value[ChargeType]{}, Value[Position]{} })},
			costs:   []Value[Cost]{cost(250, 250)},
			want: tripCost(540, 540, known(MinorRange{482, 482}), KWhPrice{
				Min: 0.25, Max: 0.25, EnergyKWh: 10, Charges: 1,
				Shares: []PriceShare{{Type: AC, EnergyKWh: 10, Min: 2.5, Max: 2.5}},
			}),
		},
		{
			// A trip that gained energy costs nothing; without a distance, no cost per 100 km.
			name:    "energy gained, no distance",
			pricing: pricing,
			trip:    with(trip, func(t *Trip) { t.EnergyKWh, t.DistanceKm = val(-0.4), Value[float64]{} }),
			charges: []Charge{charge(2, 10, nil)},
			costs:   []Value[Cost]{cost(250, 250)},
			want: tripCost(0, 0, Value[MinorRange]{}, KWhPrice{
				Min: 0.25, Max: 0.25, EnergyKWh: 10, Charges: 1,
				Shares: []PriceShare{{Place: known(home), EnergyKWh: 10, Min: 2.5, Max: 2.5}},
			}),
		},
		{
			name:    "no charge with a cost: unknown, never zero",
			pricing: pricing, trip: trip,
			charges: []Charge{charge(2, 10, nil)},
			costs:   []Value[Cost]{{}},
		},
		{
			name:    "no charge",
			pricing: pricing, trip: trip,
		},
		{
			name:    "unknown energy",
			pricing: pricing,
			trip:    with(trip, func(t *Trip) { t.EnergyKWh = Value[float64]{} }),
			charges: []Charge{charge(2, 10, nil)},
			costs:   []Value[Cost]{cost(250, 250)},
		},
		{
			name:    "no currency",
			pricing: with(pricing, func(p *Pricing) { p.Currency = Value[Currency]{} }),
			trip:    trip,
			charges: []Charge{charge(2, 10, nil)},
			costs:   []Value[Cost]{cost(250, 250)},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pricing.TripCosts([]Trip{tt.trip}, tt.charges, tt.costs)
			if len(got) != 1 {
				t.Fatalf("got %d costs", len(got))
			}
			if !reflect.DeepEqual(got[0], tt.want) {
				t.Errorf("cost = %+v\nwant   %+v", got[0], tt.want)
			}
		})
	}
}

// The period's driving cost sums its trips' costs; the cost per 100 km takes the trips
// with both a cost and a positive distance. A plug-in hybrid keeps the costs, not the
// cost per 100 km.
func TestSummarizeTripCosts(t *testing.T) {
	t1 := pt("2026-06-01 08:00")
	h := func(n int) time.Time { return t1.Add(time.Duration(n) * time.Hour) }
	trips := []Trip{
		drive(h(0), h(1), 100, 15, 80, 66),
		drive(h(2), h(3), 50, 8, 66, 59),
		drive(h(4), h(5), 0, 1, 59, 58),  // no distance: in the cost, not per 100 km
		drive(h(6), h(7), 20, 3, 58, 55), // no cost
	}
	cost := func(lo, hi int64) Value[TripCost] { return known(TripCost{Currency: eur.V, Min: lo, Max: hi}) }
	costs := Costs{Trips: []Value[TripCost]{cost(300, 330), cost(160, 170), cost(20, 20), {}}}
	s, err := Summarize(trips, nil, costs, nil, nil, pt("2026-06-01 00:00"), pt("2026-06-02 00:00"), paris, BucketDay, ChargesAt{}, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	// 460 to 500 cents over 150 km: 306.67 to 333.33 per 100 km.
	want := CostSum{Min: 480, Max: 520, Unknown: 1}
	per100 := known(MinorRange{306, 334})
	for name, got := range map[string]TripStats{"totals": s.Totals.Trips, "day": s.Buckets[0].Trips} {
		if got.Cost != want || got.CostPer100km != per100 {
			t.Errorf("%s: cost %+v, per 100 km %+v; want %+v, %+v", name, got.Cost, got.CostPer100km, want, per100)
		}
	}
	h2 := s.WithoutConsumption()
	if h2.Totals.Trips.CostPer100km.OK || h2.Buckets[0].Trips.CostPer100km.OK {
		t.Error("a plug-in hybrid has a cost per 100 km")
	}
	if h2.Totals.Trips.Cost != want {
		t.Errorf("a plug-in hybrid's cost = %+v, want %+v", h2.Totals.Trips.Cost, want)
	}
}
