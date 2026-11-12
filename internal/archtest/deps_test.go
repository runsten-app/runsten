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
		"net/http", "golang.org/x/term", "internal/api", "internal/auth", "internal/aximote", "internal/bmw", "internal/catalog", "internal/core",
		"internal/archive", "internal/derive", "internal/exporter", "internal/flags", "internal/geocode", "internal/importer", "internal/oauth", "internal/picture",
		"internal/platform", "internal/pleos", "internal/polestar", "internal/polestarid", "internal/release", "internal/skoda", "internal/store", "internal/tesla",
		"internal/teslamate", "internal/volvo", "internal/volvoportal", "internal/aaoslogging",
	},
	"cmd/runsten-collector": {
		"net/http", "internal/bmw", "internal/catalog", "internal/collector", "internal/core", "internal/debugui", "internal/derive",
		"internal/flags", "internal/oauth", "internal/platform", "internal/pleos", "internal/polestar", "internal/polestarid",
		"internal/publish", "internal/skoda",
		"internal/store", "internal/tesla", "internal/volvo",
	},
	"cmd/runsten-simulator": {"net/http", "internal/platform", "internal/simulator"},
	"cmd/runsten-web":       {"net/http", "internal/platform", "internal/web"},

	// The domain imports nothing: vendor formats are converted by the adapters.
	"internal/core": nil,
	// The variants' data sheet is pure data: its embedded file and the YAML decoder.
	"internal/catalog": {"go.yaml.in/yaml/v3"},
	// The feature flags' declarations are pure data too; eval, which the binaries wire, is
	// the only package that knows OpenFeature.
	"internal/flags":      nil,
	"internal/flags/eval": {"github.com/open-feature/go-sdk", "internal/flags"},

	// Orchestration: each declares the interfaces it needs, the binaries wire them.
	"internal/collector": {"internal/platform/clock", "internal/volvo"},
	// The collector's Source of the Polestar Data Portal: the client, adapted.
	"internal/collector/polestarsource": {"internal/collector", "internal/core", "internal/polestar"},
	// The collector's Source of Tesla's Owner API: the client, adapted.
	"internal/collector/teslasource":      {"internal/collector", "internal/core", "internal/tesla"},
	"internal/collector/polestaridsource": {"internal/collector", "internal/core", "internal/polestarid"},
	// CarData's, which holds each connection's subscription to the stream.
	"internal/collector/bmwsource": {"internal/bmw", "internal/collector", "internal/core", "internal/platform/clock"},
	// The collector's Source of the Pleos Vehicle Data API: the client, adapted, whose
	// tokens it keeps until their end.
	"internal/collector/pleossource": {"internal/collector", "internal/core", "internal/platform/clock", "internal/pleos"},
	// The MyŠkoda Public API's: the client, adapted, which holds a VIN back on its quota.
	"internal/collector/skodasource": {"internal/collector", "internal/core", "internal/platform/clock", "internal/skoda"},
	"internal/derive":                {"internal/core"},
	"internal/oauth":                 {"golang.org/x/oauth2", "internal/platform/clock"}, // PKCE helpers only
	// It reads the catalog, pure data like core, to recognize the vehicles' variants, and
	// the flags' declarations (never flags/eval: depguard).
	"internal/api": {
		"net/http", "encoding/json", "github.com/danielgtaylor/huma/v2", "internal/auth", "internal/catalog", "internal/core",
		"internal/flags", "internal/oauth", "internal/platform/clock", "internal/platform/httpserver",
		"internal/platform/netguard",
	},
	"internal/auth": {"golang.org/x/crypto", "internal/platform/clock"}, // argon2id
	// The addresses' worker declares its Store and its Geocoder.
	"internal/geocode": {"internal/core", "internal/platform/clock"},
	// The import worker declares its Store and its Source, Aximote's client, whose
	// values it converts, as it converts the Volvo Data Portal's files and TeslaMate's
	// backup.
	"internal/importer": {
		"internal/aaoslogging", "internal/aximote", "internal/core", "internal/platform/clock", "internal/teslamate", "internal/volvoportal",
	},
	// The export worker builds an account's archive from the account's export: it
	// declares its Store, which holds the archive, and is given the export; it knows
	// neither the API nor the database.
	"internal/exporter": {"internal/platform/clock"},
	// What is published to an MQTT broker, built from the domain: neither network nor
	// database; Home Assistant's discovery configurations are JSON.
	"internal/publish": {"encoding/json", "internal/core"},

	// Adapters.
	"internal/volvo":      {"net/http", "encoding/json", "golang.org/x/oauth2", "internal/core", "internal/oauth"},
	"internal/polestar":   {"net/http", "encoding/json", "internal/core"},
	"internal/tesla":      {"net/http", "encoding/json", "internal/core"},
	"internal/polestarid": {"net/http", "encoding/json", "google.golang.org/protobuf", "internal/core"},
	// The reader of the Pleos Vehicle Data API of Hyundai, Kia and Genesis.
	"internal/pleos": {"net/http", "encoding/json", "internal/core"},
	// The reader of the MyŠkoda Public API.
	"internal/skoda": {"net/http", "encoding/json", "internal/core"},
	// CarData's REST and its MQTT stream; the YAML decoder for its embedded paints' names.
	"internal/bmw":               {"net/http", "encoding/json", "github.com/eclipse/paho.mqtt.golang", "go.yaml.in/yaml/v3", "internal/core"},
	"internal/geocode/nominatim": {"net/http", "encoding/json", "internal/core"},
	"internal/aximote":           {"net/http", "encoding/json"},
	// The latest release on GitHub, compared with the one running.
	"internal/release": {"net/http", "encoding/json", "internal/platform/clock"},
	// The accounts' export archives, out of the database: a directory, or an S3 bucket.
	"internal/archive": {"net/http", "github.com/minio/minio-go/v7"},
	// The vendors' pictures of the vehicles, relayed to the browser.
	"internal/picture": {"net/http", "internal/platform/clock"},
	// AAOS Logging's export: JSON files in ZIPs.
	"internal/aaoslogging": {"encoding/json"},
	"internal/volvoportal": nil,
	"internal/teslamate":   nil,
	// It knows paho, as bmw does for CarData's stream: no other package.
	"internal/publish/mqtt": {
		"github.com/eclipse/paho.mqtt.golang", "internal/core", "internal/platform/clock", "internal/platform/netguard",
		"internal/publish",
	},
	// The Polestar credential is sealed as one JSON value.
	"internal/store": {
		"encoding/json", "github.com/jackc/pgx/v5", "internal/api", "internal/auth", "internal/collector", "internal/core", "internal/derive",
		"internal/exporter", "internal/geocode", "internal/importer", "internal/oauth", "internal/platform/secretbox", "internal/publish",
		"internal/volvo",
	},
	"internal/debugui": {"net/http", "encoding/json", "internal/collector", "internal/platform/clock", "internal/volvo"},
	// The front end's server knows runsten-api only by URL: it relays requests.
	"internal/web": {"net/http", "internal/platform/httpserver"},

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
	// The fake Polestar Data Portal exposes the same vehicle model; it never imports the client.
	"internal/simulator/polestarapi":   {"net/http", "encoding/json", "internal/platform/clock", "internal/simulator/vehicle"},
	"internal/simulator/polestaridapi": {"net/http", "encoding/json", "google.golang.org/protobuf", "internal/platform/clock", "internal/simulator/vehicle"},
	"internal/simulator/teslaapi":      {"net/http", "encoding/json", "internal/platform/clock", "internal/simulator/vehicle"},
	// The simulated CarData stream is a broker of its own: the embedded one.
	"internal/simulator/bmwapi": {"net/http", "net", "encoding/json", "github.com/mochi-mqtt/server/v2", "internal/platform/clock", "internal/simulator/vehicle"},
	// The fake Pleos Vehicle Data API exposes the same vehicle model; it never imports the client.
	"internal/simulator/pleosapi": {"net/http", "encoding/json", "internal/platform/clock", "internal/simulator/vehicle"},
	// The fake MyŠkoda Public API, the same; it never imports the client.
	"internal/simulator/skodaapi": {"net/http", "encoding/json", "internal/platform/clock", "internal/simulator/vehicle"},
	"internal/simulator/control":  {"net/http", "encoding/json", "internal/platform/clock", "internal/simulator/scenario"},
	// The fake Aximote writes its API's JSON on its own: it never imports the client.
	"internal/simulator/aximoteapi": {"net/http", "encoding/json"},
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
