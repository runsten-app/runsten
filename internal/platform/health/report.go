package health

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// ReportPath is the route of the detailed health report.
const ReportPath = Path + "/full"

// Check tells how a part of the service is: healthy or not, and why in a few words.
// It must return before ctx ends.
type Check func(ctx context.Context) (detail string, healthy bool)

// Result is a check's outcome in the report.
type Result struct {
	Status string `json:"status"` // ok or failing
	Detail string `json:"detail"`
}

// Report is the detailed health report: every check's result, and failing if any is.
type Report struct {
	Status string            `json:"status"`
	Checks map[string]Result `json:"checks"`
}

// Reporter serves the detailed health report to whoever presents its token, for an
// uptime monitor: each check is run on each request, at once, within a timeout. The
// status is 200 when every check passes, 503 otherwise; ?check=name runs that one
// alone, so that a monitor watches each part apart. Without the token, 401: the
// details are not public.
type Reporter struct {
	token   [sha256.Size]byte
	checks  map[string]Check
	timeout time.Duration
}

// NewReporter creates a reporter for the checks, behind a bearer token.
func NewReporter(token string, checks map[string]Check, timeout time.Duration) *Reporter {
	return &Reporter{token: sha256.Sum256([]byte(token)), checks: checks, timeout: timeout}
}

func (rp *Reporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Hashed first, so that the comparison takes the same time whatever the length.
	given, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if h := sha256.Sum256([]byte(given)); !ok || subtle.ConstantTimeCompare(h[:], rp.token[:]) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	names := make([]string, 0, len(rp.checks))
	for name := range rp.checks {
		names = append(names, name)
	}
	if only := r.URL.Query().Get("check"); only != "" {
		if _, ok := rp.checks[only]; !ok {
			http.Error(w, "no such check", http.StatusNotFound)
			return
		}
		names = []string{only}
	}
	slices.Sort(names)

	ctx, cancel := context.WithTimeout(r.Context(), rp.timeout)
	defer cancel()
	rep := Report{Status: "ok", Checks: make(map[string]Result, len(names))}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Go(func() {
			detail, healthy := rp.checks[name](ctx)
			res := Result{Status: "ok", Detail: detail}
			if !healthy {
				res.Status = "failing"
			}
			mu.Lock()
			defer mu.Unlock()
			rep.Checks[name] = res
			if !healthy {
				rep.Status = "failing"
			}
		})
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if rep.Status != "ok" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(rep)
}
