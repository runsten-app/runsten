// Package demosource reads the demo vehicle for the collector: it adapts internal/demo to
// collector.Source. There is no call and no credential to ask for: the vehicle is simulated
// in this process, and a read asks the simulation for its state at the collector's now,
// which is the same whatever restarted meanwhile.
package demosource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/demo"
	"runsten/internal/platform/clock"
)

// Daily is the reads a day the collector's budget grants the source (API demo in
// api_calls): a read costs nothing, the budget is set above the dearest day, one read
// a minute (1,440).
const Daily = 2000

// accessLife is how long a source's access lasts: there is none, and the session is
// asked for again at this pace.
const accessLife = 24 * time.Hour

// Intervals are the source's paces: those the history written at connection follows,
// the vehicle read every minute while it drives or charges and every ten minutes while
// it is parked. The rare and details paces read nothing apart.
func Intervals() collector.Intervals {
	return collector.Intervals{Parked: demo.ParkedEvery, Active: demo.ActiveEvery, Rare: time.Hour, Details: 24 * time.Hour}
}

// Source is the demo vehicle as a collector.Source.
type Source struct {
	demo *demo.Demo
	clk  clock.Clock

	// cursor keeps the simulation of the week being read: the reads go forward in time,
	// and one cursor serves every account (the vehicle is the same for all).
	mu     sync.Mutex
	cursor *demo.Cursor
}

// New returns the Source reading d at the time clk gives.
func New(d *demo.Demo, clk clock.Clock) *Source {
	return &Source{demo: d, clk: clk, cursor: d.Cursor()}
}

// Spec implements collector.Source: the one endpoint, read again at once when a trip or a
// charge ends, or a charge starts.
func (*Source) Spec() collector.SourceSpec {
	return collector.SourceSpec{
		Name: demo.Source,
		Endpoints: []collector.EndpointSpec{{
			Endpoint: collector.Endpoint(demo.Vehicle), API: demo.Source,
			Parked: collector.PaceParked, Driving: collector.PaceActive, Charging: collector.PaceActive,
		}},
		Settled:   []collector.Endpoint{collector.Endpoint(demo.Vehicle)},
		Daily:     Daily,
		Intervals: Intervals(),
	}
}

// Login implements collector.Source: the session reads the simulation, which needs no
// access.
func (s *Source) Login(context.Context, string, string) (collector.Session, time.Duration, error) {
	return session{src: s}, accessLife, nil
}

// Observe implements collector.Source: whether the vehicle drives, and whether it charges.
func (*Source) Observe(_ collector.Endpoint, raw []byte, a *collector.Activity) error {
	r, err := demo.Parse(raw)
	if err != nil {
		return err //nolint:wrapcheck // demo's, which says what failed
	}
	a.Engine, a.Driving = "stopped", r.Driving
	if r.Driving {
		a.Engine = "running"
	}
	a.Charging, a.Charges = r.Charging, r.Charging == "charging"
	return nil
}

// Fingerprint implements collector.Source: the values, the instant left out.
func (*Source) Fingerprint(raw []byte) ([]byte, error) {
	return demo.Fingerprint(raw) //nolint:wrapcheck // demo's, which says what failed
}

// Upload implements collector.Source: the instant of the reading.
func (*Source) Upload(_ collector.Endpoint, raw []byte) (collector.Upload, error) {
	r, err := demo.Parse(raw)
	if err != nil {
		return collector.Upload{}, err //nolint:wrapcheck // demo's, which says what failed
	}
	return collector.Upload{At: r.At, ModelYear: r.ModelYear}, nil
}

// reading is the vehicle's state now.
func (s *Source) reading() (demo.Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor.ReadingAt(s.clk.Now()) //nolint:wrapcheck // demo's, which says what failed
}

// session reads the simulation.
type session struct{ src *Source }

// Fetch implements collector.Session: the vehicle's state at the collector's now.
func (s session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("demo: %w", err)
	}
	if demo.Endpoint(ep) != demo.Vehicle {
		return nil, fmt.Errorf("demo: unknown endpoint %q", ep)
	}
	if vin != demo.VIN {
		// Not a failure to wait out: no other VIN is read through this source.
		return nil, &collector.SourceError{Kind: collector.KindUnshared, Err: errors.New("demo: not the demo vehicle")}
	}
	r, err := s.src.reading()
	if err != nil {
		return nil, err
	}
	return demo.Marshal(r) //nolint:wrapcheck // demo's, which says what failed
}
