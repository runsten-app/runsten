package renaultapi

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const testVIN = "VF1RAE0B0S0000001"

// car is a vehicle of a fixed report.
type car struct {
	vin, brand string
	rep        vehicle.Report
}

func (c car) VIN() string                       { return c.vin }
func (c car) Brand() string                     { return c.brand }
func (c car) ReportAt(time.Time) vehicle.Report { return c.rep }

// charging is a Renault 5 plugged in and charging at 11 kW, parked, last seen a moment
// before at.
func charging(at time.Time, trim string) vehicle.Report {
	return vehicle.Report{
		Spec:     vehicle.Spec{Model: "Renault 5", Trim: trim, BatteryKWh: 52, UsableKWh: 52, ConsumptionKWhPer100km: 15},
		Energy:   vehicle.State{SoC: 66.4, TargetSoC: 80, PluggedIn: true, Charging: vehicle.Charging, ChargeType: vehicle.AC, ChargingPowerW: 11000},
		EnergyAt: at.Add(-time.Minute),
		Status:   vehicle.State{OdometerKm: 6820.456},
		StatusAt: at.Add(-time.Hour), Location: vehicle.Position{Lat: 45.764, Lon: 4.8357}, LocationAt: at.Add(-time.Hour),
	}
}

type fixture struct {
	srv *httptest.Server
	clk *clock.Manual
	h   *Handler
}

func newFixture(t *testing.T, b Behavior, trim string) *fixture {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 13, 18, 0, 0, 0, time.UTC))
	h := NewHandler([]Source{car{testVIN, "renault", charging(clk.Now(), trim)}}, clk, DevKeys(), b)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &fixture{srv: srv, clk: clk, h: h}
}

// reply is a response read whole.
type reply struct {
	status  int
	cookies []*http.Cookie
	body    map[string]any
	raw     string
}

func (f *fixture) do(t *testing.T, method, path string, form url.Values, hdr map[string]string, cookies ...*http.Cookie) reply {
	t.Helper()
	var body io.Reader = http.NoBody
	if method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	} else if len(form) > 0 {
		path += "?" + form.Encode()
	}
	req, _ := http.NewRequestWithContext(t.Context(), method, f.srv.URL+path, body)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := f.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	r := reply{status: resp.StatusCode, cookies: resp.Cookies(), raw: string(raw)}
	_ = json.Unmarshal(raw, &r.body)
	return r
}

func (f *fixture) gigyaGet(t *testing.T, method string, q url.Values, dev []*http.Cookie) reply {
	t.Helper()
	q.Set("APIKey", DevGigyaKey)
	return f.do(t, http.MethodGet, "/renault/gigya/accounts."+method, q, nil, dev...)
}

func (f *fixture) gigyaPost(t *testing.T, method string, form url.Values, dev []*http.Cookie) reply {
	t.Helper()
	form.Set("ApiKey", DevGigyaKey)
	return f.do(t, http.MethodPost, "/renault/gigya/accounts."+method, form, nil, dev...)
}

func (f *fixture) bootstrap(t *testing.T) []*http.Cookie {
	t.Helper()
	r := f.gigyaGet(t, "webSdkBootstrap", url.Values{}, nil)
	if r.status != http.StatusOK || r.body["errorCode"] != 0.0 || len(r.cookies) != 2 {
		t.Fatalf("bootstrap: %d %v %v", r.status, r.cookies, r.body)
	}
	names := map[string]bool{}
	for _, c := range r.cookies {
		names[c.Name] = c.Value != ""
	}
	if !names["gmid"] || !names["ucid"] {
		t.Fatalf("device cookies: %v", r.cookies)
	}
	return r.cookies
}

func login(f *fixture, t *testing.T, dev []*http.Cookie) reply {
	t.Helper()
	return f.gigyaPost(t, "login", url.Values{"loginID": {DevEmail}, "password": {DevPassword}}, dev)
}

// signIn signs the device in and returns its login token, which must work.
func (f *fixture) signIn(t *testing.T) string {
	t.Helper()
	r := login(f, t, f.bootstrap(t))
	if r.status != http.StatusOK || r.body["errorCode"] != 0.0 {
		t.Fatalf("login: %d %v", r.status, r.body)
	}
	return r.body["sessionInfo"].(map[string]any)["cookieValue"].(string)
}

