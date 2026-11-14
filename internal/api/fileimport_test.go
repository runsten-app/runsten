package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// fakeFileImports reads the files it is given, and reports one event per line of the
// trips' files but their header.
type fakeFileImports struct {
	portal []string // the portal's files read, "name: contents"
	err    error
	// TeslaMate's: the backup read, the VIN and the currency given.
	backup, vin, currency string
	// AAOS Logging's: the export read, and its size.
	export     string
	exportSize int64
}

// ImportAAOSLogging takes an export that starts with PK as a ZIP, and reports one event
// per line.
func (f *fakeFileImports) ImportAAOSLogging(_ context.Context, _, _ string, r io.ReaderAt, size int64) (FileImport, error) {
	if f.err != nil {
		return FileImport{}, f.err
	}
	b, _ := io.ReadAll(io.NewSectionReader(r, 0, size))
	f.export, f.exportSize = string(b), size
	if !strings.HasPrefix(f.export, "PK") {
		return FileImport{}, fmt.Errorf("export: %w: not a ZIP file", ErrImportFile)
	}
	return FileImport{Imported: strings.Count(f.export, "\n"), Skipped: 1, Invalid: 1, Cutoff: h(4, 0)}, nil
}

// ImportTeslaMate takes a backup that starts with PGDMP as an archive, one that starts
// with "other" as another car's, and reports one event per line.
func (f *fakeFileImports) ImportTeslaMate(_ context.Context, _, _, vin, currency string, r io.Reader) (FileImport, error) {
	if f.err != nil {
		return FileImport{}, f.err
	}
	b, _ := io.ReadAll(r)
	f.backup, f.vin, f.currency = string(b), vin, currency
	switch {
	case strings.HasPrefix(f.backup, "PGDMP"):
		return FileImport{}, fmt.Errorf("reading the backup: %w: a custom-format archive", ErrImportFile)
	case strings.HasPrefix(f.backup, "other"):
		return FileImport{}, fmt.Errorf("%w: it has SYNTHETIC0000VIN2", ErrNoMatchingCar)
	}
	return FileImport{Imported: strings.Count(f.backup, "\n"), Skipped: 0, Invalid: 1, Cutoff: h(4, 0)}, nil
}

func (f *fakeFileImports) ImportVolvoDataPortal(_ context.Context, _, _ string, files []UploadedFile) (FileImport, error) {
	if f.err != nil {
		return FileImport{}, f.err
	}
	f.portal = nil
	trips := 0
	for _, file := range files {
		b, _ := io.ReadAll(io.NewSectionReader(file.R, 0, file.Size))
		f.portal = append(f.portal, file.Name+": "+string(b))
		if strings.HasPrefix(string(b), "Start time") {
			trips += strings.Count(string(b), "\n") - 1
		}
	}
	if trips == 0 {
		return FileImport{}, fmt.Errorf("%w: no trips' file among them", ErrImportFile)
	}
	return FileImport{Imported: trips, Skipped: 1, Invalid: 2, Cutoff: h(4, 0)}, nil
}

// portalForm is a multipart body with the files given, name then contents, all in the
// field files; its content type.
func portalForm(t *testing.T, files ...string) (string, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for ; len(files) >= 2; files = files[2:] {
		part, err := w.CreateFormFile("files", files[0])
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(files[1]))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String(), w.FormDataContentType()
}

// form is a multipart body with the files given, by field name; its content type.
func form(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for name, content := range files {
		part, err := w.CreateFormFile(name, name+".csv")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String(), w.FormDataContentType()
}

const (
	portalTrips     = "Start time,End time\n2025-10-13T07:17:26.495Z,2025-10-13T07:53:02.677Z\n2025-10-14T07:17:26Z,2025-10-14T07:53:02Z\n"
	portalWaypoints = "Timestamp,Latitude,Longitude\n2025-10-13T07:18:27.813Z,35.3,23.4\n"
)

func TestImportVolvoDataPortal(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	path := "/vehicles/" + car + "/imports/volvo_data_portal"
	url := e.api.URL + "/api/v1" + path

	body, ct := portalForm(t, "waypoint.csv", portalWaypoints, "trip.csv", portalTrips)
	resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
	if resp.status != http.StatusOK {
		t.Fatalf("%d %s", resp.status, out)
	}
	golden(t, "import-volvo-data-portal.json", out)
	if len(e.files.portal) != 2 || e.files.portal[0] != "waypoint.csv: "+portalWaypoints || e.files.portal[1] != "trip.csv: "+portalTrips {
		t.Errorf("files read: %q", e.files.portal)
	}
	if !strings.Contains(logs.String(), "files imported") {
		t.Errorf("logs:\n%s", logs)
	}

	body, ct = portalForm(t, "trip.csv", portalTrips)
	if resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c); resp.status != http.StatusOK || len(e.files.portal) != 1 {
		t.Errorf("one file: %d %s", resp.status, out)
	}

	many := make([]string, 0, 2*(maxPortalFiles+1))
	for i := range maxPortalFiles + 1 {
		many = append(many, fmt.Sprintf("%d.csv", i), portalTrips)
	}
	for _, tc := range []struct {
		name   string
		body   func() (string, string)
		status int
		code   errorCode
	}{
		{"no trips", func() (string, string) { return portalForm(t, "w.csv", portalWaypoints) }, http.StatusBadRequest, codeInvalidFile},
		{"no file", func() (string, string) { return portalForm(t) }, http.StatusBadRequest, codeInvalidBody},
		{"too many files", func() (string, string) { return portalForm(t, many...) }, http.StatusBadRequest, codeInvalidBody},
		{"another field", func() (string, string) {
			return form(t, map[string]string{"files": portalTrips, "trips": portalTrips})
		}, http.StatusBadRequest, codeInvalidBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, ct := tc.body()
			resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
			if resp.status != tc.status {
				t.Errorf("%d %.300s", resp.status, out)
			}
			wantError(t, out, tc.code)
		})
	}

	t.Run("a field", func(t *testing.T) {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		part, _ := w.CreateFormFile("files", "trips.csv")
		_, _ = part.Write([]byte(portalTrips))
		_ = w.WriteField("timezone", "Europe/Paris")
		_ = w.Close()
		resp, out := do(t, noFollow(), http.MethodPost, url, w.FormDataContentType(), b.String(), c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%d %s", resp.status, out)
		}
		wantError(t, out, codeInvalidBody)
	})

	t.Run("too large", func(t *testing.T) {
		body, ct := portalForm(t, "trips.csv", portalTrips+strings.Repeat("x", maxImportFiles))
		resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%d %.200s", resp.status, out)
		}
		wantError(t, out, codeInvalidBody)
	})

	if resp, out := do(t, noFollow(), http.MethodPost, url, jsonType, `{}`, c); resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("JSON: %d %s", resp.status, out)
	}
	body, ct = portalForm(t, "trips.csv", portalTrips)
	if resp, out := do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/vehicles/"+foreign+"/imports/volvo_data_portal",
		ct, body, c); resp.status != http.StatusNotFound {
		t.Errorf("another account's vehicle: %d %s", resp.status, out)
	}

	// A Polestar has no history at the Volvo Data Portal: nothing is read of the files.
	e.files.portal = nil
	resp, out = do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/vehicles/"+shared+"/imports/volvo_data_portal", ct, body, c)
	if resp.status != http.StatusConflict || e.files.portal != nil {
		t.Errorf("a Polestar: %d %s, files read %q", resp.status, out, e.files.portal)
	}
	wantError(t, out, codeNotVolvo)

	e.files.err = errors.New("boom")
	if resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c); resp.status != http.StatusInternalServerError {
		t.Errorf("failing: %d %s", resp.status, out)
	}
}

