// Package renaultapi exposes simulated vehicles through the My Renault app's API, the
// one the app of Renault, Dacia and Alpine uses (unofficial, as renault-api and Home
// Assistant read it): Gigya signs the owner in, Kamereon serves the vehicles' states.
// The shapes are those of renault-api's fixtures; the codes and the statuses of the
// errors are those its documentation tells, the statuses being a guess where nobody has
// published them.
//
// Gigya's methods are served under GigyaPrefix and Kamereon's under KamereonPrefix. One
// development account signs in with DevEmail and DevPassword, owns every vehicle of the
// scenarios of brand renault, dacia or alpine, and may be made to ask for the one-time
// code Renault emails to some accounts (DevCode): a device that entered it is not asked
// again. The JWT Gigya issues is unsigned, its exp being what the client reads.
//
// Kamereon allows a number of calls an hour to the account (60 by default, Home
// Assistant's budget), answered by a functional error body, not a 429. Faults are set
// on demand under ControlPrefix: the one-time code asked, the login token revoked, the
// quota spent, the car's privacy mode, the account's id changed, an outage.
package renaultapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/vehicle"
)

// Prefix is the path the handler serves; GigyaPrefix and KamereonPrefix stand in for
// https://accounts.eu1.gigya.com and https://api-wired-prod-1-euw1.wrd-aws.com: runsten-api
// and the collector are pointed at the simulator's own address for each.
const (
	Prefix         = "/renault/"
	GigyaPrefix    = Prefix + "gigya/"
	KamereonPrefix = Prefix + "kamereon/"
)

// ControlPrefix sets the faults, outside the API.
const ControlPrefix = "/sim/renault/"

// The development account and keys, development only: connect-sim signs in with the
// first three, and runsten-api and the collector are given the keys.
const (
	DevEmail         = "driver@runsten.test"
	DevPassword      = "runsten-dev-password" //nolint:gosec // the simulator's, never a real one
	DevCode          = "123456"               // the one-time code, whatever the address it is sent to
	DevGigyaKey      = "runsten-dev-gigya-key"
	DevKamereonKey   = "runsten-dev-kamereon-key"
	personID         = "person-runsten-dev"
	myRenaultAccount = "account-runsten-dev"
	// movedAccount is the MYRENAULT account's id once AccountChanged moved it.
	movedAccount = "account-runsten-dev-moved"
	sfdcAccount  = "account-runsten-dev-crm"
	emailID      = "sim-email-id"
)

// Keys are the keys of Renault's apps the simulator accepts.
type Keys struct {
	Gigya, Kamereon string
}

// DevKeys returns the development keys.
func DevKeys() Keys { return Keys{Gigya: DevGigyaKey, Kamereon: DevKamereonKey} }

// Behavior groups the assumptions about the cars and the API.
type Behavior struct {
	// PerHour is the calls the account is allowed an hour (Home Assistant's budget of
	// 60: no published limit); zero or less: unlimited.
	PerHour int
	// SendEvery is how often a car sends its state while it drives or charges; parked,
	// only at a change (assumption, to measure on a real car). Zero: only at a change.
	SendEvery time.Duration
	// TFA has the account ask for its one-time code at the first sign-in of each device.
	TFA bool
}

// DefaultBehavior returns the assumptions used without explicit configuration.
func DefaultBehavior() Behavior { return Behavior{PerHour: 60, SendEvery: 5 * time.Minute} }

// Source provides the uploaded state of a simulated vehicle: Brand is renault, dacia or
// alpine, and its Spec's Trim the model's code Kamereon names (R5E1VE).
type Source interface {
	VIN() string
	Brand() string
	ReportAt(at time.Time) vehicle.Report
}

// assertion is a step of the one-time code's flow: the device it belongs to and the
// registration it completes.
type assertion struct{ gmid, regToken string }

// Handler serves the simulated API.
type Handler struct {
	clk      clock.Clock
	behavior Behavior
	keys     Keys
	byVIN    map[string]Source
	vins     []string // in their order
	mux      *http.ServeMux

	mu           sync.Mutex
	seq          int
	tfa          bool
	remembered   map[string]bool      // the devices that entered the code
	loginTokens  map[string]bool      // the live ones
	jwts         map[string]time.Time // issued, until
	regTokens    map[string]string    // registration token → device
	gigyaAsserts map[string]assertion
	phvTokens    map[string]string // code token → gigyaAssertion
	providers    map[string]string // providerAssertion → gigyaAssertion

	privacy        bool
	accountChanged bool
	overloadedTill time.Time
	outageUntil    time.Time
	windowStart    time.Time
	windowUsed     int
}

