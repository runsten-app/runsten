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
	if brands[Volvo] == 0 || brands[Polestar] == 0 || brands[Tesla] == 0 || brands[BMW] == 0 || brands[Mini] == 0 {
		t.Errorf("brands = %v, want the five", brands)
	}

	vs[0].ID = "changed"
	if c.Variants()[0].ID == "changed" {
		t.Error("Variants returns the catalog's own slice")
	}

	if got := ids(c.Family("XC40")); !slices.Equal(got, []string{
		"xc40-twin-2021", "xc40-single-2022", "xc40-single-2024", "xc40-er-2024", "xc40-twin-2024", "xc40-phev-2020",
	}) {
		t.Errorf("Family(XC40) = %v", got)
	}
	if got := c.Family("Cybertruck"); len(got) != 0 {
		t.Errorf("Family(Cybertruck) = %v, want none", ids(got))
	}
	if got := c.Brand(Polestar); len(got) != brands[Polestar] || got[0].ID != "ps2-sr-2021" {
		t.Errorf("Brand(polestar) = %v", ids(got))
	}
	if got := c.Brand(Tesla); len(got) != brands[Tesla] || got[0].Family != "Model 3" {
		t.Errorf("Brand(tesla) = %v", ids(got))
	}
	if got := c.Brand("rivian"); len(got) != 0 {
		t.Errorf("Brand(rivian) = %v, want none", ids(got))
	}
}

func TestVariant(t *testing.T) {
	c := mustLoad(t)
	v, ok := c.Variant("ex30-er-2024")
	if !ok {
		t.Fatal("ex30-er-2024 not found")
	}
	if v.Family != "EX30" || v.GrossKWh != 69 || v.NetKWh == nil || *v.NetKWh != 64 || v.Chemistry != NMC ||
		v.ACMaxKW != 11 || v.ACOptionKW == nil || *v.ACOptionKW != 22 || v.DCMaxKW == nil || *v.DCMaxKW != 153 ||
		v.Years != (Years{From: 2024, To: 2026, bounds: 2}) {
		t.Errorf("ex30-er-2024 = %+v", v)
	}
	if v.Powertrain != Electric {
		t.Errorf("ex30-er-2024: powertrain %q, want electric when the file leaves it out", v.Powertrain)
	}
	h, _ := c.Variant("v60-phev-2022")
	if h.Powertrain != PlugInHybrid || h.GrossKWh != 18.8 || h.NetKWh == nil || *h.NetKWh != 14.9 || h.ACMaxKW != 6.4 ||
		h.DCMaxKW != nil || h.Years != (Years{From: 2022, bounds: 1}) {
		t.Errorf("v60-phev-2022 = %+v", h)
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
		// BMW: no motor code is known and no trim is listed (the model's name in basicData is
		// not verified on a car yet), so the variants that share a battery are told apart by
		// the user. A refresh that keeps the battery's gross capacity shares a model year
		// with the model it replaces, as does the 2026 refresh of the iX1 and the Countryman.
		{"i4-edrive35-2023", "i4-edrive35-2024"},
		{"i4-edrive40-2022", "i4-edrive40-2024"},
		{"i4-edrive40-2022", "i4-xdrive40-2024"},
		{"i4-edrive40-2022", "i4-m50-2022"},
		{"i4-edrive40-2022", "i4-m50-2024"},
		{"i4-edrive40-2024", "i4-xdrive40-2024"},
		{"i4-edrive40-2024", "i4-m50-2022"},
		{"i4-edrive40-2024", "i4-m50-2024"},
		{"i4-edrive40-2024", "i4-m60-2025"},
		{"i4-xdrive40-2024", "i4-m50-2022"},
		{"i4-xdrive40-2024", "i4-m50-2024"},
		{"i4-xdrive40-2024", "i4-m60-2025"},
		{"i4-m50-2022", "i4-m50-2024"},
		{"i4-m50-2024", "i4-m60-2025"},
		{"i5-edrive40-2024", "i5-xdrive40-2024"},
		{"i5-edrive40-2024", "i5-m60-2024"},
		{"i5-xdrive40-2024", "i5-m60-2024"},
		{"i7-edrive50-2023", "i7-xdrive60-2023"},
		{"i7-edrive50-2023", "i7-m70-2023"},
		{"i7-xdrive60-2023", "i7-m70-2023"},
		{"i7-50-xdrive-2027", "i7-60-xdrive-2027"},
		{"i7-50-xdrive-2027", "i7-m70-2027"},
		{"i7-60-xdrive-2027", "i7-m70-2027"},
		{"ix-xdrive50-2022", "ix-m60-2022"},
		{"ix-xdrive60-2025", "ix-m70-2025"},
		{"ix1-edrive20-2023", "ix1-xdrive30-2023"},
		{"ix1-edrive20-2023", "ix1-edrive20-2026"},
		{"ix1-xdrive30-2023", "ix1-edrive20-2026"},
		{"ix2-edrive20-2024", "ix2-xdrive30-2024"},
		{"ix3-40-2026", "ix3-40-xdrive-2026"},
		// The Mini Cooper SE and Aceman SE share their battery with their John Cooper Works.
		{"cooper-se-2024", "cooper-jcw-2025"},
		{"aceman-se-2024", "aceman-jcw-2025"},
		{"countryman-e-2024", "countryman-se-all4-2024"},
		{"countryman-e-2024", "countryman-se-all4-2026"},
		{"countryman-se-all4-2024", "countryman-se-all4-2026"},
		{"kona-64-2019", "kona-65-2023"},
		{"kona-65-2023", "kona-64-2019"},
		{"niro-64-2019", "niro-2022"},
		{"niro-2022", "niro-64-2019"},
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
