// Package web serves runsten-web: the front end's build (a single-page application) and,
// at the same origin, a relay to runsten-api for /api/, /auth/ and the OAuth metadata
// under /.well-known/. Being the same origin
// is what lets the session cookie and CrossOriginProtection work unchanged.
//
// It holds no session, secret or data: requests to runsten-api pass through.
package web

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
)

// Placeholders of web/index.html, filled by the server.
var (
	baseTag  = []byte(`<base href="/" />`)
	nonceTag = []byte(`<meta name="csp-nonce" content="" />`)
)

// csp allows only the app's own files, and the map tiles' origin when the instance has
// one: in img-src for raster tiles, in connect-src for a PMTiles file, which the browser
// reads by range requests. Vuetify writes its theme into a <style> element: it carries
// the per-response nonce, which the front end reads from the csp-nonce meta.
const csp = "default-src 'none'; script-src 'self'; style-src 'self' 'nonce-%s'; img-src 'self' data:%s; " +
	"connect-src 'self'%s; font-src 'self'; manifest-src 'self'; base-uri 'self'; form-action 'self'; " +
	"frame-ancestors 'none'"

// MapTiles are where the maps' tiles come from, and the attribution their provider
// requires, as plain text. The URL is either raster tiles, with {z}, {x} and {y}, as
// https://tile.openstreetmap.org/{z}/{x}/{y}.png, or a PMTiles file of vector tiles
// (Protomaps), its path ending in .pmtiles. The browser fetches them itself: the host
// learns the user's address and the areas they look at.
type MapTiles struct {
	URL, Attribution string
}

// vector tells whether the tiles are a PMTiles file.
func (t MapTiles) vector() bool {
	u, err := url.Parse(t.URL)
	return err == nil && strings.HasSuffix(u.Path, ".pmtiles")
}

// origin checks the URL of the tiles and gives their origin, for the CSP.
func (t MapTiles) origin() (string, error) {
	if !t.vector() {
		for _, p := range []string{"{z}", "{x}", "{y}"} {
			if !strings.Contains(t.URL, p) {
				return "", fmt.Errorf("map tiles %q: no %s, nor a .pmtiles file", t.URL, p)
			}
		}
	}
	u, err := url.Parse(strings.NewReplacer("{z}", "0", "{x}", "0", "{y}", "0").Replace(t.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(u.Host, "{}") {
		return "", fmt.Errorf("map tiles %q: an http(s) URL with {z}, {x} and {y}, or of a .pmtiles file, expected, its host fixed", t.URL)
	}
	if t.Attribution == "" {
		return "", fmt.Errorf("map tiles %q: an attribution is required", t.URL)
	}
	return u.Scheme + "://" + u.Host, nil
}

// relayed are the path prefixes that belong to runsten-api: its JSON API, the Volvo ID
// flow, whose callback is registered on the host users open (this one), and the OAuth
// metadata of a protected resource (RFC 9728) and of an authorization server (RFC
// 8414), which a client looks for at the root of that host. runsten-api answers 404 to
// these as long as it serves neither.
var relayed = []string{
	"/api/", "/auth/",
	"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server",
}

// Config configures the handler.
type Config struct {
	Files    fs.FS     // the build: index.html and assets/
	API      *url.URL  // runsten-api
	BasePath string    // public prefix of the site behind a reverse proxy, "/" by default
	Rand     io.Reader // nonces; crypto/rand when nil
	Log      *slog.Logger
	// APIHost sends runsten-api the host of its own URL rather than the browser's, which
	// stays in X-Forwarded-Host: for a platform that routes requests by Host, where
	// runsten-api is another site (as on some serverless container platforms). runsten-api's
	// CrossOriginProtection then trusts Sec-Fetch-Site alone: a browser that sends
	// Origin without it is refused its unsafe requests.
	APIHost bool
	// Map shows maps with these tiles; nil: no map, nothing fetched from a third party.
	Map *MapTiles
}

type server struct {
	files  fs.FS
	index  [][]byte // index.html split around the nonce
	imgSrc string   // the raster tiles' origin, with its leading space, for the CSP
	conSrc string   // the PMTiles file's origin, likewise
	meta   []byte   // the map's metas, after the nonce's
	proxy  *httputil.ReverseProxy
	rand   io.Reader
	log    *slog.Logger
}

// New checks the build and returns the handler.
func New(cfg Config) (http.Handler, error) {
	base, err := basePath(cfg.BasePath)
	if err != nil {
		return nil, err
	}
	page, err := fs.ReadFile(cfg.Files, "index.html")
	if err != nil {
		return nil, fmt.Errorf("reading the build: %w", err)
	}
	if bytes.Count(page, baseTag) != 1 || bytes.Count(page, nonceTag) != 1 {
		return nil, errors.New("index.html: expected one base and one csp-nonce placeholder")
	}
	page = bytes.Replace(page, baseTag, fmt.Appendf(nil, `<base href="%s" />`, base), 1)
	s := &server{
		files: cfg.Files,
		index: bytes.SplitN(page, nonceTag, 2),
		rand:  cfg.Rand,
		log:   cfg.Log,
	}
	if s.rand == nil {
		s.rand = rand.Reader
	}
	if m := cfg.Map; m != nil {
		origin, err := m.origin()
		if err != nil {
			return nil, err
		}
		if m.vector() {
			s.conSrc = " " + origin
		} else {
			s.imgSrc = " " + origin
		}
		s.meta = fmt.Appendf(nil, `<meta name="map-tiles" content="%s" /><meta name="map-attribution" content="%s" />`,
			html.EscapeString(m.URL), html.EscapeString(m.Attribution))
	}
	api, apiHost := cfg.API, cfg.APIHost
	s.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(api)
			// runsten-api compares the Origin of unsafe requests with Host
			// (CrossOriginProtection): it must see the host the browser used.
			if !apiHost {
				r.Out.Host = r.In.Host
			}
			r.SetXForwarded()
		},
		ErrorHandler: s.unavailable,
	}
	return s, nil
}

