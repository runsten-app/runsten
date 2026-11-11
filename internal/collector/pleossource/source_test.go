package pleossource

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/pleos"
	"runsten/internal/simulator/pleosapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

const simVIN = "KNAPC81ABPP000001"

// sim answers the Pleos Vehicle Data API on the EV6 of demonstration: this is the
// contract test between the source and the fake API.
type sim struct {
	src *Source
	h   *pleosapi.Handler
	clk *clock.Manual
	// other takes the key's token over, as another client on it would (evcc, Home
	// Assistant).
	other *pleos.Client
}

func newSim(t *testing.T, behavior pleosapi.Behavior) *sim {
	t.Helper()
	data, err := os.ReadFile("../../../scenarios/ev6.yaml") //nolint:gosec // the test's own scenario
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
	h := pleosapi.NewHandler([]pleosapi.Source{s}, clk, pleosapi.DevCredential(), behavior)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	base := srv.URL + pleosapi.Prefix
	dev := pleosapi.DevCredential()
	credential := func(context.Context, string, string) (Credential, error) {
		return Credential{ClientID: dev.ClientID, ClientSecret: dev.ClientSecret, Brand: pleos.Kia}, nil
	}
	src := New(pleos.NewClient(base, srv.Client()), credential, clk)
	return &sim{src: src, h: h, clk: clk, other: pleos.NewClient(base, srv.Client())}
}

// login answers a session of the source and its lifetime.
func (s *sim) login(t *testing.T) (collector.Session, time.Duration) {
	t.Helper()
	sess, ttl, err := s.src.Login(context.Background(), "account", "connection")
	if err != nil {
		t.Fatal(err)
	}
	return sess, ttl
}

// fetch reads the batteries, the endpoint every failure of the tests is told on.
func (s *sim) fetchErr(sess collector.Session) error {
	_, err := sess.Fetch(context.Background(), simVIN, collector.Endpoint(pleos.Batteries))
	return err
}

