package renaultapi

import (
	"net/http"
	"time"
)

// The faults are set on demand, as they happen in a moment on the real API: the account
// asking for its code, the login token dead, the quota spent, the car's privacy mode,
// the account's id changed, an outage. They live until the simulator stops (or Clear):
// each test builds its own handler, a development session its own run.

// RequireTFA has the account ask for its one-time code at the next sign-in of every
// device, those that entered it before included.
func (h *Handler) RequireTFA() {
	h.mu.Lock()
	h.tfa = true
	h.remembered = map[string]bool{}
	h.mu.Unlock()
}

// Revoke kills the login tokens and the JWTs issued: the next read is refused, the next
// JWT asked with a login token answers 403005, and only a new sign-in reads again.
func (h *Handler) Revoke() {
	h.mu.Lock()
	h.loginTokens, h.jwts = map[string]bool{}, map[string]time.Time{}
	h.mu.Unlock()
}

// Overloaded spends the account's quota now: Kamereon answers err.func.wired.overloaded
// for an hour.
func (h *Handler) Overloaded() {
	h.mu.Lock()
	h.overloadedTill = h.clk.Now().Add(time.Hour)
	h.mu.Unlock()
}

// Privacy turns the cars' privacy mode on: their position is refused with
// err.func.privacy.on.
func (h *Handler) Privacy() {
	h.mu.Lock()
	h.privacy = true
	h.mu.Unlock()
}

// AccountChanged has every read of a car answer err.func.not.connected, as when Renault
// changed the account's id (October 2026): only a new discovery reads again.
func (h *Handler) AccountChanged() {
	h.mu.Lock()
	h.accountChanged = true
	h.mu.Unlock()
}

// Outage has Kamereon answer a gateway's 502 in HTML for d, in no quota.
func (h *Handler) Outage(d time.Duration) {
	h.mu.Lock()
	h.outageUntil = h.clk.Now().Add(d)
	h.mu.Unlock()
}

// Clear removes every fault: the code is no longer asked for, the quota and the outage
// are over, the privacy mode and the account's id are as they were. The tokens revoked
// stay dead.
func (h *Handler) Clear() {
	h.mu.Lock()
	h.tfa, h.privacy, h.accountChanged = false, false, false
	h.overloadedTill, h.outageUntil = time.Time{}, time.Time{}
	h.mu.Unlock()
}

// listVehicles answers the VINs the account owns, which connect-sim reports.
func (h *Handler) listVehicles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"vins": append([]string{}, h.vins...)})
}

func (h *Handler) setTFA(w http.ResponseWriter, _ *http.Request) {
	h.RequireTFA()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setRevoke(w http.ResponseWriter, _ *http.Request) {
	h.Revoke()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setOverloaded(w http.ResponseWriter, _ *http.Request) {
	h.Overloaded()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setPrivacy(w http.ResponseWriter, _ *http.Request) {
	h.Privacy()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setAccountChanged(w http.ResponseWriter, _ *http.Request) {
	h.AccountChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setOutage(w http.ResponseWriter, r *http.Request) {
	d, err := time.ParseDuration(r.URL.Query().Get("for"))
	if err != nil || d <= 0 {
		d = 15 * time.Minute
	}
	h.Outage(d)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setClear(w http.ResponseWriter, _ *http.Request) {
	h.Clear()
	w.WriteHeader(http.StatusNoContent)
}
