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

func newSim(t *testing.T) *sim {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/polestar4-id.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	s, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	srv := httptest.NewServer(polestaridapi.NewHandler([]polestaridapi.Source{s}, clk, polestaridapi.DefaultBehavior()))
	t.Cleanup(srv.Close)
	base := srv.URL + strings.TrimSuffix(polestaridapi.Prefix, "/")
	c := NewClient(Hosts{Auth: base, API: srv.URL + polestaridapi.APIPath}, srv.Client())
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