// TestLogin keeps a token until five minutes before its end, read in its JWT: the
// fake API issues one of an hour, the session lives fifty-five minutes.
func TestLogin(t *testing.T) {
	s := newSim(t, pleosapi.DefaultBehavior())
	sess, ttl := s.login(t)
	if want := 55 * time.Minute; ttl != want {
		t.Errorf("the session lives %v, want %v", ttl, want)
	}
	for _, ep := range []pleos.Endpoint{pleos.Driving, pleos.Batteries, pleos.Powertrains, pleos.Locations, pleos.Status} {
		raw, err := sess.Fetch(context.Background(), simVIN, collector.Endpoint(ep))
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		if snap, err := (pleos.Normalizer{}).Normalize(core.Kind{Source: pleos.Source, Endpoint: string(ep)}, raw); err != nil || snap.Covers == 0 {
			t.Errorf("%s: nothing normalized of %s (%v)", ep, raw, err)
		}
	}
	// The consent is kept to the entry of the vehicle read.
	raw, err := sess.Fetch(context.Background(), simVIN, collector.Endpoint(pleos.Consent))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := (pleos.Normalizer{}).Normalize(core.Kind{Source: pleos.Source, Endpoint: "consent"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Family.V != "EV6" || snap.Powertrain.V != core.Electric {
		t.Errorf("the vehicle's entry: %+v", snap)
	}
}

// TestSpec answers the cadences and the providers (ADR 0049 §4): the mode first, the
// consent last, one key per brand.
func TestSpec(t *testing.T) {
	s := newSim(t, pleosapi.DefaultBehavior())
	spec := s.src.Spec()
	if spec.Name != pleos.Source {
		t.Errorf("the source %q", spec.Name)
	}
	if len(spec.Providers) != 3 {
		t.Fatalf("the providers %v", spec.Providers)
	}
	for _, b := range pleos.Brands() {
		found := false
		for _, p := range spec.Providers {
			if p == b.Provider() {
				found = true
			}
		}
		if !found {
			t.Errorf("no provider of %s", b)
		}
	}
	want := []pleos.Endpoint{pleos.Driving, pleos.Batteries, pleos.Powertrains, pleos.Locations, pleos.Status, pleos.Consent}
	for i, ep := range spec.Endpoints {
		if collector.Endpoint(want[i]) != ep.Endpoint {
			t.Errorf("the endpoint %d: %v, want %v", i, ep.Endpoint, want[i])
		}
	}
	for _, ep := range spec.Endpoints {
		if ep.API != pleos.Source {
			t.Errorf("%s counts on %s", ep.Endpoint, ep.API)
		}
	}
	if spec.Daily != Daily || spec.Intervals != Intervals() {
		t.Errorf("the budget %d at %+v", spec.Daily, spec.Intervals)
	}
	// The optional endpoints: the API's compatibility matrix names models without
	// them, and their refusal is told to no one.
	if !optional(spec, pleos.Locations) || !optional(spec, pleos.Status) {
		t.Error("the locations and status are not optional")
	}
	if optional(spec, pleos.Batteries) || optional(spec, pleos.Driving) {
		t.Error("the mode's endpoints come and go")
	}
}

func optional(spec collector.SourceSpec, ep pleos.Endpoint) bool {
	for _, e := range spec.Endpoints {
		if e.Endpoint == collector.Endpoint(ep) {
			return e.Optional
		}
	}
	return false
}

// TestTakenPlace: a token refused that another took the place of is the access's own
// refusal, which the collector answers by asking for another once; the third within
// the hour tells the key is shared, and the connection waits.
func TestTakenPlace(t *testing.T) {
	s := newSim(t, pleosapi.DefaultBehavior())
	sess, _ := s.login(t)
	// Another client asks for a token with the key: this one stops reading.
	if _, err := s.other.Token(context.Background(), pleos.Kia, cred()); err != nil {
		t.Fatal(err)
	}
	err := s.fetchErr(sess)
	var se *collector.SourceError
	if !errors.As(err, &se) || se.Kind != collector.KindUnauthorized {
		t.Fatalf("the first refusal: %v", err)
	}
	// Again, with fresh tokens: the second tells of the same, the third that the key
	// is shared (bound to another client).
	for i := 2; i <= 3; i++ {
		sess, _ = s.login(t)
		if _, err := s.other.Token(context.Background(), pleos.Kia, cred()); err != nil {
			t.Fatal(err)
		}
		err = s.fetchErr(sess)
		if !errors.As(err, &se) {
			t.Fatalf("the refusal %d: %v", i, err)
		}
		if i < 3 && se.Kind != collector.KindUnauthorized {
			t.Fatalf("the refusal %d: kind %v", i, se.Kind)
		}
	}
	if se.Kind != collector.KindKeyShared {
		t.Fatalf("the third fresh refusal: kind %v", se.Kind)
	}
	if se.RetryIn != keySharedWait {
		t.Errorf("the wait %v, want %v", se.RetryIn, keySharedWait)
	}
	// Past the wait, a token no other one takes reads again.
	s.clk.Advance(keySharedWait + time.Minute)
	sess, _ = s.login(t)
	if err := s.fetchErr(sess); err != nil {
		t.Errorf("after the wait: %v", err)
	}
}

// TestShortToken: a token shorter than the margin is kept its whole life, not the
// default's: past its end, its refusal is no live token's.
func TestShortToken(t *testing.T) {
	b := pleosapi.DefaultBehavior()
	b.TokenTTL = 3 * time.Minute
	s := newSim(t, b)
	sess, ttl := s.login(t)
	if ttl != 3*time.Minute {
		t.Fatalf("the session lives %v, want the token's 3 minutes", ttl)
	}
	s.clk.Advance(4 * time.Minute)
	for i := range keySharedAfter {
		var se *collector.SourceError
		if err := s.fetchErr(sess); !errors.As(err, &se) || se.Kind != collector.KindUnauthorized {
			t.Fatalf("the refusal %d of an ended token: %v", i+1, err)
		}
	}
}

// TestAnotherKey: the refusals counted for a key tell nothing of the one given after
// it.
func TestAnotherKey(t *testing.T) {
	s := newSim(t, pleosapi.DefaultBehavior())
	taken := func() collector.ErrorKind {
		t.Helper()
		sess, _ := s.login(t)
		if _, err := s.other.Token(context.Background(), pleos.Kia, cred()); err != nil {
			t.Fatal(err)
		}
		var se *collector.SourceError
		if err := s.fetchErr(sess); !errors.As(err, &se) {
			t.Fatalf("the refusal: %v", err)
		}
		return se.Kind
	}
	for range keySharedAfter - 1 {
		if k := taken(); k != collector.KindUnauthorized {
			t.Fatalf("a refusal before the last: kind %v", k)
		}
	}
	// Another key is given, then this one again: counted from none.
	dev := s.src.credential
	s.src.credential = func(context.Context, string, string) (Credential, error) {
		return Credential{ClientID: "another", ClientSecret: "key", Brand: pleos.Kia}, nil
	}
	if _, _, err := s.src.Login(context.Background(), "account", "connection"); err == nil {
		t.Fatal("the simulator took a key it does not know")
	}
	s.src.credential = dev
	if k := taken(); k != collector.KindUnauthorized {
		t.Errorf("the first refusal of a new key: kind %v", k)
	}
}

func cred() pleos.Credential {
	c := pleosapi.DevCredential()
	return pleos.Credential{ClientID: c.ClientID, ClientSecret: c.ClientSecret}
}

// TestErrors tells what the source makes of the API's refusals (ADR 0049 §3): the
// Kind the collector reacts to.
func TestErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(*pleosapi.Handler)
		want collector.ErrorKind
	}{
		{"the key refused", func(h *pleosapi.Handler) { h.RefuseKey() }, collector.KindCredentialRefused},
		{"the consent withdrawn", func(h *pleosapi.Handler) { h.Withdraw("") }, collector.KindUnshared},
		{"no data", func(h *pleosapi.Handler) { h.LeaveWithoutData("") }, collector.KindNotAvailable},
		{"the limit", func(h *pleosapi.Handler) { h.RateLimit(15 * time.Minute) }, collector.KindRateLimited},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newSim(t, pleosapi.DefaultBehavior())
			test.set(s.h)
			_, ttl, err := s.src.Login(context.Background(), "account", "connection")
			var se *collector.SourceError
			if test.want == collector.KindCredentialRefused {
				// The key's own refusal is the login's.
				if !errors.As(err, &se) || se.Kind != test.want {
					t.Fatalf("the login: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				err = s.fetchErr(mustSession(t, s))
				if !errors.As(err, &se) || se.Kind != test.want {
					t.Fatalf("the fetch: %v", err)
				}
			}
			if test.want == collector.KindRateLimited && se.RetryIn != 15*time.Minute {
				t.Errorf("the wait %v", se.RetryIn)
			}
			_ = ttl
		})
	}
}

func mustSession(t *testing.T, s *sim) collector.Session {
	t.Helper()
	sess, _, err := s.src.Login(context.Background(), "account", "connection")
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

// TestObserve reads the mode: the ignition drives, the batteries charge.
func TestObserve(t *testing.T) {
	s := newSim(t, pleosapi.DefaultBehavior())
	sess, _ := s.login(t)
	raw, err := sess.Fetch(context.Background(), simVIN, collector.Endpoint(pleos.Driving))
	if err != nil {
		t.Fatal(err)
	}
	var a collector.Activity
	if err := s.src.Observe(collector.Endpoint(pleos.Driving), raw, &a); err != nil {
		t.Fatal(err)
	}
	if a.Driving {
		t.Errorf("a parked car drives: %+v", a)
	}
	raw, err = sess.Fetch(context.Background(), simVIN, collector.Endpoint(pleos.Batteries))
	if err != nil {
		t.Fatal(err)
	}
	a = collector.Activity{}
	if err := s.src.Observe(collector.Endpoint(pleos.Batteries), raw, &a); err != nil {
		t.Fatal(err)
	}
	if a.Charges {
		t.Errorf("a parked car charges: %+v", a)
	}
}
