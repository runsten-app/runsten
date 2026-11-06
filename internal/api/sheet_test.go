package api

import (
	"context"
	"encoding/json"
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

// fakeStoredPictures stores pictures by vehicle, and fails while broken.
type fakeStoredPictures struct {
	byVehicle map[string]Picture
	broken    bool
}

func (f *fakeStoredPictures) HasStoredPicture(_ context.Context, _, vehicleID string) (bool, error) {
	if f.broken {
		return false, errors.New("the store failed")
	}
	_, ok := f.byVehicle[vehicleID]
	return ok, nil
}

func (f *fakeStoredPictures) StoredPicture(_ context.Context, _, vehicleID string) (Picture, bool, error) {
	if f.broken {
		return Picture{}, false, errors.New("the store failed")
	}
	p, ok := f.byVehicle[vehicleID]
	return p, ok, nil
}

func TestStoredPicture(t *testing.T) {
	e, c := readerEnv(t)
	e.stored.byVehicle = map[string]Picture{parked: {Type: "image/webp", Data: []byte("webp")}}

	// A vehicle whose source tells no sheet: one of the picture alone.
	resp, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+parked, c)
	if resp.status != http.StatusOK {
		t.Fatalf("vehicle: %d %s", resp.status, body)
	}
	var v struct {
		Sheet *struct {
			Picture bool    `json:"picture"`
			Colour  *string `json:"colour"`
		} `json:"sheet"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	if v.Sheet == nil || !v.Sheet.Picture || v.Sheet.Colour != nil {
		t.Errorf("parked's sheet: %+v", v.Sheet)
	}

	resp, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+parked+"/picture", c)
	if resp.status != http.StatusOK || body != "webp" || resp.header.Get("Content-Type") != "image/webp" ||
		resp.header.Get("Cache-Control") != "private, max-age=86400" {
		t.Errorf("stored: %d %q %v", resp.status, body, resp.header)
	}
	// The relayed one first: the store is not asked of a vehicle whose vendor links to one.
	e.stored.byVehicle[car] = Picture{Type: "image/png", Data: []byte("stored")}
	if _, body := get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+car+"/picture", c); body != "png" {
		t.Errorf("relayed first: %q", body)
	}

	e.stored.broken = true
	resp, body = get(t, noFollow(), e.api.URL+"/api/v1/vehicles/"+parked+"/picture", c)
	if resp.status != http.StatusInternalServerError {
		t.Errorf("broken: %d %s", resp.status, body)
	}
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
