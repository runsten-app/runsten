package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/config"
	liberrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/router"
	"github.com/pb33f/libopenapi-validator/schema_validation"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"go.yaml.in/yaml/v3"
)

// specFile is the OpenAPI description of /api/v1. It is part of the contract, with the
// golden files of testdata/: every API response of this package's tests is validated
// against it (checkedAPI), and so are the golden files.
const specFile = "../../api/openapi.yaml"

type contract struct {
	model     *v3.Document
	validator validator.Validator
	router    router.Router
	errorBody *base.Schema
	raw       map[string]any

	mu   sync.Mutex // the validator is not documented as safe for concurrent use
	seen map[string]bool
}

var loadContract = sync.OnceValues(func() (*contract, error) {
	raw, err := os.ReadFile(specFile)
	if err != nil {
		return nil, err
	}
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		return nil, err
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, err
	}
	c := &contract{
		model: &model.Model,
		// Formats are assertions: date-time and uuid are checked, not only annotations.
		validator: validator.NewValidatorFromV3Model(&model.Model, config.WithFormatAssertions()),
		router:    router.NewRouter(&model.Model),
		seen:      map[string]bool{},
	}
	c.validator.SetDocument(doc)
	c.errorBody = model.Model.Components.Schemas.GetOrZero("Error").Schema()
	if err := yaml.Unmarshal(raw, &c.raw); err != nil {
		return nil, err
	}
	return c, nil
})

func specContract(t *testing.T) *contract {
	t.Helper()
	c, err := loadContract()
	if err != nil {
		t.Fatalf("%s: %v", specFile, err)
	}
	return c
}

// response is the key of a response of the spec: "GET /vehicles/{vehicle} 404".
func response(method, path string, status int) string {
	return method + " " + path + " " + strconv.Itoa(status)
}

// check validates a response of runsten-api against the spec. A request to a route the
// spec does not describe must be answered 404 or 405 (403 for a cross-origin request
// that changes state) with an error body.
func (c *contract) check(r *http.Request, status int, header http.Header, body []byte) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	rt, err := c.router.FindRoute(r)
	if err != nil {
		want := http.StatusNotFound
		if errors.Is(err, router.ErrMethodNotAllowed) {
			want = http.StatusMethodNotAllowed
		}
		if status != want && status != http.StatusForbidden {
			return []string{fmt.Sprintf("%s, answered %d, want %d", err, status, want)}
		}
		if ok, errs := schema_validation.NewSchemaValidator().ValidateSchemaBytes(c.errorBody, body); !ok {
			return describe(errs)
		}
		return nil
	}
	c.seen[response(r.Method, rt.Path, status)] = true
	resp := &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)), Request: r}
	if ok, errs := c.validator.ValidateHttpResponse(r, resp); !ok {
		return describe(errs)
	}
	return nil
}

func describe(errs []*liberrors.ValidationError) []string {
	var out []string
	for _, e := range errs {
		out = append(out, e.Message)
		for _, f := range e.SchemaValidationErrors {
			out = append(out, "  "+f.FieldPath+": "+f.Reason)
		}
	}
	return out
}

// checkedAPI validates every response under /api/ against the spec.
func checkedAPI(t *testing.T, h http.Handler) http.Handler {
	t.Helper()
	c := specContract(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAPI(r) {
			h.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if errs := c.check(r, rec.Code, rec.Header(), rec.Body.Bytes()); errs != nil {
			t.Errorf("%s %s: the %d response does not match %s:\n%s\n%s",
				r.Method, r.URL, rec.Code, specFile, strings.Join(errs, "\n"), rec.Body)
		}
		maps.Copy(w.Header(), rec.Header())
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	})
}

// TestMain checks, after a full run, that the tests produced every response the spec
// declares: the spec describes no response the API never gives.
func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	if code != 0 || flag.Lookup("test.run").Value.String() != "" || flag.Lookup("test.skip").Value.String() != "" {
		os.Exit(code)
	}
	c, err := loadContract()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if missing := c.unseen(); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: %s declares responses no test produced:\n  %s\n", specFile, strings.Join(missing, "\n  "))
		os.Exit(1)
	}
	os.Exit(code)
}

func (c *contract) unseen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var missing []string
	for path, item := range c.model.Paths.PathItems.FromOldest() {
		for method, op := range item.GetOperations().FromOldest() {
			for code := range op.Responses.Codes.KeysFromOldest() {
				status, _ := strconv.Atoi(code)
				if key := response(strings.ToUpper(method), path, status); !c.seen[key] {
					missing = append(missing, key)
				}
			}
		}
	}
	sort.Strings(missing)
	return missing
}

