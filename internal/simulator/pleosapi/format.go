package pleosapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ts is the API's time format: UTC, to the second (the doc's examples).
const ts = "2006-01-02T15:04:05Z07:00"

// meta is the success envelope's second half, as the doc gives it. Its examples write
// success as the string "true"; a real response has been seen with a boolean, which is
// what a client answers on too.
func meta(now time.Time) map[string]any {
	return map[string]any{
		"code": 200, "message": "Success", "success": true,
		"timestamp": now.UTC().Format(ts), "requestId": requestID(),
	}
}

// writeError answers with the API's error envelope. Its doc examples write code as a
// string; a real response has been seen with a number, and the client reads both. The
// details array is the doc's: it carries a field's issue, which its invalid-parameter
// answer would.
func writeError(w http.ResponseWriter, now time.Time, status int, code any, message string) {
	e := map[string]any{
		"code": code, "message": message, "details": []map[string]string{},
		"timestamp": now.UTC().Format(ts), "requestId": requestID(),
	}
	writeJSON(w, status, map[string]any{"error": e})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}

// decodeJSON reads a request body into v, answering the API's error when it cannot. It
// reports whether it answered.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any, now time.Time) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		writeError(w, now, http.StatusBadRequest, "40001", "Invalid Parameters")
		return true
	}
	return false
}

func requestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// jwt writes the token as the API's: a JWT whose claims carry its end, which the client
// reads without verifying the signature. The signature is empty (an "alg": "none"
// header): the simulator issues no secret. A token of the same brand and second differs
// by its id, as the real API's do.
func jwt(brand string, until time.Time) string {
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"jti": requestID(), "exp": until.Unix(), "iat": until.Add(-time.Hour).Unix(), "brand": brand,
	})
	enc := base64.RawURLEncoding
	return enc.EncodeToString(header) + "." + enc.EncodeToString(claims) + "."
}

// timestamp writes a time as the API's data timestamp: UTC, to the second.
func timestamp(at time.Time) string { return at.UTC().Format(ts) }

// retryAfter writes the seconds the Retry-After header gives.
func retryAfter(at, now time.Time) string {
	sec := int(at.Sub(now).Round(time.Second).Seconds())
	if sec < 0 {
		sec = 0
	}
	return strconv.Itoa(sec)
}
