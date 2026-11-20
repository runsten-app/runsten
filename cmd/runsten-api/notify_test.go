package main

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"testing"
	"time"

	"runsten/internal/exporter"
	"runsten/internal/importer"
	"runsten/internal/notify"
	"runsten/internal/platform/clock"
)

// importStore and exportStore end the jobs, or fail to; the rest of their interfaces is
// not called.
type importStore struct {
	importer.Store
	fail error
}

type exportStore struct {
	exporter.Store
	fail error
}

func (s importStore) SaveImportPage(context.Context, importer.Job, importer.Page) (importer.Result, error) {
	return importer.Result{}, s.fail
}

func (s importStore) FailImport(context.Context, importer.Job, importer.Failure, time.Time) error {
	return s.fail
}

func (s exportStore) SaveExport(context.Context, exporter.Job, time.Time, int64, time.Time) error {
	return s.fail
}

func (s exportStore) FailExport(context.Context, exporter.Job, time.Time) error { return s.fail }

// told keeps one device and records what is sent, once per key.
type told struct {
	mu   sync.Mutex
	seen map[string]bool
	args []map[string]string
}

func (t *told) NotificationPreferences(context.Context, string) (notify.Preferences, error) {
	return notify.Preferences{}, nil
}

func (t *told) Devices(context.Context, string) ([]notify.Device, error) {
	return []notify.Device{{ID: "d"}}, nil
}

func (t *told) RecordDelivery(_ context.Context, e notify.Event, _ time.Time, _ int) (string, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen[e.Key] {
		return "", false, nil
	}
	t.seen[e.Key] = true
	return e.Key, true, nil
}

func (t *told) Send(_ context.Context, d notify.Delivery) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d.Event.Kind != notify.ImportFinished || d.Event.AccountID != "a" {
		return errors.New("unexpected " + string(d.Event.Kind))
	}
	t.args = append(t.args, d.Event.Args)
	return nil
}

// TestJobsTold: an import or an export that ends, done or failed, is told once; a write
// that fails tells nothing.
func TestJobsTold(t *testing.T) {
	at := time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)
	tl := &told{seen: map[string]bool{}}
	n := notify.New(notify.NewRegistry(), tl, clock.NewManual(at), slog.New(slog.DiscardHandler))
	quiet := slog.New(slog.DiscardHandler)
	ctx := t.Context()
	imports := importsTold{importStore{}, n, quiet}
	exports := exportsTold{exportStore{}, n, quiet}
	job := importer.Job{ID: "j1", AccountID: "a", VehicleID: "v", Source: importer.SourceAximote}

	// Without a sender, nothing is told.
	if _, err := imports.SaveImportPage(ctx, job, importer.Page{Done: true, At: at}); err != nil || len(tl.args) != 0 {
		t.Fatalf("without a sender: %v, %v", err, tl.args)
	}
	n.SetSender(tl)
	// A page that does not end the job tells nothing; the last one does.
	if _, err := imports.SaveImportPage(ctx, job, importer.Page{At: at}); err != nil || len(tl.args) != 0 {
		t.Fatalf("a page: %v, %v", err, tl.args)
	}
	if _, err := imports.SaveImportPage(ctx, job, importer.Page{Done: true, At: at}); err != nil {
		t.Fatal(err)
	}
	if err := imports.FailImport(ctx, importer.Job{ID: "j2", AccountID: "a", VehicleID: "v", Source: importer.SourceAximote}, importer.FailTokenRefused, at); err != nil {
		t.Fatal(err)
	}
	if err := exports.SaveExport(ctx, exporter.Job{ID: "e1", AccountID: "a"}, at, 10, at.Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := exports.FailExport(ctx, exporter.Job{ID: "e2", AccountID: "a"}, at); err != nil {
		t.Fatal(err)
	}
	want := []map[string]string{
		{"job": "import", "outcome": "done", "vehicle": "v", "source": "aximote"},
		{"job": "import", "outcome": "failed", "failure": "token_refused", "vehicle": "v", "source": "aximote"},
		{"job": "export", "outcome": "done", "expires_at": "2026-11-27T18:00:00Z"},
		{"job": "export", "outcome": "failed"},
	}
	if len(tl.args) != len(want) {
		t.Fatalf("told %v", tl.args)
	}
	for i := range want {
		if !maps.Equal(tl.args[i], want[i]) {
			t.Errorf("#%d: %v, want %v", i, tl.args[i], want[i])
		}
	}

	// A write that fails tells nothing, and returns its error.
	down := errors.New("down")
	imports.Store, exports.Store = importStore{fail: down}, exportStore{fail: down}
	failed := importer.Job{ID: "j3", AccountID: "a"}
	if _, err := imports.SaveImportPage(ctx, failed, importer.Page{Done: true}); !errors.Is(err, down) {
		t.Errorf("save page: %v", err)
	}
	if err := imports.FailImport(ctx, failed, importer.FailInternal, at); !errors.Is(err, down) {
		t.Errorf("fail import: %v", err)
	}
	if err := exports.SaveExport(ctx, exporter.Job{ID: "e3", AccountID: "a"}, at, 1, at); !errors.Is(err, down) {
		t.Errorf("save export: %v", err)
	}
	if err := exports.FailExport(ctx, exporter.Job{ID: "e3", AccountID: "a"}, at); !errors.Is(err, down) {
		t.Errorf("fail export: %v", err)
	}
	if len(tl.args) != len(want) {
		t.Errorf("told after failures: %v", tl.args)
	}
}
