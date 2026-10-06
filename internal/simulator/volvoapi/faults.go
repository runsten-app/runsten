package volvoapi

import (
	"math/rand/v2"
	"net/http"
	"time"

	"runsten/internal/platform/clock"
)

// Faults describes the injected faults. The zero value injects nothing.
// The real behavior during failures has not been observed: status and format are assumed.
type Faults struct {
	// ErrorRate is the share of requests failing with a 5xx error, in [0, 1].
	ErrorRate float64
	// Latency is added to each response, in real time (not simulated time).
	Latency time.Duration
	// Outages are simulated time ranges during which the whole API responds 503.
	Outages []Outage
	// Rand draws a number in [0, 1). Nil: non-deterministic pseudo-random generator.
	Rand func() float64
}

// Outage is an API unavailability, from From (inclusive) to To (exclusive).
type Outage struct {
	From, To time.Time
}

// faultStatuses are the statuses drawn for a random error.
func faultStatuses() []int {
	return []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout}
}

// middleware injects latency and errors before quota accounting: a failed request
// is not counted (assumption ❓).
func (f Faults) middleware(clk clock.Clock, next http.Handler) http.Handler {
	random := f.Rand
	if random == nil {
		random = rand.Float64 //nolint:gosec // fault simulation, not cryptography
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.Latency > 0 {
			t := time.NewTimer(f.Latency)
			select {
			case <-t.C:
			case <-r.Context().Done():
				t.Stop()
				return
			}
		}
		now := clk.Now()
		for _, o := range f.Outages {
			if !now.Before(o.From) && now.Before(o.To) {
				writeStatusError(w, http.StatusServiceUnavailable, "Service Unavailable")
				return
			}
		}
		if f.ErrorRate > 0 && random() < f.ErrorRate {
			statuses := faultStatuses()
			status := statuses[int(random()*float64(len(statuses)))%len(statuses)]
			writeStatusError(w, status, http.StatusText(status))
			return
		}
		next.ServeHTTP(w, r)
	})
}