// jwt asks for a JWT with a login token.
func (f *fixture) jwt(t *testing.T, token string) string {
	t.Helper()
	r := f.gigyaPost(t, "getJWT", url.Values{"login_token": {token}, "expiration": {"900"}, "fields": {"data.personId"}}, nil)
	if r.status != http.StatusOK {
		t.Fatalf("getJWT: %d %v", r.status, r.body)
	}
	return r.body["id_token"].(string)
}

func (f *fixture) session(t *testing.T) string { return f.jwt(t, f.signIn(t)) }

// kamereon reads a path under commerce/v1 with the JWT, for the country FR.
func (f *fixture) kamereon(t *testing.T, jwt, path string) reply {
	t.Helper()
	return f.do(t, http.MethodGet, "/renault/kamereon/commerce/v1/"+path, url.Values{"country": {"FR"}},
		map[string]string{"apikey": DevKamereonKey, "x-gigya-id_token": jwt, "Accept": "application/json"})
}

func carPath(ep string) string {
	if ep == "battery-status" {
		return "accounts/" + myRenaultAccount + "/kamereon/kca/car-adapter/v2/cars/" + testVIN + "/" + ep
	}
	return "accounts/" + myRenaultAccount + "/kamereon/kca/car-adapter/v1/cars/" + testVIN + "/" + ep
}

func attrs(t *testing.T, r reply) map[string]any {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.raw)
	}
	data := r.body["data"].(map[string]any)
	if data["type"] != "Car" || data["id"] != testVIN {
		t.Errorf("data %v", data)
	}
	return data["attributes"].(map[string]any)
}

func errorCode(r reply) string {
	errs, _ := r.body["errors"].([]any)
	if len(errs) == 0 {
		return ""
	}
	return errs[0].(map[string]any)["errorCode"].(string)
}

func TestSignInWithoutTFA(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	dev := f.bootstrap(t)
	r := login(f, t, dev)
	if r.status != http.StatusOK || r.body["statusCode"] != 200.0 || r.body["statusReason"] != "OK" || r.body["callId"] == nil {
		t.Fatalf("login: %d %v", r.status, r.body)
	}
	token := r.body["sessionInfo"].(map[string]any)["cookieValue"].(string)
	info := f.gigyaPost(t, "getAccountInfo", url.Values{"login_token": {token}}, dev)
	if info.status != http.StatusOK || info.body["data"].(map[string]any)["personId"] != personID {
		t.Fatalf("account info: %d %v", info.status, info.body)
	}
	// A device without cookies signs in too when the account asks for no code.
	if r := login(f, t, nil); r.status != http.StatusOK {
		t.Errorf("no device: %d", r.status)
	}
	for _, tt := range []struct {
		name      string
		form      url.Values
		wantCode  float64
		wantState int
	}{
		{"wrong password", url.Values{"loginID": {DevEmail}, "password": {"nope"}, "ApiKey": {DevGigyaKey}}, 403042, 403},
		{"unknown user", url.Values{"loginID": {"x@y.z"}, "password": {DevPassword}, "ApiKey": {DevGigyaKey}}, 403042, 403},
		{"stale key", url.Values{"loginID": {DevEmail}, "password": {DevPassword}, "ApiKey": {"old"}}, 403042, 403},
		{"no key", url.Values{"loginID": {DevEmail}, "password": {DevPassword}}, 403042, 403},
	} {
		r := f.do(t, http.MethodPost, "/renault/gigya/accounts.login", tt.form, nil, dev...)
		if r.status != tt.wantState || r.body["errorCode"] != tt.wantCode || r.body["errorDetails"] != "invalid loginID or password" {
			t.Errorf("%s: %d %v", tt.name, r.status, r.body)
		}
	}
	if r := f.gigyaPost(t, "getAccountInfo", url.Values{"login_token": {"dead"}}, dev); r.status != http.StatusForbidden || r.body["errorCode"] != 403005.0 {
		t.Errorf("dead token: %d %v", r.status, r.body)
	}
	if r := f.do(t, http.MethodPost, "/renault/gigya/accounts.getAccountInfo", url.Values{"login_token": {token}, "ApiKey": {"old"}}, nil); r.body["errorCode"] != 400093.0 {
		t.Errorf("stale key elsewhere: %v", r.body)
	}
}

