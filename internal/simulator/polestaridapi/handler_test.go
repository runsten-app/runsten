package polestaridapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

const vin = "LPSYCZRE5RL000004"

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

// fakeSource is a car whose state the test sets.
type fakeSource struct{ rep vehicle.Report }

func (f *fakeSource) VIN() string                       { return vin }
func (f *fakeSource) ReportAt(time.Time) vehicle.Report { return f.rep }

// parked is a Polestar 4 parked and unplugged since t0.
func parked() *fakeSource {
	st := vehicle.State{SoC: 66, OdometerKm: 9800.4, TargetSoC: 80}
	return &fakeSource{rep: vehicle.Report{
		Spec:   vehicle.Spec{Model: "Polestar 4", ModelYear: 2024, BatteryKWh: 100, UsableKWh: 94, ConsumptionKWhPer100km: 19},
		Energy: st, EnergyAt: t0, Status: st, StatusAt: t0,
	}}
}

type server struct {
	*httptest.Server
	clk *clock.Manual
}

func newServer(t *testing.T, src *fakeSource) *server {
	t.Helper()
	clk := clock.NewManual(t0)
	srv := httptest.NewServer(NewHandler([]Source{src}, clk, DefaultBehavior()))
	t.Cleanup(srv.Close)
	return &server{Server: srv, clk: clk}
}

// client follows no redirection: the code is read from the Location, as the user reads it
// from the browser's network log.
func (s *server) client() *http.Client {
	c := *s.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// reply is a response read and closed.
type reply struct {
	StatusCode int
	Header     http.Header
}

func (s *server) do(t *testing.T, req *http.Request) (reply, string) {
	t.Helper()
	resp, err := s.client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{resp.StatusCode, resp.Header}, string(b)
}

func (s *server) form(t *testing.T, path string, v url.Values) (reply, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL+path, strings.NewReader(v.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(t, req)
}

func (s *server) get(t *testing.T, path string) (reply, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s.do(t, req)
}

const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizeQuery(redirect string) string {
	return url.Values{
		"client_id": {ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {"openid profile email customer:attributes"}, "state": {"s1"},
		"code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}, "response_mode": {"query"},
	}.Encode()
}

var actionRe = regexp.MustCompile(`action:\s*"(.+?)"`)

// signIn opens the sign-in page and sends the form, as a browser does; it returns the
// response to the form.
func (s *server) signIn(t *testing.T, email, password string) (reply, string) {
	t.Helper()
	resp, page := s.get(t, Prefix+"as/authorization.oauth2?"+authorizeQuery(RedirectURI))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorization: %d %s", resp.StatusCode, page)
	}
	m := actionRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no resume path in %s", page)
	}
	return s.form(t, m[1], url.Values{"pf.username": {email}, "pf.pass": {password}})
}

func (s *server) code(t *testing.T) string {
	t.Helper()
	resp, body := s.signIn(t, DevEmail, DevPassword)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("sign-in: %d %s", resp.StatusCode, body)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got := loc.Scheme + "://" + loc.Host + loc.Path; got != RedirectURI {
		t.Fatalf("redirected to %s, want %s", got, RedirectURI)
	}
	if loc.Query().Get("state") != "s1" {
		t.Errorf("state %q, want s1", loc.Query().Get("state"))
	}
	return loc.Query().Get("code")
}

type tokens struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int    `json:"expires_in"`
	Error     string `json:"error"`
}

func (s *server) token(t *testing.T, v url.Values) (int, tokens) {
	t.Helper()
	v.Set("client_id", ClientID)
	resp, body := s.form(t, Prefix+"as/token.oauth2", v)
	var tk tokens
	if err := json.Unmarshal([]byte(body), &tk); err != nil {
		t.Fatalf("token: %v in %s", err, body)
	}
	return resp.StatusCode, tk
}

func (s *server) exchange(t *testing.T, code, v string) (int, tokens) {
	t.Helper()
	return s.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {RedirectURI}, "code_verifier": {v}})
}

