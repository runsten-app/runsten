package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
)

// batteryCharge is an observed charge that ends at at, whose retained span covers its
// whole window: a capacity estimate of kwh over span points of SoC, from 40 %.
func batteryCharge(at time.Time, typ core.ChargeType, span, kwh, odo float64) core.Charge {
	return core.Charge{
		DetectedAt: at.Add(-time.Minute),
		Start:      core.Bounds{After: at.Add(-2 * time.Hour), Before: at.Add(-time.Hour)},
		End:        core.Bounds{After: at.Add(-10 * time.Minute), Before: at},
		Type:       some(typ), StartSoC: some(40.0), EndSoC: some(40 + span),
		EnergySoCKWh: some(span / 100 * 75), EnergyPowerKWh: some(kwh),
		Span:       some(core.PowerSpan{StartSoC: 40, EndSoC: 40 + span, EnergyKWh: kwh, MaxGap: time.Minute, Duration: time.Hour}),
		OdometerKm: some(odo),
	}
}

// batteryTrip is an observed trip that starts at at, with 50 % of SoC and range km
// of displayed range: the displayed range at a full charge is range/50×100 km.
func batteryTrip(at time.Time, rangeKm float64) core.Trip {
	return core.Trip{
		DetectedAt: at.Add(31 * time.Minute),
		Start:      core.Bounds{After: at, Before: at.Add(6 * time.Minute)},
		End:        core.Bounds{After: at.Add(30 * time.Minute), Before: at.Add(31 * time.Minute)},
		StartSoC:   some(50.0), StartRangeKm: some(rangeKm),
	}
}

// batteryReceipt is the entered cost of the charge c: energy kWh read on its receipt,
// in minor units of the fixtures' euro. The ID leaves room for the reader's own.
func batteryReceipt(c core.Charge, energy float64, id int) EnteredCost {
	return EnteredCost{
		ID: costID(id), VehicleID: car, EnteredAt: t0,
		EnteredCost: core.EnteredCost{
			ChargeDetectedAt: c.DetectedAt, WindowAfter: c.Start.After, WindowBefore: c.End.Before,
			AmountMinor: 850, EnergyKWh: some(energy),
		},
	}
}

// setAside is one charge of every kind the filters set aside, ending at each of ats:
// a reconstructed one, one without power, one whose span was not kept, one over a
// narrow span, one over a gapped span, one over a slow one, and one whose estimate is
// implausible. None gives an estimate; all make the response's excluded reasons read.
func setAside(ats []time.Time) []core.Charge {
	base := func(i float64) core.Charge { return batteryCharge(ats[int(i)], core.AC, 40, 30, 20500) }
	reconstructed := base(0)
	reconstructed.Reconstructed, reconstructed.Type = true, core.Value[core.ChargeType]{}
	reconstructed.EnergyPowerKWh, reconstructed.Span, reconstructed.OdometerKm =
		core.Value[float64]{}, core.Value[core.PowerSpan]{}, core.Value[float64]{}
	noPower := base(1)
	noPower.EnergyPowerKWh, noPower.Span = core.Value[float64]{}, core.Value[core.PowerSpan]{}
	high := base(2)
	high.Span = core.Value[core.PowerSpan]{}
	narrow := base(3)
	narrow.StartSoC, narrow.EndSoC, narrow.EnergySoCKWh = some(40.0), some(50.0), some(7.5)
	narrow.Span = some(core.PowerSpan{StartSoC: 40, EndSoC: 50, EnergyKWh: 7.5, MaxGap: time.Minute, Duration: time.Hour})
	gappy := base(4)
	gappy.Span.V.MaxGap = 5 * time.Minute
	slow := base(5)
	slow.EnergyPowerKWh, slow.Span.V.EnergyKWh = some(1.0), 1.0
	wild := base(6)
	wild.EnergyPowerKWh, wild.Span.V.EnergyKWh = some(36.0), 36.0
	return []core.Charge{reconstructed, noPower, high, narrow, gappy, slow, wild}
}

