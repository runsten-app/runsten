// Command runsten-api is Runsten's HTTP interface. Its users sign in with a password;
// every route but the sign-in requires a session:
//
//	GET  /login, POST /login, POST /logout   sign in and out (pages, no JavaScript)
//	GET  /                    home page: connect a Volvo ID, sign out
//	GET  /auth/volvo/start    sends the browser to Volvo ID (OAuth 2.0, PKCE)
//	GET  /auth/volvo/callback exchanges the code, stores the encrypted tokens and
//	                          records the vehicles in the signed-in user's account
//	     /api/v1/...          JSON API: session, vehicles, current state, trips, charges,
//	                          statistics
//	GET  /healthz             answers 200 while the server runs
//	GET  /healthz/full        the health report, for an uptime monitor, with
//	                          RUNSTEN_HEALTH_TOKEN as a bearer token: the database's
//	                          connections and the collector's latest pass (?check=
//	                          database, collector: one alone); 503 when one fails
//
// Subcommands:
//
//	runsten-api user create <name>    creates the user of the instance's single account;
//	                                  the password is read from the terminal (asked
//	                                  twice, not echoed), or from the first line of stdin
//	runsten-api user password <name>  sets a new password and ends the user's sessions
//	runsten-api healthcheck           probes /healthz on RUNSTEN_API_ADDR and exits 0 if
//	                                  the server is healthy: the image has no shell, the
//	                                  container healthcheck runs this
//
// The user subcommands only need RUNSTEN_DATABASE_URL.
//
// The collector then refreshes the Volvo tokens on its own until the grant ends.
//
// Configuration through environment variables:
//
//	RUNSTEN_DATABASE_URL         PostgreSQL URL (required)
//	RUNSTEN_TOKEN_KEY            token encryption key, 32 bytes in base64 (required)
//	RUNSTEN_APP_URL              the address Runsten is opened at, the web interface
//	                             (required, unless RUNSTEN_VOLVO_REDIRECT_URI is set: its
//	                             host then); https, or http on a loopback host. Its
//	                             scheme decides whether cookies are Secure; the Volvo ID
//	                             connection comes back to it (Vite in development)
//	RUNSTEN_VOLVO_CLIENT_ID      Volvo application client ID (default none: no Volvo ID
//	                             can be connected)
//	RUNSTEN_VOLVO_CLIENT_SECRET  its client secret, with it; it never leaves this backend
//	RUNSTEN_VOLVO_REDIRECT_URI   redirect URI registered with the application (default:
//	                             RUNSTEN_APP_URL + auth/volvo/callback), ending with
//	                             /auth/volvo/callback; https, or http on a loopback host
//	RUNSTEN_VOLVO_API_KEY        the instance's Volvo application key (vcc-api-key), to list
//	                             the vehicles of the accounts without their own (default:
//	                             none; each account must then give its own key, on the
//	                             Connection page, before connecting a Volvo ID)
//	RUNSTEN_VOLVO_SCOPES         requested scopes, space separated (default: the read
//	                             scopes of the polled endpoints, location:read included)
//	RUNSTEN_VOLVO_BASE_URL       API address (default https://api.volvocars.com)
//	RUNSTEN_VOLVO_AUTH_URL       Volvo ID address (default https://volvoid.eu.volvocars.com)
//	RUNSTEN_API_ADDR             listen address (default 127.0.0.1:8081). runsten-api
//	                             serves plain http: beyond this machine, put an https
//	                             reverse proxy in front of it, or passwords and session
//	                             cookies travel in clear text.
//	RUNSTEN_ACCESS_RESTRICTED    true when the deployment only lets RUNSTEN_API_ADDR be
//	                             reached through the host's loopback or an https reverse
//	                             proxy (container port published on the host loopback):
//	                             a non-loopback address is then expected, and not warned
//	                             about (default false)
//	RUNSTEN_GEOCODER_URL         a reverse geocoder speaking Nominatim's API, as
//	                             https://nominatim.openstreetmap.org (default none: no
//	                             address, no position leaves the instance). It is sent
//	                             the positions outside the places, one per
//	                             RUNSTEN_GEOCODER_INTERVAL (default 1s)
//	RUNSTEN_POLESTAR_BASE_URL    Polestar Data Portal's M2M API, which checks a credential
//	                             before it is stored (default
//	                             https://pc-api.polestar.com/eu-north-1/data-portal/m2m)
//	RUNSTEN_TESLA_API_URL        Tesla's Owner API, which checks a refresh token before
//	                             it is stored (default https://owner-api.teslamotors.com)
//	RUNSTEN_TESLA_AUTH_URL       Tesla's authentication, whose /token refreshes it
//	                             (default https://auth.tesla.com/oauth2/v3)
//	RUNSTEN_AXIMOTE_BASE_URL     Aximote's public API, which the imports of a vehicle's
//	                             history read with the user's token (default
//	                             https://api.aximote.com)
//	RUNSTEN_HEALTH_TOKEN         the bearer token of /healthz/full, 32 characters at
//	                             least (default none: no report)
//	RUNSTEN_UPDATE_CHECK         true to read the latest release of Runsten on GitHub
//	                             once a day, and tell a newer one in the settings
//	                             (default false: nothing is asked of GitHub)
//	RUNSTEN_LOG_LEVEL            debug, info, warn, error (default info)
//	RUNSTEN_FLAGS                feature flags, name=true|false separated by commas,
//	                             e.g. source.tesla=false (default: each its own; the
//	                             flags are listed in internal/flags). As the collector's.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the time zones of the statistics, whatever the image or the host

	"golang.org/x/term"

	"runsten/internal/api"
	"runsten/internal/auth"
	"runsten/internal/aximote"
	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/derive"
	"runsten/internal/flags"
	"runsten/internal/flags/eval"
	"runsten/internal/geocode"
	"runsten/internal/geocode/nominatim"
	"runsten/internal/importer"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/platform/health"
	"runsten/internal/platform/httpserver"
	"runsten/internal/platform/secretbox"
	"runsten/internal/polestar"
	"runsten/internal/release"
	"runsten/internal/store"
	"runsten/internal/tesla"
	"runsten/internal/volvo"
	"runsten/internal/volvoportal"
)