func (s *server) refresh(t *testing.T, rt string) (int, tokens) {
	t.Helper()
	return s.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}})
}

func (s *server) query(t *testing.T, access, q string, vins ...string) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": q, "variables": map[string]any{"vins": vins}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL+APIPath, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	resp, b := s.do(t, req)
	var out map[string]any
	if err := json.Unmarshal([]byte(b), &out); err != nil {
		t.Fatalf("graphql: %v in %s", err, b)
	}
	return resp.StatusCode, out
}

func TestOpenIDConfigurationNamesItsEndpoints(t *testing.T) {
	s := newServer(t, parked())
	_, body := s.get(t, Prefix+".well-known/openid-configuration")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatal(err)
	}
	if want := s.URL + "/polestar-id/as/token.oauth2"; cfg["token_endpoint"] != want {
		t.Errorf("token_endpoint %v, want %s", cfg["token_endpoint"], want)
	}
	if want := s.URL + "/polestar-id/as/authorization.oauth2"; cfg["authorization_endpoint"] != want {
		t.Errorf("authorization_endpoint %v, want %s", cfg["authorization_endpoint"], want)
	}
}

func TestOnlyPolestarComsRedirectionIsAccepted(t *testing.T) {
	s := newServer(t, parked())
	for _, r := range []string{"https://www.polestar.com/", "http://localhost/callback", "polestar-explore://auth"} {
		if resp, _ := s.get(t, Prefix+"as/authorization.oauth2?"+authorizeQuery(r)); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("redirect %s: %d, want 400", r, resp.StatusCode)
		}
	}
}

func TestAWrongPasswordGivesERR001(t *testing.T) {
	s := newServer(t, parked())
	resp, page := s.signIn(t, DevEmail, "wrong")
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, `var authMessage = "ERR001";`) {
		t.Errorf("%d %s, want the page again with ERR001", resp.StatusCode, page)
	}
}

func TestTheCodeOutlivesAFailedExchange(t *testing.T) {
	s := newServer(t, parked())
	code := s.code(t)
	// polestar.com's own attempt, without our verifier: refused, and the code kept.
	if status, tk := s.exchange(t, code, "another-verifier-of-43-characters-at-least-x"); status != http.StatusBadRequest || tk.Error != "invalid_grant" {
		t.Errorf("wrong verifier: %d %q, want 400 invalid_grant", status, tk.Error)
	}
	status, tk := s.exchange(t, code, verifier)
	if status != http.StatusOK || tk.Access == "" || tk.Refresh == "" || tk.ExpiresIn != 300 {
		t.Fatalf("exchange: %d %+v", status, tk)
	}
	if status, _ := s.exchange(t, code, verifier); status != http.StatusBadRequest {
		t.Errorf("the code again: %d, want 400", status)
	}
}

func TestAnExpiredCodeIsRefused(t *testing.T) {
	s := newServer(t, parked())
	code := s.code(t)
	s.clk.Advance(2 * time.Minute)
	if status, _ := s.exchange(t, code, verifier); status != http.StatusBadRequest {
		t.Errorf("%d, want 400", status)
	}
}

func TestTheRefreshTokenRotates(t *testing.T) {
	s := newServer(t, parked())
	_, first := s.exchange(t, s.code(t), verifier)
	status, second := s.refresh(t, first.Refresh)
	if status != http.StatusOK || second.Refresh == first.Refresh {
		t.Fatalf("refresh: %d, rotated %v", status, second.Refresh != first.Refresh)
	}
	if status, tk := s.refresh(t, first.Refresh); status != http.StatusBadRequest || tk.Error != "invalid_grant" {
		t.Errorf("the previous one again: %d %q, want 400 invalid_grant", status, tk.Error)
	}
	// Using the previous one revoked nothing.
	if status, _ := s.refresh(t, second.Refresh); status != http.StatusOK {
		t.Errorf("the current one after the previous one: %d, want 200", status)
	}
}

