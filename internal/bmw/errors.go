package bmw

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Kind classifies an error of CarData by what the caller does of it. A 5xx (CU-500,
// CU-503) and anything else are KindOther: tried again later.
type Kind int

// Error kinds.
const (
	KindOther Kind = iota
	// KindTokenRefused: the refresh token no longer refreshes (invalid_grant): expired
	// after two weeks unused, or the subscription withdrawn in the portal. Only the device
	// flow, run again, reads the account again.
	KindTokenRefused
	// KindClientRefused: the client ID is unknown to BMW (invalid_client).
	KindClientRefused
	// KindPending: the owner has not approved the device code yet; KindSlowDown: the
	// same, polled too often.
	KindPending
	KindSlowDown
	// KindExpired: the device code expired before the owner approved it; KindDenied: the
	// owner refused it.
	KindExpired
	KindDenied
	// KindUnauthorized: the access token is missing, refused or expired (CU-100, CU-101,
	// CU-102); it is refreshed, once.
	KindUnauthorized
	// KindScopeMissing: the token lacks CarData's scope (CU-103): the owner subscribes to
	// both in the portal, then runs the device flow again.
	KindScopeMissing
	// KindUnshared: the VIN is not mapped to the account, or not telematics capable
	// (CU-104).
	KindUnshared
	// KindRateLimited: the day's calls are spent (CU-429, in a 429 or a 403), until 00:00
	// UTC.
	KindRateLimited
	// KindNotFound: what the call names does not exist, a container deleted in the
	// portal (assumed a 404).
	KindNotFound
)

// The OAuth errors of the token endpoint (RFC 6749 §5.2, RFC 8628 §3.5).
var oauthKinds = map[string]Kind{
	"invalid_grant":         KindTokenRefused,
	"invalid_client":        KindClientRefused,
	"unauthorized_client":   KindClientRefused,
	"authorization_pending": KindPending,
	"slow_down":             KindSlowDown,
	"expired_token":         KindExpired,
	"access_denied":         KindDenied,
}

// The API's error codes (BMW's guide, ch. 3.4).
var apiKinds = map[string]Kind{
	"CU-100": KindUnauthorized,
	"CU-101": KindUnauthorized,
	"CU-102": KindUnauthorized,
	"CU-103": KindScopeMissing,
	"CU-104": KindUnshared,
	"CU-429": KindRateLimited,
}

// call is what a request asks: the same status means another thing on each.
type call int

const (
	authCall call = iota
	apiCall
)

// APIError is an error response of the authentication or of the API. It holds neither a
// token nor a VIN: the logs carry it.
type APIError struct {
	Endpoint string // device_code, token, mappings, basic-data…
	Status   int
	// Code is the OAuth error of the authentication, or CarData's (CU-104).
	Code    string
	Message string
	Kind    Kind
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: HTTP %d %s: %s", e.Endpoint, e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Endpoint, e.Status, e.Message)
}

// vinPattern is a VIN: 17 characters, without I, O nor Q (ISO 3779).
var vinPattern = regexp.MustCompile(`\b[A-HJ-NPR-Z0-9]{17}\b`)

// maskVINs hides the VINs a message may name: the message goes to the logs.
func maskVINs(s string) string { return vinPattern.ReplaceAllString(s, "[VIN]") }

func newAPIError(endpoint string, k call, status int, body []byte) *APIError {
	e := &APIError{Endpoint: endpoint, Status: status}
	var v struct {
		// The authentication's.
		Error       string `json:"error"`
		Description string `json:"error_description"`
		// The API's (CustomerErrorResponse).
		ID  string `json:"exveErrorId"`
		Msg string `json:"exveErrorMsg"`
	}
	_ = json.Unmarshal(body, &v) // another body: its beginning stands for the message
	switch k {
	case authCall:
		e.Code, e.Message = v.Error, v.Description
		e.Kind = oauthKinds[v.Error]
	case apiCall:
		e.Code, e.Message = v.ID, v.Msg
		var ok bool
		if e.Kind, ok = apiKinds[v.ID]; !ok {
			switch status {
			case http.StatusUnauthorized:
				e.Kind = KindUnauthorized
			case http.StatusTooManyRequests:
				e.Kind = KindRateLimited
			case http.StatusNotFound:
				e.Kind = KindNotFound
			}
		}
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(body))
	}
	e.Message = maskVINs(e.Message)
	const maxLen = 200
	if len(e.Message) > maxLen {
		e.Message = e.Message[:maxLen]
	}
	return e
}
