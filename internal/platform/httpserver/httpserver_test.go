package httpserver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSetsTimeouts(t *testing.T) {
	srv := New(":0", http.NotFoundHandler(), slog.New(slog.DiscardHandler))
	if srv.ReadHeaderTimeout == 0 || srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 {
		t.Fatalf("missing timeouts: %+v", srv)
	}
}

func TestServeStopsOnCancel(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	log := slog.New(slog.DiscardHandler)
	srv := New(ln.Addr().String(), h, log)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, log) }()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not stop")
	}
}

func TestRunFailsOnBadAddress(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	if err := Run(t.Context(), New("256.0.0.1:1", http.NotFoundHandler(), log), log); err == nil {
		t.Fatal("error expected")
	}
}

func TestLogRequestsRecordsStatusWithoutHeaders(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	h := LogRequests(log, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	if !strings.Contains(out, "status=418") || !strings.Contains(out, "path=/x") {
		t.Errorf("log = %q", out)
	}
	if strings.Contains(out, "secret-token") {
		t.Errorf("the token must not be logged: %q", out)
	}
}

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8090": true, "localhost:8090": true, "[::1]:8090": true,
		":8090": false, "0.0.0.0:8090": false, "192.168.1.10:8090": false, "not-an-address": false,
	} {
		if got := IsLoopback(addr); got != want {
			t.Errorf("IsLoopback(%q) = %v", addr, got)
		}
	}
}

func TestWarnIfExposed(t *testing.T) {
	for _, tt := range []struct {
		addr       string
		restricted bool
		want       string // expected level, empty for no log
	}{
		{"127.0.0.1:8081", false, ""},
		{"127.0.0.1:8081", true, ""},
		{"0.0.0.0:8081", false, "WARN"},
		{":8081", true, "INFO"},
	} {
		var buf bytes.Buffer
		WarnIfExposed(slog.New(slog.NewTextHandler(&buf, nil)), tt.addr, tt.restricted, "exposed")
		out := buf.String()
		if tt.want == "" && out != "" || tt.want != "" && !strings.Contains(out, "level="+tt.want) {
			t.Errorf("WarnIfExposed(%q, %v) logged %q, want level %q", tt.addr, tt.restricted, out, tt.want)
		}
	}
}
