package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// VehicleHealth reads a vehicle's health: its tyres, its service and its warnings.
type VehicleHealth interface {
	// Health returns the latest known health, as of at.
	Health(ctx context.Context, accountID, vehicleID string, at time.Time) (core.CurrentHealth, error)
	// HealthReadings returns the records of the health read within [from, to], oldest
	// first, at most limit of them.
	HealthReadings(ctx context.Context, accountID, vehicleID string, from, to time.Time, limit int) ([]core.HealthRecord, error)
}

const (
	// maxTyresWindow bounds the window of the tyres' pressures: a year, read every hour.
	maxTyresWindow = 366 * 24 * time.Hour
	// maxHealthResponses bounds the responses of a window: a year of every endpoint of
	// the health, each read every hour and changed each time, would be more; identical
	// responses are one.
	maxHealthResponses = 20_000
)

// Enumerations of the health, from the domain's values.
type (
	wheelJSON        string
	tyreWarningJSON  string
	serviceStateJSON string
	checkNameJSON    string
)

// Schema lists the wheels.
func (wheelJSON) Schema(huma.Registry) *huma.Schema { return enumSchema(core.Wheels()...) }

// Schema lists the warnings of a tyre.
func (tyreWarningJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.TyreOK, core.TyreLow, core.TyreVeryLow, core.TyreHigh, core.TyreFault)
}

// Schema lists the states of the service.
func (serviceStateJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.ServiceOK, core.ServiceSoon, core.ServiceDue, core.ServiceOverdue)
}

// Schema lists the checks.
func (checkNameJSON) Schema(huma.Registry) *huma.Schema { return enumSchema(core.Checks()...) }

type tyreJSON struct {
	Wheel       wheelJSON             `json:"wheel"`
	PressureKPa *float64              `json:"pressure_kpa" doc:"The tyre's pressure, in kPa (100 kPa = 1 bar); null when the vehicle does not report it: a Volvo reports a warning only."`
	Warning     null[tyreWarningJSON] `json:"warning" doc:"What the tyre pressure monitoring tells: ok, low, very_low, high, or fault (a sensor's or the system's, or a warning not known here); null when not reported."`
	ReportedAt  *time.Time            `json:"reported_at" pattern:"Z$" doc:"When the vehicle measured it; null when unknown."`
}

type tyresJSON struct {
	Wheels              []tyreJSON `json:"wheels" nullable:"false" doc:"The wheels the vehicle reports, front left, front right, rear left, rear right."`
	RecommendedFrontKPa *float64   `json:"recommended_front_kpa" doc:"The pressure the vehicle recommends at the front, in kPa; null when not reported."`
	RecommendedRearKPa  *float64   `json:"recommended_rear_kpa" doc:"The pressure the vehicle recommends at the rear, in kPa; null when not reported."`
}

type serviceJSON struct {
	State      null[serviceStateJSON] `json:"state" doc:"ok, soon (almost time for it), due, or overdue, as the vehicle warns of it; null when unknown."`
	Days       *float64               `json:"days" doc:"Days left until the service; null when the vehicle counts months, or does not tell."`
	Months     *float64               `json:"months" doc:"Months left until the service; null when the vehicle counts days, or does not tell."`
	DistanceKm *float64               `json:"distance_km" doc:"Distance left until the service; null when not reported."`
	ReportedAt *time.Time             `json:"reported_at" pattern:"Z$" doc:"When the vehicle told it; null when unknown."`
}

type checkJSON struct {
	Check      checkNameJSON `json:"check" doc:"The part watched: a fluid (brake_fluid, coolant, oil, washer_fluid), the low voltage battery, or an exterior light (light_*)."`
	Warning    bool          `json:"warning" doc:"true where the vehicle warns: a level too low or too high, a failure."`
	ReportedAt *time.Time    `json:"reported_at" pattern:"Z$" doc:"When the vehicle told it; null when unknown."`
}

