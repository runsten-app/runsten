package scenario

import (
	"errors"
	"fmt"
	"time"

	"runsten/internal/simulator/vehicle"
)

// Fleet runs several scenarios on one clock, one vehicle each: the vehicles of one
// Volvo ID account.
type Fleet struct {
	start time.Time
	sims  []*Simulation
}

// NewFleet prepares the simulation of validated scenarios, in their order.
//
// Their VINs must be distinct. One simulated API serves every vehicle, so only the
// first scenario may have an api section. The clock starts at the earliest start: a
// vehicle whose scenario starts later stays parked in its initial state, at its
// starting place, until its own start.
func NewFleet(scs []*Scenario, policy vehicle.UploadPolicy) (*Fleet, error) {
	if len(scs) == 0 {
		return nil, errors.New("no scenario")
	}
	var errs []error
	first := map[string]int{} // VIN → number of the first scenario that has it
	start := scs[0].Start
	for i, sc := range scs {
		if j, ok := first[sc.VIN]; ok {
			errs = append(errs, fmt.Errorf("scenarios %d and %d have the same vin %q", j, i+1, sc.VIN))
		} else {
			first[sc.VIN] = i + 1
		}
		if i > 0 && !sc.API.isZero() {
			errs = append(errs, fmt.Errorf("scenario %d (vin %s): only the first scenario may have an api section", i+1, sc.VIN))
		}
		if sc.Start.Before(start) {
			start = sc.Start
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	f := &Fleet{start: start.UTC()}
	for i, sc := range scs {
		sim, err := newSimulation(sc, policy, f.start)
		if err != nil {
			return nil, fmt.Errorf("scenario %d (vin %s): %w", i+1, sc.VIN, err)
		}
		f.sims = append(f.sims, sim)
	}
	return f, nil
}

// Start returns the earliest start of the scenarios, where the clock starts.
func (f *Fleet) Start() time.Time { return f.start }

// Simulations returns the simulation of each scenario, in their order.
func (f *Fleet) Simulations() []*Simulation { return f.sims }

// StatusAt advances every simulation up to at and returns their progress.
func (f *Fleet) StatusAt(at time.Time) []Status {
	out := make([]Status, len(f.sims))
	for i, s := range f.sims {
		out[i] = s.StatusAt(at)
	}
	return out
}

func (a API) isZero() bool {
	return a.DailyQuota == nil && a.PerMinute == nil && a.TokenTTL == 0 && a.ErrorRate == 0 && a.Latency == 0 &&
		len(a.Outages) == 0 && a.AccessTokenTTL == 0 && a.RefreshTokenTTL == 0 && a.GrantTTL == 0
}
