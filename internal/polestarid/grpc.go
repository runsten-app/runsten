package polestarid

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// The gRPC services the Polestar app reads beyond the GraphQL, with the same access token
// (pypolestar's reconstruction of the Android app 5.5.0, April 2026): the battery with its
// cable and charging power on Volvo's connected-car services (C3), whose host is asked of
// a discovery service, and the charge limit on Polestar's own platform (PCCS). Spoken over
// HTTP/2 with net/http: gRPC's framing is five bytes before each message, its status in
// the trailers.
const (
	// DefaultDiscoveryURL answers the C3 host: {"c3": {"grpcHost", "grpcPort"}}.
	DefaultDiscoveryURL = "https://cnepmob.volvocars.com"
	// DefaultPCCSURL serves the charge limit.
	DefaultPCCSURL = "https://api.pccs-prod.plstr.io"
)

// The gRPC endpoints.
const (
	// Battery is GetLatestBattery: the battery, the cable, the charging power, current and
	// voltage.
	Battery Endpoint = "battery"
	// TargetSoC is GetTargetSoc: the charge limit. A Polestar 2 is not provisioned on PCCS
	// (PERMISSION_DENIED, seen by pypolestar).
	TargetSoC Endpoint = "target-soc"
)

// The methods called. The battery's service is under a path shorter than its proto
// package (pccs.vehiclestates.services.battery.v1), as pypolestar calls it on C3.
const (
	BatteryMethod   = "/services.vehiclestates.battery.BatteryService/GetLatestBattery"
	TargetSoCMethod = "/pccs.chronos.services.v1.TargetSocService/GetTargetSoc"
)

// discoveryType is the media type the discovery service is asked for, as pypolestar does.
const discoveryType = "application/volvo.cloud.cnepmob.v1+json"

// grpcHosts are the addresses the gRPC is read at: C3's discovered once, and forgotten
// when a call cannot reach it.
type grpcHosts struct {
	discovery, pccs string
	hc              *http.Client // speaks HTTP/2 only

	mu sync.Mutex
	c3 string // the discovered base URL; empty until asked
}

// h2 returns a client of hc's that speaks HTTP/2 only, as gRPC needs: over TLS, and
// without it with prior knowledge (the simulator). A transport of its own (a test's) is
// kept as it is.
func h2(hc *http.Client) *http.Client {
	out := *hc
	t, ok := hc.Transport.(*http.Transport)
	if hc.Transport == nil {
		t, ok = http.DefaultTransport.(*http.Transport)
	}
	if ok {
		t = t.Clone()
		t.Protocols = new(http.Protocols)
		t.Protocols.SetHTTP2(true)
		t.Protocols.SetUnencryptedHTTP2(true)
		out.Transport = t
	}
	return &out
}

