package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const aximoteToken = "axm_pat_AAAAAAAAAAAAAAAAAAAAAAAAAA"

// memImportJobs keeps the jobs in memory, as the store does: one active per vehicle.
type memImportJobs struct {
	mu   sync.Mutex
	jobs map[string][]ImportJob // by vehicle, oldest first
	reqs []ImportRequest
	err  error
}

func (m *memImportJobs) CreateImportJob(_ context.Context, _, vehicle string, req ImportRequest, at time.Time) (ImportJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return ImportJob{}, m.err
	}
	if m.jobs == nil {
		m.jobs = map[string][]ImportJob{}
	}
	for _, j := range m.jobs[vehicle] {
		if j.Status == ImportPending || j.Status == ImportRunning {
			return ImportJob{}, ErrImportActive
		}
	}
	j := ImportJob{
		ID: fmt.Sprintf("6f0c1d2e-0000-4000-8000-%012d", len(m.reqs)+1), Source: req.Source,
		SourceVehicle: req.SourceVehicle, Status: ImportPending, Step: "trips", CreatedAt: at, NotBefore: at,
	}
	m.reqs = append(m.reqs, req)
	m.jobs[vehicle] = append(m.jobs[vehicle], j)
	return j, nil
}

func (m *memImportJobs) LatestImportJob(_ context.Context, _, vehicle string) (ImportJob, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := m.jobs[vehicle]
	if m.err != nil || len(jobs) == 0 {
		return ImportJob{}, false, m.err
	}
	return jobs[len(jobs)-1], true, nil
}

// update changes the vehicle's latest job, as the worker does.
func (m *memImportJobs) update(vehicle string, f func(*ImportJob)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(&m.jobs[vehicle][len(m.jobs[vehicle])-1])
}

type tokenRefused struct{}

func (tokenRefused) Error() string      { return "aximote vehicles: HTTP 401" }
func (tokenRefused) TokenRefused() bool { return true }

// fakeAximote lists the vehicles of aximoteToken's account.
type fakeAximote struct {
	err error
}

func (f *fakeAximote) AximoteVehicles(_ context.Context, token string) ([]AximoteVehicle, error) {
	if f.err != nil {
		return nil, f.err
	}
	if token != aximoteToken {
		return nil, tokenRefused{}
	}
	return []AximoteVehicle{
		{ID: "veh-v60", Name: "Old V60", Make: "Volvo", Model: "V60", Year: 2016},
		{ID: "veh-ex30", Name: "My EX30", Make: "Volvo", Model: "EX30", Year: 2024, VIN: "yv1smlt0000dt0001"},
		{ID: "veh-bare"},
	}, nil
}

const startBody = `{"source":"aximote","token":"` + aximoteToken + `","vehicle":"veh-ex30"}`

