package volvo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPrivateFields: each private path is a field of the recorded responses.
func TestPrivateFields(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(fixturesDir, "location.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range PrivateFields() {
		if _, ok := doc[p]; !ok {
			t.Errorf("%s: not in the location's response", p)
		}
	}
}
