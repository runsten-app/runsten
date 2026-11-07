package bmw

import (
	"strings"
	"testing"
)

func TestPaintsTable(t *testing.T) {
	p, err := parsePaints(paintsYAML)
	if err != nil {
		t.Fatal(err)
	}
	byBrand := map[string]int{}
	for k := range p {
		byBrand[k.brand]++
	}
	if byBrand["bmw"] == 0 || byBrand["mini"] == 0 {
		t.Errorf("paints by brand: %v", byBrand)
	}
}

func TestParsePaintsRefuses(t *testing.T) {
	const ok = `- brand: bmw
  code: C4P
  name: Brooklyn Grey metallic
  sources:
    - url: https://example.com/a
      level: manufacturer
`
	if _, err := parsePaints([]byte(ok)); err != nil {
		t.Fatalf("valid entry: %v", err)
	}
	for name, mutate := range map[string]func(string) string{
		"brand":         func(s string) string { return strings.Replace(s, "brand: bmw", "brand: audi", 1) },
		"code":          func(s string) string { return strings.Replace(s, "C4P", "c4p", 1) },
		"long code":     func(s string) string { return strings.Replace(s, "C4P", "C4P1", 1) },
		"name":          func(s string) string { return strings.Replace(s, "Brooklyn Grey metallic", `" Grey"`, 1) },
		"no name":       func(s string) string { return strings.Replace(s, "Brooklyn Grey metallic", `""`, 1) },
		"http source":   func(s string) string { return strings.Replace(s, "https://", "http://", 1) },
		"level":         func(s string) string { return strings.Replace(s, "manufacturer", "forum", 1) },
		"no source":     func(s string) string { head, _, _ := strings.Cut(s, "  sources:"); return head },
		"unknown field": func(s string) string { return s + "  note: x\n" },
		"twice":         func(s string) string { return s + s },
	} {
		if _, err := parsePaints([]byte(mutate(ok))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The same code at MINI is another paint.
	if _, err := parsePaints([]byte(ok + strings.Replace(ok, "brand: bmw", "brand: mini", 1))); err != nil {
		t.Errorf("same code, two brands: %v", err)
	}
}

func TestPaintName(t *testing.T) {
	known := func(brand, code string) string {
		t.Helper()
		name, ok := paints[paintKey{brand, code}]
		if !ok {
			t.Fatalf("%s %s not in paints.yaml", brand, code)
		}
		return name
	}
	for _, c := range []struct {
		brand string
		codes []string
		want  string
	}{
		{"bmw", []string{"C4P", "C4P"}, known("bmw", "C4P")},
		{"bmw", []string{" c4p "}, known("bmw", "C4P")},
		// The raw code when the code is not given; an unknown code does not hide a known one.
		{"bmw", []string{"", "C4P"}, known("bmw", "C4P")},
		{"bmw", []string{"ZZ9", "C4P"}, known("bmw", "C4P")},
		// Unknown: the first code as BMW gives it, nothing lost.
		{"bmw", []string{" ZZ9 ", "ZZ91"}, "ZZ9"},
		{"bmw", []string{"", " "}, ""},
		// A MINI's code is not a BMW's.
		{"mini", []string{"C6A"}, known("mini", "C6A")},
		{"bmw", []string{"C6A"}, "C6A"},
		// A brand the normalizer did not recognize names nothing.
		{"", []string{"C4P"}, "C4P"},
	} {
		if got := paintName(c.brand, c.codes...); got != c.want {
			t.Errorf("paintName(%q, %q) = %q, want %q", c.brand, c.codes, got, c.want)
		}
	}
}
