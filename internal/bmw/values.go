package bmw

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// isNoise tells the keys excluded from the fingerprint: the time each value was recorded,
// which a car pushing the same value again moves.
func isNoise(k string) bool { return k == "timestamp" }

// Fingerprint returns the fingerprint of a response's values, the timestamps excluded, at
// every depth. Two responses with the same fingerprint bring nothing new.
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

// Upload is what a response tells of the car's upload it carries, for the logs: how
// fresh its values are, and which descriptors each model sends.
type Upload struct {
	At     time.Time // the latest of its values' timestamps; zero if none
	Fields []string  // the descriptors it holds, sorted; the basic data's fields
}

// UploadOf reads the upload a response of e carries.
func UploadOf(e Endpoint, raw []byte) (Upload, error) {
	var u Upload
	switch e {
	case Stream, TelematicData:
		v, err := parseValues(e, raw)
		if err != nil {
			return u, err
		}
		for k, d := range v {
			if at := stampOf(d.Timestamp); at.After(u.At) {
				u.At = at
			}
			u.Fields = append(u.Fields, k)
		}
	case BasicData:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return u, fmt.Errorf("upload: %w", err)
		}
		for k := range m {
			if k != "vin" {
				u.Fields = append(u.Fields, k)
			}
		}
	case ChargingHistory:
	}
	slices.Sort(u.Fields)
	return u, nil
}
