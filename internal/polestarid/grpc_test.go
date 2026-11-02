package polestarid

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"runsten/internal/core"
)

// h2c starts a test server speaking HTTP/2 without TLS, as the simulator does.
func h2c(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Config.Protocols = new(http.Protocols)
	srv.Config.Protocols.SetHTTP1(true)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name) //nolint:gosec // a fixture of the repository
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The battery a real car gave, plugged in and idle (pypolestar's capture).
func TestTheRealBatteryNormalizes(t *testing.T) {
	raw, err := render("battery", fixture(t, "grpc_battery_response.bin"), batteryResponse, "battery")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if _, ok := v["id"]; ok || v["vin"] != "YSMYKEAE7RB000000" {
		t.Errorf("envelope %v: the request's ID kept, every response would be new", v)
	}
	b, _ := v["battery"].(map[string]any)
	// Field 28 is not in the reconstruction, the power status's values neither: kept.
	if b["28"] != float64(5) || b["chargerPowerStatus"] != float64(5) || b["estimatedDistanceToEmptyMiles"] != float64(68) {
		t.Errorf("battery %v", b)
	}
	s, err := Normalize(Battery, raw)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if s.SoC.V != 26 || !s.SoC.At.Equal(at) || s.RangeKm.V != 111 || s.Charging.V != core.ChargingIdle {
		t.Errorf("energy %+v %+v %+v", s.SoC, s.RangeKm, s.Charging)
	}
	if s.Connection.V != core.Connected || s.ChargeType.V != core.NoCharge || !s.PowerW.OK || s.PowerW.V != 0 {
		t.Errorf("charge %+v %+v %+v", s.Connection, s.ChargeType, s.PowerW)
	}
	// The capture's consumption is the automatic trip meter's (23.8, a float32 the car
	// wrote in a double), not the one read.
	auto, _ := b["averageEnergyConsumptionKwhPer100KmAutomatic"].(float64)
	if s.TimeLeft.OK || s.CurrentA.OK || s.ConsumptionKWhPer100km.OK || math.Abs(auto-23.8) > 1e-6 {
		t.Errorf("idle: time left %+v, current %+v, consumption %+v", s.TimeLeft, s.CurrentA, s.ConsumptionKWhPer100km)
	}
	u, err := UploadOf(Battery, raw)
	fields := strings.Join(u.Fields, " ")
	// No power: protobuf leaves a zero out.
	if err != nil || !u.At.Equal(at) || u.ModelYear != 2024 || !strings.Contains(fields, "battery.28") || strings.Contains(fields, "chargingPowerWatts") {
		t.Errorf("upload %+v %v", u, err)
	}
}

func TestTheRealTargetSoCNormalizes(t *testing.T) {
	raw, err := render("target-soc", fixture(t, "grpc_target_soc_response.bin"), targetSoCResponse, "targetSoc")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		TargetSoC map[string]any `json:"targetSoc"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.TargetSoC["chargeTargetLevelSettingType"] != "CUSTOM" || v.TargetSoC["updatedAt"] != "1704067200" || v.TargetSoC["source"] != "test" {
		t.Errorf("target %v", v.TargetSoC)
	}
	s, err := Normalize(TargetSoC, raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.Covers != core.FieldTargetSoC || s.TargetSoC.V != 80 || !s.TargetSoC.At.IsZero() {
		t.Errorf("snapshot %+v", s)
	}
}

// A battery charging on DC, as pypolestar's synthetic case: what the capture did not have.
func TestABatteryCharging(t *testing.T) {
	b := appendField(nil, 6, 1) // CONNECTED
	b = appendField(b, 7, 1)    // CHARGING
	b = appendField(b, 17, 3)   // DC
	b = appendField(b, 10, 150_000)
	b = appendField(b, 11, 375)
	b = appendField(b, 18, 400)
	b = appendField(b, 5, 25)
	msg := protowire.AppendBytes(protowire.AppendTag(nil, 3, protowire.BytesType), b)
	raw, err := render("battery", msg, batteryResponse, "battery")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Normalize(Battery, raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.Charging.V != core.ChargingActive || s.ChargeType.V != core.DC || s.PowerW.V != 150_000 || s.CurrentA.V != 375 || s.VoltageV.V != 400 {
		t.Errorf("charge %+v", s)
	}
	if s.TimeLeft.V != (core.TimeLeft{Duration: 25 * time.Minute, To: core.ToFull}) {
		t.Errorf("time left %+v", s.TimeLeft)
	}
	// Neither the SoC nor the range told: left to the telematics.
	if s.Covers&(core.FieldSoC|core.FieldRange) != 0 || s.Covers&core.FieldCharging == 0 {
		t.Errorf("covers %b", s.Covers)
	}
}

// What protobuf leaves out (UNSPECIFIED, a zero) is not covered where the telematics tell
// it too.
func TestAnEmptyBatteryLeavesTheTelematicsValues(t *testing.T) {
	s, err := Normalize(Battery, []byte(`{"battery":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Covers&(core.FieldSoC|core.FieldRange|core.FieldCharging) != 0 || s.Covers&core.FieldConnection == 0 || s.PowerW.OK {
		t.Errorf("snapshot %+v", s)
	}
	if s, err := Normalize(Battery, []byte(`{"vin":"x"}`)); err != nil || s.Covers != 0 {
		t.Errorf("no battery: %+v %v", s, err)
	}
}

func appendField(b []byte, num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, num, protowire.VarintType), v)
}

