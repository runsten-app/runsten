package api

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"runsten/internal/auth"
)

// The JSON API is built with huma: its operations are typed functions, and huma reads
// and validates their inputs, writes their outputs, and describes them in an OpenAPI
// 3.1 document. That document is the contract, with the golden files of testdata/;
// api/openapi.yaml is its generated copy.

// apiPrefix is where the JSON API is mounted, and the server of its OpenAPI document.
const apiPrefix = "/api/v1"

const apiDescription = `The session, the account's vehicles, their current state (Verdandi), their
trips and charges (Urd) with the cost of each charge, their statistics, the account's
settings (its currency, and its places with their tariffs), and the costs entered for
charges.

Conventions:

- Units are in the field names: ` + "`_km`, `_m` (metres), `_kwh`, `_pct` (state of charge, in %), `_w`, `_kw`, `_kwh_per_100km`, `_s` (seconds), `_minor` (minor units of the account's currency, as cents: an integer), `price_per_kwh` (major units of the account's currency per kWh)" + `.
  Positions are WGS 84, in degrees.
- Times are RFC 3339, in UTC.
- An unknown value is ` + "`null`" + `, never zero. Every key is always present.
- Lists are ` + "`{\"items\": [...]}`" + `, never ` + "`null`" + `. Trips and charges are paged with an
  opaque cursor, newest first.
- Errors are ` + "`{\"error\": {\"code\", \"message\"}}`" + `: the code is stable, the message is
  for humans and may change. A path under ` + "`/api/`" + ` that serves no route answers 404
  ` + "`not_found`" + `, and a method that a path does not accept answers 405
  ` + "`method_not_allowed`" + ` with an ` + "`Allow`" + ` header.
- Every response carries ` + "`Cache-Control: no-store`, `X-Content-Type-Options: nosniff`" + `
  and ` + "`Referrer-Policy: no-referrer`" + `.

Every route but ` + "`POST /session`" + ` requires the session cookie it sets, or, to read, a
personal access token (` + "`Authorization: Bearer rst_…`" + `, issued by ` + "`POST /tokens`" + `). A token only
reads: it serves the ` + "`GET`" + ` routes but those of the session and of the tokens (403
` + "`insufficient_scope`" + ` otherwise), at most 120 requests at once then 2 per second (429
` + "`rate_limited`" + `, with ` + "`Retry-After`" + `). Requests that
change state (` + "`POST`, `PUT`, `DELETE`" + `) from another origin are refused (403
` + "`cross_origin`" + `), after the ` + "`Sec-Fetch-Site`" + ` or ` + "`Origin`" + ` header. A request body must be
` + "`application/json`" + ` (415 ` + "`unsupported_media_type`" + ` otherwise).`

// sessionSecurity is the requirement of the routes that need a session: either cookie.
// readSecurity is that of the reads, which an access token may make too.
var (
	sessionSecurity = []map[string][]string{{"session": {}}, {"sessionSecure": {}}}
	readSecurity    = append(slices.Clone(sessionSecurity), map[string][]string{"accessToken": {}})
)

// humaConfig is the configuration of the API: no route besides the operations (the
// document is not served), no $schema field in the bodies, and the error format of
// the contract.
func humaConfig() huma.Config {
	cfg := huma.DefaultConfig("Runsten API", "1.0.0")
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	cfg.CreateHooks = nil // the link transformer: $schema fields and Link headers
	cfg.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", schemaName)
	cfg.Info.Description = apiDescription
	cfg.Info.License = &huma.License{Name: "AGPL-3.0-or-later", Identifier: "AGPL-3.0-or-later"}
	cfg.Info.Contact = &huma.Contact{Name: "Runsten", URL: "https://github.com/runsten-app/runsten"}
	cfg.Tags = []*huma.Tag{
		{Name: "session", Description: "Sign in and out."},
		{Name: "vehicles", Description: "The account's vehicles, their model and their provider connection."},
		{Name: "events", Description: "Trips and charges, derived from the recorded readings."},
		{Name: "stats", Description: "Totals of the trips and charges of a period, by day, week or month."},
		{Name: "battery", Description: "The estimated battery capacity of a vehicle, and how it changed."},
		{Name: "settings", Description: "The account's currency, and its places with their dated tariffs."},
		{Name: "costs", Description: "The costs entered for charges, and those no charge takes."},
		{Name: "connection", Description: "The account's connection to Volvo: its Volvo ID, and the application key its vehicles are read with."},
		{Name: "account", Description: "The account itself: the export of all its data."},
		{Name: "tokens", Description: "The user's personal access tokens, with which a program reads the API without a session."},
		{Name: "mqtt", Description: "The account's MQTT broker, which the collector publishes the vehicles' state to."},
	}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"session": {
			Type: "apiKey", In: "cookie", Name: "runsten_session",
			Description: "The session cookie set by `POST /session`, when the instance is served over plain http.",
		},
		"sessionSecure": {
			Type: "apiKey", In: "cookie", Name: "__Host-runsten_session",
			Description: "The same cookie when the instance is served over https (its redirect URI is https): " +
				"`Secure`, and the `__Host-` prefix binds it to the host.",
		},
		"accessToken": {
			Type: "http", Scheme: "bearer",
			Description: "A personal access token, `rst_` then 43 characters, issued by `POST /tokens`: it reads only.",
		},
	}
	return cfg
}

