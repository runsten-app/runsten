// Command runsten-collector polls the Volvo API for the vehicles of every account,
// stores the raw responses in PostgreSQL and derives trips and charges from them.
//
// Usage:
//
//	runsten-collector          migrates the database, then collects until SIGTERM
//	runsten-collector connect  attaches RUNSTEN_VOLVO_ACCESS_TOKEN to the instance's
//	                           single account and records its vehicles. Such a token
//	                           (a test token from the developer portal) cannot be
//	                           refreshed: runsten-api connects a Volvo ID durably.
//	runsten-collector rebuild  recomputes the trips and charges of every vehicle from
//	                           the stored snapshots, then exits
//	runsten-collector healthcheck
//	                           probes the health endpoint of a running collector
//	                           (RUNSTEN_HEALTH_ADDR) and exits 0 if it is healthy: the
//	                           image has no shell, the container healthcheck runs this
//
// Configuration through environment variables:
//
//	RUNSTEN_DATABASE_URL        PostgreSQL URL (required)
//	RUNSTEN_TOKEN_KEY           token encryption key, 32 bytes in base64 (required)
//	RUNSTEN_VOLVO_API_KEY       the instance's Volvo application key, vcc-api-key header:
//	                            it reads the connections without their own key (required
//	                            for connect; default: none, only the connections with
//	                            their own key are read)
//	RUNSTEN_VOLVO_CLIENT_ID     Volvo application client ID (required to collect)
//	RUNSTEN_VOLVO_CLIENT_SECRET its client secret (required to collect): tokens are
//	                            refreshed by the collector
//	RUNSTEN_VOLVO_BASE_URL      API address (default https://api.volvocars.com)
//	RUNSTEN_VOLVO_AUTH_URL      Volvo ID address (default https://volvoid.eu.volvocars.com)
//	RUNSTEN_VOLVO_ACCESS_TOKEN  user's OAuth token (connect only)
//	RUNSTEN_TOKEN_MARGIN        refresh an access token this long before it expires
//	                            (default 2m)
//	RUNSTEN_TOKEN_KEEPALIVE     refresh an idle connection whose refresh token is older
//	                            (default 24h, below the 7 days a refresh token lasts)
//	RUNSTEN_POLL_TICK           pass frequency (default 10s)
//	RUNSTEN_POLL_PARKED         parked vehicle interval (default 10m)
//	RUNSTEN_POLL_ACTIVE         driving or charging interval (default 1m)
//	RUNSTEN_POLL_RARE           location, odometer, diagnostics… (default 1h)
//	RUNSTEN_POLL_DETAILS        vehicle details (default 24h)
//	RUNSTEN_VOLVO_DAILY_QUOTA   calls a day per API granted to the Volvo application
//	                            (default 10000), per key. The collector spreads 90 % of
//	                            each key's over the day; the instance's is shared by the
//	                            accounts without their own; above, calls wait.
//	RUNSTEN_VOLVO_QUOTA_PER_USER
//	                            true when Volvo counts the quota per user (Volvo ID)
//	                            rather than per application: each account then has
//	                            its own (default false)
//	RUNSTEN_LOG_LEVEL           debug, info, warn, error (default info)
//	RUNSTEN_DEV_CLOCK_SPEED     development only: the collector's time runs this many
//	                            times faster, as the simulator's SIM_SPEED (default 1).
//	                            The intervals above are then in simulated time.
//	RUNSTEN_DEBUG_ADDR          debug page, e.g. 127.0.0.1:8090 (default: disabled).
//	                            It shows VINs and locations: expose it locally only.
//	RUNSTEN_HEALTH_ADDR         health endpoint, e.g. 127.0.0.1:8091 (default:
//	                            disabled). GET /healthz answers 503 when no pass could
//	                            list the vehicles for 15 minutes.
//	RUNSTEN_ACCESS_RESTRICTED   true when the deployment restricts access to the debug
//	                            page (container port published on the host loopback):
//	                            a non-loopback RUNSTEN_DEBUG_ADDR is then expected, and
//	                            not warned about (default false)
//	RUNSTEN_MQTT_PRIVATE_BROKERS
//	                            false to publish to the brokers of the Internet only,
//	                            over TLS: an address of a private network is refused
//	                            when connecting (default true: a self-hosted instance
//	                            publishes to its local broker). As runsten-api's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"runsten/internal/catalog"
	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/debugui"
	"runsten/internal/derive"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
	"runsten/internal/platform/health"
	"runsten/internal/platform/httpserver"
	"runsten/internal/platform/secretbox"
	"runsten/internal/publish/mqtt"
	"runsten/internal/store"
	"runsten/internal/volvo"
)

