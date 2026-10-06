package main

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/health"
	"runsten/internal/volvo"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func base() map[string]string {
	return map[string]string{
		"RUNSTEN_DATABASE_URL": "postgres://x", "RUNSTEN_TOKEN_KEY": "k", "RUNSTEN_VOLVO_API_KEY": "a",
		"RUNSTEN_VOLVO_CLIENT_ID": "id", "RUNSTEN_VOLVO_CLIENT_SECRET": "secret",
		"RUNSTEN_VOLVO_REDIRECT_URI": "https://runsten.example/auth/volvo/callback",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != "127.0.0.1:8081" || cfg.baseURL != volvo.DefaultBaseURL || cfg.app.BaseURL != volvo.DefaultAuthURL ||
		!slices.Equal(cfg.app.Scopes, volvo.DefaultScopes()) || cfg.app.ClientID != "id" || cfg.app.ClientSecret != "secret" ||
		cfg.logLevel != slog.LevelInfo || cfg.geocoder != nil || cfg.geocode.Interval != time.Second || !cfg.privateBrokers {
		t.Errorf("config = %+v", cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	m := base()
	for k, v := range map[string]string{
		"RUNSTEN_VOLVO_BASE_URL": "http://localhost:8080", "RUNSTEN_VOLVO_AUTH_URL": "http://localhost:8080",
		"RUNSTEN_VOLVO_SCOPES": "openid  conve:odometer_status", "RUNSTEN_API_ADDR": ":9000", "RUNSTEN_LOG_LEVEL": "debug",
		"RUNSTEN_VOLVO_REDIRECT_URI": "http://127.0.0.1:8081/auth/volvo/callback", "RUNSTEN_ACCESS_RESTRICTED": "true",
		"RUNSTEN_APP_URL": "http://127.0.0.1:5173", "RUNSTEN_GEOCODER_URL": "https://nominatim.example",
		"RUNSTEN_GEOCODER_INTERVAL": "2s", "RUNSTEN_HEALTH_TOKEN": strings.Repeat("h", minHealthToken),
		"RUNSTEN_MQTT_PRIVATE_BROKERS": "false",
	} {
		m[k] = v
	}
	cfg, err := loadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.baseURL != "http://localhost:8080" || cfg.app.BaseURL != "http://localhost:8080" || cfg.addr != ":9000" || !cfg.restricted ||
		!slices.Equal(cfg.app.Scopes, []string{"openid", "conve:odometer_status"}) || cfg.logLevel != slog.LevelDebug ||
		cfg.appURL != "http://127.0.0.1:5173/" || cfg.geocoder == nil || cfg.geocode.Interval != 2*time.Second ||
		cfg.healthToken != strings.Repeat("h", minHealthToken) || cfg.privateBrokers {
		t.Errorf("config = %+v", cfg)
	}
}

// TestLoadConfigWithoutAPIKey: the instance's key is optional; without it, every account
// gives its own.
func TestLoadConfigWithoutAPIKey(t *testing.T) {
	m := base()
	delete(m, "RUNSTEN_VOLVO_API_KEY")
	if cfg, err := loadConfig(env(m)); err != nil || cfg.apiKey != "" {
		t.Errorf("without the instance's key: %+v, %v", cfg, err)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	_, err := loadConfig(env(map[string]string{
		"RUNSTEN_VOLVO_AUTH_URL": "volvoid", "RUNSTEN_VOLVO_SCOPES": "openid conve:unlock", "RUNSTEN_LOG_LEVEL": "loud",
		"RUNSTEN_ACCESS_RESTRICTED": "maybe", "RUNSTEN_APP_URL": "/connection",
		"RUNSTEN_GEOCODER_URL": "nominatim", "RUNSTEN_GEOCODER_INTERVAL": "10ms", "RUNSTEN_HEALTH_TOKEN": "short",
		"RUNSTEN_MQTT_PRIVATE_BROKERS": "sometimes",
	}))
	if err == nil {
		t.Fatal("error expected")
	}
	for _, want := range []string{
		"RUNSTEN_DATABASE_URL", "RUNSTEN_TOKEN_KEY", "RUNSTEN_VOLVO_CLIENT_ID",
		"RUNSTEN_VOLVO_CLIENT_SECRET", "RUNSTEN_VOLVO_REDIRECT_URI", "RUNSTEN_VOLVO_AUTH_URL", "conve:unlock", "RUNSTEN_LOG_LEVEL",
		"RUNSTEN_ACCESS_RESTRICTED", "RUNSTEN_APP_URL", "RUNSTEN_GEOCODER_URL", "RUNSTEN_GEOCODER_INTERVAL",
		"RUNSTEN_HEALTH_TOKEN", "RUNSTEN_MQTT_PRIVATE_BROKERS",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %s: %v", want, err)
		}
	}
}

func TestValidateRedirectURI(t *testing.T) {
	for uri, ok := range map[string]bool{
		"https://runsten.example/auth/volvo/callback":         true,
		"https://example.org/runsten/auth/volvo/callback":     true, // behind a prefix
		"http://localhost:8081/auth/volvo/callback":           true,
		"http://127.0.0.1:8081/auth/volvo/callback":           true,
		"http://[::1]:8081/auth/volvo/callback":               true,
		"http://nas.local:8081/auth/volvo/callback":           false, // code in clear text on the network
		"https://runsten.example/callback":                    false,
		"https://runsten.example/auth/volvo/callback?next=/x": false,
		"https://runsten.example/auth/volvo/callback#x":       false,
		"https://user:pw@runsten.example/auth/volvo/callback": false,
		"ftp://runsten.example/auth/volvo/callback":           false,
		"/auth/volvo/callback":                                false,
		"://":                                                 false,
	} {
		if err := validateRedirectURI(uri); (err == nil) != ok {
			t.Errorf("%s: %v", uri, err)
		}
	}
}

func TestSiteURL(t *testing.T) {
	for redirect, want := range map[string]string{
		"https://runsten.example/auth/volvo/callback":     "https://runsten.example/auth/volvo/start",
		"https://example.org/runsten/auth/volvo/callback": "https://example.org/runsten/auth/volvo/start",
	} {
		u, _ := url.Parse(redirect)
		if got := siteURL(u, "/auth/volvo/start"); got != want {
			t.Errorf("siteURL(%s) = %s", redirect, got)
		}
	}
}

func TestReadPassword(t *testing.T) {
	for _, tt := range []struct {
		in, want string
		ok       bool
	}{
		{"correct horse battery staple\n", "correct horse battery staple", true},
		{"with CRLF line end\r\nsecond line\n", "with CRLF line end", true},
		{"no newline", "no newline", true},
		{"", "", false},
	} {
		got, err := readPassword(strings.NewReader(tt.in))
		if got != tt.want || (err == nil) != tt.ok {
			t.Errorf("readPassword(%q) = %q, %v", tt.in, got, err)
		}
	}
}

// pipe returns a file whose content is s, as a non-terminal stdin.
func pipe(t *testing.T, s string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestUserCommandErrors: what is refused before any database access.
func TestUserCommandErrors(t *testing.T) {
	db := env(map[string]string{"RUNSTEN_DATABASE_URL": "postgres://127.0.0.1:1/none?connect_timeout=1"})
	for _, tt := range []struct {
		name, user, stdin, want string
		getenv                  func(string) string
	}{
		{"no database", "admin", "correct horse battery staple\n", "RUNSTEN_DATABASE_URL", env(nil)},
		{"invalid username", "two words", "correct horse battery staple\n", "username", db},
		{"no password", "admin", "", "no password", db},
		{"short password", "admin", "short\n", "at least 15", db},
		{"database unreachable", "admin", "correct horse battery staple\n", "migrations", db},
	} {
		err := userCommand("create", tt.user, tt.getenv, pipe(t, tt.stdin), io.Discard)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
}

func TestHealthcheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET "+health.Path, health.OK())
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	down := httptest.NewServer(mux)
	downAddr := down.Listener.Addr().String()
	down.Close()

	for _, tt := range []struct {
		name string
		args []string
		addr string
		ok   bool
	}{
		{"healthy, all interfaces", []string{"healthcheck"}, "0.0.0.0:" + port, true},
		{"not listening", []string{"healthcheck"}, downAddr, false},
		{"unknown command", []string{"serve"}, "0.0.0.0:" + port, false},
		{"unknown user command", []string{"user", "delete", "admin"}, "0.0.0.0:" + port, false},
	} {
		t.Setenv("RUNSTEN_API_ADDR", tt.addr)
		if err := run(tt.args); (err == nil) != tt.ok {
			t.Errorf("%s: run(%q) = %v", tt.name, tt.args, err)
		}
	}
}