// schemaName names the schemas after their Go types, without the JSON suffix of the
// wire types: vehicleJSON is Vehicle, reading[float64] ReadingFloat64.
func schemaName(t reflect.Type, hint string) string {
	return strings.ReplaceAll(huma.DefaultSchemaNamer(t, hint), "JSON", "")
}

// huma's error constructor is a package variable: it is set once, to the same value.
var setErrorFormat = sync.OnceFunc(func() { huma.NewError = newError })

// errorCode is the stable, machine-readable code of an error.
type errorCode string

// Error codes.
const (
	codeInvalidParameter     errorCode = "invalid_parameter"
	codeInvalidBody          errorCode = "invalid_body"
	codeUnauthorized         errorCode = "unauthorized"
	codeInvalidCredentials   errorCode = "invalid_credentials" //nolint:gosec // an error code, not a credential
	codeCrossOrigin          errorCode = "cross_origin"
	codeNotFound             errorCode = "not_found"
	codeMethodNotAllowed     errorCode = "method_not_allowed"
	codeUnsupportedMediaType errorCode = "unsupported_media_type"
	codeTooManyAttempts      errorCode = "too_many_attempts"
	codeCurrencyInUse        errorCode = "currency_in_use"
	codeWithoutPositionTaken errorCode = "without_position_taken"
	codeTooManyPlaces        errorCode = "too_many_places"
	codeChargeHasCost        errorCode = "charge_has_cost"
	codeAPIKeyRefused        errorCode = "api_key_refused"
	codeInstanceKey          errorCode = "instance_key"
	codeTooManyVehicles      errorCode = "too_many_vehicles"
	codeFeatureUnavailable   errorCode = "feature_unavailable"
	codeBrokerRefused        errorCode = "broker_refused"
	codeInsufficientScope    errorCode = "insufficient_scope"
	codeRateLimited          errorCode = "rate_limited"
	codeInternal             errorCode = "internal"
)

// Schema lists the codes.
func (errorCode) Schema(huma.Registry) *huma.Schema {
	return enumSchema(codeInvalidParameter, codeInvalidBody, codeUnauthorized, codeInvalidCredentials,
		codeCrossOrigin, codeNotFound, codeMethodNotAllowed, codeUnsupportedMediaType, codeTooManyAttempts,
		codeCurrencyInUse, codeWithoutPositionTaken, codeTooManyPlaces, codeChargeHasCost, codeAPIKeyRefused, codeInstanceKey, codeTooManyVehicles, codeFeatureUnavailable,
		codeBrokerRefused, codeInsufficientScope, codeRateLimited, codeInternal)
}

// errorJSON is the body of every JSON error, and the error the operations return.
type errorJSON struct {
	status int
	Detail errorDetailJSON `json:"error"`
}

type errorDetailJSON struct {
	Code    errorCode `json:"code" doc:"Stable and machine-readable."`
	Message string    `json:"message" minLength:"1" doc:"For humans; it may change."`
}

func (e *errorJSON) Error() string  { return e.Detail.Message }
func (e *errorJSON) GetStatus() int { return e.status }