// Pending authorizations: a user completes the Volvo ID login within minutes, and a
// self-hosted instance has a single user.
const (
	flowTTL    = 10 * time.Minute
	maxPending = 16
)

// version is the release built, set by the build (-ldflags "-X main.version=0.11.0":
// the Dockerfile's VERSION, which a release passes); dev from the sources.
var version = "dev"

// callbackPath is the route the redirect URI must lead to.
const callbackPath = "/auth/volvo/callback"

// probeTimeout bounds the healthcheck subcommand, below the container healthcheck
// timeout.
const probeTimeout = 5 * time.Second

const defaultAddr = "127.0.0.1:8081"

// flagsShutdown bounds the flags provider's stop, so that a provider stuck never holds
// the exit.
const flagsShutdown = 5 * time.Second

type config struct {
	databaseURL string
	tokenKey    string
	apiKey      string
	baseURL     string
	aximoteURL  string
	polestarURL string
	tesla       tesla.Hosts
	app         volvo.AuthConfig
	appURL      string
	// secureCookies: the address users open is https.
	secureCookies bool
	addr          string
	restricted    bool
	logLevel      slog.Level
	// geocoder is a Nominatim-compatible reverse geocoder; nil: no address.
	geocoder *nominatim.Client
	geocode  geocode.Params
	// healthToken guards the health report; empty: no report.
	healthToken string
	// updateCheck: read the latest release on GitHub, to tell a newer one.
	updateCheck bool
	// privateBrokers: an account's MQTT broker may be on a local network, or over
	// plain mqtt://. False on an instance open to the Internet's users.
	privateBrokers bool
	// flags are those RUNSTEN_FLAGS sets, and unknownFlags the names it gives that this
	// release does not declare.
	flags        map[flags.Name]bool
	unknownFlags []string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "runsten-api:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "healthcheck":
		addr := os.Getenv("RUNSTEN_API_ADDR")
		if addr == "" {
			addr = defaultAddr
		}
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		return health.Probe(ctx, &http.Client{Timeout: probeTimeout}, addr) //nolint:wrapcheck // already has context
	case len(args) == 3 && args[0] == "user" && (args[1] == "create" || args[1] == "password"):
		return userCommand(args[1], args[2], os.Getenv, os.Stdin, os.Stderr)
	case len(args) > 0:
		return fmt.Errorf("unknown command %q (expected: user create <name>, user password <name>, healthcheck, or none to serve)", args)
	}
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	box, err := secretbox.FromBase64(cfg.tokenKey)
	if err != nil {
		return fmt.Errorf("RUNSTEN_TOKEN_KEY: %w", err)
	}
	cat, err := catalog.Load()
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	applied, err := store.Migrate(ctx, cfg.databaseURL)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if len(applied) > 0 {
		log.Info("migrations applied", "migrations", applied)
	}
	st, err := store.Open(ctx, cfg.databaseURL, box)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer st.Close()

	// The flags of RUNSTEN_FLAGS, unless an extension brings its own provider.
	features := eval.New(log)
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flagsShutdown)
		defer cancel()
		_ = features.Shutdown(ctx)
	}()
	if err := features.Use(ctx, eval.Env(cfg.flags), ""); err != nil {
		return fmt.Errorf("flags: %w", err)
	}
	for _, name := range cfg.unknownFlags {
		log.Warn("RUNSTEN_FLAGS names a flag this release does not have: ignored", "flag", name)
	}

	hc := &http.Client{Timeout: 15 * time.Second}
	sessions := auth.New(st, clock.Real{}, auth.DefaultParams(), rand.Reader)
	authorizer := volvo.NewAuthClient(cfg.app, hc)
	// Only with a geocoder are the positions asked for: nothing leaves for a third party
	// otherwise.
	var addresses api.Addresses
	if cfg.geocoder != nil {
		addresses = st
	}
	deriver := derive.New(st, derive.Sources(volvo.Normalizer{}, polestar.Normalizer{}, tesla.Normalizer{}), core.DefaultParams(), nil, log)
	ax := aximote.NewClient(cfg.aximoteURL, hc)
	// Only when asked is GitHub read: nothing leaves for it otherwise.
	var releases api.Releases
	if cfg.updateCheck {
		checker := release.NewChecker(release.DefaultURL, version, hc, clock.Real{}, log)
		go checker.Run(ctx)
		releases = latestRelease{checker}
	}
	handler := api.New(api.Config{
		Sessions: sessions, AccessTokens: sessions,
		Addresses:   addresses,
		Reader:      st,
		Settings:    st,
		Models:      st,
		Deletion:    st,
		Costs:       st,
		Imports:     st,
		ImportJobs:  st,
		Aximote:     aximoteVehicles{ax},
		FileImports: fileImports{importer.NewFiles(st, clock.Real{})},
		Accounts:    st,
		Flags:       features,
		// It reads the current state, the readings and the health, and replays the derivation
		// for the charge in progress, with the net capacity the API gives it: it saves nothing,
		// and needs no capacity of its own.
		States:     deriver,
		Readings:   deriver,
		Health:     deriver,
		Charging:   deriver,
		Params:     core.DefaultParams(),
		CostParams: core.DefaultCostParams(), CapacityParams: core.DefaultCapacityParams(),
		Catalog:    cat,
		Authorizer: authorizer,
		Flows:      oauth.NewFlows(clock.Real{}, flowTTL, maxPending),
		Enrollment: st,
		Vehicles:   volvo.NewClient(cfg.baseURL, cfg.apiKey, hc),
		Keys:       st,
		// A new application key is checked by listing the vehicles, with a token
		// refreshed here if needed, under the same row lock as the collector's.
		Tokens:           oauth.NewManager(st, authorizer, clock.Real{}, oauth.DefaultParams(), log),
		Polestar:         st,
		PolestarVehicles: polestarVehicles{polestar.NewClient(cfg.polestarURL, hc)},
		Tesla:            st,
		TeslaVehicles:    teslaVehicles{tesla.NewClient(cfg.tesla, hc)},
		InstanceKey:      cfg.apiKey != "",
		InstanceKeyLast4: cfg.apiKey[max(0, len(cfg.apiKey)-4):],
		ClientID:         cfg.app.ClientID,
		Brokers:          st,
		// The collector checks the addresses again when it connects.
		PublicBrokersOnly: !cfg.privateBrokers,
		Clock:             clock.Real{},
		SecureCookies:     cfg.secureCookies,
		AppURL:            cfg.appURL,
		Version:           version,
		Releases:          releases,
		Log:               log,
	})
	if cfg.geocoder != nil {
		go geocode.New(st, cfg.geocoder, clock.Real{}, cfg.geocode, log).Run(ctx)
	}
	go importer.New(st, ax, clock.Real{}, importer.DefaultParams(), log).Run(ctx)
	httpserver.WarnIfExposed(log, cfg.addr, cfg.restricted,
		"listening beyond this machine over plain http: put an https reverse proxy in front of runsten-api, passwords and session cookies travel in clear text otherwise")
	if ok, err := st.HasUsers(ctx); err != nil {
		return fmt.Errorf("users: %w", err)
	} else if !ok {
		log.Warn("no user yet: create one with runsten-api user create <name> (docker compose run --rm api user create <name>)")
	}
	if cfg.app.ClientID == "" {
		log.Info("runsten-api started without a Volvo application: the Volvo IDs cannot be connected",
			"version", version, "addr", cfg.addr, "app_url", cfg.appURL, "geocoder", cfg.geocoder != nil, "update_check", cfg.updateCheck)
	} else {
		redirect, _ := url.Parse(cfg.app.RedirectURI) // validated by loadConfig
		log.Info("runsten-api started: sign in, then open the start URL to connect a Volvo ID",
			"version", version, "addr", cfg.addr, "login", siteURL(redirect, "/login"), "start", siteURL(redirect, "/auth/volvo/start"),
			"scopes", strings.Join(cfg.app.Scopes, " "), "instance_key", cfg.apiKey != "", "geocoder", cfg.geocoder != nil,
			"update_check", cfg.updateCheck)
	}
	mux := http.NewServeMux()
	mux.Handle("GET "+health.Path, health.OK()) // not logged: probed every 30 s
	if cfg.healthToken != "" {                  // not logged either: an uptime monitor's
		mux.Handle("GET "+health.ReportPath, health.NewReporter(cfg.healthToken, version, healthChecks(st, clock.Real{}), checkTimeout))
	}
	mux.Handle("/", httpserver.LogRequests(log, handler))
	if err := registerExtensions(ctx, extensions{
		mux: mux, getenv: os.Getenv, databaseURL: cfg.databaseURL, box: box,
		sessions: sessions, api: handler, flags: features, client: hc, log: log,
	}); err != nil {
		return fmt.Errorf("extensions: %w", err)
	}
	if err := httpserver.Run(ctx, httpserver.New(cfg.addr, mux, log), log); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// extensions is what registerExtensions gets: the premium build's modules add their
