package catalog

import (
	"strings"
	"testing"
)

// testCatalog is a valid catalog; each test breaks one rule in it.
const testCatalog = `
- id: aa-one-2024
  brand: volvo
  family: AA
  name: AA One
  years: [2024, 2025]
  gross_kwh: 70
  net_kwh: 66
  chemistry: nmc
  ac_max_kw: 11
  ac_option_kw: 22
  dc_max_kw: 150
  api_kwh:
    - kwh: 69.5
      source: https://example.com/api
  motor_codes: [EA]
  sources:
    - url: https://example.com/one
      level: manufacturer
      for: [gross_kwh, net_kwh, chemistry, ac_max_kw, ac_option_kw, dc_max_kw, motor_codes]
- id: aa-two-2024
  brand: polestar
  family: AA
  name: AA Two
  years: [2024]
  gross_kwh: 90
  ac_max_kw: 7.4
  dc_max_kw: 200
  sources:
    - url: https://example.com/two
      level: secondary
      for: [gross_kwh, ac_max_kw, dc_max_kw]
`

// edited applies replacements to testCatalog, each of which must match.
func edited(t *testing.T, replace ...string) string {
	t.Helper()
	src := testCatalog
	for i := 0; i+1 < len(replace); i += 2 {
		if !strings.Contains(src, replace[i]) {
			t.Fatalf("test catalog has no %q", replace[i])
		}
		src = strings.Replace(src, replace[i], replace[i+1], 1)
	}
	return src
}

const twoAmbiguousWith = "  sources:\n    - url: https://example.com/two"

