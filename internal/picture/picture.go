// Package picture relays the vendors' pictures of the vehicles, so that the browser loads
// them from Runsten and calls no third party: it fetches a picture from its vendor's CDN,
// and keeps it in memory a while.
package picture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"runsten/internal/platform/clock"
)

// Params bound what the relay fetches and keeps.
type Params struct {
	// Hosts are the hosts a picture may come from: the vendors' CDNs, over https.
	Hosts []string
	// Width is the width asked of the CDN in place of the URL's w; 0 keeps it.
	// Assumption: the CDN scales the picture to it, as its parameter says. A thumbnail
	// and a tile need no more than 640 px; the details ask for 1920.
	Width int
	// MaxSize is the largest picture relayed, MaxBytes what the cache holds in all.
	MaxSize, MaxBytes int
	// TTL is how long a picture is kept.
	TTL time.Duration
}

// DefaultParams are the relay's for the pictures Volvo's details link to, read on
// cas.volvocars.com and wizz.volvocars.com (2026-10).
func DefaultParams() Params {
	return Params{
		Hosts: []string{"cas.volvocars.com", "wizz.volvocars.com"},
		Width: 640, MaxSize: 4 << 20, MaxBytes: 32 << 20, TTL: 24 * time.Hour,
	}
}

// Picture is an image and its media type.
type Picture struct {
	Type string
	Data []byte
}

// ErrUnavailable is the CDN failing to give a picture: an error of its own, a picture
// too large, or anything but an image.
var ErrUnavailable = errors.New("picture unavailable")

// types are the media types relayed: raster images only, which the browser shows without
// running anything.
var types = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

// Relay fetches and keeps the pictures.
type Relay struct {
	p   Params
	hc  *http.Client
	clk clock.Clock

	mu    sync.Mutex
	cache map[string]entry
	held  int // bytes held
}

type entry struct {
	pic Picture
	at  time.Time
}

// New returns a relay fetching with hc, which follows a redirect only to a host of p.
func New(p Params, hc *http.Client, clk clock.Clock) *Relay {
	c := *hc
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !allowed(p.Hosts, req.URL) {
			return ErrUnavailable
		}
		return nil
	}
	return &Relay{p: p, hc: &c, clk: clk, cache: map[string]entry{}}
}

// Serves tells whether the relay fetches a picture from u: an https URL of one of its
// hosts.
func (r *Relay) Serves(u string) bool {
	parsed, err := url.Parse(u)
	return err == nil && allowed(r.p.Hosts, parsed)
}

func allowed(hosts []string, u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	for _, h := range hosts {
		if strings.EqualFold(u.Hostname(), h) {
			return true
		}
	}
	return false
}

// Picture returns the picture at u, kept or fetched. A URL the relay does not serve is
// an error, as is the CDN failing (ErrUnavailable).
func (r *Relay) Picture(ctx context.Context, u string) (Picture, error) {
	if !r.Serves(u) {
		return Picture{}, fmt.Errorf("%w: not a vendor's URL", ErrUnavailable)
	}
	now := r.clk.Now()
	r.mu.Lock()
	e, ok := r.cache[u]
	r.mu.Unlock()
	if ok && now.Sub(e.at) < r.p.TTL {
		return e.pic, nil
	}
	pic, err := r.fetch(ctx, r.sized(u))
	if err != nil {
		return Picture{}, err
	}
	r.keep(u, entry{pic, now})
	return pic, nil
}

// sized replaces the URL's w with the relay's width, leaving the rest of its query as it
// is: the CDN's parameters include one without a value (fallback).
func (r *Relay) sized(u string) string {
	base, query, ok := strings.Cut(u, "?")
	if !ok || r.p.Width <= 0 {
		return u
	}
	parts := strings.Split(query, "&")
	for i, p := range parts {
		if strings.HasPrefix(p, "w=") {
			parts[i] = fmt.Sprintf("w=%d", r.p.Width)
		}
	}
	return base + "?" + strings.Join(parts, "&")
}

func (r *Relay) fetch(ctx context.Context, u string) (Picture, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Picture{}, fmt.Errorf("picture request: %w", err)
	}
	req.Header.Set("Accept", "image/png,image/webp,image/jpeg")
	resp, err := r.hc.Do(req)
	if err != nil {
		return Picture{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Picture{}, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !types[mt] {
		return Picture{}, fmt.Errorf("%w: type %q", ErrUnavailable, resp.Header.Get("Content-Type"))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(r.p.MaxSize)+1))
	if err != nil {
		return Picture{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(data) > r.p.MaxSize {
		return Picture{}, fmt.Errorf("%w: larger than %d bytes", ErrUnavailable, r.p.MaxSize)
	}
	return Picture{Type: mt, Data: data}, nil
}

// keep adds e to the cache, dropping the oldest pictures while it holds too much.
func (r *Relay) keep(u string, e entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.cache[u]; ok {
		r.held -= len(old.pic.Data)
	}
	r.cache[u] = e
	r.held += len(e.pic.Data)
	for r.held > r.p.MaxBytes {
		var oldest string
		for k, v := range r.cache {
			if oldest == "" || v.at.Before(r.cache[oldest].at) {
				oldest = k
			}
		}
		r.held -= len(r.cache[oldest].pic.Data)
		delete(r.cache, oldest)
	}
}