// routes to mux, beside the API's, and read their own configuration with getenv. They
// may give flags a provider of their own (eval.Evaluator.Use), in place of
// RUNSTEN_FLAGS's.
type extensions struct {
	mux         *http.ServeMux
	getenv      func(string) string
	databaseURL string
	box         *secretbox.Box
	sessions    *auth.Service
	api         *api.Server
	flags       *eval.Evaluator
	client      *http.Client
	log         *slog.Logger
}

// siteURL is a route of runsten-api on the host of the redirect URI, where users must
// sign in and start the connection: cookies are only sent back to that host.
func siteURL(redirect *url.URL, route string) string {
	u := *redirect
	u.Path = strings.TrimSuffix(u.Path, callbackPath) + route
	u.RawQuery = ""
	return u.String()
}

// userCommand creates a user, or sets a user's password. The password is read from
// stdin: never from an argument or a variable, which ps and docker inspect show.
func userCommand(action, name string, getenv func(string) string, stdin *os.File, prompt io.Writer) error {
	databaseURL := getenv("RUNSTEN_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("configuration: RUNSTEN_DATABASE_URL is required")
	}
	if _, err := auth.NormalizeUsername(name); err != nil {
		return fmt.Errorf("user %q: %w", name, err)
	}
	var password string
	var err error
	if term.IsTerminal(int(stdin.Fd())) { //nolint:gosec // a file descriptor fits an int
		password, err = askPassword(int(stdin.Fd()), prompt) //nolint:gosec // as above
	} else {
		password, err = readPassword(stdin)
	}
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err //nolint:wrapcheck // a message for the user
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
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
	svc := auth.New(st, clock.Real{}, auth.DefaultParams(), rand.Reader)

	if action == "password" {
		if err := svc.SetPassword(ctx, name, password); err != nil {
			return fmt.Errorf("user %q: %w", name, err)
		}
		log.Info("password changed, sessions ended", "user", name)
		return nil
	}
	// Self-hosting: the single account, which may already hold a Volvo connection.
	account, err := st.SingleAccount(ctx)
	if err != nil {
		return fmt.Errorf("account: %w", err)
	}
	id, err := svc.CreateUser(ctx, account, name, password)
	if err != nil {
		return fmt.Errorf("user %q: %w", name, err)
	}
	log.Info("user created: sign in at /login", "user", name, "id", id, "account", account)
	return nil
}