// NewHandler creates the simulated API for the vehicles srcs, read with keys and the
// simulated clock clk. Their VINs must be distinct (scenario.NewFleet checks it):
// NewHandler panics otherwise.
func NewHandler(srcs []Source, clk clock.Clock, keys Keys, behavior Behavior) *Handler {
	h := &Handler{
		clk: clk, behavior: behavior, keys: keys, byVIN: make(map[string]Source, len(srcs)), tfa: behavior.TFA,
		remembered: map[string]bool{}, loginTokens: map[string]bool{}, jwts: map[string]time.Time{},
		regTokens: map[string]string{}, gigyaAsserts: map[string]assertion{}, phvTokens: map[string]string{},
		providers: map[string]string{},
	}
	for _, src := range srcs {
		if _, dup := h.byVIN[src.VIN()]; dup {
			panic("renaultapi: duplicate VIN " + src.VIN())
		}
		h.byVIN[src.VIN()] = src
		h.vins = append(h.vins, src.VIN())
	}
	mux := http.NewServeMux()
	g := GigyaPrefix + "accounts."
	mux.HandleFunc("GET "+g+"webSdkBootstrap", h.gigya(h.bootstrap))
	mux.HandleFunc("POST "+g+"login", h.gigya(h.login))
	mux.HandleFunc("POST "+g+"getAccountInfo", h.gigya(h.accountInfo))
	mux.HandleFunc("POST "+g+"getJWT", h.gigya(h.getJWT))
	mux.HandleFunc("GET "+g+"tfa.initTFA", h.gigya(h.initTFA))
	mux.HandleFunc("GET "+g+"tfa.email.getEmails", h.gigya(h.getEmails))
	mux.HandleFunc("GET "+g+"tfa.email.sendVerificationCode", h.gigya(h.sendCode))
	mux.HandleFunc("GET "+g+"tfa.email.completeVerification", h.gigya(h.completeVerification))
	mux.HandleFunc("GET "+g+"tfa.finalizeTFA", h.gigya(h.finalizeTFA))
	k := KamereonPrefix + "commerce/v1/"
	mux.HandleFunc("GET "+k+"persons/{personId}", h.kamereon(h.person))
	mux.HandleFunc("GET "+k+"accounts/{accountId}/vehicles", h.kamereon(h.vehicles))
	mux.HandleFunc("GET "+k+"accounts/{accountId}/vehicles/{vin}/details", h.kamereon(h.details))
	mux.HandleFunc("GET "+k+"accounts/{accountId}/kamereon/kca/car-adapter/v2/cars/{vin}/battery-status", h.kamereon(h.car("battery-status")))
	for _, ep := range []string{"cockpit", "location", "hvac-status"} {
		mux.HandleFunc("GET "+k+"accounts/{accountId}/kamereon/kca/car-adapter/v1/cars/{vin}/"+ep, h.kamereon(h.car(ep)))
	}
	mux.HandleFunc("POST "+ControlPrefix+"tfa", h.setTFA)
	mux.HandleFunc("POST "+ControlPrefix+"revoke", h.setRevoke)
	mux.HandleFunc("POST "+ControlPrefix+"overloaded", h.setOverloaded)
	mux.HandleFunc("POST "+ControlPrefix+"privacy", h.setPrivacy)
	mux.HandleFunc("POST "+ControlPrefix+"account-changed", h.setAccountChanged)
	mux.HandleFunc("POST "+ControlPrefix+"outage", h.setOutage)
	mux.HandleFunc("POST "+ControlPrefix+"clear", h.setClear)
	mux.HandleFunc("GET "+ControlPrefix+"vehicles", h.listVehicles)
	h.mux = mux
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// next returns a new identifier, unique to the handler. Call it with the lock held.
func (h *Handler) next(kind string) string {
	h.seq++
	return fmt.Sprintf("sim-%s-%d", kind, h.seq)
}

// ---- Gigya ----

// gigyaReq is a Gigya call: its parameters (the form or the query), the device it
// comes from and the instant.
type gigyaReq struct {
	r   *http.Request
	now time.Time
}

func (q gigyaReq) param(name string) string { return q.r.Form.Get(name) }

// device is the identity of the device, its cookie or, on the two-factor calls, its
// parameter.
func (q gigyaReq) device() string {
	if c, err := q.r.Cookie("gmid"); err == nil && c.Value != "" {
		return c.Value
	}
	return q.param("gmid")
}

// gigya wraps a method: the form read, the application key checked (a stale one
// answers 403042 at login, as Gigya does, and 400093 elsewhere), the lock held.
func (h *Handler) gigya(fn func(w http.ResponseWriter, q gigyaReq)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			h.gigyaError(w, h.clk.Now(), http.StatusBadRequest, 400002, "Missing required parameter", "")
			return
		}
		q := gigyaReq{r: r, now: h.clk.Now()}
		key := q.param("ApiKey")
		if key == "" {
			key = q.param("APIKey")
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if key != h.keys.Gigya {
			if strings.HasSuffix(r.URL.Path, "accounts.login") {
				h.gigyaError(w, q.now, http.StatusForbidden, 403042, "invalid loginID or password", "Invalid LoginID")
			} else {
				h.gigyaError(w, q.now, http.StatusBadRequest, 400093, "Invalid ApiKey parameter", "Invalid ApiKey parameter")
			}
			return
		}
		fn(w, q)
	}
}

