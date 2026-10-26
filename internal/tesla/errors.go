package tesla

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Kind classifies an API error by the reaction expected from the collector.
type Kind int

// Error kinds. A 5xx, a 404 and anything else are KindOther: tried again at the next
// pass.
const (
	KindOther Kind = iota
	// KindTokenRefused: the refresh token no longer refreshes (invalid_grant,
	// login_required). Only a new one from the owner reads the account again.
	KindTokenRefused
	// KindUnauthorized: a 401 on /api/1, the access token expired or revoked; it is
	// refreshed, once.
	KindUnauthorized
	// KindOwnerAPIClosed: a 403 that sends to the Fleet API. The Owner API is closed to
	// the account, or the token is the Fleet API's (refreshed below TLS 1.3).
	KindOwnerAPIClosed
	// KindUnavailable: a 408, the car asleep or out of reach. Not a failure: the car is
	// not woken.
	KindUnavailable
	// KindRateLimited: 429, wait RetryIn.
	KindRateLimited
)

// OAuth errors of the token endpoint that mean the refresh token is lost.
var refusedGrants = []string{"invalid_grant", "login_required"}

// fleetAPIHint is in the message of the 403 of an account the Owner API is closed to:
// "forbidden, see https://developer.tesla.com/docs/fleet-api".
const fleetAPIHint = "fleet-api"

// defaultRateRetry is the delay after a 429 without Retry-After: the Owner API publishes
// no limit, and none of its responses has been seen with a rate limit header.
const defaultRateRetry = 5 * time.Minute

// call is what a request asks for: the same status means another thing on each.
type call int

const (
	tokenCall call = iota
	listCall
	vehicleCall
)

// APIError is a non-200 response from the API or its authentication. It holds neither a
// token nor a VIN: the logs carry it.
type APIError struct {
	Endpoint string // token, products, vehicle or vehicle_data
	Status   int
	Code     string // the OAuth error of the token endpoint; empty elsewhere
	Message  string
	Kind     Kind
	RetryIn  time.Duration // for KindRateLimited
	// Headers are the response's headers that tell about a limit (see toldHeaders), as
	// "Name=value" sorted by name: what a 429 of the real API comes with is not known yet.
	Headers []string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s: HTTP %d: %s", e.Endpoint, e.Status, e.Message)
	if e.Code != "" {
		msg = fmt.Sprintf("%s: HTTP %d %s: %s", e.Endpoint, e.Status, e.Code, e.Message)
	}
	if len(e.Headers) > 0 {
		msg += " [" + strings.Join(e.Headers, " ") + "]"
	}
	return msg
}

// TokenRefused reports whether the refresh token itself was refused: only a new one
// reads.
func (e *APIError) TokenRefused() bool { return e.Kind == KindTokenRefused }

// toldHeaders reports whether a header tells about a limit: Retry-After and the rate
// limit headers of any convention.
func toldHeaders(name string) bool {
	n := strings.ToLower(name)
	return n == "retry-after" || strings.Contains(n, "ratelimit") || strings.Contains(n, "rate-limit")
}

func headersOf(h http.Header) []string {
	var out []string
	for name, vs := range h {
		if toldHeaders(name) && len(vs) > 0 {
			out = append(out, name+"="+maskVINs(vs[0]))
		}
	}
	slices.Sort(out)
	return out
}

// vinPattern is a VIN: 17 characters, without I, O nor Q (ISO 3779).
var vinPattern = regexp.MustCompile(`\b[A-HJ-NPR-Z0-9]{17}\b`)

// maskVINs hides the VINs a message may name: the message goes to the logs.
func maskVINs(s string) string { return vinPattern.ReplaceAllString(s, "[VIN]") }

func newAPIError(endpoint string, k call, resp *http.Response, body []byte) *APIError {
	e := &APIError{Endpoint: endpoint, Status: resp.StatusCode, Headers: headersOf(resp.Header)}
	code, message := errorBody(body)
	e.Message = message
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, rateRetry(resp.Header)
	case k == tokenCall:
		e.Code = code
		if slices.Contains(refusedGrants, code) {
			e.Kind = KindTokenRefused
		}
	case resp.StatusCode == http.StatusUnauthorized:
		e.Kind = KindUnauthorized
	case resp.StatusCode == http.StatusForbidden && strings.Contains(code+" "+message, fleetAPIHint):
		e.Kind = KindOwnerAPIClosed
	case resp.StatusCode == http.StatusRequestTimeout && k == vehicleCall:
		e.Kind = KindUnavailable
	}
	return e
}

// errorBody reads the error of both formats: the token endpoint's
// {"error", "error_description"} (RFC 6749 §5.2), and /api/1's
// {"response": null, "error", "error_description"}, whose error is the message. Without
// one, the body's beginning stands for the message.
func errorBody(body []byte) (code, message string) {
	var v struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &v) == nil {
		code, message = v.Error, v.Error
		if v.Description != "" {
			message = v.Description
		}
	}
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	message = maskVINs(message)
	const maxLen = 200
	if len(message) > maxLen {
		message = message[:maxLen]
	}
	return code, message
}

// rateRetry reads Retry-After, in seconds or as a date (against the response's Date,
// so that no clock is needed).
func rateRetry(h http.Header) time.Duration {
	ra := h.Get("Retry-After")
	if n, err := strconv.ParseInt(ra, 10, 64); err == nil && n > 0 {
		// Beyond what a Duration holds, the longest wait rather than a negative one.
		return time.Duration(min(n, math.MaxInt64/int64(time.Second))) * time.Second
	}
	at, err := http.ParseTime(ra)
	if err != nil {
		return defaultRateRetry
	}
	if now, err := http.ParseTime(h.Get("Date")); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return defaultRateRetry
}
