package picture

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

// cdn answers every request as the CDN would, and records what was asked.
type cdn struct {
	status   int
	typ      string
	body     []byte
	location string
	asked    []string
	headers  http.Header
}

func (c *cdn) RoundTrip(req *http.Request) (*http.Response, error) {
	c.asked = append(c.asked, req.URL.String())
	c.headers = req.Header
	h := http.Header{"Content-Type": {c.typ}}
	status := c.status
	if c.location != "" && len(c.asked) == 1 {
		h.Set("Location", c.location)
		status = http.StatusFound
	}
	return &http.Response{
		StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(c.body)), Request: req,
	}, nil
}

const ext = "https://cas.volvocars.com/image/exterior/default.png?market=se&w=1920&bg=00000000&fallback"

func relay(c *cdn, clk clock.Clock, p Params) *Relay {
	return New(p, &http.Client{Transport: c}, clk)
}

func TestServes(t *testing.T) {
	r := relay(&cdn{}, clock.NewManual(time.Unix(0, 0)), DefaultParams())
	for u, want := range map[string]bool{
		ext: true,
		"https://wizz.volvocars.com/images/2026/416/x.png?client=connected-vehicle-api&w=1920": true,
		"https://CAS.volvocars.com/x.png":        true,
		"http://cas.volvocars.com/x.png":         false,
		"https://cas.volvocars.com:8443/x.png":   false,
		"https://user@cas.volvocars.com/x.png":   false,
		"https://cas.volvocars.com.evil/x.png":   false,
		"https://evil.example/cas.volvocars.com": false,
		"":                                       false,
		"::":                                     false,
	} {
		if got := r.Serves(u); got != want {
			t.Errorf("Serves(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestPicture(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0))
	c := &cdn{status: http.StatusOK, typ: "image/png", body: []byte("png")}
	p := DefaultParams()
	p.UserAgent = "Runsten/v1.2.3"
	r := relay(c, clk, p)
	got, err := r.Picture(context.Background(), ext)
	if err != nil || got.Type != "image/png" || string(got.Data) != "png" {
		t.Fatalf("got %+v, %v", got, err)
	}
	// What Volvo's CDN requires, or it refuses.
	if c.headers.Get("User-Agent") != "Runsten/v1.2.3" || c.headers.Get("Accept-Language") == "" {
		t.Errorf("headers %v", c.headers)
	}
	// Asked at 640 px, the rest of the query as it was.
	if want := "https://cas.volvocars.com/image/exterior/default.png?market=se&w=640&bg=00000000&fallback"; c.asked[0] != want {
		t.Errorf("asked %s, want %s", c.asked[0], want)
	}
	// Kept a day, then fetched again.
	clk.Advance(23 * time.Hour)
	if _, err := r.Picture(context.Background(), ext); err != nil || len(c.asked) != 1 {
		t.Errorf("kept: %d fetches, %v", len(c.asked), err)
	}
	clk.Advance(time.Hour)
	if _, err := r.Picture(context.Background(), ext); err != nil || len(c.asked) != 2 {
		t.Errorf("expired: %d fetches, %v", len(c.asked), err)
	}
}

// stalled is a status for byURL: the CDN never answers, until the request is cancelled.
const stalled = -1

// byURL answers each URL with its own status, and records what was asked.
type byURL struct {
	status map[string]int
	asked  []string
}

func (c *byURL) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	c.asked = append(c.asked, u)
	if c.status[u] == stalled {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	return &http.Response{
		StatusCode: c.status[u], Header: http.Header{"Content-Type": {"image/png"}},
		Body: io.NopCloser(strings.NewReader(u)), Request: req,
	}, nil
}

func TestPictureBanner(t *testing.T) {
	const (
		banner = "https://wizz.volvocars.com/images/2027/416/exterior/studio/banner/transparent_exterior-studio-banner_4DD2.png?client=connected-vehicle-api&w=1920"
		sized  = "https://wizz.volvocars.com/images/2027/416/exterior/studio/banner/transparent_exterior-studio-banner_4DD2.png?client=connected-vehicle-api&w=640"
		front  = "https://wizz.volvocars.com/images/2027/416/exterior/studio/front/transparent_exterior-studio-front_4DD2.png?client=connected-vehicle-api&w=640"
	)
	for name, tc := range map[string]struct {
		status map[string]int
		want   string
		asked  int
		kept   bool
	}{
		"its front view": {map[string]int{front: http.StatusOK, sized: http.StatusOK}, front, 1, true},
		"no front view":  {map[string]int{front: http.StatusNotFound, sized: http.StatusOK}, sized, 2, true},
		// The front view is asked again next time.
		"front view failing": {map[string]int{front: http.StatusServiceUnavailable, sized: http.StatusOK}, sized, 2, false},
		"front view stalled": {map[string]int{front: stalled, sized: http.StatusOK}, sized, 2, false},
		"neither":            {map[string]int{front: http.StatusNotFound, sized: http.StatusNotFound}, "", 2, false},
	} {
		c := &byURL{status: tc.status}
		p := DefaultParams()
		p.FrontTimeout = 10 * time.Millisecond
		r := New(p, &http.Client{Transport: c}, clock.NewManual(time.Unix(0, 0)))
		got, err := r.Picture(context.Background(), banner)
		if tc.want == "" {
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("%s: %v", name, err)
			}
		} else if err != nil || string(got.Data) != tc.want {
			t.Errorf("%s: got %q, %v, want %s", name, got.Data, err, tc.want)
		}
		if len(c.asked) != tc.asked {
			t.Errorf("%s: asked %v", name, c.asked)
		}
		_, _ = r.Picture(context.Background(), banner)
		if kept := len(c.asked) == tc.asked; kept != tc.kept {
			t.Errorf("%s: kept %v, want %v", name, kept, tc.kept)
		}
	}
}

func TestPictureRefused(t *testing.T) {
	p := DefaultParams()
	p.MaxSize = 4
	for name, c := range map[string]*cdn{
		"an error":         {status: http.StatusNotFound, typ: "image/png"},
		"not an image":     {status: http.StatusOK, typ: "text/html", body: []byte("<html>")},
		"an SVG":           {status: http.StatusOK, typ: "image/svg+xml", body: []byte("<svg>")},
		"no type":          {status: http.StatusOK, body: []byte("png")},
		"too large":        {status: http.StatusOK, typ: "image/png", body: []byte("12345")},
		"redirected away":  {status: http.StatusOK, typ: "image/png", body: []byte("png"), location: "https://evil.example/x.png"},
		"redirected plain": {status: http.StatusOK, typ: "image/png", body: []byte("png"), location: "http://cas.volvocars.com/x.png"},
	} {
		r := relay(c, clock.NewManual(time.Unix(0, 0)), p)
		if _, err := r.Picture(context.Background(), ext); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A redirect within the CDN is followed.
	c := &cdn{status: http.StatusOK, typ: "image/jpeg; charset=binary", body: []byte("jpg"), location: "https://wizz.volvocars.com/y.jpg"}
	if got, err := relay(c, clock.NewManual(time.Unix(0, 0)), p).Picture(context.Background(), ext); err != nil || got.Type != "image/jpeg" {
		t.Errorf("redirected within: %+v, %v", got, err)
	}
	// Never a URL of another host, even asked directly.
	r := relay(&cdn{status: http.StatusOK, typ: "image/png"}, clock.NewManual(time.Unix(0, 0)), p)
	if _, err := r.Picture(context.Background(), "https://evil.example/x.png"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("another host: %v", err)
	}
}

func TestCacheBounded(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0))
	p := DefaultParams()
	p.MaxBytes = 6
	c := &cdn{status: http.StatusOK, typ: "image/png", body: []byte("abc")}
	r := relay(c, clk, p)
	for _, u := range []string{"https://cas.volvocars.com/1.png", "https://cas.volvocars.com/2.png", "https://cas.volvocars.com/3.png"} {
		if _, err := r.Picture(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		clk.Advance(time.Minute)
	}
	// The oldest went: 2 and 3 hold the 6 bytes.
	if len(r.cache) != 2 || r.held != 6 {
		t.Errorf("cache %d pictures, %d bytes", len(r.cache), r.held)
	}
	if _, ok := r.cache["https://cas.volvocars.com/1.png"]; ok {
		t.Error("the oldest kept")
	}
	// Fetched again, a picture replaces its own bytes.
	clk.Advance(25 * time.Hour)
	if _, err := r.Picture(context.Background(), "https://cas.volvocars.com/3.png"); err != nil || r.held != 6 {
		t.Errorf("refetched: %d bytes, %v", r.held, err)
	}
	// A URL without a query is asked as it is.
	if last := c.asked[len(c.asked)-1]; !strings.HasSuffix(last, "/3.png") {
		t.Errorf("asked %s", last)
	}
}
