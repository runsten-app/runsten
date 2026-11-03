package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"runsten/internal/auth"
	"runsten/internal/importer"
	"runsten/internal/platform/clock"
	"runsten/internal/store"
	"runsten/internal/tesla"
)

// importTeslaMateUsage is the command's synopsis.
const importTeslaMateUsage = "import teslamate --vin VIN [--user NAME] [--currency CODE] [FILE]"

// importTeslaMate imports the history of a TeslaMate instance into the vehicle of the VIN:
// its backup (pg_dump's plain file, maybe gzipped) read from FILE, or from stdin, so
// that it can come straight from TeslaMate's database:
//
//	docker compose exec -T database pg_dump -U teslamate teslamate | runsten-api import teslamate --vin …
//
// The account is the user's, else the instance's single one. A charge's cost is imported
// in --currency, TeslaMate's own being unknown to it; none without.
func importTeslaMate(args []string, getenv func(string) string, stdin io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("import teslamate", flag.ContinueOnError)
	fs.SetOutput(out)
	vin := fs.String("vin", "", "the VIN of the vehicle to import into, which TeslaMate's car has")
	user := fs.String("user", "", "the user whose account holds the vehicle (default: the single account)")
	currency := fs.String("currency", "", "the ISO 4217 code of TeslaMate's costs (default: costs left out)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %s: %w", importTeslaMateUsage, err)
	}
	if *vin == "" || fs.NArg() > 1 {
		return errors.New("usage: " + importTeslaMateUsage)
	}
	databaseURL := getenv("RUNSTEN_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("configuration: RUNSTEN_DATABASE_URL is required")
	}
	backup := stdin
	if fs.NArg() == 1 {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		defer f.Close()
		backup = f
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

	account, err := accountOf(ctx, st, *user)
	if err != nil {
		return err
	}
	vehicles, err := st.Vehicles(ctx, account)
	if err != nil {
		return fmt.Errorf("vehicles: %w", err)
	}
	vehicleID := ""
	for _, v := range vehicles {
		if strings.EqualFold(v.VIN, strings.TrimSpace(*vin)) {
			if v.Provider != tesla.Source && v.Provider != tesla.FleetSource {
				return errors.New("the vehicle of this VIN is not read as a Tesla: TeslaMate's history is a Tesla's")
			}
			vehicleID = v.ID
		}
	}
	if vehicleID == "" {
		return errors.New("no vehicle of the account has this VIN: connect the Tesla first")
	}

	rep, err := importer.NewFiles(st, clock.Real{}).TeslaMate(ctx, account, vehicleID, *vin, *currency, backup)
	var noCar *importer.NoCarError
	if errors.As(err, &noCar) {
		vins := make([]string, 0, len(noCar.Cars))
		for _, c := range noCar.Cars {
			vins = append(vins, c.VIN)
		}
		return fmt.Errorf("the backup has no car of this VIN, only %s", strings.Join(vins, ", "))
	}
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	log.Info("TeslaMate's history imported", "vehicle", vehicleID, "saved", rep.Saved,
		"skipped_after_cutoff", rep.Skipped, "cutoff", rep.Cutoff, "invalid", rep.Invalid)
	return nil
}

// accountOf is the account of the user, else the instance's single one.
func accountOf(ctx context.Context, st *store.Store, user string) (string, error) {
	if user == "" {
		account, err := st.SingleAccount(ctx)
		if err != nil {
			return "", fmt.Errorf("account (name the user with --user on an instance of several): %w", err)
		}
		return account, nil
	}
	name, err := auth.NormalizeUsername(user)
	if err != nil {
		return "", fmt.Errorf("user %q: %w", user, err)
	}
	u, ok, err := st.UserByName(ctx, name)
	switch {
	case err != nil:
		return "", fmt.Errorf("user %q: %w", user, err)
	case !ok:
		return "", fmt.Errorf("no user %q", user)
	}
	return u.AccountID, nil
}