func TestSpecIsValidOpenAPI(t *testing.T) {
	c := specContract(t)
	if v := c.model.Version; !strings.HasPrefix(v, "3.1.") {
		t.Errorf("OpenAPI %s, want 3.1", v)
	}
	if ok, errs := c.validator.ValidateDocument(); !ok {
		t.Errorf("%s is not a valid OpenAPI document:\n%s", specFile, strings.Join(describe(errs), "\n"))
	}
}

// specOperations lists the operations of the spec: "GET /vehicles/{vehicle}".
func specOperations(t *testing.T) []string {
	t.Helper()
	var ops []string
	for path, item := range specContract(t).model.Paths.PathItems.FromOldest() {
		for method := range item.GetOperations().KeysFromOldest() {
			ops = append(ops, strings.ToUpper(method)+" "+path)
		}
	}
	slices.Sort(ops)
	return ops
}

// TestOpenAPIFile checks that api/openapi.yaml is the document of the API: it is
// generated from the operations, and go test ./internal/api -update rewrites it.
func TestOpenAPIFile(t *testing.T) {
	doc, err := New(Config{Log: quiet}).api.OpenAPI().YAML()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(specFile, doc, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(doc, want) {
		t.Errorf("%s is not the document of the API: run go test ./internal/api -update, and review the diff as an API change", specFile)
	}
}

// goldenRequests maps each golden file to the request whose 200 response it is.
var goldenRequests = map[string]string{
	"session.json":                 "POST /api/v1/session",
	"connection.json":              "PUT /api/v1/connection/api-key",
	"vehicles.json":                "GET /api/v1/vehicles",
	"vehicle-reauth-required.json": "GET /api/v1/vehicles/" + parked,
	"state.json":                   "GET /api/v1/vehicles/" + car + "/state",
	"state-unknown.json":           "GET /api/v1/vehicles/" + parked + "/state",
	"trips-page-1.json":            "GET /api/v1/vehicles/" + car + "/trips",
	"trips-page-2.json":            "GET /api/v1/vehicles/" + car + "/trips",
	"trip.json":                    "GET /api/v1/vehicles/" + car + "/trips/2026-09-28T07:01:00.000000Z",
	"charges.json":                 "GET /api/v1/vehicles/" + car + "/charges",
	"charge.json":                  "GET /api/v1/vehicles/" + car + "/charges/2026-09-28T18:30:00.000000Z",
	"charge-onboard-charger.json":  "GET /api/v1/vehicles/" + car + "/charges/2026-09-28T18:30:00.000000Z",
	"stats.json":                   "GET /api/v1/vehicles/" + car + "/stats",
	"stats-totals.json":            "GET /api/v1/vehicles/" + car + "/stats",
	"stats-empty.json":             "GET /api/v1/vehicles/" + parked + "/stats",
	"battery.json":                 "GET /api/v1/vehicles/" + car + "/battery",
	"battery-empty.json":           "GET /api/v1/vehicles/" + car + "/battery",
	"battery-api-capacity.json":    "GET /api/v1/vehicles/" + car + "/battery",
	"series.json":                  "GET /api/v1/vehicles/" + car + "/series",
	"settings.json":                "GET /api/v1/settings",
	"settings-unset.json":          "GET /api/v1/settings",
	"places.json":                  "GET /api/v1/places",
	"place.json":                   "GET /api/v1/places/" + placeID(1),
	"place-unpriced.json":          "GET /api/v1/places/" + placeID(1) + "/unpriced",
	"charge-cost.json":             "PUT /api/v1/vehicles/" + car + "/charges/2026-09-28T04:00:00.000000Z/cost",
	"orphans.json":                 "GET /api/v1/charge-costs/orphans",
	"variants.json":                "GET /api/v1/vehicles/" + chosen + "/variants",
	"vehicle-model.json":           "PUT /api/v1/vehicles/" + chosen + "/model",
	"mqtt-none.json":               "GET /api/v1/mqtt",
	"mqtt.json":                    "GET /api/v1/mqtt",
}

func goldenFiles(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // test file
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Base(p)] = b
	}
	if len(files) != len(goldenRequests) {
		t.Errorf("testdata holds %d golden files, goldenRequests maps %d: map each to its request", len(files), len(goldenRequests))
	}
	return files
}

