package main

import (
	"archive/zip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"runsten/internal/store"
)

// exportUsage is the command's synopsis.
const exportUsage = "export [--user NAME | --account ID] [FILE]"

// exportCommand writes an account's whole history as a ZIP archive of its export
// document (the one the interface streams with GET /account/export): out of the HTTP
// API, its worker and its archive held in the database, one file written as its data
// is read at a constant memory — the one to take in a hurry, where the interface
// cannot: the shell of the machine, before a vehicle or an account is deleted, on an
// instance of another's keeping. The account is the user's, else the instance's
// single one, unless --account gives its id. FILE: the archive's path, a missing FILE
// writing runsten-account-<day>.zip in the working directory; an existing FILE is
// replaced. The archive holds what the core exports: a downstream build's own tables
// are in the export of the server of its build alone.
func exportCommand(args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(out)
	user := fs.String("user", "", "the user whose account is exported (default: the single account)")
	id := fs.String("account", "", "the account to export, by its id")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %s: %w", exportUsage, err)
	}
	if fs.NArg() > 1 || *user != "" && *id != "" {
		return errors.New("usage: " + exportUsage)
	}
	databaseURL := getenv("RUNSTEN_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("configuration: RUNSTEN_DATABASE_URL is required")
	}
	log := slog.New(slog.NewJSONHandler(out, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	applied, err := store.Migrate(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if len(applied) > 0 {
		log.Info("migrations applied", "migrations", applied)
	}
	st, err := store.Open(ctx, databaseURL, nil) // no token is read
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer st.Close()

	var account string
	if *id != "" {
		ok, err := st.AccountExists(ctx, *id)
		if err != nil {
			return fmt.Errorf("account: %w", err)
		}
		if !ok {
			return fmt.Errorf("no account %s", *id)
		}
		account = *id
	} else if account, err = accountOf(ctx, st, *user); err != nil {
		return err
	}

	at := time.Now().UTC() //nolint:forbidigo // the export of the moment, as the API's
	name := "runsten-account-" + at.Format(time.DateOnly) + ".zip"
	if fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	f, err := os.Create(name) //nolint:gosec // the archive's path: the command's argument, of the one who runs it
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	defer f.Close()
	zipFile := zip.NewWriter(f)
	entry, err := zipFile.CreateHeader(&zip.FileHeader{
		Name: "runsten-account-" + at.Format(time.DateOnly) + ".json", Method: zip.Deflate,
	})
	if err == nil {
		// The document is the one of GET /account/export, of the core's tables: the
		// command knows no extension.
		if err = st.ExportAccount(ctx, account, at, nil, entry); err != nil {
			err = fmt.Errorf("writing the export: %w", err)
		}
	}
	if closeErr := zipFile.Close(); err == nil {
		err = closeErr
	}
	// On the disk before it is told written: this is the copy taken before a deletion.
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		// A half-written archive would pass for one: it goes.
		_ = os.Remove(name)
		return fmt.Errorf("archive: %w", err)
	}
	bytes, err := os.Stat(name)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	log.Info("account exported", "account", account, "file", name, "bytes", bytes.Size())
	return nil
}
