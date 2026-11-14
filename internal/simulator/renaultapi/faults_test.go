package renaultapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func (f *fixture) fault(t *testing.T, path string) {
	t.Helper()
	r := f.do(t, http.MethodPost, ControlPrefix+path, nil, nil)
	if r.status != http.StatusNoContent {
		t.Fatalf("%s: %d", path, r.status)
	}
}

func TestTFAFault(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	dev := f.bootstrap(t)
	if r := login(f, t, dev); r.status != http.StatusOK {
		t.Fatalf("login: %d", r.status)
	}
	f.fault(t, "tfa")
	if r := login(f, t, dev); r.status != http.StatusForbidden || r.body["errorCode"] != 403101.0 {
		t.Errorf("after the fault: %d %v", r.status, r.body)
	}
	// Clear lifts it.
	f.fault(t, "clear")
	if r := login(f, t, dev); r.status != http.StatusOK {
		t.Errorf("after clear: %d", r.status)
	}
}

func TestRevokeFault(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	token := f.signIn(t)
	jwt := f.jwt(t, token)
	f.fault(t, "revoke")
	if r := f.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusUnauthorized || errorCode(r) != "err.func.wired.unauthorized" {
		t.Errorf("JWT after revoke: %d %s", r.status, r.raw)
	}
	r := f.gigyaPost(t, "getJWT", url.Values{"login_token": {token}}, nil)
	if r.status != http.StatusForbidden || r.body["errorCode"] != 403005.0 {
		t.Errorf("login token after revoke: %d %v", r.status, r.body)
	}
	// Only a new sign-in reads again.
	if r := f.kamereon(t, f.session(t), "persons/"+personID); r.status != http.StatusOK {
		t.Errorf("new sign-in: %d", r.status)
	}
}

func TestOverloadedFault(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	jwt := f.session(t)
	f.fault(t, "overloaded")
	r := f.kamereon(t, jwt, carPath("battery-status"))
	if r.status != http.StatusForbidden || errorCode(r) != "err.func.wired.overloaded" {
		t.Fatalf("overloaded: %d %s", r.status, r.raw)
	}
	f.clk.Advance(time.Hour)
	if r := f.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusUnauthorized {
		t.Fatalf("the JWT of fifteen minutes is over: %d", r.status)
	}
	if r := f.kamereon(t, f.session(t), carPath("battery-status")); r.status != http.StatusOK {
		t.Errorf("after the hour: %d %s", r.status, r.raw)
	}
}

func TestPrivacyFault(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	jwt := f.session(t)
	f.fault(t, "privacy")
	r := f.kamereon(t, jwt, carPath("location"))
	if r.status != http.StatusForbidden || errorCode(r) != "err.func.privacy.on" {
		t.Errorf("location: %d %s", r.status, r.raw)
	}
	// Only the position is hidden.
	if r := f.kamereon(t, jwt, carPath("battery-status")); r.status != http.StatusOK {
		t.Errorf("battery: %d", r.status)
	}
}

func TestAccountChangedFault(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	jwt := f.session(t)
	f.fault(t, "account-changed")
	for _, ep := range []string{"battery-status", "cockpit", "location", "hvac-status"} {
		r := f.kamereon(t, jwt, carPath(ep))
		if r.status != http.StatusForbidden || errorCode(r) != "err.func.not.connected" {
			t.Errorf("%s: %d %s", ep, r.status, r.raw)
		}
	}
	// The person tells the new id, which lists the car and reads it.
	if r := f.kamereon(t, jwt, "persons/"+personID); !strings.Contains(r.raw, movedAccount) {
		t.Errorf("person: %s", r.raw)
	}
	if r := f.kamereon(t, jwt, "accounts/"+movedAccount+"/vehicles"); r.status != http.StatusOK || !strings.Contains(r.raw, testVIN) {
		t.Errorf("vehicles: %d %s", r.status, r.raw)
	}
	if r := f.kamereon(t, jwt, strings.Replace(carPath("cockpit"), myRenaultAccount, movedAccount, 1)); r.status != http.StatusOK {
		t.Errorf("cockpit by the new id: %d %s", r.status, r.raw)
	}
}

func TestOutageFault(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	jwt := f.session(t)
	f.fault(t, "outage?for=30m")
	r := f.kamereon(t, jwt, "persons/"+personID)
	if r.status != http.StatusBadGateway || r.body != nil || r.raw == "" {
		t.Errorf("outage: %d %q", r.status, r.raw)
	}
	// Gigya is not Kamereon's gateway.
	if f.signIn(t) == "" {
		t.Error("Gigya down with Kamereon")
	}
	f.clk.Advance(31 * time.Minute)
	jwt = f.session(t)
	if r := f.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusOK {
		t.Errorf("after the outage: %d", r.status)
	}
	f.fault(t, "outage")
	if r := f.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusBadGateway {
		t.Errorf("default outage: %d", r.status)
	}
	f.fault(t, "clear")
	if r := f.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusOK {
		t.Errorf("after clear: %d", r.status)
	}
}

func TestVehiclesListing(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	r := f.do(t, http.MethodGet, ControlPrefix+"vehicles", nil, nil)
	vins := r.body["vins"].([]any)
	if r.status != http.StatusOK || len(vins) != 1 || vins[0] != testVIN {
		t.Errorf("vehicles: %d %s", r.status, r.raw)
	}
}
