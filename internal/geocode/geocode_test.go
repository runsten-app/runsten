package geocode

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/platform/clock"
)

type fakeStore struct {
	pending  []Claim
	claims   []time.Time
	resolved map[Claim]string
	failSave error
}

func (s *fakeStore) ClaimGeocoding(_ context.Context, at time.Time, lease time.Duration, maxAttempts int) (Claim, bool, error) {
	if lease != DefaultParams().Lease || maxAttempts != DefaultParams().MaxAttempts {
		return Claim{}, false, errors.New("unexpected params")
	}
	s.claims = append(s.claims, at)
	if len(s.pending) == 0 {
		return Claim{}, false, nil
	}
	c := s.pending[0]
	s.pending = s.pending[1:]
	return c, true, nil
}

func (s *fakeStore) ResolveAddress(_ context.Context, c Claim, address string, _ time.Time) error {
	if s.failSave != nil {
		return s.failSave
	}
	s.resolved[c] = address
	return nil
}

type fakeGeocoder struct {
	asked []core.Position
	err   error
}

func (g *fakeGeocoder) Reverse(_ context.Context, p core.Position) (string, error) {
	g.asked = append(g.asked, p)
	if g.err != nil {
		return "", g.err
	}
	return "Rue de la République, Lyon", nil
}

func TestOnce(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewManual(time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC))
	home := Claim{AccountID: "a", Cell: core.GeoCell{LatE4: 457640, LonE4: 48357}}

	s := &fakeStore{pending: []Claim{home}, resolved: map[Claim]string{}}
	g := &fakeGeocoder{}
	var logs bytes.Buffer
	w := New(s, g, clk, DefaultParams(), slog.New(slog.NewTextHandler(&logs, nil)))
	if worked, err := w.Once(ctx); !worked || err != nil {
		t.Fatalf("once = %v, %v", worked, err)
	}
	if len(g.asked) != 1 || g.asked[0] != (core.Position{Lat: 45.764, Lon: 4.8357}) {
		t.Errorf("asked %v: the center of the cell", g.asked)
	}
	if s.resolved[home] != "Rue de la République, Lyon" || !s.claims[0].Equal(clk.Now()) {
		t.Errorf("resolved %v, claimed at %v", s.resolved, s.claims)
	}
	// Nothing left: nothing asked.
	if worked, err := w.Once(ctx); worked || err != nil || len(g.asked) != 1 {
		t.Errorf("idle once = %v, %v, asked %d", worked, err, len(g.asked))
	}

	// The geocoder fails: the cell is left to its lease, and the log tells no position.
	s.pending, g.err = []Claim{home}, errors.New("503 from the geocoder")
	delete(s.resolved, home)
	if worked, err := w.Once(ctx); !worked || err != nil {
		t.Errorf("failing geocoder: %v, %v", worked, err)
	}
	if _, ok := s.resolved[home]; ok {
		t.Error("resolved after a failure")
	}
	// The coordinates themselves: "45." alone would match a timestamp's seconds.
	if !strings.Contains(logs.String(), "503") || strings.Contains(logs.String(), "45.76") || strings.Contains(logs.String(), "4.83") {
		t.Errorf("log %q", logs.String())
	}

	// The store fails to save: an error.
	s.pending, g.err, s.failSave = []Claim{home}, nil, errors.New("database down")
	if _, err := w.Once(ctx); err == nil {
		t.Error("no error when the address is not saved")
	}
}

// claimSignal tells each claim on a channel, for Run's goroutine.
type claimSignal struct{ claimed chan struct{} }

func (s claimSignal) ClaimGeocoding(context.Context, time.Time, time.Duration, int) (Claim, bool, error) {
	select {
	case s.claimed <- struct{}{}:
	default:
	}
	return Claim{}, false, nil
}

func (claimSignal) ResolveAddress(context.Context, Claim, string, time.Time) error { return nil }

func TestRunStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := claimSignal{claimed: make(chan struct{}, 1)}
	w := New(s, &fakeGeocoder{}, clock.Real{}, DefaultParams(), slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	select {
	case <-s.claimed: // the first claim comes at once
	case <-time.After(5 * time.Second):
		t.Fatal("Run never claimed")
	}
	cancel()
	<-done
}
