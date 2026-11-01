package polestaridsource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/platform/clock"
	"runsten/internal/polestarid"
	"runsten/internal/simulator/polestaridapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

const vin = "LPSYCZRE5RL000014"

var setAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// creds is an in-memory Store of one connection's credential.
type creds struct {
	cred      Credential
	rotateErr error
	rotations int
}

func (s *creds) PolestarIDCredential(context.Context, string, string) (Credential, error) {
	return s.cred, nil
}

func (s *creds) RotatePolestarIDToken(_ context.Context, _, _ string, at time.Time, token string) error {
	if s.rotateErr != nil {
		return s.rotateErr
	}
	if at.Equal(s.cred.SetAt) {
		s.cred.Refresh = token
		s.rotations++
	}
	return nil
}

type sim struct {
	src   *Source
	creds *creds
	clk   *clock.Manual
	srv   *httptest.Server
}

func newSim(t *testing.T, cred Credential) *sim {
	t.Helper()
	data, err := os.ReadFile("../../../scenarios/polestar4-id.yaml")
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
	client := polestarid.NewClient(polestarid.Hosts{
		Auth: srv.URL + strings.TrimSuffix(polestaridapi.Prefix, "/"), API: srv.URL + polestaridapi.APIPath,
	}, srv.Client())
	cred.SetAt = setAt
	st := &creds{cred: cred}
	return &sim{src: New(client, st), creds: st, clk: clk, srv: srv}
}

func (s *sim) revoke(t *testing.T) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.srv.URL+polestaridapi.ControlPrefix+"revoke", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func kindOf(err error) collector.ErrorKind {
	var e *collector.SourceError
	if !errors.As(err, &e) {
		return -1
	}
	return e.Kind
}

func TestEachLoginKeepsTheRotatedTokenFirst(t *testing.T) {
	s := newSim(t, Credential{Refresh: polestaridapi.DevRefreshToken()})
	for i := range 3 {
		sess, ttl, err := s.src.Login(t.Context(), "acc", "conn")
		if err != nil {
			t.Fatalf("login %d: %v", i, err)
		}
		if ttl != 300*time.Second {
			t.Errorf("ttl %v, want 5 min", ttl)
		}
		if _, err := sess.Fetch(t.Context(), vin, collector.Endpoint(polestarid.Telematics)); err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	if s.creds.rotations != 3 {
		t.Errorf("%d rotations kept, want 3: the refresh token rotates at every refresh", s.creds.rotations)
	}
}

func TestATokenNotStoredIsKeptInMemory(t *testing.T) {
	s := newSim(t, Credential{Refresh: polestaridapi.DevRefreshToken()})
	s.creds.rotateErr = errors.New("database down")
	if _, _, err := s.src.Login(t.Context(), "acc", "conn"); err == nil {
		t.Fatal("login without the rotated token stored: want an error")
	}
	// The stored one no longer refreshes; the one in memory does.
	s.creds.rotateErr = nil
	if _, _, err := s.src.Login(t.Context(), "acc", "conn"); err != nil {
		t.Fatalf("login with the token kept in memory: %v", err)
	}
	if s.creds.rotations != 1 {
		t.Errorf("%d rotations, want 1", s.creds.rotations)
	}
}

func TestARefusedTokenSignsInAgainWithThePassword(t *testing.T) {
	s := newSim(t, Credential{Refresh: polestaridapi.DevRefreshToken(), Email: polestaridapi.DevEmail, Password: polestaridapi.DevPassword})
	s.revoke(t)
	if _, _, err := s.src.Login(t.Context(), "acc", "conn"); err != nil {
		t.Fatalf("login after a revocation, with the password: %v", err)
	}
	if s.creds.rotations != 1 || s.creds.cred.Refresh == polestaridapi.DevRefreshToken() {
		t.Error("the sign-in's refresh token not kept")
	}
}

func TestARefusedTokenWithoutPasswordIsRefused(t *testing.T) {
	s := newSim(t, Credential{Refresh: polestaridapi.DevRefreshToken()})
	s.revoke(t)
	if _, _, err := s.src.Login(t.Context(), "acc", "conn"); kindOf(err) != collector.KindCredentialRefused {
		t.Errorf("%v, want KindCredentialRefused", err)
	}
}

func TestAWrongPasswordIsRefused(t *testing.T) {
	s := newSim(t, Credential{Refresh: polestaridapi.DevRefreshToken(), Email: polestaridapi.DevEmail, Password: "changed"})
	s.revoke(t)
	if _, _, err := s.src.Login(t.Context(), "acc", "conn"); kindOf(err) != collector.KindCredentialRefused {
		t.Errorf("%v, want KindCredentialRefused", err)
	}
}

func TestACarNotListedIsUnshared(t *testing.T) {
	s := newSim(t, Credential{Refresh: polestaridapi.DevRefreshToken()})
	sess, _, err := s.src.Login(t.Context(), "acc", "conn")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Fetch(t.Context(), "LPSYCZRE5RL000099", collector.Endpoint(polestarid.Telematics)); kindOf(err) != collector.KindUnshared {
		t.Errorf("%v, want KindUnshared", err)
	}
}

func TestTheModelIsTheListsEntry(t *testing.T) {
	s := newSim(t, Credential{Refresh: polestaridapi.DevRefreshToken()})
	sess, _, err := s.src.Login(t.Context(), "acc", "conn")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sess.Fetch(t.Context(), vin, collector.Endpoint(polestarid.Cars))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := polestarid.Normalize(polestarid.Cars, raw)
	if err != nil || snap.Family.V != "Polestar 4" {
		t.Errorf("model %+v %v", snap, err)
	}
}

func TestObserveTellsTheCharge(t *testing.T) {
	s := newSim(t, Credential{Refresh: polestaridapi.DevRefreshToken()})
	s.clk.Advance(11*time.Hour + 50*time.Minute) // charging at home
	sess, _, err := s.src.Login(t.Context(), "acc", "conn")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sess.Fetch(t.Context(), vin, collector.Endpoint(polestarid.Telematics))
	if err != nil {
		t.Fatal(err)
	}
	var a collector.Activity
	if err := s.src.Observe(collector.Endpoint(polestarid.Telematics), raw, &a); err != nil {
		t.Fatal(err)
	}
	if !a.Charges || a.Charging != string(core.ChargingActive) || a.Driving {
		t.Errorf("activity %+v", a)
	}
	u, err := s.src.Upload(collector.Endpoint(polestarid.Telematics), raw)
	if err != nil || u.At.IsZero() || u.ModelYear != 2024 || len(u.Fields) == 0 {
		t.Errorf("upload %+v %v", u, err)
	}
}
