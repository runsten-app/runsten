package volvoapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"time"
)

// Timestamp formats observed on the real API (Volvo demo car).
const (
	tsNano   = "2006-01-02T15:04:05.000000000Z" // Connected Vehicle, Location
	tsMillis = "2006-01-02T15:04:05.000Z"       // Connected Vehicle, /statistics
	tsSecond = "2006-01-02T15:04:05Z"           // Energy
)

// cvValue is a Connected Vehicle value: {timestamp, unit, value}. unit is null
// for states.
type cvValue struct {
	Timestamp string  `json:"timestamp"`
	Unit      *string `json:"unit"`
	Value     any     `json:"value"`
}

func cv(at time.Time, layout string, unit string, value any) cvValue {
	v := cvValue{Timestamp: at.UTC().Format(layout), Value: value}
	if unit != "" {
		v.Unit = &unit
	}
	return v
}

// energyValue is an Energy v2 value: {status, value, unit, updatedAt} or, on error,
// {status, code, message}. States have no unit key.
type energyValue struct {
	Status    string `json:"status"`
	Value     any    `json:"value,omitempty"`
	Unit      string `json:"unit,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

func energyOK(at time.Time, unit string, value any) energyValue {
	return energyValue{Status: "OK", Value: value, Unit: unit, UpdatedAt: at.UTC().Format(tsSecond)}
}

func energyErr(code, message string) energyValue {
	return energyValue{Status: "ERROR", Code: code, Message: message}
}

// The four error formats observed on the Volvo demo car. The formats of the 401 and
// 429 statuses have not been observed: they are assumed.

type cvError struct {
	Error struct {
		Description string `json:"description"`
		Message     string `json:"message"`
	} `json:"error"`
}

type energyError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details"`
}

type statusError struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
}

func writeCVError(w http.ResponseWriter, status int, message, description string) {
	var e cvError
	e.Error.Message, e.Error.Description = message, description
	writeJSON(w, status, e)
}

func writeEnergyError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, energyError{Code: code, Message: message, Details: []string{}})
}

func writeStatusError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, statusError{StatusCode: status, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}

// operationID mimics the operation ID returned by the Location API.
func operationID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	return hex.EncodeToString(b)
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }
