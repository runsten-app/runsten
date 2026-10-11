package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/aximote"
	"runsten/internal/core"
	"runsten/internal/importer"
	"runsten/internal/platform/clock"
	"runsten/internal/simulator/aximoteapi"
)

const testImportToken = "axm_pat_AAAAAAAAAAAAAAAAAAAAAAAAAA"

func aximoteRequest() api.ImportRequest {
	return api.ImportRequest{Source: "aximote", SourceVehicle: "veh-1", Token: testImportToken}
}

func TestImportJobLifecycle(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	at := t0.Add(time.Hour)
	const lease = 2 * time.Minute

	if _, found, err := s.LatestImportJob(ctx, a, v); found || err != nil {
		t.Fatalf("latest before any: %v, %v", found, err)
	}
	if _, ok, err := s.ClaimImport(ctx, at, lease); ok || err != nil {
		t.Fatalf("claim without a job: %v, %v", ok, err)
	}
	created, err := s.CreateImportJob(ctx, a, v, aximoteRequest(), at)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != api.ImportPending || created.Step != "trips" || !created.CreatedAt.Equal(at) || !created.Cutoff.IsZero() {
		t.Errorf("created %+v", created)
	}
	if _, err := s.CreateImportJob(ctx, a, v, aximoteRequest(), at); !errors.Is(err, api.ErrImportActive) {
		t.Errorf("a second active job: %v", err)
	}
	var sealed []byte
	err = s.inAccount(ctx, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT token FROM import_jobs").Scan(&sealed)
	})
	if err != nil || bytes.Contains(sealed, []byte(testImportToken)) {
		t.Fatalf("token not sealed: %v", err)
	}

	job, ok, err := s.ClaimImport(ctx, at, lease)
	if err != nil || !ok {
		t.Fatalf("claim: %v, %v", ok, err)
	}
	if job.ID != created.ID || job.AccountID != a || job.VehicleID != v || job.Source != "aximote" ||
		job.SourceVehicle != "veh-1" || job.Token != testImportToken || job.Step != importer.StepTrips || job.Cursor != "" {
		t.Errorf("claimed %+v", job)
	}
	if _, ok, _ := s.ClaimImport(ctx, at.Add(time.Minute), lease); ok {
		t.Error("claimed again within the lease")
	}

	before := t0.Add(-24 * time.Hour)
	if cutoff, err := s.ImportCutoff(ctx, a, v, at); err != nil || !cutoff.Equal(at) {
		t.Errorf("cutoff without a snapshot: %v, %v", cutoff, err)
	}
	page := importer.Page{
		Trips:   []importer.ImportedTrip{importedTrip("aximote", "trip-1", before)},
		Invalid: 2, Step: importer.StepCharges, Cursor: "", At: at.Add(time.Minute),
	}
	res, err := s.SaveImportPage(ctx, job, page)
	if err != nil || res.Saved != 1 || res.Skipped != 0 {
		t.Fatalf("page: %+v, %v", res, err)
	}
	job.Step, job.ClaimedAt = page.Step, page.At

	// The previous claim writes nothing more.
	stale := job
	stale.ClaimedAt = at
	if _, err := s.SaveImportPage(ctx, stale, importer.Page{Step: importer.StepCharges, At: at}); !errors.Is(err, importer.ErrLeaseLost) {
		t.Errorf("stale claim: %v", err)
	}

	// A 429: back to pending, until then.
	retryAt := at.Add(10 * time.Minute)
	if err := s.RetryImport(ctx, job, retryAt, 0); err != nil {
		t.Fatal(err)
	}
	latest, _, _ := s.LatestImportJob(ctx, a, v)
	if latest.Status != api.ImportPending || !latest.NotBefore.Equal(retryAt) || latest.Imported != 1 || latest.Invalid != 2 ||
		latest.Step != "charges" || latest.Cutoff.IsZero() {
		t.Errorf("after the retry %+v", latest)
	}
	if _, ok, _ := s.ClaimImport(ctx, retryAt.Add(-time.Second), lease); ok {
		t.Error("claimed before not_before")
	}
	job, ok, err = s.ClaimImport(ctx, retryAt, lease)
	if err != nil || !ok || job.Step != importer.StepCharges {
		t.Fatalf("claim after the wait: %+v, %v, %v", job, ok, err)
	}

	// The worker stopped: its lease over, another resumes the job.
	resumed, ok, err := s.ClaimImport(ctx, retryAt.Add(lease), lease)
	if err != nil || !ok || resumed.ID != job.ID {
		t.Fatalf("resume: %v, %v", ok, err)
	}
	if err := s.RetryImport(ctx, job, retryAt, 1); !errors.Is(err, importer.ErrLeaseLost) {
		t.Errorf("retry of a lost claim: %v", err)
	}

	end := retryAt.Add(lease + time.Minute)
	res, err = s.SaveImportPage(ctx, resumed, importer.Page{
		Charges: []core.Charge{importedCharge("charge-1", before.Add(time.Hour)), importedCharge("charge-2", time.Now().Add(time.Hour))}, //nolint:forbidigo // after the cutoff, the database's clock
		Step:    importer.StepCharges, Done: true, At: end,
	})
	if err != nil || res.Saved != 1 || res.Skipped != 1 {
		t.Fatalf("last page: %+v, %v", res, err)
	}
	latest, _, _ = s.LatestImportJob(ctx, a, v)
	if latest.Status != api.ImportDone || !latest.EndedAt.Equal(end) || latest.Imported != 2 || latest.Skipped != 1 || latest.Error != "" {
		t.Errorf("done %+v", latest)
	}
	var erased bool
	_ = s.inAccount(ctx, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT token IS NULL FROM import_jobs").Scan(&erased)
	})
	if !erased {
		t.Error("the token outlived the job")
	}

	// Another job, once this one is done; it fails.
	if _, err := s.CreateImportJob(ctx, a, v, aximoteRequest(), end); err != nil {
		t.Fatal(err)
	}
	job, _, _ = s.ClaimImport(ctx, end, lease)
	if err := s.FailImport(ctx, job, importer.FailTokenRefused, end.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	latest, _, _ = s.LatestImportJob(ctx, a, v)
	if latest.ID != job.ID || latest.Status != api.ImportFailed || latest.Error != "token_refused" || latest.EndedAt.IsZero() {
		t.Errorf("failed %+v", latest)
	}
	var tokens int
	_ = s.inAccount(ctx, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(token) FROM import_jobs").Scan(&tokens)
	})
	if tokens != 0 {
		t.Errorf("%d tokens left", tokens)
	}
}

