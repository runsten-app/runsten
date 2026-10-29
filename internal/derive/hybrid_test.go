package derive

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/volvo"
)

// v60 reads the stored responses of a real Volvo V60 plug-in hybrid, as the store keeps
// them, one JSON object per line (endpoint, fetched_at, checked_at, payload), from the
// directory RUNSTEN_V60_FIXTURES names. They are a user's, so never in the repository:
// without it, the test is skipped, and scenarios/v60-phev.yaml stands in for them.
func v60(t *testing.T, name string) []Snapshot {
	t.Helper()
	dir := os.Getenv("RUNSTEN_V60_FIXTURES")
	if dir == "" {
		t.Skip("RUNSTEN_V60_FIXTURES not set: no real V60's responses")
	}
	f, err := os.Open(filepath.Join(dir, name+".jsonl")) //nolint:gosec // a directory the developer names
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r struct {
			Endpoint  string          `json:"endpoint"`
			FetchedAt time.Time       `json:"fetched_at"`
			CheckedAt time.Time       `json:"checked_at"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, Snapshot{
			Kind: core.Kind{Source: volvo.Source, Endpoint: r.Endpoint}, FetchedAt: r.FetchedAt, CheckedAt: r.CheckedAt, Payload: r.Payload,
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// storeAt is a store holding the snapshots as they stood at now: those fetched by then,
// read again until now at the latest.
func storeAt(snaps []Snapshot, now time.Time, into *memStore) {
	v := into.vehicle("v")
	clear(v.rows)
	for _, s := range snaps {
		if s.FetchedAt.After(now) {
			continue
		}
		if s.CheckedAt.After(now) {
			s.CheckedAt = now
		}
		v.rows[s.Kind] = append(v.rows[s.Kind], s)
	}
}

// Two real V60 plug-in hybrids: every charge they made was seen on its plug, and their
// engines, holding an empty battery a point up and down on a 41 km trip, made none up.
// The collector's incremental derivation finds what a rebuild does.
func TestRealPlugInHybrids(t *testing.T) {
	type charge struct{ start, end float64 }
	tests := []struct {
		name    string
		trips   int
		charges []charge
	}{
		{"my2021", 20, []charge{{35, 100}, {7, 100}, {19, 100}, {9, 82}, {7, 100}}},
		{"my2027", 8, []charge{{1, 19}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snaps := v60(t, tt.name)
			ctx := context.Background()

			st := newMemStore()
			storeAt(snaps, snaps[len(snaps)-1].CheckedAt.Add(time.Hour), st)
			full, err := New(st, volvo.Normalizer{}, core.DefaultParams(), nil, quiet).Rebuild(ctx, "a", "v")
			if err != nil {
				t.Fatal(err)
			}
			if len(full.Trips) != tt.trips {
				t.Errorf("%d trips, want %d", len(full.Trips), tt.trips)
			}
			var got []charge
			for _, c := range full.Charges {
				if c.Reconstructed {
					t.Errorf("reconstructed charge %+v", c)
				}
				got = append(got, charge{c.StartSoC.V, c.EndSoC.V})
			}
			if !slices.Equal(got, tt.charges) {
				t.Errorf("charges %v, want %v", got, tt.charges)
			}

			var times []time.Time
			for _, s := range snaps {
				times = append(times, s.FetchedAt, s.CheckedAt)
			}
			slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
			times = slices.Compact(times)
			inc := newMemStore()
			d := New(inc, volvo.Normalizer{}, core.DefaultParams(), nil, quiet)
			for _, now := range times {
				storeAt(snaps, now, inc)
				if err := d.Update(ctx, "a", "v"); err != nil {
					t.Fatal(err)
				}
			}
			v := inc.vehicle("v")
			if !reflect.DeepEqual(v.trips, full.Trips) || !reflect.DeepEqual(v.charges, full.Charges) {
				t.Errorf("incremental:\n%+v\n%+v\nrebuild:\n%+v\n%+v", v.trips, v.charges, full.Trips, full.Charges)
			}
		})
	}
}

// asElectric reads a plug-in hybrid's responses as a battery electric vehicle's.
type asElectric struct{ volvo.Normalizer }

func (n asElectric) Normalize(k core.Kind, raw []byte) (core.Snapshot, error) {
	s, err := n.Normalizer.Normalize(k, raw)
	if s.PlugInHybrid() {
		s.Powertrain.V = core.Electric
	}
	return s, err //nolint:wrapcheck // the normalizer's own
}

// The simulated V60 drives in charge mode between two readings: its SoC rises, the
// cable disconnected, and no charge is made of it; read as an electric car's, the same
// responses would make one. Its charge on a socket is seen.
func TestSimulatedPlugInHybrid(t *testing.T) {
	st, d, _ := play(t, "v60-phev.yaml", 30*time.Hour)
	checkRebuild(t, st, d)
	v := st.vehicle("v")
	if len(v.charges) != 1 || v.charges[0].Reconstructed || !near(v.charges[0].EndSoC, 60, 0.5) {
		logEvents(t, st)
		t.Fatalf("charges %+v", v.charges)
	}
	var revealed bool
	for _, tr := range v.trips {
		revealed = revealed || tr.Reconstructed
	}
	if len(v.trips) != 2 || !revealed {
		logEvents(t, st)
		t.Errorf("trips %+v: want the morning's and the one the odometer reveals", v.trips)
	}

	res, err := New(st, asElectric{}, core.DefaultParams(), nil, quiet).Rebuild(context.Background(), "a", "v")
	if err != nil {
		t.Fatal(err)
	}
	var made bool
	for _, c := range res.Charges {
		made = made || c.Reconstructed
	}
	if !made {
		t.Errorf("read as an electric car's, no charge made up: the scenario misses its point: %+v", res.Charges)
	}
}
