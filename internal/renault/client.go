// Package renault is the client of the My Renault app's API (ADR 0050, unofficial,
// behind the flag source.renault), which reads the Renaults, Dacias and Alpines of an
// owner's account: Gigya signs the owner in, Kamereon serves the vehicles' states. It
// returns raw responses: the collector stores them as is, and Normalizer reads them
// into snapshots.
//
// No password is kept: the sign-in gives a login token, kept with the device's identity
// (gmid, ucid) which spares the account's one-time code at the next sign-in; a JWT of
// fifteen minutes is asked for with them before reading. The hosts and keys are those of
// renault-api (MIT, the library of Home Assistant's integration): Renault changed them
// once in 2026, so an instance may set others (Config).
package renault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The hosts and keys of Renault's European apps (renault-api's const.py, since
// 2026-05-20: one Gigya key for every European country).
const (
	DefaultGigyaURL    = "https://accounts.eu1.gigya.com"
	DefaultGigyaKey    = "3_VgdkgtIRH3AdHvJm-cjV2ug2EFE0lxt0IJzMC4MFqZjFpn_GYFXVdNZ19L7wZX0N"
	DefaultKamereonURL = "https://api-wired-prod-1-euw1.wrd-aws.com"
	DefaultKamereonKey = "YjkKtHmGfaceeuExUDKGxrLZGGvtVS0J"
)

// maxBody caps the size of a response read (a vehicles' list with its pictures is under
// 64 KiB).
const maxBody = 1 << 20

// jwtExpiration is the JWT's lifetime asked for, in seconds: renault-api's.
const jwtExpiration = 900

// Config is where the client finds Gigya and Kamereon, and the keys of Renault's apps.
type Config struct {
	GigyaURL, GigyaKey, KamereonURL, KamereonKey string
}

// DefaultConfig is the configuration of the real API in Europe.
func DefaultConfig() Config {
	return Config{GigyaURL: DefaultGigyaURL, GigyaKey: DefaultGigyaKey, KamereonURL: DefaultKamereonURL, KamereonKey: DefaultKamereonKey}
}

// Client calls Gigya and Kamereon. Each method makes one request, and never retries:
// the caller knows the quota and what an error means for the connection.
type Client struct {
	cfg Config
	hc  *http.Client
}

// NewClient creates a client of the API at cfg. hc must have a timeout; it must not
// follow redirects to other hosts (none are expected).
func NewClient(cfg Config, hc *http.Client) *Client {
	cfg.GigyaURL = strings.TrimRight(cfg.GigyaURL, "/")
	cfg.KamereonURL = strings.TrimRight(cfg.KamereonURL, "/")
	return &Client{cfg: cfg, hc: hc}
}

// Device is the identity Gigya gives the device that signs in, as two cookies: a device
// that completed the one-time code is not asked for it again for a while.
type Device struct {
	GMID, UCID string
}

// Session is what a sign-in gives, which the connection keeps: the login token, the
// device it was given to, and the person it signs in.
type Session struct {
	LoginToken string
	Device     Device
}

// Bootstrap asks Gigya for a device's identity, before a sign-in: the cookies gmid and
// ucid it sets.
func (c *Client) Bootstrap(ctx context.Context) (Device, error) {
	q := url.Values{"APIKey": {c.cfg.GigyaKey}, "pageURL": {"https://myr.renault.fr"}, "sdk": {"js_latest"}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.GigyaURL+"/accounts.webSdkBootstrap?"+q.Encode(), http.NoBody)
	if err != nil {
		return Device{}, fmt.Errorf("bootstrap: %w", err)
	}
	cookies, _, err := c.gigya(req, "bootstrap")
	if err != nil {
		return Device{}, err
	}
	var d Device
	for _, ck := range cookies {
		switch ck.Name {
		case "gmid":
			d.GMID = ck.Value
		case "ucid":
			d.UCID = ck.Value
		}
	}
	if d.GMID == "" || d.UCID == "" {
		return Device{}, errors.New("bootstrap: no device identity in the response")
	}
	return d, nil
}

// Login signs the owner in with their email and password, on the device d. An account
// that asks for its one-time code answers an *APIError of KindTFARequired, whose
// RegToken StartTFA takes.
func (c *Client) Login(ctx context.Context, d Device, email, password string) (Session, error) {
	form := url.Values{"ApiKey": {c.cfg.GigyaKey}, "loginID": {email}, "password": {password}}
	var resp struct {
		SessionInfo struct {
			CookieValue string `json:"cookieValue"`
		} `json:"sessionInfo"`
	}
	if err := c.gigyaPost(ctx, "accounts.login", "login", form, d, &resp); err != nil {
		return Session{}, err
	}
	if resp.SessionInfo.CookieValue == "" {
		return Session{}, errors.New("login: no login token in the response")
	}
	return Session{LoginToken: resp.SessionInfo.CookieValue, Device: d}, nil
}

