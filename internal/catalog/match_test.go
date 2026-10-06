package catalog

import (
	"math"
	"slices"
	"testing"
)

// vin builds a 17-character VIN with the given motor code at positions 4-5.
func vin(code string) string { return "YV1" + code + "3AV0R2000001" }

func TestMatch(t *testing.T) {
	c := mustLoad(t)
	xc40 := ids(c.Family("XC40"))
	ex90 := ids(c.Family("EX90"))
	tests := []struct {
		name       string
		family     string
		year       int
		kwh        float64
		vin        string
		want       []string
		recognized bool
	}{
		{"XC40 2021 at its gross capacity", "XC40", 2021, 78.012, "", []string{"xc40-twin-2021"}, true},
		{"XC40 2024 at 81.608: two motors share it", "XC40", 2024, 81.608, "", []string{"xc40-er-2024", "xc40-twin-2024"}, false},
		{"XC40 2024, VIN of the Single Motor", "XC40", 2024, 81.608, vin("EH"), []string{"xc40-er-2024"}, true},
		{"XC40 2024, VIN of the Twin Motor", "XC40", 2024, 81.608, vin("ER"), []string{"xc40-twin-2024"}, true},
		{"XC40 2024 at the net 79 recognizes nothing", "XC40", 2024, 79, "", xc40, false},
		{"C40 2023", "C40", 2023, 69.452, "", []string{"c40-single-2022"}, true},
		{"EX30 2024 at 69.0", "EX30", 2024, 69.0, "", []string{"ex30-er-2024", "ex30-twin-2024"}, false},
		{"EX30 2024 at 69.0, VIN EL", "EX30", 2024, 69.0, vin("EL"), []string{"ex30-er-2024"}, true},
		// 3 kWh from the gross capacity: only api_kwh recognizes the backend's episode.
		{"EX30 2024 at 66.0, VIN EK", "EX30", 2024, 66.0, vin("EK"), []string{"ex30-twin-2024"}, true},
		{"EX30 LFP at 51.0", "EX30", 2025, 51.0, "", []string{"ex30-lfp-2024"}, true},
		{"EX30 LFP at the net 49.0", "EX30", 2025, 49.0, "", []string{"ex30-lfp-2024"}, true},
		{"EX30 2027 LFP", "EX30", 2027, 51.0, "", []string{"ex30-p5-2027"}, true},
		{"EX90 2026 at 106 (800 V)", "EX90", 2026, 106, "", []string{"ex90-twin-2026"}, true},
		{"EX90 2025 at 111 (400 V)", "EX90", 2025, 111, "", []string{"ex90-twin-2024"}, true},
		{"EX90 2026 at the 400 V capacity", "EX90", 2026, 111, "", ex90, false},
		{"within the gross tolerance", "EX90", 2025, 105.5, "", []string{"ex90-single-2024"}, true},
		{"beyond the gross tolerance", "EX90", 2025, 105.6, "", ex90, false},
		{"unknown family", "XC90", 2024, 18.819, "", nil, false},
		{"model year out of the table", "XC40", 2019, 78, "", xc40, false},
		{"unknown model year", "EX90", 0, 106, "", []string{"ex90-twin-2026"}, true},
		{"capacity absent", "EX30", 2024, 0, "", []string{"ex30-lfp-2024", "ex30-er-2024", "ex30-twin-2024"}, false},
		{"capacity absent, VIN EK", "EX30", 2024, 0, vin("EK"), []string{"ex30-twin-2024"}, true},
		{"capacity absent, VIN EL", "EX30", 2024, 0, vin("EL"), []string{"ex30-lfp-2024", "ex30-er-2024"}, false},
		{"capacity NaN", "EX30", 2024, math.NaN(), vin("EK"), []string{"ex30-twin-2024"}, true},
		{"negative capacity", "EX30", 2024, -1, vin("EK"), []string{"ex30-twin-2024"}, true},
		{"lowercase VIN", "EX30", 2024, 69.0, "  " + "yv1el3av0r2000001" + " ", []string{"ex30-er-2024"}, true},
		{"VIN too short", "EX30", 2024, 69.0, "YV1EL", []string{"ex30-er-2024", "ex30-twin-2024"}, false},
		{"VIN with an unknown code", "EX30", 2024, 69.0, vin("ZZ"), []string{"ex30-er-2024", "ex30-twin-2024"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Match(tt.family, tt.year, tt.kwh, tt.vin)
			if !slices.Equal(ids(got.Candidates), tt.want) || got.Recognized != tt.recognized {
				t.Errorf("Match = %v, recognized %v; want %v, %v", ids(got.Candidates), got.Recognized, tt.want, tt.recognized)
			}
		})
	}
}

