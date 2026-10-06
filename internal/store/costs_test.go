package store

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/derive"
	"runsten/internal/volvo"
)

var (
	eur, _ = core.CurrencyOf("EUR")
	sek, _ = core.CurrencyOf("SEK")
)

// home is a place with every field set: two versions, one with windows across midnight,
// prices with five decimals.
func home(t *testing.T) core.Place {
	t.Helper()
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	return core.Place{
		Name: "Maison", Position: core.Position{Lat: 45.764, Lon: 4.8357}, RadiusM: 100, Location: paris,
		WithoutPosition: true, MaxPowerKW: some(11), Efficiency: some(0.8),
		Tariff: []core.TariffVersion{
			{ValidFrom: core.Date{Year: 2026, Month: time.January, Day: 1}, PricePerKWh: 0.1356},
			{ValidFrom: core.Date{Year: 2026, Month: time.August, Day: 1}, PricePerKWh: 0.21420, Windows: []core.PriceWindow{
				{From: 22 * 60, To: 6 * 60, PricePerKWh: 0.1589},
				{
					Days: []time.Weekday{time.Sunday, time.Saturday}, From: 0, To: 0,
					Months: []time.Month{time.November, time.December}, PricePerKWh: 0.12345,
				},
			}},
		},
	}
}

// work is a place with no optional field, a fixed price and the zone left to UTC.
func work() core.Place {
	return core.Place{
		Name: "Travail", Position: core.Position{Lat: 45.7797, Lon: 4.927}, RadiusM: 250,
		Tariff: []core.TariffVersion{{ValidFrom: core.Date{Year: 2026, Month: time.March, Day: 30}}},
	}
}

// samePlaces compares places, their zones by name: a *time.Location holds a cache.
func samePlaces(t *testing.T, got, want []core.Place) {
	t.Helper()
	zones := func(ps []core.Place) (names []string, out []core.Place) {
		for _, p := range ps {
			names = append(names, zoneName(p.Location))
			p.Location = nil
			out = append(out, p)
		}
		return names, out
	}
	gz, gp := zones(got)
	wz, wp := zones(want)
	if !reflect.DeepEqual(gz, wz) || !reflect.DeepEqual(gp, wp) {
		t.Errorf("places\n%v %+v\nwant\n%v %+v", gz, gp, wz, wp)
	}
}

