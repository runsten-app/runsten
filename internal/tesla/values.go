package tesla

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// isNoise tells the keys excluded from the fingerprint: each block's time of reading,
// which changes with each read even when the vehicle's values do not.
func isNoise(k string) bool { return k == "timestamp" }

// Fingerprint returns the fingerprint of a response's values, the blocks' timestamps
// excluded, at every depth. Two responses with the same fingerprint bring nothing new.
func Fingerprint(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	canonical, err := json.Marshal(strip(v)) // json.Marshal sorts map keys
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return sum[:], nil
}

func strip(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			if !isNoise(k) {
				out[k] = strip(x)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = strip(x)
		}
		return out
	default:
		return v
	}
}
