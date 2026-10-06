package collector

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// Quota is the Volvo quota the collector budgets. Volvo counts it on the application
// of the key (vcc-api-key) a call is made with, not on the OAuth client that issued the
// token (tried on the real API): every connection shares the instance's key, or, on an
// instance without one, each has its own, and its own quota.
type Quota struct {
	// Daily is the number of calls a day granted per API (Connected Vehicle, Energy,
	// Location) and key. Assumption: counted over any 24 hours, the window the
	// agreement leaves unsaid.
	Daily int
	// PerUser says the quota of the instance's key is granted to each user (Volvo ID)
	// rather than to the application: each account then has its own, and a refusal
	// blocks only its own calls. The agreement does not say which; the developer
	// portal counts the calls per application. False (the default) assumes the worst:
	// every account without its own key shares one quota. Assumption: one Volvo ID
	// per account.
	PerUser bool
	// NoInstanceKey says the instance has no application key of its own
	// (RUNSTEN_VOLVO_API_KEY unset, as in the hosted offer): each connection reads with
	// its own key, and one without is not read. With the instance's key, it reads every
	// connection, and a connection's own key is not used.
	NoInstanceKey bool
}

// DefaultQuota returns the quota of the API agreement: 10,000 calls a day per API and
// key, the instance's key shared by the whole application.
func DefaultQuota() Quota { return Quota{Daily: 10_000} }

const (
	// quotaReserve is the share of the quota the collector leaves unspent: runsten-api
	// lists the vehicles of each new connection with the same application, and a
	// collector that stops abruptly loses the calls of its latest pass (see budget).
	quotaReserve = 0.1
	// budgetBurst is how much of the budget may be spent at once, as calls accrued over
	// this duration: the first pass reads every endpoint of every vehicle, and the
	// mornings put many vehicles on the road together.
	budgetBurst = time.Hour
	// usageHalfLife weighs an account's past calls when accounts compete for the budget:
	// a call counts half as much after this long.
	usageHalfLife = 6 * time.Hour
	// budgetWindow is how far back the budget reads the calls saved, after a restart.
	budgetWindow = 24 * time.Hour
)

// Calls counts the calls an account made to an API within an hour. They are saved, so
// that a restarted collector knows what it spent.
type Calls struct {
	AccountID string
	API       string
	Hour      time.Time // start of the hour, UTC
	N         int
}

// unit is what a quota is counted on: an API of a connection's own key, or of the
// instance's key, with the account when each user has its own quota of it.
type unit struct {
	key     string // the connection whose own key it is; empty: the instance's key
	account string // the instance's key with Quota.PerUser; else empty
	api     string
}

// budget spreads the quota over the day, per unit, with a token bucket: calls accrue at
// a steady rate, up to a burst. Over any 24 hours, the calls stay below the burst plus
// a day's accrual, the quota less its reserve. The calls are counted per account and
// hour, saved after each pass, and replayed by a restarted collector (restore).
//
// When calls of the instance's key must wait, the accounts that called the least lately
// with it go first (order): an account's demand no longer starves the others, and the
// budget it leaves goes to those that need more. An account with its own key competes
// with no one: it spends its own quota, and waits for it alone.
type budget struct {
	perUser bool
	rate    float64 // calls per second, per unit
	burst   float64
	tokens  map[unit]float64   // a unit not seen yet has a full bucket
	at      time.Time          // when the tokens were counted
	usage   map[string]float64 // recent calls with the instance's key, by account
	usedAt  time.Time          // when the usages were decayed
	short   map[unit]bool      // units whose calls wait, for the log
	unsaved map[Calls]int      // calls not saved yet, N left at zero in the key
}

func newBudget(q Quota, now time.Time) *budget {
	usable := float64(q.Daily) * (1 - quotaReserve)
	burst := max(1, usable*budgetBurst.Hours()/24) // below one call, nothing would ever be called
	return &budget{
		perUser: q.PerUser,
		rate:    (usable - burst) / budgetWindow.Seconds(),
		burst:   burst,
		tokens:  map[unit]float64{},
		at:      now,
		usage:   map[string]float64{},
		usedAt:  now,
		short:   map[unit]bool{},
		unsaved: map[Calls]int{},
	}
}

