package publish

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "write the golden files")

// golden holds the discovery configurations of a vehicle.
const golden = "testdata/discovery.json"

func TestLabels(t *testing.T) {
	const a, b, c = "0b7e6a52-aaaa", "1c8f7b63-bbbb", "2d9a8c74-cccc"
	ex30 := Model{Variant: "EX30 Single Motor Extended Range", Family: "EX30", Brand: "Volvo", Year: 2025}
	for _, tt := range []struct {
		name   string
		models map[string]Model
		want   map[string]string
	}{
		{"variant and year", map[string]Model{a: ex30}, map[string]string{a: "EX30 Single Motor Extended Range · 2025"}},
		{"family alone", map[string]Model{a: {Family: "XC40"}}, map[string]string{a: "XC40"}},
		{"nothing read yet", map[string]Model{a: {}}, map[string]string{a: "Volvo 0b7e6a"}},
		{
			"two alike, one apart",
			map[string]Model{a: ex30, b: ex30, c: {Family: "XC40", Year: 2023}},
			map[string]string{
				a: "EX30 Single Motor Extended Range · 2025 · 0b7e6a",
				b: "EX30 Single Motor Extended Range · 2025 · 1c8f7b",
				c: "XC40 · 2023",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Labels(tt.models)
			if len(got) != len(tt.want) {
				t.Fatalf("labels = %v", got)
			}
			for id, want := range tt.want {
				if got[id] != want {
					t.Errorf("%s = %q, want %q", id, got[id], want)
				}
			}
		})
	}
}

// TestDiscovery: the configurations of a vehicle's entities are a contract, as the
// topics: testdata/discovery.json (go test ./internal/publish -update writes it).
func TestDiscovery(t *testing.T) {
	b := Broker{TopicPrefix: "runsten", DiscoveryPrefix: "homeassistant", PublishLocation: true}
	m := Model{Variant: "EX30 Single Motor Extended Range", Family: "EX30", Brand: "Volvo", Year: 2025}
	msgs := Discovery(b, vehicle, "EX30 Single Motor Extended Range · 2025", m)
	var out bytes.Buffer
	out.WriteString("{\n")
	for i, msg := range msgs {
		if !msg.Retained {
			t.Errorf("%s is not retained", msg.Topic)
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(msg.Payload), &cfg); err != nil {
			t.Fatalf("%s: %v", msg.Topic, err)
		}
		if cfg["availability_topic"] != "runsten/status" {
			t.Errorf("%s: availability %v", msg.Topic, cfg["availability_topic"])
		}
		topic, _ := json.Marshal(msg.Topic)
		var indented bytes.Buffer
		if err := json.Indent(&indented, []byte(msg.Payload), "  ", "  "); err != nil {
			t.Fatal(err)
		}
		out.WriteString("  " + string(topic) + ": " + indented.String())
		if i < len(msgs)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	if *update {
		if err := os.WriteFile(golden, out.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Errorf("the configurations differ from %s (go test -update to write them):\n%s", golden, out.String())
	}

	// Without the location, the device tracker's configuration is emptied; and every
	// configuration is emptied to remove the vehicle.
	b.PublishLocation = false
	removed := RemoveDiscovery(b, vehicle)
	for i, msg := range Discovery(b, vehicle, "x", m) {
		tracker := strings.Contains(msg.Topic, "/device_tracker/")
		if tracker != (msg.Payload == "") {
			t.Errorf("%s: payload %q without the location", msg.Topic, msg.Payload)
		}
		if removed[i].Topic != msg.Topic || removed[i].Payload != "" || !removed[i].Retained {
			t.Errorf("removal %+v, want %s emptied", removed[i], msg.Topic)
		}
	}
	if len(removed) != len(msgs) {
		t.Errorf("%d removals for %d entities", len(removed), len(msgs))
	}
}

func TestRemoveState(t *testing.T) {
	msgs := RemoveState(broker(true), vehicle)
	if len(msgs) != 11 {
		t.Fatalf("%d messages", len(msgs))
	}
	for _, m := range msgs {
		if m.Payload != "" || !m.Retained || !strings.HasPrefix(m.Topic, "runsten/vehicles/"+vehicle+"/") {
			t.Errorf("%+v", m)
		}
	}
}
