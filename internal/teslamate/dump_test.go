package teslamate

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// testdata/teslamate-v4.4.0.bck is pg_dump's backup of a database TeslaMate 4.4.0 migrated
// (its image, against PostgreSQL 17), holding synthetic rows: no real VIN nor position.
// testdata/teslamate-v1.20.0.bck holds the same rows in 1.20.0's schema (2020, PostgreSQL
// 12), but the columns that release did not have yet.

var fixtureTables = []Table{
	{Name: "cars", Columns: []string{"id", "vin", "name", "efficiency", "trim_badging"}},
	{Name: "drives", Columns: []string{"id", "start_date", "end_date", "distance", "start_rated_range_km"}},
	{Name: "positions", Columns: []string{"drive_id", "date", "latitude", "longitude"}},
	{Name: "charging_processes", Columns: []string{"id", "start_date", "charge_energy_added", "cost"}},
	{Name: "charges", Columns: []string{"charging_process_id", "fast_charger_present", "charger_phases"}},
	{Name: "updates", Columns: []string{"version", "start_date"}},
}

func readFixture(t *testing.T, tables []Table, each func(Row) error) error {
	t.Helper()
	f, err := os.Open("testdata/teslamate-v4.4.0.bck")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return Read(f, tables, each)
}

func TestReadFixture(t *testing.T) {
	var order []string
	counts := map[string]int{}
	err := readFixture(t, fixtureTables, func(r Row) error {
		if counts[r.Table()] == 0 {
			order = append(order, r.Table())
		}
		counts[r.Table()]++
		switch r.Table() {
		case "cars":
			id, _ := r.Int("id")
			eff, ok := r.Float("efficiency")
			switch *id {
			case 1:
				if *r.Text("vin") != "SYNTHETIC0000VIN1" || *r.Text("name") != "Blue\tback\\slash \"été\"" ||
					!ok || eff == nil || *eff != 0.153 || *r.Text("trim_badging") != "74D" {
					t.Errorf("car 1: %q %q %v", *r.Text("vin"), *r.Text("name"), eff)
				}
			case 2:
				if r.Text("name") != nil || !ok || eff != nil || r.Text("trim_badging") != nil {
					t.Errorf("car 2: NULLs read as values")
				}
			}
		case "drives":
			id, _ := r.Int("id")
			start, ok := r.Time("start_date")
			end, okEnd := r.Time("end_date")
			dist, okDist := r.Float("distance")
			if !ok || !okEnd || !okDist {
				t.Fatalf("drive %d unreadable", *id)
			}
			if *id == 1 && (!start.Equal(time.Date(2024, 5, 1, 7, 0, 0, 0, time.UTC)) || end == nil || *dist != 10) {
				t.Errorf("drive 1: %v %v %v", start, end, dist)
			}
			if *id == 2 && (end != nil || dist != nil) {
				t.Errorf("drive 2 unfinished: %v %v", end, dist)
			}
		case "positions":
			at, ok := r.Time("date")
			if !ok {
				t.Fatal("a position's date unreadable")
			}
			if at.Equal(time.Date(2024, 5, 1, 7, 0, 30, 250e6, time.UTC)) {
				lat, _ := r.Float("latitude")
				if *lat != 48.857 {
					t.Errorf("latitude %v", *lat)
				}
			}
		case "charging_processes":
			cost, ok := r.Float("cost")
			if !ok || cost == nil || *cost != 6.02 {
				t.Errorf("cost %v", cost)
			}
		case "charges":
			fast, ok := r.Bool("fast_charger_present")
			if !ok || fast == nil || *fast {
				t.Errorf("fast charger %v", fast)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"cars": 2, "drives": 2, "positions": 7, "charging_processes": 1, "charges": 3, "updates": 2}
	for table, n := range want {
		if counts[table] != n {
			t.Errorf("%s: %d rows, want %d", table, counts[table], n)
		}
	}
	// pg_dump's order, by name: a converter cannot count on a drive before its positions.
	if got := strings.Join(order, ","); got != "cars,charges,charging_processes,drives,positions,updates" {
		t.Errorf("order %s", got)
	}
}

func TestReadFixtureFailures(t *testing.T) {
	stop := errors.New("stop")
	if err := readFixture(t, fixtureTables, func(Row) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("callback's error: %v", err)
	}
	missing := []Table{{Name: "drives", Columns: []string{"id", "distance_km"}}}
	var fe *FormatError
	if err := readFixture(t, missing, func(Row) error { return nil }); !errors.As(err, &fe) ||
		fe.Reason != "table drives has no column distance_km" {
		t.Errorf("missing column: %v", err)
	}
	absent := []Table{{Name: "drives", Columns: []string{"id"}}, {Name: "trips", Columns: []string{"id"}}}
	if err := readFixture(t, absent, func(Row) error { return nil }); !errors.As(err, &fe) || fe.Reason != "no table trips" {
		t.Errorf("missing table: %v", err)
	}
}

func gzipped(s string) string {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

func TestReadGzip(t *testing.T) {
	plain, err := os.ReadFile("testdata/teslamate-v4.4.0.bck")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	err = Read(strings.NewReader(gzipped(string(plain))), fixtureTables[:2], func(Row) error { n++; return nil })
	if err != nil || n != 4 { // two cars, two drives
		t.Errorf("%d rows, %v", n, err)
	}
}

func TestReadFormats(t *testing.T) {
	gz := bytes.NewBufferString(gzipped("-- PostgreSQL database dump\n"))
	for name, tc := range map[string]struct {
		in   string
		want error
	}{
		"custom":     {"PGDMP\x01\x0f\x00", ErrCustomFormat},
		"gzip twice": {gzipped(gz.String()), ErrCompressed},
		"bad gzip":   {"\x1f\x8b\x00", &FormatError{Reason: "a gzip file not understood: unexpected EOF"}},
		"zstd":       {"\x28\xb5\x2f\xfd....", ErrCompressed},
		"inserts":    {"INSERT INTO public.drives VALUES (1, 2);\n", ErrInserts},
		"empty":      {"", &FormatError{Reason: "empty"}},
		"other text": {"hello\n", &FormatError{Reason: "no table drives"}},
	} {
		t.Run(name, func(t *testing.T) {
			err := Read(strings.NewReader(tc.in), []Table{{Name: "drives", Columns: []string{"id"}}},
				func(Row) error { return nil })
			var fe, want *FormatError
			if !errors.As(err, &fe) || !errors.As(tc.want, &want) || fe.Reason != want.Reason {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReadDump(t *testing.T) {
	long := strings.Repeat("x", 3*bufferSize)
	for name, tc := range map[string]struct {
		dump   string
		table  Table
		want   []string // the first column of each row, "NULL" for NULL
		reason string   // the FormatError's, when it fails
	}{
		"quoted names": {
			dump:  "COPY public.\"Odd\"\"Name\" (id, \"row\") FROM stdin;\n1\ta\n\\.\n",
			table: Table{Name: `Odd"Name`, Columns: []string{"row"}},
			want:  []string{"a"},
		},
		"unqualified, CRLF": {
			dump:  "COPY drives (id) FROM stdin;\r\n7\r\n\\N\r\n\\.\r\n",
			table: Table{Name: "drives", Columns: []string{"id"}},
			want:  []string{"7", "NULL"},
		},
		"long lines skipped": {
			dump: "CREATE FUNCTION f() AS '" + long + "';\nCOPY other (a) FROM stdin;\n" + long + "\n\\.\n" +
				"COPY drives (id) FROM stdin;\n1\n\\.\n",
			table: Table{Name: "drives", Columns: []string{"id"}},
			want:  []string{"1"},
		},
		"last line without its end": {
			dump:  "COPY drives (id) FROM stdin;\n1\n\\.",
			table: Table{Name: "drives", Columns: []string{"id"}},
			want:  []string{"1"},
		},
		"the first block only": {
			dump:  "COPY drives (id) FROM stdin;\n1\n\\.\nCOPY drives (id) FROM stdin;\n2\n\\.\n",
			table: Table{Name: "drives", Columns: []string{"id"}},
			want:  []string{"1"},
		},
		"cut short": {
			dump:   "COPY drives (id) FROM stdin;\n1\n2\n",
			table:  Table{Name: "drives", Columns: []string{"id"}},
			reason: "cut short in table drives",
		},
		"cut short elsewhere": {
			dump:   "COPY other (a) FROM stdin;\n1\n",
			table:  Table{Name: "drives", Columns: []string{"id"}},
			reason: "cut short",
		},
		"too few values": {
			dump:   "COPY drives (id, distance) FROM stdin;\n1\t2\n3\n\\.\n",
			table:  Table{Name: "drives", Columns: []string{"id"}},
			reason: "line 3: 1 values in table drives, 2 columns",
		},
		"too many values": {
			dump:   "COPY drives (id) FROM stdin;\n1\t2\n\\.\n",
			table:  Table{Name: "drives", Columns: []string{"id"}},
			reason: "line 2: 2 values in table drives, 1 columns",
		},
		"backslash at the end": {
			dump:   "COPY drives (id) FROM stdin;\nab\\\n\\.\n",
			table:  Table{Name: "drives", Columns: []string{"id"}},
			reason: "line 2: a value ends with a backslash",
		},
		"no column list": {
			dump:   "COPY drives FROM stdin;\n1\n\\.\n",
			table:  Table{Name: "drives", Columns: []string{"id"}},
			reason: "line 1: a COPY of drives not understood",
		},
		"row too long": {
			dump:   "COPY drives (id) FROM stdin;\n" + strings.Repeat("1", MaxRowBytes+1) + "\n\\.\n",
			table:  Table{Name: "drives", Columns: []string{"id"}},
			reason: "a row longer than 1048576 bytes",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var got []string
			err := Read(strings.NewReader(tc.dump), []Table{tc.table}, func(r Row) error {
				v := r.Text(tc.table.Columns[0])
				if v == nil {
					got = append(got, "NULL")
				} else {
					got = append(got, *v)
				}
				return nil
			})
			if tc.reason != "" {
				var fe *FormatError
				if !errors.As(err, &fe) || fe.Reason != tc.reason {
					t.Fatalf("got %v, want %q", err, tc.reason)
				}
				return
			}
			if err != nil || strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestReadReaderFails(t *testing.T) {
	boom := errors.New("boom")
	r := io.MultiReader(strings.NewReader("COPY drives (id) FROM stdin;\n1\n"), iotest.ErrReader(boom))
	err := Read(r, []Table{{Name: "drives", Columns: []string{"id"}}}, func(Row) error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("got %v", err)
	}
	if err := Read(iotest.ErrReader(boom), nil, nil); !errors.Is(err, boom) {
		t.Errorf("at the start: %v", err)
	}
}

func TestUnescape(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:                 "plain",
		`a\tb\nc\rd`:            "a\tb\nc\rd",
		`\b\f\v`:                "\b\f\v",
		`back\\slash`:           `back\slash`,
		`\101\7\0101`:           "A\x07\x081",
		`\x41\x4g\xz`:           "A\x04gxz",
		`\é`:                    "é",
		`\"quoted\"`:            `"quoted"`,
		"\\303\\251t\\303\\251": "été",
	} {
		got, err := unescape([]byte(in))
		if err != nil || got != want {
			t.Errorf("unescape(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestValues(t *testing.T) {
	dump := "COPY t (i, f, b, at) FROM stdin;\n" +
		"12\t1.5\tt\t2024-05-01 07:00:00+02\n" +
		"x\ty\tz\t2024-05-01 07:00:00+05:30\n" +
		"\\N\t\\N\tf\tnot a time\n\\.\n"
	var got []string
	err := Read(strings.NewReader(dump), []Table{{Name: "t", Columns: []string{"i", "f", "b", "at"}}},
		func(r Row) error {
			i, okI := r.Int("i")
			f, okF := r.Float("f")
			b, okB := r.Bool("b")
			at, okT := r.Time("at")
			got = append(got, strings.Join([]string{show(i, okI), show(f, okF), show(b, okB), show(at, okT)}, " "))
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"12 1.5 true 2024-05-01 05:00:00 +0000 UTC",
		"invalid invalid invalid 2024-05-01 01:30:00 +0000 UTC",
		"NULL NULL false invalid",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	defer func() {
		if recover() == nil {
			t.Error("a column not asked for does not panic")
		}
	}()
	_ = Read(strings.NewReader(dump), []Table{{Name: "t", Columns: []string{"i"}}},
		func(r Row) error { r.Text("f"); return nil })
}

func show[T any](v *T, ok bool) string {
	switch {
	case !ok:
		return "invalid"
	case v == nil:
		return "NULL"
	}
	return fmt.Sprint(*v)
}
