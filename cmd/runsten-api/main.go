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
//	RUNSTEN_VOLVO_API_KEY        the instance's Volvo application key (vcc-api-key), to list
//	                             the vehicles of the accounts without their own (default:
//	                             none; each account must then give its own key, on the
//	                             Connection page, before connecting a Volvo ID)
//	RUNSTEN_VOLVO_CLIENT_ID      Volvo application client ID (required)
//	RUNSTEN_VOLVO_CLIENT_SECRET  its client secret (required); it never leaves this backend
//	RUNSTEN_VOLVO_REDIRECT_URI   redirect URI registered with the application (required),
//	                             ending with /auth/volvo/callback; https, or http on a
//	                             loopback host. Its scheme also decides whether cookies
//	                             are Secure: it is on the host users open.
//	RUNSTEN_VOLVO_SCOPES         requested scopes, space separated (default: the read
//	                             scopes of the polled endpoints, location:read included)
//	RUNSTEN_VOLVO_BASE_URL       API address (default https://api.volvocars.com)
//	RUNSTEN_VOLVO_AUTH_URL       Volvo ID address (default https://volvoid.eu.volvocars.com)
//	RUNSTEN_APP_URL              the web interface, where the Volvo ID connection comes
//	                             back to, when it is not on the host of the redirect URI
//	                             (default: there; Vite in development)
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
//	RUNSTEN_HEALTH_TOKEN         the bearer token of /healthz/full, 32 characters at
//	                             least (default none: no report)
//	RUNSTEN_LOG_LEVEL            debug, info, warn, error (default info)
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
	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/derive"
	"runsten/internal/geocode"
	"runsten/internal/geocode/nominatim"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/platform/health"
	"runsten/internal/platform/httpserver"
	"runsten/internal/platform/secretbox"
	"runsten/internal/store"
	"runsten/internal/volvo"
)

// Pending authorizations: a user completes the Volvo ID login within minutes, and a
// self-hosted instance has a single user.
const (
	flowTTL    = 10 * time.Minute
	maxPending = 16
)

// callbackPath is the route the redirect URI must lead to.
const callbackPath = "/auth/volvo/callback"

// probeTimeout bounds the healthcheck subcommand, below the container healthcheck
// timeout.
const probeTimeout = 5 * time.Second

const defaultAddr = "127.0.0.1:8081"

