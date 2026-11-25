package fordsource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/ford"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/fordapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

const (
	simVIN   = "WF0TESTPUMA000001"
	redirect = "http://localhost:5173/auth/ford/callback"
)

// memStore keeps one connection's credential, as the store does.
type memStore struct {
	mu        sync.Mutex
	cred      Credential
	rotations int
	rotateErr error
	expiries  []time.Time
}

func (m *memStore) FordCredential(context.Context, string, string) (Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cred, nil
}

func (m *memStore) RotateFordToken(_ context.Context, _, _ string, setAt time.Time, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rotateErr != nil {
		return m.rotateErr
	}
	if setAt.Equal(m.cred.SetAt) {
		m.cred.Refresh = token
		m.rotations++
	}
	return nil
}

func (m *memStore) SetCredentialExpiry(_ context.Context, _, _ string, _, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expiries = append(m.expiries, expiresAt)
	return nil
}

// sim answers FordConnect Query on the Puma of demonstration: this is the contract test
// between the source and the fake service.
type sim struct {
	src   *Source
	h     *fordapi.Handler
	clk   *clock.Manual
	st    *memStore
	calls *int
}

func newSim(t *testing.T) *sim {
	t.Helper()
	data, err := os.ReadFile("../../../scenarios/puma.yaml") //nolint:gosec // the test's own scenario
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
	h := fordapi.NewHandler([]fordapi.Source{s}, clk, fordapi.DevApplication(), fordapi.DefaultBehavior())
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path != fordapi.Prefix+"auth/init" {
			calls++
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	client := ford.NewClient(ford.Hosts{
		Auth: srv.URL + fordapi.Prefix + "auth/init", Token: srv.URL + fordapi.Prefix + "token", API: srv.URL + "/ford",
	}, srv.Client())

	// The owner's sign-in, as a browser that does not follow Ford's redirect.
	dev := fordapi.DevApplication()
	app := ford.Application{ClientID: dev.ClientID, ClientSecret: dev.ClientSecret, RedirectURI: redirect}
	authURL, err := client.AuthorizationURL(app, "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	hc := *srv.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, authURL, http.NoBody)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	back, _ := url.Parse(resp.Header.Get("Location"))
	tok, err := client.Exchange(t.Context(), app, back.Query().Get("code"))
	if err != nil {
		t.Fatal(err)
	}
	st := &memStore{cred: Credential{Application: app, Refresh: tok.Refresh, SetAt: sc.Start}}
	return &sim{src: New(client, st, clk), h: h, clk: clk, st: st, calls: &calls}
}

func kindOf(t *testing.T, err error) collector.ErrorKind {
	t.Helper()
	var se *collector.SourceError
	if !errors.As(err, &se) {
		t.Fatalf("not a SourceError: %v", err)
	}
	return se.Kind
}

// TestSpec: the telemetry first, which tells the mode; nothing read again at once at a
// trip's end.
func TestSpec(t *testing.T) {
	spec := (&Source{}).Spec()
	if spec.Name != "ford" || len(spec.Endpoints) != 2 || spec.Endpoints[0].Endpoint != "telemetry" || len(spec.Settled) != 0 {
		t.Errorf("spec %+v", spec)
	}
	if iv := spec.Intervals; iv.Active != time.Minute || iv.Parked != 5*time.Minute {
		t.Errorf("intervals %+v", iv)
	}
	// A read a minute all day stays within the budget.
	if spec.Daily < 24*60 {
		t.Errorf("daily %d", spec.Daily)
	}
}

// TestLoginKeepsTheTokenGiven: each refresh gives a new refresh token, kept before the
// access is used; the grant's end is a year after the owner's sign-in, recorded once.
func TestLoginKeepsTheTokenGiven(t *testing.T) {
	s := newSim(t)
	first := s.st.cred.Refresh
	sess, ttl, err := s.src.Login(t.Context(), "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if ttl != 19*time.Minute {
		t.Errorf("ttl %v, want the access's 20 minutes less a margin", ttl)
	}
	if s.st.rotations != 1 || s.st.cred.Refresh == first {
		t.Errorf("the refresh token given was not kept: %d rotations", s.st.rotations)
	}
	if len(s.st.expiries) != 1 || !s.st.expiries[0].Equal(s.clk.Now().Add(GrantLife)) {
		t.Errorf("expiries %v", s.st.expiries)
	}
	if _, err := sess.Fetch(t.Context(), simVIN, collector.Endpoint(ford.Telemetry)); err != nil {
		t.Fatal(err)
	}
	s.clk.Advance(20 * time.Minute)
	if _, _, err := s.src.Login(t.Context(), "a", "c"); err != nil {
		t.Fatal(err)
	}
	if s.st.rotations != 2 || len(s.st.expiries) != 1 {
		t.Errorf("second login: %d rotations, expiries %v", s.st.rotations, s.st.expiries)
	}
}

// TestTokenNotKept: when the store cannot keep the refresh token given, the login fails
// and the next refreshes with it, the stored one no longer refreshing.
func TestTokenNotKept(t *testing.T) {
	s := newSim(t)
	s.st.rotateErr = errors.New("database down")
	if _, _, err := s.src.Login(t.Context(), "a", "c"); err == nil {
		t.Fatal("a login whose refresh token was not kept succeeded")
	}
	s.st.rotateErr = nil
	if _, _, err := s.src.Login(t.Context(), "a", "c"); err != nil {
		t.Fatalf("the token kept in memory did not refresh: %v", err)
	}
	if s.st.rotations != 1 {
		t.Errorf("%d rotations", s.st.rotations)
	}
	// A credential the owner set since is refreshed with its own token.
	s.st.rotateErr = errors.New("database down")
	_, _, _ = s.src.Login(t.Context(), "a", "c")
	s.st.rotateErr = nil
	s.st.cred.SetAt = s.st.cred.SetAt.Add(time.Hour)
	s.st.cred.Refresh = "the-owner-s-new-one"
	if _, _, err := s.src.Login(t.Context(), "a", "c"); kindOf(t, err) != collector.KindCredentialRefused {
		t.Errorf("the previous credential's token was used for the new one: %v", err)
	}
}

// TestGap: two calls of a connection are never closer than the gap; the second is held
// back without a call, and the vehicle held until then.
func TestGap(t *testing.T) {
	s := newSim(t)
	sess, _, err := s.src.Login(t.Context(), "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Fetch(t.Context(), simVIN, collector.Endpoint(ford.Telemetry)); err != nil {
		t.Fatal(err)
	}
	before := *s.calls
	_, err = sess.Fetch(t.Context(), simVIN, collector.Endpoint(ford.Garage))
	if kindOf(t, err) != collector.KindVehicleLimited || *s.calls != before {
		t.Errorf("a call at once: %v, %d calls made", err, *s.calls-before)
	}
	if until := s.src.HeldUntil(simVIN); !until.Equal(s.clk.Now().Add(minGap)) {
		t.Errorf("held until %v", until)
	}
	s.clk.Advance(minGap)
	raw, err := sess.Fetch(t.Context(), simVIN, collector.Endpoint(ford.Garage))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := ford.VehicleOf(raw); err != nil || v.VIN != simVIN {
		t.Errorf("garage %s, %v", raw, err)
	}
}

// TestObserve: the ignition tells a drive, the charge's status a charge.
func TestObserve(t *testing.T) {
	s := newSim(t)
	sess, _, err := s.src.Login(t.Context(), "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	read := func() collector.Activity {
		t.Helper()
		raw, err := sess.Fetch(t.Context(), simVIN, collector.Endpoint(ford.Telemetry))
		if err != nil {
			t.Fatal(err)
		}
		var a collector.Activity
		if err := s.src.Observe(collector.Endpoint(ford.Telemetry), raw, &a); err != nil {
			t.Fatal(err)
		}
		if u, err := s.src.Upload(collector.Endpoint(ford.Telemetry), raw); err != nil || u.At.IsZero() || len(u.Fields) == 0 {
			t.Errorf("upload %+v, %v", u, err)
		}
		return a
	}
	if a := read(); a.Driving || a.Charges || a.Engine != "stopped" {
		t.Errorf("parked: %+v", a)
	}
	s.clk.Advance(2*time.Hour + 10*time.Minute) // on the way to work
	sess, _, _ = s.src.Login(t.Context(), "a", "c")
	if a := read(); !a.Driving || a.Charges {
		t.Errorf("driving: %+v", a)
	}
	s.clk.Advance(11 * time.Hour) // charging at home
	sess, _, _ = s.src.Login(t.Context(), "a", "c")
	if a := read(); a.Driving || !a.Charges {
		t.Errorf("charging: %+v", a)
	}
}

// TestRefusals: a grant withdrawn marks the connection; Ford's outage and its limits do
// not.
func TestRefusals(t *testing.T) {
	s := newSim(t)
	sess, _, err := s.src.Login(t.Context(), "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	s.h.Outage(time.Hour)
	if _, _, err := s.src.Login(t.Context(), "a", "c"); kindOf(t, err) != collector.KindOther {
		t.Errorf("the sign-in's outage: %v", err)
	}
	s.clk.Advance(time.Hour)
	s.h.Limit(5 * time.Minute)
	_, _, err = s.src.Login(t.Context(), "a", "c")
	var se *collector.SourceError
	if !errors.As(err, &se) || se.Kind != collector.KindRateLimited || se.RetryIn != 5*time.Minute {
		t.Errorf("limited: %v", err)
	}
	s.clk.Advance(5 * time.Minute)
	// The access of an hour ago is over: asked for again.
	if _, err := sess.Fetch(t.Context(), simVIN, collector.Endpoint(ford.Telemetry)); kindOf(t, err) != collector.KindUnauthorized {
		t.Errorf("an access over: %v", err)
	}
	s.h.Revoke("")
	if _, _, err := s.src.Login(t.Context(), "a", "c"); kindOf(t, err) != collector.KindCredentialRefused {
		t.Errorf("a grant withdrawn: %v", err)
	}
}
