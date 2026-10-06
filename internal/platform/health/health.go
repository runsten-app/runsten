// Package health serves and probes the /healthz route of the Runsten binaries.
//
// Their images are distroless: no shell, no curl. The container healthcheck runs the
// binary itself (a healthcheck subcommand), which probes the route with Probe.
package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"runsten/internal/platform/clock"
)

// Path is the route of the health endpoint.
const Path = "/healthz"

// OK answers 200 as long as the server serves requests.
func OK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
}

// Heartbeat reports a loop healthy while it beats: it serves 200 until maxAge has
// elapsed since the last beat, then 503. It starts beating when created, so that a
// process is healthy while it starts.
type Heartbeat struct {
	clk    clock.Clock
	maxAge time.Duration

	mu   sync.Mutex
	last time.Time
}

// NewHeartbeat creates a heartbeat whose first beat is now.
func NewHeartbeat(clk clock.Clock, maxAge time.Duration) *Heartbeat {
	return &Heartbeat{clk: clk, maxAge: maxAge, last: clk.Now()}
}

// Beat records that the loop made progress.
func (h *Heartbeat) Beat() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last = h.clk.Now()
}

// ServeHTTP answers 200 if the last beat is recent enough, 503 otherwise.
func (h *Heartbeat) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	age := h.clk.Now().Sub(h.last)
	h.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if age > h.maxAge {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, "stale: last beat %s ago\n", age.Round(time.Second))
		return
	}
	_, _ = io.WriteString(w, "ok\n")
}

// Probe requests the health route of the server listening on addr (host:port) and
// fails unless it answers 200. An unspecified host (":8081", "0.0.0.0:8081") is
// probed on the loopback interface: the probe runs next to the server, in the same
// container.
func Probe(ctx context.Context, hc *http.Client, addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, port) + Path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("probe %s: %w", url, err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("probe %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe %s: status %d: %s", url, resp.StatusCode, body)
	}
	return nil
}
