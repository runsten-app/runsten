package pleos

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
	// KindCredentialRefused: the key no longer reads (refused, revoked, expired or
	// reissued in the Playground; observed a 400 on a wrong secret, its body unknown;
	// documented 404 "40404", user or key not found). Only a new key reads the
	// connection's vehicles again.
	KindCredentialRefused
	// KindUnauthorized: the token expired, or another token asked for with the same
	// key took its place (the one living at a time). A new one is asked for, once.
	KindUnauthorized
	// KindUnshared: the vehicle's consent was withdrawn, or it is no longer
	// registered to the key.
	KindUnshared
	// KindNotAvailable: the endpoint has no data for the vehicle (4045, the doc's no
	// data).
	KindNotAvailable
	// KindRateLimited: 429, wait RetryIn.
	KindRateLimited
)

// The error codes of the API the kinds rest on: Pleos writes them as strings in its
// doc's examples ("40003"), and a number has been observed (403, the research §1.8):
// both are read. The statuses are not trusted: the doc's table puts a 401, a 403 and
// a 404 behind an HTTP 400, and the observed 403 came with an HTTP 403.
const (
	codeUnauthorized = "401"
	codeForbidden    = "403"
	codeMissingAuth  = "40002"
	codeTokenExpired = "40003"
	codeTokenInvalid = "40004"
	codeInvalidAuthz = "4011"
	codeNoPermission = "5006"
	codeNoData       = "4045"
	codeUnregistered = "4101"
)

// unauthorized are the codes that tell the token no longer reads: the doc's table lists
// them behind an HTTP 400, and the observed answer of a token another one replaced.
var unauthorized = []string{codeUnauthorized, codeForbidden, codeMissingAuth, codeTokenExpired, codeTokenInvalid, codeInvalidAuthz}

// defaultRateRetry is the wait after a 429 without Retry-After: none is published, so
// the source takes fifteen minutes (assumption, ADR 0049 §3).
const defaultRateRetry = 15 * time.Minute

// call is what a request asks for: the refusal of the key is the token's alone, the
// others are a data call's.
type call int

const (
	tokenCall call = iota
	dataCall
)

// APIError is a non-200 response from the API. It holds neither the token nor the key,
// nor a VIN: the logs carry it.
type APIError struct {
	Endpoint string // the endpoint's name
	Status   int
	Code     string // the API's code (error.code), read as a string or a number; empty without one
	Message  string
	Kind     Kind
	RetryIn  time.Duration // for KindRateLimited
	// Headers are the response's headers that tell of a limit, as "Name=value"
	// sorted: what a 429 comes with is not published either.
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

// CredentialRefused reports whether the API refused the key itself: only a new one reads.
func (e *APIError) CredentialRefused() bool { return e.Kind == KindCredentialRefused }

// vinPattern is a VIN: 17 characters, without I, O nor Q (ISO 3779).
var vinPattern = regexp.MustCompile(`\b[A-HJ-NPR-Z0-9]{17}\b`)

func maskVINs(s string) string { return vinPattern.ReplaceAllString(s, "[VIN]") }

func newAPIError(endpoint string, k call, resp *http.Response, body []byte) *APIError {
	e := &APIError{Endpoint: endpoint, Status: resp.StatusCode, Code: errorBodyCode(body)}
	e.Message = maskVINs(errorBodyMessage(body))
	e.Headers = headersOf(resp.Header)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, rateRetry(resp.Header)
	case k == tokenCall && resp.StatusCode < http.StatusInternalServerError:
		// The doc lists "invalid parameters" for what a request leaves wrong: the
		// client always sends the key whole, so a refusal there is the key's. A 5xx
		// is the API's own failure, which tells nothing of the key.
		e.Kind = KindCredentialRefused
	case e.Code == codeNoPermission || e.Code == codeUnregistered:
		// The consent withdrawn (5006), or the VIN no longer registered to the key
		// (410, 4101).
		e.Kind = KindUnshared
	case e.Code == codeNoData:
		e.Kind = KindNotAvailable
	case resp.StatusCode == http.StatusUnauthorized || slices.Contains(unauthorized, e.Code):
		// The doc's own 401 (behind an HTTP 400) or the observed HTTP 403 of a
		// token another took the place of.
		e.Kind = KindUnauthorized
	}
	return e
}

// errorBodyCode reads the error envelope's code, a string or a number.
func errorBodyCode(body []byte) string {
	var v struct {
		Error struct {
			Code json.Number `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	return string(v.Error.Code)
}

// errorBodyMessage reads the error envelope's message, or the body's beginning without
// one.
func errorBodyMessage(body []byte) string {
	var v struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &v) == nil && v.Error.Message != "" {
		v.Message = v.Error.Message
	}
	const maxLen = 200
	if len(v.Message) > maxLen {
		v.Message = v.Message[:maxLen]
	}
	if v.Message == "" {
		v.Message = strings.TrimSpace(string(body))
		if len(v.Message) > maxLen {
			v.Message = v.Message[:maxLen]
		}
	}
	return v.Message
}

// headersOf lists the response's headers that tell of a limit.
func headersOf(h http.Header) []string {
	var out []string
	for name, vs := range h {
		n := strings.ToLower(name)
		if (n == "retry-after" || strings.Contains(n, "ratelimit") || strings.Contains(n, "rate-limit")) && len(vs) > 0 {
			out = append(out, name+"="+maskVINs(vs[0]))
		}
	}
	slices.Sort(out)
	return out
}

// rateRetry reads Retry-After, in seconds or as a date (against the response's own
// Date, so that no clock is needed). Without it: the wait the source takes, none being
// published (assumption).
func rateRetry(h http.Header) time.Duration {
	if ra := h.Get("Retry-After"); ra != "" {
		if n, err := strconv.Atoi(ra); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
		if at, err := http.ParseTime(ra); err == nil {
			if now, err := http.ParseTime(h.Get("Date")); err == nil && at.After(now) {
				return at.Sub(now)
			}
		}
	}
	return defaultRateRetry
}