func TestJWT(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	token := f.signIn(t)
	jwt := f.jwt(t, token)
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", jwt)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct{ Exp, Iat int64 }
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if want := f.clk.Now().Add(900 * time.Second).Unix(); claims.Exp != want || claims.Iat != f.clk.Now().Unix() {
		t.Errorf("exp %d iat %d, want exp %d", claims.Exp, claims.Iat, want)
	}
	// The expiration asked for is honoured.
	r := f.gigyaPost(t, "getJWT", url.Values{"login_token": {token}, "expiration": {"60"}}, nil)
	short := r.body["id_token"].(string)
	if r := f.kamereon(t, short, "persons/"+personID); r.status != http.StatusOK {
		t.Fatalf("person: %d %s", r.status, r.raw)
	}
	f.clk.Advance(61 * time.Second)
	r2 := f.kamereon(t, short, "persons/"+personID)
	if r2.status != http.StatusUnauthorized || errorCode(r2) != "err.func.wired.unauthorized" {
		t.Errorf("expired: %d %s", r2.status, r2.raw)
	}
	// The other, of fifteen minutes, still reads; the login token asks for another.
	if r := f.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusOK {
		t.Errorf("still live: %d", r.status)
	}
	f.clk.Advance(15 * time.Minute)
	if r := f.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusUnauthorized {
		t.Errorf("expired: %d", r.status)
	}
	if again := f.jwt(t, token); again == jwt {
		t.Error("the same JWT twice")
	}
	if r := f.gigyaPost(t, "getJWT", url.Values{"login_token": {"dead"}}, nil); r.status != http.StatusForbidden || r.body["errorCode"] != 403005.0 {
		t.Errorf("dead login token: %d %v", r.status, r.body)
	}
}

