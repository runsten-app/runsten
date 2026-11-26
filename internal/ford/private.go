package ford

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// PrivateFields are the paths of the fields of the API's responses that the readings' CSV
// file leaves out: Ford's identifiers of the car and of its modem in the owner's account,
// which are not the vehicle's. The VIN and the name the owner gave the car stay; no token
// rides in a response.
func PrivateFields() []string { return []string{"vehicleId", "deviceId"} }

// Fingerprint returns the fingerprint of a response's values, the metrics' send times
// included: Ford returns the last value the car sent of each metric, and a read may bring
// the same sends again, which only extends how long they are known to hold. A new send of
// the same values is a new reading, which tells the car still says so.
func Fingerprint(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	canonical, err := json.Marshal(v) // json.Marshal sorts map keys
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return sum[:], nil
}

// Upload is what a response tells of the car's sends, for the logs that measure the API
// on real cars, whose send cadence nothing publishes. It holds neither the VIN nor any
// value.
type Upload struct {
	At     time.Time // the latest of the metrics' sends; zero if none is told
	Fields []string  // the names of the metrics the car tells, sorted
}

// UploadOf reads the upload a telemetry response carries; a garage response carries
// none.
func UploadOf(ep Endpoint, raw []byte) (Upload, error) {
	if ep != Telemetry {
		return Upload{}, nil
	}
	var r struct {
		At      time.Time                  `json:"updateTime"`
		Metrics map[string]json.RawMessage `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Upload{}, fmt.Errorf("upload: %w", err)
	}
	u := Upload{At: r.At.UTC()}
	type sent struct {
		At time.Time `json:"updateTime"`
	}
	for name, raw := range r.Metrics {
		u.Fields = append(u.Fields, name)
		// A metric of one value, or a list of entries (the doors, the tyres), each with
		// its own send.
		var one sent
		var many []sent
		if json.Unmarshal(raw, &one) != nil && json.Unmarshal(raw, &many) != nil {
			continue
		}
		for _, m := range append(many, one) {
			if m.At.After(u.At) {
				u.At = m.At.UTC()
			}
		}
	}
	slices.Sort(u.Fields)
	return u, nil
}