// askPassword reads a password twice from the terminal fd, without echo.
func askPassword(fd int, prompt io.Writer) (string, error) {
	var entries [2]string
	for i, label := range []string{"Password: ", "Password again: "} {
		_, _ = fmt.Fprint(prompt, label)
		b, err := term.ReadPassword(fd)
		_, _ = fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("reading the password: %w", err)
		}
		entries[i] = string(b)
	}
	if entries[0] != entries[1] {
		return "", errors.New("the passwords differ")
	}
	return entries[0], nil
}

// readPassword reads the password from the first line of r (a pipe, a file).
func readPassword(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", fmt.Errorf("reading the password: %w", err)
		}
		return "", errors.New("no password on stdin")
	}
	return strings.TrimSuffix(sc.Text(), "\r"), nil
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		baseURL:     volvo.DefaultBaseURL,
		aximoteURL:  aximote.DefaultBaseURL,
		polestarURL: polestar.DefaultBaseURL,
		tesla:       tesla.Hosts{API: tesla.DefaultAPIURL, Auth: tesla.DefaultAuthURL},
		app:         volvo.AuthConfig{BaseURL: volvo.DefaultAuthURL, Scopes: volvo.DefaultScopes()},
		addr:        defaultAddr,
		logLevel:    slog.LevelInfo,
		// Self-hosting publishes to a broker on its own network.
		privateBrokers: true,
	}
	var errs []error
	for name, dst := range map[string]*string{
		"RUNSTEN_DATABASE_URL": &cfg.databaseURL,
		"RUNSTEN_TOKEN_KEY":    &cfg.tokenKey,
	} {
		if *dst = getenv(name); *dst == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	if v := getenv("RUNSTEN_APP_URL"); v != "" {
		// Its scheme decides whether the session cookie is Secure: over plain http, only
		// on the loopback, where it does not cross a network.
		switch u, err := url.Parse(v); {
		case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "":
			errs = append(errs, fmt.Errorf("RUNSTEN_APP_URL %q: http(s) URL expected, without query or fragment", v))
		case u.Scheme == "http" && !httpserver.IsLoopbackHost(u.Hostname()):
			errs = append(errs, fmt.Errorf("RUNSTEN_APP_URL %q: http is only accepted on a loopback host (localhost, 127.0.0.1, [::1]): use https", v))
		}
		cfg.appURL = strings.TrimSuffix(v, "/") + "/"
	}
	errs = append(errs, loadVolvoApp(&cfg, getenv)...)
	for name, dst := range map[string]*string{
		"RUNSTEN_VOLVO_BASE_URL": &cfg.baseURL, "RUNSTEN_VOLVO_AUTH_URL": &cfg.app.BaseURL, "RUNSTEN_AXIMOTE_BASE_URL": &cfg.aximoteURL,
		"RUNSTEN_POLESTAR_BASE_URL": &cfg.polestarURL, "RUNSTEN_TESLA_API_URL": &cfg.tesla.API, "RUNSTEN_TESLA_AUTH_URL": &cfg.tesla.Auth,
	} {
		if v := getenv(name); v != "" {
			if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				errs = append(errs, fmt.Errorf("%s %q: http(s) URL expected", name, v))
			}
			*dst = v
		}
	}
	if v := getenv("RUNSTEN_VOLVO_SCOPES"); v != "" {
		cfg.app.Scopes = strings.Fields(v)
	}
	if err := volvo.ValidateScopes(cfg.app.Scopes); err != nil {
		errs = append(errs, fmt.Errorf("RUNSTEN_VOLVO_SCOPES: %w", err))
	}
	if v := getenv("RUNSTEN_API_ADDR"); v != "" {
		cfg.addr = v
	}
	if v := getenv("RUNSTEN_ACCESS_RESTRICTED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_ACCESS_RESTRICTED %q: true or false expected", v))
		}
		cfg.restricted = b
	}
	if v := getenv("RUNSTEN_MQTT_PRIVATE_BROKERS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_MQTT_PRIVATE_BROKERS %q: true or false expected", v))
		}
		cfg.privateBrokers = b
	}
	cfg.geocode = geocode.DefaultParams()
	if v := getenv("RUNSTEN_GEOCODER_URL"); v != "" {
		g, err := nominatim.New(v, &http.Client{Timeout: 15 * time.Second})
		if err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_GEOCODER_URL: %w", err))
		}
		cfg.geocoder = g
	}
	if v := getenv("RUNSTEN_GEOCODER_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 100*time.Millisecond {
			errs = append(errs, fmt.Errorf("RUNSTEN_GEOCODER_INTERVAL %q: a duration of 100ms at least expected", v))
		}
		cfg.geocode.Interval = d
	}
	if v := getenv("RUNSTEN_UPDATE_CHECK"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_UPDATE_CHECK %q: true or false expected", v))
		}
		cfg.updateCheck = b
	}
	if v := getenv("RUNSTEN_HEALTH_TOKEN"); v != "" {
		if len(v) < minHealthToken {
			errs = append(errs, fmt.Errorf("RUNSTEN_HEALTH_TOKEN: %d characters at least expected", minHealthToken))
		}
		cfg.healthToken = v
	}
	if v := getenv("RUNSTEN_LOG_LEVEL"); v != "" {
		if err := cfg.logLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_LOG_LEVEL %q: %w", v, err))
		}
	}
	var err error
	if cfg.flags, cfg.unknownFlags, err = flags.Parse(getenv("RUNSTEN_FLAGS")); err != nil {
		errs = append(errs, fmt.Errorf("RUNSTEN_FLAGS: %w", err))
	}
	return cfg, errors.Join(errs...)
}

