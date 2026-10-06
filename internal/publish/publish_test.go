package publish

import (
	"slices"
	"testing"
	"time"

	"runsten/internal/core"
)

const vehicle = "0b7e6a52-3f1d-4c8e-9a51-1f2d3c4b5a69"

func broker(location bool) Broker {
	return Broker{AccountID: "5f0c9c1e-8a2b-4d3c-b1e0-7a6f5e4d3c2b", TopicPrefix: "runsten", PublishLocation: location}
}

// payloads maps the topics under the vehicle to their payloads, and checks that every
// message is retained.
func payloads(t *testing.T, msgs []Message) map[string]string {
	t.Helper()
	m := make(map[string]string, len(msgs))
	for _, msg := range msgs {
		if !msg.Retained {
			t.Errorf("%s is not retained", msg.Topic)
		}
		m[msg.Topic[len("runsten/vehicles/"+vehicle+"/"):]] = msg.Payload
	}
	return m
}

func TestState(t *testing.T) {
	t0 := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	full := core.Snapshot{
		Covers: core.FieldEngine | core.FieldSoC | core.FieldRange | core.FieldCharging | core.FieldConnection |
			core.FieldChargeType | core.FieldPower | core.FieldTargetSoC | core.FieldOdometer | core.FieldPosition,
		Engine:     core.Some(core.EngineStopped, t0),
		SoC:        core.Some(80.0, t0),
		RangeKm:    core.Some(312.5, t0),
		Charging:   core.Some(core.ChargingActive, t0),
		Connection: core.Some(core.Connected, t0),
		ChargeType: core.Some(core.AC, t0),
		PowerW:     core.Some(11040.0, t0),
		TargetSoC:  core.Some(90.0, t0),
		OdometerKm: core.Some(12345.0, t0),
		Position:   core.Some(core.Position{Lat: 59.3293, Lon: 18.0686}, t0),
	}
	tests := []struct {
		name    string
		records []core.Record
		b       Broker
		want    map[string]string
	}{
		{
			name:    "every value known",
			records: []core.Record{{FetchedAt: t0, CheckedAt: t0.Add(5 * time.Minute), Snapshot: full}},
			b:       broker(true),
			want: map[string]string{
				"battery_level": "80", "range_km": "312.5", "charging_status": "charging", "plug": "connected",
				"charge_type": "AC", "charging_power_kw": "11.04", "target_soc": "90", "odometer_km": "12345",
				"engine": "stopped", "location": `{"latitude":59.3293,"longitude":18.0686}`,
				"read_at": "2026-10-04T08:05:00Z",
			},
		},
		{
			name:    "location not published: emptied",
			records: []core.Record{{FetchedAt: t0, CheckedAt: t0, Snapshot: full}},
			b:       broker(false),
			want: map[string]string{
				"battery_level": "80", "range_km": "312.5", "charging_status": "charging", "plug": "connected",
				"charge_type": "AC", "charging_power_kw": "11.04", "target_soc": "90", "odometer_km": "12345",
				"engine": "stopped", "location": "", "read_at": "2026-10-04T08:00:00Z",
			},
		},
		{
			name: "unknown values are empty, never zero",
			records: []core.Record{{FetchedAt: t0, CheckedAt: t0, Snapshot: core.Snapshot{
				Covers: core.FieldSoC | core.FieldPower | core.FieldPosition, SoC: core.Some(0.0, t0),
			}}},
			b: broker(true),
			want: map[string]string{
				"battery_level": "0", "range_km": "", "charging_status": "", "plug": "", "charge_type": "",
				"charging_power_kw": "", "target_soc": "", "odometer_km": "", "engine": "", "location": "",
				"read_at": "2026-10-04T08:00:00Z",
			},
		},
		{
			name: "nothing read",
			b:    broker(true),
			want: map[string]string{
				"battery_level": "", "range_km": "", "charging_status": "", "plug": "", "charge_type": "",
				"charging_power_kw": "", "target_soc": "", "odometer_km": "", "engine": "", "location": "", "read_at": "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := State(tt.b, vehicle, core.Latest(tt.records))
			got := payloads(t, msgs)
			if len(got) != len(msgs) || len(got) != len(tt.want) {
				t.Fatalf("%d messages, %d topics, want %d: %v", len(msgs), len(got), len(tt.want), got)
			}
			for topic, want := range tt.want {
				if p, ok := got[topic]; !ok || p != want {
					t.Errorf("%s = %q (%v), want %q", topic, p, ok, want)
				}
			}
		})
	}
}

// TestStateOrder: the topics come in the contract's order, under the prefix.
func TestStateOrder(t *testing.T) {
	b := broker(true)
	b.TopicPrefix = "home/cars"
	var topics []string
	for _, m := range State(b, vehicle, core.Latest(nil)) {
		topics = append(topics, m.Topic)
	}
	base := "home/cars/vehicles/" + vehicle + "/"
	want := []string{
		base + "battery_level", base + "range_km", base + "charging_status", base + "plug", base + "charge_type",
		base + "charging_power_kw", base + "target_soc", base + "odometer_km", base + "engine", base + "location",
		base + "read_at",
	}
	if !slices.Equal(topics, want) {
		t.Errorf("topics = %v", topics)
	}
}

func TestAvailability(t *testing.T) {
	for _, tt := range []struct {
		online bool
		want   Message
	}{
		{true, Message{Topic: "runsten/status", Payload: "online", Retained: true}},
		{false, Message{Topic: "runsten/status", Payload: "offline", Retained: true}},
	} {
		if got := Availability("runsten", tt.online); got != tt.want {
			t.Errorf("Availability(%v) = %+v", tt.online, got)
		}
	}
}

func TestClientIdentifier(t *testing.T) {
	for _, tt := range []struct {
		b    Broker
		want string
	}{
		{Broker{AccountID: "5f0c9c1e-8a2b-4d3c-b1e0-7a6f5e4d3c2b"}, "runsten-5f0c9c1e8a2b4d3"},
		{Broker{AccountID: "5f0c9c1e-8a2b-4d3c-b1e0-7a6f5e4d3c2b", ClientID: "car-1"}, "car-1"},
	} {
		if got := tt.b.ClientIdentifier(); got != tt.want || len(got) > 23 {
			t.Errorf("ClientIdentifier(%+v) = %q, want %q", tt.b, got, tt.want)
		}
	}
}