// batteryHistory is the story of the battery golden files: fourteen months, from
// August 2025 to September 2026, of charges whose power estimates fade from 76 to
// 72.75 kWh — two AC ones and a DC one a month, the DC one with the energy of its
// receipt entered, a billed estimate a kWh above its power one — and two trips a month
// for the displayed range at a full charge. September 2025 holds one charge of every
// kind the filters set aside, beside two good ones; October holds trips only, and
// November nothing: one month shows the range without the capacity, one is empty.
func batteryHistory() (charges []core.Charge, trips []core.Trip, entered []EnteredCost) {
	at := func(mo, d, hh int) time.Time {
		first := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC).AddDate(0, mo, 0)
		return time.Date(first.Year(), first.Month(), d, hh, 0, 0, 0, time.UTC)
	}
	for mo := 0; mo < 14; mo++ {
		cap, odo := 76-0.25*float64(mo), 20000+float64(mo)*500
		charge := func(d, span int, typ core.ChargeType) core.Charge {
			c := batteryCharge(at(mo, d, 12), typ, float64(span), cap*float64(span)/100, odo+float64(d)*8)
			if d == 12 {
				c.OdometerKm = core.Value[float64]{} // no reading of it ended the charge
			}
			return c
		}
		switch mo {
		case 2, 3: // no charge: October holds the range only, November nothing
		case 1:
			charges = append(charges, charge(6, 40, core.AC), charge(13, 50, core.AC))
			charges = append(charges, setAside([]time.Time{
				at(1, 2, 12), at(1, 3, 12), at(1, 4, 12), at(1, 8, 12),
				at(1, 9, 12), at(1, 10, 12), at(1, 11, 12),
			})...)
		default:
			charges = append(charges, charge(5, 50, core.AC), charge(12, 25, core.AC))
			dc := charge(20, 25, core.DC)
			charges = append(charges, dc)
			// What the receipt entered covers a kWh more than the battery took, over
			// the DC efficiency: the billed estimate reads its own point.
			entered = append(entered, batteryReceipt(dc, (cap+1)/4/0.95, 100+len(entered)))
		}
		if mo != 3 {
			trips = append(trips, batteryTrip(at(mo, 7, 8), 200), batteryTrip(at(mo, 14, 8), 202))
		}
	}
	return charges, trips, entered
}

// batteryView is the part of a battery response the tests look into.
type batteryView struct {
	TimeZone  string `json:"time_zone"`
	Reference *struct {
		CapacityKWh float64 `json:"capacity_kwh"`
		Source      string  `json:"source"`
	} `json:"reference"`
	Current *struct {
		CapacityKWh float64 `json:"capacity_kwh"`
		Estimates   int     `json:"estimates"`
	} `json:"current"`
	DeviationPct *int `json:"deviation_pct"`
	Change       *struct {
		Since      time.Time `json:"since"`
		InitialKWh float64   `json:"initial_kwh"`
		ChangePct  int       `json:"change_pct"`
	} `json:"change"`
	Cycles    *float64 `json:"cycles"`
	Estimates []struct {
		Charge     string   `json:"charge"`
		OdometerKm *float64 `json:"odometer_km"`
		Source     string   `json:"source"`
	} `json:"estimates"`
	Excluded struct {
		Reconstructed int `json:"reconstructed"`
		NoPower       int `json:"no_power"`
		HighSoC       int `json:"high_soc"`
		SpanSoC       int `json:"span_soc"`
		PowerGap      int `json:"power_gap"`
		LowPower      int `json:"low_power"`
		Implausible   int `json:"implausible"`
	} `json:"excluded"`
	Months []struct {
		Start    time.Time `json:"start"`
		Capacity *struct {
			Estimates int `json:"estimates"`
		} `json:"capacity"`
		RangeAtFull *struct {
			Readings int `json:"readings"`
		} `json:"range_at_full"`
	} `json:"months"`
}

