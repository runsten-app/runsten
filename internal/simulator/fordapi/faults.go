package fordapi

import (
	"net/http"
	"time"
)

// The faults are set on demand, as they happen in a moment on the real service: a grant
// withdrawn, the sign-in's outage, a limit of calls. They live until the simulator
// stops: each test builds its own handler, a development session its own run.

// Revoke withdraws the grants of vehicle vin, or every grant when empty, as an owner
// who removes the application's access at Ford: the refresh is refused (invalid_grant),
// the access no longer reads, and a code not exchanged yet no longer is. A sign-in made
// after it reads again.
func (h *Handler) Revoke(vin string) {
	h.mu.Lock()
	for _, g := range h.grants {
		if vin == "" || g.vin == vin {
			g.revoked = true
		}
	}
	for code, g := range h.codes {
		if vin == "" || g.vin == vin {
			delete(h.codes, code)
		}
	}
	h.mu.Unlock()
}

// Outage has the token endpoint fail for d with Azure AD B2C's AADB2C90027, as it did
// for every account during an outage of the real sign-in; the API still reads with the
// accesses given before.
func (h *Handler) Outage(d time.Duration) {
	h.mu.Lock()
	h.outageUntil = h.clk.Now().Add(d)
	h.mu.Unlock()
}

// Limit has the API and the token endpoint answer 429 for d, with Retry-After.
func (h *Handler) Limit(d time.Duration) {
	h.mu.Lock()
	h.limitedUntil = h.clk.Now().Add(d)
	h.mu.Unlock()
}

// vehicles answers the VINs of the scenarios' Fords, in the order the sign-ins are given
// them.
func (h *Handler) vehicles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"vins": append([]string{}, h.vins...)})
}

func (h *Handler) setRevoke(w http.ResponseWriter, r *http.Request) {
	h.Revoke(r.URL.Query().Get("vin"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setOutage(w http.ResponseWriter, r *http.Request) {
	h.Outage(durationOf(r, 15*time.Minute))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setLimit(w http.ResponseWriter, r *http.Request) {
	h.Limit(durationOf(r, 5*time.Minute))
	w.WriteHeader(http.StatusNoContent)
}

// durationOf reads the for parameter; def when absent or unreadable.
func durationOf(r *http.Request, def time.Duration) time.Duration {
	d, err := time.ParseDuration(r.URL.Query().Get("for"))
	if err != nil || d <= 0 {
		return def
	}
	return d
}
