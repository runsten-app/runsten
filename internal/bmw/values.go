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
// every depth. Two responses with the same fingerprint bring nothing new. Whether the
// latitude and the longitude have the same timestamp is in it: the normalizer reads a
// position only then, and the longitude of a latitude pushed apart may come with the
// same value.
func Fingerprint(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	stripped := strip(v)
	if m, ok := stripped.(map[string]any); ok && paired(raw) {
		m["positionPaired"] = true // no descriptor's name: none is a top-level key
	}
	canonical, err := json.Marshal(stripped) // json.Marshal sorts map keys
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return sum[:], nil
}

// paired reports whether a response of values holds a latitude and a longitude of the
// same timestamp.
func paired(raw []byte) bool {
	v, err := parseValues(Stream, raw)
	if err != nil {
		return false
	}
	_, latAt, ok1 := v.get(DescLatitude)
	_, lonAt, ok2 := v.get(DescLongitude)
	return ok1 && ok2 && latAt.Equal(lonAt)
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
	case ChargingHistory, Image:
	}
	slices.Sort(u.Fields)
	return u, nil
}
