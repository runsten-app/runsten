package renaultsource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/platform/clock"
	"runsten/internal/renault"
	"runsten/internal/simulator/renaultapi"
	"runsten/internal/simulator/scenario"
	simvehicle "runsten/internal/simulator/vehicle"
)

const simVIN = "VF1RAE0B0S0000001"

// newSource starts the simulator on the Renault 5 of demonstration and signs its account
// in, as runsten-api does when the owner connects.
func newSource(t *testing.T) (*Source, *clock.Manual, *renaultapi.Handler) {
	t.Helper()
	return newSourceWith(t, nil)
}

// failing fails the calls whose path ends in suffix while on is set.
type failing struct {
	next   http.RoundTripper
	suffix string
	on     atomic.Bool
}

func (f *failing) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.on.Load() && strings.HasSuffix(r.URL.Path, f.suffix) {
		return nil, errors.New("connection reset")
	}
	return f.next.RoundTrip(r) //nolint:wrapcheck // the test's transport
}

// newSourceWith is newSource with the client's calls going through f, when not nil.
func newSourceWith(t *testing.T, f *failing) (*Source, *clock.Manual, *renaultapi.Handler) {
	t.Helper()
	data, err := os.ReadFile("../../../scenarios/renault5.yaml") //nolint:gosec // the test's own scenario
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := scenario.NewSimulation(sc, simvehicle.DefaultUploadPolicy())
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(sc.Start)
	h := renaultapi.NewHandler([]renaultapi.Source{sim}, clk, renaultapi.DevKeys(), renaultapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	hc := srv.Client()
	if f != nil {
		f.next = hc.Transport
		hc.Transport = f
	}
	client := renault.NewClient(renault.Config{
		GigyaURL: srv.URL + renaultapi.GigyaPrefix, GigyaKey: renaultapi.DevGigyaKey,
		KamereonURL: srv.URL + renaultapi.KamereonPrefix, KamereonKey: renaultapi.DevKamereonKey,
	}, hc)
	ctx := context.Background()
	d, err := client.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := client.Login(ctx, d, renaultapi.DevEmail, renaultapi.DevPassword)
	if err != nil {
		t.Fatal(err)
	}
	person, err := client.PersonID(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	cred := Credential{Session: sess, PersonID: person, Country: "FR"}
	return New(client, func(context.Context, string, string) (Credential, error) { return cred, nil }, clk), clk, h
}

func kindOf(err error) collector.ErrorKind {
	var se *collector.SourceError
	if !errors.As(err, &se) {
		return -1
	}
	return se.Kind
}

func TestLoginAndFetch(t *testing.T) {
	src, _, _ := newSource(t)
	ctx := context.Background()
	sess, ttl, err := src.Login(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if ttl != 14*time.Minute {
		t.Errorf("ttl %v: the JWT's fifteen minutes less one", ttl)
	}
	raw, err := sess.Fetch(ctx, simVIN, collector.Endpoint(renault.BatteryStatus))
	if err != nil {
		t.Fatal(err)
	}
	var a collector.Activity
	if err := src.Observe(collector.Endpoint(renault.BatteryStatus), raw, &a); err != nil || a.Charges || a.Driving {
		t.Errorf("activity %+v, %v", a, err)
	}
	// The response carries its model, which its normalization reads the quirks of.
	if s, _ := renault.Normalize(renault.BatteryStatus, raw); !s.SoC.OK {
		t.Errorf("battery: %+v", s)
	}
	if _, err := sess.Fetch(ctx, "VF1AAAAA555777000", collector.Endpoint(renault.Cockpit)); kindOf(err) != collector.KindUnshared {
		t.Errorf("a vehicle the account does not list: %v", err)
	}
}

func TestRefusals(t *testing.T) {
	src, _, h := newSource(t)
	ctx := context.Background()
	sess, _, err := src.Login(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	h.Overloaded()
	_, err = sess.Fetch(ctx, simVIN, collector.Endpoint(renault.Cockpit))
	var se *collector.SourceError
	if !errors.As(err, &se) || se.Kind != collector.KindRateLimited || se.RetryIn != 15*time.Minute {
		t.Errorf("the quota: %v", err)
	}
	h.Clear()
	h.Privacy()
	if _, err := sess.Fetch(ctx, simVIN, collector.Endpoint(renault.Location)); kindOf(err) != collector.KindNotAvailable {
		t.Errorf("the privacy mode: %v", err)
	}
	h.Clear()
	h.Revoke()
	if _, _, err := src.Login(ctx, "a", "c"); kindOf(err) != collector.KindCredentialRefused {
		t.Errorf("the login token revoked: %v", err)
	}
}

// TestUnserved: an endpoint the model does not serve is not called.
func TestUnserved(t *testing.T) {
	src, _, _ := newSource(t)
	sess := &session{src: src, vehicles: map[string]vehicle{simVIN: {account: "x", model: "X101VE"}}}
	if _, err := sess.Fetch(context.Background(), simVIN, collector.Endpoint(renault.Location)); kindOf(err) != collector.KindNotAvailable {
		t.Errorf("a Zoe phase 1's position: %v", err)
	}
}

// TestListedHourly: the account's vehicles are listed at the first login, then once an
// hour.
func TestListedHourly(t *testing.T) {
	src, clk, _ := newSource(t)
	ctx := context.Background()
	if _, _, err := src.Login(ctx, "a", "c"); err != nil {
		t.Fatal(err)
	}
	at := src.lists["c"].at
	clk.Advance(30 * time.Minute)
	if _, _, err := src.Login(ctx, "a", "c"); err != nil || !src.lists["c"].at.Equal(at) {
		t.Errorf("listed again within the hour: %v", err)
	}
	clk.Advance(30 * time.Minute)
	if _, _, err := src.Login(ctx, "a", "c"); err != nil || src.lists["c"].at.Equal(at) {
		t.Errorf("not listed again after the hour: %v", err)
	}
}

// TestAccountMoved: Renault moved the account to another id; the vehicles are listed
// again, and the car read by the new one, not taken for unshared.
func TestAccountMoved(t *testing.T) {
	src, _, h := newSource(t)
	ctx := context.Background()
	sess, _, err := src.Login(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	before := src.lists["c"].vehicles[simVIN].account
	h.AccountChanged()
	if _, err := sess.Fetch(ctx, simVIN, collector.Endpoint(renault.BatteryStatus)); err != nil {
		t.Fatalf("read after the move: %v", err)
	}
	src.mu.Lock()
	after := src.lists["c"].vehicles[simVIN].account
	src.mu.Unlock()
	if after == before || after == "" {
		t.Errorf("account %q, before %q", after, before)
	}
	if _, err := sess.Fetch(ctx, simVIN, collector.Endpoint(renault.Cockpit)); err != nil {
		t.Errorf("the next read: %v", err)
	}
}

// TestListingFails: a listing that fails keeps the vehicles listed before; with none
// listed before, the login fails, to be tried again, and no car is taken for unshared.
func TestListingFails(t *testing.T) {
	f := &failing{suffix: "/vehicles"}
	src, clk, _ := newSourceWith(t, f)
	ctx := context.Background()
	if _, _, err := src.Login(ctx, "a", "c"); err != nil {
		t.Fatal(err)
	}
	clk.Advance(listEvery)
	f.on.Store(true)
	sess, _, err := src.Login(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Fetch(ctx, simVIN, collector.Endpoint(renault.Cockpit)); err != nil {
		t.Errorf("the car listed before, read: %v", err)
	}
	if _, _, err := src.Login(ctx, "a", "other"); err == nil || kindOf(err) == collector.KindUnshared {
		t.Errorf("a first listing failed: %v", err)
	}
}
