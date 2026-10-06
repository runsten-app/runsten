package auth

import (
	"sync"
	"time"
)

// throttle counts failed logins per key (a username, a client address). A key with
// max failures within window is blocked until the window, started at its first
// failure, ends. It lives in memory: a restart clears it.
type throttle struct {
	max    int
	window time.Duration

	mu   sync.Mutex
	keys map[string]failures
}

type failures struct {
	n     int
	since time.Time
}

func newThrottle(maxFailures int, window time.Duration) *throttle {
	return &throttle{max: maxFailures, window: window, keys: map[string]failures{}}
}

// blocked returns how long the most restricted of keys stays blocked; zero if none is.
func (t *throttle) blocked(now time.Time, keys ...string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	var wait time.Duration
	for _, k := range keys {
		f, ok := t.keys[k]
		if !ok || f.n < t.max {
			continue
		}
		if left := f.since.Add(t.window).Sub(now); left > wait {
			wait = left
		}
	}
	return wait
}

// fail records a failure for each key.
func (t *throttle) fail(now time.Time, keys ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, f := range t.keys {
		if !now.Before(f.since.Add(t.window)) {
			delete(t.keys, k)
		}
	}
	for _, k := range keys {
		f, ok := t.keys[k]
		if !ok {
			f.since = now
		}
		f.n++
		t.keys[k] = f
	}
}

// reset forgets the failures of keys.
func (t *throttle) reset(keys ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range keys {
		delete(t.keys, k)
	}
}