func count(ctx context.Context, t *testing.T, s *Store, account, table string) int {
	t.Helper()
	var n int
	err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCurrency(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")

	if c, err := s.Currency(ctx, a); err != nil || c.OK {
		t.Fatalf("currency before any: %+v, %v", c, err)
	}
	for _, c := range []core.Currency{sek, eur, eur} { // free to change without prices
		if err := s.SetCurrency(ctx, a, c); err != nil {
			t.Fatal(err)
		}
	}
	if c, err := s.Currency(ctx, a); err != nil || c != (core.Value[core.Currency]{V: eur, OK: true}) {
		t.Fatalf("currency = %+v, %v", c, err)
	}

	// A place without a tariff gives no amount a unit.
	id, err := s.CreatePlace(ctx, a, core.Place{Name: "Parking", RadiusM: 50})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrency(ctx, a, sek); err != nil {
		t.Fatalf("no tariff yet: %v", err)
	}
	p := work()
	p.ID = id
	if err := s.ReplacePlace(ctx, a, p); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrency(ctx, a, eur); !errors.Is(err, api.ErrCurrencyInUse) {
		t.Errorf("change with a tariff: %v", err)
	}
	if err := s.SetCurrency(ctx, a, sek); err != nil {
		t.Errorf("same currency again: %v", err)
	}
	if err := s.DeletePlace(ctx, a, id); err != nil {
		t.Fatal(err)
	}

	// An entered cost locks it as well.
	if err := s.SaveDerivation(ctx, a, v, time.Time{}, sampleResult(t0)); err != nil {
		t.Fatal(err)
	}
	charge := sampleResult(t0).Charges[0]
	if err := s.SetChargeCost(ctx, a, v, core.EnteredCost{ChargeDetectedAt: charge.DetectedAt, AmountMinor: 1240}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrency(ctx, a, eur); !errors.Is(err, api.ErrCurrencyInUse) {
		t.Errorf("change with an entered cost: %v", err)
	}
	if c, _ := s.Currency(ctx, a); c.V != sek {
		t.Errorf("refused change applied: %+v", c)
	}

	// The first currency may come after the prices: they had no unit yet.
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if _, err := s.CreatePlace(ctx, b, work()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrency(ctx, b, eur); err != nil {
		t.Errorf("first currency after a tariff: %v", err)
	}

	// The list of currencies is the application's: an unknown code stored is an error.
	err = s.inAccount(ctx, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE account_settings SET currency = 'XXX'")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Currency(ctx, a); err == nil {
		t.Error("unknown currency read")
	}
	if err := s.SetCurrency(ctx, a, eur); !errors.Is(err, api.ErrCurrencyInUse) {
		t.Errorf("change from an unknown currency: %v", err)
	}
	for _, code := range []string{"eur", "EURO", ""} {
		if err := s.SetCurrency(ctx, b, core.Currency{Code: code}); err == nil {
			t.Errorf("currency %q stored", code)
		}
	}
}

func TestPlaces(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")

	if ps, err := s.Places(ctx, a); err != nil || len(ps) != 0 {
		t.Fatalf("places before any: %v, %v", ps, err)
	}
	h, w := home(t), work()
	var err error
	if h.ID, err = s.CreatePlace(ctx, a, h); err != nil {
		t.Fatal(err)
	}
	if w.ID, err = s.CreatePlace(ctx, a, w); err != nil {
		t.Fatal(err)
	}
	w.Location = time.UTC // stored as UTC when not set
	got, err := s.Places(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	samePlaces(t, got, []core.Place{h, w})
	// numeric(10,5) and jsonb give back the very floats written.
	if v := got[0].Tariff; v[0].PricePerKWh != 0.1356 || v[1].PricePerKWh != 0.2142 || v[1].Windows[1].PricePerKWh != 0.12345 {
		t.Errorf("prices = %+v", v)
	}

	if p, ok, err := s.Place(ctx, a, w.ID); err != nil || !ok {
		t.Errorf("place: %v, %v", ok, err)
	} else {
		samePlaces(t, []core.Place{p}, []core.Place{w})
	}
	const unknown = "00000000-0000-4000-8000-000000000000"
	if _, ok, err := s.Place(ctx, a, unknown); ok || err != nil {
		t.Errorf("unknown place: %v, %v", ok, err)
	}

	pricing, err := s.Pricing(ctx, a, core.DefaultCostParams())
	if err != nil || pricing.Currency.OK || len(pricing.Places) != 2 || pricing.Params != core.DefaultCostParams() {
		t.Errorf("pricing = %+v, %v", pricing, err)
	}
	if err := s.SetCurrency(ctx, a, eur); err != nil {
		t.Fatal(err)
	}
	if pricing, _ := s.Pricing(ctx, a, core.DefaultCostParams()); pricing.Currency.V != eur {
		t.Errorf("pricing currency = %+v", pricing.Currency)
	}

	t.Run("replace the place and all its versions, keeping its rank", func(t *testing.T) {
		r := h
		r.Name, r.RadiusM, r.MaxPowerKW, r.Efficiency = "Maison de campagne", 40, core.Value[float64]{}, core.Value[float64]{}
		r.Tariff = []core.TariffVersion{{ValidFrom: core.Date{Year: 2026, Month: time.October, Day: 1}, PricePerKWh: 0.25}}
		if err := s.ReplacePlace(ctx, a, r); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Places(ctx, a)
		samePlaces(t, got, []core.Place{r, w})
		if n := count(ctx, t, s, a, "place_tariffs"); n != 2 {
			t.Errorf("%d versions stored, want 2", n)
		}
		r.ID = unknown
		if err := s.ReplacePlace(ctx, a, r); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("replace an unknown place: %v", err)
		}
	})
	t.Run("a failed replacement changes nothing", func(t *testing.T) {
		before, _ := s.Places(ctx, a)
		r := before[0]
		r.Tariff = append(r.Tariff, r.Tariff[0]) // the same day twice
		if err := s.ReplacePlace(ctx, a, r); err == nil {
			t.Error("two versions from the same day stored")
		}
		after, _ := s.Places(ctx, a)
		samePlaces(t, after, before)
	})
	t.Run("one place without a position", func(t *testing.T) {
		p := work()
		p.WithoutPosition = true
		if _, err := s.CreatePlace(ctx, a, p); !errors.Is(err, api.ErrWithoutPositionTaken) {
			t.Errorf("second place without a position created: %v", err)
		}
		p.ID = w.ID
		if err := s.ReplacePlace(ctx, a, p); !errors.Is(err, api.ErrWithoutPositionTaken) {
			t.Errorf("second place without a position replaced: %v", err)
		}
		if n := count(ctx, t, s, a, "places"); n != 2 {
			t.Errorf("%d places", n)
		}
	})
	t.Run("the database checks the bounds", func(t *testing.T) {
		for name, change := range map[string]func(*core.Place){
			"latitude":        func(p *core.Place) { p.Position.Lat = 91 },
			"longitude":       func(p *core.Place) { p.Position.Lon = -181 },
			"radius":          func(p *core.Place) { p.RadiusM = 0 },
			"name":            func(p *core.Place) { p.Name = "" },
			"efficiency":      func(p *core.Place) { p.Efficiency = some(1.1) },
			"power":           func(p *core.Place) { p.MaxPowerKW = some(0) },
			"price":           func(p *core.Place) { p.Tariff[0].PricePerKWh = -0.1 },
			"price precision": func(p *core.Place) { p.Tariff[0].PricePerKWh = 100000 },
			"window price":    func(p *core.Place) { p.Tariff[1].Windows[0].PricePerKWh = -1 },
			"window minutes":  func(p *core.Place) { p.Tariff[1].Windows[0].To = 1440 },
			"window day":      func(p *core.Place) { p.Tariff[1].Windows[1].Days = []time.Weekday{7} },
			"window month":    func(p *core.Place) { p.Tariff[1].Windows[1].Months = []time.Month{0} },
		} {
			p := home(t)
			p.WithoutPosition = false
			change(&p)
			if _, err := s.CreatePlace(ctx, a, p); err == nil {
				t.Errorf("%s out of bounds stored", name)
			}
		}
		if n := count(ctx, t, s, a, "places"); n != 2 {
			t.Errorf("%d places after refused ones", n)
		}
	})
	t.Run("deleting a place deletes its tariff", func(t *testing.T) {
		if err := s.DeletePlace(ctx, a, h.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.DeletePlace(ctx, a, h.ID); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("delete again: %v", err)
		}
		if n := count(ctx, t, s, a, "place_tariffs"); n != 1 {
			t.Errorf("%d versions left, want the other place's", n)
		}
	})
	t.Run("an unknown time zone stored is an error", func(t *testing.T) {
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE places SET time_zone = 'Mars/Olympus_Mons'")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Places(ctx, a); err == nil || strings.Contains(err.Error(), "Travail") {
			t.Errorf("places with an unknown zone: %v", err)
		}
	})
}

// TestPlaceLimit creates places at once, more than the limit: the count under the
// account's lock lets exactly api.MaxPlaces through. Without the lock, several
// creations would count the same places and pass together.
func TestPlaceLimit(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if _, err := s.CreatePlace(ctx, b, work()); err != nil { // another account's count apart
		t.Fatal(err)
	}
	const tries = api.MaxPlaces + 5
	errs := make(chan error, tries)
	var wg sync.WaitGroup
	for range tries {
		wg.Go(func() {
			_, err := s.CreatePlace(ctx, a, work())
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	created, refused := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, api.ErrTooManyPlaces):
			refused++
		default:
			t.Errorf("create: %v", err)
		}
	}
	if created != api.MaxPlaces || refused != tries-api.MaxPlaces || count(ctx, t, s, a, "places") != api.MaxPlaces {
		t.Errorf("%d created, %d refused, %d places; want %d created", created, refused, count(ctx, t, s, a, "places"), api.MaxPlaces)
	}
	if _, err := s.CreatePlace(ctx, b, work()); err != nil {
		t.Errorf("another account's place: %v", err)
	}
}

// chargesAt are charges of an hour, every two hours from t0: detected 5 minutes after
// their start.
func chargesAt(hours ...int) []core.Charge {
	var out []core.Charge
	for _, h := range hours {
		start := t0.Add(time.Duration(h) * time.Hour)
		out = append(out, core.Charge{
			DetectedAt: start.Add(5 * time.Minute),
			Start:      core.Bounds{After: start, Before: start.Add(5 * time.Minute)},
			End:        core.Bounds{After: start.Add(55 * time.Minute), Before: start.Add(time.Hour)},
		})
	}
	return out
}

// insertCost stores an entered cost as is, with its own window.
func insertCost(ctx context.Context, t *testing.T, s *Store, account, vehicle string, e core.EnteredCost) {
	t.Helper()
	err := s.inAccount(ctx, account, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO charge_costs (account_id, vehicle_id, charge_detected_at, window_after, window_before, amount_minor, note)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			account, vehicle, e.ChargeDetectedAt, e.WindowAfter, e.WindowBefore, e.AmountMinor, e.Note)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func hours(h float64) time.Time { return t0.Add(time.Duration(h * float64(time.Hour))) }

func TestChargeCosts(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	charges := chargesAt(0, 2)
	if err := s.SaveDerivation(ctx, a, v, time.Time{}, core.Result{Charges: charges}); err != nil {
		t.Fatal(err)
	}
	c := charges[0]
	entered := core.EnteredCost{ChargeDetectedAt: c.DetectedAt, AmountMinor: 1240, EnergyKWh: some(31.5), Note: "Ionity, carte"}

	if err := s.SetChargeCost(ctx, a, v, entered); err != nil {
		t.Fatal(err)
	}
	entered.WindowAfter, entered.WindowBefore = c.Start.After, c.End.Before // the charge's, kept
	if got, err := s.EnteredCosts(ctx, a, v); err != nil || !reflect.DeepEqual(got, []core.EnteredCost{entered}) {
		t.Errorf("entered costs = %+v, %v\nwant %+v", got, err, entered)
	}

	t.Run("entered again, replaced", func(t *testing.T) {
		free := core.EnteredCost{ChargeDetectedAt: c.DetectedAt, WindowAfter: c.Start.After, WindowBefore: c.End.Before}
		if err := s.SetChargeCost(ctx, a, v, free); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.EnteredCosts(ctx, a, v); !reflect.DeepEqual(got, []core.EnteredCost{free}) {
			t.Errorf("entered costs = %+v", got)
		}
	})
	t.Run("refused", func(t *testing.T) {
		if err := s.SetChargeCost(ctx, a, v, core.EnteredCost{ChargeDetectedAt: hours(50)}); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("cost of an unknown charge: %v", err)
		}
		for name, e := range map[string]core.EnteredCost{
			"negative amount": {ChargeDetectedAt: c.DetectedAt, AmountMinor: -1},
			"negative energy": {ChargeDetectedAt: c.DetectedAt, EnergyKWh: some(-1)},
			"long note":       {ChargeDetectedAt: c.DetectedAt, Note: strings.Repeat("é", 501)},
		} {
			if err := s.SetChargeCost(ctx, a, v, e); err == nil {
				t.Errorf("%s stored", name)
			}
		}
		if err := s.SetChargeCost(ctx, a, v, core.EnteredCost{ChargeDetectedAt: c.DetectedAt, Note: strings.Repeat("é", 500)}); err != nil {
			t.Errorf("note of 500 characters: %v", err)
		}
	})
	t.Run("a cost attached by overlap is the one replaced and deleted", func(t *testing.T) {
		// A rebuild moved the charge's detection: its cost is now attached by overlap.
		moved := chargesAt(0, 2)
		moved[0].DetectedAt = moved[0].DetectedAt.Add(time.Minute)
		if err := s.SaveDerivation(ctx, a, v, time.Time{}, core.Result{Charges: moved}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetChargeCost(ctx, a, v, core.EnteredCost{ChargeDetectedAt: moved[0].DetectedAt, AmountMinor: 900}); err != nil {
			t.Fatal(err)
		}
		got, _ := s.EnteredCosts(ctx, a, v)
		if len(got) != 1 || !got[0].ChargeDetectedAt.Equal(moved[0].DetectedAt) || got[0].AmountMinor != 900 {
			t.Errorf("entered costs = %+v, want the one moved to the charge", got)
		}
		if err := s.DeleteChargeCost(ctx, a, v, moved[1].DetectedAt); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("delete the cost of a charge without one: %v", err)
		}
		if err := s.SaveDerivation(ctx, a, v, time.Time{}, core.Result{Charges: charges}); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteChargeCost(ctx, a, v, c.DetectedAt); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.EnteredCosts(ctx, a, v); len(got) != 0 {
			t.Errorf("entered costs after delete = %+v", got)
		}
		if err := s.DeleteChargeCost(ctx, a, v, hours(50)); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("delete the cost of an unknown charge: %v", err)
		}
	})
	t.Run("attach an orphan to a chosen charge", func(t *testing.T) {
		// Overlapping both charges: an orphan.
		insertCost(ctx, t, s, a, v, core.EnteredCost{ChargeDetectedAt: hours(40), WindowAfter: hours(0.5), WindowBefore: hours(2.5), AmountMinor: 700})
		orphans, err := s.OrphanCosts(ctx, a)
		if err != nil || len(orphans) != 1 || orphans[0].VehicleID != v || orphans[0].AmountMinor != 700 || orphans[0].EnteredAt.IsZero() {
			t.Fatalf("orphans = %+v, %v", orphans, err)
		}
		id := orphans[0].ID
		if err := s.AttachEnteredCost(ctx, a, id, v, charges[1].DetectedAt); err != nil {
			t.Fatal(err)
		}
		if orphans, _ := s.OrphanCosts(ctx, a); len(orphans) != 0 {
			t.Errorf("orphans after attaching = %+v", orphans)
		}
		want := core.EnteredCost{ChargeDetectedAt: charges[1].DetectedAt, WindowAfter: charges[1].Start.After, WindowBefore: charges[1].End.Before, AmountMinor: 700}
		if got, _ := s.EnteredCosts(ctx, a, v); !reflect.DeepEqual(got, []core.EnteredCost{want}) {
			t.Errorf("entered costs = %+v\nwant %+v", got, want)
		}
		if err := s.AttachEnteredCost(ctx, a, id, v, charges[1].DetectedAt); err != nil {
			t.Errorf("attach to its own charge again: %v", err)
		}

		if err := s.SetChargeCost(ctx, a, v, core.EnteredCost{ChargeDetectedAt: c.DetectedAt, AmountMinor: 1}); err != nil {
			t.Fatal(err)
		}
		if err := s.AttachEnteredCost(ctx, a, id, v, c.DetectedAt); !errors.Is(err, api.ErrChargeHasCost) {
			t.Errorf("attach to a charge with a cost: %v", err)
		}
		if err := s.AttachEnteredCost(ctx, a, id, v, hours(50)); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("attach to an unknown charge: %v", err)
		}
		const unknown = "00000000-0000-4000-8000-000000000000"
		if err := s.AttachEnteredCost(ctx, a, unknown, v, c.DetectedAt); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("attach an unknown cost: %v", err)
		}
		if err := s.DeleteEnteredCost(ctx, a, id); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteEnteredCost(ctx, a, id); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("delete again: %v", err)
		}
	})
	t.Run("deleting the vehicle deletes its costs", func(t *testing.T) {
		if n := count(ctx, t, s, a, "charge_costs"); n != 1 {
			t.Fatalf("%d costs", n)
		}
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "DELETE FROM vehicles WHERE id = $1", v)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if n := count(ctx, t, s, a, "charge_costs"); n != 0 {
			t.Errorf("%d costs left", n)
		}
	})
}