func TestDecodeRefusesBrokenMessages(t *testing.T) {
	for name, b := range map[string][]byte{
		"truncated tag":    {0x80},
		"truncated bytes":  {0x1a, 0x05, 0x01},
		"a string not UTF": {0x12, 0x01, 0xff},
		"truncated varint": {0x08, 0x80},
		"truncated fixed":  {0x11, 0x01},
	} {
		if _, err := decode(b, batteryResponse); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
	// Unknown fields of every wire type are kept or skipped, never an error.
	b := protowire.AppendTag(nil, 40, protowire.Fixed32Type)
	b = protowire.AppendFixed32(b, 7)
	b = protowire.AppendTag(b, 41, protowire.BytesType)
	b = protowire.AppendBytes(b, []byte{1, 2})
	b = protowire.AppendTag(b, 42, protowire.Fixed64Type)
	b = protowire.AppendFixed64(b, 9)
	v, err := decode(b, batteryMessage)
	if err != nil || v["40"] != uint32(7) || v["41"] != "AQI=" || v["42"] != uint64(9) {
		t.Errorf("%v %v", v, err)
	}
}

// grpcServer is a C3 and a PCCS answering what each test sets.
type grpcServer struct {
	srv         *httptest.Server
	discoveries atomic.Int32
	answer      func(w http.ResponseWriter, r *http.Request)
}

func newGRPCServer(t *testing.T) (*grpcServer, *Client) {
	t.Helper()
	g := &grpcServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cnepmob", func(w http.ResponseWriter, r *http.Request) {
		g.discoveries.Add(1)
		host, port, _ := strings.Cut(r.Host, ":")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"c3":{"grpcHost":"`+host+`","grpcPort":`+port+`}}`) //nolint:gosec // test: JSON, not a page
	})
	mux.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.Header.Get("Content-Type") != "application/grpc" || r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Vin") != vin {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		g.answer(w, r)
	})
	g.srv = h2c(t, mux)
	return g, NewClient(Hosts{Discovery: g.srv.URL + "/cnepmob", PCCS: g.srv.URL}, g.srv.Client())
}

// respond answers a message, then the status in the trailers.
func respond(msg []byte, status string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
		frame := make([]byte, 5)
		binary.BigEndian.PutUint32(frame[1:], uint32(len(msg))) //nolint:gosec // test
		_, _ = w.Write(append(frame, msg...))
		w.Header().Set("Grpc-Status", status)
		w.Header().Set("Grpc-Message", "")
	}
}

// refuse answers a status alone, in the headers.
func refuse(status, message string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", status)
		w.Header().Set("Grpc-Message", message)
	}
}

func TestGRPCCalls(t *testing.T) {
	battery := protowire.AppendBytes(protowire.AppendTag(nil, 3, protowire.BytesType), appendField(nil, 6, 2))
	target := protowire.AppendBytes(protowire.AppendTag(nil, 3, protowire.BytesType), appendField(nil, 1, 90))
	for _, tt := range []struct {
		name   string
		answer func(w http.ResponseWriter, r *http.Request)
		call   func(t *testing.T, c *Client) ([]byte, error)
		kind   Kind
		ok     bool
		code   string
	}{
		{"battery", respond(battery, "0"), batteryCall, 0, true, ""},
		{"target", respond(target, "0"), targetCall, 0, true, ""},
		{"a Polestar 2 on PCCS", refuse("7", "Permission%20denied for LPSYCZRE5RL000014"), targetCall, KindNotProvisioned, false, "PERMISSION_DENIED"},
		{"not implemented", refuse("12", ""), batteryCall, KindNotProvisioned, false, "UNIMPLEMENTED"},
		{"unauthenticated", refuse("16", ""), batteryCall, KindUnauthorized, false, "UNAUTHENTICATED"},
		{"limited", refuse("8", ""), batteryCall, KindRateLimited, false, "RESOURCE_EXHAUSTED"},
		{"unavailable", refuse("14", ""), batteryCall, KindOther, false, "UNAVAILABLE"},
		{"a status unknown", refuse("99", ""), batteryCall, KindOther, false, ""},
		{"no status", respond(battery, ""), batteryCall, KindOther, false, ""},
		{"failed after the message", respond(battery, "13"), batteryCall, KindOther, false, "INTERNAL"},
		{"OK without a message", refuse("0", ""), batteryCall, KindOther, false, ""},
		{"no battery", respond(nil, "0"), batteryCall, KindOther, false, ""},
		{"a gateway's 401", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }, batteryCall, KindUnauthorized, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g, c := newGRPCServer(t)
			g.answer = tt.answer
			raw, err := tt.call(t, c)
			if tt.ok {
				if err != nil || !json.Valid(raw) {
					t.Fatalf("%s %v", raw, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("no error: %s", raw)
			}
			if kindOf(err) != tt.kind {
				t.Errorf("%v: kind %d, want %d", err, kindOf(err), tt.kind)
			}
			if e, ok := err.(*APIError); ok && (e.Code != tt.code || strings.Contains(e.Message, vin)) { //nolint:errorlint // test
				t.Errorf("%+v", e)
			}
		})
	}
}

func batteryCall(t *testing.T, c *Client) ([]byte, error) { return c.Battery(t.Context(), "tok", vin) }

func targetCall(t *testing.T, c *Client) ([]byte, error) { return c.TargetSoC(t.Context(), "tok", vin) }

// The C3 host is asked for once, and again after a call could not reach it.
func TestTheC3HostIsDiscoveredOnce(t *testing.T) {
	g, c := newGRPCServer(t)
	g.answer = respond(protowire.AppendBytes(protowire.AppendTag(nil, 3, protowire.BytesType), nil), "0")
	for range 2 {
		if _, err := c.Battery(t.Context(), "tok", vin); err != nil {
			t.Fatal(err)
		}
	}
	if n := g.discoveries.Load(); n != 1 {
		t.Errorf("%d discoveries, want 1", n)
	}
	c.grpc.mu.Lock()
	c.grpc.c3 = "http://127.0.0.1:1" // gone
	c.grpc.mu.Unlock()
	if _, err := c.Battery(t.Context(), "tok", vin); err == nil || kindOf(err) != -1 {
		t.Fatalf("unreachable: %v", err)
	}
	if _, err := c.Battery(t.Context(), "tok", vin); err != nil || g.discoveries.Load() != 2 {
		t.Errorf("after: %v, %d discoveries", err, g.discoveries.Load())
	}
}

func TestADiscoveryWithoutHostFails(t *testing.T) {
	for name, body := range map[string]string{
		"no c3": `{}`, "no port": `{"c3":{"grpcHost":"h"}}`, "a path": `{"c3":{"grpcHost":"h/x","grpcPort":443}}`,
		"a port in a string": `{"c3":{"grpcHost":"","grpcPort":"443"}}`,
	} {
		srv := h2c(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
		c := NewClient(Hosts{Discovery: srv.URL}, srv.Client())
		if _, err := c.Battery(t.Context(), "tok", vin); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := NewClient(Hosts{}, http.DefaultClient).Battery(t.Context(), "tok", vin); err == nil {
		t.Error("no discovery URL: no error")
	}
}
