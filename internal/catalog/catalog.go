// Package catalog is the data sheet of the electric Volvo and Polestar variants: battery
// capacities and charging powers, read from a sourced YAML file embedded in the binary,
// and the recognition of a vehicle's variant from what the vendor API says of it.
//
// It is pure data: it imports no other package of the module.
package catalog

import (
	"bytes"
	_ "embed"
	"fmt"
	"math"
	"slices"

	"go.yaml.in/yaml/v3"
)

//go:embed variants.yaml
var variantsYAML []byte

// Brand is the maker of a variant.
type Brand string

// The brands of the catalog.
const (
	Volvo    Brand = "volvo"
	Polestar Brand = "polestar"
)

// Chemistry is the cell chemistry of a battery; empty when unknown.
type Chemistry string

// The chemistries of the catalog.
const (
	NMC Chemistry = "nmc"
	LFP Chemistry = "lfp"
)

// Level says where a figure comes from.
type Level string

// The levels of a source.
const (
	Manufacturer Level = "manufacturer"
	Secondary    Level = "secondary" // press, Wikipedia, ev-database as a cross-check
)

// Figure names a sourced value of a variant, as the YAML file writes it.
type Figure string

// The figures a source may back.
const (
	FigureGrossKWh   Figure = "gross_kwh"
	FigureNetKWh     Figure = "net_kwh"
	FigureChemistry  Figure = "chemistry"
	FigureACMaxKW    Figure = "ac_max_kw"
	FigureACOptionKW Figure = "ac_option_kw"
	FigureDCMaxKW    Figure = "dc_max_kw"
	FigureMotorCodes Figure = "motor_codes"
)

var allFigures = []Figure{
	FigureGrossKWh, FigureNetKWh, FigureChemistry, FigureACMaxKW, FigureACOptionKW, FigureDCMaxKW, FigureMotorCodes,
}

// Variant is a powertrain and battery of a family, over a range of model years.
type Variant struct {
	// ID is stable: users store it once they choose the variant.
	ID     string `yaml:"id"`
	Brand  Brand  `yaml:"brand"`
	Family string `yaml:"family"` // as the Volvo API returns descriptions.model
	Name   string `yaml:"name"`
	Years  Years  `yaml:"years"`

	GrossKWh   float64   `yaml:"gross_kwh"`
	NetKWh     *float64  `yaml:"net_kwh"` // nil: no source settles it
	Chemistry  Chemistry `yaml:"chemistry"`
	ACMaxKW    float64   `yaml:"ac_max_kw"`    // standard onboard charger
	ACOptionKW *float64  `yaml:"ac_option_kw"` // optional onboard charger, if any
	DCMaxKW    float64   `yaml:"dc_max_kw"`

	// APIKWh are the values of batteryCapacityKWH seen for the variant. They need not be
	// its capacity: the backend has returned a pack label or the net capacity for months.
	APIKWh []APIValue `yaml:"api_kwh"`
	// MotorCodes are VIN positions 4-5, from unofficial and incomplete tables.
	MotorCodes []string `yaml:"motor_codes"`
	// AmbiguousWith names the variants no reading can tell from this one.
	AmbiguousWith []string `yaml:"ambiguous_with"`
	Sources       []Source `yaml:"sources"`
}

// APIValue is a value of batteryCapacityKWH seen for a variant, and where.
type APIValue struct {
	KWh    float64 `yaml:"kwh"`
	Source string  `yaml:"source"`
}

// Source is a public page that backs some figures of a variant.
type Source struct {
	URL   string   `yaml:"url"`
	Level Level    `yaml:"level"`
	For   []Figure `yaml:"for"`
}

// Years are the model years of a variant: From to To included, To zero while on sale.
type Years struct {
	From, To int

	bounds int // how many years the file gave, checked by validation
}

// UnmarshalYAML reads [from, to] or [from]; validation checks the values.
func (y *Years) UnmarshalYAML(node *yaml.Node) error {
	var years []int
	if err := node.Decode(&years); err != nil {
		return fmt.Errorf("years: %w", err)
	}
	*y = Years{bounds: len(years)}
	if len(years) > 0 {
		y.From = years[0]
	}
	if len(years) > 1 {
		y.To = years[1]
	}
	return nil
}

// Contains reports whether the model year is one of the variant's.
func (y Years) Contains(year int) bool {
	return year >= y.From && year <= y.end()
}

func (y Years) overlap(o Years) bool {
	return y.From <= o.end() && o.From <= y.end()
}

func (y Years) end() int {
	if y.To == 0 {
		return math.MaxInt
	}
	return y.To
}

// Level tells where a figure of the variant comes from: Manufacturer when one of the
// sources backing it is the manufacturer, Secondary otherwise, empty when the variant
// does not have the figure.
func (v Variant) Level(f Figure) Level {
	var level Level
	for _, s := range v.Sources {
		if slices.Contains(s.For, f) {
			if s.Level == Manufacturer {
				return Manufacturer
			}
			level = Secondary
		}
	}
	return level
}

// figures lists the figures the variant has, each of which needs a source.
func (v Variant) figures() []Figure {
	fs := []Figure{FigureGrossKWh, FigureACMaxKW, FigureDCMaxKW}
	if v.NetKWh != nil {
		fs = append(fs, FigureNetKWh)
	}
	if v.Chemistry != "" {
		fs = append(fs, FigureChemistry)
	}
	if v.ACOptionKW != nil {
		fs = append(fs, FigureACOptionKW)
	}
	if len(v.MotorCodes) > 0 {
		fs = append(fs, FigureMotorCodes)
	}
	return fs
}

// Catalog is a validated list of variants.
type Catalog struct {
	variants []Variant
}

// Load reads the catalog embedded in the binary. An error is a bug of the file, caught
// by the tests first.
func Load() (*Catalog, error) {
	return Parse(variantsYAML)
}

// Parse reads and validates a catalog. Unknown fields are rejected, so that a misspelt
// name does not leave a figure out in silence.
func Parse(data []byte) (*Catalog, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var variants []Variant
	if err := dec.Decode(&variants); err != nil {
		return nil, fmt.Errorf("reading the catalog: %w", err)
	}
	if err := validate(variants); err != nil {
		return nil, fmt.Errorf("invalid catalog: %w", err)
	}
	return &Catalog{variants: variants}, nil
}

// Variants lists every variant, in the order of the file.
func (c *Catalog) Variants() []Variant {
	return slices.Clone(c.variants)
}

// Variant finds a variant by its ID.
func (c *Catalog) Variant(id string) (Variant, bool) {
	i := slices.IndexFunc(c.variants, func(v Variant) bool { return v.ID == id })
	if i < 0 {
		return Variant{}, false
	}
	return c.variants[i], true
}

// Family lists the variants of a family, every year, in the order of the file.
func (c *Catalog) Family(family string) []Variant {
	var vs []Variant
	for _, v := range c.variants {
		if v.Family == family {
			vs = append(vs, v)
		}
	}
	return vs
}
