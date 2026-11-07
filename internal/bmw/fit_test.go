package bmw

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func encoded(t *testing.T, w, h int, format string) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w / 2 { // the left half opaque, the right transparent
		for y := range h {
			img.SetNRGBA(x, y, color.NRGBA{R: 0x80, G: 0x90, B: 0xa0, A: 0xff})
		}
	}
	var b bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&b, img)
	} else {
		err = jpeg.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFit(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		raw := encoded(t, 1920, 1080, format)
		out, err := fit(raw, http.DetectContentType(raw))
		if err != nil {
			t.Fatal(err)
		}
		img, got, err := image.Decode(bytes.NewReader(out))
		if err != nil || got != format {
			t.Fatalf("%s: decoded as %q: %v", format, got, err)
		}
		if b := img.Bounds(); b.Dx() != PictureWidth || b.Dy() != 360 {
			t.Errorf("%s: %v", format, b)
		}
		if len(out) >= len(raw) && format == "png" {
			t.Errorf("png: %d bytes from %d", len(out), len(raw))
		}
		if format == "png" {
			if _, _, _, a := img.At(600, 100).RGBA(); a != 0 {
				t.Errorf("transparent ground: alpha %d", a)
			}
			if r, _, _, a := img.At(10, 100).RGBA(); a != 0xffff || r>>8 != 0x80 {
				t.Errorf("opaque half: %d %d", r>>8, a)
			}
		}
	}
	// Narrow enough, or a WebP: as it came.
	narrow := encoded(t, 640, 300, "png")
	if out, err := fit(narrow, "image/png"); err != nil || !bytes.Equal(out, narrow) {
		t.Errorf("narrow: %v", err)
	}
	if out, err := fit([]byte("RIFF....WEBP"), "image/webp"); err != nil || string(out) != "RIFF....WEBP" {
		t.Errorf("webp: %v", err)
	}
	if _, err := fit([]byte("\x89PNG\r\n\x1a\nbroken"), "image/png"); err == nil {
		t.Error("a broken PNG")
	}
}

func TestImageNotAPicture(t *testing.T) {
	// BMW answers, with no picture kept: a PictureError of its status, no APIError.
	for name, body := range map[string][]byte{
		"html":   []byte("<!doctype html><p>no picture</p>"),
		"broken": []byte("\x89PNG\r\n\x1a\nbroken"),
		"large":  append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, MaxImage)...),
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
		_, err := NewClient(Hosts{API: srv.URL}, srv.Client()).Image(t.Context(), "token", "WBY31AW02RFR00001")
		srv.Close()
		var pe *PictureError
		if !errors.As(err, &pe) || pe.Status != http.StatusOK {
			t.Errorf("%s: %v", name, err)
		}
	}
}
