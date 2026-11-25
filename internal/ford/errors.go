package ford

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind classifies an error by the reaction expected from the collector.
type Kind int

// Error kinds.
const (
	KindOther Kind = iota
	// KindGrantRefused: the token endpoint refused the code or the refresh token
	// (invalid_grant): the consent was withdrawn, or its year is over. Only a new
	// sign-in reads again.
	KindGrantRefused
	// KindClientRefused: the token endpoint refused the application (invalid_client,
	// unauthorized_client): a wrong ID or secret, or an application Ford has not yet
	// made ready (reports say up to two hours after its creation).
	KindClientRefused
	// KindUnauthorized: the API refused the access (401): expired, to ask for again.
	KindUnauthorized
	// KindForbidden: the API refused the read (403). What Ford means by it is not
	// known (assumption: a data category not ticked, or a consent withdrawn).
	KindForbidden
	// KindNotFound: the API knows no such resource (404).
	KindNotFound
	// KindRateLimited: 429, from the API or from the token endpoint; wait RetryIn.
	KindRateLimited
)

// defaultRateRetry is the wait after a 429 that tells no Retry-After: the five minutes
// Ford's token endpoint told during an outage (assumption for the API's).
const defaultRateRetry = 5 * time.Minute

// outageCode is the Azure AD B2C error the token endpoint answered every account during
// an outage of Ford's sign-in (reported by the users of other clients): a failure of
// Ford's own back end, not a refusal of the grant.
const outageCode = "AADB2C90027"

// APIError is a refusal of the API or of the token endpoint. It holds neither a token,
// the secret nor a VIN: the logs carry it.
type APIError struct {
	// Op is what was asked: an endpoint, or "token".
	Op     string
	Status int
	// Code is the OAuth error of the token endpoint (invalid_grant); empty for the API.
	Code    string
	Message string // the error's message, VINs masked, cut short
	Kind    Kind
	RetryIn time.Duration // for KindRateLimited
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s: HTTP %d", e.Op, e.Status)
	if e.Code != "" {
		msg += " " + e.Code
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// CredentialRefused reports whether only the owner can make the connection read again:
// a new sign-in, or the application's ID and secret given again.
func (e *APIError) CredentialRefused() bool {
	return e.Kind == KindGrantRefused || e.Kind == KindClientRefused
}

// vinPattern is a VIN: 17 characters, without I, O nor Q (ISO 3779).
var vinPattern = regexp.MustCompile(`\b[A-HJ-NPR-Z0-9]{17}\b`)

func maskVINs(s string) string { return vinPattern.ReplaceAllString(s, "[VIN]") }

// clean masks the VINs of a message and cuts it short: masked whole first, a VIN cut by
// the truncation would no longer match.
func clean(msg string) string {
	msg = maskVINs(strings.TrimSpace(msg))
	const maxLen = 200
	if len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	return msg
}

// newTokenError reads the token endpoint's refusal: an OAuth error, with Azure AD B2C's
// description.
func newTokenError(resp *http.Response, body []byte) *APIError {
	var o struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &o) != nil {
		o.Description = string(body)
	}
	e := &APIError{Op: "token", Status: resp.StatusCode, Code: o.Error, Message: clean(o.Description)}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, retryAfter(resp.Header)
	case strings.Contains(o.Description, outageCode):
		// Ford's own failure, whatever the OAuth error it rides under: the grant stands.
		e.Kind = KindOther
	case o.Error == "invalid_grant":
		e.Kind = KindGrantRefused
	case o.Error == "invalid_client" || o.Error == "unauthorized_client":
		e.Kind = KindClientRefused
	}
	return e
}

// newAPIError reads the API's refusal: the specification's ErrorDTO (error, message), or
// whatever the gateway answers in its place.
func newAPIError(ep Endpoint, resp *http.Response, body []byte) *APIError {
	var dto struct {
		Message      string `json:"message"`
		ErrorMessage string `json:"errorMessage"`
	}
	msg := ""
	if json.Unmarshal(body, &dto) == nil {
		msg = dto.Message
		if msg == "" {
			msg = dto.ErrorMessage
		}
	} else {
		msg = string(body)
	}
	e := &APIError{Op: string(ep), Status: resp.StatusCode, Message: clean(msg)}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		e.Kind = KindUnauthorized
	case http.StatusForbidden:
		e.Kind = KindForbidden
	case http.StatusNotFound:
		e.Kind = KindNotFound
	case http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, retryAfter(resp.Header)
	}
	return e
}

// retryAfter reads Retry-After, in seconds or as a date (against the response's own
// Date), else the wait the source takes.
func retryAfter(h http.Header) time.Duration {
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
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
