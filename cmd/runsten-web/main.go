// Command runsten-web serves Runsten's web interface: the front end's build, and at the
// same origin a relay to runsten-api for its JSON API (/api/) and the Volvo ID flow
// (/auth/). It is the host users open, and the one to put behind an https reverse proxy.
//
//	GET  /healthz   answers 200 while the server runs
//	     /api/…     relayed to runsten-api
//	     /auth/…    relayed to runsten-api
//	GET  /…         the build's files; any other path gets the app (index.html)
//
// Subcommands:
//
//	runsten-web healthcheck   probes /healthz on RUNSTEN_WEB_ADDR and exits 0 if the
//	                          server is healthy: the image has no shell
//
// Configuration through environment variables:
//
//	RUNSTEN_WEB_ADDR           listen address (default 127.0.0.1:8082). Plain http:
//	                           beyond this machine, put an https reverse proxy in front
//	RUNSTEN_WEB_API_URL        runsten-api (default http://127.0.0.1:8081)
//	RUNSTEN_WEB_API_HOST       the Host runsten-api is sent: browser, the one the browser
//	                           used (default), or api, the one of RUNSTEN_WEB_API_URL,
//	                           the browser's staying in X-Forwarded-Host. api is for a
//	                           platform that routes by Host to runsten-api's own site;
//	                           unsafe requests then need a browser sending Sec-Fetch-Site
//	RUNSTEN_WEB_DIR            the front end's build (default /web, as in the image)
//	RUNSTEN_WEB_BASE_PATH      public prefix, when the reverse proxy serves the site
//	                           under a path and strips it (default /)
//	RUNSTEN_WEB_MAP_TILES      the maps' tiles: raster tiles, a URL with {z}, {x} and {y},
//	                           as https://tile.openstreetmap.org/{z}/{x}/{y}.png, or a
//	                           PMTiles file of vector tiles (Protomaps), a URL ending in
//	                           .pmtiles, its host answering range requests with CORS
//	                           (default none: no map, nothing fetched from a third
//	                           party). The browser fetches them: their host sees its
//	                           address and the areas shown
//	RUNSTEN_WEB_MAP_ATTRIBUTION  the attribution the tiles' provider requires, as plain
//	                           text (default © OpenStreetMap contributors)
//	RUNSTEN_WEB_NOINDEX        true: every response asks search engines not to index
//	                           it (X-Robots-Tag), for an instance on the internet that
//	                           should not be found (default false)
//	RUNSTEN_ACCESS_RESTRICTED  true when the deployment only lets RUNSTEN_WEB_ADDR be
//	                           reached through the host's loopback or an https reverse
//	                           proxy: a non-loopback address is then not warned about
//	RUNSTEN_LOG_LEVEL          debug, info, warn, error (default info)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"runsten/internal/platform/health"
	"runsten/internal/platform/httpserver"
	"runsten/internal/web"
)

const (
	defaultAddr = "127.0.0.1:8082"
	defaultAPI  = "http://127.0.0.1:8081"
	defaultDir  = "/web"
	// defaultMapAttribution is what every provider of OpenStreetMap's data requires.
	defaultMapAttribution = "© OpenStreetMap contributors"
	// probeTimeout bounds the healthcheck subcommand, below the container healthcheck
	// timeout.
	probeTimeout = 5 * time.Second
)

type config struct {
	addr       string
	api        *url.URL
	dir        string
	basePath   string
	apiHost    bool
	restricted bool
	noIndex    bool
	logLevel   slog.Level
	mapTiles   *web.MapTiles
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "runsten-web:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "healthcheck":
		cfg, err := loadConfig(os.Getenv)
		if err != nil {
			return fmt.Errorf("configuration: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		return health.Probe(ctx, &http.Client{Timeout: probeTimeout}, cfg.addr) //nolint:wrapcheck // already has context
	case len(args) > 0:
		return fmt.Errorf("unknown command %q (expected: healthcheck, or none to serve)", args)
	}
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler, err := newHandler(cfg, log)
	if err != nil {
		return err
	}
	httpserver.WarnIfExposed(log, cfg.addr, cfg.restricted,
		"listening beyond this machine over plain http: put an https reverse proxy in front of runsten-web, passwords and session cookies travel in clear text otherwise")
	log.Info("runsten-web started", "addr", cfg.addr, "api", cfg.api.String(), "api_host", cfg.apiHost, "dir", cfg.dir, "base_path", cfg.basePath, "maps", cfg.mapTiles != nil, "noindex", cfg.noIndex)
	if err := httpserver.Run(ctx, httpserver.New(cfg.addr, handler, log), log); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

func newHandler(cfg config, log *slog.Logger) (http.Handler, error) {
	app, err := web.New(web.Config{
		Files: os.DirFS(cfg.dir), API: cfg.api, BasePath: cfg.basePath, Log: log, APIHost: cfg.apiHost, Map: cfg.mapTiles,
		NoIndex: cfg.noIndex,
	})
	if err != nil {
		return nil, fmt.Errorf("front end in %s: %w", cfg.dir, err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET "+health.Path, health.OK()) // not logged: probed every 30 s
	mux.Handle("/", httpserver.LogRequests(log, app))
	return mux, nil
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{addr: defaultAddr, dir: defaultDir, basePath: "/", logLevel: slog.LevelInfo}
	var errs []error
	for name, dst := range map[string]*string{
		"RUNSTEN_WEB_ADDR": &cfg.addr, "RUNSTEN_WEB_DIR": &cfg.dir, "RUNSTEN_WEB_BASE_PATH": &cfg.basePath,
	} {
		if v := getenv(name); v != "" {
			*dst = v
		}
	}
	api := defaultAPI
	if v := getenv("RUNSTEN_WEB_API_URL"); v != "" {
		api = v
	}
	u, err := url.Parse(api)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" && u.Path != "/" {
		errs = append(errs, fmt.Errorf("RUNSTEN_WEB_API_URL %q: http(s) URL of runsten-api expected, without path", api))
	}
	cfg.api = u
	switch v := getenv("RUNSTEN_WEB_API_HOST"); v {
	case "", "browser":
	case "api":
		cfg.apiHost = true
	default:
		errs = append(errs, fmt.Errorf("RUNSTEN_WEB_API_HOST %q: browser or api expected", v))
	}
	if v := getenv("RUNSTEN_ACCESS_RESTRICTED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_ACCESS_RESTRICTED %q: true or false expected", v))
		}
		cfg.restricted = b
	}
	if v := getenv("RUNSTEN_WEB_NOINDEX"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_WEB_NOINDEX %q: true or false expected", v))
		}
		cfg.noIndex = b
	}
	if v := getenv("RUNSTEN_WEB_MAP_TILES"); v != "" {
		cfg.mapTiles = &web.MapTiles{URL: v, Attribution: getenv("RUNSTEN_WEB_MAP_ATTRIBUTION")}
		if cfg.mapTiles.Attribution == "" {
			cfg.mapTiles.Attribution = defaultMapAttribution
		}
	}
	if v := getenv("RUNSTEN_LOG_LEVEL"); v != "" {
		if err := cfg.logLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("RUNSTEN_LOG_LEVEL %q: %w", v, err))
		}
	}
	return cfg, errors.Join(errs...)
}
