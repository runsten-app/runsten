// Command runsten-simulator is a fake backend mimicking the Volvo Cars API.
//
// Configuration through environment variables:
//
//	SIM_ADDR                listen address (default :8080)
//	SIM_SCENARIO            path to the YAML scenario, or a comma-separated list of them,
//	                        one vehicle each (required)
//	SIM_SPEED               time acceleration factor (default 1)
//	SIM_DRIVE_UPLOAD        upload interval while driving (default 1m)
//	SIM_DAILY_QUOTA         calls per day, per API and per key (default 10000, 0 = unlimited)
//	SIM_API_KEYS            application keys accepted, comma separated, each with its own
//	                        quota (default: any key); another key is refused (401)
//	SIM_RATE_PER_MINUTE     requests per minute, per token and key (default 100, 0 = unlimited)
//	SIM_TOKEN_TTL           validity of a token not issued by the simulator, from its
//	                        first use (default 0 = unlimited)
//	SIM_CLIENT_ID           registered application (default runsten-dev)
//	SIM_CLIENT_SECRET       its secret (default runsten-dev-secret)
//	SIM_REDIRECT_URI        its redirect URI (default http://127.0.0.1:8081/auth/volvo/callback)
//	SIM_ACCESS_TOKEN_TTL    lifetime of the access tokens issued (default 1799s)
//	SIM_REFRESH_TOKEN_TTL   a refresh token must be used within (default 168h)
//	SIM_GRANT_TTL           grant lifetime from the authorization (default 4320h)
//	SIM_DATABASE_URL        PostgreSQL database of its own where the simulated Volvo ID
//	                        keeps its grants and tokens across restarts (default: none,
//	                        in memory only)
//	SIM_ERROR_RATE          share of random 5xx errors, in [0, 1] (default 0)
//	SIM_LATENCY             latency added to each response, in real time (default 0)
//	SIM_LOG_LEVEL           debug, info, warn, error (default info)
//
// Limits, faults and token lifetimes can also be set by the scenario's api section;
// environment variables take precedence.
//
// Several scenarios simulate the vehicles of one account: one Volvo ID and one API
// list them all and share limits and faults. Their VINs must be distinct, and only the
// first may have an api section (its outages count from its own start). The clock
// starts at the earliest start; a vehicle whose scenario starts later stays parked
// until then.
//
// The simulated Volvo ID serves /as/authorization.oauth2 (the simulated user consents
// at once) and /as/token.oauth2 (PKCE, refresh token rotation).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/platform/httpserver"
	"runsten/internal/simulator/control"
	"runsten/internal/simulator/pgledger"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/vehicle"
	"runsten/internal/simulator/volvoapi"
)

