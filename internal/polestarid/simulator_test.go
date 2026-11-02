package polestarid

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/polestaridapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

const vin = "LPSYCZRE5RL000014"

// sim is the client against the simulator on the polestar4-id scenario, at its start.
type sim struct {
	*Client
	clk *clock.Manual
	srv *httptest.Server
}

// newSim starts the simulator on the polestar4-id scenario, its model changed to model
// when given.
func newSim(t *testing.T, model ...string) *sim {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/polestar4-id.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range model {
		sc.Vehicle.Model = m
	}
	s, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	srv := h2c(t, polestaridapi.NewHandler([]polestaridapi.Source{s}, clk, polestaridapi.DefaultBehavior()))
	base := srv.URL + strings.TrimSuffix(polestaridapi.Prefix, "/")
	c := NewClient(Hosts{
		Auth: base, API: srv.URL + polestaridapi.APIPath, Discovery: srv.URL + polestaridapi.DiscoveryPath, PCCS: srv.URL,
	}, srv.Client())
	return &sim{Client: c, clk: clk, srv: srv}
}

func (s *sim) control(t *testing.T, path string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.srv.URL+polestaridapi.ControlPrefix+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func kindOf(err error) Kind {
	var e *APIError
	if !errors.As(err, &e) {
		return -1
	}
	return e.Kind
}

// browserCode signs in as the owner does in a browser, on the page AuthorizeURL opens,
// and returns the address polestar.com's sign-in-callback receives.
func (s *sim) browserCode(t *testing.T, verifier string) string {
	t.Helper()
	hc := *s.srv.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.AuthorizeURL(verifier, "st"), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	p := page{status: resp.StatusCode, body: body}
	m := resumeRe.FindSubmatch(p.body)
	if m == nil {
		t.Fatalf("no form in %s", p.body)
	}
	form := url.Values{"pf.username": {polestaridapi.DevEmail}, "pf.pass": {polestaridapi.DevPassword}}
	resp, err = hc.PostForm(s.srv.URL+string(m[1]), form) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.Header.Get("Location")
}

func TestTheCodePastedIsExchanged(t *testing.T) {
	s := newSim(t)
	verifier := NewVerifier()
	pasted := s.browserCode(t, verifier)
	if !strings.HasPrefix(pasted, RedirectURI+"?") {
		t.Fatalf("landed on %s", pasted)
	}
	code, state, err := CodeOf(pasted)
	if err != nil || state != "st" {
		t.Fatalf("CodeOf: %q %q %v", code, state, err)
	}
	tok, err := s.Exchange(t.Context(), code, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access == "" || tok.Refresh == "" || tok.ExpiresIn != 300*time.Second {
		t.Errorf("tokens %+v", tok)
	}
	if _, err := s.Exchange(t.Context(), code, verifier); kindOf(err) != KindTokenRefused {
		t.Errorf("the code again: %v, want KindTokenRefused", err)
	}
}

func TestAnotherVerifierIsRefused(t *testing.T) {
	s := newSim(t)
	code, _, err := CodeOf(s.browserCode(t, NewVerifier()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Exchange(t.Context(), code, NewVerifier()); kindOf(err) != KindTokenRefused {
		t.Errorf("%v, want KindTokenRefused", err)
	}
}

func TestCodeOf(t *testing.T) {
	for _, tt := range []struct {
		pasted, code string
		err          bool
	}{
		{"https://www.polestar.com/sign-in-callback?code=abc-123&state=s", "abc-123", false},
		{"  https://www.polestar.com/sign-in-callback?code=abc&state=s\n", "abc", false},
		{"abc-123", "abc-123", false},
		{"https://www.polestar.com/uk/auth/error", "", true},
		{"", "", true},
	} {
		code, _, err := CodeOf(tt.pasted)
		if code != tt.code || (err != nil) != tt.err {
			t.Errorf("CodeOf(%q) = %q, %v", tt.pasted, code, err)
		}
	}
}

func TestSignInWithThePassword(t *testing.T) {
	s := newSim(t)
	tok, err := s.SignIn(t.Context(), polestaridapi.DevEmail, polestaridapi.DevPassword)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access == "" || tok.Refresh == "" {
		t.Errorf("tokens %+v", tok)
	}
	if _, err := s.SignIn(t.Context(), polestaridapi.DevEmail, "wrong"); kindOf(err) != KindSignInRefused {
		t.Errorf("wrong password: %v, want KindSignInRefused", err)
	}
}

func TestTheRefreshTokenRotates(t *testing.T) {
	s := newSim(t)
	first, err := s.Refresh(t.Context(), polestaridapi.DevRefreshToken())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Refresh(t.Context(), first.Refresh)
	if err != nil || second.Refresh == first.Refresh {
		t.Fatalf("refresh: %v, rotated %v", err, second.Refresh != first.Refresh)
	}
	if _, err := s.Refresh(t.Context(), first.Refresh); kindOf(err) != KindTokenRefused {
		t.Errorf("the previous one: %v, want KindTokenRefused", err)
	}
	s.control(t, "revoke")
	if _, err := s.Refresh(t.Context(), second.Refresh); kindOf(err) != KindTokenRefused {
		t.Errorf("revoked: %v, want KindTokenRefused", err)
	}
}

func TestCarsAndTelematicsNormalize(t *testing.T) {
	s := newSim(t)
	tok, err := s.Refresh(t.Context(), polestaridapi.DevRefreshToken())
	if err != nil {
		t.Fatal(err)
	}
	cars, err := s.Cars(t.Context(), tok.Access)
	if err != nil || len(cars) != 1 || cars[0].VIN != vin {
		t.Fatalf("cars %+v %v", cars, err)
	}
	m, err := Normalize(Cars, cars[0].Raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Family.V != "Polestar 4" || m.ModelYear.V != 2024 || m.Brand.V != Brand || m.Powertrain.V != core.Electric {
		t.Errorf("model %+v", m)
	}

	raw, err := s.Telematics(t.Context(), tok.Access, vin)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Normalize(Telematics, raw)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	if snap.SoC.V != 66 || !snap.SoC.At.Equal(start) || snap.OdometerKm.V != 9800 || snap.Charging.V != core.ChargingIdle {
		t.Errorf("snapshot %+v", snap)
	}
	if snap.Covers&(core.FieldPosition|core.FieldEngine|core.FieldConnection) != 0 {
		t.Errorf("covers %b: no position, engine nor cable", snap.Covers)
	}
	h, err := NormalizeHealth(Telematics, raw)
	if err != nil || h.Covers&core.HealthService == 0 || h.Service.V != core.ServiceOK || h.Covers&core.HealthTyres != 0 {
		t.Errorf("health %+v %v", h, err)
	}
}

func TestAnExpiredAccessIsUnauthorized(t *testing.T) {
	s := newSim(t)
	tok, err := s.Refresh(t.Context(), polestaridapi.DevRefreshToken())
	if err != nil {
		t.Fatal(err)
	}
	s.clk.Advance(6 * time.Minute)
	if _, err := s.Telematics(context.Background(), tok.Access, vin); kindOf(err) != KindUnauthorized {
		t.Errorf("%v, want KindUnauthorized", err)
	}
}

func TestAChargeIsSeen(t *testing.T) {
	s := newSim(t)
	// The scenario charges at home from 05:00 + 2h + 20m + 9h + 25m = 16:45.
	s.clk.Advance(11*time.Hour + 50*time.Minute)
	tok, err := s.Refresh(t.Context(), polestaridapi.DevRefreshToken())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.Telematics(t.Context(), tok.Access, vin)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Normalize(Telematics, raw)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Charging.V != core.ChargingActive {
		t.Errorf("charging %+v, want active", snap.Charging)
	}
}

// Over gRPC, the charge at home is measured: the cable, its type, its power, and the
// charge limit.
func TestTheBatteryAndTheChargeLimitOverGRPC(t *testing.T) {
	s := newSim(t)
	tok, err := s.Refresh(t.Context(), polestaridapi.DevRefreshToken())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.Battery(t.Context(), tok.Access, vin)
	if err != nil {
		t.Fatal(err)
	}
	parked, err := Normalize(Battery, raw)
	if err != nil {
		t.Fatal(err)
	}
	if parked.Connection.V != core.Disconnected || parked.Charging.V != core.ChargingIdle || parked.PowerW.V != 0 || !parked.PowerW.OK || parked.SoC.V != 66 {
		t.Errorf("parked %+v", parked)
	}

	s.clk.Advance(11*time.Hour + 50*time.Minute) // charging at home since 16:45
	if tok, err = s.Refresh(t.Context(), tok.Refresh); err != nil {
		t.Fatal(err)
	}
	if raw, err = s.Battery(t.Context(), tok.Access, vin); err != nil {
		t.Fatal(err)
	}
	c, err := Normalize(Battery, raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.Connection.V != core.Connected || c.Charging.V != core.ChargingActive || c.ChargeType.V != core.AC || c.PowerW.V != 11_000 ||
		c.VoltageV.V != 400 || c.CurrentA.V != 28 || !c.TimeLeft.OK || c.TimeLeft.V.To != core.ToFull {
		t.Errorf("charging %+v", c)
	}
	if raw, err = s.TargetSoC(t.Context(), tok.Access, vin); err != nil {
		t.Fatal(err)
	}
	if ts, err := Normalize(TargetSoC, raw); err != nil || ts.TargetSoC.V != 90 {
		t.Errorf("target %+v %v", ts, err)
	}
	again, err := s.TargetSoC(t.Context(), tok.Access, vin)
	if err != nil {
		t.Fatal(err)
	}
	if f1, _ := Fingerprint(raw); string(f1) != string(must(Fingerprint(again))) {
		t.Errorf("the same limit read twice is new: %s, then %s", raw, again)
	}
	if _, err := s.Battery(t.Context(), "expired", vin); kindOf(err) != KindUnauthorized {
		t.Errorf("a token refused: %v, want KindUnauthorized", err)
	}
	if _, err := s.Battery(t.Context(), tok.Access, "LPSYCZRE5RL000099"); kindOf(err) != KindNotProvisioned {
		t.Errorf("another car: %v, want KindNotProvisioned", err)
	}
}

// A Polestar 2 is not provisioned on PCCS: the charge limit is refused, the battery read.
func TestAPolestar2HasNoChargeLimit(t *testing.T) {
	s := newSim(t, "Polestar 2")
	tok, err := s.Refresh(t.Context(), polestaridapi.DevRefreshToken())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TargetSoC(t.Context(), tok.Access, vin); kindOf(err) != KindNotProvisioned {
		t.Errorf("%v, want KindNotProvisioned", err)
	}
	if _, err := s.Battery(t.Context(), tok.Access, vin); err != nil {
		t.Error(err)
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