type healthJSON struct {
	VehicleID string            `json:"vehicle_id" format:"uuid"`
	CheckedAt *time.Time        `json:"checked_at" pattern:"Z$" doc:"The latest reading of the health. null before the first one."`
	Tyres     null[tyresJSON]   `json:"tyres" doc:"null when the vehicle never reported its tyres."`
	Service   null[serviceJSON] `json:"service" doc:"null when the vehicle never reported its service."`
	Checks    []checkJSON       `json:"checks" nullable:"false" doc:"The checks the vehicle reports, with a known state: the fluids and the battery first, then the lights. Which the vehicle reports depends on its model and its source."`
}

func healthOf(vehicleID string, h core.CurrentHealth) healthJSON {
	out := healthJSON{VehicleID: vehicleID, CheckedAt: timeOrNil(h.CheckedAt), Checks: []checkJSON{}}
	if h.Covers&core.HealthTyres != 0 {
		t := tyresJSON{
			Wheels:              []tyreJSON{},
			RecommendedFrontKPa: opt(h.RecommendedFrontKPa, same), RecommendedRearKPa: opt(h.RecommendedRearKPa, same),
		}
		for _, w := range core.Wheels() {
			tyre, ok := h.Tyres[w]
			if !ok {
				continue
			}
			at := tyre.PressureKPa.At
			if !tyre.PressureKPa.OK {
				at = tyre.Warning.At
			}
			t.Wheels = append(t.Wheels, tyreJSON{
				Wheel: wheelJSON(w), PressureKPa: opt(tyre.PressureKPa, same),
				Warning: nullOf(tyre.Warning, text[core.TyreWarning, tyreWarningJSON]), ReportedAt: timeOrNil(at),
			})
		}
		out.Tyres = known(t)
	}
	if h.Covers&core.HealthService != 0 {
		at := h.Service.At
		for _, v := range []core.Value[float64]{h.DistanceToServiceKm, h.DaysToService, h.MonthsToService} {
			if at.IsZero() {
				at = v.At
			}
		}
		out.Service = known(serviceJSON{
			State: nullOf(h.Service, text[core.ServiceState, serviceStateJSON]),
			Days:  opt(h.DaysToService, same), Months: opt(h.MonthsToService, same),
			DistanceKm: opt(h.DistanceToServiceKm, same), ReportedAt: timeOrNil(at),
		})
	}
	for _, c := range core.Checks() {
		if v, ok := h.Checks[c]; ok && v.OK {
			out.Checks = append(out.Checks, checkJSON{Check: checkNameJSON(c), Warning: v.V, ReportedAt: timeOrNil(v.At)})
		}
	}
	return out
}