func TestARevocationRefusesTheRefreshTokens(t *testing.T) {
	s := newServer(t, parked())
	rt := DevRefreshToken()
	if resp, _ := s.form(t, ControlPrefix+"revoke", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	if status, _ := s.refresh(t, rt); status != http.StatusBadRequest {
		t.Errorf("%d, want 400", status)
	}
}

func TestTheAccessTokenExpires(t *testing.T) {
	s := newServer(t, parked())
	_, tk := s.refresh(t, DevRefreshToken())
	if status, _ := s.query(t, tk.Access, "query { getConsumerCarsV2 { vin } }"); status != http.StatusOK {
		t.Fatalf("%d, want 200", status)
	}
	s.clk.Advance(301 * time.Second)
	if status, _ := s.query(t, tk.Access, "query { getConsumerCarsV2 { vin } }"); status != http.StatusUnauthorized {
		t.Errorf("expired: %d, want 401", status)
	}
}

func TestCarsAndTelematics(t *testing.T) {
	src := parked()
	s := newServer(t, src)
	_, tk := s.refresh(t, DevRefreshToken())
	_, out := s.query(t, tk.Access, "query { getConsumerCarsV2 { vin modelName modelYear } }")
	cars := out["data"].(map[string]any)["getConsumerCarsV2"].([]any)
	car := cars[0].(map[string]any)
	if car["vin"] != vin || car["modelName"] != "Polestar4" || car["modelYear"] != "2024" {
		t.Errorf("car %v", car)
	}

	src.rep.Energy.Charging, src.rep.Energy.ChargingPowerW, src.rep.EnergyAt = vehicle.Charging, 11000, t0.Add(time.Minute)
	s.clk.Advance(time.Minute)
	_, out = s.query(t, tk.Access, "query T($vins: [String!]!) { carTelematicsV2(vins: $vins) { battery { vin } } }", vin, "UNKNOWNVIN0000000")
	tel := out["data"].(map[string]any)["carTelematicsV2"].(map[string]any)
	battery := tel["battery"].([]any)
	if len(battery) != 1 {
		t.Fatalf("battery of %d cars, want 1 (the unknown VIN left out)", len(battery))
	}
	b := battery[0].(map[string]any)
	if b["chargingStatusV2"] != "CHARGING_STATUS_V2_CHARGING" || b["batteryChargeLevelPercentage"] != 66.0 {
		t.Errorf("battery %v", b)
	}
	if ts := b["timestamp"].(map[string]any); ts["seconds"] != strconv.FormatInt(t0.Add(time.Minute).Unix(), 10) {
		t.Errorf("battery timestamp %v, want the seconds of t0+1m in a string", ts)
	}
	odo := tel["odometer"].([]any)[0].(map[string]any)
	if odo["odometerMeters"] != 9800400.0 {
		t.Errorf("odometer %v", odo)
	}
	if h := tel["health"].([]any)[0].(map[string]any); h["serviceWarning"] != "SERVICE_WARNING_NO_WARNING" {
		t.Errorf("health %v", h)
	}
}

func TestAParkedCarUploadsItsBatteryEveryTenMinutes(t *testing.T) {
	s := newServer(t, parked())
	s.clk.Advance(25 * time.Minute)
	_, tk := s.refresh(t, DevRefreshToken())
	_, out := s.query(t, tk.Access, "query T($vins: [String!]!) { carTelematicsV2(vins: $vins) { battery { vin } } }", vin)
	b := out["data"].(map[string]any)["carTelematicsV2"].(map[string]any)["battery"].([]any)[0].(map[string]any)
	if got, want := b["timestamp"].(map[string]any)["seconds"], strconv.FormatInt(t0.Add(20*time.Minute).Unix(), 10); got != want {
		t.Errorf("seconds %v, want %s (t0+20m, its last upload)", got, want)
	}
}

func TestAnotherQueryIsAGraphQLError(t *testing.T) {
	s := newServer(t, parked())
	_, tk := s.refresh(t, DevRefreshToken())
	status, out := s.query(t, tk.Access, "query { somethingElse }")
	if status != http.StatusOK || out["errors"] == nil {
		t.Errorf("%d %v, want 200 with errors", status, out)
	}
}