// gigyaEnvelope is what every Gigya response starts with.
func gigyaEnvelope(now time.Time, status, code int, reason string) map[string]any {
	return map[string]any{
		"callId": "sim-call", "errorCode": code, "apiVersion": 2, "statusCode": status,
		"statusReason": reason, "time": now.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
}

func (h *Handler) gigyaOK(w http.ResponseWriter, now time.Time, extra map[string]any) {
	v := gigyaEnvelope(now, http.StatusOK, 0, "OK")
	for k, e := range extra {
		v[k] = e
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) gigyaError(w http.ResponseWriter, now time.Time, status, code int, details, message string) {
	h.gigyaErrorWith(w, now, status, code, details, message, nil)
}

func (h *Handler) gigyaErrorWith(w http.ResponseWriter, now time.Time, status, code int, details, message string, extra map[string]any) {
	if message == "" {
		message = details
	}
	v := gigyaEnvelope(now, status, code, http.StatusText(status))
	v["errorDetails"], v["errorMessage"] = details, message
	for k, e := range extra {
		v[k] = e
	}
	writeJSON(w, status, v)
}

func (h *Handler) unauthorizedUser(w http.ResponseWriter, now time.Time) {
	h.gigyaError(w, now, http.StatusForbidden, 403005, "Unauthorized user", "")
}

// bootstrap gives the device its identity: the cookies gmid and ucid.
func (h *Handler) bootstrap(w http.ResponseWriter, q gigyaReq) {
	// The client reads them off the response and sends them back: no browser, no jar.
	http.SetCookie(w, &http.Cookie{Name: "gmid", Value: h.next("gmid"), Path: "/"}) //nolint:gosec // the device's identity, as Gigya gives it
	http.SetCookie(w, &http.Cookie{Name: "ucid", Value: h.next("ucid"), Path: "/"}) //nolint:gosec // idem
	h.gigyaOK(w, q.now, nil)
}

// login signs in with the email and the password; a device that has not entered the
// account's code is refused with 403101 and a registration token when the account asks
// for it.
func (h *Handler) login(w http.ResponseWriter, q gigyaReq) {
	if !strings.EqualFold(q.param("loginID"), DevEmail) || q.param("password") != DevPassword {
		h.gigyaError(w, q.now, http.StatusForbidden, 403042, "invalid loginID or password", "Invalid LoginID")
		return
	}
	gmid := q.device()
	if h.tfa && !h.remembered[gmid] {
		reg := h.next("reg")
		h.regTokens[reg] = gmid
		h.gigyaErrorWith(w, q.now, http.StatusForbidden, 403101, "Pending Two-Factor Authentication",
			"Account Pending TFA Verification", map[string]any{"regToken": reg})
		return
	}
	token := h.next("login")
	h.loginTokens[token] = true
	h.gigyaOK(w, q.now, map[string]any{
		"UID": personID, "isActive": true, "isRegistered": true, "isVerified": true, "loginProvider": "site",
		"profile":     map[string]any{"email": DevEmail},
		"sessionInfo": map[string]any{"cookieName": "glt_" + h.keys.Gigya, "cookieValue": token},
	})
}

// accountInfo gives the person the login token signs in.
func (h *Handler) accountInfo(w http.ResponseWriter, q gigyaReq) {
	if !h.loginTokens[q.param("login_token")] {
		h.unauthorizedUser(w, q.now)
		return
	}
	h.gigyaOK(w, q.now, map[string]any{
		"UID": personID, "data": map[string]any{"personId": personID, "gigyaDataCenter": "eu1"},
		"emails":  map[string]any{"verified": []string{DevEmail}, "unverified": []string{}},
		"profile": map[string]any{"email": DevEmail}, "isActive": true, "isRegistered": true, "isVerified": true,
	})
}

// getJWT issues a JWT of the lifetime asked for (900 s without), unsigned: its exp is
// what the client reads.
func (h *Handler) getJWT(w http.ResponseWriter, q gigyaReq) {
	if !h.loginTokens[q.param("login_token")] {
		h.unauthorizedUser(w, q.now)
		return
	}
	life := 900
	if n, err := strconv.Atoi(q.param("expiration")); err == nil && n > 0 {
		life = n
	}
	exp := q.now.Add(time.Duration(life) * time.Second).Truncate(time.Second)
	jwt := h.jwt(q.now, exp)
	h.jwts[jwt] = exp
	h.gigyaOK(w, q.now, map[string]any{"ignoredFields": "", "id_token": jwt})
}

// jwt writes a JWT valid until exp.
func (h *Handler) jwt(now, exp time.Time) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v) //nolint:errchkjson // maps of strings and numbers
		return base64.RawURLEncoding.EncodeToString(b)
	}
	head := enc(map[string]any{"alg": "none", "typ": "JWT"})
	claims := enc(map[string]any{
		"iss": "runsten-simulator", "sub": personID, "iat": now.Unix(), "exp": exp.Unix(), "jti": h.next("jwt"),
		"data": map[string]any{"personId": personID, "gigyaDataCenter": "eu1"},
	})
	return head + "." + claims + ".sim"
}

