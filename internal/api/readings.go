package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Every value a vehicle reported, as a CSV file: the responses stored, each flattened
// into one row per leaf, in the long format a spreadsheet's pivot table takes. The
// fields are the providers' own, never Runsten's: a field a provider adds is in the
// file the day it comes, without a line of code.

// Responses reads the responses the providers gave, as they are stored.
type Responses interface {
	// EachResponse calls fn with each response stored of the vehicle fetched within
	// [from, to), oldest first, as of one snapshot, reading them as fn takes them. An
	// error of fn stops the read and is returned.
	EachResponse(ctx context.Context, accountID, vehicleID string, from, to time.Time, fn func(Response) error) error
}

// Response is a response stored: a run of identical ones is its first, and its
// payload is the provider's JSON.
type Response struct {
	Source, Endpoint string
	FetchedAt        time.Time
	Payload          []byte
}

// maxReadingsWindow bounds the period of the file: a month, the lists' default period,
// in any time zone (its 31 days may hold one of 25 hours). A Tesla read every 15 s
// while driving gives about 1,500 responses of 250 fields a day: a month is 11.7
// million rows, 1 GB (measured on its vehicle_data), written in under 5 s at a constant
// memory; past what a spreadsheet opens (1,048,576 rows), not what a script reads.
const maxReadingsWindow = 31*24*time.Hour + time.Hour

var readingsHeader = []string{"fetched_at", "source", "endpoint", "field", "value"}

type readingsCSVInput struct {
	Vehicle string    `path:"vehicle" doc:"The vehicle ID."`
	From    time.Time `query:"from" required:"true" doc:"Keeps the responses fetched at or after this time." example:"2026-09-28T00:00:00Z"`
	To      time.Time `query:"to" required:"true" doc:"Keeps the responses fetched before this time: at most 31 days after from." example:"2026-09-29T00:00:00Z"`
}

// readingsCSV streams the file. Its status goes with its first bytes: a failure before
// them is a 500 of the contract, one after them cuts the file short.
func (s *Server) readingsCSV(ctx context.Context, in *readingsCSVInput) (*huma.StreamResponse, error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	from, to := in.From.UTC(), in.To.UTC()
	switch {
	case !from.Before(to):
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "from must be before to")
	case to.Sub(from) > maxReadingsWindow:
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "to: at most 31 days after from")
	}
	account := sessionFrom(ctx).AccountID
	l, err := s.limitsOf(ctx, account)
	if err != nil {
		return nil, err
	}
	from = later(from, l.HistoryFrom)
	name := `attachment; filename="runsten-readings-` + s.Clock.Now().UTC().Format(time.DateOnly) + `.csv"`
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		sw := &startWriter{ctx: hctx, start: func(c huma.Context) {
			c.SetHeader("Content-Type", "text/csv; charset=utf-8")
			c.SetHeader("Content-Disposition", name)
			c.SetStatus(http.StatusOK)
		}}
		// The header row stays in the buffer until the first responses fill it: a
		// failure to read them is still a 500.
		w := csv.NewWriter(bufio.NewWriterSize(sw, 64<<10))
		w.UseCRLF = true
		_ = w.Write(readingsHeader) // into the buffer
		if from.Before(to) {
			err = s.Responses.EachResponse(ctx, account, v.ID, from, to, func(r Response) error {
				return s.writeReadings(w, r)
			})
		}
		if err == nil {
			w.Flush()
			err = w.Error()
		}
		switch {
		case err != nil && !sw.started:
			_, rw := unwrap(hctx)
			writeError(rw, http.StatusInternalServerError, codeInternal, "internal error, see the logs")
			s.Log.Error("readings not read", "err", err)
		case err != nil:
			s.Log.Error("readings cut short", "err", err)
		}
	}}, nil
}

// writeReadings writes the rows of a response, its private fields left out. A source
// that declares none is left out whole: a source added without its declaration could
// hold a token.
func (s *Server) writeReadings(w *csv.Writer, r Response) error {
	private, ok := s.PrivateFields[r.Source]
	if !ok {
		return nil
	}
	at := instant(r.FetchedAt)
	err := leaves(r.Payload, private, func(field, value string) error {
		return w.Write([]string{at, r.Source, r.Endpoint, field, value}) //nolint:wrapcheck // the response's writer
	})
	if err != nil {
		return fmt.Errorf("%s %s at %s: %w", r.Source, r.Endpoint, at, err)
	}
	return nil
}

