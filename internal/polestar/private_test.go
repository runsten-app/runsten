package polestar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrivateFields: no recorded response of the Data Portal holds a field that names a
// token, a secret or the owner's account, which PrivateFields would have to list.
func TestPrivateFields(t *testing.T) {
	if got := PrivateFields(); len(got) != 0 {
		t.Errorf("private: %v", got)
	}
	names, err := filepath.Glob(filepath.Join(fixturesDir, "*", "*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no fixture: %v", err)
	}
	for _, name := range names {
		b, err := os.ReadFile(name) //nolint:gosec // repository file
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		forKeys(doc, func(k string) {
			if k := strings.ToLower(k); strings.Contains(k, "token") || strings.Contains(k, "secret") ||
				strings.Contains(k, "account") || strings.Contains(k, "user") {
				t.Errorf("%s: %s", filepath.Base(name), k)
			}
		})
	}
}

func forKeys(v any, fn func(string)) {
	switch v := v.(type) {
	case map[string]any:
		for k, e := range v {
			fn(k)
			forKeys(e, fn)
		}
	case []any:
		for _, e := range v {
			forKeys(e, fn)
		}
	}
}
