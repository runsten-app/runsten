// Package flags declares the core's feature flags: functions shipped off or on, which an
// instance, or an account, turns over without a new image. Each is declared once here,
// with its default and when to remove it: a flag is made to go. It knows no provider:
// flags/eval evaluates them through OpenFeature.
package flags

import (
	"fmt"
	"slices"
	"strings"
)

// Name names a flag: dot-separated kebab-case words, the area first (source.tesla).
type Name string

// The flags. Add one to All with its default.
const (
	// SourceTesla: a Tesla is read through the Owner API, with its owner's tokens.
	SourceTesla Name = "source.tesla"
)

// Flag is a flag's declaration.
type Flag struct {
	Name Name
	// Default is the value without a provider, or when it does not know the flag, fails,
	// or is too slow: what the release ships.
	Default bool
	// Web: the front end reads it, from the session.
	Web bool
	// Purpose says what it turns over, Remove when it goes.
	Purpose, Remove string
}

// All are the core's flags.
func All() []Flag {
	return []Flag{
		{
			Name: SourceTesla, Default: true, Web: true,
			Purpose: "Tesla as a source: its connection, its page and its reads. On by default; an instance may " +
				"turn it off, Tesla's Owner API being unofficial.",
			Remove: "when every instance offers Tesla, or reads it through the Fleet API instead",
		},
	}
}

// Of is the declaration of name; false if none.
func Of(name Name) (Flag, bool) {
	i := slices.IndexFunc(All(), func(f Flag) bool { return f.Name == name })
	if i < 0 {
		return Flag{}, false
	}
	return All()[i], true
}

// Parse reads a list of name=true|false, separated by commas (RUNSTEN_FLAGS). A name
// that is not declared is returned apart, not an error: a flag removed by a release may
// stay in a configuration.
func Parse(s string) (values map[Name]bool, unknown []string, err error) {
	values = map[Name]bool{}
	for item := range strings.SplitSeq(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			return nil, nil, fmt.Errorf("%q: expected name=true or name=false", item)
		}
		name = strings.TrimSpace(name)
		var on bool
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true":
			on = true
		case "false":
		default:
			return nil, nil, fmt.Errorf("%q: expected name=true or name=false", item)
		}
		if _, ok := Of(Name(name)); !ok {
			unknown = append(unknown, name)
			continue
		}
		if _, twice := values[Name(name)]; twice {
			return nil, nil, fmt.Errorf("%s: given twice", name)
		}
		values[Name(name)] = on
	}
	return values, unknown, nil
}
