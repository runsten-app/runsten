package flags

import (
	"maps"
	"regexp"
	"slices"
	"testing"
)

// TestDeclarations: every flag has a valid name, once, and says what it is for and when
// it goes.
func TestDeclarations(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)
	seen := map[Name]bool{}
	for _, f := range All() {
		if !valid.MatchString(string(f.Name)) {
			t.Errorf("%q: not dot-separated kebab-case words, the area first", f.Name)
		}
		if seen[f.Name] {
			t.Errorf("%s declared twice", f.Name)
		}
		seen[f.Name] = true
		if f.Purpose == "" || f.Remove == "" {
			t.Errorf("%s: say what it is for and when it goes", f.Name)
		}
	}
	if f, ok := Of(SourceTesla); !ok || !f.Default || !f.Web {
		t.Errorf("Of(SourceTesla) = %+v, %v", f, ok)
	}
	if _, ok := Of("source.unknown"); ok {
		t.Error("Of an undeclared name")
	}
}

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    map[Name]bool
		unknown []string
		err     bool
	}{
		{in: "", want: map[Name]bool{}},
		{in: " , ", want: map[Name]bool{}},
		{in: "source.tesla=false", want: map[Name]bool{SourceTesla: false}},
		{in: " source.tesla = TRUE ,", want: map[Name]bool{SourceTesla: true}},
		{in: "source.tesla=false,gone.flag=true", want: map[Name]bool{SourceTesla: false}, unknown: []string{"gone.flag"}},
		{in: "source.tesla", err: true},
		{in: "source.tesla=off", err: true},
		{in: "source.tesla=0", err: true},
		{in: "source.tesla=true,source.tesla=false", err: true},
	} {
		got, unknown, err := Parse(tt.in)
		if (err != nil) != tt.err {
			t.Errorf("Parse(%q) error = %v", tt.in, err)
			continue
		}
		if !tt.err && (!maps.Equal(got, tt.want) || !slices.Equal(unknown, tt.unknown)) {
			t.Errorf("Parse(%q) = %v, %v; want %v, %v", tt.in, got, unknown, tt.want, tt.unknown)
		}
	}
}
