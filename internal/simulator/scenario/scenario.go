// Package scenario reads a YAML scenario and evolves a virtual vehicle over
// simulated time.
package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"time"

	"runsten/internal/simulator/vehicle"

	"go.yaml.in/yaml/v3"
)

// Scenario describes a day (or more) in the life of a vehicle.
type Scenario struct {
	VIN     string           `yaml:"vin"`
	Start   time.Time        `yaml:"start"`
	Vehicle VehicleConfig    `yaml:"vehicle"`
	Places  map[string]Place `yaml:"places"`
	Steps   []Step           `yaml:"steps"`
	API     API              `yaml:"api"`
}

// API sets the simulated API's behavior for this scenario: limits, faults and token
// lifetimes. The simulator's environment variables take precedence. With several
// scenarios, only the first may have one (NewFleet).
type API struct {
	DailyQuota *int          `yaml:"dailyQuota"` // nil: default value
	PerMinute  *int          `yaml:"perMinute"`
	TokenTTL   time.Duration `yaml:"tokenTTL"` // tokens not issued by the simulator
	ErrorRate  float64       `yaml:"errorRate"`
	Latency    time.Duration `yaml:"latency"`
	Outages    []Outage      `yaml:"outages"`

	// Lifetimes of what the simulated Volvo ID issues; zero: default value.
	AccessTokenTTL  time.Duration `yaml:"accessTokenTTL"`
	RefreshTokenTTL time.Duration `yaml:"refreshTokenTTL"`
	GrantTTL        time.Duration `yaml:"grantTTL"`
}

// Outage makes the API unavailable for Duration, starting After the scenario start.
type Outage struct {
	After    time.Duration `yaml:"after"`
	Duration time.Duration `yaml:"duration"`
}

// VehicleConfig describes the vehicle and its initial state.
type VehicleConfig struct {
	Profile                string  `yaml:"profile"`
	Model                  string  `yaml:"model"`
	ModelYear              int     `yaml:"modelYear"`
	BatteryKWh             float64 `yaml:"batteryKWh"`
	UsableKWh              float64 `yaml:"usableKWh"`
	FadePerYear            float64 `yaml:"fadePerYear"`
	ConsumptionKWhPer100km float64 `yaml:"consumptionKWhPer100km"`
	ChargingCurrentLimitA  float64 `yaml:"chargingCurrentLimitA"`
	SoC                    float64 `yaml:"soc"`
	TargetSoC              float64 `yaml:"targetSoc"`
	OdometerKm             float64 `yaml:"odometerKm"`
	Place                  string  `yaml:"place"`
}

// Place is a named location.
type Place struct {
	Lat float64 `yaml:"lat"`
	Lon float64 `yaml:"lon"`
}

// Step is a scenario step: exactly one of the fields is set.
type Step struct {
	Park   *Park   `yaml:"park"`
	Drive  *Drive  `yaml:"drive"`
	Charge *Charge `yaml:"charge"`
}

// Park keeps the vehicle parked for Duration.
type Park struct {
	Duration time.Duration `yaml:"duration"`
}

// Drive moves the vehicle to To, over DistanceKm, in Duration.
type Drive struct {
	To         string        `yaml:"to"`
	DistanceKm float64       `yaml:"distanceKm"`
	Duration   time.Duration `yaml:"duration"`
}

// Charge charges the vehicle in place up to UntilSoC.
type Charge struct {
	Type     string  `yaml:"type"` // AC or DC
	PowerKW  float64 `yaml:"powerKW"`
	UntilSoC float64 `yaml:"untilSoc"`
}

var vinPattern = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`)

// Parse reads and validates a YAML scenario. Unknown fields are rejected.
func Parse(data []byte) (*Scenario, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var s Scenario
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("reading scenario: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("invalid scenario: %w", err)
	}
	return &s, nil
}

// Validate checks that the scenario is consistent.
func (s *Scenario) Validate() error {
	var errs []error
	if !vinPattern.MatchString(s.VIN) {
		errs = append(errs, fmt.Errorf("vin %q: 17 characters, no I, O or Q", s.VIN))
	}
	if _, ok := vehicle.Profile(s.Vehicle.Profile); !ok {
		errs = append(errs, fmt.Errorf("unknown profile: %q", s.Vehicle.Profile))
	}
	if _, ok := s.Places[s.Vehicle.Place]; !ok {
		errs = append(errs, fmt.Errorf("unknown starting place: %q", s.Vehicle.Place))
	}
	if len(s.Steps) == 0 {
		errs = append(errs, errors.New("no steps"))
	}
	for i, st := range s.Steps {
		if err := s.validateStep(st); err != nil {
			errs = append(errs, fmt.Errorf("step %d: %w", i+1, err))
		}
	}
	errs = append(errs, s.API.validate())
	return errors.Join(errs...)
}

func (a API) validate() error {
	var errs []error
	for name, n := range map[string]*int{"api.dailyQuota": a.DailyQuota, "api.perMinute": a.PerMinute} {
		if n != nil && *n < 0 {
			errs = append(errs, fmt.Errorf("%s must be ≥ 0", name))
		}
	}
	if a.TokenTTL < 0 || a.Latency < 0 {
		errs = append(errs, errors.New("api.tokenTTL and api.latency must be ≥ 0"))
	}
	if a.AccessTokenTTL < 0 || a.RefreshTokenTTL < 0 || a.GrantTTL < 0 {
		errs = append(errs, errors.New("api.accessTokenTTL, api.refreshTokenTTL and api.grantTTL must be ≥ 0"))
	}
	if a.ErrorRate < 0 || a.ErrorRate > 1 {
		errs = append(errs, errors.New("api.errorRate must be in [0, 1]"))
	}
	for i, o := range a.Outages {
		if o.After < 0 || o.Duration <= 0 {
			errs = append(errs, fmt.Errorf("api.outages[%d]: after ≥ 0 and duration > 0 expected", i))
		}
	}
	return errors.Join(errs...)
}

func (s *Scenario) validateStep(st Step) error {
	n := 0
	for _, set := range []bool{st.Park != nil, st.Drive != nil, st.Charge != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return errors.New("a step must contain exactly one of park, drive, charge")
	}
	switch {
	case st.Park != nil:
		if st.Park.Duration <= 0 {
			return errors.New("park.duration must be > 0")
		}
	case st.Drive != nil:
		if _, ok := s.Places[st.Drive.To]; !ok {
			return fmt.Errorf("drive.to: unknown place %q", st.Drive.To)
		}
		if st.Drive.DistanceKm <= 0 || st.Drive.Duration <= 0 {
			return errors.New("drive.distanceKm and drive.duration must be > 0")
		}
	case st.Charge != nil:
		if _, err := chargeType(st.Charge.Type); err != nil {
			return err
		}
		if st.Charge.PowerKW <= 0 {
			return errors.New("charge.powerKW must be > 0")
		}
		if st.Charge.UntilSoC <= 0 || st.Charge.UntilSoC > 100 {
			return errors.New("charge.untilSoc must be in ]0, 100]")
		}
	}
	return nil
}

func chargeType(s string) (vehicle.ChargeType, error) {
	switch s {
	case "AC":
		return vehicle.AC, nil
	case "DC":
		return vehicle.DC, nil
	}
	return vehicle.NoCharge, fmt.Errorf("charge.type %q: AC or DC expected", s)
}

func (p Place) position() vehicle.Position { return vehicle.Position{Lat: p.Lat, Lon: p.Lon} }
