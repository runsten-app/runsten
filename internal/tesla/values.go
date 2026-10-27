package tesla

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"
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

// Upload is what a response tells of the car's reading it carries, for the logs: how
// fresh the values of an awake car are, and what each model gives.
type Upload struct {
	At        time.Time // the latest of its blocks' timestamps; zero if none
	ModelYear int       // read from the VIN; 0 if unknown
	// Fields are the paths of the response's fields, sorted ("charge_state.battery_level"),
	// the VIN and the timestamps left out.
	Fields []string
}

// UploadOf reads the upload a response of /vehicles/{id} or vehicle_data carries.
func UploadOf(raw []byte) (Upload, error) {
	var v struct {
		Response map[string]any `json:"response"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Upload{}, fmt.Errorf("upload: %w", err)
	}
	var u Upload
	for _, x := range v.Response {
		block, ok := x.(map[string]any)
		if !ok {
			continue
		}
		if ms, ok := block["timestamp"].(float64); ok {
			if at := time.UnixMilli(int64(ms)).UTC(); at.After(u.At) {
				u.At = at
			}
		}
	}
	if vin, ok := v.Response["vin"].(string); ok {
		u.ModelYear, _ = modelYear(vin)
	}
	u.Fields = paths("", v.Response, nil)
	slices.Sort(u.Fields)
	return u, nil
}

// paths appends the paths of m's leaves to out.
func paths(prefix string, m map[string]any, out []string) []string {
	for k, x := range m {
		if k == "vin" || isNoise(k) {
			continue
		}
		if sub, ok := x.(map[string]any); ok {
			out = paths(prefix+k+".", sub, out)
			continue
		}
		out = append(out, prefix+k)
	}
	return out
}
