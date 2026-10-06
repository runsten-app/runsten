package main

import (
	"context"
	"fmt"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/platform/health"
)

const (
	// minHealthToken is the shortest token RUNSTEN_HEALTH_TOKEN takes.
	minHealthToken = 32
	// checkTimeout bounds the health report's checks, below a monitor's timeout.
	checkTimeout = 5 * time.Second
	// collectorMaxAge is how long the collector may go without a pass before the report
	// fails: its own health delay, after which the Connection page says it stopped. It
	// writes each vehicle's status every minute (statusEvery) while it runs.
	collectorMaxAge = 15 * time.Minute
	// connectionsMaxShare is the share of the server's connections, in percent, beyond
	// which the report fails: new ones are refused when they run out.
	connectionsMaxShare = 80
)

// healthStore is what the health report reads.
type healthStore interface {
	Connections(ctx context.Context) (open, maxConns int, err error)
	LastPass(ctx context.Context) (vehicles int, passedAt time.Time, err error)
}

// healthChecks are the checks of the health report: the database answers and has
// connections left, and the collector passes over the vehicles.
func healthChecks(st healthStore, clk clock.Clock) map[string]health.Check {
	return map[string]health.Check{
		"database": func(ctx context.Context) (string, bool) {
			open, maxConns, err := st.Connections(ctx)
			if err != nil {
				return "not reachable", false // the error may name the server: not shown
			}
			return fmt.Sprintf("%d of %d connections", open, maxConns), open*100 < maxConns*connectionsMaxShare
		},
		"collector": func(ctx context.Context) (string, bool) {
			vehicles, at, err := st.LastPass(ctx)
			switch {
			case err != nil:
				return "database not reachable", false
			case vehicles == 0:
				return "no vehicle to read", true
			case at.IsZero():
				return "no pass yet", false
			}
			// A pass ahead of this clock (a development collector's runs faster) is now.
			age := max(clk.Now().Sub(at), 0)
			return fmt.Sprintf("last pass %s ago", age.Round(time.Second).String()), age <= collectorMaxAge
		},
	}
}
