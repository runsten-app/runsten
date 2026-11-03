// Package teslamate reads the backup of a TeslaMate instance: the plain SQL file its
// documentation tells every user to make (`pg_dump -U teslamate teslamate > teslamate.bck`).
// It returns the rows of the tables asked for as they are, in TeslaMate's units, and knows
// no business code.
//
// The file is read as a stream, without PostgreSQL: the `COPY … FROM stdin;` block of each
// table asked for is read in PostgreSQL's text format (one row a line, tab-separated,
// `\N` for NULL, backslash escapes), every other line skipped. Columns are found by name
// in the block's header, so that a column a release of TeslaMate adds or moves breaks
// nothing; one asked for and missing fails with the table and the column named. A file
// compressed with gzip is read as well: a backup is several times smaller so.
package teslamate

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Table is a table to read: its name, without schema, and the columns its rows need.
type Table struct {
	Name    string
	Columns []string
}

// Row is a row of a table, its values those of the columns asked for. It is only valid
// during the call it is given to: the next row reuses it.
type Row struct {
	table  string
	cols   map[string]int
	values []string
	null   []bool
}

// FormatError is a file that is not a plain dump of TeslaMate's database, or that is
// cut short.
type FormatError struct{ Reason string }

func (e *FormatError) Error() string { return "not a backup of TeslaMate: " + e.Reason }

// Errors of a file in another format than the plain one, worded for the user.
var (
	// ErrCustomFormat is the archive `pg_dump -Fc` writes (TeslaMate's backup is plain).
	ErrCustomFormat = &FormatError{Reason: "a custom-format archive (pg_dump -Fc): make the backup " +
		"as TeslaMate's documentation says, `docker compose exec -T database pg_dump -U teslamate " +
		"teslamate > teslamate.bck`, without -F"}
	// ErrCompressed is a file compressed otherwise than with gzip.
	ErrCompressed = &FormatError{Reason: "a compressed file: decompress it, or compress it with gzip"}
	// ErrInserts is a dump made with --inserts.
	ErrInserts = &FormatError{Reason: "made with INSERT statements (pg_dump --inserts): make it " +
		"without --inserts"}
)

const (
	// bufferSize is the reading buffer: a TeslaMate row is under 1 KiB.
	bufferSize = 64 << 10
	// MaxRowBytes is the longest row of a table asked for: a longer line is no row of
	// TeslaMate's.
	MaxRowBytes = 1 << 20
)

// Read reads the dump r and calls each with every row of the tables asked for, in the
// file's order (pg_dump's: by table name, `charges` before `charging_processes`,
// `drives` before `positions`). A table asked for and missing fails, once the whole file
// is read. An error each returns stops the reading and is returned as it is.
func Read(r io.Reader, tables []Table, each func(Row) error) error {
	return read(r, tables, each, MaxBytes)
}

// MaxBytes bounds a backup once decompressed: a few years of TeslaMate are about a
// gigabyte. Beyond, a file of a few megabytes gzipped could keep the reading going for
// as long as it expands.
const MaxBytes = 8 << 30

func read(r io.Reader, tables []Table, each func(Row) error, limit int64) error {
	br := bufio.NewReaderSize(r, bufferSize)
	if head, _ := br.Peek(2); bytes.Equal(head, []byte{0x1f, 0x8b}) {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return &FormatError{Reason: "a gzip file not understood: " + err.Error()}
		}
		defer zr.Close()
		br = bufio.NewReaderSize(&limited{r: gzipFile{zr}, left: limit}, bufferSize)
	} else {
		br = bufio.NewReaderSize(&limited{r: br, left: limit}, bufferSize)
	}
	if err := checkFormat(br); err != nil {
		return err
	}
	wanted := make(map[string]Table, len(tables))
	for _, t := range tables {
		wanted[t.Name] = t
	}
	seen := make(map[string]bool, len(tables))
	inserts := false
	var line []byte
	for n := 1; ; n++ {
		var err error
		line, err = readLine(br, line[:0], false)
		if errors.Is(err, io.EOF) && len(line) == 0 {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		switch {
		case bytes.HasPrefix(line, []byte("COPY ")):
			name, cols, ok := parseCopy(string(line))
			t, want := wanted[name]
			if !want || seen[name] {
				read, err := skipCopy(br)
				n += read
				if err != nil {
					return err
				}
				continue
			}
			if !ok {
				return &FormatError{Reason: fmt.Sprintf("line %d: a COPY of %s not understood", n, name)}
			}
			seen[name] = true
			read, err := readCopy(br, n, t, cols, each)
			n += read
			if err != nil {
				return err
			}
		case bytes.HasPrefix(line, []byte("INSERT INTO ")):
			if name, _, _ := parseIdent(string(line[len("INSERT INTO "):])); wanted[name].Name != "" {
				inserts = true
			}
		}
	}
	for _, t := range tables {
		if !seen[t.Name] {
			if inserts {
				return ErrInserts
			}
			return &FormatError{Reason: "no table " + t.Name}
		}
	}
	return nil
}

