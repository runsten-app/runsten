package bmw

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/bmwapi"
	"runsten/internal/simulator/vehicle"
)

const vin = "WBY31AW02RFR00001"

// waitTries bounds a wait for what the stream delivers, 10 ms apart: 5 s.
const waitTries = 500

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

// car is a simulated i4 whose state the test sets.
type car struct {
	spec vehicle.Spec
	st   vehicle.State
}

func (*car) VIN() string                         { return vin }
func (*car) Brand() string                       { return "bmw" }
func (c *car) ReportAt(time.Time) vehicle.Report { return vehicle.Report{Spec: c.spec} }
func (c *car) StateAt(time.Time) vehicle.State   { return c.st }

// sim is the client against the simulator, with its stream.
type sim struct {
	*Client
	clk    *clock.Manual
	srv    *httptest.Server
	car    *car
	h      *bmwapi.Handler
	stream *bmwapi.Stream
	addr   string // the stream's, tcp://
}

func newSim(t *testing.T) *sim {
	t.Helper()
	c := &car{
		spec: vehicle.Spec{Model: "i4 eDrive40", ModelYear: 2024, BatteryKWh: 83.9, UsableKWh: 80.7, ConsumptionKWhPer100km: 16},
		st: vehicle.State{
			SoC: 64.2, OdometerKm: 21034.6, TargetSoC: 80,
			Position: vehicle.Position{Lat: 50.8270, Lon: 4.3720, Heading: 91},
		},
	}
	clk := clock.NewManual(t0)
	h := bmwapi.NewHandler([]bmwapi.Source{c}, clk, bmwapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &sim{
		Client: NewClient(Hosts{Auth: srv.URL + "/gcdm/oauth", API: srv.URL}, srv.Client()),
		clk:    clk, srv: srv, car: c, h: h,
	}
}

// startStream serves the simulator's stream on the loopback, stepped by the test.
func (s *sim) startStream(t *testing.T) {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	st, err := bmwapi.NewStream(s.h, ln, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = st.Run(ctx, time.Hour) // stepped by the test
	}()
	t.Cleanup(func() { cancel(); <-done })
	s.stream, s.addr = st, "tcp://"+ln.Addr().String()
}

func (s *sim) control(t *testing.T, path string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.srv.URL+bmwapi.ControlPrefix+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

// signIn runs the device flow through, as the owner approving it.
func (s *sim) signIn(t *testing.T) Tokens {
	t.Helper()
	ctx := t.Context()
	verifier, challenge := NewVerifier()
	d, err := s.DeviceCode(ctx, bmwapi.DevClientID, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if d.UserCode == "" || d.VerificationURI == "" || d.Interval != 5*time.Second || d.ExpiresIn != 5*time.Minute {
		t.Fatalf("device code: %+v", d)
	}
	if _, err := s.PollToken(ctx, bmwapi.DevClientID, d.DeviceCode, verifier); kindOf(err) != KindPending {
		t.Fatalf("before approval: %v", err)
	}
	s.clk.Advance(time.Second)
	if _, err := s.PollToken(ctx, bmwapi.DevClientID, d.DeviceCode, verifier); kindOf(err) != KindSlowDown {
		t.Fatalf("polled too soon: %v", err)
	}
	s.clk.Advance(15 * time.Second)
	tok, err := s.PollToken(ctx, bmwapi.DevClientID, d.DeviceCode, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if tok.GCID != bmwapi.GCID || tok.Access == "" || tok.ID == "" || tok.Refresh == "" || tok.ExpiresIn != time.Hour {
		t.Fatalf("tokens: %+v", tok)
	}
	return tok
}

func kindOf(err error) Kind {
	var e *APIError
	if !errors.As(err, &e) {
		return -1
	}
	return e.Kind
}

func TestSimulatorAuth(t *testing.T) {
	ctx := t.Context()
	s := newSim(t)
	if _, err := s.DeviceCode(ctx, "not-a-client", "x"); kindOf(err) != KindClientRefused {
		t.Errorf("unknown client ID: %v", err)
	}
	tok := s.signIn(t)

	next, err := s.Refresh(ctx, bmwapi.DevClientID, tok.Refresh)
	if err != nil {
		t.Fatal(err)
	}
	if next.Refresh == tok.Refresh || next.GCID != bmwapi.GCID {
		t.Errorf("refresh: %+v", next)
	}
	if _, err := s.Refresh(ctx, bmwapi.DevClientID, tok.Refresh); kindOf(err) != KindTokenRefused {
		t.Errorf("a refresh token used: %v", err)
	}
	s.control(t, "revoke")
	if _, err := s.Refresh(ctx, bmwapi.DevClientID, next.Refresh); kindOf(err) != KindTokenRefused {
		t.Errorf("revoked: %v", err)
	}
}

func TestSimulatorAPI(t *testing.T) {
	ctx := t.Context()
	s := newSim(t)
	tok := s.signIn(t)

	maps, err := s.Mappings(ctx, tok.Access)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || maps[0] != (Mapping{VIN: vin, Primary: true}) {
		t.Errorf("mappings: %+v", maps)
	}

	raw, err := s.BasicData(ctx, tok.Access, vin)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Normalize(BasicData, raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Brand.V != "bmw" || m.Family.V != "i4" || m.Trim.V != "edrive40" || m.ModelYear.V != 2024 || !m.BatteryElectric() {
		t.Errorf("model: %+v", m)
	}
	if m.CapacityKWh.V != 83.9 || m.Sheet.V != (core.Sheet{Colour: "C4P", Steering: core.LeftHand}) {
		t.Errorf("capacity and sheet: %+v %+v", m.CapacityKWh, m.Sheet)
	}

	// The container, created once, then found again.
	id, err := s.EnsureContainer(ctx, tok.Access)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.EnsureContainer(ctx, tok.Access); err != nil || again != id {
		t.Errorf("container again: %q %v, want %q", again, err, id)
	}

	raw, err = s.TelematicData(ctx, tok.Access, vin, id)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Normalize(TelematicData, raw)
	if err != nil {
		t.Fatal(err)
	}
	if snap.SoC.V != 64.2 || snap.OdometerKm.V != 21034 || snap.TargetSoC.V != 80 || !snap.SoC.At.Equal(t0.Add(16*time.Second)) {
		t.Errorf("values: %+v", snap)
	}
	if snap.Position.V != (core.Position{Lat: 50.827, Lon: 4.372}) || snap.Engine.V != core.EngineStopped ||
		snap.Charging.V != core.ChargingIdle || snap.Connection.V != core.Disconnected {
		t.Errorf("state: %+v", snap)
	}
	h, err := NormalizeHealth(TelematicData, raw)
	if err != nil || h.Tyres[core.FrontLeft].PressureKPa.V != 250 || h.Tyres[core.RearRight].PressureKPa.V != 270 {
		t.Errorf("health: %+v %v", h, err)
	}

	if _, err := s.TelematicData(ctx, tok.Access, vin, "GONE"); kindOf(err) != KindNotFound {
		t.Errorf("unknown container: %v", err)
	}
	if _, err := s.BasicData(ctx, tok.Access, "WBY31AW02RFR99999"); kindOf(err) != KindUnshared {
		t.Errorf("another VIN: %v", err)
	}
	if _, err := s.BasicData(ctx, "not-a-token", vin); kindOf(err) != KindUnauthorized {
		t.Errorf("a bad token: %v", err)
	}
	if _, err := s.ChargingHistory(ctx, tok.Access, vin, t0.Add(-30*24*time.Hour), t0); err != nil {
		t.Errorf("charging history: %v", err)
	}
	raw, err = s.Image(ctx, tok.Access, vin)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := DecodePicture(raw); err != nil || p.Type != "image/png" || len(p.Data) == 0 {
		t.Errorf("picture: %s %v", p.Type, err)
	}
	if u, err := UploadOf(Image, raw); err != nil || len(u.Fields) != 0 {
		t.Errorf("picture's upload: %+v %v", u, err)
	}
	if _, err := s.Image(ctx, tok.Access, "WBY31AW02RFR99999"); kindOf(err) != KindUnshared {
		t.Errorf("another VIN's picture: %v", err)
	}
	s.control(t, "quota?used=50")
	if _, err := s.BasicData(ctx, tok.Access, vin); kindOf(err) != KindRateLimited {
		t.Errorf("quota spent: %v", err)
	}
}

// valuesOf waits until the subscription holds a value of the descriptor k equal to want.
func valuesOf(t *testing.T, sub *Subscription, k, want string) core.Snapshot {
	t.Helper()
	for i := 0; ; i++ {
		raw := sub.Values(vin)
		v, err := parseValues(Stream, raw)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := v[k].text(); got == want {
			s, err := Normalize(Stream, raw)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
		if i == waitTries {
			t.Fatalf("%s: %q, want %q", k, v[k].Value, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSimulatorStream(t *testing.T) {
	ctx := t.Context()
	s := newSim(t)
	s.startStream(t)
	tok := s.signIn(t)

	bad := NewSubscription(s.addr, bmwapi.GCID)
	var se *StreamError
	if err := bad.Connect(ctx, "not-a-token"); !errors.As(err, &se) || se.Code != RefusedBadPassword {
		t.Errorf("a bad ID token: %v", err)
	}

	sub := NewSubscription(s.addr, bmwapi.GCID)
	t.Cleanup(sub.Close)
	if err := sub.Connect(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if up, err := sub.Up(); !up || err != nil {
		t.Errorf("up: %v %v", up, err)
	}
	// On subscribing, the current values.
	snap := valuesOf(t, sub, DescSoC, "64.2")
	if snap.OdometerKm.V != 21034 || snap.Position.V.Lat != 50.827 || snap.Charging.V != core.ChargingIdle {
		t.Errorf("first values: %+v", snap)
	}

	// Another client of the account is refused: one connection at a time.
	other := NewSubscription(s.addr, bmwapi.GCID)
	if err := other.Connect(ctx, tok.ID); !errors.As(err, &se) || se.Code != RefusedNotAuthorized {
		t.Errorf("a second connection: %v", err)
	}

	// Then what changes.
	s.clk.Advance(time.Minute)
	s.car.st.PluggedIn, s.car.st.Charging, s.car.st.ChargingPowerW, s.car.st.ChargeType = true, vehicle.Charging, 11000, vehicle.AC
	s.stream.Step()
	snap = valuesOf(t, sub, DescChargingStatus, "CHARGINGACTIVE")
	if snap.ChargeType.V != core.AC || snap.PowerW.V != 11000 || snap.Phases.V != 3 || snap.VoltageV.V != 230 || snap.Connection.V != core.Connected {
		t.Errorf("charging: %+v", snap)
	}

	// A new ID token: the connection of the old one is closed, and the new one takes the
	// account's slot.
	next, err := s.Refresh(ctx, bmwapi.DevClientID, tok.Refresh)
	if err != nil {
		t.Fatal(err)
	}
	if err := sub.Connect(ctx, next.ID); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if up, _ := sub.Up(); !up {
		t.Error("not up after reconnecting")
	}

	// The ID token's expiry: the broker closes the connection.
	s.clk.Advance(2 * time.Hour)
	s.stream.Step()
	for i := 0; ; i++ {
		if up, err := sub.Up(); !up && err != nil {
			break
		}
		if i == waitTries {
			t.Fatal("still up past the ID token's expiry")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// What was received stays.
	if snap := valuesOf(t, sub, DescChargingStatus, "CHARGINGACTIVE"); snap.SoC.V != 64.2 {
		t.Errorf("kept: %+v", snap)
	}
}
