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

func TestAccountExportLifecycle(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	at := t0.Add(time.Hour)
	const lease = 10 * time.Minute

	if _, found, err := s.LatestAccountExport(ctx, a); found || err != nil {
		t.Fatalf("latest before any: %v, %v", found, err)
	}
	if _, ok, err := s.ClaimExport(ctx, at, lease); ok || err != nil {
		t.Fatalf("claim without an export: %v, %v", ok, err)
	}
	if _, err := s.OpenAccountExport(ctx, a); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("open without an export: %v", err)
	}

	created, err := s.CreateAccountExport(ctx, a, at)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != api.ExportPending || created.Error != "" || created.Bytes != 0 || !created.CreatedAt.Equal(at) ||
		!created.NotBefore.Equal(at) || !created.EndedAt.IsZero() {
		t.Errorf("created %+v", created)
	}
	if _, err := s.CreateAccountExport(ctx, a, at.Add(time.Minute)); !errors.Is(err, api.ErrExportActive) {
		t.Errorf("a second active export: %v", err)
	}
	latest, found, err := s.LatestAccountExport(ctx, a)
	if err != nil || !found || latest.ID != created.ID {
		t.Fatalf("latest: %+v, %v, %v", latest, found, err)
	}

	// The build: a file is opened for the claim, written, and saved with its size.
	job, ok, err := s.ClaimExport(ctx, at, lease)
	if err != nil || !ok {
		t.Fatalf("claim: %v, %v", ok, err)
	}
	if job.ID != created.ID || job.AccountID != a || !job.ClaimedAt.Equal(at) {
		t.Errorf("claimed %+v", job)
	}
	if _, ok, _ := s.ClaimExport(ctx, at.Add(time.Minute), lease); ok {
		t.Error("claimed again within the lease")
	}
	w, err := s.ExportFile(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	archive := []byte(`PK\x03\x04the account's archive`)
	n, err := w.Write(archive)
	if werr := w.Close(); werr != nil {
		t.Fatal(werr)
	}
	if err != nil || n != len(archive) {
		t.Fatalf("write: %d, %v", n, err)
	}
	if _, ok, _ := s.ClaimExport(ctx, at.Add(lease), lease); !ok {
		t.Error("an export not renewed by its build: not resumed after a stop")
	}
	// The lease of the first claim is over: another worker's claim took over.
	if err := s.SaveExport(ctx, job, at.Add(2*time.Minute), int64(len(archive))); !errors.Is(err, exporter.ErrLeaseLost) {
		t.Errorf("a claim lost: %v", err)
	}

	// Downloaded once built: its size is the file's.
	job, ok, err = s.ClaimExport(ctx, at.Add(2*lease), lease)
	if err != nil || !ok {
		t.Fatal(err)
	}
	w, err = s.ExportFile(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveExport(ctx, job, at.Add(lease).Add(time.Minute), int64(len(archive))); err != nil {
		t.Fatal(err)
	}
	latest, _, _ = s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportDone || latest.Bytes != int64(len(archive)) {
		t.Errorf("done %+v", latest)
	}
	r, err := s.OpenAccountExport(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if string(read) != string(archive) {
		t.Errorf("downloaded %q, want the archive", read)
	}

	// Asking again replaces the export held: the same export, asked for anew.
	again, err := s.CreateAccountExport(ctx, a, at.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != api.ExportPending || again.ID != created.ID || !again.CreatedAt.Equal(at.Add(3*time.Hour)) {
		t.Errorf("created again %+v", again)
	}
	latest, _, _ = s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportPending || latest.Bytes != 0 {
		t.Errorf("replaced %+v", latest)
	}
	if _, err := s.OpenAccountExport(ctx, a); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("the old archive still held: %v", err)
	}

	// A failure is retried, then fails; each build throws the interrupted one away.
	job, ok, _ = s.ClaimExport(ctx, at.Add(3*time.Hour), lease)
	if !ok {
		t.Fatal("the new export claimed")
	}
	w, err = s.ExportFile(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryExport(ctx, job, at.Add(4*time.Hour), 1); err != nil {
		t.Fatal(err)
	}
	latest, _, _ = s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportPending || latest.NotBefore != at.Add(4*time.Hour) {
		t.Errorf("after the retry %+v", latest)
	}
	retried, ok, _ := s.ClaimExport(ctx, at.Add(4*time.Hour), lease)
	if !ok || retried.Failures != 1 {
		t.Fatalf("retried claim %+v, %v", retried, ok)
	}
	w, err = s.ExportFile(ctx, retried)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	end := at.Add(5 * time.Hour)
	if err := s.FailExport(ctx, retried, end); err != nil {
		t.Fatal(err)
	}
	latest, _, _ = s.LatestAccountExport(ctx, a)
	if latest.Status != api.ExportFailed || latest.Error != "internal" || latest.EndedAt.IsZero() || latest.Bytes != 0 {
		t.Fatalf("failed %+v", latest)
	}
	if _, ok, _ := s.ClaimExport(ctx, end.Add(lease), lease); ok {
		t.Error("a failed export claimed again")
	}
	if _, err := s.OpenAccountExport(ctx, a); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("a failed export held: %v", err)
	}

	// Another account's export is not this one's.
	other, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA2")
	if _, found, _ := s.LatestAccountExport(ctx, other); found {
		t.Error("another account's export")
	}
	if _, err := s.CreateAccountExport(ctx, other, at); err != nil {
		t.Fatal(err)
	}
	job, ok, _ = s.ClaimExport(ctx, at.Add(lease), lease)
	if !ok {
		t.Fatal("the other account's export claimed")
	}
	w, err = s.ExportFile(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveExport(ctx, job, at.Add(2*lease), 42); err != nil {
		t.Fatal(err)
	}
	var oid uint32
	if err := s.readInAccount(ctx, other, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT oid FROM account_exports WHERE account_id = $1`, other).Scan(&oid)
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pg_largeobject_metadata WHERE oid = $1`, oid).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Error("the other account's archive is not held")
	}
}

// An interrupted build leaves its archive half written: the next claim throws it away.
func TestAccountExportResumesClean(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA3")
	at := t0.Add(time.Hour)
	const lease = 10 * time.Minute

	created, err := s.CreateAccountExport(ctx, a, at)
	if err != nil {
		t.Fatal(err)
	}
	// The build stops halfway: the archive of the claim is half written, the file
	// closed by the worker.
	job, ok, _ := s.ClaimExport(ctx, at, lease)
	if !ok {
		t.Fatal("not claimed")
	}
	w, err := s.ExportFile(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`PKx03x04half an archive`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var oid *uint32
	if err := s.readInAccount(ctx, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT oid FROM account_exports WHERE account_id = $1`, a).Scan(&oid)
	}); err != nil {
		t.Fatal(err)
	}
	if oid == nil {
		t.Fatal("no archive held by the interrupted build")
	}

	// The worker stopped: its lease over, the export is built again, the half-written
	// archive thrown away.
	resumed := at.Add(2 * lease)
	job.ClaimedAt = resumed
	if _, ok, _ := s.ClaimExport(ctx, resumed, lease); !ok {
		t.Fatal("not resumed")
	}
	if job.ID != created.ID {
		t.Errorf("resumed %+v", job)
	}
	w, err = s.ExportFile(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pg_largeobject_metadata WHERE oid = $1`, oid).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("the interrupted build's archive is kept")
	}
}
