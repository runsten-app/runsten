package skodaapi

import (
	"net/http"
	"time"
)

// The faults are set on demand, as they happen in a moment on the real API: the key
// expired, a vehicle unticked from the key, a part that could not be read, an outage.
// They live until the simulator stops: each test builds its own handler, a development
// session its own run.

// ExpireIn has the key expire after d from now: at once when d is zero or less.
func (h *Handler) ExpireIn(d time.Duration) {
	h.mu.Lock()
	h.expiresAt = h.clk.Now().Add(max(0, d)).Truncate(time.Second)
	h.mu.Unlock()
}

// Uncover has the key no longer cover vehicle vin, or any vehicle when empty: it answers
// 403 api-key-not-authorized, as when the owner unticks the car in MyŠkoda.
func (h *Handler) Uncover(vin string) {
	h.mu.Lock()
	if vin == "" {
		for v := range h.byVIN {
			h.uncovered[v] = true
		}
	} else {
		h.uncovered[vin] = true
	}
	h.mu.Unlock()
}

// MakeUnavailable has the part (an include value: charging, parkingPosition…), or every
// part when empty, left out of the responses with its *_UNAVAILABLE error.
func (h *Handler) MakeUnavailable(part string) {
	h.mu.Lock()
	h.unavailable[part] = true
	h.mu.Unlock()
}

// Outage has the API answer 503 for d, each call counted in its VIN's quota.
func (h *Handler) Outage(d time.Duration) {
	h.mu.Lock()
	h.outageUntil = h.clk.Now().Add(d)
	h.mu.Unlock()
}

// ExpiresAt returns when the key expires.
func (h *Handler) ExpiresAt() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.expiresAt
}

// vehicles answers the VINs the key covers, which the real API never lists: connect-sim
// gives them with the key, as the owner would.
func (h *Handler) vehicles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"vins": append([]string{}, h.vins...)})
}

func (h *Handler) setExpire(w http.ResponseWriter, r *http.Request) {
	d, err := time.ParseDuration(r.URL.Query().Get("in"))
	if err != nil {
		d = 0
	}
	h.ExpireIn(d)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setUncover(w http.ResponseWriter, r *http.Request) {
	h.Uncover(r.URL.Query().Get("vin"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setUnavailable(w http.ResponseWriter, r *http.Request) {
	h.MakeUnavailable(r.URL.Query().Get("part"))
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
