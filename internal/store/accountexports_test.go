package store

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/exporter"
)

// writeParts writes the archive in parts of size, as the worker does.
func writeParts(t *testing.T, s *Store, job exporter.Job, at time.Time, archive []byte, size int) {
	t.Helper()
	ctx := context.Background()
	if err := s.ResetExport(ctx, job, at); err != nil {
		t.Fatal(err)
	}
	for start := 0; start < len(archive); start += size {
		end := min(start+size, len(archive))
		if err := s.WriteExportPart(ctx, job, int64(start), archive[start:end], at); err != nil {
			t.Fatal(err)
		}
	}
}

// admin runs sql as the database's owner, out of row-level security.
func admin(t *testing.T, dbURL, sql string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// countParts counts the parts of every account.
func countParts(t *testing.T, dbURL string) int {
	t.Helper()
	return admin(t, dbURL, `SELECT count(*) FROM account_export_parts`)
}

func TestAccountExportLifecycle(t *testing.T) {
	s, dbURL := testStore(t)
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
	if _, _, err := s.OpenAccountExport(ctx, a); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("open without an export: %v", err)
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

	// The build: each part renews the claim, the bytes written so far read as progress.
	job, ok, err := s.ClaimExport(ctx, at, lease)
	if err != nil || !ok || job.ID != created.ID || job.AccountID != a || !job.ClaimedAt.Equal(at) {
		t.Fatalf("claim: %+v, %v, %v", job, ok, err)
	}
	if _, ok, _ := s.ClaimExport(ctx, at.Add(time.Minute), lease); ok {
		t.Error("claimed again within the lease")
	}
	archive := []byte("PK\x03\x04the account's archive, in parts")
	writeParts(t, s, job, at.Add(4*time.Minute), archive[:8], 4)
	if latest, _, _ := s.LatestAccountExport(ctx, a); latest.Status != api.ExportRunning || latest.Bytes != 8 {
		t.Errorf("while running %+v", latest)
	}
	// Renewed 4 minutes in, the claim holds past the lease of its start.
	if _, ok, _ := s.ClaimExport(ctx, at.Add(lease+time.Minute), lease); ok {
		t.Error("a claim renewed by its parts taken over")
	}
	writeParts(t, s, job, at.Add(6*time.Minute), archive, 4)
	expires := at.Add(7 * 24 * time.Hour)
	if err := s.SaveExport(ctx, job, at.Add(7*time.Minute), int64(len(archive)), expires); err != nil {
		t.Fatal(err)
	}
	latest, _, _ := s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportDone || latest.Bytes != int64(len(archive)) || !latest.ExpiresAt.Equal(expires) {
		t.Errorf("done %+v", latest)
	}

	// Read whole, then from an offset within a part, then past its end.
	held, r, err := s.OpenAccountExport(ctx, a)
	if err != nil || held.ID != created.ID {
		t.Fatalf("open: %+v, %v", held, err)
	}
	if read, err := io.ReadAll(r); err != nil || string(read) != string(archive) {
		t.Errorf("downloaded %q, %v", read, err)
	}
	if _, err := r.Seek(6, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if read, err := io.ReadAll(r); err != nil || string(read) != string(archive[6:]) {
		t.Errorf("from 6: %q, %v", read, err)
	}
	if end, _ := r.Seek(0, io.SeekEnd); end != int64(len(archive)) {
		t.Errorf("end %d", end)
	}

	// Asking again replaces the export held, under another id, its parts thrown away:
	// a read of the old one fails.
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateAccountExport(ctx, a, at.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != api.ExportPending || again.ID == created.ID || !again.CreatedAt.Equal(at.Add(3*time.Hour)) {
		t.Errorf("created again %+v", again)
	}
	if countParts(t, dbURL) != 0 {
		t.Error("the old archive's parts are kept")
	}
	if _, err := io.ReadAll(r); !errors.Is(err, errArchiveGone) {
		t.Errorf("read of a replaced archive: %v", err)
	}
	if _, _, err := s.OpenAccountExport(ctx, a); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("the old archive still held: %v", err)
	}

	// A failure is retried, then fails; each throws what the build wrote away.
	job, ok, _ = s.ClaimExport(ctx, at.Add(3*time.Hour), lease)
	if !ok {
		t.Fatal("the new export claimed")
	}
	writeParts(t, s, job, at.Add(3*time.Hour), archive, 8)
	if err := s.RetryExport(ctx, job, at.Add(4*time.Hour), 1); err != nil {
		t.Fatal(err)
	}
	latest, _, _ = s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportPending || !latest.NotBefore.Equal(at.Add(4*time.Hour)) || latest.Bytes != 0 || countParts(t, dbURL) != 0 {
		t.Errorf("after the retry %+v", latest)
	}
	retried, ok, _ := s.ClaimExport(ctx, at.Add(4*time.Hour), lease)
	if !ok || retried.Failures != 1 {
		t.Fatalf("retried claim %+v, %v", retried, ok)
	}
	writeParts(t, s, retried, at.Add(4*time.Hour), archive, 8)
	end := at.Add(5 * time.Hour)
	if err := s.FailExport(ctx, retried, end); err != nil {
		t.Fatal(err)
	}
	latest, _, _ = s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportFailed || latest.Error != "internal" || latest.EndedAt.IsZero() || latest.Bytes != 0 || countParts(t, dbURL) != 0 {
		t.Fatalf("failed %+v", latest)
	}
	if _, ok, _ := s.ClaimExport(ctx, end.Add(lease), lease); ok {
		t.Error("a failed export claimed again")
	}
}

// A claim gone a lease without a part is taken over: the stopped worker's writes are
// refused, the new one starts again from nothing.
func TestAccountExportTakenOver(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA3")
	at := t0.Add(time.Hour)
	const lease = 5 * time.Minute

	if _, err := s.CreateAccountExport(ctx, a, at); err != nil {
		t.Fatal(err)
	}
	stopped, _, _ := s.ClaimExport(ctx, at, lease)
	writeParts(t, s, stopped, at, []byte("PK\x03\x04half an archive"), 4)

	resumed, ok, _ := s.ClaimExport(ctx, at.Add(lease), lease)
	if !ok || resumed.ID != stopped.ID {
		t.Fatalf("not resumed: %+v", resumed)
	}
	if err := s.WriteExportPart(ctx, stopped, 64, []byte("late"), at.Add(lease)); !errors.Is(err, exporter.ErrLeaseLost) {
		t.Errorf("a part of a claim taken over: %v", err)
	}
	if err := s.SaveExport(ctx, stopped, at.Add(lease), 64, at.Add(time.Hour)); !errors.Is(err, exporter.ErrLeaseLost) {
		t.Errorf("a save of a claim taken over: %v", err)
	}
	if err := s.ResetExport(ctx, resumed, at.Add(lease)); err != nil {
		t.Fatal(err)
	}
	if countParts(t, dbURL) != 0 {
		t.Error("the interrupted build's parts are kept")
	}
}

// heldExport is a vehicle's new account, its archive built at at and held until
// expires.
func heldExport(t *testing.T, s *Store, vin string, at, expires time.Time) string {
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
	writeParts(t, s, job, at, []byte("PK\x03\x04archive"), 4)
	if err := s.SaveExport(ctx, job, at, 11, expires); err != nil {
		t.Fatal(err)
	}
	return a
}

// Archives past their time go, their parts with them; the others stay. The account's
// deletion takes its archive.
func TestAccountExportExpires(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	at := t0.Add(time.Hour)
	old := heldExport(t, s, "YV1AAAAAAAAAAAAA1", at, at.Add(time.Hour))
	kept := heldExport(t, s, "YV1AAAAAAAAAAAAA2", at, at.Add(48*time.Hour))

	if n, err := s.ExpireExports(ctx, at.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("expired %d, %v", n, err)
	}
	if _, found, _ := s.LatestAccountExport(ctx, old); found {
		t.Error("an expired export kept")
	}
	if _, found, _ := s.LatestAccountExport(ctx, kept); !found {
		t.Error("an export in its time expired")
	}
	if n := countParts(t, dbURL); n != 3 {
		t.Errorf("%d parts left, want the kept archive's 3", n)
	}

	admin(t, dbURL, `WITH gone AS (DELETE FROM accounts WHERE id = $1 RETURNING 1) SELECT count(*) FROM gone`, kept)
	if n := countParts(t, dbURL); n != 0 {
		t.Errorf("%d parts left by a deleted account", n)
	}
}

// Another account's archive is not this one's, even by its export's id.
func TestAccountExportIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	other, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA2")
	at := t0.Add(time.Hour)
	if _, err := s.CreateAccountExport(ctx, a, at); err != nil {
		t.Fatal(err)
	}
	job, _, _ := s.ClaimExport(ctx, at, time.Minute)
	writeParts(t, s, job, at, []byte("PK\x03\x04archive"), 4)
	if err := s.SaveExport(ctx, job, at, 11, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := s.LatestAccountExport(ctx, other); found {
		t.Error("another account's export")
	}
	if _, _, err := s.OpenAccountExport(ctx, other); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("another account's archive: %v", err)
	}
	stolen := &partReader{ctx: ctx, s: s, accountID: other, exportID: job.ID, size: 11}
	if _, err := io.ReadAll(stolen); !errors.Is(err, errArchiveGone) {
		t.Errorf("another account's parts read: %v", err)
	}
	// A part written for the account under another's claim is refused.
	if err := s.WriteExportPart(ctx, exporter.Job{ID: job.ID, AccountID: other, ClaimedAt: at}, 11, []byte("x"), at); !errors.Is(err, exporter.ErrLeaseLost) {
		t.Errorf("a part across accounts: %v", err)
	}
}
