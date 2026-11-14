package renault

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Kind classifies an API error by the reaction expected from its caller.
type Kind int

// Error kinds.
const (
	KindOther Kind = iota
	// KindCredentialRefused: Gigya refused the email and password (403042, which a
	// stale application key answers too), or the login token no longer signs in
	// (403005, 403013). Only a new sign-in reads the connection's vehicles again.
	KindCredentialRefused
	// KindTFARequired: the account asks for its one-time code (403101); RegToken
	// starts it.
	KindTFARequired
	// KindWrongCode: the one-time code entered is wrong (400006); the challenge stays
	// open.
	KindWrongCode
	// KindUnauthorized: the JWT expired or was revoked; another is asked for, once.
	KindUnauthorized
	// KindRateLimited: the account's quota is spent (err.func.wired.overloaded); wait
	// RetryIn.
	KindRateLimited
	// KindNotAvailable: the vehicle does not serve the endpoint (its model, its
	// contract, or the privacy mode set in the car).
	KindNotAvailable
	// KindUnshared: the account no longer reads the vehicle (err.func.not.connected,
	// the account's id changed in October 2026).
	KindUnshared
)

// quotaWait is how long the account waits after Kamereon's quota error, which tells no
// time: Home Assistant's fifteen minutes (ADR 0050 §3).
const quotaWait = 15 * time.Minute

// The codes the kinds rest on.
const (
	gigyaInvalidLogin = 403042
	gigyaPendingTFA   = 403101
	gigyaUnauthorized = 403005
	gigyaUnverified   = 403013
	gigyaWrongCode    = 400006
	codeUnauthorized  = "err.func.wired.unauthorized"
	codeOverloaded    = "err.func.wired.overloaded"
	codeNotConnected  = "err.func.not.connected"
	codePrivacy       = "err.func.privacy.on"
	codeAccessDenied  = "err.func.403"
	codeForbidden     = "err.func.wired.forbidden"
	codeNotSupported  = "err.tech.501"
	codeNotFound      = "err.func.wired.notFound"
	codeURLNotFound   = "err.func.wired.not-found"
	codeMissingParam  = "err.func.wired.missingRequestParam"
	codeNotAllowed    = "err.func.wired.methodNotAllowed"
)

// notAvailable are Kamereon's codes of an endpoint a vehicle does not serve.
var notAvailable = map[string]bool{
	codeAccessDenied: true, codeForbidden: true, codeNotSupported: true, codeNotFound: true,
	codeURLNotFound: true, codeMissingParam: true, codeNotAllowed: true, codePrivacy: true,
}

// APIError is a refusal of Gigya or Kamereon. It holds neither a token, a password nor
// a VIN: the logs carry it.
type APIError struct {
	Call    string // the call's name
	Status  int
	Code    string // Gigya's errorCode or Kamereon's errors[0].errorCode; empty without one
	Message string
	Kind    Kind
	RetryIn time.Duration // for KindRateLimited
	// RegToken is the registration token of a login that asks for the one-time code.
	RegToken string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: HTTP %d %s: %s", e.Call, e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Call, e.Status, e.Message)
}

// CredentialRefused reports whether Renault refused the email and password, or the
// login token: only a new sign-in reads.
func (e *APIError) CredentialRefused() bool { return e.Kind == KindCredentialRefused }

// Privacy reports whether the car's privacy mode refused the read: its owner turns it
// off in the car.
func (e *APIError) Privacy() bool { return e.Code == codePrivacy }

// vinPattern is a VIN: 17 characters, without I, O nor Q (ISO 3779).
var vinPattern = regexp.MustCompile(`\b[A-HJ-NPR-Z0-9]{17}\b`)

func maskVINs(s string) string { return vinPattern.ReplaceAllString(s, "[VIN]") }

// gigyaError reads Gigya's envelope: nil when its errorCode is 0. Gigya answers 200
// with an error code at times, and its status mirrors the code otherwise.
func gigyaError(name string, resp *http.Response, body []byte) *APIError {
	var v struct {
		ErrorCode    *int   `json:"errorCode"`
		ErrorDetails string `json:"errorDetails"`
		ErrorMessage string `json:"errorMessage"`
		RegToken     string `json:"regToken"`
	}
	if err := json.Unmarshal(body, &v); err != nil || v.ErrorCode == nil {
		if resp.StatusCode == http.StatusOK && err == nil {
			return nil // no envelope: the bootstrap's
		}
		return &APIError{Call: name, Status: resp.StatusCode, Message: snippet(body)}
	}
	if *v.ErrorCode == 0 {
		return nil
	}
	e := &APIError{Call: name, Status: resp.StatusCode, Code: fmt.Sprint(*v.ErrorCode), Message: v.ErrorDetails}
	if e.Message == "" {
		e.Message = v.ErrorMessage
	}
	e.Message = maskVINs(e.Message)
	switch *v.ErrorCode {
	case gigyaInvalidLogin, gigyaUnauthorized, gigyaUnverified:
		e.Kind = KindCredentialRefused
	case gigyaPendingTFA:
		e.Kind, e.RegToken = KindTFARequired, v.RegToken
	case gigyaWrongCode:
		e.Kind = KindWrongCode
	}
	return e
}

// kamereonError reads Kamereon's error body, which it tells before its status (a 200
// never carries one): nil for a 200. A 502 may be HTML.
func kamereonError(name string, resp *http.Response, body []byte) *APIError {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	e := &APIError{Call: name, Status: resp.StatusCode}
	var v struct {
		Errors []struct {
			ErrorCode    string `json:"errorCode"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"errors"`
		Messages []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &v) == nil {
		switch {
		case len(v.Errors) > 0:
			e.Code, e.Message = v.Errors[0].ErrorCode, v.Errors[0].ErrorMessage
			// A numeric code (409001) names the message's own: the one of the
			// family is in messages.
			if !strings.HasPrefix(e.Code, "err.") && len(v.Messages) > 0 && strings.HasPrefix(v.Messages[0].Code, "err.") {
				e.Code = v.Messages[0].Code
			}
		case len(v.Messages) > 0:
			e.Code, e.Message = v.Messages[0].Code, v.Messages[0].Message
		}
	}
	if e.Message == "" {
		e.Message = snippet(body)
	}
	e.Message = maskVINs(e.Message)
	switch {
	case e.Code == codeOverloaded || resp.StatusCode == http.StatusTooManyRequests:
		e.Kind, e.RetryIn = KindRateLimited, quotaWait
	case e.Code == codeUnauthorized || e.Code == "" && resp.StatusCode == http.StatusUnauthorized:
		e.Kind = KindUnauthorized
	case e.Code == codeNotConnected:
		e.Kind = KindUnshared
	case notAvailable[e.Code]:
		e.Kind = KindNotAvailable
	}
	return e
}

// snippet is a body's beginning, for a message.
func snippet(body []byte) string {
	const maxLen = 200
	s := strings.TrimSpace(string(body))
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// WrongCode reports whether Renault refused the one-time code entered; the challenge
// stays open.
func (e *APIError) WrongCode() bool { return e.Kind == KindWrongCode }
