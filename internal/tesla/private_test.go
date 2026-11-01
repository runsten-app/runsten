package tesla

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestPrivateFields: each private path is one of the recorded responses (a path
// misspelled leaves its field in the file), and every field of theirs that names a
// token or an identifier other than the VIN is private.
func TestPrivateFields(t *testing.T) {
	private := PrivateFields()
	seen := map[string]bool{}
	for _, name := range []string{"vehicle.json", "vehicle_data.json"} {
		var doc struct {
			Response map[string]json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal(fixture(t, name), &doc); err != nil {
			t.Fatal(err)
		}
		for k := range doc.Response {
			path := "response." + k
			seen[path] = true
			if k == "vin" {
				continue
			}
			if (strings.Contains(k, "token") || k == "id" || strings.HasPrefix(k, "id_") || strings.HasSuffix(k, "_id")) &&
				!slices.Contains(private, path) {
				t.Errorf("%s: %s is not private", name, path)
			}
		}
	}
	for _, p := range private {
		if !seen[p] {
			t.Errorf("%s: in no recorded response", p)
		}
	}
	for _, p := range []string{"response.tokens", "response.backseat_token", "response.user_id", "response.vehicle_id"} {
		if !slices.Contains(private, p) {
			t.Errorf("%s is not private", p)
		}
	}
}
