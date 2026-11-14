// Package volvoportal reads the files of the Volvo Data Portal (data.volvocars.com), where
// an owner in the EU asks Volvo Cars for their vehicle's data under the Data Act: the
// trips, and the waypoints recorded while driving. It returns their values as they are,
// in the portal's units, and knows no business code.
//
// Both files are CSV, comma-separated, times in RFC 3339, each delivered zipped alone. Their columns are found by
// their header, compared without case nor whitespace, so that a reordered file is still
// read; a column of no use is ignored, an empty cell is absent.
package volvoportal

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Trip is a row of the trips' file.
type Trip struct {
	Start, End                   time.Time
	StartLat, StartLon           *float64 // decimal degrees
	EndLat, EndLon               *float64
	StartOdometerM, EndOdometerM *float64 // meters, rounded to the kilometer by the portal
	DistanceM                    *float64
	FuelL                        *float64 // always 0 for an electric car
	EnergyKWh, RegenerationKWh   *float64
}

// Waypoint is a row of the waypoints' file: about one a minute while driving.
type Waypoint struct {
	At        time.Time
	Lat, Lon  float64 // decimal degrees
	OdometerM *float64
	SpeedKmh  *float64
}

// FormatError is a file that is not the portal's: not CSV, or without a column every
// row needs.
type FormatError struct{ Reason string }

func (e *FormatError) Error() string { return "not a file of the Volvo Data Portal: " + e.Reason }

// MaxRows is the most rows a file may have: a year of waypoints is about 30,000.
const MaxRows = 500_000

var (
	tripColumns     = []string{"start time", "end time"}
	waypointColumns = []string{"timestamp", "latitude", "longitude"}
)

// readTrip is a row of the trips' file; false when its times are unreadable or a value
// is not a number.
func readTrip(row row) (Trip, bool) {
	start, okStart := row.time("start time")
	end, okEnd := row.time("end time")
	if !okStart || !okEnd {
		return Trip{}, false
	}
	t := Trip{Start: start, End: end}
	nums := []struct {
		col string
		v   **float64
	}{
		{"start latitude (decimal degrees)", &t.StartLat},
		{"start longitude (decimal degrees)", &t.StartLon},
		{"end latitude (decimal degrees)", &t.EndLat},
		{"end longitude (decimal degrees)", &t.EndLon},
		{"start odometer (meters)", &t.StartOdometerM},
		{"end odometer (meters)", &t.EndOdometerM},
		{"distance (meters)", &t.DistanceM},
		{"fuel consumption (liters)", &t.FuelL},
		{"electrical consumption (kwh)", &t.EnergyKWh},
		{"electrical regeneration (kwh)", &t.RegenerationKWh},
	}
	for _, n := range nums {
		v, ok := row.number(n.col)
		if !ok {
			return Trip{}, false
		}
		*n.v = v
	}
	return t, true
}

// readWaypoint is a row of the waypoints' file; false without its time or its position,
// or with a value that is not a number.
func readWaypoint(row row) (Waypoint, bool) {
	var w Waypoint
	at, okAt := row.time("timestamp")
	lat, okLat := row.number("latitude")
	lon, okLon := row.number("longitude")
	odo, okOdo := row.number("odometer")
	speed, okSpeed := row.number("speed (kph)")
	if !okAt || !okLat || !okLon || !okOdo || !okSpeed || lat == nil || lon == nil {
		return Waypoint{}, false
	}
	w.At, w.Lat, w.Lon, w.OdometerM, w.SpeedKmh = at, *lat, *lon, odo, speed
	return w, true
}

type row struct {
	cols   map[string]int
	fields []string
}

func (r row) cell(col string) string {
	i, ok := r.cols[normalize(col)]
	if !ok || i >= len(r.fields) {
		return ""
	}
	return strings.TrimSpace(r.fields[i])
}

func (r row) time(col string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, r.cell(col))
	return t, err == nil
}

// number is the cell's value, nil when empty or absent; false when it is not a number.
func (r row) number(col string) (*float64, bool) {
	s := r.cell(col)
	if s == "" {
		return nil, true
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// table is a CSV file whose header is read.
type table struct {
	c    *csv.Reader
	cols map[string]int
}

func open(r io.Reader) (table, error) {
	c := csv.NewReader(r)
	c.FieldsPerRecord = -1
	c.ReuseRecord = true
	header, err := c.Read()
	if err != nil {
		return table{}, formatErr(err)
	}
	cols := make(map[string]int, len(header))
	for i, h := range header {
		cols[normalize(h)] = i
	}
	return table{c: c, cols: cols}, nil
}

func (t table) has(col string) bool {
	_, ok := t.cols[normalize(col)]
	return ok
}

func (t table) rows(each func(row)) error {
	for n := 0; ; n++ {
		fields, err := t.c.Read()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return formatErr(err)
		case n >= MaxRows:
			return &FormatError{Reason: fmt.Sprintf("more than %d rows", MaxRows)}
		}
		each(row{cols: t.cols, fields: fields})
	}
}

// formatErr is a reading failure: the file's, unless the reader failed.
func formatErr(err error) error {
	var pe *csv.ParseError
	switch {
	case errors.Is(err, io.EOF):
		return &FormatError{Reason: "empty"}
	case errors.As(err, &pe):
		return &FormatError{Reason: pe.Error()}
	}
	return fmt.Errorf("reading the file: %w", err)
}

// normalize is a header as compared: lower case, without its byte order mark nor any
// whitespace (a header was seen broken over two lines).
func normalize(h string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimPrefix(h, "\ufeff")), ""))
}