// basePath validates the public prefix: an absolute path, ending with a slash.
func basePath(p string) (string, error) {
	if p == "" {
		return "/", nil
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	// It goes into an HTML attribute: no quote nor markup, and nothing to resolve.
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, `"'<>&?# `) || p != "/" && path.Clean(p)+"/" != p {
		return "", fmt.Errorf("base path %q: an absolute, clean path expected, such as /runsten/", p)
	}
	return p, nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// An instance holds one household's data behind a sign-in: nothing for a search
	// engine, relayed pages included.
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	for _, prefix := range relayed {
		if strings.HasPrefix(r.URL.Path, prefix) {
			s.proxy.ServeHTTP(w, r)
			return
		}
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" && fs.ValidPath(name) {
		if st, err := fs.Stat(s.files, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// Vite hashes the names of assets: a name never changes content.
				h.Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				h.Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, s.files, name) //nolint:gosec // a valid fs.FS path cannot leave the build's root
			return
		}
		if strings.HasPrefix(name, "assets/") {
			// An old page asking for an asset of a previous build: not the app in its place.
			http.NotFound(w, r)
			return
		}
	}
	s.serveIndex(w, r)
}

// serveIndex serves the app for any other path: the router of the front end reads it.
func (s *server) serveIndex(w http.ResponseWriter, r *http.Request) {
	var b [16]byte
	if _, err := io.ReadFull(s.rand, b[:]); err != nil {
		s.log.ErrorContext(r.Context(), "nonce", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce := base64.StdEncoding.EncodeToString(b[:])
	h := w.Header()
	h.Set("Content-Security-Policy", fmt.Sprintf(csp, nonce, s.imgSrc, s.conSrc))
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	var page bytes.Buffer
	page.Write(s.index[0])
	fmt.Fprintf(&page, `<meta name="csp-nonce" content="%s" />`, nonce)
	page.Write(s.meta)
	page.Write(s.index[1])
	_, _ = page.WriteTo(w)
}

// unavailable answers when runsten-api cannot be reached, in the error format of its
// JSON API for /api/.
func (s *server) unavailable(w http.ResponseWriter, r *http.Request, err error) {
	s.log.WarnContext(r.Context(), "runsten-api unreachable", "path", r.URL.Path, "error", err)
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		http.Error(w, "runsten-api is unreachable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadGateway)
	_, _ = io.WriteString(w, `{"error":{"code":"unavailable","message":"runsten-api is unreachable"}}`+"\n")
}
