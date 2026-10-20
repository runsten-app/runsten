package polestarapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// receiveDelay is the delay between the car's measurement and the cloud's receipt
// (metaReceivedAt): 3 to 14 s on the real car.
const receiveDelay = 5 * time.Second

// tsReceived is metaReceivedAt's format, as on the real API.
const tsReceived = "2006-01-02T15:04:05.000Z"

// timestamp is protobuf's Timestamp as the API writes it: the seconds in a string.
func timestamp(at time.Time) map[string]any {
	return map[string]any{"seconds": strconv.FormatInt(at.Unix(), 10), "nanos": at.Nanosecond()}
}

// eventID mimics metaEventId, a Kinesis shard and sequence number: the same for the
// same upload, another for the next one.
func eventID(vin, domain string, at time.Time) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d", vin, domain, at.UnixNano()))
	return fmt.Sprintf("shardId-000000000000:%056d", binary.BigEndian.Uint64(sum[:8]))
}

// settingID is a stable UUID-shaped identifier of a vehicle's setting.
func settingID(vin, name string) string {
	s := sha256.Sum256([]byte(vin + "|" + name))
	return fmt.Sprintf("%x-%x-%x-%x-%x", s[0:4], s[4:6], s[6:8], s[8:10], s[10:16])
}

// writeError writes the /v1 error body, as observed on the real API.
func writeError(w http.ResponseWriter, now time.Time, status int, code, message string, details any) {
	e := map[string]any{
		"code": code, "message": message, "httpStatus": status,
		"requestId": requestID(), "timestamp": now.UTC().Format(tsReceived),
	}
	if details != nil {
		e["details"] = details
	}
	writeJSON(w, status, map[string]any{"error": e})
}

// writeOAuthError writes the /token error body, as the specification describes it
// (never published from the real API).
func writeOAuthError(w http.ResponseWriter, now time.Time, status int, code, description string) {
	writeJSON(w, status, map[string]string{
		"error": code, "error_description": description,
		"requestId": requestID(), "timestamp": now.UTC().Format(tsReceived),
	})
}

// missingToken answers a path outside the API as its gateway does (observed).
func missingToken(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusForbidden, map[string]string{"message": "Missing Authentication Token"})
}

func requestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}
