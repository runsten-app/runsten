package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Dir keeps the archives in a directory: root/<account>/<export>.zip, written to a
// temporary file of its own beside it, renamed once complete. One instance of the API
// reads and writes it: several need a shared bucket (S3).
type Dir struct {
	root string
}

// NewDir creates the directory if needed.
func NewDir(root string) (*Dir, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("archive directory: %w", err)
	}
	return &Dir{root: root}, nil
}

func (d *Dir) path(accountID, exportID string) string {
	return filepath.Join(d.root, accountID, exportID+suffix)
}

// Create starts the export's archive. Close makes it the export's, unless ctx is over:
// the file written is then removed. Two builds of an export, one of a claim taken over,
// each write a file of their own.
func (d *Dir) Create(ctx context.Context, accountID, exportID string) (io.WriteCloser, error) {
	if err := checkIDs(accountID, exportID); err != nil {
		return nil, err
	}
	final := d.path(accountID, exportID)
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return nil, fmt.Errorf("archive directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(final), exportID+suffix+".*.part")
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	return &dirWriter{ctx: ctx, f: f, final: final}, nil
}

type dirWriter struct {
	ctx   context.Context
	f     *os.File
	final string
}

func (w *dirWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err //nolint:wrapcheck // the build's own
	}
	n, err := w.f.Write(p)
	if err != nil {
		return n, fmt.Errorf("archive: %w", err)
	}
	return n, nil
}

func (w *dirWriter) Close() error {
	err := w.ctx.Err()
	if err == nil {
		err = w.f.Sync()
	}
	if closeErr := w.f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(w.f.Name(), w.final)
	}
	if err != nil {
		_ = os.Remove(w.f.Name())
		return fmt.Errorf("archive: %w", err)
	}
	// The rename survives a crash once its directory is synced: the export is told
	// done after it.
	if err := syncDir(filepath.Dir(w.final)); err != nil {
		_ = os.Remove(w.final)
		return fmt.Errorf("archive: %w", err)
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path) //nolint:gosec // the archives' own directory
	if err != nil {
		return err //nolint:wrapcheck // wrapped by the caller
	}
	err = dir.Sync()
	if closeErr := dir.Close(); err == nil {
		err = closeErr
	}
	return err //nolint:wrapcheck // wrapped by the caller
}

// Open opens the export's archive, to be served from any offset; fs.ErrNotExist when
// there is none. The name is the server's to give: a file has none of its own.
func (d *Dir) Open(_ context.Context, accountID, exportID, _ string) (io.ReadSeekCloser, string, error) {
	if err := checkIDs(accountID, exportID); err != nil {
		return nil, "", err
	}
	f, err := os.Open(d.path(accountID, exportID))
	if err != nil {
		return nil, "", fmt.Errorf("archive: %w", err)
	}
	return f, "", nil
}

// Remove removes the export's archive, and whatever a build of it left; none is no
// error.
func (d *Dir) Remove(_ context.Context, accountID, exportID string) error {
	if err := checkIDs(accountID, exportID); err != nil {
		return err
	}
	final := d.path(accountID, exportID)
	parts, _ := filepath.Glob(final + ".*.part") // the pattern is well formed
	var errs []error
	for _, p := range append(parts, final) {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	// The account's directory goes once empty: a full one stays, which is no error.
	_ = os.Remove(filepath.Dir(final))
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	return nil
}

// List lists the exports that have an archive, or what a build left: their account by
// their id.
func (d *Dir) List(_ context.Context) (map[string]string, error) {
	accounts, err := os.ReadDir(d.root)
	if err != nil {
		return nil, fmt.Errorf("archive directory: %w", err)
	}
	out := map[string]string{}
	for _, a := range accounts {
		if !a.IsDir() || !ids.MatchString(a.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(d.root, a.Name()))
		if err != nil {
			return nil, fmt.Errorf("archive directory: %w", err)
		}
		for _, f := range files {
			if id, _, ok := strings.Cut(f.Name(), suffix); ok && ids.MatchString(id) {
				out[id] = a.Name()
			}
		}
	}
	return out, nil
}
