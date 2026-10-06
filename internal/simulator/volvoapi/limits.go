package volvoapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
)

// Limits groups the simulated limits. A zero value disables the corresponding
// limit.
type Limits struct {
	// DailyQuota is the number of calls per day, per API and per application key
	// (10,000 according to the Volvo contract). The key's application counts them,
	// whichever OAuth client issued the token: tried on the real API. Assumption: the
	// calls of every vehicle count against the same quota, not one quota per vehicle.
	DailyQuota int
	// Keys are the application keys accepted, each with its own quota; empty: any key.
	// Another one is refused as the gateway refuses an unknown key (assumed message).
	Keys []string
	// PerMinute is the number of requests per minute, per (token, key) pair, across
	// all APIs (100 according to the Volvo docs).
	PerMinute int
	// TokenTTL is the validity period of a token the simulator did not issue (a test
	// token from the developer portal, about 30 minutes reported), counted from its
	// first use. Such a token cannot be refreshed: once expired, it stays expired.
	// Tokens issued by the simulated Volvo ID follow OAuth.AccessTokenTTL instead.
	TokenTTL time.Duration
}

// DefaultLimits returns the limits documented by Volvo. The expiration of tokens not
// issued by the simulator is disabled by default: it would block a collector
// connected with such a token after 30 simulated minutes.
func DefaultLimits() Limits { return Limits{DailyQuota: 10_000, PerMinute: 100} }

type counterKey struct {
	who    string
	api    string
	window time.Time
}

// limiter counts calls per simulated time window. Expired windows are purged
// whenever the window changes.
type limiter struct {
	mu      sync.Mutex
	clk     clock.Clock
	limits  Limits
	daily   map[counterKey]int
	minute  map[counterKey]int
	lastDay time.Time
	lastMin time.Time
	seen    map[string]time.Time // first use of each token not issued here
	issuer  *issuer
}

func newLimiter(clk clock.Clock, l Limits, iss *issuer) *limiter {
	return &limiter{
		clk: clk, limits: l, issuer: iss,
		daily: map[counterKey]int{}, minute: map[counterKey]int{}, seen: map[string]time.Time{},
	}
}

// middleware applies the simulated authentication, then the rate limit and the
// quota of API api.
func (l *limiter) middleware(api string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, key, ok := credentials(r)
		if key == "" {
			keyRefused(w, "Access denied due to missing header VCC-API-KEY. Make sure to provide a valid key for an active application.")
			return
		}
		if len(l.limits.Keys) > 0 && !slices.Contains(l.limits.Keys, key) {
			keyRefused(w, "Access denied due to invalid VCC-API-KEY. Make sure to provide a valid key for an active application.")
			return
		}
		if !ok {
			unauthorized(w, api, "Missing bearer token")
			return
		}
		now := l.clk.Now()
		if l.expired(token, now) {
			unauthorized(w, api, "Access token expired")
			return
		}
		if retry, ok := l.allowMinute(token+"|"+key, now); !ok {
			// Assumed format (Azure API Management rate limit message).
			writeStatusError(w, http.StatusTooManyRequests,
				fmt.Sprintf("Rate limit is exceeded. Try again in %d seconds.", int(retry.Seconds())+1))
			return
		}
		if wait, ok := l.allowDay(key, api, now); !ok {
			// Message observed by the Home Assistant community (status 403, not 429).
			writeStatusError(w, http.StatusForbidden,
				"Out of call volume quota. Quota will be replenished in "+hms(wait)+".")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *limiter) expired(token string, now time.Time) bool {
	if known, valid := l.issuer.check(token, now); known {
		return !valid
	}
	if l.limits.TokenTTL <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	first, ok := l.seen[token]
	if !ok {
		l.seen[token] = now
		return false
	}
	return !now.Before(first.Add(l.limits.TokenTTL))
}

func (l *limiter) allowMinute(who string, now time.Time) (time.Duration, bool) {
	if l.limits.PerMinute <= 0 {
		return 0, true
	}
	win := now.Truncate(time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !win.Equal(l.lastMin) {
		clear(l.minute)
		l.lastMin = win
	}
	k := counterKey{who: who, window: win}
	if l.minute[k] >= l.limits.PerMinute {
		return win.Add(time.Minute).Sub(now), false
	}
	l.minute[k]++
	return 0, true
}

func (l *limiter) allowDay(key, api string, now time.Time) (time.Duration, bool) {
	if l.limits.DailyQuota <= 0 {
		return 0, true
	}
	day := now.UTC().Truncate(24 * time.Hour)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !day.Equal(l.lastDay) {
		clear(l.daily)
		l.lastDay = day
	}
	k := counterKey{who: key, api: api, window: day}
	if l.daily[k] >= l.limits.DailyQuota {
		return day.Add(24 * time.Hour).Sub(now), false
	}
	l.daily[k]++
	return 0, true
}

// unauthorized responds to a request without valid credentials, in each API's
// error format. The 401 status has not been observed: the 403 formats observed on
// the Volvo demo car are reused.
func unauthorized(w http.ResponseWriter, api, description string) {
	if api == apiEnergy {
		writeEnergyError(w, http.StatusUnauthorized, "AUTHORIZATION_ERROR", "Not authorized to access this resource.")
		return
	}
	writeCVError(w, http.StatusUnauthorized, "UNAUTHORIZED", description)
}

// keyRefused responds to a request without a valid application key, before any API
// sees it, in the gateway's own format and words, as observed on the real API, the same
// for every API.
func keyRefused(w http.ResponseWriter, message string) {
	type gatewayError struct {
		Status int `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	e := gatewayError{Status: http.StatusUnauthorized}
	e.Error.Message = message
	writeJSON(w, http.StatusUnauthorized, e)
}

// credentials extracts the Bearer token and the application key. Any non-empty token
// is accepted as such: expiry is checked afterwards, scopes never.
func credentials(r *http.Request) (token, key string, ok bool) {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	key = r.Header.Get("vcc-api-key")
	return token, key, found && token != "" && key != ""
}

func hms(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s%3600/60, s%60)
}
