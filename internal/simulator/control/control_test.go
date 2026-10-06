package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
)

type fakeSource []scenario.Status

func (f fakeSource) StatusAt(at time.Time) []scenario.Status {
	out := make([]scenario.Status, len(f))
	for i, st := range f {
		st.Now = at
		out[i] = st
	}
	return out
}

func TestStatus(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		src      fakeSource
		finished bool
	}{
		{"one vehicle", fakeSource{{VIN: "A", Step: 2, Steps: 5}}, false},
		{"all finished", fakeSource{{VIN: "A", Step: 5, Steps: 5, Finished: true}, {VIN: "B", Step: 3, Steps: 3, Finished: true}}, true},
		{"one still running", fakeSource{{VIN: "A", Step: 5, Steps: 5, Finished: true}, {VIN: "B", Step: 1, Steps: 3}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(tt.src, clock.NewManual(t0), 60)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sim/status", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}
			var got status
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Now.Equal(t0) || got.Speed != 60 || got.Finished != tt.finished || len(got.Vehicles) != len(tt.src) {
				t.Fatalf("status = %+v", got)
			}
			for i, v := range got.Vehicles {
				if w := tt.src[i]; v.VIN != w.VIN || v.Step != w.Step || v.Steps != w.Steps || v.Finished != w.Finished {
					t.Errorf("vehicle %d = %+v, want %+v", i, v, w)
				}
			}
		})
	}
}
