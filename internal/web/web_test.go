package web

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
)

const indexHTML = `<!doctype html><html><head>
<base href="/" />
<meta name="csp-nonce" content="" />
<script type="module" src="./assets/app-1a2b.js"></script>
</head><body><div id="app"></div></body></html>`

func build() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte(indexHTML)},
		"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
		"favicon.ico":        {Data: []byte("ico")},
	}
}

var quiet = slog.New(slog.DiscardHandler)

func newServer(t *testing.T, api *url.URL, base string) http.Handler {
	t.Helper()
	if api == nil {
		api, _ = url.Parse("http://127.0.0.1:1")
	}
	h, err := New(Config{Files: build(), API: api, BasePath: base, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 1024)), Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func do(t *testing.T, h http.Handler, method, target string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestNewChecksTheBuild(t *testing.T) {
	api, _ := url.Parse("http://api")
	for name, files := range map[string]fstest.MapFS{
		"no index":       {},
		"no base":        {"index.html": {Data: []byte(`<meta name="csp-nonce" content="" />`)}},
		"no nonce":       {"index.html": {Data: []byte(`<base href="/" />`)}},
		"two nonces":     {"index.html": {Data: []byte(`<base href="/" /><meta name="csp-nonce" content="" /><meta name="csp-nonce" content="" />`)}},
		"base path only": nil,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := Config{Files: files, API: api, Log: quiet}
			if files == nil {
				cfg.Files, cfg.BasePath = build(), "runsten"
			}
			if _, err := New(cfg); err == nil {
				t.Error("no error")
			}
		})
	}
}

func TestBasePath(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"", "/", true},
		{"/", "/", true},
		{"/runsten", "/runsten/", true},
		{"/a/b/", "/a/b/", true},
		{"runsten/", "", false},
		{"/a/../b/", "", false},
		{"/a//b/", "", false},
		{`/a"onload="x/`, "", false},
		{"/a?b/", "", false},
	} {
		got, err := basePath(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("basePath(%q) = %q, %v", tc.in, got, err)
		}
	}
}

func TestStatic(t *testing.T) {
	h := newServer(t, nil, "/runsten")
	for _, tc := range []struct {
		name, method, path string
		status             int
		cache, body        string
		app                bool // the app's page, with its CSP
	}{
		{"root", http.MethodGet, "/", http.StatusOK, "no-cache", `<base href="/runsten/" />`, true},
		{"index by name", http.MethodGet, "/index.html", http.StatusOK, "no-cache", `<div id="app">`, true},
		{"route of the app", http.MethodGet, "/vehicles/v1/trips/2026-09-28T07:01:00.000000Z", http.StatusOK, "no-cache", `<div id="app">`, true},
		{"asset", http.MethodGet, "/assets/app-1a2b.js", http.StatusOK, "public, max-age=31536000, immutable", "console.log(1)", false},
		{"asset of an old build", http.MethodGet, "/assets/app-0000.js", http.StatusNotFound, "", "404", false},
		{"other file", http.MethodGet, "/favicon.ico", http.StatusOK, "no-cache", "ico", false},
		{"directory", http.MethodGet, "/assets/", http.StatusOK, "no-cache", `<div id="app">`, true},
		{"no escape from the build", http.MethodGet, "/../../etc/passwd", http.StatusOK, "no-cache", `<div id="app">`, true},
		{"head", http.MethodHead, "/", http.StatusOK, "no-cache", "", true},
		{"post", http.MethodPost, "/", http.StatusMethodNotAllowed, "", "method not allowed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, tc.method, tc.path, nil)
			res := rec.Result()
			if res.StatusCode != tc.status || res.Header.Get("Cache-Control") != tc.cache ||
				!strings.Contains(rec.Body.String(), tc.body) {
				t.Fatalf("status %d, cache %q, body %q", res.StatusCode, res.Header.Get("Cache-Control"), rec.Body.String())
			}
			if res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("Referrer-Policy") != "no-referrer" {
				t.Errorf("headers %v", res.Header)
			}
			if csp := res.Header.Get("Content-Security-Policy"); (csp != "") != tc.app {
				t.Errorf("CSP %q", csp)
			}
			if tc.method == http.MethodHead && rec.Body.Len() != 0 {
				t.Error("HEAD with a body")
			}
			if tc.status == http.StatusMethodNotAllowed && res.Header.Get("Allow") != "GET, HEAD" {
				t.Error("no Allow")
			}
		})
	}
}

