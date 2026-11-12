package skoda

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// PrivateFields are the paths of the fields of the API's responses that the readings' CSV
// file leaves out: the licence plate, the address the API formats from the position (the
// coordinates stay, as the other sources' positions), and the link to the car's picture.
// The key never rides in a response.
func PrivateFields() []string {
	return []string{"vehicle.licensePlate", "vehicle.parkingPosition.formattedAddress", "vehicle.renderUrl"}
}

// stripKey are the keys the fingerprint leaves out: the picture's link, which may change
// at every response (assumption: a signed link) without the car sending anything. The
// parts' send times stay: a new send of the same values is a new reading, which tells the
// car still says so.
func stripKey(k string) bool { return k == "renderUrl" }

// Fingerprint returns the fingerprint of a response's values, the picture's link excluded,
// at every depth. Two responses with the same fingerprint bring nothing new: the API
// returns the last state each part was sent in, and a read may bring the same sends again,
// which only extends how long they are known to hold.
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

// Upload is what a response tells of the car's sends, for the logs that measure the API
// on real cars, whose send cadence nothing publishes. It holds neither the VIN nor any
// value.
type Upload struct {
	At        time.Time // the latest of the parts' sends; zero if none is told
	ModelYear int       // from the VIN; 0 if unknown
	Fields    []string  // the paths of the vehicle's fields, sorted ("charging.status.state")
}

// UploadOf reads the upload a response carries.
func UploadOf(raw []byte) (Upload, error) {
	var r struct {
		Vehicle map[string]any `json:"vehicle"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Upload{}, fmt.Errorf("upload: %w", err)
	}
	u := Upload{}
	for _, part := range r.Vehicle {
		m, _ := part.(map[string]any)
		s, _ := m["carCapturedTimestamp"].(string)
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil && t.After(u.At) {
			u.At = t.UTC()
		}
	}
	if vin, _ := r.Vehicle["vin"].(string); vin != "" {
		u.ModelYear, _ = modelYear(vin)
	}
	u.Fields = paths("", r.Vehicle, nil)
	slices.Sort(u.Fields)
	return u, nil
}

// paths appends the paths of m's leaves to out, the private and stripped ones left out.
func paths(prefix string, m map[string]any, out []string) []string {
	for k, x := range m {
		if stripKey(k) || k == "carCapturedTimestamp" {
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
