package catalog

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
)

var (
	idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// VIN characters: no I, O or Q.
	motorCodePattern = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{2}$`)
)

// validate checks every variant, then that each pair can be told apart.
func validate(variants []Variant) error {
	if len(variants) == 0 {
		return errors.New("no variant")
	}
	var errs []error
	byID := map[string]Variant{}
	for i, v := range variants {
		name := v.ID
		if name == "" {
			name = fmt.Sprintf("#%d", i+1)
		}
		if _, dup := byID[v.ID]; dup && v.ID != "" {
			errs = append(errs, fmt.Errorf("variant %s: duplicate id", name))
		}
		byID[v.ID] = v
		if err := v.validate(); err != nil {
			errs = append(errs, fmt.Errorf("variant %s: %w", name, err))
		}
	}
	errs = append(errs, validateAmbiguity(variants, byID))
	return errors.Join(errs...)
}

func (v Variant) validate() error {
	var errs []error
	if !idPattern.MatchString(v.ID) {
		errs = append(errs, fmt.Errorf("id %q: lowercase words joined by hyphens expected", v.ID))
	}
	if v.Brand != Volvo && v.Brand != Polestar {
		errs = append(errs, fmt.Errorf("brand %q: volvo or polestar expected", v.Brand))
	}
	if v.Family == "" || v.Name == "" {
		errs = append(errs, errors.New("family and name are required"))
	}
	if y := v.Years; y.bounds < 1 || y.bounds > 2 || y.From <= 0 || (y.bounds == 2 && y.To < y.From) {
		errs = append(errs, errors.New("years: [from, to] with 0 < from ≤ to, or [from] while on sale, expected"))
	}
	if v.GrossKWh <= 0 || v.ACMaxKW <= 0 || v.DCMaxKW <= 0 {
		errs = append(errs, errors.New("gross_kwh, ac_max_kw and dc_max_kw must be > 0"))
	}
	if n := v.NetKWh; n != nil && (*n <= 0 || *n > v.GrossKWh) {
		errs = append(errs, errors.New("net_kwh must be in ]0, gross_kwh]"))
	}
	if o := v.ACOptionKW; o != nil && *o <= v.ACMaxKW {
		errs = append(errs, errors.New("ac_option_kw must be > ac_max_kw"))
	}
	if v.Chemistry != "" && v.Chemistry != NMC && v.Chemistry != LFP {
		errs = append(errs, fmt.Errorf("chemistry %q: nmc or lfp expected, or left out", v.Chemistry))
	}
	for i, a := range v.APIKWh {
		if a.KWh <= 0 || !isHTTPS(a.Source) {
			errs = append(errs, fmt.Errorf("api_kwh[%d]: kwh > 0 and an https source expected", i))
		}
	}
	for _, c := range v.MotorCodes {
		if !motorCodePattern.MatchString(c) {
			errs = append(errs, fmt.Errorf("motor code %q: two uppercase VIN characters expected", c))
		}
	}
	errs = append(errs, v.validateSources())
	return errors.Join(errs...)
}

// validateSources checks that every figure is backed, and that a source backs only
// figures the variant has.
func (v Variant) validateSources() error {
	if len(v.Sources) == 0 {
		return errors.New("no source")
	}
	var errs []error
	has := v.figures()
	for i, s := range v.Sources {
		if !isHTTPS(s.URL) {
			errs = append(errs, fmt.Errorf("sources[%d]: url %q is not https", i, s.URL))
		}
		if s.Level != Manufacturer && s.Level != Secondary {
			errs = append(errs, fmt.Errorf("sources[%d]: level %q: manufacturer or secondary expected", i, s.Level))
		}
		if len(s.For) == 0 {
			errs = append(errs, fmt.Errorf("sources[%d]: for lists no figure", i))
		}
		for _, f := range s.For {
			switch {
			case !slices.Contains(allFigures, f):
				errs = append(errs, fmt.Errorf("sources[%d]: unknown figure %q", i, f))
			case !slices.Contains(has, f):
				errs = append(errs, fmt.Errorf("sources[%d]: backs %s, which the variant does not have", i, f))
			}
		}
	}
	for _, f := range has {
		if v.Level(f) == "" {
			errs = append(errs, fmt.Errorf("%s is backed by no source", f))
		}
	}
	return errors.Join(errs...)
}

// validateAmbiguity requires every pair of indiscernible variants to name each other in
// ambiguous_with, and every name there to be such a pair: a mark does not outlive the
// ambiguity it excused.
func validateAmbiguity(variants []Variant, byID map[string]Variant) error {
	var errs []error
	for i, a := range variants {
		for _, b := range variants[i+1:] {
			if indiscernible(a, b) && !slices.Contains(a.AmbiguousWith, b.ID) && !slices.Contains(b.AmbiguousWith, a.ID) {
				errs = append(errs, fmt.Errorf(
					"variants %s and %s are indiscernible: separate them (motor codes, api_kwh) or name each other in ambiguous_with",
					a.ID, b.ID))
			}
		}
		for _, id := range a.AmbiguousWith {
			b, ok := byID[id]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("variant %s: ambiguous_with names unknown variant %q", a.ID, id))
			case id == a.ID:
				errs = append(errs, fmt.Errorf("variant %s: ambiguous_with names itself", a.ID))
			case !indiscernible(a, b):
				errs = append(errs, fmt.Errorf("variant %s: ambiguous_with names %s, which Match tells apart", a.ID, id))
			}
		}
	}
	return errors.Join(errs...)
}

// indiscernible reports whether some reading would leave both variants candidates of
// Match: same family, overlapping years, no motor codes apart, and a capacity both
// accept (a shared api_kwh value, or gross capacities whose tolerance windows meet).
func indiscernible(a, b Variant) bool {
	if a.ID == b.ID || a.Family != b.Family || !a.Years.overlap(b.Years) {
		return false
	}
	if len(a.MotorCodes) > 0 && len(b.MotorCodes) > 0 &&
		!slices.ContainsFunc(a.MotorCodes, func(c string) bool { return slices.Contains(b.MotorCodes, c) }) {
		return false
	}
	if math.Abs(a.GrossKWh-b.GrossKWh) <= 2*grossTolerance {
		return true
	}
	return slices.ContainsFunc(a.APIKWh, func(x APIValue) bool { return b.seen(x.KWh) })
}

func isHTTPS(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != ""
}
