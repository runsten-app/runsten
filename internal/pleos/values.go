package pleos

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// stripKey are the keys the fingerprint leaves out: meta (the envelope's own half,
// which changes with every response) and timestamp (the car's send, which moves at
// each one even when the values do not).
func stripKey(k string) bool { return k == "meta" || k == "timestamp" }

// Fingerprint returns the fingerprint of a response's values, the envelope and the
// car's send times excluded, at every depth. Two responses with the same fingerprint
// bring nothing new: the API returns the last state the car sent, and a read may bring
// the same send again, which only extends how long the previous one is known to hold.
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
			if !stripKey(k) {
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

// Upload is what a response tells of the car's upload it carries, for the logs that
// measure the API on real cars, whose upload cadence nothing publishes: how often the
// car sends, and what each model gives. It holds neither the VIN nor any value.
type Upload struct {
	At     time.Time // when the car sent it; zero if not told
	Fields []string  // the paths of the data's fields, sorted ("charge.plugin")
}

// UploadOf reads the upload a response carries. The responses hold no VIN and no model
// year: the model is the recognition's, from the consent's list.
func UploadOf(raw []byte) (Upload, error) {
	var v struct {
		Data struct {
			Timestamp string `json:"timestamp"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Upload{}, fmt.Errorf("upload: %w", err)
	}
	var data struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return Upload{}, fmt.Errorf("upload: %w", err)
	}
	u := Upload{}
	if at, err := time.Parse(time.RFC3339, v.Data.Timestamp); err == nil {
		u.At = at.UTC()
	}
	u.Fields = paths("", data.Data, nil)
	slices.Sort(u.Fields)
	return u, nil
}

// paths appends the paths of m's leaves to out, the stripped keys left out.
func paths(prefix string, m map[string]any, out []string) []string {
	for k, x := range m {
		if stripKey(k) {
			continue
		}
		p := prefix + k
		if sub, ok := x.(map[string]any); ok {
			out = paths(p+".", sub, out)
		} else {
			out = append(out, p)
		}
	}
	return out
}