// TestEnteredCostsForPage attaches the entered costs to a page of the list as over the
// vehicle's whole history. Charges start every two hours and last one:
//
//	C1 0h   C2 2h   C3 4h   C4 6h   C5 8h   C6 10h, three pages of two, newest first.
//
// f is entered on C2; k overlaps C2 and C3, so C3 alone may take it, which the page of
// C3 tells only with f; e overlaps C4 and C5, on two pages: an orphan; h is entered on
// C6; o is far from any charge.
func TestEnteredCostsForPage(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	charges := chargesAt(0, 2, 4, 6, 8, 10)
	for _, x := range [][2]string{{a, v}, {b, vb}} {
		if err := s.SaveDerivation(ctx, x[0], x[1], time.Time{}, core.Result{Charges: charges}); err != nil {
			t.Fatal(err)
		}
	}
	costs := map[string]core.EnteredCost{
		"f": {ChargeDetectedAt: charges[1].DetectedAt, WindowAfter: hours(2), WindowBefore: hours(3), AmountMinor: 1},
		"k": {ChargeDetectedAt: hours(101), WindowAfter: hours(2.5), WindowBefore: hours(4.5), AmountMinor: 2},
		"e": {ChargeDetectedAt: hours(100), WindowAfter: hours(6.5), WindowBefore: hours(8.5), AmountMinor: 3},
		"h": {ChargeDetectedAt: charges[5].DetectedAt, WindowAfter: hours(10), WindowBefore: hours(11), AmountMinor: 4},
		"o": {ChargeDetectedAt: hours(102), WindowAfter: hours(20), WindowBefore: hours(21), AmountMinor: 5},
	}
	for _, e := range costs {
		insertCost(ctx, t, s, a, v, e)
	}
	// Another vehicle's costs change nothing, however they overlap.
	insertCost(ctx, t, s, b, vb, core.EnteredCost{ChargeDetectedAt: charges[2].DetectedAt, WindowAfter: hours(0), WindowBefore: hours(12)})

	if all, e, err := s.EnteredCostsFor(ctx, a, v, nil); err != nil || len(all) != 0 || len(e) != 0 {
		t.Errorf("for no charge: %v, %v, %v", all, e, err)
	}
	history, _ := s.EnteredCosts(ctx, a, v)
	full, orphans := core.AttachCosts(charges, history)
	amounts := func(attached []core.Value[core.EnteredCost]) []int64 {
		out := make([]int64, len(attached))
		for i, e := range attached {
			if e.OK {
				out[i] = e.V.AmountMinor
			}
		}
		return out
	}
	if got := amounts(full); !reflect.DeepEqual(got, []int64{0, 1, 2, 0, 0, 4}) || len(orphans) != 2 {
		t.Fatalf("whole history: %v, orphans %+v", got, orphans)
	}
	fullOf := func(c core.Charge) core.Value[core.EnteredCost] {
		for i, h := range charges {
			if h.DetectedAt.Equal(c.DetectedAt) {
				return full[i]
			}
		}
		t.Fatalf("charge %v not in the history", c.DetectedAt)
		return core.Value[core.EnteredCost]{}
	}
	check := func(t *testing.T, page []core.Charge) (naiveDiffers bool) {
		t.Helper()
		all, entered, err := s.EnteredCostsFor(ctx, a, v, page)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(all[:len(page)], page) {
			t.Fatalf("the given charges do not come first: %+v", all)
		}
		attached, _ := core.AttachCosts(all, entered)
		naive, _ := core.AttachCosts(page, history)
		for i, c := range page {
			if want := fullOf(c); !reflect.DeepEqual(attached[i], want) {
				t.Errorf("charge %v takes %+v, over the whole history %+v", c.DetectedAt, attached[i], want)
			}
			naiveDiffers = naiveDiffers || !reflect.DeepEqual(naive[i], fullOf(c))
		}
		return naiveDiffers
	}
	// checkPriced checks a read of the API: its charges, with the others that decide,
	// take the costs they take over the whole history.
	checkPriced := func(t *testing.T, p api.PricedCharges) {
		t.Helper()
		if !p.Currency.OK || len(p.Places) != 1 {
			t.Errorf("pricing = %+v, %+v", p.Currency, p.Places)
		}
		attached, _ := core.AttachCosts(append(slices.Clip(p.Charges), p.Deciding...), p.Entered)
		for i, c := range p.Charges {
			if want := fullOf(c); !reflect.DeepEqual(attached[i], want) {
				t.Errorf("charge %v takes %+v, over the whole history %+v", c.DetectedAt, attached[i], want)
			}
			found, ok, err := s.FindCharge(ctx, a, v, c.DetectedAt)
			if err != nil || !ok {
				t.Fatalf("find %v: %v, %v", c.DetectedAt, ok, err)
			}
			one, _ := core.AttachCosts(append(found.Charges, found.Deciding...), found.Entered)
			if want := fullOf(c); !reflect.DeepEqual(one[0], want) {
				t.Errorf("charge %v found alone takes %+v, over the whole history %+v", c.DetectedAt, one[0], want)
			}
		}
	}
	if err := s.SetCurrency(ctx, a, eur); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePlace(ctx, a, work()); err != nil {
		t.Fatal(err)
	}

	var after *api.EventKey
	naive := 0
	for page := 1; ; page++ {
		priced, err := s.ListCharges(ctx, a, v, api.EventQuery{After: after, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		charges := priced.Charges
		if len(charges) == 0 {
			break
		}
		t.Run("page "+string(rune('0'+page)), func(t *testing.T) {
			if check(t, charges) {
				naive++
			}
			checkPriced(t, priced)
		})
		last := charges[len(charges)-1]
		after = &api.EventKey{StartedAfter: last.Start.After, DetectedAt: last.DetectedAt}
	}
	if naive != 2 {
		t.Errorf("attaching over the page alone goes wrong on %d pages, want 2: the test lost its traps", naive)
	}
	t.Run("a period", func(t *testing.T) {
		period, err := s.PeriodEvents(ctx, a, v, hours(3), hours(9))
		if err != nil || len(period.Charges) != 4 {
			t.Fatalf("period: %d charges, %v", len(period.Charges), err)
		}
		check(t, period.Charges)
		checkPriced(t, period.PricedCharges)
		// The orphans of the vehicle's whole history, newest first, whatever the period.
		if o := period.Orphans; len(o) != 2 || o[0] != costs["o"] || o[1] != costs["e"] {
			t.Errorf("orphans = %+v", o)
		}
		if other, err := s.PeriodEvents(ctx, b, vb, hours(3), hours(9)); err != nil || len(other.Orphans) != 0 {
			t.Errorf("orphans of B's vehicle = %+v, %v", other.Orphans, err)
		}
	})
	t.Run("orphans of the account", func(t *testing.T) {
		got, err := s.OrphanCosts(ctx, a)
		if err != nil || len(got) != 2 || got[0].EnteredCost != costs["o"] || got[1].EnteredCost != costs["e"] {
			t.Errorf("orphans = %+v, %v", got, err)
		}
	})
}

// TestEnteredCostSurvivesRebuild enters the cost of a derived charge, then rebuilds with
// other parameters. The charge is reconstructed, never seen charging, from a SoC that
// rose by 1.5 then 3.5 points: with MinChargeSoC 2, the second rise reveals it; with 1,
// the first. Its detection time moves; with 5, there is no charge at all.
func TestEnteredCostSurvivesRebuild(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	save := func(ep volvo.Endpoint, at time.Time, raw string) {
		t.Helper()
		h, _ := volvo.Fingerprint([]byte(raw))
		if _, err := s.SaveSnapshot(ctx, collector.Snapshot{AccountID: a, VehicleID: v, Endpoint: ep, FetchedAt: at, Payload: []byte(raw), Hash: h}); err != nil {
			t.Fatal(err)
		}
	}
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * 10 * time.Minute) }
	save(volvo.Details, at(0), `{"data":{"batteryCapacityKWH":80}}`)
	for i, soc := range []string{"50", "51.5", "55", "55"} {
		save(volvo.EnergyState, at(i), `{"batteryChargeLevel":{"status":"OK","value":`+soc+`,"unit":"percentage","updatedAt":"`+
			at(i).Format(time.RFC3339)+`"}}`)
	}

	rebuild := func(t *testing.T, minChargeSoC float64) []core.Charge {
		t.Helper()
		p := core.DefaultParams()
		p.MinChargeSoC = minChargeSoC
		if _, err := derive.New(s, p, nil, quietLog).Rebuild(ctx, a, v); err != nil {
			t.Fatal(err)
		}
		charges, err := s.Charges(ctx, a, v)
		if err != nil {
			t.Fatal(err)
		}
		return charges
	}
	// attached is the cost each charge takes, read as the API will.
	attached := func(t *testing.T, charges []core.Charge) []core.Value[core.EnteredCost] {
		t.Helper()
		all, entered, err := s.EnteredCostsFor(ctx, a, v, charges)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := core.AttachCosts(all, entered)
		return out[:len(charges)]
	}
	orphans := func(t *testing.T) int {
		t.Helper()
		o, err := s.OrphanCosts(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		return len(o)
	}

	charges := rebuild(t, core.DefaultParams().MinChargeSoC)
	if len(charges) != 1 || !charges[0].Reconstructed || !charges[0].DetectedAt.Equal(at(2)) {
		t.Fatalf("charges = %+v, want one reconstructed, detected at the second rise", charges)
	}
	detected := charges[0].DetectedAt
	if err := s.SetChargeCost(ctx, a, v, core.EnteredCost{ChargeDetectedAt: detected, AmountMinor: 1240, Note: "Ionity"}); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.EnteredCosts(ctx, a, v)

	t.Run("same rebuild: attached at the same detection time", func(t *testing.T) {
		charges := rebuild(t, core.DefaultParams().MinChargeSoC)
		got := attached(t, charges)
		if len(charges) != 1 || !charges[0].DetectedAt.Equal(detected) || !got[0].OK || got[0].V.AmountMinor != 1240 || orphans(t) != 0 {
			t.Errorf("charges %+v take %+v", charges, got)
		}
	})
	t.Run("detection moved: attached by overlap", func(t *testing.T) {
		charges := rebuild(t, 1)
		if len(charges) != 1 || !charges[0].DetectedAt.Equal(at(1)) {
			t.Fatalf("charges = %+v, want one detected at the first rise", charges)
		}
		got := attached(t, charges)
		if !got[0].OK || !got[0].V.ChargeDetectedAt.Equal(detected) || orphans(t) != 0 {
			t.Errorf("charge %v takes %+v", charges[0].DetectedAt, got)
		}
	})
	t.Run("charge gone: orphaned, kept", func(t *testing.T) {
		if charges := rebuild(t, 5); len(charges) != 0 {
			t.Fatalf("charges = %+v, want none", charges)
		}
		if orphans(t) != 1 {
			t.Error("the cost is not listed as an orphan")
		}
	})
	t.Run("back to the first parameters: attached again", func(t *testing.T) {
		charges := rebuild(t, core.DefaultParams().MinChargeSoC)
		if got := attached(t, charges); !got[0].OK || orphans(t) != 0 {
			t.Errorf("charges %+v take %+v", charges, got)
		}
		if now, _ := s.EnteredCosts(ctx, a, v); !reflect.DeepEqual(now, stored) {
			t.Errorf("entered costs changed by the rebuilds: %+v, want %+v", now, stored)
		}
	})
}

// TestCostIsolation extends TestAccountIsolation to the settings, places, tariffs and
// entered costs.
func TestCostIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	charge := chargesAt(0)[0]
	placeOf := map[string]string{}
	for _, x := range [][2]string{{a, va}, {b, vb}} {
		if err := s.SetCurrency(ctx, x[0], eur); err != nil {
			t.Fatal(err)
		}
		id, err := s.CreatePlace(ctx, x[0], home(t))
		if err != nil {
			t.Fatal(err)
		}
		placeOf[x[0]] = id
		if err := s.SaveDerivation(ctx, x[0], x[1], time.Time{}, core.Result{Charges: chargesAt(0)}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetChargeCost(ctx, x[0], x[1], core.EnteredCost{ChargeDetectedAt: charge.DetectedAt, AmountMinor: 1}); err != nil {
			t.Fatal(err)
		}
		// Orphaned: overlapping nothing.
		insertCost(ctx, t, s, x[0], x[1], core.EnteredCost{ChargeDetectedAt: hours(10), WindowAfter: hours(10), WindowBefore: hours(11)})
	}
	orphansOf := func(account string) []api.EnteredCost {
		t.Helper()
		o, err := s.OrphanCosts(ctx, account)
		if err != nil || len(o) != 1 {
			t.Fatalf("orphans = %+v, %v", o, err)
		}
		return o
	}
	orphanB := orphansOf(b)[0].ID
	tables := map[string]int{"account_settings": 1, "places": 1, "place_tariffs": 2, "charge_costs": 2}
	for table, n := range tables {
		if got := count(ctx, t, s, a, table); got != n {
			t.Errorf("account A, %s: %d rows visible, want %d", table, got, n)
		}
	}

	t.Run("with no account set, nothing is visible", func(t *testing.T) {
		for table := range tables {
			var n int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("%s: %d visible, %v", table, n, err)
			}
		}
	})
	t.Run("another account's rows are not found", func(t *testing.T) {
		if ps, err := s.Places(ctx, a); err != nil || len(ps) != 1 || ps[0].ID != placeOf[a] {
			t.Errorf("places of A = %+v, %v", ps, err)
		}
		if _, ok, err := s.Place(ctx, a, placeOf[b]); ok || err != nil {
			t.Errorf("A reads B's place: %v, %v", ok, err)
		}
		if e, err := s.EnteredCosts(ctx, a, vb); len(e) != 0 || err != nil {
			t.Errorf("A reads B's costs: %+v, %v", e, err)
		}
		if all, e, err := s.EnteredCostsFor(ctx, a, vb, []core.Charge{charge}); len(all) != 1 || len(e) != 0 || err != nil {
			t.Errorf("A reads B's costs for a charge: %+v, %+v, %v", all, e, err)
		}
		if p, err := s.ListCharges(ctx, a, vb, api.EventQuery{Limit: 10}); len(p.Charges) != 0 || len(p.Entered) != 0 || err != nil {
			t.Errorf("A lists B's charges: %+v, %v", p, err)
		}
		if p, ok, err := s.FindCharge(ctx, a, vb, charge.DetectedAt); ok || err != nil {
			t.Errorf("A finds B's charge: %+v, %v", p, err)
		}
		if p, err := s.PeriodEvents(ctx, a, vb, time.Time{}, hours(24)); len(p.Charges) != 0 || len(p.Orphans) != 0 || err != nil {
			t.Errorf("A reads B's period: %+v, %v", p, err)
		}
		p, err := s.ListCharges(ctx, a, va, api.EventQuery{Limit: 10})
		if err != nil || len(p.Places) != 1 || p.Places[0].ID != placeOf[a] || len(p.Entered) != 1 {
			t.Errorf("A's charges = %+v, %v", p, err)
		}
		h, err := s.ChargeHistory(ctx, a)
		if err != nil || len(h) != 1 || len(h[0].Charges) != 1 || len(h[0].Entered) != 2 || !h[0].Currency.OK ||
			len(h[0].Places) != 1 || h[0].Places[0].ID != placeOf[a] {
			t.Errorf("A's charge history = %+v, %v", h, err)
		}
		if o := orphansOf(a); o[0].VehicleID != va {
			t.Errorf("orphans of A = %+v", o)
		}
	})
	t.Run("rows are not moved nor inserted into another account", func(t *testing.T) {
		for table := range tables {
			err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, "UPDATE "+table+" SET account_id = $1", b)
				return err
			})
			if err == nil {
				t.Errorf("%s: rows moved to another account", table)
			}
		}
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO places (account_id, name, lat, lon, radius_m, time_zone, without_position) VALUES ($1, 'x', 0, 0, 1, 'UTC', false)", b)
			return err
		})
		if err == nil {
			t.Error("place inserted into another account")
		}
		err = s.inAccount(ctx, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO place_tariffs (account_id, place_id, valid_from, price_per_kwh) VALUES ($1, $2, '2026-01-01', 0)", a, placeOf[b])
			return err
		})
		if err == nil {
			t.Error("version added to another account's place")
		}
	})
	t.Run("another account's rows are not written", func(t *testing.T) {
		p := home(t)
		p.ID = placeOf[b]
		p.Name = "Volé"
		for name, err := range map[string]error{
			"replace place":  s.ReplacePlace(ctx, a, p),
			"delete place":   s.DeletePlace(ctx, a, placeOf[b]),
			"enter cost":     s.SetChargeCost(ctx, a, vb, core.EnteredCost{ChargeDetectedAt: charge.DetectedAt, AmountMinor: 9}),
			"delete cost":    s.DeleteChargeCost(ctx, a, vb, charge.DetectedAt),
			"delete orphan":  s.DeleteEnteredCost(ctx, a, orphanB),
			"attach orphan":  s.AttachEnteredCost(ctx, a, orphanB, va, charge.DetectedAt),
			"attach to B's":  s.AttachEnteredCost(ctx, a, orphansOf(a)[0].ID, vb, charge.DetectedAt),
			"attach to none": s.AttachEnteredCost(ctx, a, orphansOf(a)[0].ID, va, hours(50)),
		} {
			if !errors.Is(err, api.ErrNotFound) {
				t.Errorf("%s: %v, want not found", name, err)
			}
		}
		if ps, _ := s.Places(ctx, b); len(ps) != 1 || ps[0].Name != "Maison" || len(ps[0].Tariff) != 2 {
			t.Errorf("B's places = %+v", ps)
		}
		if e, _ := s.EnteredCosts(ctx, b, vb); len(e) != 2 || e[0].AmountMinor != 1 {
			t.Errorf("B's costs = %+v", e)
		}
		orphansOf(b)
	})
	t.Run("deleting from another account has no effect", func(t *testing.T) {
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			for table := range tables {
				if _, err := tx.Exec(ctx, "DELETE FROM "+table); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for table, n := range tables {
			if got := count(ctx, t, s, b, table); got != n {
				t.Errorf("%s of account B: %d rows, want %d", table, got, n)
			}
		}
		// A's currency is A's alone, and so are its prices.
		if err := s.SetCurrency(ctx, a, sek); err != nil {
			t.Fatal(err)
		}
		if c, _ := s.Currency(ctx, b); c.V != eur {
			t.Errorf("B's currency = %+v", c)
		}
		if err := s.SetCurrency(ctx, b, sek); !errors.Is(err, api.ErrCurrencyInUse) {
			t.Errorf("B's currency unlocked by A's prices gone: %v", err)
		}
	})
}