// PersonID reads the person the login token signs in: Kamereon names the owner by it.
func (c *Client) PersonID(ctx context.Context, s Session) (string, error) {
	form := url.Values{"ApiKey": {c.cfg.GigyaKey}, "login_token": {s.LoginToken}}
	var resp struct {
		Data struct {
			PersonID string `json:"personId"`
		} `json:"data"`
	}
	if err := c.gigyaPost(ctx, "accounts.getAccountInfo", "account info", form, s.Device, &resp); err != nil {
		return "", err
	}
	if resp.Data.PersonID == "" {
		return "", errors.New("account info: no person in the response")
	}
	return resp.Data.PersonID, nil
}

// JWT is the token Kamereon reads with, and when it ends.
type JWT struct {
	Value     string
	ExpiresAt time.Time // zero when unreadable
}

// JWT asks Gigya for a token of fifteen minutes with the login token. A login token
// that no longer signs in answers an *APIError of KindCredentialRefused.
func (c *Client) JWT(ctx context.Context, s Session) (JWT, error) {
	form := url.Values{
		"ApiKey": {c.cfg.GigyaKey}, "login_token": {s.LoginToken},
		"fields": {"data.personId,data.gigyaDataCenter"}, "expiration": {fmt.Sprint(jwtExpiration)},
	}
	var resp struct {
		IDToken string `json:"id_token"`
	}
	if err := c.gigyaPost(ctx, "accounts.getJWT", "jwt", form, s.Device, &resp); err != nil {
		return JWT{}, err
	}
	if resp.IDToken == "" {
		return JWT{}, errors.New("jwt: no token in the response")
	}
	return JWT{Value: resp.IDToken, ExpiresAt: expOf(resp.IDToken)}, nil
}

// Account is a Kamereon account of the person: the one of type MYRENAULT holds the
// vehicles.
type Account struct {
	ID, Type, Country string
}

// Person is the owner as Kamereon knows them: their country, which every call names,
// and their accounts.
type Person struct {
	Country  string
	Accounts []Account
}

