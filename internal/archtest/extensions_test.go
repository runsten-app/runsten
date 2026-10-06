//go:build !premium

package archtest

// premium adds the premium build's packages to allowed, and the imports its files give
// the packages of the module (cmd/runsten-api's extensions): none here. The premium
// build replaces this file. go list sees its files through GOFLAGS=-tags=premium.
var premium map[string][]string
