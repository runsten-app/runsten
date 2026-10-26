package release

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

var t0 = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestNewer(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"0.11.0", "0.10.1", true},
		{"0.10.10", "0.10.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.10.1", "0.10.1", false},
		{"0.10.0", "0.10.1", false},
		{"0.11.0", "0.11.0-rc.1", true},
		{"0.11.0", "0.12.0-rc.1", false},
		{"0.12.0", "0.11.0-rc.1", true},
		{"0.11.0", "0.11.0-", false},
		{"0.11.0", "dev", false},
		{"0.11.0", "", false},
		{"0.11.0-rc.1", "0.10.0", false},
		{"0.11", "0.10.0", false},
		{"0.011.0", "0.10.0", false},
		{"0.-1.0", "0.0.0", false},
	}
	for _, tt := range tests {
		if got := Newer(tt.a, tt.b); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// github serves body as the latest release, with status.
func github(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("Accept: %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

const latest = `{"tag_name":"v0.11.0","html_url":"https://github.com/runsten-app/runsten/releases/tag/v0.11.0","body":"Notes"}`

func TestCheck(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewManual(t0)
	c := NewChecker(github(t, http.StatusOK, latest), "0.10.1", http.DefaultClient, clk, quiet)
	if _, ok := c.Update(); ok || !c.CheckedAt().IsZero() {
		t.Error("an update, or a read, before any check")
	}
	if err := c.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.CheckedAt().Equal(t0) {
		t.Errorf("CheckedAt() = %v", c.CheckedAt())
	}
	got, ok := c.Update()
	want := Release{Version: "0.11.0", URL: "https://github.com/runsten-app/runsten/releases/tag/v0.11.0"}
	if !ok || got != want {
		t.Errorf("Update() = %+v, %v", got, ok)
	}

	for _, running := range []string{"0.11.0", "0.12.0", "0.12.0-rc.1", "dev"} {
		c := NewChecker(github(t, http.StatusOK, latest), running, http.DefaultClient, clk, quiet)
		if err := c.Check(ctx); err != nil {
			t.Fatal(err)
		}
		if r, ok := c.Update(); ok {
			t.Errorf("running %s: update %+v", running, r)
		}
	}
}

// TestCheckFails: a failed read is an error, and keeps the release read before.
func TestCheckFails(t *testing.T) {
	ctx := context.Background()
	for name, srv := range map[string]string{
		"status":    github(t, http.StatusForbidden, `{"message":"API rate limit exceeded"}`),
		"not JSON":  github(t, http.StatusOK, `<html>`),
		"no tag":    github(t, http.StatusOK, `{"html_url":"https://github.com/x"}`),
		"bad tag":   github(t, http.StatusOK, `{"tag_name":"latest","html_url":"https://github.com/x"}`),
		"elsewhere": github(t, http.StatusOK, `{"tag_name":"v0.11.0","html_url":"https://example.com/x"}`),
	} {
		clk := clock.NewManual(t0)
		c := NewChecker(github(t, http.StatusOK, latest), "0.10.1", http.DefaultClient, clk, quiet)
		if err := c.Check(ctx); err != nil {
			t.Fatal(err)
		}
		clk.Advance(time.Hour)
		c.url = srv
		if err := c.Check(ctx); err == nil {
			t.Errorf("%s: no error", name)
		}
		if r, ok := c.Update(); !ok || r.Version != "0.11.0" || !c.CheckedAt().Equal(t0) {
			t.Errorf("%s: the release read before is lost: %+v, %v, %v", name, r, ok, c.CheckedAt())
		}
	}
}