// TestImportJobIsolation extends TestAccountIsolation to the import jobs.
func TestImportJobIsolation(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	a, va := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, vb := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if _, err := s.CreateImportJob(ctx, b, vb, aximoteRequest(), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateImportJob(ctx, a, vb, aximoteRequest(), t0); err == nil {
		t.Error("a job for another account's vehicle")
	}
	if _, found, err := s.LatestImportJob(ctx, a, vb); found || err != nil {
		t.Errorf("account A read B's job: %v, %v", found, err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM import_jobs").Scan(&n); err != nil || n != 0 {
		t.Errorf("no account: %d visible, %v", n, err)
	}
	err := s.inAccount(ctx, b, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE import_jobs SET account_id = $1", a)
		return err
	})
	if err == nil {
		t.Error("a job moved to another account")
	}

	// Claimed across the accounts, its token opens with B's only: a copy into A's row does
	// not.
	job, ok, err := s.ClaimImport(ctx, t0, time.Minute)
	if err != nil || !ok || job.AccountID != b || job.Token != testImportToken {
		t.Fatalf("claim %+v, %v, %v", job, ok, err)
	}
	if _, err := s.CreateImportJob(ctx, a, va, aximoteRequest(), t0); err != nil {
		t.Fatal(err)
	}
	db, err := pgx.Connect(ctx, dbURL) // the owner, outside RLS
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	_, err = db.Exec(ctx, `UPDATE import_jobs SET token = (SELECT token FROM import_jobs WHERE account_id = $1)
		WHERE account_id = $2`, b, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimImport(ctx, t0, time.Minute); err == nil {
		t.Error("B's token opened in A's job")
	}
}

// TestImportWorker runs the import worker on the store against the simulator's Aximote,
// whose history ends a month before now: everything is imported, the track with its
// trip, the job's report written and its token erased.
func TestImportWorker(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1SMLT0000ER0001")
	now := time.Now().UTC() //nolint:forbidigo // the database's clock decides the cutoff
	srv := httptest.NewServer(aximoteapi.NewHandler(aximoteapi.DevToken, aximoteapi.Default(now, "YV1SMLT0000ER0001")))
	defer srv.Close()
	req := api.ImportRequest{Source: "aximote", SourceVehicle: "veh-ex30", Token: aximoteapi.DevToken}
	if _, err := s.CreateImportJob(ctx, a, v, req, now); err != nil {
		t.Fatal(err)
	}
	params := importer.DefaultParams()
	params.PageSize = 2
	w := importer.New(s, aximote.NewClient(srv.URL, srv.Client()), clock.NewManual(now), params, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if worked, err := w.Once(ctx); !worked || err != nil {
		t.Fatalf("%v, %v", worked, err)
	}
	job, _, err := s.LatestImportJob(ctx, a, v)
	if err != nil || job.Status != api.ImportDone || job.Imported != 3 || job.Invalid != 2 || job.Skipped != 0 || job.Cutoff.IsZero() {
		t.Fatalf("job %+v, %v", job, err)
	}
	trips, err := s.ListTrips(ctx, a, v, api.EventQuery{})
	if err != nil || len(trips) != 2 || trips[0].Import.V.StartAddress == "" {
		t.Fatalf("trips %+v, %v", trips, err)
	}
	charges, err := s.ListCharges(ctx, a, v, api.EventQuery{})
	if err != nil || len(charges.Charges) != 1 || charges.Charges[0].PaidCost.V.Minor != 437 {
		t.Fatalf("charges %+v, %v", charges, err)
	}
	var tracks int
	_ = s.inAccount(ctx, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM imported_tracks").Scan(&tracks)
	})
	if tracks != 1 {
		t.Errorf("%d tracks", tracks)
	}
}
