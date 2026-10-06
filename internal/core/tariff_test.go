package core

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var london = mustLoad("Europe/London")

// lt is a wall time in London, "2026-03-29 08:00".
func lt(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, london)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// hm is a time of day in minutes, "22:30".
func hm(s string) int {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		panic(err)
	}
	return h*60 + m
}

// window is a price window of every day and month, "22:00-06:00".
func window(span string, price float64) PriceWindow {
	from, to, _ := strings.Cut(span, "-")
	return PriceWindow{From: hm(from), To: hm(to), PricePerKWh: price}
}

func date(s string) Date {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return dateOf(t, time.UTC)
}

// seg is a price segment, "2026-06-01T20:00:00Z 2026-06-01T22:00:00Z 0.2142", for
// readable failures.
func segs(ss []PriceSegment) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%s %s %g", s.From.Format(time.RFC3339), s.To.Format(time.RFC3339), s.PricePerKWh)
	}
	return out
}

func seg(from, to time.Time, price float64) string {
	return segs([]PriceSegment{{from, to, price}})[0]
}

// hphc is the French off-peak tariff of the example of the ADR: 21.42 c€ by day, 15.89
// from 22:00 to 06:00.
func hphc(from string) TariffVersion {
	return TariffVersion{ValidFrom: date(from), PricePerKWh: 0.2142, Windows: []PriceWindow{window("22:00-06:00", 0.1589)}}
}