// initTFA opens the one-time code's registration: the assertion of the device's.
func (h *Handler) initTFA(w http.ResponseWriter, q gigyaReq) {
	reg := q.param("regToken")
	gmid, ok := h.regTokens[reg]
	if !ok {
		h.gigyaError(w, q.now, http.StatusForbidden, 403007, "Invalid or expired registration token", "")
		return
	}
	if d := q.device(); d != "" {
		gmid = d
	}
	a := h.next("assertion")
	h.gigyaAsserts[a] = assertion{gmid: gmid, regToken: reg}
	h.gigyaOK(w, q.now, map[string]any{"gigyaAssertion": a})
}

// asserted reads the assertion of a call, or answers 403005.
func (h *Handler) asserted(w http.ResponseWriter, q gigyaReq) (string, assertion, bool) {
	name := q.param("gigyaAssertion")
	a, ok := h.gigyaAsserts[name]
	if !ok {
		h.unauthorizedUser(w, q.now)
	}
	return name, a, ok
}

// getEmails lists the addresses the code may be sent to: the account's, masked.
func (h *Handler) getEmails(w http.ResponseWriter, q gigyaReq) {
	if _, _, ok := h.asserted(w, q); !ok {
		return
	}
	h.gigyaOK(w, q.now, map[string]any{"emails": []map[string]any{{
		"id": emailID, "obfuscated": mask(DevEmail), "lastVerification": q.now.UTC().Format("2006-01-02T15:04:05.000Z"),
	}}})
}

// mask hides an address as Gigya does: its first letter, then stars.
func mask(email string) string {
	local, domain, _ := strings.Cut(email, "@")
	return local[:1] + "***@" + domain
}