func TestImportAAOSLogging(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	url := e.api.URL + "/api/v1/vehicles/" + car + "/imports/aaos_logging"
	export := "PK\ntrip\ntrip\n"

	body, ct := form(t, map[string]string{"export": export})
	resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
	if resp.status != http.StatusOK {
		t.Fatalf("%d %s", resp.status, out)
	}
	golden(t, "import-aaos-logging.json", out)
	if e.files.export != export || e.files.exportSize != int64(len(export)) {
		t.Errorf("export read: %q, %d bytes", e.files.export, e.files.exportSize)
	}
	if !strings.Contains(logs.String(), `"source":"aaos_logging"`) {
		t.Errorf("logs:\n%s", logs)
	}

	for _, tc := range []struct {
		name   string
		files  map[string]string
		status int
		code   errorCode
	}{
		{"no file", map[string]string{}, http.StatusBadRequest, codeInvalidBody},
		{"not a ZIP", map[string]string{"export": "trips"}, http.StatusBadRequest, codeInvalidFile},
		{"another file", map[string]string{"export": export, "trips": "x"}, http.StatusBadRequest, codeInvalidBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, ct := form(t, tc.files)
			resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
			if resp.status != tc.status {
				t.Errorf("%d %s", resp.status, out)
			}
			wantError(t, out, tc.code)
		})
	}
	if resp, out := do(t, noFollow(), http.MethodPost, url, jsonType, `{}`, c); resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("JSON: %d %s", resp.status, out)
	}
	if resp, out := do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/vehicles/"+foreign+"/imports/aaos_logging",
		ct, body, c); resp.status != http.StatusNotFound {
		t.Errorf("another account's vehicle: %d %s", resp.status, out)
	}
	// Of any brand: a Polestar's too.
	if resp, out := do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/vehicles/"+shared+"/imports/aaos_logging",
		ct, body, c); resp.status != http.StatusOK {
		t.Errorf("a Polestar: %d %s", resp.status, out)
	}

	t.Run("one at a time per vehicle", func(t *testing.T) {
		e.server.aaosLogging.start(car, aaosLoggingImports)
		defer e.server.aaosLogging.done(car)
		body, ct := form(t, map[string]string{"export": export})
		resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
		if resp.status != http.StatusConflict {
			t.Errorf("%d %s", resp.status, out)
		}
		wantError(t, out, codeImportActive)
	})
	t.Run("a few per instance", func(t *testing.T) {
		for _, v := range []string{"v1", "v2"} {
			e.server.aaosLogging.start(v, aaosLoggingImports)
			defer e.server.aaosLogging.done(v)
		}
		body, ct := form(t, map[string]string{"export": export})
		resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
		if resp.status != http.StatusServiceUnavailable || resp.header.Get("Retry-After") != "60" {
			t.Errorf("%d %v %s", resp.status, resp.header, out)
		}
		wantError(t, out, codeSourceUnavailable)
	})
	// The slots are given back, whatever the import did.
	body, ct = form(t, map[string]string{"export": export})
	if resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c); resp.status != http.StatusOK {
		t.Errorf("after: %d %s", resp.status, out)
	}
	body, ct = form(t, map[string]string{"export": export})
	e.files.err = errors.New("boom")
	if resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c); resp.status != http.StatusInternalServerError {
		t.Errorf("failing: %d %s", resp.status, out)
	}
}