// loadVolvoApp reads the instance's Volvo application, optional: its client ID and
// secret go together, and its redirect URI defaults to RUNSTEN_APP_URL's callback
// route. Without one, the Volvo IDs cannot connect. The address users open is
// RUNSTEN_APP_URL, or the redirect URI's host when only that is set (as before
// RUNSTEN_APP_URL was): one of them is required, and its scheme decides whether the
// cookies are Secure.
func loadVolvoApp(cfg *config, getenv func(string) string) []error {
	var errs []error
	cfg.apiKey = getenv("RUNSTEN_VOLVO_API_KEY")
	cfg.app.ClientID = getenv("RUNSTEN_VOLVO_CLIENT_ID")
	cfg.app.ClientSecret = getenv("RUNSTEN_VOLVO_CLIENT_SECRET")
	cfg.app.RedirectURI = getenv("RUNSTEN_VOLVO_REDIRECT_URI")
	given := cfg.app.RedirectURI != ""
	switch {
	case (cfg.app.ClientID == "") != (cfg.app.ClientSecret == ""):
		errs = append(errs, errors.New("RUNSTEN_VOLVO_CLIENT_ID and RUNSTEN_VOLVO_CLIENT_SECRET go together: set both, or neither on an instance without Volvo"))
	case cfg.app.ClientID == "" && cfg.apiKey != "":
		errs = append(errs, errors.New("RUNSTEN_VOLVO_API_KEY needs the Volvo application: set RUNSTEN_VOLVO_CLIENT_ID and RUNSTEN_VOLVO_CLIENT_SECRET, or remove it"))
	case cfg.app.ClientID != "" && !given && cfg.appURL != "":
		cfg.app.RedirectURI = cfg.appURL + strings.TrimPrefix(callbackPath, "/")
	}
	switch {
	case cfg.appURL == "" && !given:
		errs = append(errs, errors.New("RUNSTEN_APP_URL is required: the address Runsten is opened at, as https://runsten.example.org"))
	case cfg.app.RedirectURI == "":
	default:
		if err := validateRedirectURI(cfg.app.RedirectURI); err != nil && given {
			errs = append(errs, fmt.Errorf("RUNSTEN_VOLVO_REDIRECT_URI %q: %w", cfg.app.RedirectURI, err))
		} else if err != nil {
			errs = append(errs, fmt.Errorf("the Volvo redirect URI derived from RUNSTEN_APP_URL, %q: %w", cfg.app.RedirectURI, err))
		}
	}
	// The address users open: https there means https here.
	site := cfg.appURL
	if site == "" {
		site = cfg.app.RedirectURI
	}
	if u, err := url.Parse(site); err == nil {
		cfg.secureCookies = u.Scheme == "https"
	}
	return errs
}