// gzipFile tells a gzip file cut short or corrupt as such: the reader's own failure, not
// the connection's, which stays as it is.
type gzipFile struct{ r io.Reader }

func (g gzipFile) Read(p []byte) (int, error) {
	n, err := g.r.Read(p)
	var corrupt flate.CorruptInputError
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, gzip.ErrChecksum) || errors.Is(err, gzip.ErrHeader) ||
		errors.As(err, &corrupt) {
		return n, &FormatError{Reason: "a gzip file cut short or corrupt"}
	}
	return n, err //nolint:wrapcheck // the reader's, as it is
}

// limited fails past left bytes.
type limited struct {
	r    io.Reader
	left int64
}

func (l *limited) Read(p []byte) (int, error) {
	if l.left <= 0 {
		// A file of exactly the limit ends there.
		var one [1]byte
		if n, err := l.r.Read(one[:]); n == 0 && err != nil {
			return 0, err //nolint:wrapcheck // the reader's, as it is
		}
		return 0, &FormatError{Reason: fmt.Sprintf("larger than %d GiB once decompressed", MaxBytes>>30)}
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err //nolint:wrapcheck // the reader's, as it is
}

// checkFormat refuses the formats a user may give instead of the plain one.
func checkFormat(br *bufio.Reader) error {
	head, err := br.Peek(5)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading the backup: %w", err)
	}
	switch {
	case len(head) == 0:
		return &FormatError{Reason: "empty"}
	case bytes.HasPrefix(head, []byte("PGDMP")):
		return ErrCustomFormat
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}), bytes.HasPrefix(head, []byte{0x28, 0xb5, 0x2f, 0xfd}),
		bytes.HasPrefix(head, []byte("BZh")), bytes.HasPrefix(head, []byte{0xfd, '7', 'z', 'X', 'Z'}):
		return ErrCompressed // gzip within gzip, zstd, bzip2, xz
	}
	return nil
}

// readLine appends the next line to buf, without its end. Without keep, a line is kept up
// to about bufferSize bytes, the rest skipped; with keep, a line longer than MaxRowBytes
// fails.
func readLine(br *bufio.Reader, buf []byte, keep bool) ([]byte, error) {
	for {
		chunk, err := br.ReadSlice('\n')
		if keep && len(buf)+len(bytes.TrimSuffix(chunk, []byte("\n"))) > MaxRowBytes {
			return buf, &FormatError{Reason: fmt.Sprintf("a row longer than %d bytes", MaxRowBytes)}
		}
		switch {
		case err == nil:
			buf = append(buf, chunk[:len(chunk)-1]...)
			return bytes.TrimSuffix(buf, []byte("\r")), nil
		case errors.Is(err, bufio.ErrBufferFull):
			// A statement skipped needs only its start: keep what tells it apart.
			if keep || len(buf) < bufferSize {
				buf = append(buf, chunk...)
			}
		case errors.Is(err, io.EOF):
			return append(buf, chunk...), io.EOF
		default:
			return buf, fmt.Errorf("reading the backup: %w", err)
		}
	}
}