// validateGolden validates a 200 response body of the request of a golden file.
func validateGolden(t *testing.T, c *contract, name string, body []byte) []string {
	t.Helper()
	req, ok := goldenRequests[name]
	if !ok {
		t.Fatalf("%s: no request in goldenRequests", name)
	}
	method, target, _ := strings.Cut(req, " ")
	r := httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
	resp := &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Request: r,
		Header: http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"runsten_session=token"}},
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ok, errs := c.validator.ValidateHttpResponse(r, resp); !ok {
		return describe(errs)
	}
	return nil
}

func TestGoldenFilesMatchTheSpec(t *testing.T) {
	c := specContract(t)
	for name, body := range goldenFiles(t) {
		if errs := validateGolden(t, c, name, body); errs != nil {
			t.Errorf("%s does not match %s:\n%s", name, specFile, strings.Join(errs, "\n"))
		}
	}
}

// TestSpecRejectsMutations checks that the spec is strict enough to notice a change of
// the golden files: every field removed or renamed, every unknown field, every value
// of another type must fail the validation.
func TestSpecRejectsMutations(t *testing.T) {
	c := specContract(t)
	for name, body := range goldenFiles(t) {
		var doc any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		var ms []mutation
		mutate(doc, func(v any) any { return v }, "$", &ms)
		for _, m := range ms {
			b, err := json.Marshal(m.doc)
			if err != nil {
				t.Fatal(err)
			}
			if validateGolden(t, c, name, b) == nil {
				t.Errorf("%s: %s still matches the spec", name, m.what)
			}
		}
	}
}

type mutation struct {
	what string
	doc  any
}

// mutate lists the mutations of v, a part of a JSON document; wrap puts a replacement
// of v back into a copy of the whole document.
func mutate(v any, wrap func(any) any, at string, out *[]mutation) {
	switch x := v.(type) {
	case map[string]any:
		*out = append(*out, mutation{at + " with an unknown field", wrap(with(x, "unexpected", "x"))})
		for k, child := range x {
			rest := with(x, k, nil)
			delete(rest, k)
			*out = append(*out,
				mutation{at + "." + k + " removed", wrap(rest)},
				mutation{at + "." + k + " renamed", wrap(with(rest, k+"_renamed", child))},
			)
			mutate(child, func(c any) any { return wrap(with(x, k, c)) }, at+"."+k, out)
		}
	case []any:
		for i, child := range x {
			mutate(child, func(c any) any {
				cp := slices.Clone(x)
				cp[i] = c
				return wrap(cp)
			}, fmt.Sprintf("%s[%d]", at, i), out)
		}
	}
	*out = append(*out, mutation{at + " of another type", wrap(otherType(v))})
}

func with(m map[string]any, k string, v any) map[string]any {
	cp := maps.Clone(m)
	cp[k] = v
	return cp
}

// otherType is a value of a type no field accepts in place of v: no field is both a
// number and a string, nor boolean or null.
func otherType(v any) any {
	switch v.(type) {
	case nil:
		return true
	case string:
		return 42
	default:
		return "x"
	}
}

// TestSpecIsStrict checks the conventions every object of the spec follows: no field
// other than those described (additionalProperties: false), and every key always
// present (required lists every property), null standing for an unknown value.
func TestSpecIsStrict(t *testing.T) {
	c := specContract(t)
	objects := 0
	var walk func(v any, at string)
	walk = func(v any, at string) {
		switch x := v.(type) {
		case map[string]any:
			props, isObject := x["properties"].(map[string]any)
			if typ, _ := x["type"].(string); typ == "object" && !isObject {
				t.Errorf("%s: an object without properties", at)
			}
			if isObject {
				objects++
				if x["additionalProperties"] != false {
					t.Errorf("%s: additionalProperties must be false", at)
				}
				var required []string
				for _, r := range x["required"].([]any) {
					required = append(required, r.(string))
				}
				slices.Sort(required)
				if keys := slices.Sorted(maps.Keys(props)); !slices.Equal(required, keys) {
					t.Errorf("%s: required %v, want every property %v", at, required, keys)
				}
			}
			for k, child := range x {
				walk(child, at+"."+k)
			}
		case []any:
			for i, child := range x {
				walk(child, fmt.Sprintf("%s[%d]", at, i))
			}
		}
	}
	walk(c.raw, "$")
	if objects < 20 {
		t.Errorf("only %d objects found in the spec", objects)
	}
}
