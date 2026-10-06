package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/collector"
	"runsten/internal/volvo"
)

// TestCalls checks the calls the collector counts: added up per account, API and hour,
// read across the accounts from a time, pruned after callsKept, and confined to their
// account.
func TestCalls(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	h := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	save := func(calls ...collector.Calls) {
		t.Helper()
		if err := s.SaveCalls(ctx, calls); err != nil {
			t.Fatal(err)
		}
	}

	save(collector.Calls{AccountID: a, API: volvo.APIEnergy, Hour: h.Add(-callsKept - time.Hour), N: 9})
	save(
		collector.Calls{AccountID: a, API: volvo.APIEnergy, Hour: h, N: 3},
		collector.Calls{AccountID: b, API: volvo.APILocation, Hour: h.Add(-2 * time.Hour), N: 1},
	)
	save(
		collector.Calls{AccountID: a, API: volvo.APIEnergy, Hour: h, N: 2},
		collector.Calls{AccountID: a, API: volvo.APIConnectedVehicle, Hour: h, N: 5},
	)

	got, err := s.Calls(ctx, h.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := []collector.Calls{
		{AccountID: a, API: volvo.APIConnectedVehicle, Hour: h, N: 5},
		{AccountID: a, API: volvo.APIEnergy, Hour: h, N: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("calls since 05:00 = %+v, want %+v", got, want)
	}
	all, err := s.Calls(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("every call kept = %+v: B's, and A's but the pruned hour", all)
	}

	for _, bad := range []collector.Calls{
		{AccountID: a, API: "commands", Hour: h, N: 1},
		{AccountID: a, API: volvo.APIEnergy, Hour: h.Add(time.Minute), N: 1},
		{AccountID: a, API: volvo.APIEnergy, Hour: h, N: 0},
	} {
		if err := s.SaveCalls(ctx, []collector.Calls{bad}); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}

	t.Run("confined to the account", func(t *testing.T) {
		var n int
		err := s.inAccount(ctx, b, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "UPDATE api_calls SET calls = 100 WHERE account_id = $1", a); err != nil {
				return err
			}
			return tx.QueryRow(ctx, "SELECT count(*) FROM api_calls").Scan(&n)
		})
		if err != nil || n != 1 {
			t.Errorf("account B sees %d rows, want its own: %v", n, err)
		}
		err = s.inAccount(ctx, b, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO api_calls (account_id, api, hour, calls) VALUES ($1, 'energy', $2, 1)", a, h)
			return err
		})
		if err == nil {
			t.Error("account B wrote A's calls")
		}
		got, err := s.Calls(ctx, h)
		if err != nil || len(got) != 2 || got[0].N != 5 {
			t.Errorf("A's calls changed by B: %+v, %v", got, err)
		}
	})
}