// skipCopy skips a COPY block up to its end, and gives the lines it read.
func skipCopy(br *bufio.Reader) (int, error) {
	var line []byte
	for n := 1; ; n++ {
		var err error
		line, err = readLine(br, line[:0], false)
		if string(line) == `\.` {
			return n, nil
		}
		if errors.Is(err, io.EOF) {
			return n, &FormatError{Reason: "cut short"}
		}
		if err != nil {
			return n, err
		}
	}
}

// readCopy reads the rows of t's COPY block, whose header at line start names cols, up to
// its end, and gives the lines it read.
func readCopy(br *bufio.Reader, start int, t Table, cols []string, each func(Row) error) (int, error) {
	slot := make([]int, len(cols)) // the column of the file's i-th field in the row, or -1
	for i := range slot {
		slot[i] = -1
	}
	row := Row{
		table:  t.Name,
		cols:   make(map[string]int, len(t.Columns)),
		values: make([]string, len(t.Columns)),
		null:   make([]bool, len(t.Columns)),
	}
	for j, c := range t.Columns {
		i := slices.Index(cols, c)
		if i < 0 {
			return 0, &FormatError{Reason: fmt.Sprintf("table %s has no column %s", t.Name, c)}
		}
		slot[i] = j
		row.cols[c] = j
	}
	var line []byte
	for n := 1; ; n++ {
		var err error
		line, err = readLine(br, line[:0], true)
		if string(line) == `\.` {
			return n, nil
		}
		if errors.Is(err, io.EOF) {
			return n, &FormatError{Reason: fmt.Sprintf("cut short in table %s", t.Name)}
		}
		if err != nil {
			return n, err
		}
		fields := 0
		for i, f := range splitTabs(line) {
			fields = i + 1
			if i >= len(slot) || slot[i] < 0 {
				continue
			}
			j := slot[i]
			row.null[j] = string(f) == `\N`
			if row.null[j] {
				row.values[j] = ""
				continue
			}
			if row.values[j], err = unescape(f); err != nil {
				return n, &FormatError{Reason: fmt.Sprintf("line %d: %v", start+n, err)}
			}
		}
		if fields != len(cols) {
			return n, &FormatError{Reason: fmt.Sprintf("line %d: %d values in table %s, %d columns",
				start+n, fields, t.Name, len(cols))}
		}
		if err := each(row); err != nil {
			return n, err
		}
	}
}

// splitTabs yields the fields of a row: a tab within a value is escaped, so every tab
// separates two.
func splitTabs(line []byte) func(func(int, []byte) bool) {
	return func(yield func(int, []byte) bool) {
		for i := 0; ; i++ {
			f, rest, more := bytes.Cut(line, []byte{'\t'})
			if !yield(i, f) || !more {
				return
			}
			line = rest
		}
	}
}

// unescape decodes a value of COPY's text format: `\b`, `\f`, `\n`, `\r`, `\t`, `\v`,
// `\` and one to three octal digits, `\x` and one or two hex digits; a backslash before
// any other character stands for that character.
func unescape(f []byte) (string, error) {
	if bytes.IndexByte(f, '\\') < 0 {
		return string(f), nil
	}
	out := make([]byte, 0, len(f))
	for i := 0; i < len(f); i++ {
		c := f[i]
		if c != '\\' {
			out = append(out, c)
			continue
		}
		i++
		if i == len(f) {
			return "", errors.New("a value ends with a backslash")
		}
		switch c = f[i]; c {
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, '\v')
		case 'x':
			j, v := i+1, byte(0)
			for ; j < len(f) && j < i+3; j++ {
				d, ok := hexDigit(f[j])
				if !ok {
					break
				}
				v = v<<4 | d
			}
			if j == i+1 {
				out = append(out, 'x')
				continue
			}
			out = append(out, v)
			i = j - 1
		default:
			if c < '0' || c > '7' {
				out = append(out, c)
				continue
			}
			j, v := i, byte(0)
			for ; j < len(f) && j < i+3 && f[j] >= '0' && f[j] <= '7'; j++ {
				v = v<<3 | (f[j] - '0') // \777 wraps around, as PostgreSQL's
			}
			out = append(out, v)
			i = j - 1
		}
	}
	return string(out), nil
}

