// Package archtest checks the import graph of the module against an allowlist, in the
// spirit of the standard library's go/build deps_test.go.
package archtest

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const module = "runsten"

// allowed lists, for each package of the module, the imports it may use: packages of the
// module (written without the module prefix), third-party modules, and the watched
// standard packages. An entry also allows its subpackages. A package missing from the
// list fails the test, so a new package has to declare its dependencies here.
//
// depguard (.golangci.yml) reports the main violations in the editor, with their reason;
// this list is exhaustive. Test files are not checked: depguard covers them where it
// matters.
var allowed = map[string][]string{
	// The binaries wire the adapters together. Only runsten-simulator knows the simulator.
	"cmd/runsten-api": {
		"net/http", "golang.org/x/term", "internal/api", "internal/auth", "internal/catalog", "internal/core", "internal/derive",
		"internal/geocode", "internal/oauth", "internal/platform", "internal/store", "internal/volvo",
	},
	"cmd/runsten-collector": {
		"net/http", "internal/catalog", "internal/collector", "internal/core", "internal/debugui", "internal/derive",
		"internal/oauth", "internal/platform", "internal/publish", "internal/store", "internal/volvo",
	},
	"cmd/runsten-simulator": {"net/http", "internal/platform", "internal/simulator"},
	"cmd/runsten-web":       {"net/http", "internal/platform", "internal/web"},

	// The domain imports nothing: vendor formats are converted by the adapters.
	"internal/core": nil,
	// The variants' data sheet is pure data: its embedded file and the YAML decoder.
	"internal/catalog": {"go.yaml.in/yaml/v3"},

	// Orchestration: each declares the interfaces it needs, the binaries wire them.
	"internal/collector": {"internal/platform/clock", "internal/volvo"},
	"internal/derive":    {"internal/core", "internal/volvo"},
	"internal/oauth":     {"golang.org/x/oauth2", "internal/platform/clock"}, // PKCE helpers only
	// It reads the catalog, pure data like core, to recognize the vehicles' variants.
	"internal/api": {
		"net/http", "encoding/json", "github.com/danielgtaylor/huma/v2", "internal/auth", "internal/catalog", "internal/core",
		"internal/oauth", "internal/platform/clock", "internal/platform/netguard",
	},
	"internal/auth": {"golang.org/x/crypto", "internal/platform/clock"}, // argon2id
	// The addresses' worker declares its Store and its Geocoder.
	"internal/geocode": {"internal/core", "internal/platform/clock"},
	// What is published to an MQTT broker, built from the domain: neither network nor
	// database; Home Assistant's discovery configurations are JSON.
	"internal/publish": {"encoding/json", "internal/core"},

	// Adapters.
	"internal/volvo":             {"net/http", "encoding/json", "golang.org/x/oauth2", "internal/core", "internal/oauth"},
	"internal/geocode/nominatim": {"net/http", "encoding/json", "internal/core"},
	// The only package that knows paho.
	"internal/publish/mqtt": {
		"github.com/eclipse/paho.mqtt.golang", "internal/core", "internal/platform/clock", "internal/platform/netguard",
		"internal/publish",
	},
	"internal/store": {
		"github.com/jackc/pgx/v5", "internal/api", "internal/auth", "internal/collector", "internal/core", "internal/derive",
		"internal/geocode", "internal/oauth", "internal/platform/secretbox", "internal/publish", "internal/volvo",
	},
	"internal/debugui": {"net/http", "encoding/json", "internal/collector", "internal/platform/clock", "internal/volvo"},
	// The front end's server knows runsten-api only by URL: it relays requests.
	"internal/web": {"net/http"},

	// Technical building blocks: no business code.
	"internal/platform/clock":      nil,
	"internal/platform/health":     {"net/http", "encoding/json", "internal/platform/clock"},
	"internal/platform/httpserver": {"net/http"},
	"internal/platform/netguard":   nil,
	"internal/platform/secretbox":  nil,

	// Simulator: the vehicle model is pure, the scenario engine drives it, volvoapi exposes it.
	"internal/simulator/vehicle":  nil,
	"internal/simulator/scenario": {"go.yaml.in/yaml/v3", "internal/simulator/vehicle"},
	"internal/simulator/volvoapi": {
		"net/http", "encoding/json", "golang.org/x/oauth2", "internal/platform/clock", "internal/simulator/vehicle",
	},
	"internal/simulator/control": {"net/http", "encoding/json", "internal/platform/clock", "internal/simulator/scenario"},
	// The simulated Volvo ID's grants, kept across restarts.
	"internal/simulator/pgledger": {"github.com/jackc/pgx/v5", "internal/simulator/volvoapi"},
}

// watchedStd are the standard packages that mark an adapter: transport, wire format,
// storage. The rest of the standard library is free.
var watchedStd = []string{"net/http", "encoding/json", "database/sql"}

type pkg struct {
	ImportPath string
	GoFiles    []string
	Imports    []string
}

func TestDependencies(t *testing.T) {
	allowed := maps.Clone(allowed)
	for name, imports := range premium {
		allowed[name] = append(slices.Clone(allowed[name]), imports...)
	}
	pkgs := listPackages(t)
	seen := map[string]bool{}
	for _, p := range pkgs {
		if len(p.GoFiles) == 0 {
			continue // test-only package, such as this one
		}
		name := strings.TrimPrefix(p.ImportPath, module+"/")
		seen[name] = true
		allow, ok := allowed[name]
		if !ok {
			t.Errorf("%s is not in allowed: declare the imports it may use", name)
			continue
		}
		for _, imp := range p.Imports {
			if watched(imp) && !slices.ContainsFunc(allow, func(a string) bool { return within(imp, qualify(a)) }) {
				t.Errorf("%s imports %s, which its allowlist does not permit", name, imp)
			}
		}
	}
	for name := range allowed {
		if !seen[name] {
			t.Errorf("allowed lists %s, which is not a package of the module", name)
		}
	}
}

// listPackages lists the packages of the module with go list, as deps_test.go does.
func listPackages(t *testing.T) []pkg {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "list", "-json=ImportPath,GoFiles,Imports", module+"/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []pkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p pkg
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		t.Fatal("go list found no package")
	}
	return pkgs
}

// watched reports whether an import is subject to the allowlist: a package of the
// module, a third-party module (a dot in its first element) or a watched standard package.
func watched(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	if first == module || strings.Contains(first, ".") {
		return true
	}
	return slices.ContainsFunc(watchedStd, func(s string) bool { return within(path, s) })
}

func qualify(entry string) string {
	if strings.HasPrefix(entry, "internal/") || strings.HasPrefix(entry, "cmd/") || strings.HasPrefix(entry, "premium/") {
		return module + "/" + entry
	}
	return entry
}

func within(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
