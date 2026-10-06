package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/collector"
	"runsten/internal/oauth"
	"runsten/internal/volvo"
)

// TestCollectorStatus checks the status the collector writes: read with the vehicle,
// replaced but for a zero ReadAt or Failure, and confined to its account.
func TestCollectorStatus(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	read := func(account, vehicle string) *api.Collection {
		t.Helper()
		v, ok, err := s.Vehicle(ctx, account, vehicle)
		if err != nil || !ok {
			t.Fatalf("vehicle %s: %v, %v", vehicle, ok, err)
		}
		return v.Collection
	}
	save := func(st collector.Status) {
		t.Helper()
		if err := s.SaveStatus(ctx, st); err != nil {
			t.Fatal(err)
		}
	}

	if c := read(a, va); c != nil {
		t.Errorf("before any pass: %+v", c)
	}

	paris := time.FixedZone("CEST", 2*3600)
	save(collector.Status{
		AccountID: a, VehicleID: va, PassedAt: t0, Mode: "charging", ReadAt: t0, NextAt: t0.Add(time.Minute),
		PausedUntil: t0.Add(time.Hour), Quota: map[string]time.Time{volvo.APIEnergy: t0.Add(2 * time.Hour).In(paris)},
		Failure: collector.Failure{At: t0, Endpoint: volvo.EnergyState, Status: 403, Kind: collector.FailQuota},
	})
	want := &api.Collection{
		PassedAt: t0, Mode: "charging", ReadAt: t0, NextAt: t0.Add(time.Minute), PausedUntil: t0.Add(time.Hour),
		Quota:   map[string]time.Time{volvo.APIEnergy: t0.Add(2 * time.Hour)},
		Failure: api.CollectionFailure{At: t0, Endpoint: "energy-state", Status: 403, Kind: "quota"},
	}
	if got := read(a, va); !reflect.DeepEqual(got, want) {
		t.Errorf("read %+v, want %+v", got, want)
	}

	// A restarted collector knows neither the latest reading nor the latest failure.
	t1 := t0.Add(10 * time.Minute)
	save(collector.Status{AccountID: a, VehicleID: va, PassedAt: t1, Mode: "parked"})
	want = &api.Collection{
		PassedAt: t1, Mode: "parked", ReadAt: t0, Quota: map[string]time.Time{},
		Failure: api.CollectionFailure{At: t0, Endpoint: "energy-state", Status: 403, Kind: "quota"},
	}
	if got := read(a, va); !reflect.DeepEqual(got, want) {
		t.Errorf("after a restart: %+v, want %+v", got, want)
	}

	// A new failure replaces the whole of the former, its missing status included.
	save(collector.Status{
		AccountID: a, VehicleID: va, PassedAt: t1, Mode: "parked",
		Failure: collector.Failure{At: t1, Kind: collector.FailToken},
	})
	if got := read(a, va).Failure; got != (api.CollectionFailure{At: t1, Kind: "token"}) {
		t.Errorf("new failure: %+v", got)
	}

	t.Run("the list reads each vehicle's own", func(t *testing.T) {
		conn, err := s.SaveConnection(ctx, a, oauth.Credentials{AccessToken: "token-a"})
		if err != nil {
			t.Fatal(err)
		}
		va2, err := s.AddVehicle(ctx, a, conn, "YV1AAAAAAAAAAAAA2")
		if err != nil {
			t.Fatal(err)
		}
		save(collector.Status{
			AccountID: a, VehicleID: va, PassedAt: t1, Mode: "parked",
			Quota: map[string]time.Time{volvo.APILocation: t1},
		})
		save(collector.Status{
			AccountID: a, VehicleID: va2, PassedAt: t1, Mode: "driving",
			Quota: map[string]time.Time{volvo.APIEnergy: t1},
		})
		vs, err := s.Vehicles(ctx, a)
		if err != nil || len(vs) != 2 {
			t.Fatalf("A's vehicles: %+v, %v", vs, err)
		}
		for _, v := range vs {
			if len(v.Collection.Quota) != 1 {
				t.Errorf("vehicle %s: quota %+v", v.VIN, v.Collection.Quota)
			}
		}
		save(collector.Status{AccountID: b, VehicleID: vb, PassedAt: t1, Mode: "driving"})
	})

	t.Run("another account's status is neither read nor written", func(t *testing.T) {
		if err := s.SaveStatus(ctx, collector.Status{AccountID: a, VehicleID: vb, PassedAt: t1, Mode: "parked"}); err == nil {
			t.Error("A wrote the status of B's vehicle")
		}
		if c := read(b, vb); c.Mode != "driving" {
			t.Errorf("B's status: %+v", c)
		}
		err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM collector_status").Scan(&n); err != nil {
				return err
			}
			if n != 2 {
				t.Errorf("A sees %d statuses", n)
			}
			tag, err := tx.Exec(ctx, "UPDATE collector_status SET mode = 'parked' WHERE vehicle_id = $1", vb)
			if err == nil && tag.RowsAffected() != 0 {
				t.Error("B's status updated")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

// TestEveryFailureKindStored: the table accepts every kind of failure the collector
// writes.
func TestEveryFailureKindStored(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	for i, kind := range []string{
		collector.FailQuota, collector.FailRateLimited, collector.FailRejected, collector.FailNotFound,
		collector.FailUnavailable, collector.FailToken, collector.FailKeyRefused, collector.FailOther,
	} {
		at := t0.Add(time.Duration(i) * time.Minute)
		err := s.SaveStatus(ctx, collector.Status{
			AccountID: a, VehicleID: va, PassedAt: at, Mode: "parked",
			Failure: collector.Failure{At: at, Endpoint: volvo.Odometer, Status: 401, Kind: kind},
		})
		if err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}