// sendCode "emails" the code: the simulator sends nothing, DevCode is the one.
func (h *Handler) sendCode(w http.ResponseWriter, q gigyaReq) {
	name, _, ok := h.asserted(w, q)
	if !ok {
		return
	}
	if q.param("emailID") != emailID {
		h.gigyaError(w, q.now, http.StatusBadRequest, 400006, "Invalid parameter value", "")
		return
	}
	phv := h.next("phv")
	h.phvTokens[phv] = name
	h.gigyaOK(w, q.now, map[string]any{"phvToken": phv})
}

// completeVerification checks the code: a wrong one leaves the challenge open.
func (h *Handler) completeVerification(w http.ResponseWriter, q gigyaReq) {
	name, _, ok := h.asserted(w, q)
	if !ok {
		return
	}
	if h.phvTokens[q.param("phvToken")] != name {
		h.gigyaError(w, q.now, http.StatusBadRequest, 400006, "Invalid parameter value", "")
		return
	}
	if q.param("code") != DevCode {
		h.gigyaError(w, q.now, http.StatusBadRequest, 400006, "Wrong verification code", "Invalid parameter value")
		return
	}
	prov := h.next("provider")
	h.providers[prov] = name
	h.gigyaOK(w, q.now, map[string]any{"providerAssertion": prov})
}

// finalizeTFA registers the device: it is not asked for the code again (tempDevice or
// not: the simulator remembers it until the fault tfa forgets it).
func (h *Handler) finalizeTFA(w http.ResponseWriter, q gigyaReq) {
	name, a, ok := h.asserted(w, q)
	if !ok {
		return
	}
	if h.providers[q.param("providerAssertion")] != name || q.param("regToken") != a.regToken {
		h.unauthorizedUser(w, q.now)
		return
	}
	h.remembered[a.gmid] = true
	h.gigyaOK(w, q.now, nil)
}

// ---- Kamereon ----

// kamereonReq is a Kamereon call, once its key, its JWT, its country and the quota
// have passed.
type kamereonReq struct {
	r   *http.Request
	now time.Time
}

// kamereon wraps a read: an outage first (a gateway's, before any key), then the key and
// the JWT (401), the country (400), the quota; then fn, whose answer is the status and
// the JSON body.
func (h *Handler) kamereon(fn func(q kamereonReq) (int, any)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := h.clk.Now()
		h.mu.Lock()
		defer h.mu.Unlock()
		if now.Before(h.outageUntil) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>\n  <head>\n    <title>502 Bad Gateway</title>\n  </head>\n  <body>\n    <center>\n      <h1>502 Bad Gateway</h1>\n    </center>\n  </body>\n</html>\n"))
			return
		}
		exp, live := h.jwts[r.Header.Get("x-gigya-id_token")]
		switch {
		case r.Header.Get("apikey") != h.keys.Kamereon || !live || !now.Before(exp):
			writeJSON(w, http.StatusUnauthorized, kamereonError("FUNCTIONAL", "err.func.wired.unauthorized", "The access is unauthorized"))
			return
		case r.URL.Query().Get("country") == "":
			writeJSON(w, http.StatusBadRequest, kamereonError("FUNCTIONAL", "err.func.wired.missingRequestParam",
				"Required request parameter 'country' for method parameter type String is not present"))
			return
		case !h.count(now):
			writeJSON(w, http.StatusForbidden, kamereonError("FUNCTIONAL", "err.func.wired.overloaded", "You have reached your quota limit"))
			return
		}
		status, body := fn(kamereonReq{r: r, now: now})
		writeJSON(w, status, body)
	}
}

// kamereonError is the body of an error, as the real API writes it: the code is in the
// message and in the error.
func kamereonError(typ, code, message string) map[string]any {
	return map[string]any{
		"type":     typ,
		"messages": []map[string]any{{"code": code, "message": message}},
		"errors":   []map[string]any{{"errorCode": code, "errorMessage": message}},
	}
}

// count counts the call in the account's window and reports whether it goes on: a
// window opens at the first call past the previous one's end, and a refused call does
// not prolong it.
func (h *Handler) count(now time.Time) bool {
	if now.Before(h.overloadedTill) {
		return false
	}
	if h.behavior.PerHour <= 0 {
		return true
	}
	if h.windowStart.IsZero() || !now.Before(h.windowStart.Add(time.Hour)) {
		h.windowStart, h.windowUsed = now, 0
	}
	if h.windowUsed >= h.behavior.PerHour {
		return false
	}
	h.windowUsed++
	return true
}