func TestTwoFactor(t *testing.T) {
	f := newFixture(t, Behavior{PerHour: 60, TFA: true}, "R5E1VE")
	dev := f.bootstrap(t)
	r := login(f, t, dev)
	reg, _ := r.body["regToken"].(string)
	if r.status != http.StatusForbidden || r.body["errorCode"] != 403101.0 || reg == "" || r.body["errorDetails"] != "Pending Two-Factor Authentication" {
		t.Fatalf("login: %d %v", r.status, r.body)
	}
	q := url.Values{"provider": {"gigyaEmail"}, "mode": {"verify"}, "regToken": {reg}, "gmid": {dev[0].Value}, "ucid": {dev[1].Value}}
	if r := f.gigyaGet(t, "tfa.initTFA", url.Values{"regToken": {"nope"}}, dev); r.status != http.StatusForbidden {
		t.Errorf("unknown registration: %d", r.status)
	}
	init := f.gigyaGet(t, "tfa.initTFA", q, dev)
	assertion, _ := init.body["gigyaAssertion"].(string)
	if init.status != http.StatusOK || assertion == "" {
		t.Fatalf("init: %d %v", init.status, init.body)
	}
	if r := f.gigyaGet(t, "tfa.email.getEmails", url.Values{"gigyaAssertion": {"nope"}}, dev); r.body["errorCode"] != 403005.0 {
		t.Errorf("emails without assertion: %v", r.body)
	}
	em := f.gigyaGet(t, "tfa.email.getEmails", url.Values{"gigyaAssertion": {assertion}}, dev)
	list := em.body["emails"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["obfuscated"] != "d***@runsten.test" || list[0].(map[string]any)["id"] == "" {
		t.Fatalf("emails: %v", em.body)
	}
	id := list[0].(map[string]any)["id"].(string)
	if r := f.gigyaGet(t, "tfa.email.sendVerificationCode", url.Values{"gigyaAssertion": {assertion}, "emailID": {"other"}}, dev); r.body["errorCode"] != 400006.0 {
		t.Errorf("unknown address: %v", r.body)
	}
	sent := f.gigyaGet(t, "tfa.email.sendVerificationCode", url.Values{"gigyaAssertion": {assertion}, "emailID": {id}, "lang": {"fr"}}, dev)
	phv, _ := sent.body["phvToken"].(string)
	if phv == "" {
		t.Fatalf("send: %v", sent.body)
	}
	verify := func(code string) reply {
		return f.gigyaGet(t, "tfa.email.completeVerification", url.Values{"gigyaAssertion": {assertion}, "phvToken": {phv}, "code": {code}}, dev)
	}
	if r := verify("000000"); r.status != http.StatusBadRequest || r.body["errorCode"] != 400006.0 || r.body["errorDetails"] != "Wrong verification code" {
		t.Errorf("wrong code: %d %v", r.status, r.body)
	}
	// A wrong code leaves the challenge open, and the device not yet signed in.
	if r := login(f, t, dev); r.status != http.StatusForbidden {
		t.Errorf("login before the code: %d", r.status)
	}
	done := verify(DevCode)
	provider, _ := done.body["providerAssertion"].(string)
	if done.status != http.StatusOK || provider == "" {
		t.Fatalf("verify: %d %v", done.status, done.body)
	}
	fin := url.Values{"gigyaAssertion": {assertion}, "providerAssertion": {"nope"}, "regToken": {reg}, "tempDevice": {"false"}}
	if r := f.gigyaGet(t, "tfa.finalizeTFA", fin, dev); r.status != http.StatusForbidden {
		t.Errorf("finalize with another provider: %d", r.status)
	}
	fin.Set("providerAssertion", provider)
	if r := f.gigyaGet(t, "tfa.finalizeTFA", fin, dev); r.status != http.StatusOK || r.body["errorCode"] != 0.0 {
		t.Fatalf("finalize: %d %v", r.status, r.body)
	}
	if r := login(f, t, dev); r.status != http.StatusOK || r.body["sessionInfo"] == nil {
		t.Errorf("login after the code: %d %v", r.status, r.body)
	}
	// Another device is asked again.
	if r := login(f, t, f.bootstrap(t)); r.status != http.StatusForbidden || r.body["errorCode"] != 403101.0 {
		t.Errorf("another device: %d %v", r.status, r.body)
	}
}

func TestKamereonShapes(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	jwt := f.session(t)

	p := f.kamereon(t, jwt, "persons/"+personID)
	if p.status != http.StatusOK || p.body["country"] != "FR" || p.body["personId"] != personID {
		t.Fatalf("person: %d %s", p.status, p.raw)
	}
	var types []string
	for _, a := range p.body["accounts"].([]any) {
		types = append(types, a.(map[string]any)["accountType"].(string))
	}
	if strings.Join(types, ",") != "MYRENAULT,SFDC" {
		t.Errorf("accounts %v", types)
	}
	if r := f.kamereon(t, jwt, "persons/someone-else"); r.status != http.StatusForbidden || errorCode(r) != "err.func.403" {
		t.Errorf("another person: %d %s", r.status, r.raw)
	}

	l := f.kamereon(t, jwt, "accounts/"+myRenaultAccount+"/vehicles")
	links := l.body["vehicleLinks"].([]any)
	if l.status != http.StatusOK || len(links) != 1 || l.body["accountId"] != myRenaultAccount {
		t.Fatalf("vehicles: %d %s", l.status, l.raw)
	}
	link := links[0].(map[string]any)
	d := link["vehicleDetails"].(map[string]any)
	if link["vin"] != testVIN || link["brand"] != "RENAULT" || d["vin"] != testVIN || d["brand"].(map[string]any)["label"] != "RENAULT" ||
		d["model"].(map[string]any)["code"] != "R5E1VE" || d["model"].(map[string]any)["label"] != "RENAULT 5" ||
		d["energy"].(map[string]any)["code"] != "ELECX" || d["registrationNumber"] == "" || len(d["assets"].([]any)) == 0 {
		t.Errorf("link %v", link)
	}
	if r := f.kamereon(t, jwt, "accounts/"+sfdcAccount+"/vehicles"); r.status != http.StatusOK || len(r.body["vehicleLinks"].([]any)) != 0 {
		t.Errorf("CRM account: %d %s", r.status, r.raw)
	}
	if r := f.kamereon(t, jwt, "accounts/other/vehicles"); r.status != http.StatusForbidden {
		t.Errorf("other account: %d", r.status)
	}
	det := f.kamereon(t, jwt, "accounts/"+myRenaultAccount+"/vehicles/"+testVIN+"/details")
	if det.status != http.StatusOK || det.body["model"].(map[string]any)["code"] != "R5E1VE" || det.body["data"] != nil {
		t.Errorf("details: %d %s", det.status, det.raw)
	}
	if r := f.kamereon(t, jwt, "accounts/"+myRenaultAccount+"/vehicles/VF1AAAAA555777999/details"); r.status != http.StatusNotFound || errorCode(r) != "err.func.wired.notFound" {
		t.Errorf("unknown VIN: %d %s", r.status, r.raw)
	}

	// The charge is dated from the car's send, a minute ago: the next is at five.
	b := attrs(t, f.kamereon(t, jwt, carPath("battery-status")))
	if b["timestamp"] != "2026-11-13T17:59:00Z" || b["batteryLevel"] != 66.0 || b["plugStatus"] != 1.0 || b["chargingStatus"] != 1.0 ||
		b["batteryAutonomy"] != 230.0 || b["batteryCapacity"] != 0.0 || b["chargingInstantaneousPower"] != 11.0 {
		t.Errorf("battery %v", b)
	}
	// 13.6 % of 52 kWh at 11 kW: 7.07 kWh, 38.6 minutes.
	if b["chargingRemainingTime"] != 39.0 {
		t.Errorf("remaining %v", b["chargingRemainingTime"])
	}
	c := attrs(t, f.kamereon(t, jwt, carPath("cockpit")))
	if c["totalMileage"] != 6820.46 || c["timestamp"] != "2026-11-13T18:00:00Z" {
		t.Errorf("cockpit %v", c)
	}
	loc := attrs(t, f.kamereon(t, jwt, carPath("location")))
	if loc["gpsLatitude"] != 45.764 || loc["gpsLongitude"] != 4.8357 || loc["lastUpdateTime"] != "2026-11-13T17:00:00Z" {
		t.Errorf("location %v", loc)
	}
	h := attrs(t, f.kamereon(t, jwt, carPath("hvac-status")))
	if h["hvacStatus"] != "off" || h["externalTemperature"] != 12.0 || h["lastUpdateTime"] == nil {
		t.Errorf("hvac %v", h)
	}
}

func TestChargeStates(t *testing.T) {
	tests := []struct {
		name   string
		energy func(*vehicle.State)
		plug   float64
		status float64
	}{
		{"done", func(s *vehicle.State) { s.Charging = vehicle.Done }, 1, 0.2},
		{"plugged idle", func(s *vehicle.State) { s.Charging = vehicle.Idle }, 1, 0.0},
		{"unplugged", func(s *vehicle.State) { s.PluggedIn, s.Charging = false, vehicle.Idle }, 0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := clock.NewManual(time.Date(2026, 11, 13, 18, 0, 0, 0, time.UTC))
			rep := charging(clk.Now(), "R5E1VE")
			tt.energy(&rep.Energy)
			h := NewHandler([]Source{car{testVIN, "renault", rep}}, clk, DevKeys(), DefaultBehavior())
			srv := httptest.NewServer(h)
			defer srv.Close()
			f := &fixture{srv: srv, clk: clk, h: h}
			b := attrs(t, f.kamereon(t, f.session(t), carPath("battery-status")))
			if b["plugStatus"] != tt.plug || b["chargingStatus"] != tt.status || b["chargingRemainingTime"] != nil {
				t.Errorf("battery %v", b)
			}
			if b["timestamp"] != "2026-11-13T17:59:00Z" {
				t.Errorf("timestamp %v: a car that does not charge sends at a change only", b["timestamp"])
			}
		})
	}
}