func TestNonce(t *testing.T) {
	h := newServer(t, nil, "")
	rec := do(t, h, http.MethodGet, "/", nil)
	nonce := "BwcHBwcHBwcHBwcHBwcHBw==" // 16 bytes of 7, from the injected source
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self';", "style-src 'self' 'nonce-" + nonce + "'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q without %q", csp, want)
		}
	}
	if !strings.Contains(rec.Body.String(), `<meta name="csp-nonce" content="`+nonce+`" />`) ||
		!strings.Contains(rec.Body.String(), `<base href="/" />`) {
		t.Errorf("page %s", rec.Body.String())
	}

	failing, err := New(Config{Files: build(), API: &url.URL{Scheme: "http", Host: "api"}, Rand: iotest{}, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(t, failing, http.MethodGet, "/", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("without randomness: %d", rec.Code)
	}
}

func TestMap(t *testing.T) {
	api, _ := url.Parse("http://api")
	// Without tiles: images from the origin only, and no map meta.
	rec := do(t, newServer(t, nil, ""), http.MethodGet, "/", nil)
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' data:;") {
		t.Errorf("CSP %q", csp)
	}
	if strings.Contains(rec.Body.String(), "map-tiles") {
		t.Errorf("page %s", rec.Body.String())
	}

	h, err := New(Config{Files: build(), API: api, Log: quiet, Map: &MapTiles{
		URL: "https://tile.openstreetmap.org/{z}/{x}/{y}.png", Attribution: `© OpenStreetMap <contributors> & "friends"`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/trips", nil)
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' data: https://tile.openstreetmap.org;") ||
		!strings.Contains(csp, "connect-src 'self';") {
		t.Errorf("CSP %q", csp)
	}
	for _, want := range []string{
		`<meta name="map-tiles" content="https://tile.openstreetmap.org/{z}/{x}/{y}.png" />`,
		`<meta name="map-attribution" content="© OpenStreetMap &lt;contributors&gt; &amp; &#34;friends&#34;" />`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("page without %s: %s", want, rec.Body.String())
		}
	}

	// A PMTiles file: the browser reads it by range requests, in connect-src.
	h, err = New(Config{Files: build(), API: api, Log: quiet, Map: &MapTiles{
		URL: "https://maps.example/europe.pmtiles", Attribution: "Protomaps © OpenStreetMap",
	}})
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/", nil)
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' data:;") ||
		!strings.Contains(csp, "connect-src 'self' https://maps.example;") {
		t.Errorf("CSP %q", csp)
	}
	if want := `<meta name="map-tiles" content="https://maps.example/europe.pmtiles" />`; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("page without %s: %s", want, rec.Body.String())
	}

	for name, m := range map[string]MapTiles{
		"pmtiles without scheme": {URL: "maps.example/europe.pmtiles", Attribution: "a"},
		"pmtiles in the query":   {URL: "https://maps.example/tiles?file=europe.pmtiles", Attribution: "a"},
		"no z":                   {URL: "https://tiles.example/{x}/{y}.png", Attribution: "a"},
		"no scheme":              {URL: "tiles.example/{z}/{x}/{y}.png", Attribution: "a"},
		"other scheme":           {URL: "ftp://tiles.example/{z}/{x}/{y}.png", Attribution: "a"},
		"subdomains":             {URL: "https://{s}.tiles.example/{z}/{x}/{y}.png", Attribution: "a"},
		"no attribution":         {URL: "https://tiles.example/{z}/{x}/{y}.png"},
	} {
		if _, err := New(Config{Files: build(), API: api, Log: quiet, Map: &m}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

type iotest struct{}

func (iotest) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestRelay(t *testing.T) {
	type seen struct{ host, path, query, cookie, forwardedHost string }
	var got seen
	// runsten-api refuses cross-origin unsafe requests: the relay must keep Host.
	backend := httptest.NewServer(http.NewCrossOriginProtection().Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{r.Host, r.URL.Path, r.URL.RawQuery, r.Header.Get("Cookie"), r.Header.Get("X-Forwarded-Host")}
		if r.URL.Path == "/auth/volvo/start" {
			w.Header().Set("Location", "https://volvoid.example/as/authorization.oauth2?state=s")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Set-Cookie", "runsten_session=t; Path=/; HttpOnly")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})))
	defer backend.Close()
	api, _ := url.Parse(backend.URL)
	h := newServer(t, api, "")

	rec := do(t, h, http.MethodPost, "http://runsten.example/api/v1/session?x=1", http.Header{
		"Origin": {"http://runsten.example"}, "Cookie": {"runsten_session=old"},
	})
	if rec.Code != http.StatusOK || rec.Body.String() != `{"ok":true}` ||
		rec.Header().Get("Set-Cookie") != "runsten_session=t; Path=/; HttpOnly" {
		t.Fatalf("relayed POST: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if got != (seen{"runsten.example", "/api/v1/session", "x=1", "runsten_session=old", "runsten.example"}) {
		t.Errorf("runsten-api saw %+v", got)
	}
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("the relay adds the app's CSP to API responses")
	}

	if rec := do(t, h, http.MethodPost, "http://runsten.example/api/v1/session", http.Header{"Origin": {"https://evil.example"}}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST through the relay: %d", rec.Code)
	}

	rec = do(t, h, http.MethodGet, "http://runsten.example/auth/volvo/start", nil)
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://volvoid.example/") {
		t.Errorf("Volvo ID start: %d %v", rec.Code, rec.Header())
	}

	for _, p := range []string{"/.well-known/oauth-protected-resource/api/mcp", "/.well-known/oauth-authorization-server"} {
		if rec := do(t, h, http.MethodGet, "http://runsten.example"+p, nil); rec.Code != http.StatusOK || got.path != p {
			t.Errorf("OAuth metadata %s: %d, runsten-api saw %q", p, rec.Code, got.path)
		}
	}
	if rec := do(t, h, http.MethodGet, "http://runsten.example/.well-known/security.txt", nil); got.path == "/.well-known/security.txt" {
		t.Errorf("another well-known path relayed: %d", rec.Code)
	}
}

func TestRelayWithAPIHost(t *testing.T) {
	type seen struct{ host, forwardedHost string }
	var got seen
	backend := httptest.NewServer(http.NewCrossOriginProtection().Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{r.Host, r.Header.Get("X-Forwarded-Host")}
		w.WriteHeader(http.StatusNoContent)
	})))
	defer backend.Close()
	api, _ := url.Parse(backend.URL)
	h, err := New(Config{Files: build(), API: api, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 1024)), Log: quiet, APIHost: true})
	if err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, http.MethodPost, "http://runsten.example/api/v1/session", http.Header{
		"Origin": {"http://runsten.example"}, "Sec-Fetch-Site": {"same-origin"},
	})
	if rec.Code != http.StatusNoContent || got != (seen{api.Host, "runsten.example"}) {
		t.Errorf("same-origin POST: %d, runsten-api saw %+v", rec.Code, got)
	}
	for name, header := range map[string]http.Header{
		"cross-site":                    {"Origin": {"https://evil.example"}, "Sec-Fetch-Site": {"cross-site"}},
		"Origin without Sec-Fetch-Site": {"Origin": {"http://runsten.example"}},
	} {
		if rec := do(t, h, http.MethodPost, "http://runsten.example/api/v1/session", header); rec.Code != http.StatusForbidden {
			t.Errorf("%s POST: %d", name, rec.Code)
		}
	}
}

func TestRelayUnavailable(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	api, _ := url.Parse(down.URL)
	down.Close()
	h := newServer(t, api, "")

	rec := do(t, h, http.MethodGet, "/api/v1/vehicles", nil)
	if rec.Code != http.StatusBadGateway || rec.Header().Get("Content-Type") != "application/json" ||
		!strings.Contains(rec.Body.String(), `"code":"unavailable"`) {
		t.Errorf("API: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/auth/volvo/start", nil)
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "{") {
		t.Errorf("auth: %d %q", rec.Code, rec.Body.String())
	}
}
