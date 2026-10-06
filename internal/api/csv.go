package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// The trips and charges of a period as CSV files, for a spreadsheet: the items of the
// lists, flattened, one row per event. RFC 4180, UTF-8, a header row named after the
// JSON fields, oldest first; times in RFC 3339 UTC, numbers with a decimal point, an
// unknown value an empty cell. The whole period is read at once, from one snapshot.

// csvInput is the period of a CSV file.
type csvInput struct {
	Vehicle string    `path:"vehicle" doc:"The vehicle ID."`
	From    time.Time `query:"from" doc:"Keeps the events that may have happened, at least partly, at or after this time, as the lists do." example:"2026-09-01T00:00:00Z"`
	To      time.Time `query:"to" doc:"Keeps the events that may have happened, at least partly, before this time. With from, it must come after from." example:"2026-10-01T00:00:00Z"`
}

type csvOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

// csvEvents reads every event of the period, as the lists filter them, the account's
// limits applied, newest first.
func csvEvents[E any](ctx context.Context, s *Server, in *csvInput,
	read func(accountID string, v Vehicle, q EventQuery) ([]E, error),
) ([]E, AccountLimits, error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, AccountLimits{}, err
	}
	q, err := eventQuery(&listInput{From: in.From, To: in.To})
	if err != nil {
		return nil, AccountLimits{}, err
	}
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, AccountLimits{}, err
	}
	if l.HistoryFrom.After(q.From) {
		q.From = l.HistoryFrom
		if !q.To.IsZero() && !q.From.Before(q.To) {
			return nil, l, nil
		}
	}
	events, err := read(sessionFrom(ctx).AccountID, v, q)
	if err != nil {
		return nil, l, s.internal("events not read", err)
	}
	return events, l, nil
}

// csvFile writes the rows, oldest first, after the header, as an attachment named
// after the kind and the day.
func (s *Server) csvFile(kind string, header []string, rows [][]string) (*csvOutput, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	_ = w.Write(header) // into a buffer: never fails
	for _, r := range slices.Backward(rows) {
		_ = w.Write(r)
	}
	w.Flush()
	return &csvOutput{
		ContentType: "text/csv; charset=utf-8",
		ContentDisposition: `attachment; filename="runsten-` + kind + `-` +
			s.Clock.Now().UTC().Format(time.DateOnly) + `.csv"`,
		Body: b.Bytes(),
	}, nil
}

var tripsHeader = []string{
	"id", "reconstructed", "start_after", "start_before", "end_after", "end_before",
	"distance_km", "start_odometer_km", "end_odometer_km", "start_soc_pct", "end_soc_pct",
	"start_range_km", "end_range_km", "energy_kwh", "capacity_kwh", "capacity_source",
	"start_lat", "start_lon", "start_place", "start_address", "end_lat", "end_lon", "end_place", "end_address",
	"vehicle_trip_meter_km", "vehicle_consumption_kwh_per_100km",
}

func (s *Server) tripsCSV(ctx context.Context, in *csvInput) (*csvOutput, error) {
	var places []core.Place
	trips, _, err := csvEvents(ctx, s, in, func(acc string, v Vehicle, q EventQuery) ([]core.Trip, error) {
		var err error
		if places, err = s.Settings.Places(ctx, acc); err != nil {
			return nil, err //nolint:wrapcheck // logged by csvEvents, never shown
		}
		return s.Reader.ListTrips(ctx, acc, v.ID, q)
	})
	if err != nil {
		return nil, err
	}
	book := s.addresses(ctx, places, tripPositions(trips...)...)
	rows := make([][]string, len(trips))
	for i, t := range trips {
		j := tripOf(t, places, book)
		rows[i] = slices.Concat(eventCells(j.EventFields), []string{
			num(j.DistanceKm), num(j.StartOdometerKm), num(j.EndOdometerKm), num(j.StartSoCPct), num(j.EndSoCPct),
			num(j.StartRangeKm), num(j.EndRangeKm), num(j.EnergyKWh),
		}, capacityCells(j.CapacityFields),
			positionCells(j.StartPosition, j.StartPlace, j.StartAddress),
			positionCells(j.EndPosition, j.EndPlace, j.EndAddress),
			[]string{num(j.VehicleReported.TripMeterKm), num(j.VehicleReported.ConsumptionKWhPer100km)})
	}
	return s.csvFile("trips", tripsHeader, rows)
}

var chargesHeader = []string{
	"id", "reconstructed", "start_after", "start_before", "end_after", "end_before",
	"type", "start_soc_pct", "end_soc_pct", "target_soc_pct", "energy_soc_kwh", "energy_power_kwh",
	"capacity_kwh", "capacity_source", "lat", "lon", "place", "address",
	"cost_currency", "cost_min", "cost_max", "cost_source", "cost_energy_kwh", "cost_efficiency", "cost_note",
}

