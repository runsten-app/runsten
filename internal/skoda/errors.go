package skoda

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind classifies an API error by the reaction expected from the collector.
type Kind int

// Error kinds.
const (
	KindOther Kind = iota
	// KindKeyExpired: the key passed its expiry (401, problem api-key-expired). Only a
	// new key, created in MyŠkoda, reads the vehicles again.
	KindKeyExpired
	// KindKeyRefused: any other 401, a key unknown or deleted in MyŠkoda.
	KindKeyRefused
	// KindNotAuthorized: the key does not cover the vehicle (403,
	// api-key-not-authorized): it was not ticked when the key was created, or was
	// unticked since.
	KindNotAuthorized
	// KindNotFound: no vehicle has the VIN (404).
	KindNotFound
	// KindRateLimited: 429, the VIN's quota spent (rate-limit-exceeded) or the vehicle
	// declining (vehicle-not-accepting-requests); wait RetryIn.
	KindRateLimited
	// KindBadRequest: 400, a request the client built wrong (an include value the API
	// no longer knows).
	KindBadRequest
)

// The problem types the kinds rest on: the last segment of the problem's type URI
// (https://public.api.connect.skoda-auto.cz/problems/<name>).
const (
	problemKeyExpired       = "api-key-expired"
	problemKeyNotAuthorized = "api-key-not-authorized"
)

// defaultRateRetry is the wait after a 429 that tells neither Retry-After nor
// RateLimit-Reset: a quarter of an hour (assumption: the docs promise Retry-After).
const defaultRateRetry = 15 * time.Minute

// APIError is a non-200 response from the API, as an RFC 9457 problem. It holds neither
// the key nor a VIN: the logs carry it.
type APIError struct {
	Status  int
	Problem string // the problem type's last segment; empty for about:blank or none
	Message string // the problem's title and detail, VINs masked
	Kind    Kind
	RetryIn time.Duration // for KindRateLimited
	// Limit is the VIN's quota the response told: a 5xx counts in it, and carries it.
	Limit RateLimit
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("vehicle: HTTP %d", e.Status)
	if e.Problem != "" {
		msg += " " + e.Problem
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Limit.Known {
		msg += fmt.Sprintf(" [ratelimit remaining=%d reset=%s]", e.Limit.Remaining, e.Limit.Reset)
	}
	return msg
}

// CredentialRefused reports whether the API refused the key itself, expired or not:
// only a new one reads.
func (e *APIError) CredentialRefused() bool {
	return e.Kind == KindKeyExpired || e.Kind == KindKeyRefused
}

// KeyExpired reports whether the key passed its expiry.
func (e *APIError) KeyExpired() bool { return e.Kind == KindKeyExpired }

// NotCovered reports whether the key does not cover the vehicle.
func (e *APIError) NotCovered() bool { return e.Kind == KindNotAuthorized || e.Kind == KindNotFound }

// vinPattern is a VIN: 17 characters, without I, O nor Q (ISO 3779).
var vinPattern = regexp.MustCompile(`\b[A-HJ-NPR-Z0-9]{17}\b`)

func maskVINs(s string) string { return vinPattern.ReplaceAllString(s, "[VIN]") }

func newAPIError(resp *http.Response, body []byte, limit RateLimit) *APIError {
	p := problemOf(body)
	e := &APIError{Status: resp.StatusCode, Problem: p.name(), Message: p.message(), Limit: limit}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		e.Kind = KindKeyRefused
		if e.Problem == problemKeyExpired {
			e.Kind = KindKeyExpired
		}
	case http.StatusForbidden:
		// api-key-not-authorized, or operation-not-authorized, which a read never
		// meets: either way the key does not read the vehicle.
		e.Kind = KindNotAuthorized
	case http.StatusNotFound:
		e.Kind = KindNotFound
	case http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, rateRetry(resp.Header, limit)
	case http.StatusBadRequest:
		e.Kind = KindBadRequest
	}
	return e
}

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func problemOf(body []byte) problem {
	var p problem
	if json.Unmarshal(body, &p) != nil {
		p.Detail = strings.TrimSpace(string(body))
	}
	return p
}

// name is the problem type's last segment; empty for about:blank.
func (p problem) name() string {
	if p.Type == "" || p.Type == "about:blank" {
		return ""
	}
	return p.Type[strings.LastIndexByte(p.Type, '/')+1:]
}

func (p problem) message() string {
	msg := p.Title
	if p.Detail != "" {
		if msg != "" {
			msg += ": "
		}
		msg += p.Detail
	}
	const maxLen = 200
	if len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	return maskVINs(msg)
}

// rateRetry reads Retry-After, in seconds or as a date (against the response's own
// Date), else the quota's reset, else the wait the source takes.
func rateRetry(h http.Header, limit RateLimit) time.Duration {
	if ra := h.Get("Retry-After"); ra != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
		if at, err := http.ParseTime(ra); err == nil {
			if now, err := http.ParseTime(h.Get("Date")); err == nil && at.After(now) {
				return at.Sub(now)
			}
		}
	}
	if limit.Known && limit.Reset > 0 {
		return limit.Reset
	}
	return defaultRateRetry
}