// Person reads the person's accounts. country is any the API accepts: the person's own
// comes back in the response.
func (c *Client) Person(ctx context.Context, jwt, personID, country string) (Person, error) {
	raw, err := c.kamereon(ctx, jwt, "/commerce/v1/persons/"+url.PathEscape(personID), country, "person")
	if err != nil {
		return Person{}, err
	}
	var resp struct {
		Country  string `json:"country"`
		Accounts []struct {
			AccountID   string `json:"accountId"`
			AccountType string `json:"accountType"`
			Country     string `json:"country"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Person{}, fmt.Errorf("person: %w", err)
	}
	p := Person{Country: resp.Country}
	for _, a := range resp.Accounts {
		p.Accounts = append(p.Accounts, Account{ID: a.AccountID, Type: a.AccountType, Country: a.Country})
	}
	return p, nil
}

// AccountMyRenault is the type of the accounts that hold vehicles.
const AccountMyRenault = "MYRENAULT"

// Vehicle is a vehicle of an account, as its list tells it.
type Vehicle struct {
	VIN, Brand, ModelCode, ModelLabel, Energy string
}

// Electric reports whether the vehicle runs on electricity alone (energy ELEC or ELECX).
func (v Vehicle) Electric() bool { return v.Energy == "ELEC" || v.Energy == "ELECX" }

// Vehicles lists the vehicles of an account, with what each one's details tell of its
// model; a vehicle listed without its details is listed with its VIN alone.
func (c *Client) Vehicles(ctx context.Context, jwt, accountID, country string) ([]Vehicle, error) {
	raw, err := c.kamereon(ctx, jwt, "/commerce/v1/accounts/"+url.PathEscape(accountID)+"/vehicles", country, "vehicles")
	if err != nil {
		return nil, err
	}
	var resp struct {
		VehicleLinks []struct {
			VIN     string          `json:"vin"`
			Details json.RawMessage `json:"vehicleDetails"`
		} `json:"vehicleLinks"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("vehicles: %w", err)
	}
	out := make([]Vehicle, 0, len(resp.VehicleLinks))
	for _, l := range resp.VehicleLinks {
		v := Vehicle{VIN: l.VIN}
		if len(l.Details) > 0 {
			d := detailsOf(l.Details)
			v.Brand, v.ModelCode, v.ModelLabel, v.Energy = d.Brand.Label, d.Model.Code, d.Model.Label, d.Energy.Code
			if v.VIN == "" {
				v.VIN = d.VIN
			}
		}
		if vinOf(v.VIN) {
			out = append(out, v)
		}
	}
	return out, nil
}

// detailsJSON is what a vehicle's details tell of its model.
type detailsJSON struct {
	VIN   string `json:"vin"`
	Brand struct {
		Label string `json:"label"`
	} `json:"brand"`
	Model struct {
		Code  string `json:"code"`
		Label string `json:"label"`
	} `json:"model"`
	Energy struct {
		Code string `json:"code"`
	} `json:"energy"`
}

func detailsOf(raw []byte) detailsJSON {
	var d detailsJSON
	_ = json.Unmarshal(raw, &d) // a detail unreadable tells nothing
	return d
}

// ErrUnknownEndpoint is returned, without calling the API, for an endpoint the client
// does not read.
var ErrUnknownEndpoint = errors.New("unknown endpoint")

// Fetch returns the raw response of the endpoint for vehicle vin of the account.
func (c *Client) Fetch(ctx context.Context, jwt, accountID, country, vin string, ep Endpoint) ([]byte, error) {
	path, ok := ep.path()
	if !ok {
		return nil, fmt.Errorf("%s: %w", ep, ErrUnknownEndpoint)
	}
	a := "/commerce/v1/accounts/" + url.PathEscape(accountID)
	return c.kamereon(ctx, jwt, a+strings.ReplaceAll(path, "{vin}", url.PathEscape(vin)), country, string(ep))
}

// kamereon sends a GET to Kamereon and returns its body when it is a 200 holding JSON.
func (c *Client) kamereon(ctx context.Context, jwt, path, country, name string) ([]byte, error) {
	u := c.cfg.KamereonURL + path + "?" + url.Values{"country": {country}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/vnd.api+json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("apikey", c.cfg.KamereonKey)
	req.Header.Set("x-gigya-id_token", jwt)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, withoutQuery(err))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", name, err)
	}
	// Kamereon tells its errors in the body: the status is read after it.
	if e := kamereonError(name, resp, body); e != nil {
		return nil, e
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%s: invalid JSON response", name)
	}
	return body, nil
}

// gigyaPost posts form to a Gigya method, with the device's cookies, and decodes the
// response into out when its errorCode is 0.
func (c *Client) gigyaPost(ctx context.Context, method, name string, form url.Values, d Device, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.GigyaURL+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withDevice(req, d)
	_, body, err := c.gigya(req, name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// gigyaGet sends a GET to a Gigya method with q and the device's cookies, and decodes
// the response into out when its errorCode is 0.
func (c *Client) gigyaGet(ctx context.Context, method, name string, q url.Values, d Device, out any) error {
	q.Set("APIKey", c.cfg.GigyaKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.GigyaURL+"/"+method+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	withDevice(req, d)
	_, body, err := c.gigya(req, name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// withDevice sends the device's identity as Gigya set it: two cookies of a request, never
// a browser's.
func withDevice(req *http.Request, d Device) {
	var cookies []string
	if d.GMID != "" {
		cookies = append(cookies, "gmid="+d.GMID)
	}
	if d.UCID != "" {
		cookies = append(cookies, "ucid="+d.UCID)
	}
	if len(cookies) > 0 {
		req.Header.Set("Cookie", strings.Join(cookies, "; "))
	}
}

// withoutQuery takes the query out of a transport error's URL: Gigya's GETs carry the
// login token, the assertions and the one-time code there, which must reach no log. The
// error underneath, a timeout or a cancellation, stays.
func withoutQuery(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	u, perr := url.Parse(ue.URL)
	if perr != nil {
		return &url.Error{Op: ue.Op, URL: "(url withheld)", Err: ue.Err}
	}
	u.RawQuery, u.User = "", nil
	return &url.Error{Op: ue.Op, URL: u.String(), Err: ue.Err}
}

// gigya sends req and returns the cookies the response sets and its body, when the
// body's errorCode is 0: Gigya tells its errors there, the HTTP status mirroring them.
func (c *Client) gigya(req *http.Request, name string) ([]*http.Cookie, []byte, error) {
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, withoutQuery(err))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: read: %w", name, err)
	}
	if e := gigyaError(name, resp, body); e != nil {
		return nil, nil, e
	}
	return resp.Cookies(), body, nil
}
