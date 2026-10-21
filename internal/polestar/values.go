package polestar

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"
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

// Upload is what a response tells of the car's upload it carries, for the logs that
// measure the Data Portal on real cars: how often a car uploads, and what each model
// gives. It holds neither the VIN nor any value.
type Upload struct {
	At         time.Time // the car's timestamp; zero if missing
	ReceivedAt time.Time // when the Data Portal received it (metaReceivedAt); zero if missing
	ModelYear  int       // read from the VIN; 0 if unknown
	// Fields are the paths of the data's fields, sorted ("coordinate.latitude"), the VIN
	// and the upload's keys left out.
	Fields []string
}

// UploadOf reads the upload a response of a domain carries.
func UploadOf(raw []byte) (Upload, error) {
	var v struct {
		Data struct {
			Timestamp  *stamp `json:"timestamp"`
			ReceivedAt string `json:"metaReceivedAt"`
			VIN        string `json:"vin"`
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
	u := Upload{At: v.Data.Timestamp.instant()}
	if at, err := time.Parse(time.RFC3339Nano, v.Data.ReceivedAt); err == nil {
		u.ReceivedAt = at.UTC()
	}
	if len(v.Data.VIN) == 17 {
		u.ModelYear, _ = modelYear(v.Data.VIN)
	}
	u.Fields = paths("", data.Data, nil)
	slices.Sort(u.Fields)
	return u, nil
}

// paths appends the paths of m's leaves to out: a protobuf Timestamp is a leaf.
func paths(prefix string, m map[string]any, out []string) []string {
	for k, x := range m {
		if k == "vin" || isUploadKey(k) {
			continue
		}
		p := prefix + k
		if sub, ok := x.(map[string]any); ok && !isStamp(sub) {
			out = paths(p+".", sub, out)
			continue
		}
		out = append(out, p)
	}
	return out
}

func isStamp(m map[string]any) bool {
	_, ok := m["seconds"]
	return ok && len(m) <= 2
}
