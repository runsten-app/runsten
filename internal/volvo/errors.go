package volvo

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
	KindOther        Kind = iota
	KindUnauthorized      // token expired or revoked: re-authentication
	KindRateLimited       // 429: wait RetryIn
	KindQuota             // daily quota exhausted: wait RetryIn, for the whole API
	KindNotFound          // unknown VIN or removed from the account
	KindKeyRefused        // the application key (vcc-api-key) is refused: not the token's fault
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
	if n, err := strconv.Atoi(header); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if m := rateRetryRe.FindStringSubmatch(msg); m != nil {
		n, _ := strconv.Atoi(m[1])
		return time.Duration(n) * time.Second
	}
	return defaultRateRetry
}

func quotaRetry(msg string) time.Duration {
	m := quotaRetryRe.FindStringSubmatch(msg)
	if m == nil {
		return defaultQuotaRetry
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	s, _ := strconv.Atoi(m[3])
	return time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute + time.Duration(s)*time.Second
}
