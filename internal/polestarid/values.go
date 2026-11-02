package polestarid

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Fingerprint returns the fingerprint of a response's values, the car's timestamps
// excluded at every depth. Two responses with the same fingerprint bring nothing new.
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
			if k != "timestamp" {
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

// Upload is what a response tells of the car's uploads it carries, for the logs that
// measure the API on real cars: how often a car uploads, and what each model gives. It
// holds neither the VIN nor any value.
type Upload struct {
	At        time.Time // the latest of its blocks' timestamps; zero if none
	ModelYear int       // read from the VIN; 0 if unknown
	// Fields are the paths of its fields, sorted ("battery.chargingStatusV2"), the VIN and
	// the timestamps left out.
	Fields []string
}

// UploadOf reads the uploads a response of ep carries.
func UploadOf(ep Endpoint, raw []byte) (Upload, error) {
	switch ep {
	case Battery, TargetSoC:
		return grpcUploadOf(raw)
	case Telematics:
		return telematicsUploadOf(raw)
	}
	return Upload{}, nil
}

// grpcUploadOf reads a gRPC response stored (wire.go): its battery's timestamp, the
// fields of each message of it.
func grpcUploadOf(raw []byte) (Upload, error) {
	var v map[string]json.RawMessage
	if err := json.Unmarshal(raw, &v); err != nil {
		return Upload{}, fmt.Errorf("upload: %w", err)
	}
	var u Upload
	if vin, ok := v["vin"]; ok {
		var s string
		if json.Unmarshal(vin, &s) == nil {
			if y, ok := modelYear(s); ok {
				u.ModelYear = y
			}
		}
	}
	for kind, m := range v {
		var fields map[string]json.RawMessage
		if json.Unmarshal(m, &fields) != nil {
			continue
		}
		for k, x := range fields {
			if k != "timestamp" {
				u.Fields = append(u.Fields, kind+"."+k)
				continue
			}
			var t stamp
			if json.Unmarshal(x, &t) == nil {
				u.At = t.instant()
			}
		}
	}
	slices.Sort(u.Fields)
	return u, nil
}

func telematicsUploadOf(raw []byte) (Upload, error) {
	var v telematics
	if err := json.Unmarshal(raw, &v); err != nil {
		return Upload{}, fmt.Errorf("upload: %w", err)
	}
	var u Upload
	t := v.Data.T
	stamps := []*stamp{}
	for _, b := range t.Battery {
		stamps = append(stamps, b.Timestamp)
		if y, ok := modelYear(b.VIN); ok {
			u.ModelYear = y
		}
	}
	for _, o := range t.Odometer {
		stamps = append(stamps, o.Timestamp)
	}
	for _, h := range t.Health {
		stamps = append(stamps, h.Timestamp)
	}
	for _, s := range stamps {
		if at := s.instant(); at.After(u.At) {
			u.At = at
		}
	}
	var generic struct {
		Data map[string]map[string][]map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &generic) == nil {
		for _, kinds := range generic.Data {
			for kind, entries := range kinds {
				for _, e := range entries {
					for k := range e {
						if k != "vin" && k != "timestamp" {
							u.Fields = append(u.Fields, kind+"."+k)
						}
					}
				}
			}
		}
	}
	slices.Sort(u.Fields)
	u.Fields = slices.Compact(u.Fields)
	return u, nil
}
