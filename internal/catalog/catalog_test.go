package catalog

import (
	"slices"
	"testing"
)

func mustLoad(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ids(vs []Variant) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.ID
	}
	return out
}

func TestEmbeddedCatalog(t *testing.T) {
	c := mustLoad(t)
	vs := c.Variants()
	brands := map[Brand]int{}
	for _, v := range vs {
		brands[v.Brand]++
	}
	if brands[Volvo] == 0 || brands[Polestar] == 0 {
		t.Errorf("brands = %v, want both", brands)
	}

	vs[0].ID = "changed"
	if c.Variants()[0].ID == "changed" {
		t.Error("Variants returns the catalog's own slice")
	}

	if got := ids(c.Family("XC40")); !slices.Equal(got, []string{
		"xc40-twin-2021", "xc40-single-2022", "xc40-single-2024", "xc40-er-2024", "xc40-twin-2024",
	}) {
		t.Errorf("Family(XC40) = %v", got)
	}
	if got := c.Family("Model 3"); len(got) != 0 {
		t.Errorf("Family(Model 3) = %v, want none", ids(got))
	}
}

func TestVariant(t *testing.T) {
	c := mustLoad(t)
	v, ok := c.Variant("ex30-er-2024")
	if !ok {
		t.Fatal("ex30-er-2024 not found")
	}
	if v.Family != "EX30" || v.GrossKWh != 69 || v.NetKWh == nil || *v.NetKWh != 64 || v.Chemistry != NMC ||
		v.ACMaxKW != 11 || v.ACOptionKW == nil || *v.ACOptionKW != 22 || v.DCMaxKW != 153 ||
		v.Years != (Years{From: 2024, To: 2026, bounds: 2}) {
		t.Errorf("ex30-er-2024 = %+v", v)
	}
	if _, ok := c.Variant("ex30-er-2023"); ok {
		t.Error("found an unknown id")
	}
}

func TestLevel(t *testing.T) {
	v, _ := mustLoad(t).Variant("xc40-er-2024")
	tests := []struct {
		figure Figure
		want   Level
	}{
		{FigureGrossKWh, Manufacturer}, // the press release and ev-database: the manufacturer wins
		{FigureDCMaxKW, Manufacturer},
		{FigureNetKWh, Secondary},
		{FigureMotorCodes, Secondary},
		{FigureACOptionKW, ""}, // the variant has no option
	}
	for _, tt := range tests {
		if got := v.Level(tt.figure); got != tt.want {
			t.Errorf("Level(%s) = %q, want %q", tt.figure, got, tt.want)
		}
	}
}

// TestAmbiguousPairs lists the real variants that no reading tells apart, so that a new
// one is a decision in this diff rather than a silent mark in the file.
func TestAmbiguousPairs(t *testing.T) {
	want := [][2]string{
		// No motor code is known for the C40 from 2024; both have the 82 kWh battery.
		{"c40-er-2024", "c40-twin-2024"},
		// No motor code is known for Polestar; the Single and Dual Motor share the battery.
		{"ps2-lr-single-2021", "ps2-lr-dual-2021"},
		{"ps2-lr-single-2024", "ps2-lr-dual-2025"},
	}
	var got [][2]string
	for _, v := range mustLoad(t).Variants() {
		for _, id := range v.AmbiguousWith {
			got = append(got, [2]string{v.ID, id})
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("ambiguous pairs = %v, want %v", got, want)
	}
}