func TestParseAcceptsTheTestCatalog(t *testing.T) {
	tests := []struct {
		name    string
		replace []string
	}{
		{"as written", nil},
		// Overlapping capacities are allowed once the pair is marked.
		{"marked ambiguity", []string{
			"gross_kwh: 90", "gross_kwh: 72",
			twoAmbiguousWith, "  ambiguous_with: [aa-one-2024]\n" + twoAmbiguousWith,
		}},
		// Disjoint motor codes tell the variants apart, whatever their capacity.
		{"motor codes apart", []string{
			"gross_kwh: 90", "gross_kwh: 70",
			twoAmbiguousWith, "  motor_codes: [EB]\n" + twoAmbiguousWith,
			"for: [gross_kwh, ac_max_kw, dc_max_kw]", "for: [gross_kwh, ac_max_kw, dc_max_kw, motor_codes]",
		}},
		{"years apart", []string{"gross_kwh: 90", "gross_kwh: 70", "years: [2024]", "years: [2026]"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(edited(t, tt.replace...))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParseRejectsInvalidCatalogs(t *testing.T) {
	tests := []struct {
		name    string
		replace []string
		wantErr []string // every part must appear in the error
	}{
		{"duplicate id", []string{"id: aa-two-2024", "id: aa-one-2024"}, []string{"variant aa-one-2024: duplicate id"}},
		{"malformed id", []string{"id: aa-one-2024", "id: AA_one"}, []string{"variant AA_one: id"}},
		{"missing id", []string{"- id: aa-two-2024\n  brand", "- brand"}, []string{"variant #2: id"}},
		{"unknown field", []string{"gross_kwh: 90", "grosskwh: 90"}, []string{"field grosskwh not found"}},
		{"unknown brand", []string{"brand: polestar", "brand: tesla"}, []string{"variant aa-two-2024: brand"}},
		{"no family", []string{"family: AA\n  name: AA Two", "family: ''\n  name: AA Two"}, []string{"aa-two-2024: family"}},
		{"no name", []string{"name: AA Two", "name: ''"}, []string{"aa-two-2024: family and name"}},
		{"no years", []string{"  years: [2024]\n", ""}, []string{"aa-two-2024: years"}},
		{"three years", []string{"years: [2024]", "years: [2024, 2025, 2026]"}, []string{"aa-two-2024: years"}},
		{"reversed years", []string{"years: [2024, 2025]", "years: [2025, 2024]"}, []string{"aa-one-2024: years"}},
		{"zero year", []string{"years: [2024]", "years: [0]"}, []string{"aa-two-2024: years"}},
		{"years not a list", []string{"years: [2024]", "years: 2024"}, []string{"years:"}},
		{"zero gross", []string{"gross_kwh: 90", "gross_kwh: 0"}, []string{"aa-two-2024: gross_kwh"}},
		{"negative ac", []string{"ac_max_kw: 7.4", "ac_max_kw: -1"}, []string{"aa-two-2024: gross_kwh, ac_max_kw"}},
		{"no dc", []string{"  dc_max_kw: 200\n", ""}, []string{"aa-two-2024: gross_kwh, ac_max_kw and dc_max_kw"}},
		{"net above gross", []string{"net_kwh: 66", "net_kwh: 71"}, []string{"aa-one-2024: net_kwh"}},
		{"zero net", []string{"net_kwh: 66", "net_kwh: 0"}, []string{"aa-one-2024: net_kwh"}},
		{"option not above", []string{"ac_option_kw: 22", "ac_option_kw: 11"}, []string{"aa-one-2024: ac_option_kw"}},
		{"unknown chemistry", []string{"chemistry: nmc", "chemistry: nca"}, []string{"aa-one-2024: chemistry"}},
		{"zero api value", []string{"kwh: 69.5", "kwh: 0"}, []string{"aa-one-2024: api_kwh[0]"}},
		{"api source not a url", []string{"source: https://example.com/api", "source: example.com/api"}, []string{"aa-one-2024: api_kwh[0]"}},
		{"lowercase motor code", []string{"motor_codes: [EA]", "motor_codes: [ea]"}, []string{`aa-one-2024: motor code "ea"`}},
		{"motor code with O", []string{"motor_codes: [EA]", "motor_codes: [EO]"}, []string{`motor code "EO"`}},
		{"no source", []string{
			"  sources:\n    - url: https://example.com/two\n      level: secondary\n      for: [gross_kwh, ac_max_kw, dc_max_kw]\n", "",
		}, []string{"aa-two-2024: no source"}},
		{"http source", []string{"https://example.com/two", "http://example.com/two"}, []string{"aa-two-2024: sources[0]: url"}},
		{"unknown level", []string{"level: secondary", "level: official"}, []string{"aa-two-2024: sources[0]: level"}},
		{"empty for", []string{"for: [gross_kwh, ac_max_kw, dc_max_kw]", "for: []"}, []string{
			"aa-two-2024: sources[0]: for lists no figure", "gross_kwh is backed by no source",
		}},
		{"unknown figure", []string{"for: [gross_kwh, ac_max_kw, dc_max_kw]", "for: [gross_kwh, ac_max_kw, dc_max_kw, range_km]"}, []string{
			`aa-two-2024: sources[0]: unknown figure "range_km"`,
		}},
		{"figure the variant lacks", []string{"for: [gross_kwh, ac_max_kw, dc_max_kw]", "for: [gross_kwh, ac_max_kw, dc_max_kw, net_kwh]"}, []string{
			"aa-two-2024: sources[0]: backs net_kwh",
		}},
		{"figure without source", []string{"for: [gross_kwh, ac_max_kw, dc_max_kw]", "for: [gross_kwh, ac_max_kw]"}, []string{
			"aa-two-2024: dc_max_kw is backed by no source",
		}},
		{"indiscernible by gross", []string{"gross_kwh: 90", "gross_kwh: 73"}, []string{
			"variants aa-one-2024 and aa-two-2024 are indiscernible",
		}},
		{"indiscernible by api_kwh", []string{
			"gross_kwh: 90", "gross_kwh: 80",
			twoAmbiguousWith, "  api_kwh:\n    - kwh: 69.5\n      source: https://example.com/api\n" + twoAmbiguousWith,
		}, []string{"variants aa-one-2024 and aa-two-2024 are indiscernible"}},
		{"shared motor code", []string{
			"gross_kwh: 90", "gross_kwh: 70",
			twoAmbiguousWith, "  motor_codes: [EB, EA]\n" + twoAmbiguousWith,
			"for: [gross_kwh, ac_max_kw, dc_max_kw]", "for: [gross_kwh, ac_max_kw, dc_max_kw, motor_codes]",
		}, []string{"are indiscernible"}},
		{"ambiguous with unknown", []string{twoAmbiguousWith, "  ambiguous_with: [aa-three-2024]\n" + twoAmbiguousWith}, []string{
			`variant aa-two-2024: ambiguous_with names unknown variant "aa-three-2024"`,
		}},
		{"ambiguous with itself", []string{twoAmbiguousWith, "  ambiguous_with: [aa-two-2024]\n" + twoAmbiguousWith}, []string{
			"variant aa-two-2024: ambiguous_with names itself",
		}},
		{"stale ambiguity", []string{twoAmbiguousWith, "  ambiguous_with: [aa-one-2024]\n" + twoAmbiguousWith}, []string{
			"variant aa-two-2024: ambiguous_with names aa-one-2024, which Match tells apart",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(edited(t, tt.replace...)))
			if err == nil {
				t.Fatal("Parse accepted an invalid catalog")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q\nwant it to contain %q", err, want)
				}
			}
		})
	}
}

func TestParseRejectsEmptyCatalogs(t *testing.T) {
	for _, src := range []string{"", "[]"} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%q) accepted an empty catalog", src)
		}
	}
}

func TestIndiscernible(t *testing.T) {
	base := Variant{ID: "a", Family: "AA", Years: Years{From: 2024, To: 2025}, GrossKWh: 70}
	tests := []struct {
		name string
		b    Variant
		want bool
	}{
		{"same gross", Variant{ID: "b", Family: "AA", Years: Years{From: 2025}, GrossKWh: 70}, true},
		// Tolerance windows of 1.5 kWh on each side still meet at 73.
		{"windows meet", Variant{ID: "b", Family: "AA", Years: Years{From: 2025}, GrossKWh: 73}, true},
		{"windows apart", Variant{ID: "b", Family: "AA", Years: Years{From: 2025}, GrossKWh: 73.1}, false},
		{"other family", Variant{ID: "b", Family: "BB", Years: Years{From: 2025}, GrossKWh: 70}, false},
		{"years apart", Variant{ID: "b", Family: "AA", Years: Years{From: 2026}, GrossKWh: 70}, false},
		{"same variant", Variant{ID: "a", Family: "AA", Years: Years{From: 2025}, GrossKWh: 70}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := indiscernible(base, tt.b); got != tt.want {
				t.Errorf("indiscernible = %v, want %v", got, tt.want)
			}
			if got := indiscernible(tt.b, base); got != tt.want {
				t.Errorf("indiscernible, reversed = %v, want %v", got, tt.want)
			}
		})
	}
}
