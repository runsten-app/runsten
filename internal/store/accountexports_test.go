package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/api"
	"runsten/internal/exporter"
)

func TestAccountExportLifecycle(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	at := t0.Add(time.Hour)
	const lease = 5 * time.Minute

	if _, found, err := s.LatestAccountExport(ctx, a); found || err != nil {
		t.Fatalf("latest before any: %v, %v", found, err)
	}
	if _, ok, err := s.ClaimExport(ctx, at, lease); ok || err != nil {
		t.Fatalf("claim without an export: %v, %v", ok, err)
	}

	created, err := s.CreateAccountExport(ctx, a, at)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != api.ExportPending || created.Error != "" || created.Bytes != 0 || !created.CreatedAt.Equal(at) ||
		!created.NotBefore.Equal(at) || !created.EndedAt.IsZero() || !created.ExpiresAt.IsZero() {
		t.Errorf("created %+v", created)
	}
	if _, err := s.CreateAccountExport(ctx, a, at.Add(time.Minute)); !errors.Is(err, api.ErrExportActive) {
		t.Errorf("a second active export: %v", err)
	}
	if held, err := s.HeldExports(ctx); err != nil || !held[created.ID] {
		t.Errorf("a pending export not held: %v, %v", held, err)
	}

	// The build: each renewal keeps the claim, the bytes written so far read as progress.
	job, ok, err := s.ClaimExport(ctx, at, lease)
	if err != nil || !ok || job.ID != created.ID || job.AccountID != a || !job.ClaimedAt.Equal(at) {
		t.Fatalf("claim: %+v, %v, %v", job, ok, err)
	}
	if _, ok, _ := s.ClaimExport(ctx, at.Add(time.Minute), lease); ok {
		t.Error("claimed again within the lease")
	}
	if err := s.StartExport(ctx, job, at); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewExport(ctx, job, at.Add(4*time.Minute), 8<<20); err != nil {
		t.Fatal(err)
	}
	if latest, _, _ := s.LatestAccountExport(ctx, a); latest.Status != api.ExportRunning || latest.Bytes != 8<<20 {
		t.Errorf("while running %+v", latest)
	}
	// Renewed 4 minutes in, the claim holds past the lease of its start.
	if _, ok, _ := s.ClaimExport(ctx, at.Add(lease+time.Minute), lease); ok {
		t.Error("a renewed claim taken over")
	}
	expires := at.Add(7 * 24 * time.Hour)
	if err := s.SaveExport(ctx, job, at.Add(7*time.Minute), 9<<20, expires); err != nil {
		t.Fatal(err)
	}
	latest, _, _ := s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportDone || latest.Bytes != 9<<20 || !latest.ExpiresAt.Equal(expires) ||
		!latest.EndedAt.Equal(at.Add(7*time.Minute)) {
		t.Errorf("done %+v", latest)
	}

	// Asking again replaces the export held, under another id: the old one is no longer
	// held, its archive no export's.
	again, err := s.CreateAccountExport(ctx, a, at.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != api.ExportPending || again.ID == created.ID || !again.CreatedAt.Equal(at.Add(3*time.Hour)) {
		t.Errorf("created again %+v", again)
	}
	if held, _ := s.HeldExports(ctx); held[created.ID] || !held[again.ID] {
		t.Errorf("held after the replacement: %v", held)
	}

	// A failure is retried, then fails: a failed export holds no archive.
	job, ok, _ = s.ClaimExport(ctx, at.Add(3*time.Hour), lease)
	if !ok {
		t.Fatal("the new export claimed")
	}
	if err := s.RetryExport(ctx, job, at.Add(4*time.Hour), 1); err != nil {
		t.Fatal(err)
	}
	latest, _, _ = s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportPending || !latest.NotBefore.Equal(at.Add(4*time.Hour)) || latest.Bytes != 0 {
		t.Errorf("after the retry %+v", latest)
	}
	retried, ok, _ := s.ClaimExport(ctx, at.Add(4*time.Hour), lease)
	if !ok || retried.Failures != 1 {
		t.Fatalf("retried claim %+v, %v", retried, ok)
	}
	end := at.Add(5 * time.Hour)
	if err := s.FailExport(ctx, retried, end); err != nil {
		t.Fatal(err)
	}
	latest, _, _ = s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportFailed || latest.Error != "internal" || latest.EndedAt.IsZero() || latest.Bytes != 0 {
		t.Fatalf("failed %+v", latest)
	}
	if held, _ := s.HeldExports(ctx); held[again.ID] {
		t.Error("a failed export held")
	}
	if _, ok, _ := s.ClaimExport(ctx, end.Add(lease), lease); ok {
		t.Error("a failed export claimed again")
	}
}

// A claim gone a lease without a renewal is taken over: the stopped worker's writes
// are refused.
func TestAccountExportTakenOver(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA3")
	at := t0.Add(time.Hour)
	const lease = 5 * time.Minute

	if _, err := s.CreateAccountExport(ctx, a, at); err != nil {
		t.Fatal(err)
	}
	stopped, _, _ := s.ClaimExport(ctx, at, lease)
	if err := s.StartExport(ctx, stopped, at); err != nil {
		t.Fatal(err)
	}
	resumed, ok, _ := s.ClaimExport(ctx, at.Add(lease), lease)
	if !ok || resumed.ID != stopped.ID {
		t.Fatalf("not resumed: %+v", resumed)
	}
	if err := s.RenewExport(ctx, stopped, at.Add(lease), 64); !errors.Is(err, exporter.ErrLeaseLost) {
		t.Errorf("a renewal of a claim taken over: %v", err)
	}
	if err := s.SaveExport(ctx, stopped, at.Add(lease), 64, at.Add(time.Hour)); !errors.Is(err, exporter.ErrLeaseLost) {
		t.Errorf("a save of a claim taken over: %v", err)
	}
	if err := s.StartExport(ctx, resumed, at.Add(lease)); err != nil {
		t.Fatal(err)
	}
}

// heldExport is a vehicle's new account, its export built at at and held until
// expires.
func heldExport(t *testing.T, s *Store, vin string, at, expires time.Time) (string, string) {
	t.Helper()
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", vin)
	if _, err := s.CreateAccountExport(ctx, a, at); err != nil {
		t.Fatal(err)
	}
	job, ok, _ := s.ClaimExport(ctx, at, time.Minute)
	if !ok {
		t.Fatal("not claimed")
	}
	if err := s.SaveExport(ctx, job, at, 11, expires); err != nil {
		t.Fatal(err)
	}
	return a, job.ID
}

// Exports past their time go, across the accounts; the others stay.
func TestAccountExportExpires(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	at := t0.Add(time.Hour)
	old, oldID := heldExport(t, s, "YV1AAAAAAAAAAAAA1", at, at.Add(time.Hour))
	kept, keptID := heldExport(t, s, "YV1AAAAAAAAAAAAA2", at, at.Add(48*time.Hour))

	if n, err := s.ExpireExports(ctx, at.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("expired %d, %v", n, err)
	}
	if _, found, _ := s.LatestAccountExport(ctx, old); found {
		t.Error("an expired export kept")
	}
	if _, found, _ := s.LatestAccountExport(ctx, kept); !found {
		t.Error("an export in its time expired")
	}
	if held, _ := s.HeldExports(ctx); held[oldID] || !held[keptID] {
		t.Errorf("held %v", held)
	}
}

// Another account's export is not this one's.
func TestAccountExportIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	at := t0.Add(time.Hour)
	_, id := heldExport(t, s, "YV1AAAAAAAAAAAAA1", at, at.Add(time.Hour))
	other, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA2")

	if _, found, _ := s.LatestAccountExport(ctx, other); found {
		t.Error("another account's export")
	}
	// A claim written for the export under another account is refused.
	if err := s.RenewExport(ctx, exporter.Job{ID: id, AccountID: other, ClaimedAt: at}, at, 1); !errors.Is(err, exporter.ErrLeaseLost) {
		t.Errorf("a renewal across accounts: %v", err)
	}
}