// hexDigit is a hex digit's value; false for another character.
func hexDigit(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// identChar tells a character of a name pg_dump leaves unquoted.
func identChar(r rune) bool {
	return r == '_' || r == '$' || 'a' <= r && r <= 'z' || '0' <= r && r <= '9'
}

// parseCopy reads the header of a COPY block, `COPY schema.table (col, …) FROM stdin;`:
// the table's name without its schema, and its columns; ok false when it has no column
// list or no FROM stdin.
func parseCopy(line string) (name string, cols []string, ok bool) {
	name, rest, okName := parseIdent(strings.TrimPrefix(line, "COPY "))
	if !okName {
		return "", nil, false
	}
	rest, found := strings.CutPrefix(rest, " (")
	if !found {
		return name, nil, false
	}
	for {
		var col string
		var okCol bool
		col, rest, okCol = parseIdent(rest)
		if !okCol {
			return name, nil, false
		}
		cols = append(cols, col)
		if after, more := strings.CutPrefix(rest, ", "); more {
			rest = after
			continue
		}
		return name, cols, rest == ") FROM stdin;"
	}
}

// parseIdent reads a name, maybe qualified, maybe quoted (`"row"`, `""` within a quoted
// name), at the start of s: the last part of it, and what follows.
func parseIdent(s string) (name, rest string, ok bool) {
	for {
		var part string
		if strings.HasPrefix(s, `"`) {
			var b strings.Builder
			i := 1
			for {
				j := strings.IndexByte(s[i:], '"')
				if j < 0 {
					return "", s, false
				}
				b.WriteString(s[i : i+j])
				i += j + 1
				if !strings.HasPrefix(s[i:], `"`) {
					break
				}
				b.WriteByte('"')
				i++
			}
			part, s = b.String(), s[i:]
		} else {
			end := strings.IndexFunc(s, func(r rune) bool { return !identChar(r) })
			if end < 0 {
				end = len(s)
			}
			if end == 0 {
				return "", s, false
			}
			part, s = s[:end], s[end:]
		}
		if after, more := strings.CutPrefix(s, "."); more {
			s = after
			continue
		}
		return part, s, true
	}
}

// Table is the row's table.
func (r Row) Table() string { return r.table }

func (r Row) value(col string) (string, bool) {
	j, ok := r.cols[col]
	if !ok {
		panic("teslamate: column " + col + " of " + r.table + " not asked for")
	}
	return r.values[j], !r.null[j]
}

// Text is the column's text, nil when NULL.
func (r Row) Text(col string) *string {
	v, ok := r.value(col)
	if !ok {
		return nil
	}
	return &v
}

// Int is the column's integer, nil when NULL; false when it is not one.
func (r Row) Int(col string) (*int64, bool) {
	return parse(r, col, func(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) })
}

// Float is the column's number (a double precision or a numeric), nil when NULL; false
// when it is not one.
func (r Row) Float(col string) (*float64, bool) {
	return parse(r, col, func(s string) (float64, error) { return strconv.ParseFloat(s, 64) })
}

// Bool is the column's boolean, nil when NULL; false when it is not one.
func (r Row) Bool(col string) (*bool, bool) {
	return parse(r, col, func(s string) (bool, error) {
		switch s {
		case "t":
			return true, nil
		case "f":
			return false, nil
		}
		return false, errors.New("not a boolean")
	})
}

// timeLayouts are the timestamps pg_dump writes (DateStyle ISO): TeslaMate's are without
// a time zone, in UTC; with one, its offset in hours, or hours and minutes.
var timeLayouts = []string{
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05.999999999-07",
	"2006-01-02 15:04:05.999999999-07:00",
}

// Time is the column's time, in UTC, nil when NULL; false when it is not one.
func (r Row) Time(col string) (*time.Time, bool) {
	return parse(r, col, func(s string) (time.Time, error) {
		for _, layout := range timeLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC(), nil
			}
		}
		return time.Time{}, errors.New("not a time")
	})
}

func parse[T any](r Row, col string, conv func(string) (T, error)) (*T, bool) {
	s, ok := r.value(col)
	if !ok {
		return nil, true
	}
	v, err := conv(s)
	if err != nil {
		return nil, false
	}
	return &v, true
}