type config struct {
	databaseURL  string
	tokenKey     string
	apiKey       string
	baseURL      string
	authURL      string
	clientID     string
	clientSecret string
	accessToken  string
	tokens       oauth.Params
	tick         time.Duration
	intervals    collector.Intervals
	quota        collector.Quota
	logLevel     slog.Level
	debugAddr    string
	healthAddr   string
	restricted   bool
	clockSpeed   float64
	// privateBrokers lets the accounts publish to a broker of a private network, or
	// without TLS.
	privateBrokers bool
}

const (
	// debugCalls is the number of calls kept in memory by the debug page.
	debugCalls = 500
	// healthMaxAge is how long the collector stays healthy without a pass that could
	// list the vehicles. A pass calls the due endpoints of each vehicle one after the
	// other, each call bounded by the 30 s client timeout: 15 minutes leaves room for a
	// few slow vehicles and a short database outage.
	healthMaxAge = 15 * time.Minute
	// probeTimeout bounds the healthcheck subcommand, below the container healthcheck
	// timeout.
	probeTimeout = 5 * time.Second
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "runsten-collector:", err)
		os.Exit(1)
	}
}

type command int

const (
	collect command = iota
	connect
	rebuild
	healthcheck
)

func parseCommand(args []string) (command, error) {
	switch {
	case len(args) == 0:
		return collect, nil
	case len(args) == 1 && args[0] == "connect":
		return connect, nil
	case len(args) == 1 && args[0] == "rebuild":
		return rebuild, nil
	case len(args) == 1 && args[0] == "healthcheck":
		return healthcheck, nil
	}
	return 0, fmt.Errorf("unknown command %q (expected: connect, rebuild, healthcheck)", args)
}

