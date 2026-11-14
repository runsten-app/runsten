package renault

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// PrivateFields are the paths of the fields of the API's responses that the readings'
// CSV file leaves out: the details' licence plate and radio code, and the links to the
// car's pictures. The coordinates stay, as the other sources' positions. No token rides
// in a response.
func PrivateFields() []string {
	return []string{"registrationNumber", "radioCode", "assets"}
}

// stripKey are the keys the fingerprint leaves out: the pictures' links, which tell
// nothing of the car's state. The send times stay: a new send of the same values is a
// new reading, which tells the car still says so.
func stripKey(k string) bool { return k == "assets" }

// Fingerprint returns the fingerprint of a response's values, the pictures' links
// excluded, at every depth. Two responses with the same fingerprint bring nothing new:
// Kamereon returns the last state the car sent, and a read may bring the same send
// again.
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
// measure the API on real cars: how often the car sends, and what each model gives. It
// holds neither the VIN nor any value.
type Upload struct {
	At     time.Time // when the car sent it; zero if not told
	Fields []string  // the attributes' names, sorted
}

// UploadOf reads the upload a car-adapter response carries; the details carry none.
func UploadOf(raw []byte) (Upload, error) {
	var v struct {
		Data struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Upload{}, fmt.Errorf("upload: %w", err)
	}
	u := Upload{At: timeOf(v.Data.Attributes, "timestamp", "lastUpdateTime")}
	for k := range v.Data.Attributes {
		u.Fields = append(u.Fields, k)
	}
	slices.Sort(u.Fields)
	return u, nil
}

// expOf reads when a JWT ends, in its claims, without verifying the signature (it is
// read for the lifetime alone). Zero when there is none.
func expOf(tok string) time.Time {
	_, rest, _ := strings.Cut(tok, ".")
	claims, _, _ := strings.Cut(rest, ".")
	raw, err := base64.RawURLEncoding.DecodeString(claims)
	if err != nil {
		return time.Time{}
	}
	var e struct {
		Exp json.Number `json:"exp"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return time.Time{}
	}
	sec, err := e.Exp.Float64()
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(sec), 0).UTC()
}