func apiError(status int, code errorCode, message string) *errorJSON {
	return &errorJSON{status: status, Detail: errorDetailJSON{Code: code, Message: message}}
}

// newError turns the errors huma raises itself (invalid parameters or body, unknown
// content type) into those of the contract: huma answers 422 to an invalid input and
// 413 to a body too large, the contract 400.
func newError(status int, msg string, errs ...error) huma.StatusError {
	var details []string
	inBody := false
	for _, err := range errs {
		if err == nil {
			continue
		}
		var d *huma.ErrorDetail
		if errors.As(err, &d) {
			inBody = inBody || d.Location == "body" || strings.HasPrefix(d.Location, "body.")
			details = append(details, d.Location+": "+d.Message)
			continue
		}
		details = append(details, err.Error())
	}
	if len(details) > 0 {
		msg += ": " + strings.Join(details, "; ")
	}
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		if inBody {
			return apiError(http.StatusBadRequest, codeInvalidBody, msg)
		}
		return apiError(http.StatusBadRequest, codeInvalidParameter, msg)
	case http.StatusRequestEntityTooLarge:
		return apiError(http.StatusBadRequest, codeInvalidBody, msg)
	case http.StatusUnsupportedMediaType:
		return apiError(status, codeUnsupportedMediaType, "application/json expected")
	case http.StatusUnauthorized:
		return apiError(status, codeUnauthorized, msg)
	case http.StatusNotFound:
		return apiError(status, codeNotFound, msg)
	default:
		// Never a detail of an internal failure.
		return apiError(http.StatusInternalServerError, codeInternal, "internal error, see the logs")
	}
}

// null is a value that may be unknown: JSON null, and oneOf [T, null] in the document.
// huma describes nullable scalars (*float64), not nullable objects.
type null[T any] struct {
	v  T
	ok bool
}

func known[T any](v T) null[T] { return null[T]{v: v, ok: true} }

// MarshalJSON writes the value, or null.
func (n null[T]) MarshalJSON() ([]byte, error) {
	if !n.ok {
		return []byte("null"), nil
	}
	return json.Marshal(n.v) //nolint:wrapcheck // marshaler
}

// Schema is oneOf [T, null].
func (null[T]) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{OneOf: []*huma.Schema{r.Schema(reflect.TypeFor[T](), true, ""), {Type: "null"}}}
}

func enumSchema[T ~string](values ...T) *huma.Schema {
	s := &huma.Schema{Type: huma.TypeString}
	for _, v := range values {
		s.Enum = append(s.Enum, string(v))
	}
	return s
}

// operation completes an operation that requires a session, or an access token if it
// reads.
func (s *Server) operation(op huma.Operation, success string, errs map[int]string) huma.Operation {
	op = s.publicOperation(op, success, errs)
	op.Security = sessionSecurity
	op.Middlewares = append(huma.Middlewares{s.requireSession}, op.Middlewares...)
	if op.Method != http.MethodGet {
		forbidden := strings.TrimSpace(descriptionOf(op, http.StatusForbidden) + " " + errReadOnly)
		op.Responses[strconv.Itoa(http.StatusForbidden)] = s.errorResponse(forbidden)
		return op
	}
	op.Security = readSecurity
	limited := s.errorResponse(errRateLimited)
	one := 1.0
	limited.Headers = map[string]*huma.Param{
		"Retry-After": {
			Description: "Seconds to wait before the next request.", Required: true,
			Schema: &huma.Schema{Type: huma.TypeInteger, Minimum: &one},
		},
	}
	op.Responses[strconv.Itoa(http.StatusTooManyRequests)] = limited
	return op
}

// sessionOperation completes an operation that requires a session, never a token.
func (s *Server) sessionOperation(op huma.Operation, success string, errs map[int]string) huma.Operation {
	op = s.publicOperation(op, success, errs)
	op.Security = sessionSecurity
	op.Middlewares = append(huma.Middlewares{s.requireSessionOnly}, op.Middlewares...)
	return op
}

func descriptionOf(op huma.Operation, status int) string {
	if r, ok := op.Responses[strconv.Itoa(status)]; ok {
		return r.Description
	}
	return ""
}