func run(args []string) error {
	cmd, err := parseCommand(args)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(os.Getenv, cmd)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cmd == healthcheck {
		return health.Probe(ctx, &http.Client{Timeout: probeTimeout}, cfg.healthAddr) //nolint:wrapcheck // already has context
	}

	box, err := secretbox.FromBase64(cfg.tokenKey)
	if err != nil {
		return fmt.Errorf("RUNSTEN_TOKEN_KEY: %w", err)
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

	cat, err := catalog.Load()
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	deriver := derive.New(st, core.DefaultParams(), capacities{catalog: cat, vehicle: vehicleOf(st)}, log)
	if cmd == rebuild {
		return rebuildAll(ctx, st, deriver, log)
	}

	hc := &http.Client{Timeout: 30 * time.Second}
	api := volvo.NewClient(cfg.baseURL, cfg.apiKey, hc)

	if cmd == connect {
		account, err := st.SingleAccount(ctx)
		if err != nil {
			return fmt.Errorf("connect: account: %w", err)
		}
		vins, err := oauth.Enroll(ctx, st, api, account, oauth.APIKey{}, oauth.Credentials{AccessToken: cfg.accessToken}, 0)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		log.Warn("connection saved with a token that cannot be refreshed: use runsten-api for a lasting connection",
			"account", account, "vehicles", len(vins))
		return nil
	}

	var clk clock.Clock = clock.Real{}
	if cfg.clockSpeed != 1 {
		latest, err := latestPass(ctx, st)
		if err != nil {
			return fmt.Errorf("development clock: %w", err)
		}
		clk = devClock(clk, cfg.clockSpeed, latest)
		// runsten-api stamps a new connection in real time, days behind this clock: the
		// lifetime of its refresh token would be over at once. The simulator decides.
		cfg.tokens.RefreshTTL = 0
		log.Warn("development clock: time runs faster than real time", "speed", cfg.clockSpeed, "now", clk.Now())
	}

	auth := volvo.NewAuthClient(volvo.AuthConfig{BaseURL: cfg.authURL, ClientID: cfg.clientID, ClientSecret: cfg.clientSecret}, hc)
	tokens := oauth.NewManager(st, auth, clk, cfg.tokens, log)

	// serve runs an optional side server until ctx is canceled.
	serve := func(addr string, h http.Handler) <-chan error {
		done := make(chan error, 1)
		if addr == "" {
			done <- nil
			return done
		}
		go func() { done <- httpserver.Run(ctx, httpserver.New(addr, h, log), log) }()
		return done
	}

	var obs collector.Observer
	var debugDone <-chan error
	if cfg.debugAddr != "" {
		journal := debugui.New(debugCalls, clock.Real{})
		obs = journal
		httpserver.WarnIfExposed(log, cfg.debugAddr, cfg.restricted, "debug page exposed beyond this machine: it shows VINs and positions")
		debugDone = serve(cfg.debugAddr, journal)
	} else {
		debugDone = serve("", nil)
	}
	beat := health.NewHeartbeat(clock.Real{}, healthMaxAge)
	healthMux := http.NewServeMux()
	healthMux.Handle("GET "+health.Path, beat)
	healthDone := serve(cfg.healthAddr, healthMux)

	cfg.quota.NoInstanceKey = cfg.apiKey == ""
	log.Info("collector started", "tick", cfg.tick.String(), "base_url", cfg.baseURL, "auth_url", cfg.authURL,
		"daily_quota", cfg.quota.Daily, "quota_per_user", cfg.quota.PerUser, "instance_key", cfg.apiKey != "",
		"mqtt_private_brokers", cfg.privateBrokers)
	c := collector.New(api, st, tokens, clk, cfg.intervals, cfg.quota, log, obs, deriver)
	pub := mqtt.New(st, deriver, models{catalog: cat, vehicle: vehicleOf(st)}, clk, !cfg.privateBrokers, log)
	defer pub.Close()
	c.SetPublisher(pub)
	if err := registerExtensions(ctx, extensions{getenv: os.Getenv, databaseURL: cfg.databaseURL, collector: c, log: log}); err != nil {
		return fmt.Errorf("extensions: %w", err)
	}
	if err := c.Run(ctx, cfg.tick, beat.Beat); err != nil {
		return fmt.Errorf("collect: %w", err)
	}
	if err := <-debugDone; err != nil {
		return fmt.Errorf("debug page: %w", err)
	}
	if err := <-healthDone; err != nil {
		return fmt.Errorf("health endpoint: %w", err)
	}
	log.Info("collector stopped")
	return nil
}

// extensions is what registerExtensions gets: the premium build's modules set the
// collector's read policy, and read their own configuration with getenv.
type extensions struct {
	getenv      func(string) string
	databaseURL string
	collector   *collector.Collector
	log         *slog.Logger
}

// rebuildAll recomputes the events of every vehicle. Rebuilding is idempotent: it
// replaces the events, it never adds to them.
func rebuildAll(ctx context.Context, st *store.Store, d *derive.Deriver, log *slog.Logger) error {
	targets, err := st.Targets(ctx)
	if err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	for _, t := range targets {
		res, err := d.Rebuild(ctx, t.AccountID, t.VehicleID)
		if err != nil {
			return fmt.Errorf("rebuild vehicle %s: %w", t.VehicleID, err)
		}
		log.Info("events rebuilt", "vehicle", t.VehicleID, "trips", len(res.Trips), "charges", len(res.Charges))
	}
	return nil
}

func loadConfig(getenv func(string) string, cmd command) (config, error) {
	cfg := config{
		baseURL:    volvo.DefaultBaseURL,
		authURL:    volvo.DefaultAuthURL,
		tokens:     oauth.DefaultParams(),
		tick:       10 * time.Second,
		intervals:  collector.DefaultIntervals(),
		quota:      collector.DefaultQuota(),
		logLevel:   slog.LevelInfo,
		clockSpeed: 1,

		privateBrokers: true,
	}
	var errs []error
	for name, dst := range map[string]*string{"RUNSTEN_DEBUG_ADDR": &cfg.debugAddr, "RUNSTEN_HEALTH_ADDR": &cfg.healthAddr} {
		if v := getenv(name); v != "" {
			if _, _, err := net.SplitHostPort(v); err != nil {
				errs = append(errs, fmt.Errorf("%s %q: host:port expected", name, v))
			}
			*dst = v
		}
	}
	if cmd == healthcheck {
		if cfg.healthAddr == "" {
			errs = append(errs, errors.New("RUNSTEN_HEALTH_ADDR is required: the collector has no health endpoint without it"))
		}
		return cfg, errors.Join(errs...)
	}
	required := map[string]*string{
		"RUNSTEN_DATABASE_URL": &cfg.databaseURL,
		"RUNSTEN_TOKEN_KEY":    &cfg.tokenKey,
	}
	cfg.apiKey = getenv("RUNSTEN_VOLVO_API_KEY")
	if cmd == connect { // a test token of the portal, listed with the instance's key
		required["RUNSTEN_VOLVO_API_KEY"] = &cfg.apiKey
		required["RUNSTEN_VOLVO_ACCESS_TOKEN"] = &cfg.accessToken
	}
	if cmd == collect {
		required["RUNSTEN_VOLVO_CLIENT_ID"] = &cfg.clientID
		required["RUNSTEN_VOLVO_CLIENT_SECRET"] = &cfg.clientSecret
	}
	for name, dst := range required {
		if *dst = getenv(name); *dst == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	bools := map[string]*bool{
		"RUNSTEN_ACCESS_RESTRICTED":    &cfg.restricted,
		"RUNSTEN_VOLVO_QUOTA_PER_USER": &cfg.quota.PerUser,
		"RUNSTEN_MQTT_PRIVATE_BROKERS": &cfg.privateBrokers,
	}
	for name, dst := range bools {
		if v := getenv(name); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s %q: true or false expected", name, v))
			}
			*dst = b
		}
	}
	for name, dst := range map[string]*string{"RUNSTEN_VOLVO_BASE_URL": &cfg.baseURL, "RUNSTEN_VOLVO_AUTH_URL": &cfg.authURL} {
		if v := getenv(name); v != "" {
			if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				errs = append(errs, fmt.Errorf("%s %q: http(s) URL expected", name, v))
			}
			*dst = v
		}
	}
	durations := map[string]*time.Duration{
		"RUNSTEN_TOKEN_MARGIN":    &cfg.tokens.Margin,
		"RUNSTEN_TOKEN_KEEPALIVE": &cfg.tokens.KeepAlive,
		"RUNSTEN_POLL_TICK":       &cfg.tick,
		"RUNSTEN_POLL_PARKED":     &cfg.intervals.Parked,
		"RUNSTEN_POLL_ACTIVE":     &cfg.intervals.Active,
		"RUNSTEN_POLL_RARE":       &cfg.intervals.Rare,
		"RUNSTEN_POLL_DETAILS":    &cfg.intervals.Details,
	}
	for name, dst := range durations {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				errs = append(errs, fmt.Errorf("%s %q: duration > 0 expected", name, v))
			}
			*dst = d
		}
	}
	if cfg.tokens.KeepAlive >= cfg.tokens.RefreshTTL {
		errs = append(errs, fmt.Errorf("RUNSTEN_TOKEN_KEEPALIVE %s: must be below %s, the lifetime of a refresh token",
			cfg.tokens.KeepAlive, cfg.tokens.RefreshTTL))
	}
	if v := getenv("RUNSTEN_VOLVO_DAILY_QUOTA"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("RUNSTEN_VOLVO_DAILY_QUOTA %q: integer > 0 expected", v))
		}
		cfg.quota.Daily = n
	}
	if v := getenv("RUNSTEN_DEV_CLOCK_SPEED"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			errs = append(errs, fmt.Errorf("RUNSTEN_DEV_CLOCK_SPEED %q: number > 0 expected", v))
		}
		cfg.clockSpeed = f
	}
	if v := getenv("RUNSTEN_LOG_LEVEL"); v != "" {
		if err := cfg.logLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_LOG_LEVEL %q: %w", v, err))
		}
	}
	return cfg, errors.Join(errs...)
}
