package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImportTeslaMate(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	url := e.api.URL + "/api/v1/vehicles/" + car + "/imports/teslamate"
	const backup = "COPY public.cars (id) FROM stdin;\n1\n\\.\n"

	// A Volvo: nothing read.
	resp, out := do(t, noFollow(), http.MethodPost, url, "application/octet-stream", backup, c)
	if resp.status != http.StatusConflict || e.files.backup != "" {
		t.Errorf("a Volvo: %d %s", resp.status, out)
	}
	wantError(t, out, codeNotTesla)

	e.reader.vehicles[account] = []Vehicle{{ID: car, VIN: teslaVIN, Provider: "tesla"}}
	resp, out = do(t, noFollow(), http.MethodPost, url+"?currency=EUR", "application/gzip", backup, c)
	if resp.status != http.StatusOK {
		t.Fatalf("%d %s", resp.status, out)
	}
	golden(t, "import-teslamate.json", out)
	if e.files.backup != backup || e.files.vin != teslaVIN || e.files.currency != "EUR" {
		t.Errorf("read %+v", e.files)
	}
	if resp, out := do(t, noFollow(), http.MethodPost, url, "application/octet-stream", backup, c); resp.status != http.StatusOK ||
		e.files.currency != "" {
		t.Errorf("without a currency: %d %s, %q", resp.status, out, e.files.currency)
	}
	if !strings.Contains(logs.String(), "backup imported") {
		t.Errorf("logs:\n%s", logs)
	}

	for _, tc := range []struct {
		name, query, contentType, body string
		status                         int
		code                           errorCode
	}{
		{"a currency Runsten does not accept", "?currency=XYZ", "application/octet-stream", backup, http.StatusBadRequest, codeInvalidParameter},
		{"not a currency", "?currency=eur", "application/octet-stream", backup, http.StatusBadRequest, codeInvalidParameter},
		{"an archive", "", "application/octet-stream", "PGDMP", http.StatusBadRequest, codeInvalidFile},
		{"another car", "", "application/octet-stream", "other", http.StatusConflict, codeNoMatchingCar},
		{"JSON", "", jsonType, "{}", http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
		{"no type", "", "", backup, http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, out := do(t, noFollow(), http.MethodPost, url+tc.query, tc.contentType, tc.body, c)
			if resp.status != tc.status {
				t.Errorf("%d %s", resp.status, out)
			}
			wantError(t, out, tc.code)
		})
	}

	if resp, out := do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/vehicles/"+foreign+"/imports/teslamate",
		"application/octet-stream", backup, c); resp.status != http.StatusNotFound {
		t.Errorf("another account's vehicle: %d %s", resp.status, out)
	}

	t.Run("one at a time per vehicle", func(t *testing.T) {
		e.server.teslaMate.start(car, DefaultTeslaMateImports)
		defer e.server.teslaMate.done(car)
		resp, out := do(t, noFollow(), http.MethodPost, url, "application/octet-stream", backup, c)
		if resp.status != http.StatusConflict {
			t.Errorf("%d %s", resp.status, out)
		}
		wantError(t, out, codeImportActive)
	})
	t.Run("a few per instance", func(t *testing.T) {
		for _, v := range []string{"v1", "v2"} {
			e.server.teslaMate.start(v, DefaultTeslaMateImports)
			defer e.server.teslaMate.done(v)
		}
		resp, out := do(t, noFollow(), http.MethodPost, url, "application/octet-stream", backup, c)
		if resp.status != http.StatusServiceUnavailable || resp.header.Get("Retry-After") != "60" {
			t.Errorf("%d %v %s", resp.status, resp.header, out)
		}
		wantError(t, out, codeSourceUnavailable)
	})
	t.Run("one at a time where the instance says so", func(t *testing.T) {
		e.server.TeslaMateImports = 1
		defer func() { e.server.TeslaMateImports = 0 }()
		e.server.teslaMate.start("v1", DefaultTeslaMateImports)
		defer e.server.teslaMate.done("v1")
		resp, out := do(t, noFollow(), http.MethodPost, url, "application/octet-stream", backup, c)
		if resp.status != http.StatusServiceUnavailable {
			t.Errorf("%d %s", resp.status, out)
		}
	})
	// The slots are given back, whatever the import did.
	if resp, out := do(t, noFollow(), http.MethodPost, url, "application/octet-stream", backup, c); resp.status != http.StatusOK {
		t.Errorf("after: %d %s", resp.status, out)
	}

	e.files.err = &http.MaxBytesError{Limit: maxTeslaMateBody}
	resp, out = do(t, noFollow(), http.MethodPost, url, "application/octet-stream", backup, c)
	if resp.status != http.StatusBadRequest {
		t.Errorf("too large: %d %s", resp.status, out)
	}
	wantError(t, out, codeInvalidBody)
	e.files.err = errors.New("boom")
	if resp, out := do(t, noFollow(), http.MethodPost, url, "application/octet-stream", backup, c); resp.status != http.StatusInternalServerError {
		t.Errorf("failing: %d %s", resp.status, out)
	}
}

// TestTeslaMateSlowBody: a backup takes longer to send than the server's read deadline,
// which the operation lifts.
func TestTeslaMateSlowBody(t *testing.T) {
	e, c := readerEnv(t)
	e.reader.vehicles[account] = []Vehicle{{ID: car, VIN: teslaVIN, Provider: "tesla"}}
	front := httptest.NewUnstartedServer(e.server)
	front.Config.ReadTimeout = 100 * time.Millisecond
	front.Start()
	defer front.Close()
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("first\n"))
		time.Sleep(300 * time.Millisecond) // past the read deadline
		_, _ = pw.Write([]byte("then\n"))
		_ = pw.Close()
	}()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, front.URL+"/api/v1/vehicles/"+car+"/imports/teslamate", pr)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.AddCookie(c)
	resp, err := front.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || e.files.backup != "first\nthen\n" {
		t.Errorf("%d, read %q", resp.StatusCode, e.files.backup)
	}
}
