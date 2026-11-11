package pleosapi

import (
	"net/http"
	"time"
)

// The faults are set on demand, as installed in a moment on the real API (ADR 0049 §3):
// the key revoked, a vehicle's consent withdrawn, an endpoint without data, the calls
// answered 429. They live until the simulator stops: each test builds its own handler,
// a development session its own run.

// RefuseKey has the key refused at /auth: revoked or reissued, as in the Playground.
func (h *Handler) RefuseKey() {
	h.mu.Lock()
	h.refused = true
	h.mu.Unlock()
}

// Withdraw withdraws the consent of vehicle vin, or of every vehicle's when empty:
// its endpoints answer 5006, as an owner's refusal in the connected services would.
func (h *Handler) Withdraw(vin string) {
	h.mu.Lock()
	if vin == "" {
		for _, src := range h.srcs {
			h.withdrawn[src.VIN()] = true
		}
	} else {
		h.withdrawn[vin] = true
	}
	h.mu.Unlock()
}

// LeaveWithoutData leaves endpoint without data, or every endpoint when empty: it
// answers 4045, the doc's No Data, as a model collecting none of it does.
func (h *Handler) LeaveWithoutData(endpoint string) {
	h.mu.Lock()
	h.noData[endpoint] = true
	h.mu.Unlock()
}

// RateLimit has the API answer 429 for the given time (assumption: none is
// documented, its body and its header are the ones its clients honour; a quarter of an
// hour is the wait a client takes without Retry-After).
func (h *Handler) RateLimit(wait time.Duration) {
	if wait <= 0 {
		wait = 15 * time.Minute
	}
	h.mu.Lock()
	h.rateUntil = h.clk.Now().Add(wait)
	h.mu.Unlock()
}

func (h *Handler) setRevoke(w http.ResponseWriter, _ *http.Request) {
	h.RefuseKey()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setWithdraw(w http.ResponseWriter, r *http.Request) {
	h.Withdraw(r.URL.Query().Get("vin"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setNoData(w http.ResponseWriter, r *http.Request) {
	h.LeaveWithoutData(r.URL.Query().Get("endpoint"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setRateLimit(w http.ResponseWriter, r *http.Request) {
	d, err := time.ParseDuration(r.URL.Query().Get("for"))
	if err != nil || d <= 0 {
		d = 15 * time.Minute
	}
	h.RateLimit(d)
	w.WriteHeader(http.StatusNoContent)
}
