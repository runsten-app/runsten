package polestar

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Kind classifies an API error by the reaction expected from the collector.
type Kind int

// Error kinds.
const (
	KindOther Kind = iota
	// KindCredentialRefused: the credential no longer reads (a 401 on /token: refused
	// or expired after its 90 days; or an account ID that is not the credential's). Only
	// a new credential reads the connection's vehicles again.
	KindCredentialRefused
	// KindUnauthorized: a 401 on /v1, the access token expired; a new one is asked, once.
	KindUnauthorized
	// KindVINUnauthorized: the vehicle is no longer shared with the credential.
	KindVINUnauthorized
	// KindDomainNotGranted: a 403 on a domain, of another code: its scope was not
	// granted to the credential.
	KindDomainNotGranted
	// KindNotAvailable: the car does not upload the domain (DATA_NOT_AVAILABLE).
	KindNotAvailable
	// KindRateLimited: 429, wait RetryIn.
	KindRateLimited
)

// Error codes of /v1 the kinds rest on, observed on the real API.
const (
	codeVINUnauthorized  = "AUTHZ_VIN_UNAUTHORIZED"
	codeClientIDMismatch = "AUTHZ_CLIENT_ID_MISMATCH"
	codeNotAvailable     = "DATA_NOT_AVAILABLE"
)

// defaultRateRetry is the delay after a 429 without Retry-After: the limit is counted
// per minute (100 calls, according to the community).
const defaultRateRetry = time.Minute

// call is what a request asks for: the same status means another thing on each.
type call int

const (
	tokenCall call = iota
	listCall
	domainCall
)

// APIError is a non-200 response from the API. It holds neither the token nor the
// credential, nor a VIN: the logs carry it.
type APIError struct {
	Endpoint string // token, vehicles, or the domain
	Status   int
	Code     string // the API's code (error.code on /v1, error on /token); empty without one
	Message  string
	Kind     Kind
	RetryIn  time.Duration // for KindRateLimited
	// Headers are the response's headers that tell about a limit or a refusal (see
	// toldHeaders), as "Name=value" sorted by name: what a 401 or a 429 of the real API
	// comes with is not known yet.
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

// toldHeaders reports whether a header tells about a limit or a refusal: Retry-After,
// the rate limit headers of any convention, and the gateway's error type (AWS).
func toldHeaders(name string) bool {
	n := strings.ToLower(name)
	return n == "retry-after" || n == "x-amzn-errortype" || strings.Contains(n, "ratelimit") || strings.Contains(n, "rate-limit")
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

// maskVINs hides the VINs a message of the API may name ("VIN … is not authorized"):
// the message goes to the logs.
func maskVINs(s string) string { return vinPattern.ReplaceAllString(s, "[VIN]") }

// CredentialRefused reports whether the Data Portal refused the credential itself: only
// a new one reads.
func (e *APIError) CredentialRefused() bool { return e.Kind == KindCredentialRefused }

func newAPIError(endpoint string, k call, resp *http.Response, body []byte) *APIError {
	e := &APIError{Endpoint: endpoint, Status: resp.StatusCode, Headers: headersOf(resp.Header)}
	e.Code, e.Message = errorBody(body)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, rateRetry(resp.Header)
	case k == tokenCall:
		if resp.StatusCode == http.StatusUnauthorized {
			e.Kind = KindCredentialRefused
		}
	case resp.StatusCode == http.StatusUnauthorized:
		// Assumption: the specification lists no 401 on /v1, nor its body; an expired
		// token is refused by the gateway, whatever it writes.
		e.Kind = KindUnauthorized
	case resp.StatusCode == http.StatusForbidden && e.Code == codeClientIDMismatch:
		e.Kind = KindCredentialRefused
	case resp.StatusCode == http.StatusForbidden && e.Code == codeVINUnauthorized:
		e.Kind = KindVINUnauthorized
	case resp.StatusCode == http.StatusForbidden && k == domainCall && e.Code != "":
		// Assumption: the specification gives a 403 for a scope not granted, without its
		// code. A 403 without the API's body is the gateway's ("Missing Authentication
		// Token" on a path it does not serve), not a scope's.
		e.Kind = KindDomainNotGranted
	case resp.StatusCode == http.StatusNotFound && e.Code == codeNotAvailable:
		e.Kind = KindNotAvailable
	}
	return e
}

// errorBody reads the code and the message of the three formats: /v1's
// {"error": {"code", "message", …}}, /token's {"error", "error_description"}, and the
// gateway's {"message"}. Without a message, the body's beginning stands for it.
func errorBody(body []byte) (code, message string) {
	var v struct {
		Error       json.RawMessage `json:"error"`
		Description string          `json:"error_description"`
		Message     string          `json:"message"`
	}
	if json.Unmarshal(body, &v) == nil {
		var m2m struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		switch {
		case json.Unmarshal(v.Error, &m2m) == nil && (m2m.Code != "" || m2m.Message != ""):
			code, message = m2m.Code, m2m.Message
		case json.Unmarshal(v.Error, &code) == nil:
			message = v.Description
		}
		if message == "" {
			message = v.Message
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
// so that no clock is needed). A 429 has been seen without its headers being published.
func rateRetry(h http.Header) time.Duration {
	ra := h.Get("Retry-After")
	if n, err := strconv.Atoi(ra); err == nil && n > 0 {
		return time.Duration(n) * time.Second
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
