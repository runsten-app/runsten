package volvo

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

// Kind classifies an API error by the reaction expected from the collector.
type Kind int

// Error kinds.
const (
	KindOther        Kind = iota
	KindUnauthorized      // token expired or revoked: re-authentication
	KindRateLimited       // 429: wait RetryIn
	KindQuota             // daily quota exhausted: wait RetryIn, for the whole API
	KindNotFound          // unknown VIN or removed from the account
	KindKeyRefused        // the application key (vcc-api-key) is refused: not the token's fault
	KindMarket            // the API does not serve the vehicle's market: nothing will be read
)

// Default delays when the response does not say when to retry.
const (
	defaultRateRetry  = time.Minute
	defaultQuotaRetry = time.Hour
)

// APIError is a non-200 response from the API.
type APIError struct {
	Endpoint string
	Status   int
	Message  string
	Kind     Kind
	RetryIn  time.Duration // for KindRateLimited and KindQuota
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: HTTP %d: %s", e.Endpoint, e.Status, e.Message)
}

// KeyRefused reports whether the application key was refused, rather than the token:
// the connection keeps its grant, and only a new key reads its vehicles again.
func (e *APIError) KeyRefused() bool { return e.Kind == KindKeyRefused }

// Retry-delay message patterns: the quota message was reported by the Home Assistant
// community, the rate-limit message is assumed (Azure API Management style).
var (
	rateRetryRe  = regexp.MustCompile(`in (\d+) seconds`)
	quotaRetryRe = regexp.MustCompile(`replenished in (\d+):(\d{2}):(\d{2})`)
)

// keyRefusedRe tells a refused application key from a refused token, in a 401 or a 403
// that is not the quota's. The gateway names the key, observed on the real API: 401
// "Access denied due to invalid VCC-API-KEY. Make sure to provide a valid key for an
// active application." for an unknown key, "… missing header VCC-API-KEY. …" without
// one. A refusal that does not name the key stays a token's.
var keyRefusedRe = regexp.MustCompile(`(?i)(subscription|vcc-api|api)[ -]?key`)

// marketMessage tells a vehicle whose market the API does not serve from one it does not
// know. Observed on the real API (2026-10): the Energy API's 404 says it on energy-state,
// while every Connected Vehicle and Location endpoint, the details included, answers a
// plain 404 "NOT_FOUND" for the same vehicle. The refusal names no market.
const marketMessage = "does not belong to allowed markets"

func newAPIError(endpoint string, resp *http.Response, body []byte) *APIError {
	e := &APIError{Endpoint: endpoint, Status: resp.StatusCode, Message: errorMessage(body)}
	switch {
	case resp.StatusCode == http.StatusForbidden && strings.Contains(e.Message, "Out of call volume quota"):
		e.Kind, e.RetryIn = KindQuota, quotaRetry(e.Message)
	case (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) &&
		keyRefusedRe.MatchString(e.Message):
		e.Kind = KindKeyRefused
	case resp.StatusCode == http.StatusUnauthorized:
		e.Kind = KindUnauthorized
	case resp.StatusCode == http.StatusNotFound && strings.Contains(e.Message, marketMessage):
		e.Kind = KindMarket
	case resp.StatusCode == http.StatusNotFound:
		e.Kind = KindNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, rateRetry(resp.Header.Get("Retry-After"), e.Message)
	}
	return e
}

// errorMessage extracts the message from the four observed error formats:
// {"error":{"message"}}, {"code","message"}, {"statusCode","message"}.
func errorMessage(body []byte) string {
	var v struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &v) == nil {
		if v.Message != "" {
			return v.Message
		}
		if v.Error.Message != "" {
			return v.Error.Message
		}
	}
	const maxLen = 200
	s := strings.TrimSpace(string(body))
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

func rateRetry(header, msg string) time.Duration {
	if n, err := strconv.ParseInt(header, 10, 64); err == nil && n > 0 {
		return seconds(n)
	}
	if m := rateRetryRe.FindStringSubmatch(msg); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 64) // out of range: the largest int64
		return seconds(n)
	}
	return defaultRateRetry
}

// seconds converts a wait the API gives in seconds: beyond what a Duration holds, the
// longest wait rather than a negative one.
func seconds(n int64) time.Duration {
	return time.Duration(min(n, math.MaxInt64/int64(time.Second))) * time.Second
}

func quotaRetry(msg string) time.Duration {
	m := quotaRetryRe.FindStringSubmatch(msg)
	if m == nil {
		return defaultQuotaRetry
	}
	h, _ := strconv.ParseInt(m[1], 10, 64) // out of range: the largest int64
	mi, _ := strconv.ParseInt(m[2], 10, 64)
	s, _ := strconv.ParseInt(m[3], 10, 64)
	// The hours capped first, so that the seconds they make cannot wrap either.
	h = min(h, math.MaxInt64/int64(time.Second)/3600)
	return seconds(h*3600 + mi*60 + s)
}