// validateRedirectURI checks that the redirect URI leads to the callback route and
// does not carry the code in clear text over a network: http is only accepted on a
// loopback host (RFC 8252 §7.3).
func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return fmt.Errorf("invalid URL: %w", err)
	case u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "":
		return errors.New("absolute URL expected, without credentials, query or fragment")
	case u.Scheme == "http" && !httpserver.IsLoopbackHost(u.Hostname()):
		return errors.New("http is only accepted on a loopback host (localhost, 127.0.0.1, [::1]): use https")
	case u.Scheme != "http" && u.Scheme != "https":
		return errors.New("http(s) URL expected")
	case !strings.HasSuffix(u.Path, callbackPath):
		return fmt.Errorf("the path must end with %s", callbackPath)
	}
	return nil
}

// fileImports imports the files a user uploads, for the API.
type fileImports struct{ f *importer.Files }

func (i fileImports) ImportVolvoDataPortal(ctx context.Context, accountID, vehicleID string, trips, waypoints io.Reader) (api.FileImport, error) {
	rep, err := i.f.VolvoDataPortal(ctx, accountID, vehicleID, trips, waypoints)
	var fe *volvoportal.FormatError
	switch {
	case errors.As(err, &fe):
		return api.FileImport{}, fmt.Errorf("%w: %w", api.ErrImportFile, err)
	case err != nil:
		return api.FileImport{}, err //nolint:wrapcheck // the importer's, with its context
	}
	return api.FileImport{Imported: rep.Saved, Skipped: rep.Skipped, Invalid: rep.Invalid, Cutoff: rep.Cutoff}, nil
}