func TestModelQuirks(t *testing.T) {
	t.Run("Zoe phase 1 reports watts and serves no position", func(t *testing.T) {
		f := newFixture(t, DefaultBehavior(), "X101VE")
		jwt := f.session(t)
		if b := attrs(t, f.kamereon(t, jwt, carPath("battery-status"))); b["chargingInstantaneousPower"] != 11000.0 {
			t.Errorf("power %v", b["chargingInstantaneousPower"])
		}
		l := f.kamereon(t, jwt, carPath("location"))
		if l.status != http.StatusNotImplemented || errorCode(l) != "err.tech.501" {
			t.Errorf("location: %d %s", l.status, l.raw)
		}
		if det := f.kamereon(t, jwt, "accounts/"+myRenaultAccount+"/vehicles/"+testVIN+"/details"); det.body["energy"].(map[string]any)["code"] != "ELEC" {
			t.Errorf("details %s", det.raw)
		}
	})
	t.Run("Dacia and Alpine tell their brand", func(t *testing.T) {
		clk := clock.NewManual(time.Date(2026, 11, 13, 18, 0, 0, 0, time.UTC))
		h := NewHandler([]Source{car{testVIN, "dacia", charging(clk.Now(), "XBG1VE")}}, clk, DevKeys(), DefaultBehavior())
		srv := httptest.NewServer(h)
		defer srv.Close()
		f := &fixture{srv: srv, clk: clk, h: h}
		det := f.kamereon(t, f.session(t), "accounts/"+myRenaultAccount+"/vehicles/"+testVIN+"/details")
		if det.body["brand"].(map[string]any)["label"] != "DACIA" {
			t.Errorf("details %s", det.raw)
		}
	})
}