func TestLocalTime(t *testing.T) {
	for _, tt := range []struct {
		name    string
		loc     *time.Location
		day     string
		minutes string
		want    string
	}{
		{"Paris, winter", paris, "2026-01-15", "22:00", "2026-01-15T21:00:00Z"},
		{"Paris, summer", paris, "2026-07-15", "22:00", "2026-07-15T20:00:00Z"},
		// 29 March 2026: 02:00 CET becomes 03:00 CEST, at 01:00 UTC.
		{"Paris, before the gap", paris, "2026-03-29", "01:59", "2026-03-29T00:59:00Z"},
		{"Paris, start of the gap", paris, "2026-03-29", "02:00", "2026-03-29T01:00:00Z"},
		// time.Date gives 03:30 CEST (01:30 UTC); the clock reads 02:30 or later from the
		// change on.
		{"Paris, in the gap", paris, "2026-03-29", "02:30", "2026-03-29T01:00:00Z"},
		{"Paris, after the gap", paris, "2026-03-29", "03:00", "2026-03-29T01:00:00Z"},
		// 25 October 2026: 03:00 CEST becomes 02:00 CET, at 01:00 UTC.
		{"Paris, repeated hour: the first", paris, "2026-10-25", "02:30", "2026-10-25T00:30:00Z"},
		{"Paris, start of the repeated hour", paris, "2026-10-25", "02:00", "2026-10-25T00:00:00Z"},
		{"Paris, after the repeated hour", paris, "2026-10-25", "03:00", "2026-10-25T02:00:00Z"},
		// 29 March 2026: 01:00 GMT becomes 02:00 BST.
		{"London, in the gap", london, "2026-03-29", "01:30", "2026-03-29T01:00:00Z"},
		// 25 October 2026: 02:00 BST becomes 01:00 GMT; time.Date takes 01:00 GMT.
		{"London, repeated hour: the first", london, "2026-10-25", "01:00", "2026-10-25T00:00:00Z"},
		{"midnight", paris, "2026-10-25", "00:00", "2026-10-24T22:00:00Z"},
		{"UTC", time.UTC, "2026-03-29", "02:30", "2026-03-29T02:30:00Z"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := localTime(date(tt.day), hm(tt.minutes), tt.loc).Format(time.RFC3339); got != tt.want {
				t.Errorf("localTime = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTariffSegments(t *testing.T) {
	weekend := PriceWindow{Days: []time.Weekday{time.Saturday, time.Sunday}, From: 0, To: 0, PricePerKWh: 0.10}
	summer := []time.Month{4, 5, 6, 7, 8, 9, 10}
	winter := []time.Month{11, 12, 1, 2, 3}
	// The French off-peak reform: 02:00–07:00 in summer, 23:00–07:00 in winter.
	seasons := TariffVersion{ValidFrom: date("2026-01-01"), PricePerKWh: 0.2142, Windows: []PriceWindow{
		{From: hm("02:00"), To: hm("07:00"), Months: summer, PricePerKWh: 0.1589},
		{From: hm("23:00"), To: hm("07:00"), Months: winter, PricePerKWh: 0.1489},
	}}
	for _, tt := range []struct {
		name     string
		tariff   Tariff
		from, to time.Time
		want     []string
	}{
		{
			name:   "base price only",
			tariff: Tariff{paris, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.2}}},
			from:   pt("2026-06-01 20:00"), to: pt("2026-06-03 08:00"),
			want: []string{seg(pt("2026-06-01 20:00"), pt("2026-06-03 08:00"), 0.2)},
		},
		{
			name:   "window across midnight",
			tariff: Tariff{paris, []TariffVersion{hphc("2026-01-01")}},
			from:   pt("2026-06-01 20:20"), to: pt("2026-06-02 08:00"),
			want: []string{
				seg(pt("2026-06-01 20:20"), pt("2026-06-01 22:00"), 0.2142),
				seg(pt("2026-06-01 22:00"), pt("2026-06-02 06:00"), 0.1589),
				seg(pt("2026-06-02 06:00"), pt("2026-06-02 08:00"), 0.2142),
			},
		},
		{
			// 1 June 2026 is a Monday: the window of Sunday is not, nor the early hours
			// of Monday; Monday night is, into Tuesday.
			name: "days: those the window starts on",
			tariff: Tariff{paris, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.2, Windows: []PriceWindow{
				{Days: []time.Weekday{time.Monday}, From: hm("22:00"), To: hm("06:00"), PricePerKWh: 0.1},
			}}}},
			from: pt("2026-05-31 20:00"), to: pt("2026-06-02 08:00"),
			want: []string{
				seg(pt("2026-05-31 20:00"), pt("2026-06-01 22:00"), 0.2),
				seg(pt("2026-06-01 22:00"), pt("2026-06-02 06:00"), 0.1),
				seg(pt("2026-06-02 06:00"), pt("2026-06-02 08:00"), 0.2),
			},
		},
		{
			name:   "months: summer",
			tariff: Tariff{paris, []TariffVersion{seasons}},
			from:   pt("2026-06-01 20:00"), to: pt("2026-06-02 08:00"),
			want: []string{
				seg(pt("2026-06-01 20:00"), pt("2026-06-02 02:00"), 0.2142),
				seg(pt("2026-06-02 02:00"), pt("2026-06-02 07:00"), 0.1589),
				seg(pt("2026-06-02 07:00"), pt("2026-06-02 08:00"), 0.2142),
			},
		},
		{
			name:   "months: winter",
			tariff: Tariff{paris, []TariffVersion{seasons}},
			from:   pt("2026-12-01 20:00"), to: pt("2026-12-02 08:00"),
			want: []string{
				seg(pt("2026-12-01 20:00"), pt("2026-12-01 23:00"), 0.2142),
				seg(pt("2026-12-01 23:00"), pt("2026-12-02 07:00"), 0.1489),
				seg(pt("2026-12-02 07:00"), pt("2026-12-02 08:00"), 0.2142),
			},
		},
		{
			// Winter starts at midnight: its window of 31 October does not apply before,
			// and holds the early hours of 1 November; the summer one does not.
			name:   "months: summer to winter at midnight",
			tariff: Tariff{paris, []TariffVersion{seasons}},
			from:   pt("2026-10-31 20:00"), to: pt("2026-11-01 08:00"),
			want: []string{
				seg(pt("2026-10-31 20:00"), pt("2026-11-01 00:00"), 0.2142),
				seg(pt("2026-11-01 00:00"), pt("2026-11-01 07:00"), 0.1489),
				seg(pt("2026-11-01 07:00"), pt("2026-11-01 08:00"), 0.2142),
			},
		},
		{
			name:   "months: winter to summer at midnight",
			tariff: Tariff{paris, []TariffVersion{seasons}},
			from:   pt("2026-03-31 20:00"), to: pt("2026-04-01 08:00"),
			want: []string{
				seg(pt("2026-03-31 20:00"), pt("2026-03-31 23:00"), 0.2142),
				seg(pt("2026-03-31 23:00"), pt("2026-04-01 00:00"), 0.1489),
				seg(pt("2026-04-01 00:00"), pt("2026-04-01 02:00"), 0.2142),
				seg(pt("2026-04-01 02:00"), pt("2026-04-01 07:00"), 0.1589),
				seg(pt("2026-04-01 07:00"), pt("2026-04-01 08:00"), 0.2142),
			},
		},
		{
			// 6 June 2026 is a Saturday. The night window comes last: it wins over the
			// weekend price.
			name: "the last window wins",
			tariff: Tariff{paris, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.2, Windows: []PriceWindow{
				weekend, window("22:00-06:00", 0.15),
			}}}},
			from: pt("2026-06-05 20:00"), to: pt("2026-06-06 23:00"),
			want: []string{
				seg(pt("2026-06-05 20:00"), pt("2026-06-05 22:00"), 0.2),
				seg(pt("2026-06-05 22:00"), pt("2026-06-06 06:00"), 0.15),
				seg(pt("2026-06-06 06:00"), pt("2026-06-06 22:00"), 0.10),
				seg(pt("2026-06-06 22:00"), pt("2026-06-06 23:00"), 0.15),
			},
		},
		{
			name: "the last window wins, the other way round",
			tariff: Tariff{paris, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.2, Windows: []PriceWindow{
				window("22:00-06:00", 0.15), weekend,
			}}}},
			from: pt("2026-06-05 20:00"), to: pt("2026-06-06 23:00"),
			want: []string{
				seg(pt("2026-06-05 20:00"), pt("2026-06-05 22:00"), 0.2),
				seg(pt("2026-06-05 22:00"), pt("2026-06-06 00:00"), 0.15),
				seg(pt("2026-06-06 00:00"), pt("2026-06-06 23:00"), 0.10),
			},
		},
		{
			// 29 March 2026 in Paris: 01:00 CET to 03:00 CEST lasts one hour.
			name:   "Paris, clocks forward",
			tariff: Tariff{paris, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.2, Windows: []PriceWindow{window("01:00-03:00", 0.1)}}}},
			from:   utc("2026-03-28T23:00:00Z"), to: utc("2026-03-29T03:00:00Z"),
			want: []string{
				seg(utc("2026-03-28T23:00:00Z"), utc("2026-03-29T00:00:00Z"), 0.2),
				seg(utc("2026-03-29T00:00:00Z"), utc("2026-03-29T01:00:00Z"), 0.1),
				seg(utc("2026-03-29T01:00:00Z"), utc("2026-03-29T03:00:00Z"), 0.2),
			},
		},
		{
			// A window within the skipped hour never applies.
			name:   "Paris, a window in the skipped hour",
			tariff: Tariff{paris, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.2, Windows: []PriceWindow{window("02:00-03:00", 0.1)}}}},
			from:   utc("2026-03-28T23:00:00Z"), to: utc("2026-03-29T03:00:00Z"),
			want: []string{seg(utc("2026-03-28T23:00:00Z"), utc("2026-03-29T03:00:00Z"), 0.2)},
		},
		{
			// 25 October 2026 in Paris: 01:00 CEST to 03:00 CET lasts three hours.
			name:   "Paris, clocks back",
			tariff: Tariff{paris, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.2, Windows: []PriceWindow{window("01:00-03:00", 0.1)}}}},
			from:   utc("2026-10-24T22:00:00Z"), to: utc("2026-10-25T04:00:00Z"),
			want: []string{
				seg(utc("2026-10-24T22:00:00Z"), utc("2026-10-24T23:00:00Z"), 0.2),
				seg(utc("2026-10-24T23:00:00Z"), utc("2026-10-25T02:00:00Z"), 0.1),
				seg(utc("2026-10-25T02:00:00Z"), utc("2026-10-25T04:00:00Z"), 0.2),
			},
		},
		{
			// 29 March 2026 in London: 01:00 GMT becomes 02:00 BST; the window opens at
			// the change and ends at 03:00 BST.
			name:   "London, clocks forward",
			tariff: Tariff{london, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.3099, Windows: []PriceWindow{window("01:00-03:00", 0.0863)}}}},
			from:   lt("2026-03-29 00:00"), to: lt("2026-03-29 04:00"),
			want: []string{
				seg(utc("2026-03-29T00:00:00Z"), utc("2026-03-29T01:00:00Z"), 0.3099),
				seg(utc("2026-03-29T01:00:00Z"), utc("2026-03-29T02:00:00Z"), 0.0863),
				seg(utc("2026-03-29T02:00:00Z"), utc("2026-03-29T03:00:00Z"), 0.3099),
			},
		},
		{
			// 25 October 2026 in London: from the first 01:00 (BST) to 03:00 GMT.
			name:   "London, clocks back",
			tariff: Tariff{london, []TariffVersion{{ValidFrom: date("2026-01-01"), PricePerKWh: 0.3099, Windows: []PriceWindow{window("01:00-03:00", 0.0863)}}}},
			from:   utc("2026-10-24T23:00:00Z"), to: utc("2026-10-25T04:00:00Z"),
			want: []string{
				seg(utc("2026-10-24T23:00:00Z"), utc("2026-10-25T00:00:00Z"), 0.3099),
				seg(utc("2026-10-25T00:00:00Z"), utc("2026-10-25T03:00:00Z"), 0.0863),
				seg(utc("2026-10-25T03:00:00Z"), utc("2026-10-25T04:00:00Z"), 0.3099),
			},
		},
		{
			name: "Octopus Go in local time, summer",
			tariff: Tariff{london, []TariffVersion{{
				ValidFrom: date("2026-01-01"), PricePerKWh: 0.3099,
				Windows: []PriceWindow{window("00:30-05:30", 0.0863)},
			}}},
			from: lt("2026-07-01 00:00"), to: lt("2026-07-01 06:00"),
			want: []string{
				seg(utc("2026-06-30T23:00:00Z"), utc("2026-06-30T23:30:00Z"), 0.3099),
				seg(utc("2026-06-30T23:30:00Z"), utc("2026-07-01T04:30:00Z"), 0.0863),
				seg(utc("2026-07-01T04:30:00Z"), utc("2026-07-01T05:00:00Z"), 0.3099),
			},
		},
		{
			name: "a new version in the middle",
			tariff: Tariff{paris, []TariffVersion{
				{ValidFrom: date("2026-08-01"), PricePerKWh: 0.25},
				hphc("2026-02-01"), // unsorted
			}},
			from: pt("2026-07-31 20:00"), to: pt("2026-08-01 02:00"),
			// The version of the instant decides: the night window of 31 July ends at
			// midnight.
			want: []string{
				seg(pt("2026-07-31 20:00"), pt("2026-07-31 22:00"), 0.2142),
				seg(pt("2026-07-31 22:00"), pt("2026-08-01 00:00"), 0.1589),
				seg(pt("2026-08-01 00:00"), pt("2026-08-01 02:00"), 0.25),
			},
		},
		{
			name:   "no price before the first version",
			tariff: Tariff{paris, []TariffVersion{hphc("2026-08-01")}},
			from:   pt("2026-07-31 23:00"), to: pt("2026-08-01 02:00"),
			want: []string{seg(pt("2026-08-01 00:00"), pt("2026-08-01 02:00"), 0.1589)},
		},
		{
			name: "two versions of the same day: the latter wins",
			tariff: Tariff{paris, []TariffVersion{
				{ValidFrom: date("2026-08-01"), PricePerKWh: 0.25}, {ValidFrom: date("2026-08-01"), PricePerKWh: 0.30},
			}},
			from: pt("2026-08-01 10:00"), to: pt("2026-08-01 12:00"),
			want: []string{seg(pt("2026-08-01 10:00"), pt("2026-08-01 12:00"), 0.30)},
		},
		{
			name:   "no zone: UTC",
			tariff: Tariff{nil, []TariffVersion{hphc("2026-01-01")}},
			from:   utc("2026-06-01T21:00:00Z"), to: utc("2026-06-01T23:00:00Z"),
			want: []string{
				seg(utc("2026-06-01T21:00:00Z"), utc("2026-06-01T22:00:00Z"), 0.2142),
				seg(utc("2026-06-01T22:00:00Z"), utc("2026-06-01T23:00:00Z"), 0.1589),
			},
		},
		{name: "no version", tariff: Tariff{paris, nil}, from: pt("2026-06-01 10:00"), to: pt("2026-06-01 12:00")},
		{name: "empty interval", tariff: Tariff{paris, []TariffVersion{hphc("2026-01-01")}}, from: pt("2026-06-01 10:00"), to: pt("2026-06-01 10:00")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := segs(tt.tariff.Segments(tt.from, tt.to))
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("segments =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}
