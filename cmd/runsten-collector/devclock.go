package main

import (
	"context"
	"fmt"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/store"
)

// resumeMargin is added to the latest time a previous run wrote: an unchanged status is
// written again only every minute of its clock, so a reading may follow the latest one.
const resumeMargin = time.Minute

// devClock is the collector's clock: real time, or with RUNSTEN_DEV_CLOCK_SPEED a clock
// that runs speed times faster, as the simulator's does. The readings are then stamped
// in the simulator's time scale, and the energy of a charge integrated over its
// simulated duration. It starts at the latest time a previous run wrote, if later than
// now: the readings of the store never go back in time.
func devClock(base clock.Clock, speed float64, latest time.Time) clock.Clock {
	if speed == 1 {
		return base
	}
	epoch := base.Now()
	if l := latest.Add(resumeMargin); l.After(epoch) {
		epoch = l
	}
	return clock.NewAccelerated(base, epoch, speed)
}

// latestPass is the latest pass or successful call any vehicle's status records: the
// time a previous accelerated run had reached. Zero when none is recorded.
func latestPass(ctx context.Context, st *store.Store) (time.Time, error) {
	targets, err := st.Targets(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("targets: %w", err)
	}
	var latest time.Time
	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		if seen[t.AccountID] {
			continue
		}
		seen[t.AccountID] = true
		vehicles, err := st.Vehicles(ctx, t.AccountID)
		if err != nil {
			return time.Time{}, fmt.Errorf("vehicles: %w", err)
		}
		for _, v := range vehicles {
			if v.Collection == nil {
				continue
			}
			for _, at := range []time.Time{v.Collection.PassedAt, v.Collection.ReadAt} {
				if at.After(latest) {
					latest = at
				}
			}
		}
	}
	return latest, nil
}
