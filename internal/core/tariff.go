package core

import (
	"cmp"
	"slices"
	"time"
)

// PriceSchedule gives the price per kWh over [from, to), as consecutive segments.
// A gap is a time without a known price.
type PriceSchedule interface {
	Segments(from, to time.Time) []PriceSegment
}

// PriceSegment is a time [From, To) at a constant price per kWh, in major units of the
// account's currency.
type PriceSegment struct {
	From, To    time.Time
	PricePerKWh float64
}

// Date is a civil date, without a time or a zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// dateOf is the date of t in loc.
func dateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{y, m, d}
}

func (d Date) addDays(n int) Date {
	return dateOf(time.Date(d.Year, d.Month, d.Day+n, 12, 0, 0, 0, time.UTC), time.UTC)
}

func (d Date) compare(o Date) int {
	return cmp.Or(cmp.Compare(d.Year, o.Year), cmp.Compare(d.Month, o.Month), cmp.Compare(d.Day, o.Day))
}

// TariffVersion is a tariff valid from the start of a day, in the place's zone, until the
// next version.
type TariffVersion struct {
	ValidFrom   Date
	PricePerKWh float64 // outside any window
	// Windows are tried in order: the last one that holds an instant gives its price.
	Windows []PriceWindow
}

// PriceWindow is a time of the day with its own price. From and To are minutes since
// midnight, in local time; To before From crosses midnight, To equal to From lasts a full
// day from From.
//
// Days are those the window starts on: Monday 22:00–06:00 covers the night from Monday
// to Tuesday, not the early hours of Monday. Months are those of the instant: a season
// is a span of the calendar, which starts and ends at midnight, so a winter window
// 23:00–07:00 (November to March) holds the early hours of the 1st of November, and not
// those of the 1st of April. Assumption: meters switch seasons at midnight, as the
// French off-peak reform states its seasons in dates (1 April to 31 October). Empty Days
// or Months mean every day or every month.
type PriceWindow struct {
	Days        []time.Weekday
	From, To    int
	Months      []time.Month
	PricePerKWh float64
}

// opensOn tells whether the window opens on day d.
func (w PriceWindow) opensOn(d Date) bool {
	t := time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC)
	return len(w.Days) == 0 || slices.Contains(w.Days, t.Weekday())
}

// inMonth tells whether the window applies in month m.
func (w PriceWindow) inMonth(m time.Month) bool {
	return len(w.Months) == 0 || slices.Contains(w.Months, m)
}

// on is the window opening on day d, in loc.
func (w PriceWindow) on(d Date, loc *time.Location) (start, end time.Time) {
	endDay := d
	if w.To <= w.From {
		endDay = d.addDays(1)
	}
	return localTime(d, w.From, loc), localTime(endDay, w.To, loc)
}

// localTime is the first instant at which the clock of loc shows minutes past midnight
// of day d, or a later time.
//
// time.Date alone would not do across a change of time: of a wall time shown twice, it
// takes the second instant, and it moves a skipped one forward by the length of the gap
// (02:30 becomes 03:30 when clocks go from 02:00 to 03:00). Here a skipped time is the
// change itself, where the clock jumps over it, and a repeated one its first instant.
// Assumption: a meter switches price as soon as its clock reads the limit; how meters
// handle the repeated hour is not documented.
func localTime(d Date, minutes int, loc *time.Location) time.Time {
	wall := time.Date(d.Year, d.Month, d.Day, 0, minutes, 0, 0, time.UTC)
	t := time.Date(d.Year, d.Month, d.Day, 0, minutes, 0, 0, loc)
	var first, latest time.Time
	// The offsets in force around t: those of both sides of a change.
	for _, near := range []time.Time{t.Add(-12 * time.Hour), t, t.Add(12 * time.Hour)} {
		_, off := near.In(loc).Zone()
		c := wall.Add(-time.Duration(off) * time.Second)
		if _, o := c.In(loc).Zone(); o == off && (first.IsZero() || c.Before(first)) {
			first = c
		}
		if c.After(latest) {
			latest = c
		}
	}
	if first.IsZero() { // skipped: the latest candidate lies after the change
		start, _ := latest.In(loc).ZoneBounds()
		return start.UTC()
	}
	return first.UTC()
}

// Tariff is a place's tariff: its versions, in the place's zone. It is the first
// PriceSchedule; Tempo and dynamic prices will be others.
type Tariff struct {
	Location *time.Location // UTC if nil
	Versions []TariffVersion
}

// Segments splits [from, to) where the price changes: at the limits of the windows, at
// midnight, at a change of version. There is no price before the first version.
func (t Tariff) Segments(from, to time.Time) []PriceSegment {
	if !from.Before(to) || len(t.Versions) == 0 {
		return nil
	}
	loc := t.Location
	if loc == nil {
		loc = time.UTC
	}
	versions := slices.Clone(t.Versions)
	// Stable: of two versions from the same day, the latter wins.
	slices.SortStableFunc(versions, func(a, b TariffVersion) int { return a.ValidFrom.compare(b.ValidFrom) })

	// Every instant where the price may change. The windows of a day's version open on
	// that day or the day before (crossing midnight), so the day before from is included.
	cuts := []time.Time{from.UTC(), to.UTC()}
	last := dateOf(to, loc)
	for d := dateOf(from, loc).addDays(-1); d.compare(last) <= 0; d = d.addDays(1) {
		cuts = append(cuts, localTime(d, 0, loc))
		for _, day := range []Date{d, d.addDays(1)} {
			v, ok := versionOn(versions, day)
			if !ok {
				continue
			}
			for _, w := range v.Windows {
				start, end := w.on(d, loc)
				cuts = append(cuts, start, end)
			}
		}
	}
	cuts = slices.DeleteFunc(cuts, func(c time.Time) bool { return c.Before(from) || c.After(to) })
	slices.SortFunc(cuts, time.Time.Compare)
	cuts = slices.CompactFunc(cuts, time.Time.Equal)

	var out []PriceSegment
	for i := 1; i < len(cuts); i++ {
		a, b := cuts[i-1], cuts[i]
		p, ok := priceAt(versions, a, loc)
		if !ok {
			continue
		}
		if n := len(out); n > 0 && out[n-1].To.Equal(a) && out[n-1].PricePerKWh == p {
			out[n-1].To = b
			continue
		}
		out = append(out, PriceSegment{a, b, p})
	}
	return out
}

// versionOn is the version valid on day d: the latest that starts on or before it.
func versionOn(versions []TariffVersion, d Date) (TariffVersion, bool) {
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].ValidFrom.compare(d) <= 0 {
			return versions[i], true
		}
	}
	return TariffVersion{}, false
}

// priceAt is the price at t: the version valid on t's day decides, and the last of its
// windows that holds t, opened that day or the day before and applying in t's month,
// gives the price.
func priceAt(versions []TariffVersion, t time.Time, loc *time.Location) (float64, bool) {
	d := dateOf(t, loc)
	v, ok := versionOn(versions, d)
	if !ok {
		return 0, false
	}
	price := v.PricePerKWh
	for _, w := range v.Windows {
		if !w.inMonth(d.Month) {
			continue
		}
		for _, day := range []Date{d.addDays(-1), d} {
			if !w.opensOn(day) {
				continue
			}
			if start, end := w.on(day, loc); !t.Before(start) && t.Before(end) {
				price = w.PricePerKWh
			}
		}
	}
	return price, true
}
