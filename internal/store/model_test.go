package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/core"
)

// TestVehicleModel checks the columns of the user's choice: written, read with the
// vehicle, cleared, and confined to the account like the rest of the row.
func TestVehicleModel(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	model := func(account, vehicle string) (string, core.Value[float64]) {
		t.Helper()
		v, ok, err := s.Vehicle(ctx, account, vehicle)
		if err != nil || !ok {
			t.Fatalf("vehicle %s: %v, %v", vehicle, ok, err)
		}
		return v.VariantID, v.ACMaxKW
	}

	if id, kw := model(a, va); id != "" || kw.OK {
		t.Errorf("a new vehicle: %q, %+v", id, kw)
	}
	for _, tt := range []struct {
		name    string
		variant string
		charger core.Value[float64]
	}{
		{"a variant and a charger", "ex30-er-2024", core.Value[float64]{V: 11, OK: true}},
		{"a variant only", "ex30-twin-2024", core.Value[float64]{}},
		{"a charger only", "", core.Value[float64]{V: 22, OK: true}},
		{"back to the recognition", "", core.Value[float64]{}},
	} {
		if err := s.SetVehicleModel(ctx, a, va, tt.variant, tt.charger); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if id, kw := model(a, va); id != tt.variant || kw != tt.charger {
			t.Errorf("%s: read %q, %+v", tt.name, id, kw)
		}
	}
	// The list reads them too, each vehicle its own.
	if err := s.SetVehicleModel(ctx, a, va, "xc40-twin-2021", core.Value[float64]{V: 11, OK: true}); err != nil {
		t.Fatal(err)
	}
	vs, err := s.Vehicles(ctx, a)
	if err != nil || len(vs) != 1 || vs[0].VariantID != "xc40-twin-2021" || vs[0].ACMaxKW.V != 11 {
		t.Errorf("vehicles = %+v, %v", vs, err)
	}

	t.Run("another account's vehicle is not found, nor written", func(t *testing.T) {
		err := s.SetVehicleModel(ctx, a, vb, "ex30-er-2024", core.Value[float64]{V: 22, OK: true})
		if !errors.Is(err, api.ErrNotFound) {
			t.Errorf("write to B's vehicle: %v", err)
		}
		if id, kw := model(b, vb); id != "" || kw.OK {
			t.Errorf("B's vehicle written: %q, %+v", id, kw)
		}
		// Without the store's filter either: RLS hides the row.
		err = s.inAccount(ctx, a, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, "UPDATE vehicles SET variant_id = 'x', ac_max_kw = 7 WHERE id = $1", vb)
			if err == nil && tag.RowsAffected() != 0 {
				t.Error("B's vehicle updated")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := s.Vehicle(ctx, b, va); ok || err != nil {
			t.Errorf("B reads A's vehicle: %v, %v", ok, err)
		}
	})
	t.Run("the columns refuse what no choice is", func(t *testing.T) {
		for _, sql := range []string{
			"UPDATE vehicles SET variant_id = '' WHERE id = $1",
			"UPDATE vehicles SET ac_max_kw = 0 WHERE id = $1",
		} {
			err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, sql, va)
				return err
			})
			if err == nil {
				t.Errorf("%s: accepted", sql)
			}
		}
	})
}

// TestVehicleModelResetsDerivation checks that a new variant deletes the vehicle's
// derivation cursor, for its energies to be derived again on its net capacity, while a
// new charger, which only the costs read, keeps it.
func TestVehicleModelResetsDerivation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	settled := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	derived := func() {
		t.Helper()
		if err := s.SaveDerivation(ctx, a, va, time.Time{}, core.Result{Cursor: core.Cursor{Settled: settled, From: settled}}); err != nil {
			t.Fatal(err)
		}
	}
	cursor := func() bool {
		t.Helper()
		c, err := s.DerivationCursor(ctx, a, va)
		if err != nil {
			t.Fatal(err)
		}
		return !c.Settled.IsZero()
	}
	ex30 := core.Value[float64]{V: 11, OK: true}
	for _, tt := range []struct {
		name    string
		variant string
		charger core.Value[float64]
		kept    bool
	}{
		{"a variant chosen", "ex30-er-2024", core.Value[float64]{}, false},
		{"the same variant, a charger stated", "ex30-er-2024", ex30, true},
		{"another variant", "ex30-lfp-2024", ex30, false},
		{"back to the recognition", "", core.Value[float64]{}, false},
		{"still the recognition", "", core.Value[float64]{}, true},
	} {
		derived()
		if err := s.SetVehicleModel(ctx, a, va, tt.variant, tt.charger); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got := cursor(); got != tt.kept {
			t.Errorf("%s: cursor kept %v, want %v", tt.name, got, tt.kept)
		}
	}
}