// unitOf is the unit a call of t to api counts on.
func (b *budget) unitOf(t Target, api string) unit {
	switch {
	case t.ownKey():
		return unit{key: t.ConnectionID, api: api}
	case b.perUser:
		return unit{account: t.AccountID, api: api}
	}
	return unit{api: api}
}

// refill adds the calls accrued since the last count.
func (b *budget) refill(now time.Time) {
	if elapsed := now.Sub(b.at).Seconds(); elapsed > 0 {
		for u, n := range b.tokens {
			b.tokens[u] = min(b.burst, n+elapsed*b.rate)
		}
		b.at = now
	}
}

// allows reports whether t may call api now, and whether that changed since the
// previous question: the caller logs when calls start and stop waiting.
func (b *budget) allows(t Target, api string, now time.Time) (ok, changed bool) {
	b.refill(now)
	u := b.unitOf(t, api)
	n, seen := b.tokens[u]
	ok = !seen || n >= 1
	changed = b.short[u] == ok
	b.short[u] = !ok
	return ok, changed
}

// spend counts a call made by t to api. A call the budget did not allow (the retry of
// a rejected token) is counted too: the bucket may run below zero.
func (b *budget) spend(t Target, api string, now time.Time) {
	u := b.unitOf(t, api)
	b.take(u, 1, now)
	if u.key == "" {
		b.decay(now)
		b.usage[t.AccountID]++
	}
	b.unsaved[Calls{AccountID: t.AccountID, API: api, Hour: now.UTC().Truncate(time.Hour)}]++
}

func (b *budget) take(u unit, n float64, now time.Time) {
	b.refill(now)
	left, seen := b.tokens[u]
	if !seen {
		left = b.burst
	}
	b.tokens[u] = left - n
}

// decay brings the usages to now.
func (b *budget) decay(now time.Time) {
	if elapsed := now.Sub(b.usedAt); elapsed > 0 {
		f := math.Exp2(-elapsed.Hours() / usageHalfLife.Hours())
		for a, u := range b.usage {
			if u *= f; u < 1e-3 {
				delete(b.usage, a)
			} else {
				b.usage[a] = u
			}
		}
		b.usedAt = now
	}
}

// order sorts the targets by the recent calls of their account, fewest first. The sort
// is stable: the vehicles of an account keep their order, and so do the accounts that
// called as much.
func (b *budget) order(targets []Target, now time.Time) {
	b.decay(now)
	slices.SortStableFunc(targets, func(x, y Target) int {
		return cmp.Compare(b.usage[x.AccountID], b.usage[y.AccountID])
	})
}

// restore replays the calls saved since now less budgetWindow, oldest hour first, into
// a fresh budget: each hour's calls are taken at its end (now, for the current hour),
// the latest they may have been made, which leaves the fewest tokens. The calls are
// saved per account: they count on the key each account's connection has now (among
// targets), the instance's for an account without a vehicle.
func (b *budget) restore(calls []Calls, targets []Target, now time.Time) {
	b.at, b.usedAt = now.Add(-budgetWindow), now
	byAccount := map[string]Target{}
	for _, t := range targets {
		byAccount[t.AccountID] = t
	}
	calls = slices.Clone(calls)
	slices.SortStableFunc(calls, func(x, y Calls) int { return x.Hour.Compare(y.Hour) })
	for _, c := range calls {
		end := c.Hour.Add(time.Hour)
		if end.After(now) {
			end = now
		}
		t, ok := byAccount[c.AccountID]
		if !ok {
			t = Target{AccountID: c.AccountID}
		}
		u := b.unitOf(t, c.API)
		b.take(u, float64(c.N), end)
		if u.key == "" {
			age := now.Sub(c.Hour.Add(time.Hour / 2))
			b.usage[c.AccountID] += float64(c.N) * math.Exp2(-max(0, age.Hours())/usageHalfLife.Hours())
		}
	}
	b.refill(now)
}

// unsavedCalls returns the calls counted since the latest save, and forgets them: the
// caller gives them back (keep) if it could not save them.
func (b *budget) unsavedCalls() []Calls {
	out := make([]Calls, 0, len(b.unsaved))
	for c, n := range b.unsaved {
		c.N = n
		out = append(out, c)
	}
	clear(b.unsaved)
	return out
}

// keep counts again calls that could not be saved.
func (b *budget) keep(calls []Calls) {
	for _, c := range calls {
		n := c.N
		c.N = 0
		b.unsaved[c] += n
	}
}
