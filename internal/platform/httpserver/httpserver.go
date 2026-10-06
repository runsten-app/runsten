// Package httpserver provides an HTTP server with safe settings (explicit timeouts,
// graceful shutdown) and a request logging middleware.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Default server settings. They bound slow or abusive connections (gosec G112).
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
	maxHeaderBytes    = 64 << 10
)

// New creates an HTTP server with explicit timeouts.
func New(addr string, h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// Run listens on the server address until ctx is canceled, then shuts the server
// down gracefully.
func Run(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	}
	return Serve(ctx, srv, ln, log)
}

// Serve serves connections from ln until ctx is canceled, then shuts the server
// down gracefully.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, log *slog.Logger) error {
	errc := make(chan error, 1)
	go func() {
		log.Info("HTTP server started", "addr", ln.Addr().String())
		errc <- srv.Serve(ln)
	}()

	select {
	case err := <-errc:
		return fmt.Errorf("HTTP server: %w", err)
	case <-ctx.Done():
	}

	log.Info("stopping HTTP server")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("HTTP server shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server: %w", err)
	}
	return nil
}

// LogRequests logs each request: method, path, status and duration. Headers
// (including tokens) are never logged.
func LogRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.LogAttrs(r.Context(), slog.LevelInfo, "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// WarnIfExposed warns, with risk, when addr listens beyond this machine. restricted
// means the operator declared that the deployment restricts access to addr: a
// container that listens on all its interfaces but whose port is only published on
// the host's loopback, or a reverse proxy with authentication. The binary cannot see
// that from inside, so it only logs the address.
func WarnIfExposed(log *slog.Logger, addr string, restricted bool, risk string) {
	switch {
	case IsLoopback(addr):
	case restricted:
		log.Info("listening beyond loopback: access restricted by the deployment (RUNSTEN_ACCESS_RESTRICTED)", "addr", addr)
	default:
		log.Warn(risk, "addr", addr)
	}
}

// IsLoopback reports whether addr (host:port) only listens on this machine.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && IsLoopbackHost(host)
}

// IsLoopbackHost reports whether host (a name or an IP, without port) designates this
// machine.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