// readBattery reads a battery response, or fails the test with it.
func readBattery(t *testing.T, target string, c *http.Cookie) batteryView {
	t.Helper()
	resp, body := get(t, noFollow(), target, c)
	if resp.status != http.StatusOK {
		t.Fatalf("%s: status %d: %s", target, resp.status, body)
	}
	var b batteryView
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("%s: %v", target, err)
	}
	return b
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestBatteryJSON(t *testing.T) {
	t.Run("fourteen months of history", func(t *testing.T) {
		e, c := readerEnv(t)
		charges, trips, entered := batteryHistory()
		e.reader.charges[car], e.reader.trips[car] = charges, trips
		e.reader.entered = append(e.reader.entered, entered...)
		base := e.api.URL + "/api/v1/vehicles/" + car + "/battery"
		_, body := get(t, noFollow(), base, c)
		golden(t, "battery.json", body)
		b := readBattery(t, base, c)

		// The XC40 is recognized: its variant's net capacity is the reference.
		if b.Reference == nil || b.Reference.Source != "catalog_net" || !near(b.Reference.CapacityKWh, 75) {
			t.Errorf("reference = %+v", b.Reference)
		}
		if b.Current == nil || !near(b.Current.CapacityKWh, 73.5) || b.Current.Estimates != 20 {
			t.Errorf("current = %+v", b.Current)
		}
		if b.DeviationPct == nil || *b.DeviationPct != -2 {
			t.Errorf("deviation_pct = %v", b.DeviationPct)
		}
		if b.Change == nil || !b.Change.Since.Equal(time.Date(2025, 8, 5, 12, 0, 0, 0, time.UTC)) ||
			!near(b.Change.InitialKWh, 75) || b.Change.ChangePct != -2 {
			t.Errorf("change = %+v", b.Change)
		}
		if b.Cycles == nil || !near(*b.Cycles, 14.4) {
			t.Errorf("cycles = %v", b.Cycles)
		}
		// 44 estimates from the eleven good months, 2 from September 2025, and the
		// DC charges' receipts add one billed point each.
		if len(b.Estimates) != 46 {
			t.Errorf("%d estimates, want 46", len(b.Estimates))
		}
		if x := b.Excluded; x.Reconstructed != 1 || x.NoPower != 1 || x.HighSoC != 1 || x.SpanSoC != 1 ||
			x.PowerGap != 1 || x.LowPower != 1 || x.Implausible != 1 {
			t.Errorf("excluded = %+v, want one of every reason", x)
		}
		if len(b.Months) != 14 {
			t.Errorf("%d months, want 14", len(b.Months))
		}
		oct, nov := b.Months[2], b.Months[3]
		if !oct.Start.Equal(time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("October starts at %s", oct.Start)
		}
		if oct.Capacity != nil || oct.RangeAtFull == nil || oct.RangeAtFull.Readings != 2 {
			t.Errorf("October = %+v: the range without the capacity", oct)
		}
		if nov.Capacity != nil || nov.RangeAtFull != nil {
			t.Errorf("November = %+v: an empty month, present with no value", nov)
		}
		// The first estimate is the first charge's, and the second one's odometer
		// never ended a charge.
		if b.Estimates[0].Charge != "2025-08-05T11:59:00.000000Z" || b.Estimates[0].Source != "power" {
			t.Errorf("first estimate = %+v", b.Estimates[0])
		}
		if b.Estimates[1].OdometerKm != nil {
			t.Errorf("second estimate = %+v: its charge has no odometer", b.Estimates[1])
		}
		// The last charge gives two points, power before billed.
		if last := b.Estimates[45]; last.Source != "billed" || b.Estimates[44].Charge != last.Charge ||
			b.Estimates[44].Source != "power" {
			t.Errorf("last estimates = %+v %+v", b.Estimates[44], b.Estimates[45])
		}
		// The months are split in the requested time zone.
		paris := readBattery(t, base+"?tz=Europe/Paris", c)
		if !paris.Months[0].Start.Equal(time.Date(2025, 7, 31, 22, 0, 0, 0, time.UTC)) ||
			paris.TimeZone != "Europe/Paris" {
			t.Errorf("Paris months start at %s (time zone %s)", paris.Months[0].Start, paris.TimeZone)
		}
	})

	t.Run("under five estimates, no current capacity", func(t *testing.T) {
		e, c := readerEnv(t)
		at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
		dc := batteryCharge(at.AddDate(0, 0, 15), core.DC, 40, 30, 24200)
		e.reader.charges[car] = []core.Charge{
			batteryCharge(at, core.AC, 50, 37.5, 24000),
			batteryCharge(at.AddDate(0, 0, 7), core.AC, 25, 18.75, 24100),
			dc,
		}
		e.reader.entered = append(e.reader.entered, batteryReceipt(dc, 32, 100))
		e.reader.trips[car] = []core.Trip{
			batteryTrip(at.AddDate(0, 0, 7), 200), batteryTrip(at.AddDate(0, 0, 14), 202),
		}
		base := e.api.URL + "/api/v1/vehicles/" + car + "/battery"
		_, body := get(t, noFollow(), base, c)
		golden(t, "battery-empty.json", body)
		b := readBattery(t, base, c)
		if b.Current != nil || b.DeviationPct != nil || b.Change != nil {
			t.Errorf("current, deviation and change = %+v %v %+v, want none", b.Current, b.DeviationPct, b.Change)
		}
		if b.Reference == nil || b.Reference.Source != "catalog_net" || !near(b.Reference.CapacityKWh, 75) {
			t.Errorf("reference = %+v", b.Reference)
		}
		if b.Cycles == nil || !near(*b.Cycles, 1.15) {
			t.Errorf("cycles = %v", b.Cycles)
		}
		if len(b.Estimates) != 4 || len(b.Months) != 1 || b.Months[0].Capacity == nil {
			t.Errorf("%d estimates, %d months, first month %+v", len(b.Estimates), len(b.Months), b.Months[0])
		}
	})

	t.Run("without a variant, the reference is the vendor's", func(t *testing.T) {
		e, c := readerEnv(t)
		e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 0), Snapshot: details("EX-SIM", 2026, 80, true)}}
		aug := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
		dc1 := batteryCharge(aug.AddDate(0, 0, 15), core.DC, 40, 30, 23200)
		dc2 := batteryCharge(aug.AddDate(0, 1, 7), core.DC, 40, 30, 23400)
		e.reader.charges[car] = []core.Charge{
			batteryCharge(aug, core.AC, 50, 37.5, 23000),
			batteryCharge(aug.AddDate(0, 0, 7), core.AC, 25, 18.75, 23100),
			dc1,
			batteryCharge(aug.AddDate(0, 1, 0), core.AC, 50, 37.5, 23300),
			dc2,
		}
		e.reader.entered = append(e.reader.entered, batteryReceipt(dc1, 32, 100), batteryReceipt(dc2, 32, 101))
		e.reader.trips[car] = []core.Trip{
			batteryTrip(aug.AddDate(0, 0, 2), 200), batteryTrip(aug.AddDate(0, 0, 9), 202),
		}
		base := e.api.URL + "/api/v1/vehicles/" + car + "/battery"
		_, body := get(t, noFollow(), base, c)
		golden(t, "battery-api-capacity.json", body)
		b := readBattery(t, base, c)
		if b.Reference == nil || b.Reference.Source != "api" || !near(b.Reference.CapacityKWh, 80) {
			t.Errorf("reference = %+v", b.Reference)
		}
		// Enough estimates for a current capacity, and a deviation from the vendor's
		// gross capacity; no evolution, the history is too short.
		if b.Current == nil || b.Current.Estimates != 7 || b.DeviationPct == nil || *b.DeviationPct != -6 {
			t.Errorf("current = %+v, deviation = %v", b.Current, b.DeviationPct)
		}
		if b.Change != nil {
			t.Errorf("change = %+v, want none", b.Change)
		}
	})

	t.Run("the reference follows the variant in effect", func(t *testing.T) {
		at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
		one := func() []core.Charge {
			return []core.Charge{batteryCharge(at, core.AC, 50, 37.5, 24000)}
		}
		// The variant the driver chose, as the one the details recognize.
		e, c := readerEnv(t)
		e.reader.charges[chosen] = one()
		if b := readBattery(t, e.api.URL+"/api/v1/vehicles/"+chosen+"/battery", c); b.Reference == nil ||
			b.Reference.Source != "catalog_net" || !near(b.Reference.CapacityKWh, 64) {
			t.Errorf("chosen EX30: reference = %+v", b.Reference)
		}
		// No details were ever read: no reference, no deviation, no cycles, however
		// many estimates.
		e, c = readerEnv(t)
		month := func(d int) core.Charge { return batteryCharge(at.AddDate(0, 0, d), core.AC, 50, 37.5, 24000) }
		e.reader.charges[parked] = []core.Charge{month(0), month(2), month(4), month(6), month(8)}
		b := readBattery(t, e.api.URL+"/api/v1/vehicles/"+parked+"/battery", c)
		if b.Reference != nil || b.DeviationPct != nil || b.Cycles != nil {
			t.Errorf("no details: reference = %+v, deviation = %v, cycles = %v", b.Reference, b.DeviationPct, b.Cycles)
		}
		if b.Current == nil || b.Current.Estimates != 5 {
			t.Errorf("current = %+v: the estimates stand without a reference", b.Current)
		}
	})

	t.Run("a receipt gives a second estimate, without its energy none", func(t *testing.T) {
		e, c := readerEnv(t)
		at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
		e.reader.charges[car] = []core.Charge{batteryCharge(at, core.AC, 50, 37.5, 24000)}
		base := e.api.URL + "/api/v1/vehicles/" + car
		id := at.Add(-time.Minute).Format("2006-01-02T15:04:05Z") // an RFC 3339 form of the ID

		// A cost entered without its billed energy: the charge keeps one estimate.
		resp, body := do(t, noFollow(), http.MethodPut, base+"/charges/"+id+"/cost",
			"application/json", `{"amount_minor":850,"energy_kwh":null,"note":null}`, c)
		if resp.status != http.StatusOK {
			t.Fatalf("enter cost: %d %s", resp.status, body)
		}
		if b := readBattery(t, base+"/battery", c); len(b.Estimates) != 1 || b.Estimates[0].Source != "power" {
			t.Errorf("estimates = %+v, want the power one only", b.Estimates)
		}
		// With the energy of the receipt: the charge gives two points, power first.
		resp, body = do(t, noFollow(), http.MethodPut, base+"/charges/"+id+"/cost",
			"application/json", `{"amount_minor":850,"energy_kwh":40,"note":null}`, c)
		if resp.status != http.StatusOK {
			t.Fatalf("enter cost again: %d %s", resp.status, body)
		}
		b := readBattery(t, base+"/battery", c)
		if len(b.Estimates) != 2 || b.Estimates[0].Source != "power" || b.Estimates[1].Source != "billed" ||
			b.Estimates[0].Charge != b.Estimates[1].Charge {
			t.Errorf("estimates = %+v, want the power and the billed ones of the charge", b.Estimates)
		}
	})

	t.Run("invalid parameters", func(t *testing.T) {
		e, c := readerEnv(t)
		base := e.api.URL + "/api/v1/vehicles/" + car + "/battery"
		for q, want := range map[string]string{
			"?tz=Mars/Olympus_Mons": "tz",
			"?tz=Local":             "tz",
			"?tz=../../etc/passwd":  "tz",
		} {
			resp, body := get(t, noFollow(), base+q, c)
			if resp.status != http.StatusBadRequest || !strings.Contains(body, want) {
				t.Errorf("%s: %d %s", q, resp.status, body)
			}
			wantError(t, body, "invalid_parameter")
		}
		// More than 400 months of history: refused as the statistics are. Thirty-six
		// years should never happen, the collector stores nothing that old.
		old := time.Date(1990, 1, 5, 12, 0, 0, 0, time.UTC)
		e.reader.charges[car] = []core.Charge{
			batteryCharge(old, core.AC, 50, 37.5, 1000),
			batteryCharge(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), core.AC, 50, 37.5, 24000),
		}
		resp, body := get(t, noFollow(), base, c)
		if resp.status != http.StatusBadRequest || !strings.Contains(body, "400 months") {
			t.Errorf("36 years of history: %d %s", resp.status, body)
		}
		wantError(t, body, "invalid_parameter")
	})
}
