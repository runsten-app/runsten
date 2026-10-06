package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReporter(t *testing.T) {
	healthy := func(context.Context) (string, bool) { return "fine", true }
	failing := func(context.Context) (string, bool) { return "broken", false }
	// A check that outlives the timeout returns when its context ends.
	slow := func(ctx context.Context) (string, bool) {
		<-ctx.Done()
		return "timed out", false
	}
	rp := NewReporter("secret", map[string]Check{"database": healthy, "collector": failing, "slow": slow}, 10*time.Millisecond)
	for _, tt := range []struct {
		name   string
		auth   string
		query  string
		status int
		want   Report
	}{
		{name: "no token", query: "", status: http.StatusUnauthorized},
		{name: "wrong token", auth: "Bearer secreT", status: http.StatusUnauthorized},
		{name: "not bearer", auth: "secret", status: http.StatusUnauthorized},
		{name: "every check", auth: "Bearer secret", status: http.StatusServiceUnavailable, want: Report{
			Status: "failing", Checks: map[string]Result{
				"database":  {"ok", "fine"},
				"collector": {"failing", "broken"},
				"slow":      {"failing", "timed out"},
			},
		}},
		{name: "one healthy", auth: "Bearer secret", query: "?check=database", status: http.StatusOK, want: Report{
			Status: "ok", Checks: map[string]Result{"database": {"ok", "fine"}},
		}},
		{name: "one failing", auth: "Bearer secret", query: "?check=collector", status: http.StatusServiceUnavailable, want: Report{
			Status: "failing", Checks: map[string]Result{"collector": {"failing", "broken"}},
		}},
		{name: "unknown check", auth: "Bearer secret", query: "?check=nope", status: http.StatusNotFound},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, ReportPath+tt.query, nil)
		if tt.auth != "" {
			req.Header.Set("Authorization", tt.auth)
		}
		rec := httptest.NewRecorder()
		rp.ServeHTTP(rec, req)
		if rec.Code != tt.status {
			t.Errorf("%s: status %d, want %d (%s)", tt.name, rec.Code, tt.status, rec.Body)
			continue
		}
		if tt.want.Status == "" {
			continue
		}
		var got Report
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got.Status != tt.want.Status || len(got.Checks) != len(tt.want.Checks) {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
			continue
		}
		for name, res := range tt.want.Checks {
			if got.Checks[name] != res {
				t.Errorf("%s: %s = %+v, want %+v", tt.name, name, got.Checks[name], res)
			}
		}
	}
}