func (s *Server) chargesCSV(ctx context.Context, in *csvInput) (*csvOutput, error) {
	var priced PricedCharges
	var charger core.Value[float64]
	charges, l, err := csvEvents(ctx, s, in, func(acc string, v Vehicle, q EventQuery) ([]core.Charge, error) {
		var err error
		if charger, err = s.chargerOf(ctx, v); err != nil {
			return nil, err
		}
		priced, err = s.Reader.ListCharges(ctx, acc, v.ID, q)
		return priced.Charges, err //nolint:wrapcheck // logged by csvEvents, never shown
	})
	if err != nil {
		return nil, err
	}
	rows := make([][]string, len(charges))
	for i, j := range s.chargesOf(ctx, priced, charger, l) {
		typ := ""
		if j.Type.ok {
			typ = string(j.Type.v)
		}
		rows[i] = slices.Concat(eventCells(j.EventFields), []string{
			typ, num(j.StartSoCPct), num(j.EndSoCPct), num(j.TargetSoCPct), num(j.EnergySoCKWh), num(j.EnergyPowerKWh),
		}, capacityCells(j.CapacityFields), positionCells(j.Position, j.Place, j.Address),
			costCells(j.Cost, priced.Currency.V.MinorDigits))
	}
	return s.csvFile("charges", chargesHeader, rows)
}

func eventCells(e EventFields) []string {
	return []string{
		e.ID, strconv.FormatBool(e.Reconstructed),
		instant(e.Start.After), instant(e.Start.Before), instant(e.End.After), instant(e.End.Before),
	}
}

func capacityCells(c CapacityFields) []string {
	source := ""
	if c.CapacitySource.ok {
		source = string(c.CapacitySource.v)
	}
	return []string{num(c.CapacityKWh), source}
}

func positionCells(pos null[positionJSON], place null[placeRefJSON], address *string) []string {
	out := []string{"", "", "", textCell(address)}
	if pos.ok {
		out[0], out[1] = strconv.FormatFloat(pos.v.Lat, 'f', -1, 64), strconv.FormatFloat(pos.v.Lon, 'f', -1, 64)
	}
	if place.ok {
		out[2] = cell(place.v.Name)
	}
	return out
}

// costCells write the bounds in major units, with the currency's digits: what a
// spreadsheet sums.
func costCells(c null[costJSON], digits int) []string {
	if !c.ok {
		return make([]string, 7)
	}
	return []string{
		string(c.v.Currency), major(c.v.MinMinor, digits), major(c.v.MaxMinor, digits), string(c.v.Source),
		num(c.v.EnergyKWh), num(c.v.Efficiency), textCell(c.v.Note),
	}
}

func instant(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func num(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func textCell(v *string) string {
	if v == nil {
		return ""
	}
	return cell(*v)
}

// cell keeps a text a spreadsheet would take for a formula a text: a place's name, an
// address or a note may begin with one of its signs (OWASP's CSV injection).
func cell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// major writes an amount of minor units in major units: 524 cents, 5.24.
func major(minor int64, digits int) string {
	s := strconv.FormatInt(minor, 10)
	if digits == 0 {
		return s
	}
	if len(s) <= digits {
		s = strings.Repeat("0", digits-len(s)+1) + s
	}
	return s[:len(s)-digits] + "." + s[len(s)-digits:]
}

// registerCSV registers the CSV files of the trips and charges.
func (s *Server) registerCSV(api huma.API) {
	errs := map[int]string{
		http.StatusBadRequest:   "`invalid_parameter`: `from` or `to`; the message names the parameter.",
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errFeature, http.StatusNotFound: errVehicle,
		http.StatusInternalServerError: errInternal,
	}
	const description = "Every event of the period the list would give, as a CSV file (RFC 4180, UTF-8, a header row) " +
		"for a spreadsheet: oldest first, one row per event, its fields flattened and named after the JSON ones " +
		"(`start_after` is `start.after`). Times are RFC 3339 in UTC, numbers have a decimal point, an unknown value " +
		"is an empty cell. A text that would begin with `=`, `+`, `-` or `@` is prefixed with `'`, so that no " +
		"spreadsheet takes it for a formula."
	for _, op := range []struct {
		id, kind, summary, columns string
		handler                    func(context.Context, *csvInput) (*csvOutput, error)
	}{
		{"tripsCSV", "trips", "The trips of a vehicle, as CSV", "", s.tripsCSV},
		{
			"chargesCSV", "charges", "The charges of a vehicle, as CSV",
			" A charge's cost is in major units of the currency (`cost_min` and `cost_max`, 5.24 for 524 cents).", s.chargesCSV,
		},
	} {
		o := s.operation(huma.Operation{
			OperationID: op.id, Method: http.MethodGet, Path: "/vehicles/{vehicle}/" + op.kind + ".csv", Tags: []string{"events"},
			Summary: op.summary, Description: description + op.columns,
			Middlewares: huma.Middlewares{s.requireCSV},
		}, "The CSV file, as an attachment (Content-Disposition), named after the day.", errs)
		o.Responses["200"].Content = map[string]*huma.MediaType{
			"text/csv": {Schema: &huma.Schema{Type: huma.TypeString, Description: "The header row, then a row per event."}},
		}
		huma.Register(api, o, op.handler)
		// huma describes the output's headers itself, Content-Type among them, which
		// the content's media type already tells.
		api.OpenAPI().Paths[o.Path].Get.Responses["200"].Headers = map[string]*huma.Param{
			"Content-Disposition": {
				Description: "attachment, with a file name such as runsten-" + op.kind + "-2026-10-01.csv.", Required: true,
				Schema: &huma.Schema{Type: huma.TypeString},
			},
		}
	}
}
