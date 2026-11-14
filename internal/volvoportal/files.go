package volvoportal

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// File is a file the portal gave, as the user uploads it.
type File struct {
	Name string
	R    io.ReaderAt
	Size int64
}

// Export is what the portal's files hold, merged: the portal splits its answer into many
// ZIPs, and the waypoints of a trip are not always in the ZIP whose name matches it.
type Export struct {
	Trips     []Trip
	Waypoints []Waypoint
	// Invalid counts the trips' rows left out; a waypoint left out only thins a track.
	Invalid int
}

// The bounds of an upload once inflated: a year of waypoints is a few megabytes of CSV.
const maxCSVFiles = 10_000

// maxInflated is a variable for the tests alone.
var maxInflated int64 = 256 << 20

// ReadFiles reads the files the portal gave, as downloaded: its ZIPs of one data.csv
// each, a ZIP of them all, or the CSVs themselves. Trips and waypoints are told apart by
// their header, never by their name. What holds no data (metadata.json, final.zip, a
// macOS resource fork) is ignored. Files without any trip return a *FormatError, and so
// does a file that is neither ZIP nor JSON nor the portal's CSV.
func ReadFiles(files []File) (Export, error) {
	r := reader{left: maxInflated}
	for _, f := range files {
		if err := r.file(f.Name, f.R, f.Size, true); err != nil {
			return Export{}, err
		}
	}
	if r.tripFiles == 0 {
		return Export{}, &FormatError{Reason: "no trips' file among them"}
	}
	return r.out, nil
}

type reader struct {
	out       Export
	tripFiles int
	csvFiles  int
	left      int64 // bytes that may still be inflated
}

var zipMagic = []byte("PK\x03\x04")

// file reads an uploaded file, or a ZIP inside one when outer is false: the portal's ZIPs
// may come zipped together, never deeper.
func (r *reader) file(name string, ra io.ReaderAt, size int64, outer bool) error {
	head := make([]byte, len(zipMagic))
	n, err := ra.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading %s: %w", name, err)
	}
	switch {
	case bytes.Equal(head[:n], zipMagic):
		return r.zip(name, ra, size, outer)
	case ignored(name):
		return nil
	}
	return r.csv(name, io.NewSectionReader(ra, 0, size))
}

// ignored is a file of the portal's answer that holds no data, or one an archiver adds.
func ignored(name string) bool {
	base := path.Base(name)
	return strings.EqualFold(path.Ext(base), ".json") || strings.HasPrefix(base, "._") || base == ".DS_Store"
}

func (r *reader) zip(name string, ra io.ReaderAt, size int64, outer bool) error {
	z, err := zip.NewReader(ra, size)
	if err != nil {
		return &FormatError{Reason: fmt.Sprintf("%s: %v", name, err)}
	}
	for _, f := range z.File {
		entry := name + "/" + f.Name
		ext := strings.ToLower(path.Ext(f.Name))
		switch {
		case f.FileInfo().IsDir(), ignored(f.Name), strings.HasPrefix(f.Name, "__MACOSX/"):
		case ext == ".csv":
			if err := r.member(entry, f, func(rd io.Reader) error { return r.csv(entry, rd) }); err != nil {
				return err
			}
		case ext == ".zip" && outer:
			err := r.member(entry, f, func(rd io.Reader) error {
				data, err := io.ReadAll(rd)
				if err != nil {
					return err //nolint:wrapcheck // member gives it its context
				}
				return r.file(entry, bytes.NewReader(data), int64(len(data)), false)
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// member opens a ZIP's member, bounded by what may still be inflated: its header may lie.
func (r *reader) member(name string, f *zip.File, read func(io.Reader) error) error {
	if f.UncompressedSize64 > uint64(r.left) { //nolint:gosec // left is never negative
		return r.tooLarge()
	}
	rc, err := f.Open()
	if err != nil {
		return &FormatError{Reason: fmt.Sprintf("%s: %v", name, err)}
	}
	defer rc.Close()
	lr := &io.LimitedReader{R: rc, N: r.left + 1}
	err = read(lr)
	r.left = max(lr.N-1, 0)
	var fe *FormatError
	switch {
	case lr.N == 0:
		return r.tooLarge()
	case errors.As(err, &fe):
		return err
	case err != nil:
		return &FormatError{Reason: fmt.Sprintf("%s: %v", name, err)}
	}
	return nil
}

func (r *reader) tooLarge() error {
	return &FormatError{Reason: fmt.Sprintf("more than %d MiB once inflated", maxInflated>>20)}
}

// csv reads a CSV of the portal, a trips' or a waypoints' file by its header.
func (r *reader) csv(name string, rd io.Reader) error {
	if r.csvFiles++; r.csvFiles > maxCSVFiles {
		return &FormatError{Reason: fmt.Sprintf("more than %d files", maxCSVFiles)}
	}
	t, err := open(bufio.NewReader(rd))
	if err != nil {
		return named(name, err)
	}
	switch {
	case hasAll(t, tripColumns):
		r.tripFiles++
		err = t.rows(func(row row) {
			if trip, ok := readTrip(row); ok {
				r.out.Trips = append(r.out.Trips, trip)
			} else {
				r.out.Invalid++
			}
		})
	case hasAll(t, waypointColumns):
		err = t.rows(func(row row) {
			if w, ok := readWaypoint(row); ok {
				r.out.Waypoints = append(r.out.Waypoints, w)
			}
		})
	default:
		return &FormatError{Reason: name + ": neither the trips nor the waypoints"}
	}
	return named(name, err)
}

func hasAll(t table, cols []string) bool {
	for _, c := range cols {
		if !t.has(c) {
			return false
		}
	}
	return true
}

// named puts the file's name in a format error: there are many.
func named(name string, err error) error {
	var fe *FormatError
	if errors.As(err, &fe) {
		return &FormatError{Reason: name + ": " + fe.Reason}
	}
	return err
}
