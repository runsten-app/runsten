package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// TestNewError checks how the errors huma raises itself map to the contract.
func TestNewError(t *testing.T) {
	param := &huma.ErrorDetail{Location: "query.limit", Message: "expected number >= 1"}
	field := &huma.ErrorDetail{Location: "body.remember", Message: "unexpected property"}
	for _, tt := range []struct {
		name    string
		status  int
		errs    []error
		want    int
		code    errorCode
		message string
	}{
		{"invalid parameter", http.StatusUnprocessableEntity, []error{param}, http.StatusBadRequest, codeInvalidParameter, "query.limit"},
		{"unparsable parameter", http.StatusBadRequest, []error{param, nil}, http.StatusBadRequest, codeInvalidParameter, "query.limit"},
		{"invalid body", http.StatusUnprocessableEntity, []error{param, field}, http.StatusBadRequest, codeInvalidBody, "body.remember"},
		{"body too large", http.StatusRequestEntityTooLarge, nil, http.StatusBadRequest, codeInvalidBody, "failed"},
		{"content type", http.StatusUnsupportedMediaType, nil, http.StatusUnsupportedMediaType, codeUnsupportedMediaType, "application/json"},
		{"unauthorized", http.StatusUnauthorized, nil, http.StatusUnauthorized, codeUnauthorized, "failed"},
		{"not found", http.StatusNotFound, nil, http.StatusNotFound, codeNotFound, "failed"},
		{"plain error", http.StatusBadRequest, []error{errors.New("plain")}, http.StatusBadRequest, codeInvalidParameter, "plain"},
		{"internal, without detail", http.StatusInternalServerError, []error{errors.New("secret")}, http.StatusInternalServerError, codeInternal, "see the logs"},
	} {
		e := newError(tt.status, "failed", tt.errs...)
		got, ok := e.(*errorJSON)
		if !ok || got.GetStatus() != tt.want || got.Detail.Code != tt.code || !strings.Contains(got.Error(), tt.message) ||
			strings.Contains(got.Error(), "secret") {
			t.Errorf("%s: %d %+v", tt.name, e.GetStatus(), e)
		}
	}
}

func TestNull(t *testing.T) {
	for _, tt := range []struct {
		v    null[positionJSON]
		want string
	}{
		{null[positionJSON]{}, "null"},
		{known(positionJSON{Lat: 1, Lon: 2}), `{"lat":1,"lon":2}`},
	} {
		if b, err := tt.v.MarshalJSON(); err != nil || string(b) != tt.want {
			t.Errorf("%+v: %s, %v", tt.v, b, err)
		}
	}
}
