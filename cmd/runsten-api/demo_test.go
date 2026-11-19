package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/demo"
	"runsten/internal/store"
)

type fakeHistory struct {
	account, vehicle string
	snaps            []store.HistoricSnapshot
	err              error
}

func (f *fakeHistory) SaveHistory(_ context.Context, account, vehicle string, snaps []store.HistoricSnapshot) error {
	f.account, f.vehicle, f.snaps = account, vehicle, snaps
	return f.err
}

type fakeRebuild struct {
	account, vehicle string
	net              core.NetCapacity
	calls            int
	err              error
}

func (f *fakeRebuild) RebuildWith(_ context.Context, account, vehicle string, net core.NetCapacity) (core.Result, error) {
	f.account, f.vehicle, f.net = account, vehicle, net
	f.calls++
	return core.Result{}, f.err
}

// TestDemoBackfill: the last four weeks of the demo vehicle are saved in order, up to the
// time given, then derived from scratch with the net capacity given; a failure of either is
// returned.
func TestDemoBackfill(t *testing.T) {
	week, err := demo.New()
	if err != nil {
		t.Fatal(err)
	}
	to := demo.Origin.Add(20*demo.Week + 36*time.Hour)
	hist, rebuild := &fakeHistory{}, &fakeRebuild{}
	d := demoVehicle{week: week, store: hist, deriver: rebuild}
	if d.VIN() != demo.VIN {
		t.Errorf("VIN %q", d.VIN())
	}
	net := func(core.Snapshot) (float64, bool) { return 64, true }
	if err := d.Backfill(context.Background(), "acc", "veh", to, net); err != nil {
		t.Fatal(err)
	}
	if hist.account != "acc" || hist.vehicle != "veh" || rebuild.account != "acc" || rebuild.vehicle != "veh" || rebuild.calls != 1 {
		t.Errorf("history of %s/%s, rebuilt %s/%s %d times", hist.account, hist.vehicle, rebuild.account, rebuild.vehicle, rebuild.calls)
	}
	if v, ok := rebuild.net(core.Snapshot{}); !ok || v != 64 {
		t.Errorf("net capacity %v, %v", v, ok)
	}
	if len(hist.snaps) < 2000 {
		t.Fatalf("%d snapshots", len(hist.snaps))
	}
	first, last := hist.snaps[0], hist.snaps[len(hist.snaps)-1]
	if !first.FetchedAt.Equal(to.Add(-demo.HistoryDays*24*time.Hour)) || last.FetchedAt.After(to) || to.Sub(last.FetchedAt) > demo.ParkedEvery {
		t.Errorf("from %v to %v, want the four weeks before %v", first.FetchedAt, last.FetchedAt, to)
	}
	for i, s := range hist.snaps[1:] {
		if !s.FetchedAt.After(hist.snaps[i].FetchedAt) || s.Kind.Source != demo.Source || len(s.Hash) == 0 || len(s.Payload) == 0 {
			t.Fatalf("snapshot %d: %+v", i+1, s.FetchedAt)
		}
	}

	hist.err = errors.New("disk full")
	rebuild.calls = 0
	if err := d.Backfill(context.Background(), "acc", "veh", to, net); !errors.Is(err, hist.err) || rebuild.calls != 0 {
		t.Errorf("a history that fails: %v, rebuilt %d times", err, rebuild.calls)
	}
	hist.err, rebuild.err = nil, errors.New("no events")
	if err := d.Backfill(context.Background(), "acc", "veh", to, net); !errors.Is(err, rebuild.err) {
		t.Errorf("a derivation that fails: %v", err)
	}
}