type config struct {
	databaseURL string
	tokenKey    string
	apiKey      string
	baseURL     string
	app         volvo.AuthConfig
	appURL      string
	addr        string
	restricted  bool
	logLevel    slog.Level
	// geocoder is a Nominatim-compatible reverse geocoder; nil: no address.
	geocoder *nominatim.Client
	geocode  geocode.Params
	// healthToken guards the health report; empty: no report.
	healthToken string
	// privateBrokers: an account's MQTT broker may be on a local network, or over
	// plain mqtt://. False on an instance open to the Internet's users.
	privateBrokers bool
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

	hc := &http.Client{Timeout: 15 * time.Second}
	redirect, _ := url.Parse(cfg.app.RedirectURI) // validated by loadConfig
	sessions := auth.New(st, clock.Real{}, auth.DefaultParams(), rand.Reader)
	authorizer := volvo.NewAuthClient(cfg.app, hc)
	// Only with a geocoder are the positions asked for: nothing leaves for a third party
	// otherwise.
	var addresses api.Addresses
	if cfg.geocoder != nil {
		addresses = st
	}
	deriver := derive.New(st, core.DefaultParams(), nil, log)
	handler := api.New(api.Config{
		Sessions: sessions, AccessTokens: sessions,
		Addresses: addresses,
		Reader:    st,
		Settings:  st,
		Models:    st,
		Costs:     st,
		Accounts:  st,
		// It only reads the current state and the readings: it derives nothing, and
		// needs no capacity.
		States:     deriver,
		Readings:   deriver,
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
		InstanceKey:      cfg.apiKey != "",
		InstanceKeyLast4: cfg.apiKey[max(0, len(cfg.apiKey)-4):],
		ClientID:         cfg.app.ClientID,
		Brokers:          st,
		// The collector checks the addresses again when it connects.
		PublicBrokersOnly: !cfg.privateBrokers,
		Clock:             clock.Real{},
		// The redirect URI is on the host users open: https there means https here.
		SecureCookies: redirect.Scheme == "https",
		AppURL:        cfg.appURL,
		Log:           log,
	})
	if cfg.geocoder != nil {
		go geocode.New(st, cfg.geocoder, clock.Real{}, cfg.geocode, log).Run(ctx)
	}
	httpserver.WarnIfExposed(log, cfg.addr, cfg.restricted,
		"listening beyond this machine over plain http: put an https reverse proxy in front of runsten-api, passwords and session cookies travel in clear text otherwise")
	if ok, err := st.HasUsers(ctx); err != nil {
		return fmt.Errorf("users: %w", err)
	} else if !ok {
		log.Warn("no user yet: create one with runsten-api user create <name> (docker compose run --rm api user create <name>)")
	}
	log.Info("runsten-api started: sign in, then open the start URL to connect a Volvo ID",
		"addr", cfg.addr, "login", siteURL(redirect, "/login"), "start", siteURL(redirect, "/auth/volvo/start"),
		"scopes", strings.Join(cfg.app.Scopes, " "), "instance_key", cfg.apiKey != "", "geocoder", cfg.geocoder != nil)
	mux := http.NewServeMux()
	mux.Handle("GET "+health.Path, health.OK()) // not logged: probed every 30 s
	if cfg.healthToken != "" {                  // not logged either: an uptime monitor's
		mux.Handle("GET "+health.ReportPath, health.NewReporter(cfg.healthToken, healthChecks(st, clock.Real{}), checkTimeout))
	}
	mux.Handle("/", httpserver.LogRequests(log, handler))
	if err := registerExtensions(ctx, extensions{
		mux: mux, getenv: os.Getenv, databaseURL: cfg.databaseURL, box: box,
		sessions: sessions, api: handler, client: hc, log: log,
	}); err != nil {
		return fmt.Errorf("extensions: %w", err)
	}
	if err := httpserver.Run(ctx, httpserver.New(cfg.addr, mux, log), log); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// extensions is what registerExtensions gets: the premium build's modules add their
// routes to mux, beside the API's, and read their own configuration with getenv.
type extensions struct {
	mux         *http.ServeMux
	getenv      func(string) string
	databaseURL string
	box         *secretbox.Box
	sessions    *auth.Service
	api         *api.Server
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
		baseURL:  volvo.DefaultBaseURL,
		app:      volvo.AuthConfig{BaseURL: volvo.DefaultAuthURL, Scopes: volvo.DefaultScopes()},
		addr:     defaultAddr,
		logLevel: slog.LevelInfo,
		// Self-hosting publishes to a broker on its own network.
		privateBrokers: true,
	}
	var errs []error
	cfg.apiKey = getenv("RUNSTEN_VOLVO_API_KEY")
	for name, dst := range map[string]*string{
		"RUNSTEN_DATABASE_URL":        &cfg.databaseURL,
		"RUNSTEN_TOKEN_KEY":           &cfg.tokenKey,
		"RUNSTEN_VOLVO_CLIENT_ID":     &cfg.app.ClientID,
		"RUNSTEN_VOLVO_CLIENT_SECRET": &cfg.app.ClientSecret,
		"RUNSTEN_VOLVO_REDIRECT_URI":  &cfg.app.RedirectURI,
	} {
		if *dst = getenv(name); *dst == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	if cfg.app.RedirectURI != "" {
		if err := validateRedirectURI(cfg.app.RedirectURI); err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_VOLVO_REDIRECT_URI %q: %w", cfg.app.RedirectURI, err))
		}
	}
	for name, dst := range map[string]*string{"RUNSTEN_VOLVO_BASE_URL": &cfg.baseURL, "RUNSTEN_VOLVO_AUTH_URL": &cfg.app.BaseURL} {
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
	if v := getenv("RUNSTEN_APP_URL"); v != "" {
		if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
			u.RawQuery != "" || u.Fragment != "" {
			errs = append(errs, fmt.Errorf("RUNSTEN_APP_URL %q: http(s) URL expected, without query or fragment", v))
		}
		cfg.appURL = strings.TrimSuffix(v, "/") + "/"
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
	return cfg, errors.Join(errs...)
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
