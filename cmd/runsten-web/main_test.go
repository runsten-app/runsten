package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(env(nil))
	if err != nil || cfg.addr != "127.0.0.1:8082" || cfg.api.String() != "http://127.0.0.1:8081" || cfg.dir != "/web" ||
		cfg.basePath != "/" || cfg.apiHost || cfg.restricted || cfg.logLevel != slog.LevelInfo || cfg.mapTiles != nil {
		t.Fatalf("defaults: %+v, %v", cfg, err)
	}
	cfg, err = loadConfig(env(map[string]string{
		"RUNSTEN_WEB_ADDR": "0.0.0.0:8082", "RUNSTEN_WEB_API_URL": "http://api:8081", "RUNSTEN_WEB_DIR": "/srv/web",
		"RUNSTEN_WEB_BASE_PATH": "/runsten/", "RUNSTEN_WEB_API_HOST": "api", "RUNSTEN_ACCESS_RESTRICTED": "true",
		"RUNSTEN_LOG_LEVEL": "debug",
	}))
	if err != nil || cfg.addr != "0.0.0.0:8082" || cfg.api.Host != "api:8081" || cfg.dir != "/srv/web" ||
		cfg.basePath != "/runsten/" || !cfg.apiHost || !cfg.restricted || cfg.logLevel != slog.LevelDebug {
		t.Fatalf("overrides: %+v, %v", cfg, err)
	}
	tiles := "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	cfg, _ = loadConfig(env(map[string]string{"RUNSTEN_WEB_MAP_TILES": tiles}))
	if m := cfg.mapTiles; m == nil || m.URL != tiles || m.Attribution != "© OpenStreetMap contributors" {
		t.Errorf("map tiles: %+v", m)
	}
	cfg, _ = loadConfig(env(map[string]string{"RUNSTEN_WEB_MAP_TILES": tiles, "RUNSTEN_WEB_MAP_ATTRIBUTION": "© Someone"}))
	if m := cfg.mapTiles; m == nil || m.Attribution != "© Someone" {
		t.Errorf("map attribution: %+v", m)
	}
	for name, v := range map[string]map[string]string{
		"api scheme":  {"RUNSTEN_WEB_API_URL": "ftp://api"},
		"api path":    {"RUNSTEN_WEB_API_URL": "http://api:8081/api/v1"},
		"api no host": {"RUNSTEN_WEB_API_URL": "http://"},
		"api host":    {"RUNSTEN_WEB_API_HOST": "upstream"},
		"restricted":  {"RUNSTEN_ACCESS_RESTRICTED": "yes please"},
		"log level":   {"RUNSTEN_LOG_LEVEL": "loud"},
	} {
		if _, err := loadConfig(env(v)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestNewHandler(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := loadConfig(env(map[string]string{"RUNSTEN_WEB_DIR": dir}))
	if _, err := newHandler(cfg, slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("empty build: %v", err)
	}
	page := `<base href="/" /><meta name="csp-nonce" content="" /><div id="app"></div>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := newHandler(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/healthz": "ok", "/trips": `<div id="app">`} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body.String())
		}
	}
}

func TestRunRejectsUnknownCommands(t *testing.T) {
	if err := run([]string{"serve", "now"}); err == nil {
		t.Error("no error")
	}
}
