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
	// SourcePolestarID: a Polestar is read through the Polestar app's API, with its
	// owner's Polestar ID (ADR 0042).
	SourcePolestarID Name = "source.polestar-id"
	// SourceBMW: a BMW or a Mini is read through BMW CarData, with its owner's client ID
	// (ADR 0045).
	SourceBMW Name = "source.bmw"
	// PictureTesla: a Tesla's picture, as Tesla's configurator draws it, relayed.
	PictureTesla Name = "picture.tesla"
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
		{
			Name: SourcePolestarID, Default: false, Web: true,
			Purpose: "The Polestar ID as a source, for the Polestars the Data Portal does not read (outside the EU/EEA): " +
				"its connection and its reads. Off by default: the Polestar app's API is unofficial, and many accounts " +
				"read from one instance's addresses could have them blocked; a self-hosted instance turns it on.",
			Remove: "when Polestar opens an official API to these cars, or the source is offered everywhere",
		},
		{
			Name: SourceBMW, Default: true, Web: true,
			Purpose: "BMW CarData as a source, for the BMWs and the Minis: its connection and its reads. On by default, " +
				"the API being BMW's own; an instance may turn it off while the source is in beta, not seen yet on a " +
				"real car.",
			Remove: "once the source has been checked on real cars and is out of beta",
		},
		{
			Name: PictureTesla, Default: true,
			Purpose: "A Tesla's picture, drawn by Tesla's configurator in its colour and on its rims, and relayed. On by " +
				"default; an instance may turn it off, the configurator's endpoint being undocumented: Tesla may " +
				"change it, or object to its use.",
			Remove: "when Tesla's API gives a picture of the car, or an owner's own picture replaces it",
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
