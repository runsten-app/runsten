package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// fakePictures relays the pictures of cas.volvocars.com, and fails while broken.
type fakePictures struct {
	broken bool
	asked  []string
}

func (f *fakePictures) Serves(u string) bool {
	return strings.HasPrefix(u, "https://cas.volvocars.com/")
}

func (f *fakePictures) Picture(_ context.Context, u string) (Picture, error) {
	f.asked = append(f.asked, u)
	if f.broken {
		return Picture{}, errors.New("the CDN failed")
	}
	return Picture{Type: "image/png", Data: []byte("png")}, nil
}

func TestVehiclePicture(t *testing.T) {
	e, c := readerEnv(t)
	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/picture", c)
	if resp.status != http.StatusOK || body != "png" || resp.header.Get("Content-Type") != "image/png" ||
		resp.header.Get("Cache-Control") != "private, max-age=86400" {
		t.Errorf("picture: %d %q %v", resp.status, body, resp.header)
	}
	// The details' URL, never the request's.
	if len(e.pictures.asked) != 1 || e.pictures.asked[0] != xc40Picture {
		t.Errorf("asked %v", e.pictures.asked)
	}

	// No sheet, a picture the relay does not serve, a vehicle of another account.
	for _, id := range []string{parked, chosen, foreign} {
		resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+id+"/picture", c)
		if resp.status != http.StatusNotFound {
			t.Errorf("%s: %d %s", id, resp.status, body)
		}
		wantError(t, body, "not_found")
	}

	e.pictures.broken = true
	resp, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/picture", c)
	if resp.status != http.StatusBadGateway {
		t.Errorf("broken: %d %s", resp.status, body)
	}
	wantError(t, body, "source_unavailable")
}