func TestKamereonRefusals(t *testing.T) {
	f := newFixture(t, DefaultBehavior(), "R5E1VE")
	jwt := f.session(t)
	path := "persons/" + personID
	tests := []struct {
		name   string
		hdr    map[string]string
		query  url.Values
		status int
		code   string
	}{
		{"no key", map[string]string{"x-gigya-id_token": jwt}, url.Values{"country": {"FR"}}, http.StatusUnauthorized, "err.func.wired.unauthorized"},
		{"wrong key", map[string]string{"apikey": "old", "x-gigya-id_token": jwt}, url.Values{"country": {"FR"}}, http.StatusUnauthorized, "err.func.wired.unauthorized"},
		{"no token", map[string]string{"apikey": DevKamereonKey}, url.Values{"country": {"FR"}}, http.StatusUnauthorized, "err.func.wired.unauthorized"},
		{"foreign token", map[string]string{"apikey": DevKamereonKey, "x-gigya-id_token": "a.b.c"}, url.Values{"country": {"FR"}}, http.StatusUnauthorized, "err.func.wired.unauthorized"},
		{"no country", map[string]string{"apikey": DevKamereonKey, "x-gigya-id_token": jwt}, url.Values{}, http.StatusBadRequest, "err.func.wired.missingRequestParam"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := f.do(t, http.MethodGet, "/renault/kamereon/commerce/v1/"+path, tt.query, tt.hdr)
			if r.status != tt.status || errorCode(r) != tt.code {
				t.Errorf("%d %s", r.status, r.raw)
			}
			msgs := r.body["messages"].([]any)
			if msgs[0].(map[string]any)["code"] != tt.code {
				t.Errorf("messages %v", msgs)
			}
		})
	}
}

func TestQuota(t *testing.T) {
	f := newFixture(t, Behavior{PerHour: 3}, "R5E1VE")
	token := f.signIn(t) // Gigya's calls are in no quota
	jwt := f.jwt(t, token)
	for i := range 3 {
		if r := f.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusOK {
			t.Fatalf("call %d: %d", i, r.status)
		}
	}
	over := func() reply { return f.kamereon(t, f.jwt(t, token), carPath("cockpit")) }
	r := over()
	if r.status == http.StatusTooManyRequests || r.status != http.StatusForbidden || errorCode(r) != "err.func.wired.overloaded" ||
		r.body["errors"].([]any)[0].(map[string]any)["errorMessage"] != "You have reached your quota limit" {
		t.Fatalf("over: %d %s", r.status, r.raw)
	}
	// A refused call does not prolong the window: it opens again an hour after its first.
	f.clk.Advance(59 * time.Minute)
	if over().status != http.StatusForbidden {
		t.Error("answered inside the window")
	}
	f.clk.Advance(time.Minute)
	if r := over(); r.status != http.StatusOK {
		t.Errorf("after the window: %d %s", r.status, r.raw)
	}
	// A call refused for its key counts in none.
	for range 5 {
		f.do(t, http.MethodGet, "/renault/kamereon/commerce/v1/persons/"+personID, url.Values{"country": {"FR"}}, map[string]string{"apikey": "old"})
	}
	if r := over(); r.status != http.StatusOK {
		t.Errorf("refused calls counted: %d", r.status)
	}

	free := newFixture(t, Behavior{}, "R5E1VE")
	jwt = free.session(t)
	for i := range 100 {
		if r := free.kamereon(t, jwt, "persons/"+personID); r.status != http.StatusOK {
			t.Fatalf("unlimited, call %d: %d", i, r.status)
		}
	}
}

func TestNewHandlerDuplicateVIN(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic on a duplicate VIN")
		}
	}()
	c := car{testVIN, "renault", vehicle.Report{}}
	NewHandler([]Source{c, c}, clock.NewManual(time.Time{}), DevKeys(), DefaultBehavior())
}