func TestImportFromAximote(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	base := e.api.URL + "/api/v1/vehicles/" + car

	resp, body := get(t, noFollow(), base+"/import-job", c)
	if resp.status != http.StatusOK || strings.TrimSpace(body) != `{"job":null}` {
		t.Fatalf("before any import: %d %s", resp.status, body)
	}

	// The vehicles, the one with this vehicle's VIN told, whatever its case.
	resp, body = do(t, noFollow(), http.MethodPost, base+"/aximote-vehicles", jsonType, `{"token":"`+aximoteToken+`"}`, c)
	if resp.status != http.StatusOK {
		t.Fatalf("vehicles: %d %s", resp.status, body)
	}
	golden(t, "aximote-vehicles.json", body)
	if _, body := do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/vehicles/"+chosen+"/aximote-vehicles", jsonType,
		`{"token":"`+aximoteToken+`"}`, c); strings.Contains(body, `"vin_matches":true`) {
		t.Errorf("another vehicle matches: %s", body)
	}

	resp, body = do(t, noFollow(), http.MethodPost, base+"/import-job", jsonType, startBody, c)
	if resp.status != http.StatusAccepted {
		t.Fatalf("start: %d %s", resp.status, body)
	}
	_, read := get(t, noFollow(), base+"/import-job", c)
	if read != `{"job":`+strings.TrimSpace(body)+"}\n" {
		t.Errorf("read back: %s, created: %s", read, body)
	}
	golden(t, "import-job-pending.json", read)
	if r := e.jobs.reqs; len(r) != 1 || r[0] != (ImportRequest{Source: "aximote", SourceVehicle: "veh-ex30", Token: aximoteToken}) {
		t.Errorf("requests %+v", r)
	}
	resp, body = do(t, noFollow(), http.MethodPost, base+"/import-job", jsonType, startBody, c)
	if resp.status != http.StatusConflict {
		t.Errorf("a second one: %d %s", resp.status, body)
	}
	wantError(t, body, codeImportActive)

	e.jobs.update(car, func(j *ImportJob) {
		j.Status, j.Step, j.Imported, j.Skipped, j.Invalid = ImportDone, "charges", 3, 1, 2
		j.Cutoff, j.EndedAt = h(4, 0), h(5, 31)
	})
	_, body = get(t, noFollow(), base+"/import-job", c)
	golden(t, "import-job-done.json", body)
	e.jobs.update(car, func(j *ImportJob) { j.Status, j.Error = ImportFailed, "token_refused" })
	if _, body := get(t, noFollow(), base+"/import-job", c); !strings.Contains(body, `"error":"token_refused"`) {
		t.Errorf("failed: %s", body)
	}
	if strings.Contains(logs.String(), aximoteToken) || !strings.Contains(logs.String(), "import started") {
		t.Errorf("logs:\n%s", logs)
	}

	for _, tc := range []struct {
		name, path, body string
		status           int
		code             errorCode
	}{
		{"a token refused", "/aximote-vehicles", `{"token":"axm_pat_BBBBBBBBBBBBBBBBBBBBBBBBBB"}`, http.StatusBadRequest, codeTokenRefused},
		{"not a token", "/aximote-vehicles", `{"token":"rst_abc"}`, http.StatusBadRequest, codeInvalidBody},
		{"the token in the URL", "/aximote-vehicles?token=" + aximoteToken, `{}`, http.StatusBadRequest, codeInvalidBody},
		{"not a vehicle ID", "/import-job", `{"source":"aximote","token":"` + aximoteToken + `","vehicle":"a/b"}`, http.StatusBadRequest, codeInvalidBody},
		{"no vehicle", "/import-job", `{"source":"aximote","token":"` + aximoteToken + `"}`, http.StatusBadRequest, codeInvalidBody},
		{"another source", "/import-job", `{"source":"volvo","token":"` + aximoteToken + `","vehicle":"v"}`, http.StatusBadRequest, codeInvalidBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := do(t, noFollow(), http.MethodPost, base+tc.path, jsonType, tc.body, c)
			if resp.status != tc.status {
				t.Errorf("%d %s", resp.status, body)
			}
			wantError(t, body, tc.code)
		})
	}

	for _, r := range [][3]string{
		{http.MethodGet, "/import-job", ""},
		{http.MethodPost, "/import-job", startBody},
		{http.MethodPost, "/aximote-vehicles", `{"token":"` + aximoteToken + `"}`},
	} {
		resp, body := do(t, noFollow(), r[0], e.api.URL+"/api/v1/vehicles/"+foreign+r[1], jsonType, r[2], c)
		if resp.status != http.StatusNotFound {
			t.Errorf("another account's vehicle, %s: %d %s", r[1], resp.status, body)
		}
	}

	for _, path := range []string{"/aximote-vehicles", "/import-job"} {
		resp, body := do(t, noFollow(), http.MethodPost, base+path, "text/plain", startBody, c)
		if resp.status != http.StatusUnsupportedMediaType {
			t.Errorf("%s as text: %d %s", path, resp.status, body)
		}
	}

	e.aximote.err = errors.New("aximote vehicles: HTTP 503")
	resp, body = do(t, noFollow(), http.MethodPost, base+"/aximote-vehicles", jsonType, `{"token":"`+aximoteToken+`"}`, c)
	if resp.status != http.StatusBadGateway {
		t.Errorf("Aximote down: %d %s", resp.status, body)
	}
	wantError(t, body, codeSourceUnavailable)

	e.jobs.err = errors.New("boom")
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		resp, body := do(t, noFollow(), method, base+"/import-job", jsonType, startBody, c)
		if resp.status != http.StatusInternalServerError {
			t.Errorf("%s failing: %d %s", method, resp.status, body)
		}
	}
}
