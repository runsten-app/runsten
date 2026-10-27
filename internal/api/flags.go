package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/flags"
)

// flagsJSON are the flags the front end reads, evaluated for the session's account: the
// browser evaluates nothing, and never talks to a provider.
type flagsJSON map[flags.Name]bool

// Schema describes every flag the front end reads, from their declarations.
func (flagsJSON) Schema(huma.Registry) *huma.Schema {
	s := &huma.Schema{
		Type: huma.TypeObject, Properties: map[string]*huma.Schema{}, AdditionalProperties: false,
		Description: "The feature flags the web interface reads, evaluated for the session's account: a function " +
			"shipped off or on, which the instance, or the account, turns over. Made to go: a flag is removed " +
			"once on everywhere.",
	}
	for _, f := range flags.All() {
		if !f.Web {
			continue
		}
		s.Properties[string(f.Name)] = &huma.Schema{Type: huma.TypeBoolean, Description: f.Purpose}
		s.Required = append(s.Required, string(f.Name))
	}
	return s
}

// enabled is the flag's value for the account; its default without Flags.
func (s *Server) enabled(ctx context.Context, name flags.Name, accountID string) bool {
	if s.Flags == nil {
		f, _ := flags.Of(name)
		return f.Default
	}
	return s.Flags.Enabled(ctx, name, accountID)
}

// flagsOf are the account's flags the front end reads.
func (s *Server) flagsOf(ctx context.Context, accountID string) flagsJSON {
	j := flagsJSON{}
	for _, f := range flags.All() {
		if f.Web {
			j[f.Name] = s.enabled(ctx, f.Name, accountID)
		}
	}
	return j
}
