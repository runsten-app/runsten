// Command runsten-simulator is a fake backend mimicking the Volvo Cars API, the
// Polestar Data Portal's M2M API (under polestarapi.Prefix, for the scenarios of
// brand polestar, with the credential polestarapi.DevCredential), Tesla's Owner API
// (under teslaapi.APIPrefix, its token under teslaapi.AuthPrefix and its faults under
// teslaapi.ControlPrefix, for the scenarios of brand tesla, with the refresh token
// teslaapi.DevRefreshToken), and Aximote's public
// API for the imports (/api/public/v1/, its token
// aximoteapi.DevToken): an electric car with the first scenario's VIN, whose history
// ends a month before the simulator started.
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
//	SIM_POLESTAR_CLIENT_ID  the Polestar credential's client ID (default runsten-dev)
//	SIM_POLESTAR_CLIENT_SECRET  its secret (default runsten-dev-secret)
//	SIM_POLESTAR_ACCOUNT_ID its account, the x-client-id header (default
//	                        polestarapi.DevCredential's)
//	SIM_POLESTAR_CREDENTIAL_TTL  the credential's lifetime from the start (default 0 =
//	                        unlimited)
//	SIM_POLESTAR_PARKED_UPLOAD  how often a Polestar not driving uploads its battery and
//	                        availability (default 10m, 0 = only on a change)
//	SIM_TESLA_SLEEP_AFTER   how long a Tesla neither driving, charging nor read falls asleep
//	                        after its last activity (default 10m, 0 = never)
//	SIM_TESLA_TOKEN_TTL     lifetime of the Tesla access tokens issued (default 8h)
//	SIM_LOG_LEVEL           debug, info, warn, error (default info)
//
// SIM_DAILY_QUOTA and SIM_RATE_PER_MINUTE also set the Polestar credential's limits.
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
	"runsten/internal/simulator/aximoteapi"
	"runsten/internal/simulator/control"
	"runsten/internal/simulator/pgledger"
	"runsten/internal/simulator/polestarapi"
	"runsten/internal/simulator/scenario"
	"runsten/internal/simulator/teslaapi"
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

	polestar         polestarapi.Credential
	polestarCredTTL  time.Duration
	polestarBehavior polestarapi.Behavior
	tesla            teslaapi.Behavior
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
	var (
		srcs     []volvoapi.Source
		polestar []polestarapi.Source
		tesla    []teslaapi.Source
	)
	vins := make([]string, 0, len(scs))
	for _, sim := range fleet.Simulations() {
		switch sim.Brand() {
		case scenario.BrandPolestar:
			polestar = append(polestar, sim)
		case scenario.BrandTesla:
			tesla = append(tesla, sim)
		default:
			srcs = append(srcs, sim)
		}
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
	// In real time: the history must stay before the collector's first reading, which
	// it stamps with its own clock.
	mux.Handle(aximoteapi.Prefix, aximoteapi.NewHandler(aximoteapi.DevToken, aximoteapi.Default(clock.Real{}.Now(), vins[0])))
	psLimits := cfg.polestarLimits()
	mux.Handle(polestarapi.Prefix, polestarapi.NewHandler(polestar, clk, cfg.polestar, psLimits, cfg.polestarBehavior))
	th := teslaapi.NewHandler(tesla, clk, cfg.tesla)
	for _, prefix := range []string{teslaapi.APIPrefix, teslaapi.AuthPrefix, teslaapi.ControlPrefix} {
		mux.Handle(prefix, th)
	}
	mux.Handle("/", volvoapi.NewHandler(srcs, clk, limits, faults, auth))

	log.Info("simulation ready", "vins", vins, "start", fleet.Start(), "speed", cfg.speed,
		"daily_quota", limits.DailyQuota, "api_keys", len(limits.Keys), "per_minute", limits.PerMinute, "token_ttl", limits.TokenTTL.String(),
		"error_rate", faults.ErrorRate, "latency", faults.Latency.String(), "outages", len(faults.Outages),
		"client_id", auth.ClientID, "redirect_uri", auth.RedirectURI, "access_token_ttl", auth.AccessTokenTTL.String(),
		"refresh_token_ttl", auth.RefreshTokenTTL.String(), "grant_ttl", auth.GrantTTL.String(),
		"grants_kept", auth.Ledger != nil, "grants_restored", len(auth.Issued.Grants),
		"polestar_vehicles", len(polestar), "polestar_client_id", cfg.polestar.ClientID,
		"polestar_credential_ttl", psLimits.CredentialTTL.String(), "polestar_parked_upload", cfg.polestarBehavior.ParkedUpload.String(),
		"tesla_vehicles", len(tesla), "tesla_sleep_after", cfg.tesla.SleepAfter.String(), "tesla_token_ttl", cfg.tesla.TokenTTL.String())

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

		polestar:         polestarapi.DevCredential(),
		polestarBehavior: polestarapi.DefaultBehavior(),
		tesla:            teslaapi.DefaultBehavior(),
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
	for name, dst := range map[string]*string{
		"SIM_POLESTAR_CLIENT_ID": &cfg.polestar.ClientID, "SIM_POLESTAR_CLIENT_SECRET": &cfg.polestar.ClientSecret,
		"SIM_POLESTAR_ACCOUNT_ID": &cfg.polestar.AccountID,
	} {
		if v := getenv(name); v != "" {
			*dst = v
		}
	}
	for name, dst := range map[string]*time.Duration{
		"SIM_POLESTAR_CREDENTIAL_TTL": &cfg.polestarCredTTL, "SIM_POLESTAR_PARKED_UPLOAD": &cfg.polestarBehavior.ParkedUpload,
	} {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d < 0 {
				errs = append(errs, fmt.Errorf("%s %q: duration ≥ 0 expected", name, v))
			}
			*dst = d
		}
	}
	if v := getenv("SIM_TESLA_SLEEP_AFTER"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			errs = append(errs, fmt.Errorf("SIM_TESLA_SLEEP_AFTER %q: duration ≥ 0 expected", v))
		}
		cfg.tesla.SleepAfter = d
	}
	if v := getenv("SIM_TESLA_TOKEN_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("SIM_TESLA_TOKEN_TTL %q: duration > 0 expected", v))
		}
		cfg.tesla.TokenTTL = d
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

// polestarLimits are the observed limits of the Polestar API, with the environment's
// quota, rate and credential lifetime.
func (c config) polestarLimits() polestarapi.Limits {
	l := polestarapi.DefaultLimits()
	if c.dailyQuota != nil {
		l.DailyQuota = *c.dailyQuota
	}
	if c.perMinute != nil {
		l.PerMinute = *c.perMinute
	}
	l.CredentialTTL = c.polestarCredTTL
	return l
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
