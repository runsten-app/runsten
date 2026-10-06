// Package clock provides an injectable clock: real, accelerated or manual.
//
// The domain and the simulator never call time.Now directly, which makes the
// simulator's accelerated clock and deterministic tests possible.
package clock

import (
	"sync"
	"time"
)

// Clock gives the current time.
type Clock interface {
	Now() time.Time
}

// Real is the system clock.
type Real struct{}

// Now returns the system time in UTC.
func (Real) Now() time.Time { return time.Now().UTC() }

// Accelerated advances a simulated time Factor times faster than real time,
// starting from Epoch.
type Accelerated struct {
	base      Clock
	realStart time.Time
	epoch     time.Time
	factor    float64
}

// NewAccelerated creates a simulated clock that reads epoch at the time of the call
// and advances factor times faster than base. factor must be strictly positive.
func NewAccelerated(base Clock, epoch time.Time, factor float64) *Accelerated {
	return &Accelerated{base: base, realStart: base.Now(), epoch: epoch.UTC(), factor: factor}
}

// Now returns the current simulated time.
func (a *Accelerated) Now() time.Time {
	elapsed := a.base.Now().Sub(a.realStart)
	return a.epoch.Add(time.Duration(float64(elapsed) * a.factor))
}

// Manual is a manually driven clock, for tests.
type Manual struct {
	mu  sync.Mutex
	now time.Time
}

// NewManual creates a manual clock set to t.
func NewManual(t time.Time) *Manual { return &Manual{now: t.UTC()} }

// Now returns the clock's current time.
func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

// Advance moves the clock forward by d.
func (m *Manual) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
}
