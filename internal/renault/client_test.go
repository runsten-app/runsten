package renault

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/renaultapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
)

const simVIN = "VF1RAE0B0S0000001"

// newSim starts the simulator on the Renault 5 of demonstration: this is the contract
// test between the client and the fake API.
func newSim(t *testing.T) (*Client, *clock.Manual, *renaultapi.Handler) {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/renault5.yaml") //nolint:gosec // the test's own scenario
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
	clk := clock.NewManual(sc.Start)
	h := renaultapi.NewHandler([]renaultapi.Source{sim}, clk, renaultapi.DevKeys(), renaultapi.DefaultBehavior())
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(Config{
		GigyaURL: srv.URL + renaultapi.GigyaPrefix, GigyaKey: renaultapi.DevGigyaKey,
		KamereonURL: srv.URL + renaultapi.KamereonPrefix, KamereonKey: renaultapi.DevKamereonKey,
	}, srv.Client()), clk, h
}

func kind(err error) Kind {
	var e *APIError
	if !errors.As(err, &e) {
		return -1
	}
	return e.Kind
}

// signIn signs the development account in, with its code when the account asks.
func signIn(ctx context.Context, t *testing.T, c *Client) Session {
	t.Helper()
	d, err := c.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Login(ctx, d, renaultapi.DevEmail, renaultapi.DevPassword)
	var e *APIError
	if errors.As(err, &e) && e.Kind == KindTFARequired {
		ch, serr := c.SendCode(ctx, d, e.RegToken, "fr")
		if serr != nil {
			t.Fatal(serr)
		}
		if ch.MaskedMail == "" {
			t.Error("no address told")
		}
		if err := c.Verify(ctx, ch, "000000"); kind(err) != KindWrongCode {
			t.Errorf("a wrong code: %v", err)
		}
		if err := c.Verify(ctx, ch, renaultapi.DevCode); err != nil {
			t.Fatal(err)
		}
		s, err = c.Login(ctx, d, renaultapi.DevEmail, renaultapi.DevPassword)
	}
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSignInAndRead(t *testing.T) {
	c, clk, _ := newSim(t)
	ctx := context.Background()
	s := signIn(ctx, t, c)
	person, err := c.PersonID(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	jwt, err := c.JWT(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if want := clk.Now().Add(jwtExpiration * time.Second); !jwt.ExpiresAt.Equal(want.Truncate(time.Second)) {
		t.Errorf("the JWT's end %v, want %v", jwt.ExpiresAt, want)
	}
	p, err := c.Person(ctx, jwt.Value, person, "FR")
	if err != nil {
		t.Fatal(err)
	}
	var account string
	for _, a := range p.Accounts {
		if a.Type == AccountMyRenault {
			account = a.ID
		}
	}
	if account == "" || p.Country == "" {
		t.Fatalf("person %+v", p)
	}
	vs, err := c.Vehicles(ctx, jwt.Value, account, p.Country)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].VIN != simVIN || vs[0].ModelCode != "R5E1VE" || !vs[0].Electric() {
		t.Fatalf("vehicles %+v", vs)
	}
	for _, ep := range Endpoints() {
		raw, err := c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, ep)
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		if _, err := Normalize(ep, raw); err != nil {
			t.Errorf("%s: %v", ep, err)
		}
	}
	raw, _ := c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, BatteryStatus)
	if snap, _ := Normalize(BatteryStatus, raw); !snap.SoC.OK || snap.SoC.V != 66 {
		t.Errorf("SoC = %+v", snap.SoC)
	}
	raw, _ = c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, Details)
	if snap, _ := Normalize(Details, raw); snap.Family.V != "Renault 5 E-Tech" || snap.Brand.V != "renault" {
		t.Errorf("details: %+v %+v", snap.Brand, snap.Family)
	}
}

func TestSignInWithCode(t *testing.T) {
	c, _, h := newSim(t)
	h.RequireTFA()
	ctx := context.Background()
	d, _ := c.Bootstrap(ctx)
	if _, err := c.Login(ctx, d, renaultapi.DevEmail, renaultapi.DevPassword); kind(err) != KindTFARequired {
		t.Fatalf("login: %v", err)
	}
	signIn(ctx, t, c)
}

func TestRefusals(t *testing.T) {
	c, clk, h := newSim(t)
	ctx := context.Background()
	d, _ := c.Bootstrap(ctx)
	if _, err := c.Login(ctx, d, renaultapi.DevEmail, "wrong"); kind(err) != KindCredentialRefused {
		t.Errorf("a wrong password: %v", err)
	}
	s := signIn(ctx, t, c)
	jwt, _ := c.JWT(ctx, s)
	person, _ := c.PersonID(ctx, s)
	p, _ := c.Person(ctx, jwt.Value, person, "FR")
	account := p.Accounts[0].ID
	for _, a := range p.Accounts {
		if a.Type == AccountMyRenault {
			account = a.ID
		}
	}

	// The JWT past its end: refused, another asked for.
	clk.Advance(jwtExpiration*time.Second + time.Second)
	if _, err := c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, Cockpit); kind(err) != KindUnauthorized {
		t.Errorf("a JWT expired: %v", err)
	}
	jwt, _ = c.JWT(ctx, s)

	h.Privacy()
	if _, err := c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, Location); kind(err) != KindNotAvailable {
		t.Errorf("the privacy mode: %v", err)
	}
	h.Overloaded()
	_, err := c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, Cockpit)
	var e *APIError
	if !errors.As(err, &e) || e.Kind != KindRateLimited || e.RetryIn != quotaWait {
		t.Errorf("the quota: %v", err)
	}
	h.Clear()
	h.AccountChanged()
	if _, err := c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, BatteryStatus); kind(err) != KindUnshared {
		t.Errorf("the account changed: %v", err)
	}
	h.Clear()
	h.Outage(time.Hour)
	if _, err := c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, BatteryStatus); kind(err) != KindOther {
		t.Errorf("an outage: %v", err)
	}
	h.Clear()
	h.Revoke()
	if _, err := c.JWT(ctx, s); kind(err) != KindCredentialRefused {
		t.Errorf("a login token revoked: %v", err)
	}
	if _, err := c.Fetch(ctx, jwt.Value, account, p.Country, simVIN, "charges"); !errors.Is(err, ErrUnknownEndpoint) {
		t.Errorf("an unknown endpoint: %v", err)
	}
}
