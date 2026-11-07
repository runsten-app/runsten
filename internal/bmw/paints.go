package bmw

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// CarData tells a car's paint by BMW's option code (C4P), never by its name, and BMW
// publishes no table of them: paints.yaml names the paints of the electric models in the
// catalog, each from its sources, as the catalog's figures are. A code it does not know (a
// special paint, BMW Individual, an older model's) is shown as it is, so that nothing is
// lost. The names are BMW's own, in English: proper names, never translated.

//go:embed paints.yaml
var paintsYAML []byte

// paint is an entry of paints.yaml.
type paint struct {
	Brand   string        `yaml:"brand"`
	Code    string        `yaml:"code"`
	Name    string        `yaml:"name"`
	Sources []paintSource `yaml:"sources"`
}

type paintSource struct {
	URL   string `yaml:"url"`
	Level string `yaml:"level"`
}

// paintKey is a paint's brand, the catalog's (bmw, mini), and its code, upper-cased: the
// same code may name two paints at BMW and at MINI.
type paintKey struct{ brand, code string }

// codePattern is BMW's paint code: three letters or digits.
var codePattern = regexp.MustCompile(`^[A-Z0-9]{3}$`)

// paints are the names of paints.yaml by brand and code. The file is embedded and its
// tests check it: a failure here is a build's, never a car's.
var paints = func() map[paintKey]string {
	p, err := parsePaints(paintsYAML)
	if err != nil {
		panic(err)
	}
	return p
}()

// parsePaints decodes and checks the table: every entry a brand, a code, a name and a
// source over https, its level manufacturer or secondary; no code twice for a brand.
func parsePaints(raw []byte) (map[paintKey]string, error) {
	var entries []paint
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("paints: %w", err)
	}
	names := make(map[paintKey]string, len(entries))
	var errs []error
	for i, e := range entries {
		k := paintKey{e.Brand, e.Code}
		switch {
		case e.Brand != "bmw" && e.Brand != "mini":
			errs = append(errs, fmt.Errorf("paint #%d: brand %q", i+1, e.Brand))
		case !codePattern.MatchString(e.Code):
			errs = append(errs, fmt.Errorf("paint #%d: code %q", i+1, e.Code))
		case strings.TrimSpace(e.Name) != e.Name || e.Name == "":
			errs = append(errs, fmt.Errorf("paint %s %s: name %q", e.Brand, e.Code, e.Name))
		case names[k] != "":
			errs = append(errs, fmt.Errorf("paint %s %s: twice", e.Brand, e.Code))
		case len(e.Sources) == 0:
			errs = append(errs, fmt.Errorf("paint %s %s: no source", e.Brand, e.Code))
		}
		for _, s := range e.Sources {
			if u, err := url.Parse(s.URL); err != nil || u.Scheme != "https" || u.Host == "" {
				errs = append(errs, fmt.Errorf("paint %s %s: source %q", e.Brand, e.Code, s.URL))
			}
			if s.Level != "manufacturer" && s.Level != "secondary" {
				errs = append(errs, fmt.Errorf("paint %s %s: level %q", e.Brand, e.Code, s.Level))
			}
		}
		names[k] = e.Name
	}
	return names, errors.Join(errs...)
}

// paintName is the name of the first of codes the table knows for brand, the catalog's;
// else the first code given, as it is. Empty when none is.
func paintName(brand string, codes ...string) string {
	first := ""
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if name, ok := paints[paintKey{brand, strings.ToUpper(c)}]; ok {
			return name
		}
		if first == "" {
			first = c
		}
	}
	return first
}
