package polestarid

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind classifies an error by the reaction expected from the collector.
type Kind int

// Error kinds. A 5xx, a 404 and anything else are KindOther: tried again at the next
// pass.
const (
	KindOther Kind = iota
	// KindTokenRefused: the refresh token or the code no longer gives tokens
	// (invalid_grant). With the password, a sign-in reads again; else only the owner.
	KindTokenRefused
	// KindSignInRefused: the email or the password is wrong (ERR001).
	KindSignInRefused
	// KindUnauthorized: the API refused the access token (expired, revoked); it is
	// refreshed, once.
	KindUnauthorized
	// KindRateLimited: 429, wait RetryIn.
	KindRateLimited
)

// defaultRateRetry is the delay after a 429 without Retry-After: no limit of the API is
// published.
const defaultRateRetry = 5 * time.Minute

// call is what a request asks for: the same status means another thing on each.
type call int

const (
	tokenCall call = iota
	graphqlCall
)

// APIError is a refusal of the Polestar ID or of the API. It holds neither a token, a
// password nor a VIN: the logs carry it.
type APIError struct {
	Endpoint string // token, sign-in, cars or telematics
	Status   int
	Code     string // the OAuth error of the token endpoint; empty elsewhere
	Message  string
	Kind     Kind
	RetryIn  time.Duration // for KindRateLimited
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: HTTP %d %s: %s", e.Endpoint, e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Endpoint, e.Status, e.Message)
}

// Refused reports whether the owner must sign in again: the refresh token, the code or
// the password refused.
func (e *APIError) Refused() bool { return e.Kind == KindTokenRefused || e.Kind == KindSignInRefused }

func newAPIError(endpoint string, k call, resp *http.Response, body []byte) *APIError {
	code, message := errorBody(body)
	e := &APIError{Endpoint: endpoint, Status: resp.StatusCode, Message: message}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, rateRetry(resp.Header)
	case k == tokenCall:
		e.Code = code
		if code == "invalid_grant" {
			e.Kind = KindTokenRefused
		}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		e.Kind = KindUnauthorized
	}
	return e
}

// unauthorizedRe recognizes a GraphQL error that refuses the access token: its wording
// on the real API is not seen yet.
var unauthorizedRe = regexp.MustCompile(`(?i)unauthori[sz]ed|unauthenticated|token.*(expired|invalid)`)

func graphqlError(endpoint string, status int, message string) *APIError {
	e := &APIError{Endpoint: endpoint, Status: status, Message: clip(maskVINs(message))}
	if unauthorizedRe.MatchString(message) {
		e.Kind = KindUnauthorized
	}
	return e
}

// errorBody reads the error of the token endpoint ({"error", "error_description"}, RFC
// 6749 §5.2) and of the GraphQL ({"errors": [{"message"}]}). Without one, the body's
// beginning stands for the message.
func errorBody(body []byte) (code, message string) {
	var v struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
		Errors      []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &v) == nil {
		code, message = v.Error, v.Error
		if v.Description != "" {
			message = v.Description
		}
		if message == "" && len(v.Errors) > 0 {
			message = v.Errors[0].Message
		}
	}
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	return code, clip(maskVINs(message))
}

func clip(s string) string {
	const maxLen = 200
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// vinPattern is a VIN: 17 characters, without I, O nor Q (ISO 3779).
var vinPattern = regexp.MustCompile(`\b[A-HJ-NPR-Z0-9]{17}\b`)

// maskVINs hides the VINs a message may name: the message goes to the logs.
func maskVINs(s string) string { return vinPattern.ReplaceAllString(s, "[VIN]") }

// rateRetry reads Retry-After, in seconds or as a date (against the response's Date, so
// that no clock is needed).
func rateRetry(h http.Header) time.Duration {
	ra := h.Get("Retry-After")
	if n, err := strconv.ParseInt(ra, 10, 64); err == nil && n > 0 {
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
