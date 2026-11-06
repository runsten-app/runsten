// Package bmw is the client of BMW CarData (ADR 0045), the access BMW gives the owner of a
// BMW or a Mini to their car's data, with a client ID the owner creates in the MyBMW or
// MyMINI portal: the OAuth device flow and its tokens, the REST API (the account's
// vehicles, their basic data, a container's last values, the charging history), and the
// MQTT stream the car pushes its changes to (Stream). It returns raw responses: the
// collector stores them as is, and Normalizer reads them into snapshots.
//
// None of it is checked on a real car yet: the shapes are those of BMW's OpenAPI
// description, of its guides and of the free clients, marked "assumed" where they guess.
package bmw

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Source names CarData among the sources of readings, and the provider of its
// connections.
const Source = "bmw"

// The addresses of the real services. Another host serving the same paths (the
// simulator) is given to NewClient and NewSubscription instead.
const (
	DefaultAuthURL   = "https://customer.bmwgroup.com/gcdm/oauth"
	DefaultAPIURL    = "https://api-cardata.bmwgroup.com"
	DefaultStreamURL = "tls://customer.streaming-cardata.bmwgroup.com:9000"
)

// scopes are those the device flow asks for: the API's and the stream's, which the owner
// subscribed to in the portal before.
const scopes = "authenticate_user openid cardata:streaming:read cardata:api:read"

// The device grant (RFC 8628).
const deviceGrant = "urn:ietf:params:oauth:grant-type:device_code"

// apiVersion is the API's version, sent in x-version with every call.
const apiVersion = "v1"

// maxBody caps the size of a response read (a charging history of 30 days is a few KiB).
const maxBody = 1 << 20

// MaxImage caps the size of the vehicle's picture, as the relay of the vendors' others.
const MaxImage = 4 << 20

// Endpoint is a kind of response read for a vehicle.
type Endpoint string

// Endpoints.
const (
	// Stream is what the stream pushed of the vehicle, its last value of each
	// descriptor, as a TelematicData response: read without a call.
	Stream Endpoint = "stream"
	// TelematicData is the container's last values, read from the API.
	TelematicData Endpoint = "telematic"
	// BasicData is the vehicle's model, from the API.
	BasicData Endpoint = "basic-data"
	// ChargingHistory is the charging sessions of the last 30 days, from the API.
	ChargingHistory Endpoint = "charging-history"
	// Image is BMW's picture of the vehicle, from the API, kept as a Picture's JSON.
	Image Endpoint = "image"
)

// Hosts are the addresses of the authentication and of the API, without a trailing slash
// needed.
type Hosts struct {
	Auth string // DefaultAuthURL: the device code is its /device/code, the tokens its /token
	API  string // DefaultAPIURL
}

// Tokens are what the device flow and a refresh give.
type Tokens struct {
	Access string
	// ID is the stream's password.
	ID string
	// Refresh is the refresh token to keep: a new one at each refresh (BMW's guide), the
	// one given only when none came back. Keep it before using the others: the one given
	// may no longer refresh.
	Refresh   string
	ExpiresIn time.Duration
	// GCID is the BMW account's identifier: the stream's username and its topics' root.
	GCID string
	// Scope is the scopes granted, as the token endpoint writes them.
	Scope string
}

// DeviceAuthorization is a device code given: the owner enters UserCode at
// VerificationURI, while the client polls with DeviceCode every Interval.
type DeviceAuthorization struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn, Interval     time.Duration
}

// Mapping is a vehicle of the account; Primary when the account is its primary user, the
// only one CarData reads it for.
type Mapping struct {
	VIN     string
	Primary bool
}

// Container is a set of descriptors the API reads together.
type Container struct {
	ID, Name string
}

// Client calls CarData. Each method makes one request, and never retries: the caller
// knows what an error means for the connection.
type Client struct {
	auth, api string
	hc        *http.Client
}

// NewClient creates a client. hc must have a timeout.
func NewClient(h Hosts, hc *http.Client) *Client {
	return &Client{auth: strings.TrimRight(h.Auth, "/"), api: strings.TrimRight(h.API, "/"), hc: hc}
}

