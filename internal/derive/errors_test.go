package derive

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/volvo"
)

// scriptedStore fails where told to, and serves fixed snapshots.
type scriptedStore struct {
	cursorErr, snapErr, saveErr error
	snaps                       []Snapshot
	saved                       *core.Result
}

func (s *scriptedStore) DerivationCursor(context.Context, string, string) (core.Cursor, error) {
	return core.Cursor{}, s.cursorErr
}

func (s *scriptedStore) SnapshotsSince(context.Context, string, string, time.Time, []volvo.Endpoint) ([]Snapshot, error) {
	return s.snaps, s.snapErr
}

func (s *scriptedStore) SnapshotsBetween(context.Context, string, string, time.Time, time.Time, []volvo.Endpoint, int) ([]Snapshot, error) {
	return s.snaps, s.snapErr
}

func (s *scriptedStore) SaveDerivation(_ context.Context, _, _ string, _ time.Time, res core.Result) error {
	s.saved = &res
	return s.saveErr
}

func TestStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	for name, st := range map[string]*scriptedStore{
		"cursor":    {cursorErr: boom},
		"snapshots": {snapErr: boom},
		"save":      {saveErr: boom},
	} {
		t.Run(name, func(t *testing.T) {
			if err := New(st, core.DefaultParams(), nil, quiet).Update(context.Background(), "a", "v"); !errors.Is(err, boom) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestReadingsErrors(t *testing.T) {
	boom := errors.New("boom")
	at := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	d := New(&scriptedStore{snapErr: boom}, core.DefaultParams(), nil, quiet)
	if _, err := d.Readings(context.Background(), "a", "v", at, at.Add(time.Hour), 10); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	// An unreadable response is skipped, as the derivation skips it.
	d = New(&scriptedStore{snaps: []Snapshot{{Endpoint: volvo.EnergyState, FetchedAt: at, CheckedAt: at, Payload: []byte("{")}}}, core.DefaultParams(), nil, quiet)
	if got, err := d.Readings(context.Background(), "a", "v", at, at.Add(time.Hour), 10); err != nil || len(got) != 0 {
		t.Errorf("readings = %+v, %v", got, err)
	}
}

type failingCapacities struct{ err error }

func (f failingCapacities) NetCapacity(context.Context, string, string) (core.NetCapacity, error) {
	return nil, f.err
}

func TestCapacitiesError(t *testing.T) {
	boom := errors.New("boom")
	st := &scriptedStore{}
	if err := New(st, core.DefaultParams(), failingCapacities{boom}, quiet).Update(context.Background(), "a", "v"); !errors.Is(err, boom) || st.saved != nil {
		t.Errorf("err = %v, saved %+v", err, st.saved)
	}
}

func TestUnreadableSnapshotSkipped(t *testing.T) {
	at := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	st := &scriptedStore{snaps: []Snapshot{
		{Endpoint: volvo.Odometer, FetchedAt: at, CheckedAt: at, Payload: []byte("{")},
		{
			Endpoint: volvo.Odometer, FetchedAt: at.Add(time.Hour), CheckedAt: at.Add(time.Hour),
			Payload: []byte(`{"data":{"odometer":{"timestamp":"x","unit":"km","value":1}}}`),
		},
	}}
	res, err := New(st, core.DefaultParams(), nil, quiet).Rebuild(context.Background(), "a", "v")
	if err != nil || st.saved == nil || len(res.Trips) != 0 {
		t.Errorf("rebuild: %+v, %v", res, err)
	}
}