// getHealth reads the vehicle's health now.
func (s *Server) getHealth(ctx context.Context, in *vehicleInput) (*body[healthJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	h, err := s.Health.Health(ctx, sessionFrom(ctx).AccountID, v.ID, s.Clock.Now())
	if err != nil {
		return nil, s.internal("health not read", err)
	}
	return respond(healthOf(v.ID, h)), nil
}

type tyresInput struct {
	Vehicle string    `path:"vehicle" doc:"The vehicle ID."`
	From    time.Time `query:"from" required:"true" doc:"The start of the window [from, to], included." example:"2026-09-01T00:00:00Z"`
	To      time.Time `query:"to" doc:"The end of the window, included; it must come after from, at most 366 days later. Without it, now." example:"2026-10-01T00:00:00Z"`
}

// tyreReadingJSON is the tyres' pressures as one response gave them, at one instant.
type tyreReadingJSON struct {
	At                  time.Time `json:"at" pattern:"Z$" doc:"When it was read: the response's fetched_at, or the latest time it was read again unchanged; a response read before from or again after to gives its value at that edge."`
	FrontLeftKPa        *float64  `json:"front_left_kpa" doc:"In kPa; null when the response lacked it."`
	FrontRightKPa       *float64  `json:"front_right_kpa"`
	RearLeftKPa         *float64  `json:"rear_left_kpa"`
	RearRightKPa        *float64  `json:"rear_right_kpa"`
	RecommendedFrontKPa *float64  `json:"recommended_front_kpa" doc:"The pressure the vehicle recommended at the front then; null when the response lacked it."`
	RecommendedRearKPa  *float64  `json:"recommended_rear_kpa"`
}

type tyreRunJSON struct {
	Readings []tyreReadingJSON `json:"readings" nullable:"false" minItems:"1" doc:"Oldest first, at most 90 minutes between two responses: a line joins them."`
}

type tyreSeriesJSON struct {
	From time.Time     `json:"from" pattern:"Z$" doc:"The start of the window: the one asked for, or the start of the history the account sees when later."`
	To   time.Time     `json:"to" pattern:"Z$"`
	Runs []tyreRunJSON `json:"runs" nullable:"false" doc:"The readings giving a pressure, oldest first, in runs: a new run starts where more than 90 minutes separate two responses. [] when none was read in the window, or the vehicle reports no pressure (a Volvo)."`
}

// getTyres reads the tyres' pressures of a window: the readings themselves, never a
// value between two of them.
func (s *Server) getTyres(ctx context.Context, in *tyresInput) (*body[tyreSeriesJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	from, to := in.From.UTC(), in.To.UTC()
	if in.To.IsZero() {
		to = s.Clock.Now().UTC()
	}
	switch {
	case !from.Before(to):
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "from must be before to")
	case to.Sub(from) > maxTyresWindow:
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "to: at most 366 days after from")
	}
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	from = later(from, l.HistoryFrom)
	out := tyreSeriesJSON{From: from, To: to, Runs: []tyreRunJSON{}}
	if !from.Before(to) {
		out.From = to
		return respond(out), nil
	}
	records, err := s.Health.HealthReadings(ctx, sessionFrom(ctx).AccountID, v.ID, from, to, maxHealthResponses+1)
	if err != nil {
		return nil, s.internal("health not read", err)
	}
	if len(records) > maxHealthResponses {
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter,
			"from: more than "+strconv.Itoa(maxHealthResponses)+" responses in the window, ask a shorter one")
	}
	for _, run := range core.TyreRuns(records, from, to, seriesGap) {
		readings := make([]tyreReadingJSON, len(run))
		for i, r := range run {
			readings[i] = tyreReadingJSON{
				At:                  r.At,
				FrontLeftKPa:        opt(r.PressureKPa[core.FrontLeft], same),
				FrontRightKPa:       opt(r.PressureKPa[core.FrontRight], same),
				RearLeftKPa:         opt(r.PressureKPa[core.RearLeft], same),
				RearRightKPa:        opt(r.PressureKPa[core.RearRight], same),
				RecommendedFrontKPa: opt(r.RecommendedFrontKPa, same),
				RecommendedRearKPa:  opt(r.RecommendedRearKPa, same),
			}
		}
		out.Runs = append(out.Runs, tyreRunJSON{Readings: readings})
	}
	return respond(out), nil
}

// registerHealth registers the health of a vehicle.
func (s *Server) registerHealth(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getVehicleHealth", Method: http.MethodGet, Path: "/vehicles/{vehicle}/health", Tags: []string{"vehicles"},
		Summary: "The health of a vehicle",
		Description: "The latest the vehicle told of its tyres (their pressures and warnings, and the recommended " +
			"pressures), its next service, and its warnings (fluids, low voltage battery, exterior lights). What is " +
			"told depends on the vehicle's source: a Volvo reports a warning per tyre and no pressure; a Polestar's " +
			"credential reads the health only when it was granted its domain. Read every hour.",
	}, "The health.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getHealth)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getVehicleTyres", Method: http.MethodGet, Path: "/vehicles/{vehicle}/health/tyres", Tags: []string{"vehicles"},
		Summary: "The tyres' pressures of a vehicle over a window",
		Description: "The pressures as the vehicle gave them within `[from, to]`, both included, with the recommended " +
			"ones. As the energy states' series: a stored response is two readings, nothing is interpolated, and the " +
			"readings come in runs, a new one where more than 90 minutes separate two responses. At most 366 days, " +
			"and 20,000 responses; the history the account does not see is left out, as from the lists.",
	}, "The pressures.", map[int]string{
		http.StatusBadRequest: "`invalid_parameter`: `from` or `to`, a window over 366 days, or more than 20,000 " +
			"responses in it; the message names the parameter.",
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getTyres)
}
