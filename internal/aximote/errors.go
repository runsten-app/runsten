package aximote

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Kind classifies an API error by the reaction it calls for.
type Kind int

// Error kinds.
const (
	KindOther        Kind = iota
	KindTokenRefused      // 401 or 403: the token is unknown, revoked, or not allowed
	KindRateLimited       // 429: wait RetryIn
)

// defaultRetry is the wait after a 429 that does not say: quotas are not documented.
const defaultRetry = time.Minute

// APIError is a response other than 200. It holds the status only: the body is the
// API's, whose format is not described.
type APIError struct {
	Call    string
	Status  int
	Kind    Kind
	RetryIn time.Duration // for KindRateLimited
}

func (e *APIError) Error() string { return fmt.Sprintf("aximote %s: HTTP %d", e.Call, e.Status) }

// TokenRefused reports whether Aximote refused the token.
func (e *APIError) TokenRefused() bool { return e.Kind == KindTokenRefused }

func newAPIError(call string, resp *http.Response) *APIError {
	e := &APIError{Call: call, Status: resp.StatusCode}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		e.Kind = KindTokenRefused
	case http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, retryAfter(resp.Header.Get("Retry-After"))
	}
	return e
}

// retryAfter reads Retry-After in seconds. Assumption: the API gives seconds, if
// anything; a date is taken as missing.
func retryAfter(h string) time.Duration {
	if n, err := strconv.Atoi(h); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return defaultRetry
}
