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
// trips' file but its header.
type fakeFileImports struct {
	trips, waypoints string
	gotWaypoints     bool
	err              error
}

func (f *fakeFileImports) ImportVolvoDataPortal(_ context.Context, _, _ string, trips, waypoints io.Reader) (FileImport, error) {
	if f.err != nil {
		return FileImport{}, f.err
	}
	b, _ := io.ReadAll(trips)
	f.trips, f.gotWaypoints, f.waypoints = string(b), waypoints != nil, ""
	if waypoints != nil {
		w, _ := io.ReadAll(waypoints)
		f.waypoints = string(w)
	}
	if !strings.HasPrefix(f.trips, "Start time") {
		return FileImport{}, fmt.Errorf("trips: %w: no column \"start time\"", ErrImportFile)
	}
	return FileImport{Imported: strings.Count(f.trips, "\n") - 1, Skipped: 1, Invalid: 2, Cutoff: h(4, 0)}, nil
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

	body, ct := form(t, map[string]string{"trips": portalTrips, "waypoints": portalWaypoints})
	resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
	if resp.status != http.StatusOK {
		t.Fatalf("%d %s", resp.status, out)
	}
	golden(t, "import-volvo-data-portal.json", out)
	if e.files.trips != portalTrips || !e.files.gotWaypoints || e.files.waypoints != portalWaypoints {
		t.Errorf("files read: %+v", e.files)
	}
	if !strings.Contains(logs.String(), "files imported") {
		t.Errorf("logs:\n%s", logs)
	}

	body, ct = form(t, map[string]string{"trips": portalTrips})
	if resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c); resp.status != http.StatusOK || e.files.gotWaypoints {
		t.Errorf("without waypoints: %d %s", resp.status, out)
	}

	for _, tc := range []struct {
		name   string
		files  map[string]string
		status int
		code   errorCode
	}{
		{"no trips", map[string]string{"waypoints": portalWaypoints}, http.StatusBadRequest, codeInvalidBody},
		{"no file", map[string]string{}, http.StatusBadRequest, codeInvalidBody},
		{"the waypoints as trips", map[string]string{"trips": portalWaypoints}, http.StatusBadRequest, codeInvalidFile},
		{"another file", map[string]string{"trips": portalTrips, "charges": "x"}, http.StatusBadRequest, codeInvalidBody},
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

	t.Run("a field", func(t *testing.T) {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		part, _ := w.CreateFormFile("trips", "trips.csv")
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
		body, ct := form(t, map[string]string{"trips": portalTrips + strings.Repeat("x", maxImportFiles)})
		resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%d %.200s", resp.status, out)
		}
		wantError(t, out, codeInvalidBody)
	})

	if resp, out := do(t, noFollow(), http.MethodPost, url, jsonType, `{}`, c); resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("JSON: %d %s", resp.status, out)
	}
	body, ct = form(t, map[string]string{"trips": portalTrips})
	if resp, out := do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/vehicles/"+foreign+"/imports/volvo_data_portal",
		ct, body, c); resp.status != http.StatusNotFound {
		t.Errorf("another account's vehicle: %d %s", resp.status, out)
	}

	e.files.err = errors.New("boom")
	if resp, out := do(t, noFollow(), http.MethodPost, url, ct, body, c); resp.status != http.StatusInternalServerError {
		t.Errorf("failing: %d %s", resp.status, out)
	}
}