func TestRecognize(t *testing.T) {
	c := mustLoad(t)
	ex30 := Reading{Family: "EX30", ModelYear: 2024, CapacityKWh: 69.0, VIN: vin("EL"), BatteryElectric: true}
	tests := []struct {
		name string
		r    Reading
		want string // "" when nothing is recognized
	}{
		{"one variant fits", ex30, "ex30-er-2024"},
		{"several fit", with(ex30, func(r *Reading) { r.VIN = "" }), ""},
		{"none fits", with(ex30, func(r *Reading) { r.CapacityKWh = 90 }), ""},
		// It would be recognized, were it known to be battery electric.
		{"a hybrid, or an unknown fuel type", with(ex30, func(r *Reading) { r.BatteryElectric = false }), ""},
		{"unknown family", with(ex30, func(r *Reading) { r.Family = "" }), ""},
		{"year and capacity unknown", Reading{Family: "EX90", BatteryElectric: true}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := c.Recognize(tt.r)
			if v.ID != tt.want || ok != (tt.want != "") {
				t.Errorf("Recognize = %q, %v; want %q", v.ID, ok, tt.want)
			}
		})
	}
}

func TestEffective(t *testing.T) {
	c := mustLoad(t)
	// Several EX30 fit without the VIN: nothing is recognized.
	ex30 := Reading{Family: "EX30", ModelYear: 2024, CapacityKWh: 69.0, BatteryElectric: true}
	recognized := with(ex30, func(r *Reading) { r.VIN = vin("EL") })
	tests := []struct {
		name   string
		chosen string
		r      Reading
		want   string // "" when none
	}{
		{"a choice outranks the recognition", "ex30-lfp-2024", recognized, "ex30-lfp-2024"},
		{"a choice where nothing is recognized", "ex30-er-2024", ex30, "ex30-er-2024"},
		{"no choice: the recognition", "", recognized, "ex30-er-2024"},
		{"a variant the catalog no longer has", "ex30-gone", recognized, "ex30-er-2024"},
		{"a variant of another family", "xc40-twin-2021", recognized, "ex30-er-2024"},
		{"a hybrid, even chosen", "ex30-er-2024", with(ex30, func(r *Reading) { r.BatteryElectric = false }), ""},
		{"no family read", "ex30-er-2024", Reading{BatteryElectric: true}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := c.Effective(tt.chosen, tt.r)
			if v.ID != tt.want || ok != (tt.want != "") {
				t.Errorf("Effective = %q, %v; want %q", v.ID, ok, tt.want)
			}
		})
	}
}

func with[T any](v T, edit func(*T)) T {
	edit(&v)
	return v
}

// TestMatchRules checks the rules the real catalog does not exercise, on the test catalog
// with its two variants brought close.
func TestMatchRules(t *testing.T) {
	src := edited(t,
		"gross_kwh: 90", "gross_kwh: 70.8",
		twoAmbiguousWith, "  ambiguous_with: [aa-one-2024]\n"+twoAmbiguousWith)
	withCode := edited(t,
		"gross_kwh: 90", "gross_kwh: 70",
		twoAmbiguousWith, "  motor_codes: [EB]\n"+twoAmbiguousWith,
		"for: [gross_kwh, ac_max_kw, dc_max_kw]", "for: [gross_kwh, ac_max_kw, dc_max_kw, motor_codes]")
	tests := []struct {
		name    string
		catalog string
		kwh     float64
		vin     string
		want    []string
	}{
		// 69.5 is within the tolerance of aa-two's gross capacity, but it has been seen on aa-one.
		{"a value seen outranks a gross capacity nearby", src, 69.5, "", []string{"aa-one-2024"}},
		{"a value seen by neither", src, 70.3, "", []string{"aa-one-2024", "aa-two-2024"}},
		// aa-two lists no motor code: the VIN cannot rule it out.
		{"a variant without codes is kept", src, 70.3, vin("EA"), []string{"aa-one-2024", "aa-two-2024"}},
		{"a variant listing another code is not", withCode, 0, vin("EA"), []string{"aa-one-2024"}},
		{"a code nobody lists rules nothing out", withCode, 0, vin("EC"), []string{"aa-one-2024", "aa-two-2024"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Parse([]byte(tt.catalog))
			if err != nil {
				t.Fatal(err)
			}
			if got := c.Match("AA", 2024, tt.kwh, tt.vin); !slices.Equal(ids(got.Candidates), tt.want) {
				t.Errorf("Match = %v, want %v", ids(got.Candidates), tt.want)
			}
		})
	}
}
