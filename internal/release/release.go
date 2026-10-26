// Package release tells whether a newer release of Runsten is out: it reads the latest
// release on GitHub (its API's /releases/latest, which leaves out the pre-releases and
// drafts), at start then once a day, and compares it with the release running. It only
// runs when the instance asks for it (RUNSTEN_UPDATE_CHECK): nothing leaves otherwise.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the latest release of Runsten, on GitHub's API.
const DefaultURL = "https://api.github.com/repos/runsten-app/runsten/releases/latest"

// Every is how often the latest release is read: GitHub allows 60 calls an hour to an
// address without a token, and a release comes out every few days at most.
const Every = 24 * time.Hour

// maxBody caps the response: a release's notes, a few kilobytes.
const maxBody = 1 << 20

// Release is a published release.
type Release struct {
	Version string // X.Y.Z
	URL     string // its page, with its notes
}

// Checker reads the latest release and keeps it.
type Checker struct {
	url     string
	running string
	hc      *http.Client
	log     *slog.Logger

	mu     sync.Mutex
	latest Release
}

// NewChecker creates a checker for the release running, as X.Y.Z (another value, as a
// development build's "dev", is never older than a release). hc must have a timeout.
func NewChecker(url, running string, hc *http.Client, log *slog.Logger) *Checker {
	return &Checker{url: url, running: running, hc: hc, log: log}
}

// Run reads the latest release now, then every Every, until ctx ends. A failure is
// logged and the release read before kept: the next read tries again.
func (c *Checker) Run(ctx context.Context) {
	t := time.NewTicker(Every)
	defer t.Stop()
	for {
		if err := c.Check(ctx); err != nil && ctx.Err() == nil {
			c.log.Warn("the latest release of Runsten cannot be read", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Check reads the latest release once.
func (c *Checker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("latest release: status %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return fmt.Errorf("latest release: %w", err)
	}
	version := strings.TrimPrefix(body.TagName, "v")
	if _, ok := parse(version); !ok || !strings.HasPrefix(body.HTMLURL, "https://github.com/") {
		return errors.New("latest release: not a release")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latest = Release{Version: version, URL: body.HTMLURL}
	return nil
}

// Update is the latest release when it is newer than the one running.
func (c *Checker) Update() (Release, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest.Version == "" || !Newer(c.latest.Version, c.running) {
		return Release{}, false
	}
	return c.latest, true
}

// Newer tells whether the version a, X.Y.Z, comes after b. A b that is no X.Y.Z (a
// development build) is never older.
func Newer(a, b string) bool {
	va, ok := parse(a)
	if !ok {
		return false
	}
	vb, ok := parse(b)
	if !ok {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// parse reads X.Y.Z, without a pre-release.
func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != len(out) {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