type config struct {
	addr        string
	scenarios   []string
	speed       float64
	driveUpload time.Duration
	logLevel    slog.Level

	// Overrides for limits, faults and tokens: nil or empty when the variable is unset.
	dailyQuota, perMinute               *int
	tokenTTL, latency                   *time.Duration
	errorRate                           *float64
	accessTTL, refreshTTL, grantTTL     *time.Duration
	clientID, clientSecret, redirectURI string
	apiKeys                             []string
	databaseURL                         string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "runsten-simulator:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))

	scs, fleet, err := loadFleet(cfg.scenarios, vehicle.UploadPolicy{DriveInterval: cfg.driveUpload})
	if err != nil {
		return err
	}
	srcs := make([]volvoapi.Source, 0, len(scs))
	vins := make([]string, 0, len(scs))
	for _, sim := range fleet.Simulations() {
		srcs = append(srcs, sim)
		vins = append(vins, sim.VIN())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	limits, faults, auth := cfg.apiBehavior(scs[0])
	if cfg.databaseURL != "" {
		ledger, err := pgledger.Open(ctx, cfg.databaseURL)
		if err != nil {
			return fmt.Errorf("grants: %w", err)
		}
		defer ledger.Close()
		if auth.Issued, err = ledger.Load(ctx); err != nil {
			return fmt.Errorf("grants: %w", err)
		}
		auth.Ledger = ledger
	}
	clk := clock.NewAccelerated(clock.Real{}, fleet.Start(), cfg.speed)
	mux := http.NewServeMux()
	mux.Handle("/sim/", control.NewHandler(fleet, clk, cfg.speed))
	mux.Handle("/", volvoapi.NewHandler(srcs, clk, limits, faults, auth))

	log.Info("simulation ready", "vins", vins, "start", fleet.Start(), "speed", cfg.speed,
		"daily_quota", limits.DailyQuota, "api_keys", len(limits.Keys), "per_minute", limits.PerMinute, "token_ttl", limits.TokenTTL.String(),
		"error_rate", faults.ErrorRate, "latency", faults.Latency.String(), "outages", len(faults.Outages),
		"client_id", auth.ClientID, "redirect_uri", auth.RedirectURI, "access_token_ttl", auth.AccessTokenTTL.String(),
		"refresh_token_ttl", auth.RefreshTokenTTL.String(), "grant_ttl", auth.GrantTTL.String(),
		"grants_kept", auth.Ledger != nil, "grants_restored", len(auth.Issued.Grants))

	if err := httpserver.Run(ctx, httpserver.New(cfg.addr, httpserver.LogRequests(log, mux), log), log); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// loadFleet reads the scenario files, in their order, and prepares their simulation.
func loadFleet(files []string, policy vehicle.UploadPolicy) ([]*scenario.Scenario, *scenario.Fleet, error) {
	scs := make([]*scenario.Scenario, len(files))
	for i, file := range files {
		data, err := os.ReadFile(file) //nolint:gosec // path chosen by the operator
		if err != nil {
			return nil, nil, fmt.Errorf("reading scenario: %w", err)
		}
		if scs[i], err = scenario.Parse(data); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", file, err)
		}
	}
	fleet, err := scenario.NewFleet(scs, policy)
	if err != nil {
		return nil, nil, fmt.Errorf("simulation of %s: %w", strings.Join(files, ", "), err)
	}
	return scs, fleet, nil
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		addr:        ":8080",
		speed:       1,
		driveUpload: vehicle.DefaultUploadPolicy().DriveInterval,
		logLevel:    slog.LevelInfo,
	}
	var errs []error
	if v := getenv("SIM_ADDR"); v != "" {
		cfg.addr = v
	}
	if v := getenv("SIM_SCENARIO"); v == "" {
		errs = append(errs, errors.New("SIM_SCENARIO is required"))
	} else {
		for file := range strings.SplitSeq(v, ",") {
			if file = strings.TrimSpace(file); file == "" {
				errs = append(errs, fmt.Errorf("SIM_SCENARIO %q: empty entry in the list", v))
				break
			}
			cfg.scenarios = append(cfg.scenarios, file)
		}
	}
	if v := getenv("SIM_SPEED"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			errs = append(errs, fmt.Errorf("SIM_SPEED %q: number > 0 expected", v))
		}
		cfg.speed = f
	}
	if v := getenv("SIM_DRIVE_UPLOAD"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("SIM_DRIVE_UPLOAD %q: duration > 0 expected", v))
		}
		cfg.driveUpload = d
	}
	for name, dst := range map[string]**int{"SIM_DAILY_QUOTA": &cfg.dailyQuota, "SIM_RATE_PER_MINUTE": &cfg.perMinute} {
		if v := getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				errs = append(errs, fmt.Errorf("%s %q: integer ≥ 0 expected", name, v))
			}
			*dst = &n
		}
	}
	for name, dst := range map[string]**time.Duration{"SIM_TOKEN_TTL": &cfg.tokenTTL, "SIM_LATENCY": &cfg.latency} {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d < 0 {
				errs = append(errs, fmt.Errorf("%s %q: duration ≥ 0 expected", name, v))
			}
			*dst = &d
		}
	}
	for name, dst := range map[string]**time.Duration{
		"SIM_ACCESS_TOKEN_TTL": &cfg.accessTTL, "SIM_REFRESH_TOKEN_TTL": &cfg.refreshTTL, "SIM_GRANT_TTL": &cfg.grantTTL,
	} {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				errs = append(errs, fmt.Errorf("%s %q: duration > 0 expected", name, v))
			}
			*dst = &d
		}
	}
	cfg.clientID, cfg.clientSecret, cfg.redirectURI = getenv("SIM_CLIENT_ID"), getenv("SIM_CLIENT_SECRET"), getenv("SIM_REDIRECT_URI")
	cfg.databaseURL = getenv("SIM_DATABASE_URL")
	if v := getenv("SIM_API_KEYS"); v != "" {
		for key := range strings.SplitSeq(v, ",") {
			if key = strings.TrimSpace(key); key == "" {
				errs = append(errs, errors.New("SIM_API_KEYS: empty entry in the list"))
				break
			}
			cfg.apiKeys = append(cfg.apiKeys, key)
		}
	}
	if v := getenv("SIM_ERROR_RATE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 1 {
			errs = append(errs, fmt.Errorf("SIM_ERROR_RATE %q: number in [0, 1] expected", v))
		}
		cfg.errorRate = &f
	}
	if v := getenv("SIM_LOG_LEVEL"); v != "" {
		if err := cfg.logLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("SIM_LOG_LEVEL %q: %w", v, err))
		}
	}
	return cfg, errors.Join(errs...)
}

