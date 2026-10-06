package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
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
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(base()), collect)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.baseURL != volvo.DefaultBaseURL || cfg.authURL != volvo.DefaultAuthURL || cfg.tick != 10*time.Second ||
		cfg.intervals != collector.DefaultIntervals() || cfg.quota != collector.DefaultQuota() || cfg.logLevel != slog.LevelInfo ||
		cfg.tokens != oauth.DefaultParams() || cfg.clientID != "id" || cfg.clientSecret != "secret" || cfg.clockSpeed != 1 ||
		!cfg.privateBrokers {
		t.Errorf("config = %+v", cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	m := base()
	for k, v := range map[string]string{
		"RUNSTEN_VOLVO_BASE_URL": "http://localhost:8080", "RUNSTEN_POLL_TICK": "1s",
		"RUNSTEN_POLL_PARKED": "10s", "RUNSTEN_POLL_ACTIVE": "2s", "RUNSTEN_POLL_RARE": "1m",
		"RUNSTEN_POLL_DETAILS": "1h", "RUNSTEN_LOG_LEVEL": "debug", "RUNSTEN_VOLVO_ACCESS_TOKEN": "t",
		"RUNSTEN_DEBUG_ADDR": "127.0.0.1:8090", "RUNSTEN_VOLVO_AUTH_URL": "http://localhost:8080",
		"RUNSTEN_TOKEN_MARGIN": "30s", "RUNSTEN_TOKEN_KEEPALIVE": "6h",
		"RUNSTEN_HEALTH_ADDR": "127.0.0.1:8091", "RUNSTEN_ACCESS_RESTRICTED": "1",
		"RUNSTEN_DEV_CLOCK_SPEED": "60", "RUNSTEN_VOLVO_DAILY_QUOTA": "50000",
		"RUNSTEN_VOLVO_QUOTA_PER_USER": "true", "RUNSTEN_MQTT_PRIVATE_BROKERS": "false",
	} {
		m[k] = v
	}
	cfg, err := loadConfig(env(m), connect)
	if err != nil {
		t.Fatal(err)
	}
	want := collector.Intervals{Parked: 10 * time.Second, Active: 2 * time.Second, Rare: time.Minute, Details: time.Hour}
	if cfg.baseURL != "http://localhost:8080" || cfg.tick != time.Second || cfg.intervals != want ||
		cfg.accessToken != "t" || cfg.logLevel != slog.LevelDebug || cfg.debugAddr != "127.0.0.1:8090" ||
		cfg.authURL != "http://localhost:8080" || cfg.tokens.Margin != 30*time.Second || cfg.tokens.KeepAlive != 6*time.Hour ||
		cfg.healthAddr != "127.0.0.1:8091" || !cfg.restricted || cfg.clockSpeed != 60 || cfg.quota != (collector.Quota{Daily: 50000, PerUser: true}) ||
		cfg.privateBrokers {
		t.Errorf("config = %+v", cfg)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	_, err := loadConfig(env(map[string]string{
		"RUNSTEN_POLL_TICK": "fast", "RUNSTEN_POLL_ACTIVE": "-1s", "RUNSTEN_LOG_LEVEL": "chatty",
		"RUNSTEN_DEBUG_ADDR": "8090", "RUNSTEN_VOLVO_AUTH_URL": "volvoid", "RUNSTEN_TOKEN_KEEPALIVE": "168h",
		"RUNSTEN_HEALTH_ADDR": "8091", "RUNSTEN_ACCESS_RESTRICTED": "yes please",
		"RUNSTEN_DEV_CLOCK_SPEED": "0", "RUNSTEN_VOLVO_DAILY_QUOTA": "10k",
		"RUNSTEN_VOLVO_QUOTA_PER_USER": "maybe", "RUNSTEN_MQTT_PRIVATE_BROKERS": "sometimes",
	}), connect)
	if err == nil {
		t.Fatal("error expected")
	}
	for _, want := range []string{
		"RUNSTEN_DATABASE_URL", "RUNSTEN_TOKEN_KEY", "RUNSTEN_VOLVO_API_KEY", "RUNSTEN_VOLVO_ACCESS_TOKEN",
		"RUNSTEN_POLL_TICK", "RUNSTEN_POLL_ACTIVE", "RUNSTEN_LOG_LEVEL", "RUNSTEN_DEBUG_ADDR",
		"RUNSTEN_VOLVO_AUTH_URL", "RUNSTEN_TOKEN_KEEPALIVE", "RUNSTEN_HEALTH_ADDR", "RUNSTEN_ACCESS_RESTRICTED",
		"RUNSTEN_DEV_CLOCK_SPEED", "RUNSTEN_VOLVO_DAILY_QUOTA", "RUNSTEN_VOLVO_QUOTA_PER_USER",
		"RUNSTEN_MQTT_PRIVATE_BROKERS",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %s: %v", want, err)
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	if err := run([]string{"bogus"}); err == nil {
		t.Error("unknown command accepted")
	}
}

func TestParseCommand(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want command
		ok   bool
	}{
		{nil, collect, true},
		{[]string{"connect"}, connect, true},
		{[]string{"rebuild"}, rebuild, true},
		{[]string{"healthcheck"}, healthcheck, true},
		{[]string{"rebuild", "now"}, 0, false},
	} {
		got, err := parseCommand(tt.args)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("parseCommand(%q) = %v, %v", tt.args, got, err)
		}
	}
}

func TestLoadConfigClientRequiredToCollect(t *testing.T) {
	m := base()
	delete(m, "RUNSTEN_VOLVO_CLIENT_ID")
	delete(m, "RUNSTEN_VOLVO_CLIENT_SECRET")
	_, err := loadConfig(env(m), collect)
	if err == nil || !strings.Contains(err.Error(), "RUNSTEN_VOLVO_CLIENT_ID") || !strings.Contains(err.Error(), "RUNSTEN_VOLVO_CLIENT_SECRET") {
		t.Errorf("collect without the client credentials: %v", err)
	}
	m["RUNSTEN_VOLVO_ACCESS_TOKEN"] = "t"
	if _, err := loadConfig(env(m), connect); err != nil {
		t.Errorf("connect needs no client credentials: %v", err)
	}
}

// TestLoadConfigAPIKey: the instance's key is optional to collect (the hosted offer
// reads only the connections with their own) and to rebuild; connect lists the vehicles
// of a test token with it.
func TestLoadConfigAPIKey(t *testing.T) {
	m := base()
	delete(m, "RUNSTEN_VOLVO_API_KEY")
	for _, cmd := range []command{rebuild, collect} {
		if _, err := loadConfig(env(m), cmd); err != nil {
			t.Errorf("command %d without the instance's key: %v", cmd, err)
		}
	}
	m["RUNSTEN_VOLVO_ACCESS_TOKEN"] = "t"
	if _, err := loadConfig(env(m), connect); err == nil || !strings.Contains(err.Error(), "RUNSTEN_VOLVO_API_KEY") {
		t.Errorf("connect without the instance's key: %v", err)
	}
}

func TestLoadConfigHealthcheck(t *testing.T) {
	// The probe runs in the collector's container, with its environment, but needs
	// none of its settings except the health address.
	cfg, err := loadConfig(env(map[string]string{"RUNSTEN_HEALTH_ADDR": "127.0.0.1:8091"}), healthcheck)
	if err != nil || cfg.healthAddr != "127.0.0.1:8091" {
		t.Errorf("healthcheck: %+v, %v", cfg, err)
	}
	if _, err := loadConfig(env(base()), healthcheck); err == nil || !strings.Contains(err.Error(), "RUNSTEN_HEALTH_ADDR") {
		t.Errorf("healthcheck without a health address: %v", err)
	}
}

func TestHealthcheck(t *testing.T) {
	stale := httptest.NewServer(health.NewHeartbeat(clock.NewManual(time.Time{}), -time.Second))
	defer stale.Close()
	mux := http.NewServeMux()
	mux.Handle("GET "+health.Path, health.NewHeartbeat(clock.NewManual(time.Time{}), time.Minute))
	fresh := httptest.NewServer(mux)
	defer fresh.Close()

	for addr, ok := range map[string]bool{
		fresh.Listener.Addr().String(): true,
		stale.Listener.Addr().String(): false,
	} {
		t.Setenv("RUNSTEN_HEALTH_ADDR", addr)
		if err := run([]string{"healthcheck"}); (err == nil) != ok {
			t.Errorf("healthcheck %s: %v", addr, err)
		}
	}
}

func TestDevClock(t *testing.T) {
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		speed  float64
		latest time.Time
		want   time.Time // after one real second
	}{
		{"real time", 1, now.Add(time.Hour), now.Add(time.Second)},
		{"from now", 60, time.Time{}, now.Add(time.Minute)},
		{"latest in the past", 60, now.Add(-time.Hour), now.Add(time.Minute)},
		{"resumes after a previous run", 60, now.Add(5 * time.Hour), now.Add(5*time.Hour + resumeMargin + time.Minute)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := clock.NewManual(now)
			clk := devClock(base, tt.speed, tt.latest)
			base.Advance(time.Second)
			if got := clk.Now(); !got.Equal(tt.want) {
				t.Errorf("Now() = %s, want %s", got, tt.want)
			}
		})
	}
}
