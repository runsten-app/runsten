package scenario

import (
	"fmt"
	"sync"
	"time"

	"runsten/internal/simulator/vehicle"
)

// tick is the simulated time step during a trip or a charge.
const tick = time.Second

// Simulation runs a scenario. State advances lazily: on each ReportAt call, the
// simulation catches up to the requested time. It is safe for concurrent use.
type Simulation struct {
	mu        sync.Mutex
	sc        *Scenario
	veh       *vehicle.Vehicle
	now       time.Time
	begin     time.Time // the scenario's start: until then, the vehicle stays parked
	step      int
	stepStart time.Time
	entered   bool
}

// Status summarizes the simulation progress.
type Status struct {
	VIN      string
	Now      time.Time
	Step     int // index of the current step, len(Steps) once the scenario is over
	Steps    int
	Finished bool
}

// NewSimulation prepares the simulation of a validated scenario.
func NewSimulation(sc *Scenario, policy vehicle.UploadPolicy) (*Simulation, error) {
	return newSimulation(sc, policy, sc.Start)
}

// newSimulation creates the vehicle at origin, no later than the scenario's start.
func newSimulation(sc *Scenario, policy vehicle.UploadPolicy, origin time.Time) (*Simulation, error) {
	caps, ok := vehicle.Profile(sc.Vehicle.Profile)
	if !ok {
		return nil, fmt.Errorf("unknown profile: %q", sc.Vehicle.Profile)
	}
	origin, start := origin.UTC(), sc.Start.UTC()
	vc := sc.Vehicle
	spec := vehicle.Spec{
		Model:                  vc.Model,
		ModelYear:              vc.ModelYear,
		BatteryKWh:             vc.BatteryKWh,
		UsableKWh:              vc.UsableKWh,
		FadePerYear:            vc.FadePerYear,
		FadeStart:              start, // each vehicle fades from its own start, not the fleet's
		ConsumptionKWhPer100km: vc.ConsumptionKWhPer100km,
		ChargingCurrentLimitA:  vc.ChargingCurrentLimitA,
		Capabilities:           caps,
	}
	initial := vehicle.State{
		SoC:        vc.SoC,
		TargetSoC:  vc.TargetSoC,
		OdometerKm: vc.OdometerKm,
		Position:   sc.Places[vc.Place].position(),
		Locked:     true,
	}
	veh, err := vehicle.New(spec, initial, policy, origin)
	if err != nil {
		return nil, fmt.Errorf("creating vehicle: %w", err)
	}
	return &Simulation{sc: sc, veh: veh, now: origin, begin: start, stepStart: start}, nil
}

// VIN returns the simulated vehicle's VIN.
func (s *Simulation) VIN() string { return s.sc.VIN }

// ReportAt advances the simulation up to at (if at is in the future) and returns
// what the cloud knows about the vehicle.
func (s *Simulation) ReportAt(at time.Time) vehicle.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advance(at.UTC())
	return s.veh.Report()
}

// StatusAt advances the simulation up to at and returns its progress.
func (s *Simulation) StatusAt(at time.Time) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advance(at.UTC())
	return Status{VIN: s.sc.VIN, Now: s.now, Step: s.step, Steps: len(s.sc.Steps), Finished: s.step >= len(s.sc.Steps)}
}

// State returns the vehicle's actual state, without advancing the simulation.
func (s *Simulation) State() vehicle.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.veh.State()
}

func (s *Simulation) advance(to time.Time) {
	for s.now.Before(to) {
		if s.now.Before(s.begin) { // parked until the scenario starts
			s.now = s.begin
			if to.Before(s.begin) {
				s.now = to
			}
			continue
		}
		if s.step >= len(s.sc.Steps) {
			s.now = to // scenario over: the vehicle stays parked
			return
		}
		st := s.sc.Steps[s.step]
		switch {
		case st.Park != nil:
			s.park(st.Park, to)
		case st.Drive != nil:
			s.drive(st.Drive, to)
		case st.Charge != nil:
			s.charge(st.Charge, to)
		}
	}
}

func (s *Simulation) next() {
	s.step++
	s.stepStart = s.now
	s.entered = false
}

func (s *Simulation) park(p *Park, to time.Time) {
	end := s.stepStart.Add(p.Duration)
	if to.Before(end) {
		s.now = to
		return
	}
	s.now = end
	s.next()
}

func (s *Simulation) drive(d *Drive, to time.Time) {
	if !s.entered {
		s.veh.StartDrive(s.now)
		s.entered = true
	}
	end := s.stepStart.Add(d.Duration)
	kmPerTick := d.DistanceKm * float64(tick) / float64(d.Duration)
	for s.now.Before(to) && s.now.Before(end) {
		dt := min(tick, end.Sub(s.now), to.Sub(s.now))
		s.now = s.now.Add(dt)
		s.veh.Drive(s.now, kmPerTick*float64(dt)/float64(tick))
	}
	if !s.now.Before(end) {
		s.veh.EndDrive(s.now, s.sc.Places[d.To].position())
		s.next()
	}
}

func (s *Simulation) charge(c *Charge, to time.Time) {
	if !s.entered {
		typ, _ := chargeType(c.Type) // validated by Parse
		s.veh.PlugIn(s.now, typ, c.PowerKW*1000, c.UntilSoC)
		s.entered = true
	}
	for s.now.Before(to) {
		dt := min(tick, to.Sub(s.now))
		s.now = s.now.Add(dt)
		if s.veh.Charge(s.now, dt) {
			s.next()
			return
		}
	}
}
