package demosource

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/collector"
	"runsten/internal/demo"
	"runsten/internal/platform/clock"
)

func newSource(t *testing.T, at time.Time) (*Source, *clock.Manual) {
	t.Helper()
	d, err := demo.New()
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(at)
	return New(d, clk), clk
}

func fetch(t *testing.T, s *Source) demo.Reading {
	t.Helper()
	sess, ttl, err := s.Login(context.Background(), "account", "connection")
	if err != nil || ttl != accessLife {
		t.Fatalf("login: %v, %v", ttl, err)
	}
	raw, err := sess.Fetch(context.Background(), demo.VIN, collector.Endpoint(demo.Vehicle))
	if err != nil {
		t.Fatal(err)
	}
	r, err := demo.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestSpec: one endpoint, read as the history written at connection is: every minute
// while it drives or charges, every ten while parked.
func TestSpec(t *testing.T) {
	s, _ := newSource(t, demo.Origin())
	spec := s.Spec()
	if spec.Name != demo.Source || len(spec.Endpoints) != 1 || spec.Intervals.Active != demo.ActiveEvery || spec.Intervals.Parked != demo.ParkedEvery {
		t.Fatalf("spec %+v", spec)
	}
	ep := spec.Endpoints[0]
	if ep.Parked != collector.PaceParked || ep.Driving != collector.PaceActive || ep.Charging != collector.PaceActive || ep.API != demo.Source {
		t.Errorf("endpoint %+v", ep)
	}
	if spec.Daily < int(24*time.Hour/demo.ActiveEvery) {
		t.Errorf("a budget of %d reads a day is under a read a minute", spec.Daily)
	}
}

// TestFetchFollowsTheClock: a read is the vehicle's state at the collector's now.
func TestFetchFollowsTheClock(t *testing.T) {
	s, clk := newSource(t, demo.Origin().Add(9*demo.Week+3*time.Hour))
	if r := fetch(t, s); r.Driving || r.Charging != "done" || r.SoC != 80 || !r.At.Equal(clk.Now()) {
		t.Errorf("parked at night: %+v", r)
	}
	clk.Advance(3*time.Hour + 55*time.Minute) // 06:55 on the Monday
	if r := fetch(t, s); !r.Driving || r.SoC >= 80 || !r.At.Equal(clk.Now()) {
		t.Errorf("on the way to work: %+v", r)
	}
	clk.Advance(-time.Hour) // the clock never goes back, but a source reads whatever time it is given
	if r := fetch(t, s); r.Driving {
		t.Errorf("before the drive: %+v", r)
	}
}

// TestFetchRefuses: another vehicle or endpoint, and a cancelled context.
func TestFetchRefuses(t *testing.T) {
	s, _ := newSource(t, demo.Origin())
	sess, _, _ := s.Login(context.Background(), "a", "c")
	_, err := sess.Fetch(context.Background(), "YV1AAAAAAAAAAAAA1", collector.Endpoint(demo.Vehicle))
	var se *collector.SourceError
	if !errors.As(err, &se) || se.Kind != collector.KindUnshared {
		t.Errorf("another VIN: %v", err)
	}
	if _, err := sess.Fetch(context.Background(), demo.VIN, "other"); err == nil {
		t.Error("another endpoint read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sess.Fetch(ctx, demo.VIN, collector.Endpoint(demo.Vehicle)); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

// TestObserve: the mode is what the reading says.
func TestObserve(t *testing.T) {
	s, clk := newSource(t, demo.Origin().Add(9*demo.Week+6*time.Hour+55*time.Minute))
	read := func() collector.Activity {
		sess, _, _ := s.Login(context.Background(), "a", "c")
		raw, err := sess.Fetch(context.Background(), demo.VIN, collector.Endpoint(demo.Vehicle))
		if err != nil {
			t.Fatal(err)
		}
		var a collector.Activity
		if err := s.Observe(collector.Endpoint(demo.Vehicle), raw, &a); err != nil {
			t.Fatal(err)
		}
		up, err := s.Upload(collector.Endpoint(demo.Vehicle), raw)
		if err != nil || !up.At.Equal(clk.Now()) || up.ModelYear == 0 {
			t.Errorf("upload %+v, %v", up, err)
		}
		return a
	}
	if a := read(); !a.Driving || a.Charges || a.Engine != "running" {
		t.Errorf("driving: %+v", a)
	}
	clk.Advance(9*time.Hour + 35*time.Minute) // 16:30: plugged in on arriving
	if a := read(); a.Driving || !a.Charges || a.Charging != "charging" {
		t.Errorf("charging: %+v", a)
	}
	clk.Advance(5 * time.Hour)
	if a := read(); a.Driving || a.Charges || a.Engine != "stopped" {
		t.Errorf("parked: %+v", a)
	}
	var a collector.Activity
	if err := s.Observe(collector.Endpoint(demo.Vehicle), []byte(`{`), &a); err == nil {
		t.Error("garbage observed")
	}
	if _, err := s.Upload(collector.Endpoint(demo.Vehicle), []byte(`{`)); err == nil {
		t.Error("garbage uploaded")
	}
}

// TestFingerprint: two reads of a parked vehicle bring nothing new, whatever the time.
func TestFingerprint(t *testing.T) {
	s, clk := newSource(t, demo.Origin().Add(9*demo.Week+time.Hour))
	sess, _, _ := s.Login(context.Background(), "a", "c")
	a, _ := sess.Fetch(context.Background(), demo.VIN, collector.Endpoint(demo.Vehicle))
	clk.Advance(10 * time.Minute)
	b, _ := sess.Fetch(context.Background(), demo.VIN, collector.Endpoint(demo.Vehicle))
	fa, err := s.Fingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	if fb, _ := s.Fingerprint(b); string(fa) != string(fb) || string(a) == string(b) {
		t.Errorf("parked reads: fingerprints %x and %x", fa, fb)
	}
	if _, err := s.Fingerprint([]byte(`{`)); err == nil {
		t.Error("garbage fingerprinted")
	}
}
