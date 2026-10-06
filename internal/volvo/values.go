package volvo

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// timeKeys are the keys excluded from the fingerprint: timestamps and operation IDs
// change on every call even when the values do not.
func isTimeKey(k string) bool {
	return k == "timestamp" || k == "updatedAt" || k == "operationId"
}

// Fingerprint returns the fingerprint of a response's values, timestamps excluded. Two
// responses with the same fingerprint bring nothing new.
func Fingerprint(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	canonical, err := json.Marshal(stripTimes(v)) // json.Marshal sorts map keys
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return sum[:], nil
}

func stripTimes(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			if !isTimeKey(k) {
				out[k] = stripTimes(x)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = stripTimes(x)
		}
		return out
	default:
		return v
	}
}

// ParseEngineStatus reads the engine-status value (RUNNING, STOPPED…).
func ParseEngineStatus(raw []byte) (string, error) {
	var v struct {
		Data struct {
			EngineStatus struct {
				Value string `json:"value"`
			} `json:"engineStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("engine-status: %w", err)
	}
	return v.Data.EngineStatus.Value, nil
}

// ParseChargingStatus reads chargingStatus from energy-state (CHARGING, IDLE, DONE…).
// Returns "" if the value is missing or in error: an absence is not a state.
func ParseChargingStatus(raw []byte) (string, error) {
	var v struct {
		ChargingStatus struct {
			Status string `json:"status"`
			Value  string `json:"value"`
		} `json:"chargingStatus"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("energy-state: %w", err)
	}
	if v.ChargingStatus.Status != "OK" {
		return "", nil
	}
	return v.ChargingStatus.Value, nil
}
