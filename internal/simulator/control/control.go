// Package control exposes the simulator's internal state (outside the Volvo API), for
// debugging and integration tests.
package control

import (
	"encoding/json"
	"net/http"
	"time"

	"runsten/internal/platform/clock"
	"runsten/internal/simulator/scenario"
)

// Source provides the progress of each simulated vehicle.
type Source interface {
	StatusAt(at time.Time) []scenario.Status
}

type status struct {
	Now      time.Time       `json:"now"`
	Speed    float64         `json:"speed"`
	Finished bool            `json:"finished"` // every scenario is over
	Vehicles []vehicleStatus `json:"vehicles"`
}

type vehicleStatus struct {
	VIN      string `json:"vin"`
	Step     int    `json:"step"`
	Steps    int    `json:"steps"`
	Finished bool   `json:"finished"`
}

// NewHandler serves GET /sim/status.
func NewHandler(src Source, clk clock.Clock, speed float64) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sim/status", func(w http.ResponseWriter, _ *http.Request) {
		now := clk.Now()
		out := status{Now: now, Speed: speed, Finished: true, Vehicles: []vehicleStatus{}}
		for _, st := range src.StatusAt(now) {
			out.Now = st.Now
			out.Finished = out.Finished && st.Finished
			out.Vehicles = append(out.Vehicles, vehicleStatus{VIN: st.VIN, Step: st.Step, Steps: st.Steps, Finished: st.Finished})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	return mux
}
