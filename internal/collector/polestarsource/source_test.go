package polestarsource

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/platform/clock"
	"runsten/internal/polestar"
	"runsten/internal/simulator/polestarapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

const vin = "LPSVSEDE5RL000001"

// simulated serves the demonstration Polestar (polestar2), read with the simulator's credential, or
// with cred when it is not empty.
func simulated(t *testing.T, cred polestar.Credential) Source {
	t.Helper()
	data, err := os.ReadFile("../../../scenarios/polestar2.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := scenario.NewSimulation(sc, vehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	dev := polestarapi.DevCredential()
	srv := httptest.NewServer(polestarapi.NewHandler([]polestarapi.Source{sim}, clock.NewManual(sc.Start), dev,
		polestarapi.DefaultLimits(), polestarapi.DefaultBehavior()))
	t.Cleanup(srv.Close)
	if cred == (polestar.Credential{}) {
		cred = polestar.Credential{ClientID: dev.ClientID, ClientSecret: dev.ClientSecret, AccountID: dev.AccountID}
	}
	return New(polestar.NewClient(srv.URL+polestarapi.Prefix, srv.Client()),
		func(context.Context, string, string) (polestar.Credential, error) { return cred, nil })
}

func TestLoginAndFetch(t *testing.T) {
	s := simulated(t, polestar.Credential{})
	sess, ttl, err := s.Login(context.Background(), "a", "c")
	if err != nil || ttl != time.Hour {
		t.Fatalf("login: %v, %v", ttl, err)
	}
	var a collector.Activity
	for _, ep := range s.Spec().Endpoints {
		raw, err := sess.Fetch(context.Background(), vin, ep.Endpoint)
		if err != nil {
			t.Fatalf("%s: %v", ep.Endpoint, err)
		}
		if err := s.Observe(ep.Endpoint, raw, &a); err != nil {
			t.Fatalf("%s: %v", ep.Endpoint, err)
		}
		if _, err := s.Fingerprint(raw); err != nil {
			t.Fatal(err)
		}
	}
	// Parked at home: the engine stopped, no charge.
	if a.Driving || a.Charges || a.Engine == "" || a.Charging == "" {
		t.Errorf("activity %+v", a)
	}
	if _, err := sess.Fetch(context.Background(), "LPSVSEDE5RL000009", collector.Endpoint(polestar.Battery)); !isKind(err, collector.KindUnshared) {
		t.Errorf("another VIN: %v", err)
	}
}

func TestLoginRefused(t *testing.T) {
	s := simulated(t, polestar.Credential{ClientID: "runsten-dev", ClientSecret: "wrong", AccountID: "x"})
	if _, _, err := s.Login(context.Background(), "a", "c"); !isKind(err, collector.KindCredentialRefused) {
		t.Errorf("login: %v", err)
	}
	boom := errors.New("boom")
	failing := New(nil, func(context.Context, string, string) (polestar.Credential, error) { return polestar.Credential{}, boom })
	if _, _, err := failing.Login(context.Background(), "a", "c"); !errors.Is(err, boom) {
		t.Errorf("credential not read: %v", err)
	}
}

func TestSourceError(t *testing.T) {
	for kind, want := range map[polestar.Kind]collector.ErrorKind{
		polestar.KindCredentialRefused: collector.KindCredentialRefused,
		polestar.KindUnauthorized:      collector.KindUnauthorized,
		polestar.KindVINUnauthorized:   collector.KindUnshared,
		polestar.KindDomainNotGranted:  collector.KindNotGranted,
		polestar.KindNotAvailable:      collector.KindNotAvailable,
		polestar.KindRateLimited:       collector.KindRateLimited,
		polestar.KindOther:             collector.KindOther,
	} {
		err := sourceError(&polestar.APIError{Kind: kind, Status: 403, RetryIn: time.Minute})
		var se *collector.SourceError
		if !errors.As(err, &se) || se.Kind != want || se.Status != 403 || se.RetryIn != time.Minute {
			t.Errorf("%v: %+v", kind, err)
		}
	}
	network := errors.New("dial tcp: refused")
	if err := sourceError(network); err != network { //nolint:errorlint // the same error, untouched
		t.Errorf("network error: %v", err)
	}
}

func TestSpec(t *testing.T) {
	spec := Source{}.Spec()
	if spec.Name != polestar.Source || spec.Daily != Daily || len(spec.Endpoints) != len(polestar.Domains()) {
		t.Fatalf("spec %+v", spec)
	}
	for i, d := range polestar.Domains() {
		if ep := spec.Endpoints[i]; ep.Endpoint != collector.Endpoint(d) || ep.API != polestar.Source {
			t.Errorf("endpoint %d: %+v", i, ep)
		}
	}
}

func isKind(err error, k collector.ErrorKind) bool {
	var se *collector.SourceError
	return errors.As(err, &se) && se.Kind == k
}