// polestarVehicles lists the vehicles of a Polestar credential for the API: a token, then
// the VINs.
type polestarVehicles struct{ c *polestar.Client }

func (p polestarVehicles) PolestarVehicles(ctx context.Context, cred api.PolestarCredential) ([]string, error) {
	tok, err := p.c.Token(ctx, polestar.Credential(cred))
	if err != nil {
		return nil, err //nolint:wrapcheck // the client's, with its context
	}
	return p.c.Vehicles(ctx, tok.Value, cred.AccountID) //nolint:wrapcheck // the client's, with its context
}

// teslaVehicles lists the vehicles of a Tesla refresh token for the API: a refresh, then
// the VINs, with the refresh token to keep.
type teslaVehicles struct{ c *tesla.Client }

func (t teslaVehicles) TeslaVehicles(ctx context.Context, refreshToken string) ([]string, string, error) {
	tok, err := t.c.Refresh(ctx, refreshToken)
	if err != nil {
		return nil, "", err //nolint:wrapcheck // the client's, with its context
	}
	products, err := t.c.Products(ctx, tok.Access)
	if err != nil {
		// Refreshed already: the token given may no longer refresh, but nothing is stored
		// without the vehicles. The user gives another.
		return nil, "", err //nolint:wrapcheck // the client's, with its context
	}
	vins := make([]string, len(products))
	for i, p := range products {
		vins[i] = p.VIN
	}
	return vins, tok.Refresh, nil
}

// aximoteVehicles lists an Aximote account's vehicles for the API.
type aximoteVehicles struct{ c *aximote.Client }

func (a aximoteVehicles) AximoteVehicles(ctx context.Context, token string) ([]api.AximoteVehicle, error) {
	vs, err := a.c.Vehicles(ctx, token)
	if err != nil {
		return nil, err //nolint:wrapcheck // the client's, with its context
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	out := make([]api.AximoteVehicle, len(vs))
	for i, v := range vs {
		out[i] = api.AximoteVehicle{ID: v.ID, Name: str(v.Name), Make: str(v.Make), Model: str(v.Model), VIN: str(v.VIN)}
		if v.Year != nil {
			out[i].Year = *v.Year
		}
	}
	return out, nil
}
