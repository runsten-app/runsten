package polestar

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// isUploadKey tells the keys excluded from the fingerprint: they change with each upload
// of the car, or each receipt of the cloud, even when the values do not.
func isUploadKey(k string) bool {
	return k == "timestamp" || k == "metaReceivedAt" || k == "metaEventId"
}

// Fingerprint returns the fingerprint of a response's values, the upload's time and
// identifiers excluded, at every depth. Two responses with the same fingerprint bring
// nothing new.
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
			if !isUploadKey(k) {
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