// person answers the person the JWT signs in, with their two accounts.
func (h *Handler) person(q kamereonReq) (int, any) {
	if q.r.PathValue("personId") != personID {
		return http.StatusForbidden, accessDenied()
	}
	country := countryOf(q.r)
	account := func(id, typ string) map[string]any {
		return map[string]any{
			"accountId": id, "accountType": typ, "accountStatus": "ACTIVE", "country": country,
			"personId": personID, "relationType": "OWNER",
		}
	}
	return http.StatusOK, map[string]any{
		"personId": personID, "type": "I", "country": country, "locale": "fr-" + country,
		"idp":    map[string]any{"idpType": "GIGYA", "idpStatus": "ACTIVE", "login": DevEmail, "loginType": "EMAIL"},
		"emails": []map[string]any{{"emailType": "MAIN", "emailValue": DevEmail, "validityFlag": true}},
		"accounts": []map[string]any{
			account(h.myRenault(), "MYRENAULT"), account(sfdcAccount, "SFDC"),
		},
	}
}

// myRenault is the MYRENAULT account's id, moved by AccountChanged. h.mu is held.
func (h *Handler) myRenault() string {
	if h.accountChanged {
		return movedAccount
	}
	return myRenaultAccount
}

// countryOf is the country asked for, which a person answers with as their own.
func countryOf(r *http.Request) string { return strings.ToUpper(r.URL.Query().Get("country")) }

func accessDenied() map[string]any {
	return kamereonError("FUNCTIONAL", "err.func.403",
		`{"errors":[{"status":"403","code":"security.access","detail":"Access is denied for this resource"}]}`)
}

// vehicles lists the vehicles of the MYRENAULT account; the CRM's holds none.
func (h *Handler) vehicles(q kamereonReq) (int, any) {
	account := q.r.PathValue("accountId")
	if account != h.myRenault() && account != sfdcAccount {
		return http.StatusForbidden, accessDenied()
	}
	links := []map[string]any{}
	if account == h.myRenault() {
		for _, vin := range h.vins {
			links = append(links, h.link(vin, q.now))
		}
	}
	return http.StatusOK, map[string]any{"accountId": account, "country": countryOf(q.r), "vehicleLinks": links}
}

// details answers one vehicle's details, the object a link carries at its top level.
func (h *Handler) details(q kamereonReq) (int, any) {
	src, status, body := h.owned(q)
	if src == nil {
		return status, body
	}
	return http.StatusOK, detailsOf(src, q.now)
}

// owned finds the vehicle of a call of the MYRENAULT account, or the error to answer: a
// call by the account's id before it moved is told err.func.not.connected.
func (h *Handler) owned(q kamereonReq) (Source, int, any) {
	switch account := q.r.PathValue("accountId"); {
	case h.accountChanged && account == myRenaultAccount:
		return nil, http.StatusForbidden, kamereonError("FUNCTIONAL", "err.func.not.connected", "Access is denied for this resource")
	case account != h.myRenault():
		return nil, http.StatusForbidden, accessDenied()
	}
	src := h.byVIN[q.r.PathValue("vin")]
	if src == nil {
		return nil, http.StatusNotFound, kamereonError("FUNCTIONAL", "err.func.wired.notFound", "Resource not found")
	}
	return src, 0, nil
}

// car answers a read of the car adapter, from the car's report: the faults first (the
// car's privacy mode on its position), then the endpoints the
// model does not serve.
func (h *Handler) car(endpoint string) func(q kamereonReq) (int, any) {
	return func(q kamereonReq) (int, any) {
		src, status, body := h.owned(q)
		if src == nil {
			return status, body
		}
		rep := src.ReportAt(q.now)
		switch {
		case h.privacy && endpoint == "location":
			return http.StatusForbidden, kamereonError("FUNCTIONAL", "err.func.privacy.on", "Privacy mode currently ON")
		case !served(rep.Spec.Trim, endpoint):
			return http.StatusNotImplemented, kamereonError("TECHNICAL", "err.tech.501", "This feature is not technically supported by this gateway")
		}
		return http.StatusOK, h.render(src.VIN(), endpoint, rep, q.now)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the header is already sent: nothing more to do
}