// NewVerifier returns a PKCE verifier (RFC 7636) and its S256 challenge.
func NewVerifier() (verifier, challenge string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// DeviceCode starts the device flow of the owner's client ID, with the challenge of a
// PKCE verifier (NewVerifier).
func (c *Client) DeviceCode(ctx context.Context, clientID, challenge string) (DeviceAuthorization, error) {
	raw, err := c.post(ctx, "device_code", c.auth+"/device/code", url.Values{
		"client_id":             {clientID},
		"response_type":         {"device_code"},
		"scope":                 {scopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	})
	if err != nil {
		return DeviceAuthorization{}, err
	}
	var resp struct {
		DeviceCode              string  `json:"device_code"`
		UserCode                string  `json:"user_code"`
		VerificationURI         string  `json:"verification_uri"`
		VerificationURIComplete string  `json:"verification_uri_complete"`
		ExpiresIn               float64 `json:"expires_in"`
		Interval                float64 `json:"interval"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return DeviceAuthorization{}, fmt.Errorf("device_code: %w", err)
	}
	if resp.DeviceCode == "" || resp.UserCode == "" || resp.VerificationURI == "" {
		return DeviceAuthorization{}, errors.New("device_code: no device code in the response")
	}
	d := DeviceAuthorization{
		DeviceCode: resp.DeviceCode, UserCode: resp.UserCode,
		VerificationURI: resp.VerificationURI, VerificationURIComplete: resp.VerificationURIComplete,
		ExpiresIn: seconds(resp.ExpiresIn), Interval: seconds(resp.Interval),
	}
	if d.Interval <= 0 {
		d.Interval = 5 * time.Second // RFC 8628 §3.2's default
	}
	return d, nil
}

// PollToken asks for the tokens of a device code, once. Until the owner approved it, the
// error is an *APIError of KindPending (or KindSlowDown: poll less often); KindExpired
// and KindDenied end the flow.
func (c *Client) PollToken(ctx context.Context, clientID, deviceCode, verifier string) (Tokens, error) {
	return c.tokens(ctx, url.Values{
		"grant_type":    {deviceGrant},
		"client_id":     {clientID},
		"device_code":   {deviceCode},
		"code_verifier": {verifier},
	}, "")
}

// Refresh exchanges the refresh token for new tokens, a new refresh token among them.
func (c *Client) Refresh(ctx context.Context, clientID, refreshToken string) (Tokens, error) {
	return c.tokens(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}, refreshToken)
}

func (c *Client) tokens(ctx context.Context, form url.Values, refreshToken string) (Tokens, error) {
	raw, err := c.post(ctx, "token", c.auth+"/token", form)
	if err != nil {
		return Tokens{}, err
	}
	var resp struct {
		AccessToken  string  `json:"access_token"`
		IDToken      string  `json:"id_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
		TokenType    string  `json:"token_type"`
		Scope        string  `json:"scope"`
		GCID         string  `json:"gcid"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Tokens{}, fmt.Errorf("token: %w", err)
	}
	if resp.AccessToken == "" || resp.ExpiresIn <= 0 || !strings.EqualFold(resp.TokenType, "Bearer") {
		return Tokens{}, errors.New("token: no bearer token with a lifetime in the response")
	}
	t := Tokens{
		Access: resp.AccessToken, ID: resp.IDToken, Refresh: resp.RefreshToken,
		ExpiresIn: seconds(resp.ExpiresIn), GCID: resp.GCID, Scope: resp.Scope,
	}
	if t.Refresh == "" {
		t.Refresh = refreshToken
	}
	if t.Refresh == "" {
		return Tokens{}, errors.New("token: no refresh token in the response")
	}
	return t, nil
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// Mappings lists the account's vehicles. The response is assumed a bare array of
// mappings, BMW's description giving only the item; an object holding the array is read
// too, as the free clients do.
func (c *Client) Mappings(ctx context.Context, token string) ([]Mapping, error) {
	raw, err := c.get(ctx, token, "mappings", "/customers/vehicles/mappings")
	if err != nil {
		return nil, err
	}
	type mapping struct {
		VIN         string `json:"vin"`
		MappingType string `json:"mappingType"`
	}
	var list []mapping
	if err := json.Unmarshal(raw, &list); err != nil {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return nil, fmt.Errorf("mappings: %w", err)
		}
		for _, v := range obj {
			if json.Unmarshal(v, &list) == nil {
				break
			}
		}
	}
	out := []Mapping{}
	for _, m := range list {
		if m.VIN != "" {
			out = append(out, Mapping{VIN: m.VIN, Primary: strings.EqualFold(m.MappingType, "PRIMARY")})
		}
	}
	return out, nil
}

// BasicData returns the raw response of the vehicle's basic data: its brand, model,
// series and propulsion.
func (c *Client) BasicData(ctx context.Context, token, vin string) ([]byte, error) {
	return c.get(ctx, token, string(BasicData), "/customers/vehicles/"+url.PathEscape(vin)+"/basicData")
}

// TelematicData returns the raw response of the container's last values for the vehicle:
// {"telematicData": {descriptor: {value, unit, timestamp}}}.
func (c *Client) TelematicData(ctx context.Context, token, vin, containerID string) ([]byte, error) {
	q := url.Values{"containerId": {containerID}}
	return c.get(ctx, token, string(TelematicData), "/customers/vehicles/"+url.PathEscape(vin)+"/telematicData?"+q.Encode())
}

// ChargingHistory returns the raw response of the vehicle's charging sessions started
// within [from, to): one page, BMW keeping about 30 days.
func (c *Client) ChargingHistory(ctx context.Context, token, vin string, from, to time.Time) ([]byte, error) {
	q := url.Values{"from": {from.UTC().Format(time.RFC3339)}, "to": {to.UTC().Format(time.RFC3339)}}
	return c.get(ctx, token, string(ChargingHistory), "/customers/vehicles/"+url.PathEscape(vin)+"/chargingHistory?"+q.Encode())
}

// Picture is the vehicle's picture as Image stores it: the API answers the image itself,
// which the collector's store, of JSON responses, keeps in base64.
type Picture struct {
	// Type is the image's media type, sniffed from its bytes: image/png, image/jpeg or
	// image/webp.
	Type string `json:"type"`
	Data []byte `json:"image"`
}

// PictureError is an answer of Image that holds no picture Runsten keeps: not an image
// of a known type, larger than MaxImage, or unreadable. BMW answered, with Status: the
// vehicle gives no picture, not a failure of the API.
type PictureError struct {
	Status int
	Err    error
}

func (e *PictureError) Error() string { return e.Err.Error() }
func (e *PictureError) Unwrap() error { return e.Err }

// pictureTypes are the media types of a picture kept.
var pictureTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

// Image returns the raw response of the vehicle's picture: a Picture's JSON, the picture
// scaled down to PictureWidth when wider (fit), so that what is stored every day stays
// small. Assumed: one picture from the outside, within MaxImage (BMW's OpenAPI tells
// neither the size nor the type, */*).
func (c *Client) Image(ctx context.Context, token, vin string) ([]byte, error) {
	name := string(Image)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+"/customers/vehicles/"+url.PathEscape(vin)+"/image", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-version", apiVersion)
	req.Header.Set("Accept", "image/png, image/jpeg, image/webp, application/json;q=0.5")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxImage+1))
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, newAPIError(name, apiCall, resp.StatusCode, body)
	}
	if len(body) > MaxImage {
		return nil, &PictureError{Status: resp.StatusCode, Err: fmt.Errorf("%s: larger than %d bytes", name, MaxImage)}
	}
	mt := http.DetectContentType(body)
	if !pictureTypes[mt] {
		return nil, &PictureError{Status: resp.StatusCode, Err: fmt.Errorf("%s: not an image: %s", name, mt)}
	}
	body, err = fit(body, mt)
	if err != nil {
		return nil, &PictureError{Status: resp.StatusCode, Err: fmt.Errorf("%s: %w", name, err)}
	}
	raw, err := json.Marshal(Picture{Type: mt, Data: body})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return raw, nil
}

// DecodePicture reads an Image response.
func DecodePicture(raw []byte) (Picture, error) {
	var p Picture
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("%s: %w", Image, err)
	}
	if !pictureTypes[p.Type] || len(p.Data) == 0 {
		return p, fmt.Errorf("%s: no image of a known type", Image)
	}
	return p, nil
}

// Containers lists the account's containers.
func (c *Client) Containers(ctx context.Context, token string) ([]Container, error) {
	raw, err := c.get(ctx, token, "containers", "/customers/containers")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Containers []struct {
			ID   string `json:"containerId"`
			Name string `json:"name"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("containers: %w", err)
	}
	out := make([]Container, 0, len(resp.Containers))
	for _, ct := range resp.Containers {
		out = append(out, Container{ID: ct.ID, Name: ct.Name})
	}
	return out, nil
}

// CreateContainer creates a container of the descriptors.
func (c *Client) CreateContainer(ctx context.Context, token, name, purpose string, descriptors []string) (Container, error) {
	body, err := json.Marshal(map[string]any{"name": name, "purpose": purpose, "technicalDescriptors": descriptors})
	if err != nil {
		return Container{}, fmt.Errorf("create container: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api+"/customers/containers", strings.NewReader(string(body)))
	if err != nil {
		return Container{}, fmt.Errorf("create container: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	raw, err := c.do(req, token, "create container")
	if err != nil {
		return Container{}, err
	}
	var resp struct {
		ID   string `json:"containerId"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Container{}, fmt.Errorf("create container: %w", err)
	}
	if resp.ID == "" {
		return Container{}, errors.New("create container: no containerId in the response")
	}
	return Container{ID: resp.ID, Name: resp.Name}, nil
}

// ContainerName is the name of the container Runsten creates for itself.
const ContainerName = "runsten"

// EnsureContainer returns the ID of the account's container named ContainerName,
// created with Descriptors when there is none: 1 or 2 calls.
func (c *Client) EnsureContainer(ctx context.Context, token string) (string, error) {
	cts, err := c.Containers(ctx, token)
	if err != nil {
		return "", err
	}
	for _, ct := range cts {
		if ct.Name == ContainerName {
			return ct.ID, nil
		}
	}
	ct, err := c.CreateContainer(ctx, token, ContainerName, "Runsten, a data logger of the owner's vehicles", Descriptors())
	if err != nil {
		return "", err
	}
	return ct.ID, nil
}

func (c *Client) get(ctx context.Context, token, name, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return c.do(req, token, name)
}

// do sends a call of the API.
func (c *Client) do(req *http.Request, token, name string) ([]byte, error) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-version", apiVersion)
	return send(c.hc, req, name, apiCall)
}

// post sends a form to the authentication.
func (c *Client) post(ctx context.Context, name, endpoint string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return send(c.hc, req, name, authCall)
}

// send sends req and returns its body when it is a 2xx holding JSON.
func send(hc *http.Client, req *http.Request, name string, k call) ([]byte, error) {
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, newAPIError(name, k, resp.StatusCode, body)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%s: invalid JSON response", name)
	}
	return body, nil
}
