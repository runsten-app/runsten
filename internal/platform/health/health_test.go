package health

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

func TestHeartbeat(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC))
	h := NewHeartbeat(clk, time.Minute)
	for _, tt := range []struct {
		name    string
		advance time.Duration
		beat    bool
		want    int
	}{
		{"healthy when created", 0, false, http.StatusOK},
		{"healthy up to maxAge", time.Minute, false, http.StatusOK},
		{"stale beyond maxAge", time.Second, false, http.StatusServiceUnavailable},
		{"healthy again after a beat", 0, true, http.StatusOK},
		{"stale again", 2 * time.Minute, false, http.StatusServiceUnavailable},
	} {
		clk.Advance(tt.advance)
		if tt.beat {
			h.Beat()
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, Path, nil))
		if rec.Code != tt.want {
			t.Errorf("%s: status %d, want %d (%s)", tt.name, rec.Code, tt.want, rec.Body)
		}
	}
}

func TestProbe(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET "+Path, OK())
	up := httptest.NewServer(mux)
	defer up.Close()
	stale := httptest.NewServer(NewHeartbeat(clock.NewManual(time.Time{}), -time.Second))
	defer stale.Close()
	_, upPort, _ := net.SplitHostPort(up.Listener.Addr().String())
	closed := httptest.NewServer(OK())
	closedAddr := closed.Listener.Addr().String()
	closed.Close()

	hc := &http.Client{Timeout: 5 * time.Second}
	for _, tt := range []struct {
		name, addr, err string
	}{
		{"healthy", up.Listener.Addr().String(), ""},
		{"unspecified host probed on loopback", "0.0.0.0:" + upPort, ""},
		{"empty host probed on loopback", ":" + upPort, ""},
		{"unhealthy", stale.Listener.Addr().String(), "status 503: stale"},
		{"not listening", closedAddr, "probe"},
		{"invalid address", "8081", "address"},
	} {
		err := Probe(t.Context(), hc, tt.addr)
		switch {
		case tt.err == "" && err != nil:
			t.Errorf("%s: %v", tt.name, err)
		case tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)):
			t.Errorf("%s: error %v, want %q", tt.name, err, tt.err)
		}
	}
}