// apiBehavior combines, in increasing order of priority, the documented limits,
// the scenario's api section and the environment variables.
func (c config) apiBehavior(sc *scenario.Scenario) (volvoapi.Limits, volvoapi.Faults, volvoapi.OAuth) {
	limits := volvoapi.DefaultLimits()
	a := sc.API
	faults := volvoapi.Faults{ErrorRate: a.ErrorRate, Latency: a.Latency}
	limits.TokenTTL = a.TokenTTL
	limits.Keys = c.apiKeys
	for _, o := range a.Outages {
		from := sc.Start.Add(o.After)
		faults.Outages = append(faults.Outages, volvoapi.Outage{From: from, To: from.Add(o.Duration)})
	}
	pick := func(dst *int, values ...*int) {
		for _, v := range values {
			if v != nil {
				*dst = *v
			}
		}
	}
	pick(&limits.DailyQuota, a.DailyQuota, c.dailyQuota)
	pick(&limits.PerMinute, a.PerMinute, c.perMinute)
	if c.tokenTTL != nil {
		limits.TokenTTL = *c.tokenTTL
	}
	if c.latency != nil {
		faults.Latency = *c.latency
	}
	if c.errorRate != nil {
		faults.ErrorRate = *c.errorRate
	}
	return limits, faults, c.oauth(sc)
}

func (c config) oauth(sc *scenario.Scenario) volvoapi.OAuth {
	auth := volvoapi.DefaultOAuth()
	a := sc.API
	pick := func(dst *time.Duration, scenario time.Duration, env *time.Duration) {
		if scenario > 0 {
			*dst = scenario
		}
		if env != nil {
			*dst = *env
		}
	}
	pick(&auth.AccessTokenTTL, a.AccessTokenTTL, c.accessTTL)
	pick(&auth.RefreshTokenTTL, a.RefreshTokenTTL, c.refreshTTL)
	pick(&auth.GrantTTL, a.GrantTTL, c.grantTTL)
	for dst, v := range map[*string]string{&auth.ClientID: c.clientID, &auth.ClientSecret: c.clientSecret, &auth.RedirectURI: c.redirectURI} {
		if v != "" {
			*dst = v
		}
	}
	return auth
}