// c3Base returns C3's base URL, asking the discovery service the first time: the host and
// port it answers, under the discovery's own scheme.
func (c *Client) c3Base(ctx context.Context) (string, error) {
	c.grpc.mu.Lock()
	base := c.grpc.c3
	c.grpc.mu.Unlock()
	if base != "" {
		return base, nil
	}
	d, err := url.Parse(c.grpc.discovery)
	if err != nil || d.Host == "" {
		return "", fmt.Errorf("discovery: no discovery URL: %q", c.grpc.discovery)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.grpc.discovery, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("discovery: %w", err)
	}
	req.Header.Set("Accept", discoveryType)
	raw, err := do(c.hc, req, "discovery", discoveryCall)
	if err != nil {
		return "", err
	}
	// Assumption: the port a number, as pypolestar reads it; a string is read too.
	var v struct {
		C3 struct {
			Host string          `json:"grpcHost"`
			Port json.RawMessage `json:"grpcPort"`
		} `json:"c3"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("discovery: %w", err)
	}
	port, err := strconv.Atoi(strings.Trim(string(v.C3.Port), `"`))
	if err != nil || port <= 0 || port > 65535 || v.C3.Host == "" || strings.ContainsAny(v.C3.Host, "/?#@") {
		return "", errors.New("discovery: no C3 host and port in the response")
	}
	base = d.Scheme + "://" + net.JoinHostPort(v.C3.Host, strconv.Itoa(port))
	c.grpc.mu.Lock()
	c.grpc.c3 = base
	c.grpc.mu.Unlock()
	return base, nil
}

// forgetC3 has the next call ask the discovery again, should C3 have moved.
func (c *Client) forgetC3() {
	c.grpc.mu.Lock()
	c.grpc.c3 = ""
	c.grpc.mu.Unlock()
}

// Battery returns GetLatestBattery's response for the car vin, as JSON (wire.go):
// {"vin", "battery": {…}}.
func (c *Client) Battery(ctx context.Context, token, vin string) ([]byte, error) {
	base, err := c.c3Base(ctx)
	if err != nil {
		return nil, err
	}
	msg, err := c.invoke(ctx, base+BatteryMethod, string(Battery), token, vin, batteryRequest(requestID(), vin), false)
	var e *APIError
	if err != nil && !errors.As(err, &e) {
		c.forgetC3()
	}
	if err != nil {
		return nil, err
	}
	return render(string(Battery), msg, batteryResponse, "battery")
}

// TargetSoC returns the first response of GetTargetSoc, a stream, for the car vin, as
// JSON: {"vin", "targetSoc": {…}, "pendingTargetSoc": {…}}.
func (c *Client) TargetSoC(ctx context.Context, token, vin string) ([]byte, error) {
	msg, err := c.invoke(ctx, strings.TrimRight(c.grpc.pccs, "/")+TargetSoCMethod, string(TargetSoC), token, vin,
		targetSoCRequest(requestID(), vin), true)
	if err != nil {
		return nil, err
	}
	return render(string(TargetSoC), msg, targetSoCResponse, "targetSoc")
}

// render writes a response's message as JSON, which must hold the field want.
func render(name string, msg []byte, m message, want string) ([]byte, error) {
	v, err := decode(msg, m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if _, ok := v[want]; !ok {
		return nil, &APIError{Endpoint: name, Status: http.StatusOK, Message: "no " + want + " in the response"}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return raw, nil
}

// requestID is a request's ID, a random UUID (version 4) as the app sends.
func requestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// invoke calls a gRPC method with the message req and returns the response's first
// message: the only one of a unary call, whose status is then read in the trailers, or the
// first of a stream, which is then left.
func (c *Client) invoke(ctx context.Context, target, name, token, vin string, req []byte, stream bool) ([]byte, error) {
	frame := make([]byte, 5, 5+len(req))
	binary.BigEndian.PutUint32(frame[1:], uint32(len(req))) //nolint:gosec // a request of a few bytes
	frame = append(frame, req...)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(frame))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	r.Header.Set("Content-Type", "application/grpc")
	r.Header.Set("TE", "trailers")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Vin", vin)
	resp, err := c.grpc.hc.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		return nil, newAPIError(name, grpcCall, resp, body)
	}
	// A status in the headers: a response without a message (trailers-only), an error.
	if s := resp.Header.Get("Grpc-Status"); s != "" {
		if err := grpcStatus(name, s, resp.Header.Get("Grpc-Message")); err != nil {
			return nil, err
		}
		return nil, &APIError{Endpoint: name, Status: resp.StatusCode, Message: "no message in the response"}
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/grpc") {
		return nil, &APIError{Endpoint: name, Status: resp.StatusCode, Message: "not a gRPC response: " + clip(ct)}
	}
	msg, err := readFrame(resp.Body)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if msg != nil && stream {
		return msg, nil
	}
	// The rest of a unary call, read up to its trailers.
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody)); err != nil {
		return nil, fmt.Errorf("%s: read: %w", name, err)
	}
	if err := grpcStatus(name, resp.Trailer.Get("Grpc-Status"), resp.Trailer.Get("Grpc-Message")); err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, &APIError{Endpoint: name, Status: resp.StatusCode, Message: "no message in the response"}
	}
	return msg, nil
}

// readFrame reads a gRPC message: a compression flag, its length, then itself. io.EOF:
// none.
func readFrame(r io.Reader) ([]byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("read: %w", err)
		}
		return nil, err //nolint:wrapcheck // io.EOF, which the caller tests
	}
	if head[0] != 0 {
		// None is asked for (no grpc-accept-encoding): a compressed message is not read.
		return nil, errors.New("read: a compressed message")
	}
	n := binary.BigEndian.Uint32(head[1:])
	if n > maxBody {
		return nil, fmt.Errorf("read: a message of %d bytes", n)
	}
	msg := make([]byte, n)
	if _, err := io.ReadFull(r, msg); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return msg, nil
}
