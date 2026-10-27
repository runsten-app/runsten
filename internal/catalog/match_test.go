package catalog

import (
	"math"
	"slices"
	"testing"
)

// vin builds a 17-character VIN with the given motor code at positions 6-7, after an
// EX30's model code (positions 4-5), as the Volvo API returns them.
func vin(code string) string { return "YV12Z" + code + "9XRS000001" }

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
		{"lowercase VIN", "EX30", 2024, 69.0, "  " + "yv12zel9xrs000001" + " ", []string{"ex30-er-2024"}, true},
		{"VIN too short", "EX30", 2024, 69.0, "YV1EL", []string{"ex30-er-2024", "ex30-twin-2024"}, false},
		{"VIN with an unknown code", "EX30", 2024, 69.0, vin("ZZ"), []string{"ex30-er-2024", "ex30-twin-2024"}, false},
		{"XC40 2024, its model code before the motor's", "XC40", 2024, 81.608, "YV1XZEHR0R2000001", []string{"xc40-er-2024"}, true},
		{"a code at positions 4-5 is not the motor's", "EX30", 2024, 69.0, "YV1EL3AV0R2000001", []string{"ex30-er-2024", "ex30-twin-2024"}, false},
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
		{"no family, its brand", Reading{Brand: Polestar, ModelYear: 2026, CapacityKWh: 92, BatteryElectric: true}, "ps3-rear-2026"},
		{"no family, its brand: several fit", Reading{Brand: Polestar, ModelYear: 2024, BatteryElectric: true}, ""},
		{"no family, its brand, a hybrid", Reading{Brand: Polestar, ModelYear: 2026, CapacityKWh: 92}, ""},
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
	polestar := Reading{Brand: Polestar, ModelYear: 2024, BatteryElectric: true}
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
		// The Polestar Data Portal tells the brand alone: the choice sets the family.
		{"no family, a variant of its brand", "ps2-lr-single-2024", polestar, "ps2-lr-single-2024"},
		{"no family, a variant of another brand", "ex30-er-2024", polestar, ""},
		{"a family read outranks the brand", "ps3-2024", with(polestar, func(r *Reading) { r.Family = "Polestar 2" }), ""},
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

func TestMatchBrand(t *testing.T) {
	c := mustLoad(t)
	var every []string
	for _, v := range c.Variants() {
		if v.Brand == Polestar {
			every = append(every, v.ID)
		}
	}
	tests := []struct {
		name       string
		year       int
		kwh        float64
		want       []string
		recognized bool
	}{
		{"every family of the year", 2026, 0, []string{
			"ps2-sr-2025", "ps2-lr-single-2024", "ps2-lr-dual-2025", "ps3-rear-2026", "ps3-dual-2026", "ps4-2024", "ps5-2025",
		}, false},
		{"the capacity narrows them", 2026, 92, []string{"ps3-rear-2026"}, true},
		{"none fits: the whole brand", 2019, 0, every, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.MatchBrand(Polestar, tt.year, tt.kwh, "")
			if !slices.Equal(ids(got.Candidates), tt.want) || got.Recognized != tt.recognized {
				t.Errorf("MatchBrand = %v, recognized %v; want %v, %v", ids(got.Candidates), got.Recognized, tt.want, tt.recognized)
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

// TestRecognizeTesla: a Tesla tells no capacity; its trim rules out the variants that
// list another, and recognizes a pack alone in its family, years and trim.
func TestRecognizeTesla(t *testing.T) {
	c := mustLoad(t)
	tests := []struct {
		name string
		r    Reading
		want []string
	}{
		{"Model Y 2021 74d: the Shanghai pack alone", Reading{Family: "Model Y", ModelYear: 2021, Trim: "74d"}, []string{"my-lr-awd-2021"}},
		{"Model Y 2023 74d: the Berlin pack alone", Reading{Family: "Model Y", ModelYear: 2023, Trim: "74d"}, []string{"my-lr-awd-2022"}},
		{"Model Y 2022 74d: both packs", Reading{Family: "Model Y", ModelYear: 2022, Trim: "74d"}, []string{"my-lr-awd-2021", "my-lr-awd-2022"}},
		{"Model Y 2023 p74d", Reading{Family: "Model Y", ModelYear: 2023, Trim: "P74D"}, []string{"my-perf-2022"}},
		{"Model Y 2023 50: CATL or BYD", Reading{Family: "Model Y", ModelYear: 2023, Trim: "50"}, []string{"my-rwd-2022", "my-rwd-byd-2022"}},
		{"Model 3 2019 74", Reading{Family: "Model 3", ModelYear: 2019, Trim: "74"}, []string{"m3-lr-rwd-2019"}},
		// A trim no variant lists rules nothing out.
		{"Model 3 2019, unknown trim", Reading{Family: "Model 3", ModelYear: 2019, Trim: "62"}, []string{"m3-sr-plus-2019", "m3-lr-rwd-2019"}},
		// The 2025 refresh lists no trim: kept beside the pack its trim names.
		{"Model Y 2025 74d", Reading{Family: "Model Y", ModelYear: 2025, Trim: "74d"}, []string{
			"my-lr-awd-2022", "my-rwd-2025", "my-lr-awd-2025", "my-lr-rwd-2025", "my-premium-rwd-2025",
			"my-premium-rwd-5m-2025", "my-premium-rwd-4680-2025", "my-premium-awd-2025",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.BatteryElectric = true
			m := c.MatchReading(tt.r)
			if got := ids(m.Candidates); !slices.Equal(got, tt.want) {
				t.Errorf("candidates %v, want %v", got, tt.want)
			}
			v, ok := c.Recognize(tt.r)
			if ok != (len(tt.want) == 1) || (ok && v.ID != tt.want[0]) {
				t.Errorf("recognized %s, %v", v.ID, ok)
			}
		})
	}
}