func (s *Server) errorResponse(description string) *huma.Response {
	return &huma.Response{
		Description: description,
		Content:     map[string]*huma.MediaType{"application/json": {Schema: s.errorSchema}},
	}
}

// publicOperation documents the responses of an operation, by status: huma would
// describe the errors by status text only, and add a 422 the contract does not have.
func (s *Server) publicOperation(op huma.Operation, success string, errs map[int]string) huma.Operation {
	status := op.DefaultStatus
	if status == 0 {
		status = http.StatusOK
	}
	op.Responses = map[string]*huma.Response{strconv.Itoa(status): {Description: success}}
	for code, description := range errs {
		op.Responses[strconv.Itoa(code)] = s.errorResponse(description)
	}
	return op
}

// Errors common to the operations.
const (
	errUnauthorized = "`unauthorized`: no valid session, or an invalid or expired access token (with a `WWW-Authenticate` header)."
	errReadOnly     = "`insufficient_scope`: an access token only reads; this operation requires a session."
	errSessionOnly  = "`insufficient_scope`: this operation requires a session, never an access token."
	errRateLimited  = "`rate_limited`: too many requests with this access token."
	errCrossOrigin  = "`cross_origin`: a request that changes state, from another origin."
	errInternal     = "`internal`: an internal failure, without detail; the cause is logged."
	errVehicle      = "`not_found`: no such vehicle in the account. Another account's vehicle is not found either."
	errEvent        = "`not_found`: no such vehicle in the account, or no such event for it."
	errParameter    = "`invalid_parameter`: `from`, `to`, `limit` or `cursor`; the message names the parameter."
	errBody         = "`invalid_body`: not a single JSON object of the schema, a value out of its bounds, an unknown " +
		"field, or more than 1 MiB; the message names the field, never its value."
	errMediaType = "`unsupported_media_type`: the body is not `application/json`."
)

// writeErrors are the errors of an operation that reads a JSON body, and more.
func writeErrors(more map[int]string) map[int]string {
	errs := map[int]string{
		http.StatusBadRequest: errBody, http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errCrossOrigin,
		http.StatusUnsupportedMediaType: errMediaType, http.StatusInternalServerError: errInternal,
	}
	for code, d := range more {
		errs[code] = d
	}
	return errs
}

// internal logs err and returns the 500 of the contract, without its details.
func (s *Server) internal(msg string, err error) error {
	s.Log.Error(msg, "err", err)
	return apiError(http.StatusInternalServerError, codeInternal, "internal error, see the logs")
}

type sessionKey struct{}

// requireSession is the middleware of the operations that need a session, or an access
// token if they read: it answers 401 without either, 403 to a token that would change
// state, and passes the session on in the context.
func (s *Server) requireSession(ctx huma.Context, next func(huma.Context)) {
	s.require(ctx, next, true)
}

// requireSessionOnly is that of the operations that need a session, never a token.
func (s *Server) requireSessionOnly(ctx huma.Context, next func(huma.Context)) {
	s.require(ctx, next, false)
}

func (s *Server) require(ctx huma.Context, next func(huma.Context), tokens bool) {
	r, w := unwrap(ctx)
	sess, fail, err := s.authenticate(w, r, tokens)
	switch {
	case err != nil:
		s.Log.Error("session check failed", "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error, see the logs")
	case fail.status != 0:
		fail.write(w)
	default:
		next(huma.WithValue(ctx, sessionKey{}, sess))
	}
}

// requireJSON refuses a body that is not JSON, before huma reads it: huma would take a
// body without Content-Type for JSON, and a form posted cross-site is refused anyway.
func (s *Server) requireJSON(ctx huma.Context, next func(huma.Context)) {
	if mt, _, err := mime.ParseMediaType(ctx.Header("Content-Type")); err != nil || mt != "application/json" {
		_, w := unwrap(ctx)
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType, "application/json expected")
		return
	}
	next(ctx)
}

// unwrap returns the request and the response writer of an operation.
func unwrap(ctx huma.Context) (*http.Request, http.ResponseWriter) { return humago.Unwrap(ctx) }

func sessionFrom(ctx interface{ Value(any) any }) auth.Session {
	sess, _ := ctx.Value(sessionKey{}).(auth.Session)
	return sess
}