// leaves calls emit with each leaf of a JSON document: its path (data.odometer.value,
// an array's elements as [0], an object's keys in order) and its value as the provider
// wrote it. A null is an empty cell; an empty object or array is a leaf, {} or [], so
// that a field the provider sent empty is seen. The paths of private are left out,
// with everything under them.
func leaves(doc []byte, private []string, emit func(field, value string) error) error {
	d := json.NewDecoder(bytes.NewReader(doc))
	d.UseNumber() // a number as it was written: 80.50 stays 80.50
	var v any
	if err := d.Decode(&v); err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	skip := make(map[string]bool, len(private))
	for _, p := range private {
		skip[p] = true
	}
	return walk("", v, skip, emit)
}

func walk(path string, v any, skip map[string]bool, emit func(field, value string) error) error {
	switch v := v.(type) {
	case map[string]any:
		if len(v) == 0 {
			return emit(path, "{}")
		}
		for _, k := range slices.Sorted(maps.Keys(v)) {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if skip[p] {
				continue
			}
			if err := walk(p, v[k], skip, emit); err != nil {
				return err
			}
		}
		return nil
	case []any:
		if len(v) == 0 {
			return emit(path, "[]")
		}
		for i, e := range v {
			if err := walk(path+"["+strconv.Itoa(i)+"]", e, skip, emit); err != nil {
				return err
			}
		}
		return nil
	case string:
		return emit(path, cell(v))
	case json.Number:
		return emit(path, string(v))
	case bool:
		return emit(path, strconv.FormatBool(v))
	default: // null
		return emit(path, "")
	}
}

// registerReadings registers the CSV file of the readings.
func (s *Server) registerReadings(api huma.API) {
	o := s.operation(huma.Operation{
		OperationID: "readingsCSV", Method: http.MethodGet, Path: "/vehicles/{vehicle}/readings.csv", Tags: []string{"vehicles"},
		Summary: "Every value a vehicle reported, as CSV",
		Description: "The responses the providers gave for the vehicle within the period, as stored (a response " +
			"identical to the previous one of its endpoint is not stored again), as a CSV file (RFC 4180, UTF-8, a " +
			"header row) for a spreadsheet: one row per value, oldest first, with fetched_at (RFC 3339 in UTC), the " +
			"source (volvo, polestar, tesla), the endpoint and the field, the path of the value in the response " +
			"(data.frontLeftDoor.value, an array's elements as [0]), and the value as the provider gave it: a null is " +
			"an empty cell, an empty object or array {} or []. The fields are the providers' own, which may change " +
			"without notice. What is secret or not the vehicle's (Tesla's tokens and account identifiers) is left " +
			"out. A text that would begin with `=`, `+`, `-` or `@` is prefixed with `'`, so that no spreadsheet " +
			"takes it for a formula. The file is written as it is read: a failure midway cuts it short.",
		Middlewares: huma.Middlewares{s.requireCSV},
	}, "The CSV file, as an attachment (Content-Disposition), named after the day.", map[int]string{
		http.StatusBadRequest:   "`invalid_parameter`: `from` or `to`, missing, in the wrong order or more than 31 days apart; the message names the parameter.",
		http.StatusUnauthorized: errUnauthorized, http.StatusForbidden: errFeature, http.StatusNotFound: errVehicle,
		http.StatusInternalServerError: errInternal,
	})
	o.Responses["200"].Content = map[string]*huma.MediaType{
		"text/csv": {Schema: &huma.Schema{Type: huma.TypeString, Description: "The header row, then a row per value."}},
	}
	o.Responses["200"].Headers = map[string]*huma.Param{
		"Content-Disposition": {
			Description: "attachment, with a file name such as runsten-readings-2026-10-01.csv.", Required: true,
			Schema: &huma.Schema{Type: huma.TypeString},
		},
	}
	huma.Register(api, o, s.readingsCSV)
}
